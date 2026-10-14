package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
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

// RCONResponse is the JSON body returned to the client.
type RCONResponse struct {
	Status int    `json:"status"`
	Msg    string `json:"msg"`
}

// maxBodyBytes caps the request body. A valid request is a few hundred bytes;
// the RCON client itself rejects commands longer than 1014 bytes.
const maxBodyBytes = 64 << 10

func rconHandler(authKey string) http.HandlerFunc {
	m := t()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, m.MethodNotAllowed, http.StatusMethodNotAllowed)
			return
		}

		var req RCONRequest
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.Auth != authKey {
			log.Printf("denied request from %s: %s", r.RemoteAddr, m.InvalidAuthKey)
			jsonResponse(w, RCONResponse{Status: 0, Msg: m.InvalidAuthKey})
			return
		}

		start := time.Now()
		conn, err := utils.Dial(fmt.Sprintf("%s:%d", req.IP, req.Port), req.Passwd)
		if err != nil {
			jsonResponse(w, RCONResponse{Status: 0, Msg: fmt.Sprintf(m.FailedConnectRCON, err)})
			return
		}
		defer conn.Close()

		// Re-dial with the requested port: Dial needs host:port.
		requestID, err := conn.Write(req.Msg)
		if err != nil {
			jsonResponse(w, RCONResponse{Status: 0, Msg: fmt.Sprintf(m.FailedExecRCON, err)})
			return
		}

		response, responseID, err := conn.Read()
		if err != nil {
			jsonResponse(w, RCONResponse{Status: 0, Msg: fmt.Sprintf(m.FailedReadRCON, err)})
			return
		}
		if requestID != responseID {
			jsonResponse(w, RCONResponse{Status: 0, Msg: m.RCONIDMismatch})
			return
		}

		log.Printf("%s:%d <- %q (%dms)", req.IP, req.Port, req.Msg, time.Since(start).Milliseconds())
		jsonResponse(w, RCONResponse{Status: 1, Msg: response})
	}
}

func jsonResponse(w http.ResponseWriter, response RCONResponse) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func main() {
	initLang()

	cfg := loadConfig()
	if cfg == nil {
		cfg = promptConfig()
	}

	m := t()
	serveMux := http.NewServeMux()
	serveMux.HandleFunc("/api/icu/daemon/rcon", rconHandler(cfg.AuthKey))

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
