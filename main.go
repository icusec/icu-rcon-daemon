package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"icu-rcon-daemon/utils"
)

// RCONRequest is the JSON body accepted by POST /api/icu/daemon/rcon.
type RCONRequest struct {
	IP     string `json:"ip"`
	Port   int    `json:"port"`
	Passwd string `json:"passwd"`
	Msg    string `json:"msg"`
	Auth   string `json:"auth"`
}

// RCONResponse is the JSON body returned by the single-command endpoint.
type RCONResponse struct {
	Status int    `json:"status"`
	Msg    string `json:"msg"`
}

// RCONBatchRequest is the JSON body accepted by POST /api/icu/daemon/rcon/batch.
type RCONBatchRequest struct {
	IP     string   `json:"ip"`
	Port   int      `json:"port"`
	Passwd string   `json:"passwd"`
	Msgs   []string `json:"msgs"`
	Auth   string   `json:"auth"`
}

// RCONBatchResult is one entry of a batch response; results are positional and
// always the same length as the request's msgs.
type RCONBatchResult struct {
	Status int    `json:"status"`
	Msg    string `json:"msg"`
}

// RCONBatchResponse is the JSON body returned by the batch endpoint. Msg is set
// only when the whole request was rejected (bad auth or parameters).
type RCONBatchResponse struct {
	Status  int               `json:"status"`
	Msg     string            `json:"msg,omitempty"`
	Results []RCONBatchResult `json:"results,omitempty"`
}

// maxBodyBytes caps the request body. A valid batch is at most 8 commands of
// 1446 bytes, so 64 KB leaves plenty of headroom.
const maxBodyBytes = 64 << 10

const (
	// maxBatchCommands bounds one batch request.
	maxBatchCommands = 8
	// batchTimeout is the overall budget for a batch request (connect + auth +
	// every command). The new backend relies on this to avoid hanging HTTP
	// calls; unfinished commands are reported as failures.
	batchTimeout = 8 * time.Second
	// singleCommandTimeout is the per-command budget of the legacy endpoint.
	// Long-running commands (backups, world saves) can take minutes.
	singleCommandTimeout = utils.DefaultCommandTimeout
)

// validateTarget returns a user-facing message when the RCON target is
// incomplete, or "" when it is usable.
func validateTarget(ip string, port int, passwd string) string {
	if strings.TrimSpace(ip) == "" || port < 1 || port > 65535 || passwd == "" {
		return t().InvalidTarget
	}
	return ""
}

// targetAddr builds "host:port", bracketing IPv6 literals correctly.
func targetAddr(ip string, port int) string {
	return net.JoinHostPort(strings.TrimSpace(ip), strconv.Itoa(port))
}

// decodeJSONBody decodes at most maxBodyBytes of JSON into dst.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	return json.NewDecoder(r.Body).Decode(dst)
}

func rconHandler(authKey string) http.HandlerFunc {
	m := t()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, m.MethodNotAllowed, http.StatusMethodNotAllowed)
			return
		}

		var req RCONRequest
		if err := decodeJSONBody(w, r, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.Auth != authKey {
			log.Printf("denied request from %s: %s", r.RemoteAddr, m.InvalidAuthKey)
			writeJSON(w, r, RCONResponse{Status: 0, Msg: m.InvalidAuthKey})
			return
		}
		if msg := validateTarget(req.IP, req.Port, req.Passwd); msg != "" {
			writeJSON(w, r, RCONResponse{Status: 0, Msg: msg})
			return
		}
		if strings.TrimSpace(req.Msg) == "" {
			writeJSON(w, r, RCONResponse{Status: 0, Msg: m.MsgRequired})
			return
		}
		if len(req.Msg) > utils.MaxCommandBytes {
			writeJSON(w, r, RCONResponse{Status: 0, Msg: fmt.Sprintf(m.CommandTooLong, utils.MaxCommandBytes)})
			return
		}

		start := time.Now()
		conn, err := utils.Dial(targetAddr(req.IP, req.Port), req.Passwd)
		if err != nil {
			writeJSON(w, r, RCONResponse{Status: 0, Msg: fmt.Sprintf(m.FailedConnectRCON, err)})
			return
		}
		defer conn.Close()

		response, err := conn.Exec(req.Msg, singleCommandTimeout)
		if err != nil {
			failMsg := fmt.Sprintf(m.FailedExecRCON, err)
			if errors.Is(err, utils.ErrConnectionClosed) ||
				errors.Is(err, utils.ErrResponseTimeout) ||
				errors.Is(err, utils.ErrResponseTooLong) {
				failMsg = fmt.Sprintf(m.FailedReadRCON, err)
			}
			log.Printf("%s:%d <- %q failed: %v", strings.TrimSpace(req.IP), req.Port, req.Msg, err)
			writeJSON(w, r, RCONResponse{Status: 0, Msg: failMsg})
			return
		}

		log.Printf("%s:%d <- %q (%dms, %d bytes)", strings.TrimSpace(req.IP), req.Port, req.Msg,
			time.Since(start).Milliseconds(), len(response))
		writeJSON(w, r, RCONResponse{Status: 1, Msg: response})
	}
}

