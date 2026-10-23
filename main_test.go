package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeRCON is a configurable Source RCON server for handler tests. It can split
// replies into many packets, mirror the empty end marker like Sandstorm, send a
// stray packet before the auth reply, or drop the connection on demand.
type fakeRCON struct {
	password  string
	reply     func(cmd string) []byte // nil means "send no reply"
	chunk     int                     // >0 splits each reply into packets of at most chunk bytes
	mirror    bool                    // mirror empty type-0 packets back
	strayAuth bool                    // send a stray packet before the auth reply
	dropOnCmd string                  // close the connection when this command arrives
}

func startFakeRCON(t *testing.T, s *fakeRCON) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(conn)
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return ln.Addr().String()
}

func (s *fakeRCON) serve(conn net.Conn) {
	defer conn.Close()
	for {
		reqid, typ, body, err := readTestPacket(conn)
		if err != nil {
			return
		}
		switch typ {
		case 3: // auth
			if s.strayAuth {
				if writeTestPacket(conn, reqid, 0, nil) != nil {
					return
				}
			}
			rid := reqid
			if string(body) != s.password {
				rid = -1
			}
			if writeTestPacket(conn, rid, 2, nil) != nil {
				return
			}
		case 0: // client's empty end marker
			if s.mirror {
				if writeTestPacket(conn, reqid, 0, nil) != nil {
					return
				}
			}
		case 2: // exec
			cmd := string(body)
			if s.dropOnCmd == cmd {
				return
			}
			var payload []byte
			if s.reply != nil {
				payload = s.reply(cmd)
			}
			if payload == nil {
				continue
			}
			for _, part := range splitPayload(payload, s.chunk) {
				if writeTestPacket(conn, reqid, 0, part) != nil {
					return
				}
			}
		}
	}
}

func readTestPacket(conn net.Conn) (int32, int32, []byte, error) {
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	var sizeBuf [4]byte
	if _, err := io.ReadFull(conn, sizeBuf[:]); err != nil {
		return 0, 0, nil, err
	}
	size := int(int32(binary.LittleEndian.Uint32(sizeBuf[:])))
	if size < 10 || size > 1<<20 {
		return 0, 0, nil, errors.New("bad packet size")
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return 0, 0, nil, err
	}
	return int32(binary.LittleEndian.Uint32(buf[0:4])), int32(binary.LittleEndian.Uint32(buf[4:8])), trimAtNUL(buf[8:]), nil
}

func writeTestPacket(conn net.Conn, reqid, typ int32, body []byte) error {
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	out := make([]byte, 14+len(body))
	binary.LittleEndian.PutUint32(out[0:], uint32(10+len(body)))
	binary.LittleEndian.PutUint32(out[4:], uint32(reqid))
	binary.LittleEndian.PutUint32(out[8:], uint32(typ))
	copy(out[12:], body)
	_, err := conn.Write(out)
	return err
}

func trimAtNUL(b []byte) []byte {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		return b[:i]
	}
	return b
}

// splitPayload emulates Sandstorm's 4082-byte packetisation (and the empty
// packet an empty response would produce).
func splitPayload(payload []byte, chunk int) [][]byte {
	if len(payload) == 0 {
		return [][]byte{{}}
	}
	if chunk <= 0 || len(payload) <= chunk {
		return [][]byte{payload}
	}
	var out [][]byte
	for len(payload) > 0 {
		n := chunk
		if n > len(payload) {
			n = len(payload)
		}
		out = append(out, payload[:n])
		payload = payload[n:]
	}
	return out
}

func makePayload(n int) []byte {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = alphabet[i%len(alphabet)]
	}
	return b
}

func splitAddr(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split %q: %v", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("port %q: %v", portStr, err)
	}
	return host, port
}

func postRCON(t *testing.T, h http.HandlerFunc, path, body string) *httptest.ResponseRecorder {
	return postRCONEncoding(t, h, path, body, "")
}

func postRCONEncoding(t *testing.T, h http.HandlerFunc, path, body, acceptEncoding string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	w := httptest.NewRecorder()
	h(w, req)
	return w
}

func decodeResponse(t *testing.T, w *httptest.ResponseRecorder) RCONResponse {
	t.Helper()
	var resp RCONResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	return resp
}

func TestRconHandlerEndToEnd(t *testing.T) {
	addr := startFakeRCON(t, &fakeRCON{
		password: "pw",
		mirror:   true,
		reply:    func(cmd string) []byte { return []byte("OK:" + cmd) },
	})
	host, port := splitAddr(t, addr)

	body, _ := json.Marshal(RCONRequest{IP: host, Port: port, Passwd: "pw", Msg: "list", Auth: "key123"})
	w := postRCON(t, rconHandler("key123"), "/api/icu/daemon/rcon", string(body))
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	resp := decodeResponse(t, w)
	if resp.Status != 1 || resp.Msg != "OK:list" {
		t.Fatalf("resp=%+v", resp)
	}
}

