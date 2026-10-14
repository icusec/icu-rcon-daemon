package utils

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// RCON packet types (Source RCON protocol).
const (
	cmdAuth        = 3
	cmdExecCommand = 2

	respResponse     = 0
	respAuthResponse = 2
)

// readBufferSize is the maximum size of a single RCON packet payload plus its
// 4-byte length header. Vanilla Minecraft caps responses at 4096 bytes; the
// generous headroom keeps unusual servers working while still bounding reads.
const readBufferSize = 99999

// maxCommandLength limits the command body. Some servers drop or corrupt
// longer commands, so the historic 1024-10 limit is preserved.
const maxCommandLength = 1024 - 10

// Network budgets. The command read timeout is intentionally generous: long
// running commands (backups, world saves) can legitimately take minutes.
const (
	dialTimeout  = 10 * time.Second
	writeTimeout = 10 * time.Second
	authTimeout  = 10 * time.Second
	readTimeout  = 2 * time.Minute
)

type RemoteConsole struct {
	conn    net.Conn
	readbuf []byte
	readmu  sync.Mutex
	reqid   int32
}

var (
	ErrAuthFailed          = errors.New("认证失败")
	ErrInvalidAuthResponse = errors.New("认证时返回了不可用的数据")
	ErrUnexpectedFormat    = errors.New("返回数据格式不正确")
	ErrCommandTooLong      = errors.New("命令太长")
	ErrResponseTooLong     = errors.New("返回信息太长")
)

// Dial connects to the RCON server at host ("host:port") and authenticates
// with password. On any handshake failure the underlying socket is closed, so
// a failed authentication never leaks a connection.
func Dial(host, password string) (*RemoteConsole, error) {
	conn, err := net.DialTimeout("tcp", host, dialTimeout)
	if err != nil {
		return nil, err
	}

	ok := false
	defer func() {
		if !ok {
			conn.Close()
		}
	}()

	r := &RemoteConsole{conn: conn, reqid: 0x7fffffff, readbuf: make([]byte, readBufferSize)}

	reqid, err := r.writeCmd(cmdAuth, password)
	if err != nil {
		return nil, err
	}

	respType, requestId, _, err := r.readResponse(authTimeout)
	// Some RCON implementations emit a stray packet before the auth response;
	// read once more when the first packet is not the auth reply.
	if err == nil && respType != respAuthResponse {
		respType, requestId, _, err = r.readResponse(authTimeout)
	}
	if err != nil {
		return nil, err
	}
	if respType != respAuthResponse {
		return nil, ErrInvalidAuthResponse
	}
	if requestId != reqid {
		return nil, ErrAuthFailed
	}

	ok = true
	return r, nil
}

func (r *RemoteConsole) LocalAddr() net.Addr {
	return r.conn.LocalAddr()
}

func (r *RemoteConsole) RemoteAddr() net.Addr {
	return r.conn.RemoteAddr()
}

// Write sends an exec command and returns its request id.
func (r *RemoteConsole) Write(cmd string) (requestId int, err error) {
	return r.writeCmd(cmdExecCommand, cmd)
}

// Read waits for the response of the previously sent command. The response is
// always a freshly allocated string, safe to retain after further reads.
func (r *RemoteConsole) Read() (response string, requestId int, err error) {
	var respType int
	respType, requestId, respBytes, err := r.readResponse(readTimeout)
	if err != nil || respType != respResponse {
		return "", 0, err
	}
	return string(respBytes), requestId, nil
}

func (r *RemoteConsole) Close() error {
	return r.conn.Close()
}

func newRequestId(id int32) int32 {
	if id&0x0fffffff != id {
		return int32((time.Now().UnixNano() / 100000) % 100000)
	}
	return id + 1
}

// writeCmd assembles and sends one RCON packet. The 14-byte header and body
// are written into a single pre-sized buffer: no intermediate bytes.Buffer,
// no binary.Write reflection, one allocation, one socket write.
func (r *RemoteConsole) writeCmd(cmdType int32, str string) (int, error) {
	if len(str) > maxCommandLength {
		return -1, ErrCommandTooLong
	}

	reqid := newRequestId(atomic.LoadInt32(&r.reqid))
	atomic.StoreInt32(&r.reqid, reqid)

	packet := make([]byte, 14+len(str))
	binary.LittleEndian.PutUint32(packet[0:], uint32(10+len(str)))
	binary.LittleEndian.PutUint32(packet[4:], uint32(reqid))
	binary.LittleEndian.PutUint32(packet[8:], uint32(cmdType))
	copy(packet[12:], str)
	// The two trailing NUL terminators are already zero-valued.

	r.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	if _, err := r.conn.Write(packet); err != nil {
		return -1, err
	}
	return int(reqid), nil
}

// readResponse reads exactly one packet: 4-byte little-endian size, then the
// payload. Exact io.ReadFull reads mean two packets sent back-to-back are
// never coalesced incorrectly and no leftover bytes need buffering.
func (r *RemoteConsole) readResponse(timeout time.Duration) (int, int, []byte, error) {
	r.readmu.Lock()
	defer r.readmu.Unlock()

	r.conn.SetReadDeadline(time.Now().Add(timeout))

	var sizeBuf [4]byte
	if _, err := io.ReadFull(r.conn, sizeBuf[:]); err != nil {
		return 0, 0, nil, err
	}
	dataSize := int(int32(binary.LittleEndian.Uint32(sizeBuf[:])))
	if dataSize < 10 {
		return 0, 0, nil, ErrUnexpectedFormat
	}
	if dataSize > readBufferSize-4 {
		return 0, 0, nil, ErrResponseTooLong
	}

	body := r.readbuf[:dataSize]
	if _, err := io.ReadFull(r.conn, body); err != nil {
		return 0, 0, nil, err
	}
	return readResponseData(body)
}

// readResponseData parses id/type/body out of a packet payload. The returned
// body aliases data (and therefore the internal read buffer); callers must
// copy before the next read. RemoteConsole.Read does this by returning a
// string immediately.
func readResponseData(data []byte) (int, int, []byte, error) {
	if len(data) < 8 {
		return 0, 0, nil, ErrUnexpectedFormat
	}
	requestId := int(int32(binary.LittleEndian.Uint32(data[0:4])))
	respType := int(int32(binary.LittleEndian.Uint32(data[4:8])))
	payload := data[8:]
	if i := bytes.IndexByte(payload, 0); i >= 0 {
		payload = payload[:i]
	}
	return respType, requestId, payload, nil
}
