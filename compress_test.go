package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

func TestNegotiateEncoding(t *testing.T) {
	cases := []struct {
		header      string
		wantCoding  string
		wantIDOK    bool
		description string
	}{
		{"", "", true, "no header means identity"},
		{"gzip", "gzip", true, "single coding"},
		{"br", "br", true, "brotli"},
		{"zstd", "zstd", true, "zstd"},
		{"zstd, br, gzip", "zstd", true, "daemon preference on equal q"},
		{"gzip, br", "br", true, "br preferred over gzip on equal q"},
		{"gzip;q=0.5, br;q=0.9", "br", true, "q wins"},
		{"gzip;q=0.9, br;q=0.5", "gzip", true, "q wins"},
		{"gzip;q=0, br;q=0, zstd;q=0", "", true, "all codings refused"},
		{"gzip;q=0, br;q=0, zstd;q=0, identity;q=0", "", false, "nothing acceptable"},
		{"*", "zstd", true, "wildcard accepts the server preference"},
		{"*;q=0", "", false, "wildcard refusal also excludes identity"},
		{"gzip, *;q=0", "gzip", false, "explicit coding survives wildcard refusal"},
		{"identity;q=1, gzip;q=0.5", "", true, "explicit identity preference"},
		{"identity;q=0, gzip;q=0.5", "gzip", false, "identity explicitly refused"},
		{"deflate", "", true, "unsupported coding falls back to identity"},
		{"GZIP", "gzip", true, "case insensitive"},
		{"x-gzip", "gzip", true, "legacy gzip alias"},
		{"gzip;q=bogus", "gzip", true, "malformed q defaults to 1"},
		{"gzip; q = 0.5, br", "br", true, "whitespace around q"},
		{"gzip;q=2", "gzip", true, "q clamped to 1"},
		{"gzip;q=-1", "", true, "q clamped to 0"},
		{" gzip ; q=0.8 ", "gzip", true, "surrounding whitespace"},
		{"deflate, gzip;q=0.1, br;q=0.2", "br", true, "best of the supported set"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			coding, idOK := negotiateEncoding(tc.header)
			if coding != tc.wantCoding || idOK != tc.wantIDOK {
				t.Fatalf("negotiate(%q) = (%q, %v), want (%q, %v)", tc.header, coding, idOK, tc.wantCoding, tc.wantIDOK)
			}
		})
	}
}

func decodeCompressed(t *testing.T, coding string, raw []byte) []byte {
	t.Helper()
	var r io.Reader
	switch coding {
	case "zstd":
		d, err := zstd.NewReader(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("zstd reader: %v", err)
		}
		defer d.Close()
		r = d
	case "br":
		r = brotli.NewReader(bytes.NewReader(raw))
	case "gzip":
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("gzip reader: %v", err)
		}
		defer zr.Close()
		r = zr
	default:
		t.Fatalf("unknown coding %q", coding)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("decompress %s: %v", coding, err)
	}
	return out
}

