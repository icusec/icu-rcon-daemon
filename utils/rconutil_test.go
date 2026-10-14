package utils

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// fakeRCONServer is a minimal Source RCON server for tests. It answers auth
// packets with an auth response (echoing the request id on success, -1 on
// failure) and replies to exec packets with "OK:<body>".
type fakeRCONServer struct {
	ln       net.Listener
	password string
	// coalesce makes the server prepend a stray RESPONSE_VALUE packet to every
	// reply and send both in a single write, exercising the "stray packet
	// before the expected reply" behaviour of some RCON servers.
	coalesce bool
	// bigReply makes the server reply with a payload of ~40KB, exceeding one
	// TCP segment.
	bigReply bool
	// oversize makes the server announce a packet larger than readBufferSize.
	oversize bool
}

func startFakeRCON(t *testing.T, s *fakeRCONServer) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s.ln = ln
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

func (s *fakeRCONServer) serve(conn net.Conn) {
	defer conn.Close()
	for {
		packet, err := readPacket(conn)
		if err != nil {
			return
		}
		reqid := int32(binary.LittleEndian.Uint32(packet[0:4]))
		typ := int32(binary.LittleEndian.Uint32(packet[4:8]))
		body := packet[8:]
		if i := indexByte0(body); i >= 0 {
			body = body[:i]
		}
		switch typ {
		case cmdAuth:
			ok := string(body) == s.password
			rid := reqid
			if !ok {
				rid = -1
			}
			resp := buildPacket(rid, respAuthResponse, "")
			if ok && s.coalesce {
				resp = append(buildPacket(reqid, respResponse, "stray"), resp...)
			}
			if _, err := conn.Write(resp); err != nil {
				return
			}
		case cmdExecCommand:
			body2 := []byte("OK:" + string(body))
			if s.bigReply {
				body2 = make([]byte, 40000)
				for i := range body2 {
					body2[i] = byte('a' + i%26)
				}
			}
			if s.oversize {
				body2 = make([]byte, readBufferSize+100)
			}
			resp := buildPacket(reqid, respResponse, string(body2))
			if _, err := conn.Write(resp); err != nil {
				return
			}
		}
	}
}

// pending holds an unsent auth reply (coalesce mode).

func readPacket(conn net.Conn) ([]byte, error) {
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var sizeBuf [4]byte
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

func buildPacket(reqid, typ int32, body string) []byte {
	out := make([]byte, 14+len(body))
	binary.LittleEndian.PutUint32(out[0:], uint32(10+len(body)))
	binary.LittleEndian.PutUint32(out[4:], uint32(reqid))
	binary.LittleEndian.PutUint32(out[8:], uint32(typ))
	copy(out[12:], body)
	return out
}

func indexByte0(b []byte) int {
	for i, c := range b {
		if c == 0 {
			return i
		}
	}
	return -1
}

func TestDialAndExec(t *testing.T) {
	s := &fakeRCONServer{password: "secret"}
	addr := startFakeRCON(t, s)

	c, err := Dial(addr, "secret")
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	id, err := c.Write("say hi")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	resp, rid, err := c.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if rid != id || resp != "OK:say hi" {
		t.Fatalf("got resp=%q rid=%d want rid=%d", resp, rid, id)
	}

	// Second command on the same connection must work too.
	id2, _ := c.Write("list")
	resp2, rid2, err := c.Read()
	if err != nil || rid2 != id2 || resp2 != "OK:list" {
		t.Fatalf("second command: resp=%q err=%v", resp2, err)
	}
}

func TestDialAuthFailed(t *testing.T) {
	s := &fakeRCONServer{password: "secret"}
	addr := startFakeRCON(t, s)

	if _, err := Dial(addr, "wrong"); !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("want ErrAuthFailed, got %v", err)
	}
}

func TestDialCoalescedPackets(t *testing.T) {
	s := &fakeRCONServer{password: "secret", coalesce: true}
	addr := startFakeRCON(t, s)

	c, err := Dial(addr, "secret")
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	c.Write("ping")
	resp, _, err := c.Read()
	if err != nil || resp != "OK:ping" {
		t.Fatalf("coalesced read: resp=%q err=%v", resp, err)
	}
}

func TestLargeResponse(t *testing.T) {
	s := &fakeRCONServer{password: "secret", bigReply: true}
	addr := startFakeRCON(t, s)

	c, err := Dial(addr, "secret")
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	c.Write("big")
	resp, _, err := c.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(resp) != 40000 {
		t.Fatalf("want 40000 bytes, got %d", len(resp))
	}
}

func TestOversizeResponse(t *testing.T) {
	s := &fakeRCONServer{password: "secret", oversize: true}
	addr := startFakeRCON(t, s)

	c, err := Dial(addr, "secret")
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	c.Write("huge")
	if _, _, err := c.Read(); !errors.Is(err, ErrResponseTooLong) {
		t.Fatalf("want ErrResponseTooLong, got %v", err)
	}
}

func TestCommandTooLong(t *testing.T) {
	s := &fakeRCONServer{password: "secret"}
	addr := startFakeRCON(t, s)

	c, err := Dial(addr, "secret")
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	if _, err := c.Write(string(make([]byte, 2000))); !errors.Is(err, ErrCommandTooLong) {
		t.Fatalf("want ErrCommandTooLong, got %v", err)
	}
}

func TestDialRefused(t *testing.T) {
	// Nothing listens on this port (kernel assigns ephemeral ports from
	// above 1024; use a reserved port on loopback).
	if _, err := Dial("127.0.0.1:1", "x"); err == nil {
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
		resp := buildPacket(-1, respAuthResponse, "")
		conn.Write(resp)
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		for {
			_, err := conn.Read(buf)
			if err != nil {
				close(seenEOF)
				return
			}
		}
	}()

	if _, err := Dial(ln.Addr().String(), "wrong"); !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("want ErrAuthFailed, got %v", err)
	}
	select {
	case <-seenEOF:
		// good: server observed the closed connection
	case <-time.After(5 * time.Second):
		t.Fatal("connection was not closed after failed auth (leak)")
	}
}
