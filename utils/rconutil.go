package utils

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// RCON packet types (Source RCON protocol).
const (
	cmdAuth        = 3
	cmdExecCommand = 2

	respResponse     = 0
	respAuthResponse = 2
)

// Protocol limits.
const (
	// MaxCommandBytes is the maximum command body accepted by the protocol.
	// Longer commands are rejected before anything is written to the socket.
	MaxCommandBytes = 1446

	// maxPacketBytes bounds one RCON packet (the size field counts the 8-byte
	// id/type header, the body and the two trailing NULs).
	maxPacketBytes = 64 << 10

	// maxResponseBytes bounds the concatenated response of one command.
	maxResponseBytes = 4 << 20

	// maxAuthPackets bounds the stray packets tolerated before the auth reply.
	maxAuthPackets = 8
)

// Default network budgets. These match the behaviour that survived real
// Sandstorm servers: a response can arrive as many packets, and the end marker
// may race with the output, so silence is the final fallback.
const (
	defaultDialTimeout  = 10 * time.Second
	defaultAuthTimeout  = 10 * time.Second
	defaultFirstPacket  = 2 * time.Second
	defaultQuietGap     = 800 * time.Millisecond
	defaultWriteTimeout = 10 * time.Second

	// DefaultCommandTimeout is the overall budget for one command. Long
	// running commands (backups, world saves) can legitimately take minutes.
	DefaultCommandTimeout = 2 * time.Minute
)

// Config tunes the network behaviour of a RemoteConsole. The zero value is
// valid and selects the defaults.
type Config struct {
	// DialTimeout bounds the TCP handshake.
	DialTimeout time.Duration
	// AuthTimeout bounds the whole authentication exchange.
	AuthTimeout time.Duration
	// FirstPacketTimeout is how long to wait for the first packet of a
	// response before giving up on it (the server may be busy).
	FirstPacketTimeout time.Duration
	// QuietGap is the silence after at least one response packet that means
	// "this response is complete" when the server does not mirror the end
	// marker.
	QuietGap time.Duration
	// Deadline, when non-zero, caps dialing and authentication together. The
	// batch endpoint uses it to keep connect + auth inside the request budget.
	Deadline time.Time
}

func (c Config) withDefaults() Config {
	if c.DialTimeout <= 0 {
		c.DialTimeout = defaultDialTimeout
	}
	if c.AuthTimeout <= 0 {
		c.AuthTimeout = defaultAuthTimeout
	}
	if c.FirstPacketTimeout <= 0 {
		c.FirstPacketTimeout = defaultFirstPacket
	}
	if c.QuietGap <= 0 {
		c.QuietGap = defaultQuietGap
	}
	return c
}

// Err... values are stable so callers can map them to user-facing messages.
var (
	ErrAuthFailed          = errors.New("认证失败")
	ErrAuthTimeout         = errors.New("认证超时")
	ErrDialTimeout         = errors.New("连接 RCON 服务器超时")
	ErrInvalidAuthResponse = errors.New("认证时返回了不可用的数据")
	ErrUnexpectedFormat    = errors.New("返回数据格式不正确")
	ErrCommandTooLong      = errors.New("命令太长")
	ErrResponseTooLong     = errors.New("返回信息太长")
	ErrResponseTimeout     = errors.New("读取 RCON 响应超时")
	ErrConnectionClosed    = errors.New("RCON 连接已断开")
)

// RemoteConsole is one Source RCON connection. Exec serialises command
// exchanges internally, so concurrent calls are safe; the daemon still uses
// one connection per HTTP request (one for a whole batch).
type RemoteConsole struct {
	conn    net.Conn
	cfg     Config
	readbuf []byte

	// readmu serialises packet reads so a deadline race can never interleave
	// two reads on the same socket.
	readmu sync.Mutex
	// cmdmu serialises complete command exchanges (flush, write, collect).
	cmdmu sync.Mutex

	seq int32

	// needsFlush is set when the previous read loop ended on its overall
	// deadline (as opposed to a quiet gap), meaning unread packets may still
	// be queued on the connection.
	needsFlush bool
	// dead is set on any fatal socket or framing error; the connection must
	// not be used again.
	dead bool
}