// TestCompressedJSONResponses exercises the full path: negotiate, compress,
// header, and round-trip of a large RCON reply.
func TestCompressedJSONResponses(t *testing.T) {
	payload := makePayload(120_000)
	addr := startFakeRCON(t, &fakeRCON{
		password: "pw",
		chunk:    4082,
		mirror:   true,
		reply: func(cmd string) []byte {
			if cmd == "listgamemodeproperties" {
				return payload
			}
			return []byte("OK:" + cmd)
		},
	})
	host, port := splitAddr(t, addr)
	body, _ := json.Marshal(RCONRequest{
		IP: host, Port: port, Passwd: "pw", Msg: "listgamemodeproperties", Auth: "k",
	})
	h := rconHandler("k")

	for _, coding := range []string{"zstd", "br", "gzip"} {
		t.Run(coding, func(t *testing.T) {
			w := postRCONEncoding(t, h, "/api/icu/daemon/rcon", string(body), coding)
			if got := w.Header().Get("Content-Encoding"); got != coding {
				t.Fatalf("Content-Encoding = %q, want %q", got, coding)
			}
			if got := w.Header().Get("Vary"); got != "Accept-Encoding" {
				t.Fatalf("Vary = %q", got)
			}
			if got := w.Header().Get("Content-Length"); got != strconv.Itoa(w.Body.Len()) {
				t.Fatalf("Content-Length = %q, body = %d", got, w.Body.Len())
			}
			if w.Body.Len() >= len(payload)/2 {
				t.Fatalf("%s did not compress a 120 KB payload (%d bytes)", coding, w.Body.Len())
			}
			var resp RCONResponse
			if err := json.Unmarshal(decodeCompressed(t, coding, w.Body.Bytes()), &resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.Status != 1 || resp.Msg != string(payload) {
				t.Fatalf("round-trip mismatch: status=%d len=%d", resp.Status, len(resp.Msg))
			}
		})
	}
}

func TestCompressionHeaderVariants(t *testing.T) {
	payload := makePayload(50_000)
	addr := startFakeRCON(t, &fakeRCON{
		password: "pw",
		mirror:   true,
		reply:    func(string) []byte { return payload },
	})
	host, port := splitAddr(t, addr)
	body, _ := json.Marshal(RCONRequest{IP: host, Port: port, Passwd: "pw", Msg: "list", Auth: "k"})
	h := rconHandler("k")

	t.Run("no header stays identity", func(t *testing.T) {
		w := postRCON(t, h, "/api/icu/daemon/rcon", string(body))
		if got := w.Header().Get("Content-Encoding"); got != "" {
			t.Fatalf("Content-Encoding = %q, want identity", got)
		}
		var resp RCONResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || resp.Msg != string(payload) {
			t.Fatalf("identity response invalid: %v", err)
		}
	})

	t.Run("browser and backend style lists", func(t *testing.T) {
		for _, header := range []string{"gzip, deflate", "deflate, gzip"} {
			w := postRCONEncoding(t, h, "/api/icu/daemon/rcon", string(body), header)
			if got := w.Header().Get("Content-Encoding"); got != "gzip" {
				t.Fatalf("%q -> %q", header, got)
			}
		}
	})

	t.Run("wildcard picks zstd", func(t *testing.T) {
		w := postRCONEncoding(t, h, "/api/icu/daemon/rcon", string(body), "*")
		if got := w.Header().Get("Content-Encoding"); got != "zstd" {
			t.Fatalf("Content-Encoding = %q, want zstd", got)
		}
	})

	t.Run("unsupported coding stays identity", func(t *testing.T) {
		w := postRCONEncoding(t, h, "/api/icu/daemon/rcon", string(body), "deflate")
		if got := w.Header().Get("Content-Encoding"); got != "" {
			t.Fatalf("Content-Encoding = %q, want identity", got)
		}
	})
}

func TestSmallResponseNotCompressed(t *testing.T) {
	addr := startFakeRCON(t, &fakeRCON{
		password: "pw",
		mirror:   true,
		reply:    func(cmd string) []byte { return []byte("OK:" + cmd) },
	})
	host, port := splitAddr(t, addr)
	body, _ := json.Marshal(RCONRequest{IP: host, Port: port, Passwd: "pw", Msg: "list", Auth: "k"})

	w := postRCONEncoding(t, rconHandler("k"), "/api/icu/daemon/rcon", string(body), "gzip")
	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("small response must stay identity, got %q", got)
	}
	if got := w.Header().Get("Content-Length"); got != strconv.Itoa(w.Body.Len()) {
		t.Fatalf("Content-Length = %q, body = %d", got, w.Body.Len())
	}
}

func TestNotAcceptable(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/icu/daemon/rcon", strings.NewReader("{}"))
	req.Header.Set("Accept-Encoding", "identity;q=0, gzip;q=0, br;q=0, zstd;q=0")
	w := httptest.NewRecorder()

	acceptEncodingMiddleware(rconHandler("k")).ServeHTTP(w, req)
	if w.Code != http.StatusNotAcceptable {
		t.Fatalf("code = %d, want 406", w.Code)
	}
}

func TestAcceptEncodingHeaderMultipleLines(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Add("Accept-Encoding", "gzip")
	req.Header.Add("Accept-Encoding", "br;q=0.5")
	if got := acceptEncodingHeader(req); got != "gzip,br;q=0.5" {
		t.Fatalf("joined header = %q", got)
	}
}
