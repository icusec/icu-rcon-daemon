package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"gioui.org/app"
)

// ---------------------------------------------------------------------------
// Direct RCON client (Source RCON protocol).
//
// This is the same algorithm as the daemon's utils/rconutil.go (the client is
// a separate Go module, so the code is mirrored rather than imported):
//
//   - a response may be split into many packets and must be concatenated;
//   - the empty RESPONSE_VALUE end marker is sent only after the first response
//     packet, because Sandstorm sometimes mirrors it before the output exists;
//   - if the server does not mirror the marker, a quiet gap ends the response;
//   - Sandstorm does not send SRCDS' extra 8-byte terminator, so the mirror is
//     the only explicit end signal;
//   - after a deadline, queued packets are drained before the next command.

const (
	rconAuth   = 3
	rconExec   = 2
	respValue  = 0
	respAuthRt = 2
)

const (
	rconMaxCommandBytes  = 1446
	rconMaxPacketBytes   = 64 << 10
	rconMaxResponseBytes = 4 << 20
	rconMaxAuthPackets   = 8

	rconFirstPacket    = 2 * time.Second
	rconQuietGap       = 800 * time.Millisecond
	rconCommandTimeout = 2 * time.Minute
	rconWriteTimeout   = 10 * time.Second
)

var (
	errRconAuth    = errors.New("RCON 认证失败 / RCON authentication failed")
	errRconFormat  = errors.New("RCON 返回数据格式不正确 / unexpected response format")
	errRconTooLong = errors.New("命令太长 / command too long")
	errRconTimeout = errors.New("RCON 响应超时 / RCON response timed out")
	errRconClosed  = errors.New("RCON 连接已断开 / RCON connection closed")
)

type rconConn struct {
	conn    net.Conn
	readbuf []byte
	reqid   int32

	// needsFlush is set when the previous response read hit its overall
	// deadline, so unread packets may still be queued.
	needsFlush bool
	// dead means a fatal socket/framing error was seen; the connection must
	// not be reused.
	dead bool
}

func rconDial(ctx context.Context, host string, port int, password string, timeout time.Duration) (*rconConn, error) {
	d := &net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			conn.Close()
		}
	}()

	c := &rconConn{
		conn:    conn,
		reqid:   int32(time.Now().UnixNano()%1_000_000) + 1000,
		readbuf: make([]byte, rconMaxPacketBytes),
	}
	if err := c.authenticate(password, timeout); err != nil {
		return nil, err
	}
	ok = true
	return c, nil
}

// authenticate performs the login handshake. Stray packets before the auth
// reply are skipped; id -1 means the password was rejected. The timeout bounds
// the whole exchange, not a single read.
func (c *rconConn) authenticate(password string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	authID := c.nextID()
	if err := c.writeCmd(authID, rconAuth, password, rconTimeLeft(deadline)); err != nil {
		return err
	}
	for i := 0; i < rconMaxAuthPackets; i++ {
		wait := time.Until(deadline)
		if wait <= 0 {
			return errRconTimeout
		}
		requestID, respType, _, err := c.readPacket(wait)
		if err != nil {
			if isRconTimeout(err) {
				return errRconTimeout
			}
			return err
		}
		if respType != respAuthRt {
			continue
		}
		if requestID == -1 {
			return errRconAuth
		}
		return nil
	}
	return errRconFormat
}

func (c *rconConn) Close() error {
	return c.conn.Close()
}

// exec runs one command and returns the concatenation of every response
// packet. timeout bounds the whole exchange.
func (c *rconConn) exec(cmd string, timeout time.Duration) (string, error) {
	if len(cmd) > rconMaxCommandBytes {
		return "", errRconTooLong
	}
	if timeout <= 0 {
		timeout = rconCommandTimeout
	}
	if c.dead {
		return "", errRconClosed
	}
	if c.needsFlush {
		c.drain()
		if c.dead {
			return "", errRconClosed
		}
	}

	deadline := time.Now().Add(timeout)
	commandID := c.nextID()
	markerID := c.nextID()
	if err := c.writeCmd(commandID, rconExec, cmd, rconTimeLeft(deadline)); err != nil {
		c.dead = true
		return "", rconWrapClosed(err)
	}
	return c.collect(commandID, markerID, deadline)
}

