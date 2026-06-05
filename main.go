package main

import (
	"encoding/json"
	"fmt"
	"icu-rcon-daemon/utils"
	"log"
	"net/http"

	"github.com/gorilla/mux"
)

type RCONRequest struct {
	IP     string `json:"ip"`
	Port   int    `json:"port"`
	Passwd string `json:"passwd"`
	Msg    string `json:"msg"`
	Auth   string `json:"auth"`
}

type RCONResponse struct {
	Status int    `json:"status"`
	Msg    string `json:"msg"`
}

func rconHandler(authKey string) http.HandlerFunc {
	m := t()
	return func(w http.ResponseWriter, r *http.Request) {
		var req RCONRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		if req.Auth != authKey {
			jsonResponse(w, RCONResponse{Status: 0, Msg: m.InvalidAuthKey})
			return
		}

		conn, err := utils.Dial(fmt.Sprintf("%s:%d", req.IP, req.Port), req.Passwd)
		if err != nil {
			jsonResponse(w, RCONResponse{Status: 0, Msg: fmt.Sprintf(m.FailedConnectRCON, err)})
			return
		}
		defer conn.Close()

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
	router := mux.NewRouter()
	router.HandleFunc("/api/icu/daemon/rcon", rconHandler(cfg.AuthKey)).Methods("POST")

	fmt.Printf(m.Starting+"\n", Version, cfg.Port)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, router))
}