// rconBatchHandler runs several read-only commands over a single RCON
// connection. The response always mirrors the request: results[i] belongs to
// msgs[i]. A failing command yields status 0 for that entry and does not abort
// the remaining commands.
func rconBatchHandler(authKey string) http.HandlerFunc {
	m := t()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, m.MethodNotAllowed, http.StatusMethodNotAllowed)
			return
		}

		var req RCONBatchRequest
		if err := decodeJSONBody(w, r, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.Auth != authKey {
			log.Printf("denied batch request from %s: %s", r.RemoteAddr, m.InvalidAuthKey)
			writeJSON(w, r, RCONBatchResponse{Status: 0, Msg: m.InvalidAuthKey})
			return
		}
		if msg := validateTarget(req.IP, req.Port, req.Passwd); msg != "" {
			writeJSON(w, r, RCONBatchResponse{Status: 0, Msg: msg})
			return
		}
		if len(req.Msgs) == 0 {
			writeJSON(w, r, RCONBatchResponse{Status: 0, Msg: m.BatchEmpty})
			return
		}
		if len(req.Msgs) > maxBatchCommands {
			writeJSON(w, r, RCONBatchResponse{Status: 0, Msg: fmt.Sprintf(m.BatchTooMany, maxBatchCommands)})
			return
		}
		for _, cmd := range req.Msgs {
			if strings.TrimSpace(cmd) == "" {
				writeJSON(w, r, RCONBatchResponse{Status: 0, Msg: m.MsgRequired})
				return
			}
			if len(cmd) > utils.MaxCommandBytes {
				writeJSON(w, r, RCONBatchResponse{Status: 0, Msg: fmt.Sprintf(m.CommandTooLong, utils.MaxCommandBytes)})
				return
			}
		}

		start := time.Now()
		deadline := start.Add(batchTimeout)
		results := make([]RCONBatchResult, len(req.Msgs))

		conn, err := utils.DialWithConfig(targetAddr(req.IP, req.Port), req.Passwd, utils.Config{
			// One deadline covers the handshake, so connect + auth can never
			// consume the command budget twice over.
			Deadline: deadline,
		})
		if err != nil {
			// The endpoint is supported, but the target is not reachable. Every
			// command is reported as failed; the request shape stays valid so
			// the backend does not fall back to one request per command.
			msg := fmt.Sprintf(m.FailedConnectRCON, err)
			for i := range results {
				results[i] = RCONBatchResult{Status: 0, Msg: msg}
			}
			log.Printf("%s:%d batch connect failed (%dms): %v", strings.TrimSpace(req.IP), req.Port,
				time.Since(start).Milliseconds(), err)
			writeJSON(w, r, RCONBatchResponse{Status: 1, Results: results})
			return
		}
		defer conn.Close()

		for i, cmd := range req.Msgs {
			if left := time.Until(deadline); left <= 0 {
				results[i] = RCONBatchResult{Status: 0, Msg: m.BatchTimeout}
				continue
			}
			text, err := conn.Exec(cmd, timeLeft(deadline))
			if err != nil {
				results[i] = RCONBatchResult{Status: 0, Msg: err.Error()}
				continue
			}
			results[i] = RCONBatchResult{Status: 1, Msg: text}
		}

		log.Printf("%s:%d batch[%d] (%dms)", strings.TrimSpace(req.IP), req.Port, len(req.Msgs),
			time.Since(start).Milliseconds())
		writeJSON(w, r, RCONBatchResponse{Status: 1, Results: results})
	}
}

// timeLeft returns the remaining budget, never zero or negative so it can be
// handed to a dial/read timeout directly.
func timeLeft(deadline time.Time) time.Duration {
	left := time.Until(deadline)
	if left < time.Millisecond {
		return time.Millisecond
	}
	return left
}

func main() {
	initLang()

	cfg := loadConfig()
	if cfg == nil {
		cfg = promptConfig()
	}

	m := t()
	serveMux := http.NewServeMux()
	serveMux.Handle("/api/icu/daemon/rcon", acceptEncodingMiddleware(rconHandler(cfg.AuthKey)))
	serveMux.Handle("/api/icu/daemon/rcon/batch", acceptEncodingMiddleware(rconBatchHandler(cfg.AuthKey)))

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: serveMux,
		// Timeouts keep the daemon stable against slow or broken clients.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		// WriteTimeout must comfortably exceed the 2-minute RCON read budget.
		WriteTimeout:   150 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe() }()

	fmt.Printf(m.Starting+"\n", Version, cfg.Port)

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf(m.ListenFailed+"\n", err)
		}
	case <-ctx.Done():
		fmt.Println(m.ShuttingDown)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}
}