// collect reads packets until the server mirrors the empty end marker after at
// least one data packet, or until the stream goes quiet.
func (c *rconConn) collect(commandID, markerID int32, deadline time.Time) (string, error) {
	var out []byte
	sawCommandPacket := false

	for {
		now := time.Now()
		if !now.Before(deadline) {
			c.needsFlush = true
			return string(out), errRconTimeout
		}

		wait := rconFirstPacket
		if sawCommandPacket {
			wait = rconQuietGap
		}
		if left := deadline.Sub(now); left < wait {
			wait = left
		}

		requestID, respType, body, err := c.readPacket(wait)
		if err != nil {
			if isRconTimeout(err) {
				if time.Now().Before(deadline) {
					// Quiet gap (or a server that ignores the marker).
					c.needsFlush = false
					return string(out), nil
				}
				c.needsFlush = true
				return string(out), errRconTimeout
			}
			// The peer closed or reset the connection after the response
			// started: keep what arrived and mark the connection unusable.
			if sawCommandPacket && !errors.Is(err, errRconFormat) {
				c.dead = true
				c.needsFlush = false
				return string(out), nil
			}
			c.dead = true
			return string(out), rconWrapClosed(err)
		}

		if requestID == markerID && respType == respValue && len(body) == 0 {
			if sawCommandPacket {
				c.needsFlush = false
				return string(out), nil
			}
			// Mirror raced ahead of the output; ignore it and keep reading.
			continue
		}
		if requestID != commandID {
			continue
		}

		if !sawCommandPacket {
			sawCommandPacket = true
			// Ask for the end marker only now; sending it with the command
			// makes Sandstorm mirror it before the output exists.
			_ = c.writeCmd(markerID, respValue, "", rconTimeLeft(deadline))
		}

		if len(out)+len(body) > rconMaxResponseBytes {
			c.needsFlush = true
			return string(out), errors.New("RCON 响应过大 / RCON response too large")
		}
		out = append(out, body...)
	}
}

// drain discards queued packets before the next command.
func (c *rconConn) drain() {
	defer func() { c.needsFlush = false }()
	deadline := time.Now().Add(rconFirstPacket)
	for time.Now().Before(deadline) {
		if _, _, _, err := c.readPacket(rconQuietGap); err != nil {
			if !isRconTimeout(err) {
				c.dead = true
			}
			return
		}
	}
}

func (c *rconConn) nextID() int32 {
	c.reqid++
	if c.reqid > 0x0fffffff || c.reqid <= 0 {
		c.reqid = 1000
	}
	return c.reqid
}

func (c *rconConn) writeCmd(id, cmdType int32, body string, timeout time.Duration) error {
	if len(body) > rconMaxCommandBytes {
		return errRconTooLong
	}
	packet := make([]byte, 14+len(body))
	binary.LittleEndian.PutUint32(packet[0:], uint32(10+len(body)))
	binary.LittleEndian.PutUint32(packet[4:], uint32(id))
	binary.LittleEndian.PutUint32(packet[8:], uint32(cmdType))
	copy(packet[12:], body)

	if timeout <= 0 {
		timeout = rconWriteTimeout
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(timeout))
	if _, err := c.conn.Write(packet); err != nil {
		return err
	}
	return nil
}

