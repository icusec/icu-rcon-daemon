package utils

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// testServer is a configurable Source RCON server used by the protocol tests.
type testServer struct {
	password   string
	reply      func(cmd string) []byte // nil means "send no reply"
	chunk      int                     // >0 splits each reply into packets of at most chunk bytes
	mirror     bool                    // mirror empty type-0 packets like Sandstorm
	strayAuth  bool                    // send a stray packet before the auth reply
	stallAuth  bool                    // never answer the auth request
	closeOnCmd string                  // close the connection when this command arrives
	truncOnCmd string                  // send only the first chunk, then close
	delayCmd   string                  // delay the reply of this command
	delay      time.Duration
	oversize   bool // announce an oversized packet for every command
	badSize    bool // announce a malformed size for every command
}

func startTestServer(t *testing.T, s *testServer) string {
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

func (s *testServer) serve(conn net.Conn) {
	defer conn.Close()
	for {
		reqid, typ, body, err := readTestPacket(conn)
		if err != nil {
			return
		}
		switch typ {
		case cmdAuth:
			if s.stallAuth {
				continue
			}
			if s.strayAuth {
				if writeTestPacket(conn, reqid, respResponse, nil) != nil {
					return
				}
			}
			rid := reqid
			if string(body) != s.password {
				rid = -1
			}
			if writeTestPacket(conn, rid, respAuthResponse, nil) != nil {
				return
			}
		case respResponse: // client's empty end marker
			if s.mirror {
				if writeTestPacket(conn, reqid, respResponse, nil) != nil {
					return
				}
			}
		case cmdExecCommand:
			cmd := string(body)
			if s.closeOnCmd == cmd {
				return
			}
			if s.oversize {
				writeRawSize(conn, maxPacketBytes+1)
				return
			}
			if s.badSize {
				writeRawSize(conn, 4)
				return
			}
			if s.delayCmd == cmd && s.delay > 0 {
				time.Sleep(s.delay)
			}
			var payload []byte
			if s.reply != nil {
				payload = s.reply(cmd)
			}
			if payload == nil {
				continue
			}
			parts := splitTestPayload(payload, s.chunk)
			if s.truncOnCmd == cmd {
				parts = parts[:1]
			}
			for _, part := range parts {
				if writeTestPacket(conn, reqid, respResponse, part) != nil {
					return
				}
			}
			if s.truncOnCmd == cmd {
				return
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
	return int32(binary.LittleEndian.Uint32(buf[0:4])), int32(binary.LittleEndian.Uint32(buf[4:8])), trimTestNUL(buf[8:]), nil
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

func writeRawSize(conn net.Conn, size int) {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], uint32(int32(size)))
	_, _ = conn.Write(buf[:])
}

func trimTestNUL(b []byte) []byte {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		return b[:i]
	}
	return b
}

func splitTestPayload(payload []byte, chunk int) [][]byte {
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

func testPayload(n int) []byte {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = alphabet[i%len(alphabet)]
	}
	return b
}

// fastConfig keeps the quiet-gap tests quick while exercising the same paths.
func fastConfig() Config {
	return Config{
		DialTimeout:        2 * time.Second,
		AuthTimeout:        2 * time.Second,
		FirstPacketTimeout: 250 * time.Millisecond,
		QuietGap:           100 * time.Millisecond,
	}
}

func dialTest(t *testing.T, s *testServer) *RemoteConsole {
	t.Helper()
	addr := startTestServer(t, s)
	c, err := DialWithConfig(addr, s.password, fastConfig())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestExecBasic(t *testing.T) {
	c := dialTest(t, &testServer{
		password: "secret",
		mirror:   true,
		reply:    func(cmd string) []byte { return []byte("OK:" + cmd) },
	})

	for _, cmd := range []string{"say hi", "list"} {
		resp, err := c.Exec(cmd, 2*time.Second)
		if err != nil {
			t.Fatalf("Exec(%q): %v", cmd, err)
		}
		if want := "OK:" + cmd; resp != want {
			t.Fatalf("Exec(%q)=%q want %q", cmd, resp, want)
		}
	}
}

func TestDialAuthFailed(t *testing.T) {
	s := &testServer{password: "secret", mirror: true}
	addr := startTestServer(t, s)

	if _, err := DialWithConfig(addr, "wrong", fastConfig()); !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("want ErrAuthFailed, got %v", err)
	}
}

func TestDialCoalescedAuth(t *testing.T) {
	c := dialTest(t, &testServer{
		password:  "secret",
		mirror:    true,
		strayAuth: true,
		reply:     func(cmd string) []byte { return []byte("OK:" + cmd) },
	})
	resp, err := c.Exec("ping", 2*time.Second)
	if err != nil || resp != "OK:ping" {
		t.Fatalf("resp=%q err=%v", resp, err)
	}
}

// TestAuthTimeout covers a server that accepts the TCP connection but never
// answers the auth request. The whole exchange is bounded, not each read.
func TestAuthTimeout(t *testing.T) {
	addr := startTestServer(t, &testServer{password: "secret", stallAuth: true})

	start := time.Now()
	_, err := DialWithConfig(addr, "secret", Config{
		DialTimeout: 2 * time.Second,
		AuthTimeout: 100 * time.Millisecond,
	})
	if !errors.Is(err, ErrAuthTimeout) {
		t.Fatalf("want ErrAuthTimeout, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("auth timeout took %v", elapsed)
	}
}

// TestAuthDeadline covers the batch endpoint's overall connect budget.
func TestAuthDeadline(t *testing.T) {
	addr := startTestServer(t, &testServer{password: "secret", stallAuth: true})

	start := time.Now()
	_, err := DialWithConfig(addr, "secret", Config{
		DialTimeout: 2 * time.Second,
		AuthTimeout: 5 * time.Second,
		Deadline:    time.Now().Add(100 * time.Millisecond),
	})
	if !errors.Is(err, ErrAuthTimeout) {
		t.Fatalf("want ErrAuthTimeout, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("deadline-capped auth took %v", elapsed)
	}
}

func TestDialDeadlinePassed(t *testing.T) {
	addr := startTestServer(t, &testServer{password: "secret"})

	if _, err := DialWithConfig(addr, "secret", Config{Deadline: time.Now().Add(-time.Second)}); !errors.Is(err, ErrDialTimeout) {
		t.Fatalf("want ErrDialTimeout, got %v", err)
	}
}

// TestMultiPacketResponse covers the >100 KB replies Sandstorm splits into
// 4082-byte packets; every packet must be concatenated.
func TestMultiPacketResponse(t *testing.T) {
	payload := testPayload(120_000)
	c := dialTest(t, &testServer{
		password: "secret",
		chunk:    4082,
		mirror:   true,
		reply:    func(string) []byte { return payload },
	})

	resp, err := c.Exec("listgamemodeproperties", 5*time.Second)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if resp != string(payload) {
		t.Fatalf("got %d bytes, want %d", len(resp), len(payload))
	}
}

// TestNoMarkerQuietGap covers servers that ignore the empty end marker.
func TestNoMarkerQuietGap(t *testing.T) {
	c := dialTest(t, &testServer{
		password: "secret",
		reply:    func(cmd string) []byte { return []byte("OK:" + cmd) },
	})

	start := time.Now()
	resp, err := c.Exec("list", 2*time.Second)
	if err != nil || resp != "OK:list" {
		t.Fatalf("resp=%q err=%v", resp, err)
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("quiet gap was not applied (%v)", elapsed)
	}
}

func TestSilentCommand(t *testing.T) {
	c := dialTest(t, &testServer{password: "secret", mirror: true})

	start := time.Now()
	resp, err := c.Exec("say hello", 2*time.Second)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if resp != "" {
		t.Fatalf("want empty response, got %q", resp)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("silent command took %v", elapsed)
	}
}

func TestCommandTooLong(t *testing.T) {
	c := dialTest(t, &testServer{
		password: "secret",
		mirror:   true,
		reply:    func(cmd string) []byte { return []byte("OK:" + cmd) },
	})

	if _, err := c.Exec(string(make([]byte, MaxCommandBytes+1)), 2*time.Second); !errors.Is(err, ErrCommandTooLong) {
		t.Fatalf("want ErrCommandTooLong, got %v", err)
	}

	// The limit itself must be accepted.
	cmd := strings.Repeat("x", MaxCommandBytes)
	resp, err := c.Exec(cmd, 2*time.Second)
	if err != nil {
		t.Fatalf("Exec at the size limit: %v", err)
	}
	if resp != "OK:"+cmd {
		t.Fatalf("unexpected response at the size limit (%d bytes)", len(resp))
	}
}

func TestOversizePacket(t *testing.T) {
	c := dialTest(t, &testServer{password: "secret", mirror: true, oversize: true})

	if _, err := c.Exec("huge", 2*time.Second); !errors.Is(err, ErrResponseTooLong) {
		t.Fatalf("want ErrResponseTooLong, got %v", err)
	}
	if !c.Broken() {
		t.Fatal("connection must be marked broken after a framing error")
	}
}

func TestBadPacketSize(t *testing.T) {
	c := dialTest(t, &testServer{password: "secret", mirror: true, badSize: true})

	if _, err := c.Exec("bad", 2*time.Second); !errors.Is(err, ErrUnexpectedFormat) {
		t.Fatalf("want ErrUnexpectedFormat, got %v", err)
	}
}

func TestConnectionClosedBeforeResponse(t *testing.T) {
	c := dialTest(t, &testServer{password: "secret", mirror: true, closeOnCmd: "bye"})

	if _, err := c.Exec("bye", 2*time.Second); !errors.Is(err, ErrConnectionClosed) {
		t.Fatalf("want ErrConnectionClosed, got %v", err)
	}
	if !c.Broken() {
		t.Fatal("connection must be marked broken")
	}
	if _, err := c.Exec("list", 2*time.Second); !errors.Is(err, ErrConnectionClosed) {
		t.Fatalf("second Exec: want ErrConnectionClosed, got %v", err)
	}
}

// TestConnectionClosedMidResponse keeps the data already received (the command
// itself succeeded) but marks the connection unusable for later commands.
func TestConnectionClosedMidResponse(t *testing.T) {
	c := dialTest(t, &testServer{
		password:   "secret",
		mirror:     true,
		truncOnCmd: "partial",
		chunk:      10,
		reply:      func(string) []byte { return testPayload(100) },
	})

	resp, err := c.Exec("partial", 2*time.Second)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if want := string(testPayload(100)[:10]); resp != want {
		t.Fatalf("got %q want %q", resp, want)
	}
	if !c.Broken() {
		t.Fatal("connection must be marked broken after EOF")
	}
}

// TestDeadlineThenReuse forces the overall deadline, then verifies the next
// command drains the late reply instead of mixing it into its own response.
func TestDeadlineThenReuse(t *testing.T) {
	c := dialTest(t, &testServer{
		password: "secret",
		mirror:   true,
		delayCmd: "slow",
		delay:    150 * time.Millisecond,
		reply:    func(cmd string) []byte { return []byte("OK:" + cmd) },
	})

	if _, err := c.Exec("slow", 50*time.Millisecond); !errors.Is(err, ErrResponseTimeout) {
		t.Fatalf("want ErrResponseTimeout, got %v", err)
	}

	resp, err := c.Exec("fast", 2*time.Second)
	if err != nil {
		t.Fatalf("Exec(fast): %v", err)
	}
	if resp != "OK:fast" {
		t.Fatalf("got %q, stale packets leaked into the next response", resp)
	}
}

func TestDialRefused(t *testing.T) {
	if _, err := DialWithConfig("127.0.0.1:1", "x", fastConfig()); err == nil {
		t.Fatal("want dial error, got nil")
	}
}

// TestFailedAuthClosesConn verifies that a failed handshake does not leak the
// socket: the server must see EOF shortly after rejecting the auth.
func TestFailedAuthClosesConn(t *testing.T) {
	seenEOF := make(chan struct{})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		buf := make([]byte, 4096)
		n, _ := conn.Read(buf) // auth packet
		if n == 0 {
			return
		}
		// Reply with a failed auth (id = -1).
		_ = writeTestPacket(conn, -1, respAuthResponse, nil)
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		for {
			_, err := conn.Read(buf)
			if err != nil {
				close(seenEOF)
				return
			}
		}
	}()

	if _, err := DialWithConfig(ln.Addr().String(), "wrong", fastConfig()); !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("want ErrAuthFailed, got %v", err)
	}
	select {
	case <-seenEOF:
		// good: server observed the closed connection
	case <-time.After(5 * time.Second):
		t.Fatal("connection was not closed after failed auth (leak)")
	}
}