// TestRconHandlerMultiPacket covers the >100 KB responses Sandstorm splits into
// ~4082-byte packets (listgamemodeproperties is 28-31 packets).
func TestRconHandlerMultiPacket(t *testing.T) {
	payload := makePayload(120_000)
	addr := startFakeRCON(t, &fakeRCON{
		password: "pw",
		chunk:    4082,
		mirror:   true,
		reply:    func(string) []byte { return payload },
	})
	host, port := splitAddr(t, addr)

	body, _ := json.Marshal(RCONRequest{IP: host, Port: port, Passwd: "pw", Msg: "listgamemodeproperties", Auth: "k"})
	w := postRCON(t, rconHandler("k"), "/api/icu/daemon/rcon", string(body))
	resp := decodeResponse(t, w)
	if resp.Status != 1 {
		t.Fatalf("status=%d msg=%q", resp.Status, resp.Msg)
	}
	if resp.Msg != string(payload) {
		t.Fatalf("payload mismatch: got %d bytes, want %d", len(resp.Msg), len(payload))
	}
}

// TestRconHandlerNoMarker verifies the quiet-gap fallback for servers that do
// not mirror the empty end marker (vanilla Minecraft).
func TestRconHandlerNoMarker(t *testing.T) {
	addr := startFakeRCON(t, &fakeRCON{
		password: "pw",
		reply:    func(cmd string) []byte { return []byte("OK:" + cmd) },
	})
	host, port := splitAddr(t, addr)

	body, _ := json.Marshal(RCONRequest{IP: host, Port: port, Passwd: "pw", Msg: "list", Auth: "k"})
	start := time.Now()
	w := postRCON(t, rconHandler("k"), "/api/icu/daemon/rcon", string(body))
	resp := decodeResponse(t, w)
	if resp.Status != 1 || resp.Msg != "OK:list" {
		t.Fatalf("resp=%+v", resp)
	}
	// One quiet gap (800ms default) is the expected fallback.
	if elapsed := time.Since(start); elapsed < 700*time.Millisecond {
		t.Fatalf("returned too early (%v), marker fallback did not wait", elapsed)
	}
}

// TestRconHandlerRawListPlayers ensures the daemon does not "fix" the broken
// listplayers output: Sandstorm sends it without line breaks, and the bytes
// pass through unchanged.
func TestRconHandlerRawListPlayers(t *testing.T) {
	raw := []byte("ID\t | Name0\t | Alice")
	addr := startFakeRCON(t, &fakeRCON{
		password: "pw",
		mirror:   true,
		reply:    func(string) []byte { return raw },
	})
	host, port := splitAddr(t, addr)

	body, _ := json.Marshal(RCONRequest{IP: host, Port: port, Passwd: "pw", Msg: "listplayers", Auth: "k"})
	w := postRCON(t, rconHandler("k"), "/api/icu/daemon/rcon", string(body))
	resp := decodeResponse(t, w)
	if resp.Status != 1 || resp.Msg != string(raw) {
		t.Fatalf("resp=%q", resp.Msg)
	}
}

func TestRconHandlerBadAuth(t *testing.T) {
	startFakeRCON(t, &fakeRCON{password: "pw", mirror: true})
	body, _ := json.Marshal(RCONRequest{IP: "127.0.0.1", Port: 1, Passwd: "pw", Msg: "list", Auth: "WRONG"})
	w := postRCON(t, rconHandler("key123"), "/api/icu/daemon/rcon", string(body))
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d", w.Code)
	}
	resp := decodeResponse(t, w)
	if resp.Status != 0 || resp.Msg != msgEN().InvalidAuthKey {
		t.Fatalf("resp=%+v", resp)
	}
}

func TestRconHandlerUnreachable(t *testing.T) {
	body, _ := json.Marshal(RCONRequest{IP: "127.0.0.1", Port: 1, Passwd: "x", Msg: "list", Auth: "k"})
	w := postRCON(t, rconHandler("k"), "/api/icu/daemon/rcon", string(body))
	resp := decodeResponse(t, w)
	if resp.Status != 0 || resp.Msg == "" {
		t.Fatalf("resp=%+v", resp)
	}
}