// Dial connects to the RCON server at host ("host:port") and authenticates
// with password using the default Config.
func Dial(host, password string) (*RemoteConsole, error) {
	return DialWithConfig(host, password, Config{})
}

// DialWithConfig is Dial with explicit timeouts. On any handshake failure the
// socket is closed, so a failed authentication never leaks a connection.
func DialWithConfig(host, password string, cfg Config) (*RemoteConsole, error) {
	cfg = cfg.withDefaults()

	dialTimeout := cfg.DialTimeout
	if !cfg.Deadline.IsZero() {
		if left := time.Until(cfg.Deadline); left < dialTimeout {
			dialTimeout = left
		}
	}
	if dialTimeout <= 0 {
		return nil, ErrDialTimeout
	}

	dialer := net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.Dial("tcp", host)
	if err != nil {
		return nil, err
	}

	ok := false
	defer func() {
		if !ok {
			conn.Close()
		}
	}()

	r := &RemoteConsole{
		conn:    conn,
		cfg:     cfg,
		readbuf: make([]byte, maxPacketBytes),
		// Start somewhere random so a reused connection or a restarted daemon
		// does not collide with a stale response still in flight.
		seq: int32(time.Now().UnixNano()%1_000_000) + 1000,
	}

	authDeadline := time.Now().Add(cfg.AuthTimeout)
	if !cfg.Deadline.IsZero() && cfg.Deadline.Before(authDeadline) {
		authDeadline = cfg.Deadline
	}
	if err := r.authenticate(password, authDeadline); err != nil {
		return nil, err
	}

	ok = true
	return r, nil
}

// Exec sends one command and returns the concatenation of every packet its
// response was split into. timeout is the overall budget for the command; a
// value <= 0 selects DefaultCommandTimeout.
//
// The empty RESPONSE_VALUE end marker is sent only after the first response
// packet has arrived. Sandstorm sometimes mirrors a marker sent earlier before
// the command output exists, which makes the mirror useless as an end signal;
// deferring it keeps the mirror last. When a server does not mirror at all,
// the quiet gap (Config.QuietGap) ends the response instead.
func (r *RemoteConsole) Exec(cmd string, timeout time.Duration) (string, error) {
	if len(cmd) > MaxCommandBytes {
		return "", ErrCommandTooLong
	}
	if timeout <= 0 {
		timeout = DefaultCommandTimeout
	}

	r.cmdmu.Lock()
	defer r.cmdmu.Unlock()

	if r.dead {
		return "", ErrConnectionClosed
	}
	// A previous command that hit its overall deadline may have left packets
	// queued. Drain them before sending anything new, or they would be
	// mistaken for this command's output.
	if r.needsFlush {
		r.drain()
		if r.dead {
			return "", ErrConnectionClosed
		}
	}

	deadline := time.Now().Add(timeout)
	commandID := r.nextID()
	markerID := r.nextID()

	if err := r.writeCmd(commandID, cmdExecCommand, cmd, timeLeft(deadline)); err != nil {
		return "", r.fail(err)
	}
	return r.collect(commandID, markerID, deadline)
}

// Close closes the underlying socket.
func (r *RemoteConsole) Close() error {
	return r.conn.Close()
}

// Broken reports whether a previous operation left the connection unusable.
func (r *RemoteConsole) Broken() bool {
	r.cmdmu.Lock()
	defer r.cmdmu.Unlock()
	return r.dead
}

// LocalAddr returns the local endpoint of the connection.
func (r *RemoteConsole) LocalAddr() net.Addr {
	return r.conn.LocalAddr()
}

