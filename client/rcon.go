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
// Direct RCON client (Source RCON protocol), adapted from the daemon's
// utils/rconutil.go. One short-lived connection per command keeps the client
// stateless and simple.

const (
	rconAuth   = 3
	rconExec   = 2
	respValue  = 0
	respAuthRt = 2
)

var (
	errRconAuth   = errors.New("RCON 认证失败 / RCON authentication failed")
	errRconFormat = errors.New("RCON 返回数据格式不正确 / unexpected response format")
)

type rconConn struct {
	conn    net.Conn
	readbuf []byte
	reqid   int32
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
	c := &rconConn{conn: conn, reqid: 0x7fffffff, readbuf: make([]byte, 40960)}
	reqid, err := c.writeCmd(rconAuth, password, timeout)
	if err != nil {
		return nil, err
	}
	typ, rid, _, err := c.readResponse(timeout)
	if err == nil && typ != respAuthRt {
		typ, rid, _, err = c.readResponse(timeout)
	}
	if err != nil {
		return nil, err
	}
	if typ != respAuthRt {
		return nil, errRconFormat
	}
	if rid != reqid {
		return nil, errRconAuth
	}
	ok = true
	return c, nil
}

func (c *rconConn) Close() error {
	return c.conn.Close()
}

func (c *rconConn) writeCmd(cmdType int32, str string, timeout time.Duration) (int, error) {
	if len(str) > 1014 {
		return -1, errors.New("命令太长 / command too long")
	}
	c.reqid++
	if c.reqid&0x0fffffff != c.reqid {
		c.reqid = int32((time.Now().UnixNano() / 100000) % 100000)
	}
	packet := make([]byte, 14+len(str))
	binary.LittleEndian.PutUint32(packet[0:], uint32(10+len(str)))
	binary.LittleEndian.PutUint32(packet[4:], uint32(c.reqid))
	binary.LittleEndian.PutUint32(packet[8:], uint32(cmdType))
	copy(packet[12:], str)
	c.conn.SetWriteDeadline(time.Now().Add(timeout))
	if _, err := c.conn.Write(packet); err != nil {
		return -1, err
	}
	return int(c.reqid), nil
}

func (c *rconConn) readResponse(timeout time.Duration) (int, int, []byte, error) {
	c.conn.SetReadDeadline(time.Now().Add(timeout))
	var sizeBuf [4]byte
	if _, err := io.ReadFull(c.conn, sizeBuf[:]); err != nil {
		return 0, 0, nil, err
	}
	dataSize := int(int32(binary.LittleEndian.Uint32(sizeBuf[:])))
	if dataSize < 10 || dataSize > len(c.readbuf) {
		return 0, 0, nil, errRconFormat
	}
	body := c.readbuf[:dataSize]
	if _, err := io.ReadFull(c.conn, body); err != nil {
		return 0, 0, nil, err
	}
	requestId := int(int32(binary.LittleEndian.Uint32(body[0:4])))
	respType := int(int32(binary.LittleEndian.Uint32(body[4:8])))
	payload := body[8:]
	if i := bytes.IndexByte(payload, 0); i >= 0 {
		payload = payload[:i]
	}
	return respType, requestId, payload, nil
}

// ---------------------------------------------------------------------------
// Execution backends. Both return the plain response text or an error.

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
	// The daemon may legitimately spend up to two minutes reading the RCON
	// response of long commands; stay above that budget.
	client := &http.Client{Timeout: 140 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	var out struct {
		Status int    `json:"status"`
		Msg    string `json:"msg"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 200))
		}
		return "", fmt.Errorf("无效的响应 / invalid response: %s", truncate(string(raw), 200))
	}
	if resp.StatusCode != http.StatusOK && out.Status != 0 && out.Msg == "" {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if out.Status != 1 {
		return "", errors.New(out.Msg)
	}
	return out.Msg, nil
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
	reqid, err := conn.writeCmd(rconExec, cmd, 10*time.Second)
	if err != nil {
		return "", err
	}
	typ, rid, payload, err := conn.readResponse(2 * time.Minute)
	if err != nil {
		return "", err
	}
	if typ != respValue {
		return "", errRconFormat
	}
	if rid != reqid {
		return "", errRconFormat
	}
	return string(payload), nil
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
	At   time.Time
	MS   int64 // round-trip milliseconds (0 for system lines)
}

type rconTask struct {
	ServerID string
	Cmd      string
	IsTest   bool
}

type rconService struct {
	w        *app.Window
	mu       sync.Mutex
	inflight map[string]bool              // per server, only one request at a time
	cancels  map[string]context.CancelFunc // cancel handle per in-flight request
	logs     map[string][]logLine
	next     int64
}

func newRconService(w *app.Window) *rconService {
	return &rconService{
		w:        w,
		inflight: map[string]bool{},
		cancels:  map[string]context.CancelFunc{},
		logs:     map[string][]logLine{},
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

// logsFor copies the log slice of a server for rendering.
func (s *rconService) logsFor(id string) []logLine {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.logs[id]
}

func (s *rconService) clearLogs(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.logs, id)
	s.invalidate()
}

func (s *rconService) append(id string, line logLine) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logs[id] = append(s.logs[id], line)
	if n := len(s.logs[id]); n > logLimit {
		s.logs[id] = s.logs[id][n-logLimit:]
	}
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
	if e.Mode == modeDirect {
		return "测试连接 → 直连 " + e.RconHost + ":" + fmt.Sprint(e.RconPort)
	}
	return "测试连接 → Daemon " + e.DaemonHost + ":" + fmt.Sprint(e.DaemonPort)
}

func testOKDesc(ms int64) string {
	return fmt.Sprintf("测试成功 (%dms)，连接可用", ms)
}