func (c *rconConn) readPacket(timeout time.Duration) (int32, int32, []byte, error) {
	if timeout <= 0 {
		timeout = time.Millisecond
	}
	_ = c.conn.SetReadDeadline(time.Now().Add(timeout))

	var sizeBuf [4]byte
	if _, err := io.ReadFull(c.conn, sizeBuf[:]); err != nil {
		return 0, 0, nil, err
	}
	dataSize := int(int32(binary.LittleEndian.Uint32(sizeBuf[:])))
	if dataSize < 10 {
		return 0, 0, nil, errRconFormat
	}
	if dataSize > rconMaxPacketBytes {
		return 0, 0, nil, errRconFormat
	}
	body := c.readbuf[:dataSize]
	if _, err := io.ReadFull(c.conn, body); err != nil {
		return 0, 0, nil, err
	}
	requestID := int32(binary.LittleEndian.Uint32(body[0:4]))
	respType := int32(binary.LittleEndian.Uint32(body[4:8]))
	payload := body[8:]
	if i := bytes.IndexByte(payload, 0); i >= 0 {
		payload = payload[:i]
	}
	return requestID, respType, payload, nil
}

func isRconTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func rconWrapClosed(err error) error {
	if errors.Is(err, errRconClosed) {
		return err
	}
	return fmt.Errorf("%w: %v", errRconClosed, err)
}

func rconTimeLeft(deadline time.Time) time.Duration {
	left := time.Until(deadline)
	if left < time.Millisecond {
		return time.Millisecond
	}
	return left
}

// ---------------------------------------------------------------------------
// Execution backends. Both return the plain response text or an error.

// maxDaemonResponseBytes bounds how much of a daemon HTTP response is read. A
// command response can be 4 MB; JSON escaping can inflate that, so leave room.
const maxDaemonResponseBytes = 16 << 20

