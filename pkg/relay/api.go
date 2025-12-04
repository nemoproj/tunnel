package relay

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

type StatusResponse struct {
	PublicIP         string `json:"public_ip"`
	ControlPort      int    `json:"control_port"`
	GamePort         int    `json:"game_port"`
	ActivePlayers    int64  `json:"active_players"`
	BytesTransferred int64  `json:"bytes_transferred"`
	TunnelConnected  bool   `json:"tunnel_connected"`
	UptimeSeconds    int64  `json:"uptime_seconds"`
}

func (r *Relay) StartAPI(port int) {
	mux := http.NewServeMux()
	mux.HandleFunc("/status", r.handleStatus)
	mux.HandleFunc("/logs", r.handleLogs)

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	r.Log(fmt.Sprintf("[API] Listening on :%d", port))
	if err := server.ListenAndServe(); err != nil {
		r.Log(fmt.Sprintf("[API] Server failed: %v", err))
	}
}

func (r *Relay) handleStatus(w http.ResponseWriter, req *http.Request) {
	r.tunnelMutex.Lock()
	connected := r.tunnelSession != nil && !r.tunnelSession.IsClosed()
	r.tunnelMutex.Unlock()

	status := StatusResponse{
		PublicIP:         r.PublicIP,
		ControlPort:      r.Config.ControlPort,
		GamePort:         r.Config.GamePort,
		ActivePlayers:    atomic.LoadInt64(&r.ActivePlayers),
		BytesTransferred: atomic.LoadInt64(&r.GlobalBytes),
		TunnelConnected:  connected,
		UptimeSeconds:    int64(time.Since(r.StartTime).Seconds()),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (r *Relay) handleLogs(w http.ResponseWriter, req *http.Request) {
	// SSE implementation
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := r.logBroadcaster.Subscribe()
	defer r.logBroadcaster.Unsubscribe(ch)

	// Send initial connection message
	fmt.Fprintf(w, "data: Connected to log stream\n\n")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		case <-req.Context().Done():
			return
		}
	}
}
