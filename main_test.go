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
	"strings"
	"testing"
	"time"
)

// fakeRCON spins up a minimal Source RCON server for handler tests.
func fakeRCON(t *testing.T, password string) string {
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
			go serveRCON(conn, password)
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return ln.Addr().String()
}

func serveRCON(conn net.Conn, password string) {
	defer conn.Close()
	readPacket := func() ([]byte, error) {
		var sizeBuf [4]byte
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		if _, err := io.ReadFull(conn, sizeBuf[:]); err != nil {
			return nil, err
		}
		size := int(int32(binary.LittleEndian.Uint32(sizeBuf[:])))
		if size < 10 || size > 1<<20 {
			return nil, errors.New("bad size")
		}
		buf := make([]byte, size)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return nil, err
		}
		return buf, nil
	}
	for {
		packet, err := readPacket()
		if err != nil {
			return
		}
		reqid := int32(binary.LittleEndian.Uint32(packet[0:4]))
		typ := int32(binary.LittleEndian.Uint32(packet[4:8]))
		body := packet[8:]
		if i := bytes.IndexByte(body, 0); i >= 0 {
			body = body[:i]
		}
		switch typ {
		case 3: // auth
			rid := reqid
			if string(body) != password {
				rid = -1
			}
			conn.Write(buildTestPacket(rid, 2, ""))
		case 2: // exec
			conn.Write(buildTestPacket(reqid, 0, "OK:"+string(body)))
		}
	}
}

func buildTestPacket(reqid, typ int32, body string) []byte {
	out := make([]byte, 14+len(body))
	binary.LittleEndian.PutUint32(out[0:], uint32(10+len(body)))
	binary.LittleEndian.PutUint32(out[4:], uint32(reqid))
	binary.LittleEndian.PutUint32(out[8:], uint32(typ))
	copy(out[12:], body)
	return out
}

func post(t *testing.T, h http.HandlerFunc, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/icu/daemon/rcon", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h(w, req)
	return w
}

func TestRconHandlerEndToEnd(t *testing.T) {
	addr := fakeRCON(t, "pw")
	parts := strings.Split(addr, ":")
	h := rconHandler("key123")

	body, _ := json.Marshal(RCONRequest{IP: parts[0], Port: atoi(parts[1]), Passwd: "pw", Msg: "list", Auth: "key123"})
	w := post(t, h, string(body))
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var resp RCONResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != 1 || resp.Msg != "OK:list" {
		t.Fatalf("resp=%+v", resp)
	}
}

func TestRconHandlerBadAuth(t *testing.T) {
	fakeRCON(t, "pw")
	h := rconHandler("key123")
	body, _ := json.Marshal(RCONRequest{IP: "127.0.0.1", Port: 1, Passwd: "pw", Msg: "list", Auth: "WRONG"})
	w := post(t, h, string(body))
	var resp RCONResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Status != 0 || resp.Msg != msgMap[currentLang].InvalidAuthKey {
		t.Fatalf("resp=%+v", resp)
	}
}

func TestRconHandlerUnreachable(t *testing.T) {
	h := rconHandler("k")
	body, _ := json.Marshal(RCONRequest{IP: "127.0.0.1", Port: 1, Passwd: "x", Msg: "list", Auth: "k"})
	w := post(t, h, string(body))
	var resp RCONResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Status != 0 || resp.Msg == "" {
		t.Fatalf("resp=%+v", resp)
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
	w := post(t, rconHandler("k"), "{not json")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d", w.Code)
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

func atoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}