func TestRconHandlerValidation(t *testing.T) {
	h := rconHandler("k")
	cases := []struct {
		name string
		req  RCONRequest
		want string
	}{
		{"missing ip", RCONRequest{Port: 1, Passwd: "p", Msg: "list", Auth: "k"}, msgEN().InvalidTarget},
		{"bad port", RCONRequest{IP: "127.0.0.1", Port: 70000, Passwd: "p", Msg: "list", Auth: "k"}, msgEN().InvalidTarget},
		{"missing passwd", RCONRequest{IP: "127.0.0.1", Port: 1, Msg: "list", Auth: "k"}, msgEN().InvalidTarget},
		{"empty msg", RCONRequest{IP: "127.0.0.1", Port: 1, Passwd: "p", Msg: "  ", Auth: "k"}, msgEN().MsgRequired},
		{"long msg", RCONRequest{IP: "127.0.0.1", Port: 1, Passwd: "p", Msg: strings.Repeat("x", 1447), Auth: "k"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.req)
			w := postRCON(t, h, "/api/icu/daemon/rcon", string(body))
			resp := decodeResponse(t, w)
			if resp.Status != 0 || resp.Msg == "" {
				t.Fatalf("resp=%+v", resp)
			}
			if tc.want != "" && resp.Msg != tc.want {
				t.Fatalf("msg=%q want %q", resp.Msg, tc.want)
			}
			if strings.Contains(tc.name, "long") && !strings.Contains(resp.Msg, "1446") {
				t.Fatalf("long command message should name the limit: %q", resp.Msg)
			}
		})
	}
}

func TestRconHandlerMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/icu/daemon/rcon", nil)
	w := httptest.NewRecorder()
	rconHandler("k")(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code=%d", w.Code)
	}
}

func TestRconHandlerBadJSON(t *testing.T) {
	w := postRCON(t, rconHandler("k"), "/api/icu/daemon/rcon", "{not json")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d", w.Code)
	}
}

func TestRconBatchEndpoint(t *testing.T) {
	payload := makePayload(120_000)
	addr := startFakeRCON(t, &fakeRCON{
		password: "pw",
		chunk:    4082,
		mirror:   true,
		reply: func(cmd string) []byte {
			if strings.HasPrefix(cmd, "listgamemodeproperties") {
				return payload
			}
			return []byte("OK:" + cmd)
		},
	})
	host, port := splitAddr(t, addr)

	msgs := []string{"listplayers", "gamemodeproperty NumPlayers", "listgamemodeproperties ServerHostname"}
	body, _ := json.Marshal(RCONBatchRequest{IP: host, Port: port, Passwd: "pw", Msgs: msgs, Auth: "key123"})
	w := postRCON(t, rconBatchHandler("key123"), "/api/icu/daemon/rcon/batch", string(body))
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var resp RCONBatchResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != 1 || len(resp.Results) != len(msgs) {
		t.Fatalf("status=%d results=%d", resp.Status, len(resp.Results))
	}
	want := []string{
		"OK:listplayers",
		"OK:gamemodeproperty NumPlayers",
		string(payload),
	}
	for i := range want {
		if resp.Results[i].Status != 1 || resp.Results[i].Msg != want[i] {
			t.Fatalf("result[%d]: status=%d len=%d", i, resp.Results[i].Status, len(resp.Results[i].Msg))
		}
	}
}

// TestRconBatchKeepsGoingAfterDisconnect verifies positional failure reporting
// when the connection dies: executed commands keep their result, the rest are
// marked failed, and the HTTP request itself still succeeds.
func TestRconBatchKeepsGoingAfterDisconnect(t *testing.T) {
	addr := startFakeRCON(t, &fakeRCON{
		password:  "pw",
		mirror:    true,
		dropOnCmd: "listplayers",
		reply:     func(cmd string) []byte { return []byte("OK:" + cmd) },
	})
	host, port := splitAddr(t, addr)

	msgs := []string{"listplayers", "gamemodeproperty NumPlayers"}
	body, _ := json.Marshal(RCONBatchRequest{IP: host, Port: port, Passwd: "pw", Msgs: msgs, Auth: "k"})
	w := postRCON(t, rconBatchHandler("k"), "/api/icu/daemon/rcon/batch", string(body))
	var resp RCONBatchResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != 1 || len(resp.Results) != 2 {
		t.Fatalf("resp=%+v", resp)
	}
	if resp.Results[0].Status != 0 || resp.Results[1].Status != 0 {
		t.Fatalf("results=%+v", resp.Results)
	}
	if !strings.Contains(resp.Results[0].Msg, "RCON") {
		t.Fatalf("unexpected failure message: %q", resp.Results[0].Msg)
	}
}