// RemoteAddr returns the remote endpoint of the connection.
func (r *RemoteConsole) RemoteAddr() net.Addr {
	return r.conn.RemoteAddr()
}

// authenticate performs the login handshake. Some implementations emit a
// stray packet before the auth reply, so a few non-auth packets are skipped;
// an auth reply with id -1 means the password was rejected. deadline bounds the
// whole exchange, so a server that keeps emitting stray packets cannot stall it.
func (r *RemoteConsole) authenticate(password string, deadline time.Time) error {
	authID := r.nextID()
	if err := r.writeCmd(authID, cmdAuth, password, timeLeft(deadline)); err != nil {
		return err
	}
	for i := 0; i < maxAuthPackets; i++ {
		wait := time.Until(deadline)
		if wait <= 0 {
			return ErrAuthTimeout
		}
		requestID, respType, _, err := r.readPacket(wait)
		if err != nil {
			if isTimeout(err) {
				return ErrAuthTimeout
			}
			return err
		}
		if respType != respAuthResponse {
			continue
		}
		if requestID == -1 {
			return ErrAuthFailed
		}
		return nil
	}
	return ErrInvalidAuthResponse
}

// collect reads packets until the response ends: either the server mirrors the
// empty end marker after at least one data packet, or the stream goes quiet.
func (r *RemoteConsole) collect(commandID, markerID int32, deadline time.Time) (string, error) {
	var out []byte
	sawCommandPacket := false

	for {
		now := time.Now()
		if !now.Before(deadline) {
			r.needsFlush = true
			return string(out), ErrResponseTimeout
		}

		wait := r.cfg.FirstPacketTimeout
		if sawCommandPacket {
			wait = r.cfg.QuietGap
		}
		if left := deadline.Sub(now); left < wait {
			wait = left
		}

		requestID, respType, body, err := r.readPacket(wait)
		if err != nil {
			if isTimeout(err) {
				if time.Now().Before(deadline) {
					// Genuine silence after the response started (or a server
					// that does not mirror the marker): the response is done.
					r.needsFlush = false
					return string(out), nil
				}
				r.needsFlush = true
				return string(out), ErrResponseTimeout
			}
			// A connection that closed or was reset after the response started
			// still leaves the command itself successful: keep what was
			// received and mark the connection unusable for later commands.
			// (Windows reports a peer close as ECONNRESET, not EOF.)
			if sawCommandPacket &&
				!errors.Is(err, ErrUnexpectedFormat) &&
				!errors.Is(err, ErrResponseTooLong) {
				r.dead = true
				r.needsFlush = false
				return string(out), nil
			}
			return string(out), r.fail(err)
		}

		if requestID == markerID && respType == respResponse && len(body) == 0 {
			if sawCommandPacket {
				// The mirrored marker arrived after output: end of response.
				r.needsFlush = false
				return string(out), nil
			}
			// The mirror raced ahead of the output (this happens when the
			// marker is sent together with the command). Ignore it and keep
			// reading until data arrives or the stream goes quiet.
			continue
		}
		if requestID != commandID {
			// Stale packet from a previous exchange.
			continue
		}

		if !sawCommandPacket {
			sawCommandPacket = true
			// Ask the server to mirror an empty packet back once it has
			// written everything. Sent now, so the mirror queues behind the
			// output instead of racing it. Servers that ignore the marker
			// simply fall back to the quiet gap below, so a failed write is
			// not fatal for the data already collected.
			_ = r.writeCmd(markerID, respResponse, "", timeLeft(deadline))
		}

		if len(out)+len(body) > maxResponseBytes {
			r.needsFlush = true
			return string(out), ErrResponseTooLong
		}
		out = append(out, body...)
	}
}

// drain reads and discards queued packets until the connection is quiet. It is
// used only after a command hit its overall deadline and may have left unread
// packets behind.
func (r *RemoteConsole) drain() {
	defer func() { r.needsFlush = false }()

	deadline := time.Now().Add(r.cfg.FirstPacketTimeout)
	for time.Now().Before(deadline) {
		if _, _, _, err := r.readPacket(r.cfg.QuietGap); err != nil {
			if !isTimeout(err) {
				r.dead = true
			}
			return
		}
	}
}

