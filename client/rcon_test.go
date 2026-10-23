package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeRconServer is a configurable Source RCON server for the client's protocol
// tests. It mirrors empty end markers like Sandstorm and can split large
// replies the same way (4082-byte packets).
type fakeRconServer struct {
	password string
	reply    func(cmd string) []byte // nil means no reply
	chunk    int
	mirror   bool
}

func startFakeRcon(t *testing.T, s *fakeRconServer) (string, int) {
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
	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	return host, port
}

func (s *fakeRconServer) serve(conn net.Conn) {
	defer conn.Close()
	for {
		reqid, typ, body, err := readClientTestPacket(conn)
		if err != nil {
			return
		}
		switch typ {
		case rconAuth:
			rid := reqid
			if string(body) != s.password {
				rid = -1
			}
			_ = writeClientTestPacket(conn, rid, respAuthRt, nil)
		case respValue:
			if s.mirror {
				_ = writeClientTestPacket(conn, reqid, respValue, nil)
			}
		case rconExec:
			if s.reply == nil {
				continue
			}
			payload := s.reply(string(body))
			if payload == nil {
				continue
			}
			for _, part := range splitClientPayload(payload, s.chunk) {
				if err := writeClientTestPacket(conn, reqid, respValue, part); err != nil {
					return
				}
			}
		}
	}
}

func readClientTestPacket(conn net.Conn) (int32, int32, []byte, error) {
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	var sizeBuf [4]byte
	if _, err := io.ReadFull(conn, sizeBuf[:]); err != nil {
		return 0, 0, nil, err
	}
	size := int(int32(binary.LittleEndian.Uint32(sizeBuf[:])))
	if size < 10 || size > 1<<20 {
		return 0, 0, nil, errors.New("bad size")
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return 0, 0, nil, err
	}
	return int32(binary.LittleEndian.Uint32(buf[0:4])), int32(binary.LittleEndian.Uint32(buf[4:8])), clientTrimNUL(buf[8:]), nil
}

func writeClientTestPacket(conn net.Conn, reqid, typ int32, body []byte) error {
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	out := make([]byte, 14+len(body))
	binary.LittleEndian.PutUint32(out[0:], uint32(10+len(body)))
	binary.LittleEndian.PutUint32(out[4:], uint32(reqid))
	binary.LittleEndian.PutUint32(out[8:], uint32(typ))
	copy(out[12:], body)
	_, err := conn.Write(out)
	return err
}

func clientTrimNUL(b []byte) []byte {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		return b[:i]
	}
	return b
}

func splitClientPayload(payload []byte, chunk int) [][]byte {
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

func clientPayload(n int) []byte {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = alphabet[i%len(alphabet)]
	}
	return b
}

func TestRconExecMultiPacket(t *testing.T) {
	payload := clientPayload(120_000)
	host, port := startFakeRcon(t, &fakeRconServer{
		password: "pw",
		chunk:    4082,
		mirror:   true,
		reply:    func(string) []byte { return payload },
	})

	c, err := rconDial(context.Background(), host, port, "pw", 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	resp, err := c.exec("listgamemodeproperties", 5*time.Second)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if resp != string(payload) {
		t.Fatalf("got %d bytes, want %d", len(resp), len(payload))
	}
}

func TestRconExecQuietGap(t *testing.T) {
	host, port := startFakeRcon(t, &fakeRconServer{
		password: "pw",
		reply:    func(cmd string) []byte { return []byte("OK:" + cmd) },
	})

	c, err := rconDial(context.Background(), host, port, "pw", 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	start := time.Now()
	resp, err := c.exec("list", 3*time.Second)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if resp != "OK:list" {
		t.Fatalf("resp=%q", resp)
	}
	if elapsed := time.Since(start); elapsed < 700*time.Millisecond {
		t.Fatalf("quiet gap fallback did not wait (%v)", elapsed)
	}
}

func TestRconAuthFailed(t *testing.T) {
	host, port := startFakeRcon(t, &fakeRconServer{password: "pw", mirror: true})

	if _, err := rconDial(context.Background(), host, port, "wrong", 2*time.Second); !errors.Is(err, errRconAuth) {
		t.Fatalf("want errRconAuth, got %v", err)
	}
}

func TestRconExecDirectCancelled(t *testing.T) {
	host, port := startFakeRcon(t, &fakeRconServer{password: "pw", mirror: true})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	e := &ServerEntry{Mode: modeDirect, RconHost: host, RconPort: port, RconPass: "pw"}
	start := time.Now()
	if _, err := execDirect(ctx, e, "list"); err == nil {
		t.Fatal("want error from cancelled context")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("cancel took %v", elapsed)
	}
}

func daemonEntry(t *testing.T, srv *httptest.Server) *ServerEntry {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return &ServerEntry{
		Mode:       modeDaemon,
		DaemonHost: u.Hostname(),
		DaemonPort: port,
		AuthKey:    "k",
		RconHost:   "10.0.0.1",
		RconPort:   27112,
		RconPass:   "pw",
	}
}

func TestExecDaemonSuccessAndError(t *testing.T) {
	reply := "ID\t | Name"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/icu/daemon/rcon" {
			http.NotFound(w, r)
			return
		}
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		if req["msg"] == "boom" {
			json.NewEncoder(w).Encode(map[string]any{"status": 0, "msg": "命令执行失败"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"status": 1, "msg": reply})
	}))
	defer srv.Close()

	e := daemonEntry(t, srv)
	got, err := execDaemon(context.Background(), e, "listplayers")
	if err != nil || got != reply {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if _, err := execDaemon(context.Background(), e, "boom"); err == nil || err.Error() != "命令执行失败" {
		t.Fatalf("want daemon error, got %v", err)
	}
}

func TestExecDaemonHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()

	if _, err := execDaemon(context.Background(), daemonEntry(t, srv), "list"); err == nil {
		t.Fatal("want HTTP error")
	}
}

// TestExecDaemonLargeResponse makes sure the client does not truncate a big
// listgamemodeproperties-style reply at 1 MB (the old limit).
func TestExecDaemonLargeResponse(t *testing.T) {
	big := strings.Repeat("x", 2<<20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": 1, "msg": big})
	}))
	defer srv.Close()

	got, err := execDaemon(context.Background(), daemonEntry(t, srv), "listgamemodeproperties")
	if err != nil {
		t.Fatalf("execDaemon: %v", err)
	}
	if len(got) != len(big) {
		t.Fatalf("truncated response: got %d bytes, want %d", len(got), len(big))
	}
}