func TestRconBatchConnectFailure(t *testing.T) {
	body, _ := json.Marshal(RCONBatchRequest{
		IP: "127.0.0.1", Port: 1, Passwd: "x", Auth: "k",
		Msgs: []string{"listplayers", "gamemodeproperty NumPlayers"},
	})
	start := time.Now()
	w := postRCON(t, rconBatchHandler("k"), "/api/icu/daemon/rcon/batch", string(body))
	elapsed := time.Since(start)

	var resp RCONBatchResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != 1 || len(resp.Results) != 2 {
		t.Fatalf("resp=%+v", resp)
	}
	for i, r := range resp.Results {
		if r.Status != 0 || r.Msg == "" {
			t.Fatalf("result[%d]=%+v", i, r)
		}
	}
	if elapsed > 9*time.Second {
		t.Fatalf("unreachable target took %v (must not hang)", elapsed)
	}
}

func TestRconBatchRejectsBadRequests(t *testing.T) {
	base := RCONBatchRequest{IP: "127.0.0.1", Port: 1, Passwd: "p", Auth: "k"}
	tooMany := base
	for i := 0; i <= maxBatchCommands; i++ {
		tooMany.Msgs = append(tooMany.Msgs, "listplayers")
	}
	cases := []struct {
		name string
		req  RCONBatchRequest
		want string
	}{
		{"bad auth", func() RCONBatchRequest { r := base; r.Auth = "nope"; r.Msgs = []string{"list"}; return r }(), msgEN().InvalidAuthKey},
		{"no msgs", func() RCONBatchRequest { r := base; return r }(), msgEN().BatchEmpty},
		{"too many", tooMany, ""},
		{"empty cmd", func() RCONBatchRequest { r := base; r.Msgs = []string{" "}; return r }(), msgEN().MsgRequired},
		{"long cmd", func() RCONBatchRequest { r := base; r.Msgs = []string{strings.Repeat("x", 1447)}; return r }(), ""},
	}
	h := rconBatchHandler("k")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.req)
			w := postRCON(t, h, "/api/icu/daemon/rcon/batch", string(body))
			var resp RCONBatchResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.Status != 0 || resp.Msg == "" || len(resp.Results) != 0 {
				t.Fatalf("resp=%+v", resp)
			}
			if tc.want != "" && resp.Msg != tc.want {
				t.Fatalf("msg=%q want %q", resp.Msg, tc.want)
			}
		})
	}
}

// TestRouting covers the contract the backend uses to detect batch support:
// unknown paths must 404 and the endpoints are POST-only.
func TestRouting(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/icu/daemon/rcon", rconHandler("k"))
	mux.HandleFunc("/api/icu/daemon/rcon/batch", rconBatchHandler("k"))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/icu/daemon/unknown", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown path: %d", resp.StatusCode)
	}

	resp2, err := http.Get(srv.URL + "/api/icu/daemon/rcon/batch")
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET batch: %d", resp2.StatusCode)
	}

	resp3, err := http.Post(srv.URL+"/api/icu/daemon/rcon/batch", "application/json", strings.NewReader("{"))
	if err != nil {
		t.Fatal(err)
	}
	resp3.Body.Close()
	if resp3.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad json: %d", resp3.StatusCode)
	}
}

func TestTargetAddr(t *testing.T) {
	if got := targetAddr("1.2.3.4", 27112); got != "1.2.3.4:27112" {
		t.Fatalf("got %q", got)
	}
	if got := targetAddr(" ::1 ", 27112); got != "[::1]:27112" {
		t.Fatalf("ipv6 got %q", got)
	}
}

func TestLoadConfigValidation(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	defer os.Chdir(old)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if loadConfig() != nil {
		t.Fatal("want nil for missing config")
	}

	os.WriteFile(configFile, []byte("port=25500\nauthkey=abc\n"), 0o600)
	cfg := loadConfig()
	if cfg == nil || cfg.Port != "25500" || cfg.AuthKey != "abc" {
		t.Fatalf("cfg=%+v", cfg)
	}

	os.WriteFile(configFile, []byte("port=25500\nauthkey=\n"), 0o600)
	if loadConfig() != nil {
		t.Fatal("empty authkey must be rejected")
	}

	os.WriteFile(configFile, []byte("port=99999\nauthkey=abc\n"), 0o600)
	if loadConfig() != nil {
		t.Fatal("invalid port must be rejected")
	}

	os.WriteFile(configFile, []byte("# comment\r\nport=1234\r\nauthkey=k\r\n"), 0o600)
	cfg = loadConfig()
	if cfg == nil || cfg.Port != "1234" || cfg.AuthKey != "k" {
		t.Fatalf("crlf cfg=%+v", cfg)
	}
}

func TestNewUUID(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := newUUID()
		if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[14] != '4' || id[18] != '-' || id[23] != '-' {
			t.Fatalf("bad uuid %q", id)
		}
		if seen[id] {
			t.Fatalf("duplicate uuid %q", id)
		}
		seen[id] = true
	}
}

// msgEN returns the English messages so tests do not depend on the host locale.
func msgEN() messages { return msgMap[langEN] }