func execDaemon(ctx context.Context, e *ServerEntry, cmd string) (string, error) {
	if e.DaemonHost == "" {
		return "", errors.New("Daemon 地址为空")
	}
	payload := map[string]any{
		"ip":     e.RconHost,
		"port":   e.RconPort,
		"passwd": e.RconPass,
		"msg":    cmd,
		"auth":   e.AuthKey,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	url := "http://" + net.JoinHostPort(e.DaemonHost, strconv.Itoa(e.DaemonPort)) + "/api/icu/daemon/rcon"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	// Ask for the best coding the daemon supports; it is free to answer with
	// identity when compression would not help.
	req.Header.Set("Accept-Encoding", acceptEncoding)
	// The daemon may legitimately spend up to two minutes reading the RCON
	// response of long commands; stay above that budget.
	client := &http.Client{Timeout: 140 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	reader, closeReader, err := decompressResponse(resp)
	if err != nil {
		return "", err
	}
	defer closeReader()

	raw, err := io.ReadAll(io.LimitReader(reader, maxDaemonResponseBytes))
	if err != nil {
		return "", err
	}
	var out struct {
		Status int    `json:"status"`
		Msg    string `json:"msg"`
	}
	if err := json.Unmarshal(raw, &out); err == nil {
		if out.Status == 1 {
			return out.Msg, nil
		}
		if out.Msg != "" {
			return "", errors.New(out.Msg)
		}
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	return "", fmt.Errorf("无效的响应 / invalid response: %s", truncate(string(raw), 200))
}

func execDirect(ctx context.Context, e *ServerEntry, cmd string) (string, error) {
	conn, err := rconDial(ctx, e.RconHost, e.RconPort, e.RconPass, 8*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	// A cancel (or timeout) closes the socket, which unblocks any read.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-done:
		}
	}()

	return conn.exec(cmd, rconCommandTimeout)
}

// sanitizeOut makes server text safe for the bundled font: tabs would render
// as missing glyphs, CR would overwrite line starts.
func sanitizeOut(s string) string {
	s = strings.ReplaceAll(s, "\t", "    ")
	s = strings.ReplaceAll(s, "\r", "")
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ---------------------------------------------------------------------------
// Async service. Executions run on goroutines; finished results are appended
// to the matching server's in-memory log and the window is invalidated so the
// UI paints them on its next frame.

type logKind int

const (
	logCmd logKind = iota
	logResp
	logErr
	logSys
)

type logLine struct {
	Kind logKind
	Text string
	// RowCount is len(splitDisplayRows(Text)); the row strings themselves live
	// in the flattened view so large replies are not stored twice.
	RowCount int
	At       time.Time
	MS       int64 // round-trip milliseconds (0 for system lines)
}

// logViewRow is one rendered line of a log entry. First marks the row that
// carries the timestamp and command/error prefix.
type logViewRow struct {
	Kind  logKind
	Text  string
	At    time.Time
	MS    int64
	First bool
}

// maxRowRunes bounds one display row. Splitting by rune keeps UTF-8 intact;
// 1000 runes is small enough to shape cheaply yet large enough to make a copied
// row useful.
const maxRowRunes = 1000

// maxLogBytes caps the text retained per server. A handful of 4 MB RCON
// replies would otherwise keep hundreds of megabytes for the lifetime of the
// process. It is a var so tests can lower the budget.
var maxLogBytes = 16 << 20

// splitDisplayRows breaks text into units that are cheap to shape: first on
// newlines (the common case for RCON tables), then by rune count for servers
// such as Sandstorm that send a whole response without line breaks.
func splitDisplayRows(text string) []string {
	if text == "" {
		return []string{""}
	}
	lines := strings.Split(text, "\n")
	// A trailing newline should not create an extra empty row.
	if len(lines) > 1 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	rows := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			rows = append(rows, "")
			continue
		}
		runes := []rune(line)
		for len(runes) > maxRowRunes {
			rows = append(rows, string(runes[:maxRowRunes]))
			runes = runes[maxRowRunes:]
		}
		rows = append(rows, string(runes))
	}
	if len(rows) == 0 {
		rows = []string{""}
	}
	return rows
}

type rconTask struct {
	ServerID string
	Cmd      string
	IsTest   bool
}

type rconService struct {
	w        *app.Window
	mu       sync.Mutex
	inflight map[string]bool               // per server, only one request at a time
	cancels  map[string]context.CancelFunc // cancel handle per in-flight request
	logs     map[string][]logLine
	views    map[string][]logViewRow // flattened rows derived from logs
	next     int64
}

func newRconService(w *app.Window) *rconService {
	return &rconService{
		w:        w,
		inflight: map[string]bool{},
		cancels:  map[string]context.CancelFunc{},
		logs:     map[string][]logLine{},
		views:    map[string][]logViewRow{},
	}
}

func (s *rconService) busy(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inflight[id]
}

// invalidate nudges the window to re-render (no-op in selftest mode).
func (s *rconService) invalidate() {
	if s.w != nil {
		s.w.Invalidate()
	}
}

// logsFor returns the raw log entries of a server (for Copy All and status).
func (s *rconService) logsFor(id string) []logLine {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.logs[id]
}

// rowsFor returns the flattened display rows of a server. The slice is
// immutable until the next append or clear; the UI only iterates it.
func (s *rconService) rowsFor(id string) []logViewRow {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.views[id]
}

func (s *rconService) clearLogs(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.logs, id)
	delete(s.views, id)
	s.invalidate()
}

func (s *rconService) append(id string, line logLine) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := splitDisplayRows(line.Text)
	line.RowCount = len(rows)
	s.logs[id] = append(s.logs[id], line)
	for i, part := range rows {
		s.views[id] = append(s.views[id], logViewRow{
			Kind: line.Kind, Text: part, At: line.At, MS: line.MS, First: i == 0,
		})
	}
	s.trimLogsLocked(id)
}