func (r *RemoteConsole) nextID() int32 {
	r.seq++
	if r.seq > 0x0fffffff || r.seq <= 0 {
		r.seq = 1000
	}
	return r.seq
}

// writeCmd assembles and sends one RCON packet. The 14-byte header and body
// are written into a single pre-sized buffer: one allocation, one socket write.
func (r *RemoteConsole) writeCmd(id, cmdType int32, body string, timeout time.Duration) error {
	if len(body) > MaxCommandBytes {
		return ErrCommandTooLong
	}

	packet := make([]byte, 14+len(body))
	binary.LittleEndian.PutUint32(packet[0:], uint32(10+len(body)))
	binary.LittleEndian.PutUint32(packet[4:], uint32(id))
	binary.LittleEndian.PutUint32(packet[8:], uint32(cmdType))
	copy(packet[12:], body)
	// The two trailing NUL terminators are already zero-valued.

	if timeout <= 0 {
		timeout = defaultWriteTimeout
	}
	_ = r.conn.SetWriteDeadline(time.Now().Add(timeout))
	if _, err := r.conn.Write(packet); err != nil {
		return err
	}
	return nil
}

// readPacket reads exactly one packet: a 4-byte little-endian size followed by
// the payload. The returned body aliases the internal read buffer; callers must
// copy it before the next read. A timeout leaves the connection usable.
func (r *RemoteConsole) readPacket(timeout time.Duration) (int32, int32, []byte, error) {
	r.readmu.Lock()
	defer r.readmu.Unlock()

	if timeout <= 0 {
		timeout = time.Millisecond
	}
	_ = r.conn.SetReadDeadline(time.Now().Add(timeout))

	var sizeBuf [4]byte
	if _, err := io.ReadFull(r.conn, sizeBuf[:]); err != nil {
		return 0, 0, nil, err
	}
	dataSize := int(int32(binary.LittleEndian.Uint32(sizeBuf[:])))
	if dataSize < 10 {
		return 0, 0, nil, ErrUnexpectedFormat
	}
	if dataSize > maxPacketBytes {
		return 0, 0, nil, ErrResponseTooLong
	}

	body := r.readbuf[:dataSize]
	if _, err := io.ReadFull(r.conn, body); err != nil {
		return 0, 0, nil, err
	}
	return decodePacket(body)
}

// decodePacket parses id/type/body out of a packet payload. The trailing NUL
// terminators (and anything after the first embedded NUL) are stripped.
func decodePacket(data []byte) (int32, int32, []byte, error) {
	if len(data) < 8 {
		return 0, 0, nil, ErrUnexpectedFormat
	}
	requestID := int32(binary.LittleEndian.Uint32(data[0:4]))
	respType := int32(binary.LittleEndian.Uint32(data[4:8]))
	payload := data[8:]
	if i := bytes.IndexByte(payload, 0); i >= 0 {
		payload = payload[:i]
	}
	return requestID, respType, payload, nil
}

// fail records a fatal error and wraps it for the caller. Framing errors are
// returned as-is; every other read/write error means the socket is gone.
func (r *RemoteConsole) fail(err error) error {
	if errors.Is(err, ErrCommandTooLong) {
		return err
	}
	r.dead = true
	if errors.Is(err, ErrUnexpectedFormat) || errors.Is(err, ErrResponseTooLong) || errors.Is(err, ErrConnectionClosed) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrConnectionClosed, err)
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// timeLeft returns the remaining budget, never zero or negative so callers can
// pass it straight to a deadline.
func timeLeft(deadline time.Time) time.Duration {
	left := time.Until(deadline)
	if left < time.Millisecond {
		return time.Millisecond
	}
	return left
}
