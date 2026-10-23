package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

func compressForTest(t *testing.T, coding string, body []byte) []byte {
	t.Helper()
	switch coding {
	case "gzip":
		var buf bytes.Buffer
		w := gzip.NewWriter(&buf)
		if _, err := w.Write(body); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	case "br":
		var buf bytes.Buffer
		w := brotli.NewWriterLevel(&buf, brotli.DefaultCompression)
		if _, err := w.Write(body); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	case "zstd":
		enc, err := zstd.NewWriter(nil)
		if err != nil {
			t.Fatal(err)
		}
		defer enc.Close()
		return enc.EncodeAll(body, nil)
	default:
		t.Fatalf("unknown coding %q", coding)
		return nil
	}
}

func compressedDaemonServer(t *testing.T, coding string, jsonBody []byte) *httptest.Server {
	t.Helper()
	compressed := compressForTest(t, coding, jsonBody)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ae := r.Header.Get("Accept-Encoding")
		for _, want := range []string{"zstd", "br", "gzip"} {
			if !strings.Contains(ae, want) {
				t.Errorf("Accept-Encoding = %q, missing %q", ae, want)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", coding)
		_, _ = w.Write(compressed)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestExecDaemonCompressed(t *testing.T) {
	payload := strings.Repeat("ID\t | Name0\t | Alice", 4000)
	jsonBody, _ := json.Marshal(map[string]any{"status": 1, "msg": payload})

	for _, coding := range []string{"zstd", "br", "gzip"} {
		t.Run(coding, func(t *testing.T) {
			srv := compressedDaemonServer(t, coding, jsonBody)
			got, err := execDaemon(context.Background(), daemonEntry(t, srv), "listplayers")
			if err != nil {
				t.Fatalf("execDaemon: %v", err)
			}
			if got != payload {
				t.Fatalf("decoded %d bytes, want %d", len(got), len(payload))
			}
		})
	}
}

// TestExecDaemonCompressedLarge makes sure the decoded size limit is applied
// after decompression, so a compressed multi-megabyte reply still arrives whole.
func TestExecDaemonCompressedLarge(t *testing.T) {
	payload := strings.Repeat("x", 2<<20)
	jsonBody, _ := json.Marshal(map[string]any{"status": 1, "msg": payload})
	srv := compressedDaemonServer(t, "gzip", jsonBody)

	got, err := execDaemon(context.Background(), daemonEntry(t, srv), "listgamemodeproperties")
	if err != nil {
		t.Fatalf("execDaemon: %v", err)
	}
	if got != payload {
		t.Fatalf("decoded %d bytes, want %d", len(got), len(payload))
	}
}

func TestExecDaemonIdentityEncoding(t *testing.T) {
	payload := strings.Repeat("y", 4096)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "identity")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": 1, "msg": payload})
	}))
	defer srv.Close()

	got, err := execDaemon(context.Background(), daemonEntry(t, srv), "list")
	if err != nil || got != payload {
		t.Fatalf("got=%d bytes err=%v", len(got), err)
	}
}

func TestExecDaemonUnsupportedEncoding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "deflate")
		_, _ = w.Write([]byte(`{"status":1,"msg":"hello"}`))
	}))
	defer srv.Close()

	if _, err := execDaemon(context.Background(), daemonEntry(t, srv), "list"); err == nil ||
		!strings.Contains(err.Error(), "unsupported Content-Encoding") {
		t.Fatalf("want unsupported encoding error, got %v", err)
	}
}