// trimLogsLocked enforces both the entry count and the text byte budget,
// dropping the oldest entries and their rendered rows.
func (s *rconService) trimLogsLocked(id string) {
	entries := s.logs[id]
	if len(entries) == 0 {
		return
	}
	total := 0
	for _, e := range entries {
		total += len(e.Text)
	}
	drop := 0
	for len(entries)-drop > 1 && (len(entries)-drop > logLimit || total > maxLogBytes) {
		total -= len(entries[drop].Text)
		drop++
	}
	if drop == 0 {
		return
	}
	removedRows := 0
	for i := 0; i < drop; i++ {
		removedRows += entries[i].RowCount
	}
	s.logs[id] = entries[drop:]
	s.views[id] = s.views[id][removedRows:]
}

func (s *rconService) sysLog(id, text string) {
	s.append(id, logLine{Kind: logSys, Text: text, At: time.Now()})
	s.invalidate()
}

// cancel aborts the in-flight request of a server, if any. The aborted
// goroutine logs the cancellation itself.
func (s *rconService) cancel(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.cancels[id]; ok {
		c()
		return true
	}
	return false
}

// send executes cmd against the server asynchronously.
func (s *rconService) send(e *ServerEntry, cmd string, isTest bool) {
	s.mu.Lock()
	if s.inflight[e.ID] {
		s.mu.Unlock()
		return
	}
	s.inflight[e.ID] = true
	ctx, cancel := context.WithCancel(context.Background())
	s.cancels[e.ID] = cancel
	s.mu.Unlock()

	if isTest && cmd == "" {
		// "help" exists on virtually every RCON implementation and is
		// side-effect free — a better probe than an empty packet.
		cmd = "help"
	}
	s.append(e.ID, logLine{Kind: logCmd, Text: sanitizeOut(cmd), At: time.Now()})
	if isTest {
		s.append(e.ID, logLine{Kind: logSys, Text: "→ " + testDesc(e), At: time.Now()})
	}
	s.invalidate()

	task := *e
	go func() {
		start := time.Now()
		var text string
		var err error
		if task.Mode == modeDirect {
			text, err = execDirect(ctx, &task, cmd)
		} else {
			text, err = execDaemon(ctx, &task, cmd)
		}
		ms := time.Since(start).Milliseconds()

		s.mu.Lock()
		delete(s.inflight, task.ID)
		delete(s.cancels, task.ID)
		s.mu.Unlock()

		switch {
		case ctx.Err() == context.Canceled:
			s.append(task.ID, logLine{Kind: logSys, Text: "· " + stringsFor(currentLang()).Cancelled, At: time.Now(), MS: ms})
		case err != nil:
			s.append(task.ID, logLine{Kind: logErr, Text: sanitizeOut(err.Error()), At: time.Now(), MS: ms})
		case isTest && strings.TrimSpace(text) == "":
			// The test command is "help"; every working RCON server answers
			// it. Silence means the TCP port is open but RCON does not answer,
			// which is not a successful connection test.
			s.append(task.ID, logLine{Kind: logErr, Text: stringsFor(currentLang()).NoResponse, At: time.Now(), MS: ms})
		case isTest:
			s.append(task.ID, logLine{Kind: logSys, Text: "√ " + testOKDesc(ms), At: time.Now(), MS: ms})
			s.append(task.ID, logLine{Kind: logResp, Text: sanitizeOut(text), At: time.Now(), MS: ms})
		default:
			s.append(task.ID, logLine{Kind: logResp, Text: sanitizeOut(text), At: time.Now(), MS: ms})
		}
		s.invalidate()
	}()
}

func testDesc(e *ServerEntry) string {
	L := stringsFor(currentLang())
	if e.Mode == modeDirect {
		return sprintf(L.TestViaDirect, e.RconHost+":"+fmt.Sprint(e.RconPort))
	}
	return sprintf(L.TestViaDaemon, e.DaemonHost+":"+fmt.Sprint(e.DaemonPort))
}

func testOKDesc(ms int64) string {
	return sprintf(stringsFor(currentLang()).TestSuccess, ms)
}
