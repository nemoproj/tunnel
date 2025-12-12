package relay

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

type StatusResponse struct {
	PublicIP         string `json:"public_ip"`
	ControlPort      int    `json:"control_port"`
	GamePort         int    `json:"game_port"`
	BedrockPort      int    `json:"bedrock_port,omitempty"`
	ActivePlayers    int64    `json:"active_players"`
	PeakPlayers      int64    `json:"peak_players"`
	PlayerList       []string `json:"player_list"`
	BytesTransferred int64    `json:"bytes_transferred"`
	BytesFromPlayers int64  `json:"bytes_from_players"`
	BytesFromTunnel  int64  `json:"bytes_from_tunnel"`
	TunnelConnected  bool   `json:"tunnel_connected"`
	TunnelRemoteAddr string `json:"tunnel_remote_addr"`
	NumStreams       int    `json:"num_streams"`
	UptimeSeconds    int64  `json:"uptime_seconds"`
}

func (r *Relay) StartAPI(port int) {
	mux := http.NewServeMux()
	mux.HandleFunc("/status", r.handleStatus)
	mux.HandleFunc("/logs", r.handleLogs)
	mux.HandleFunc("/connections", r.handleConnections)
	mux.HandleFunc("/blocklist", r.handleBlocklist)
	mux.HandleFunc("/nicknames", r.handleNicknames)

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
	numStreams := 0
	if connected {
		numStreams = r.tunnelSession.NumStreams()
	}
	remoteAddr := r.TunnelRemoteAddr
	r.tunnelMutex.Unlock()

	r.playersMutex.Lock()
	playerList := make([]string, 0, len(r.PlayerIPs))
	for ip := range r.PlayerIPs {
		playerList = append(playerList, ip)
	}
	r.playersMutex.Unlock()

	bytesFromPlayers := atomic.LoadInt64(&r.BytesFromPlayers)
	bytesFromTunnel := atomic.LoadInt64(&r.BytesFromTunnel)

	status := StatusResponse{
		PublicIP:         r.PublicIP,
		ControlPort:      r.Config.ControlPort,
		GamePort:         r.Config.GamePort,
		BedrockPort:      r.Config.BedrockPort,
		ActivePlayers:    atomic.LoadInt64(&r.ActivePlayers),
		PeakPlayers:      atomic.LoadInt64(&r.PeakPlayers),
		PlayerList:       playerList,
		BytesTransferred: bytesFromPlayers + bytesFromTunnel,
		BytesFromPlayers: bytesFromPlayers,
		BytesFromTunnel:  bytesFromTunnel,
		TunnelConnected:  connected,
		TunnelRemoteAddr: remoteAddr,
		NumStreams:       numStreams,
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

func (r *Relay) handleConnections(w http.ResponseWriter, req *http.Request) {
	if req.Method == http.MethodDelete {
		ip := req.URL.Query().Get("ip")
		if ip == "" {
			http.Error(w, "Missing ip parameter", http.StatusBadRequest)
			return
		}

		if r.Disconnect(ip) {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "Disconnected %s", ip)
		} else {
			http.Error(w, "Player not found", http.StatusNotFound)
		}
		return
	}

	if req.Method == http.MethodGet {
		r.playersMutex.Lock()
		defer r.playersMutex.Unlock()

		type ConnectionInfo struct {
			IP          string    `json:"ip"`
			Nickname    string    `json:"nickname"`
			Type        string    `json:"type"`
			ConnectedAt time.Time `json:"connected_at"`
			BytesIn     int64     `json:"bytes_in"`
			BytesOut    int64     `json:"bytes_out"`
			Latency     string    `json:"latency"`
		}

		conns := make([]ConnectionInfo, 0)
		for ip, t := range r.PlayerIPs {
			connType := "unknown"
			if _, ok := r.TCPConns[ip]; ok {
				connType = "tcp"
			} else if _, ok := r.UDPSessions[ip]; ok {
				connType = "udp"
			}
			
			var bytesIn, bytesOut int64
			if stats, ok := r.PlayerStats[ip]; ok {
				bytesIn = atomic.LoadInt64(&stats.BytesIn)
				bytesOut = atomic.LoadInt64(&stats.BytesOut)
			}

			nickname := r.Nicknames[ip]

			conns = append(conns, ConnectionInfo{
				IP:          ip,
				Nickname:    nickname,
				Type:        connType,
				ConnectedAt: t,
				BytesIn:     bytesIn,
				BytesOut:    bytesOut,
				Latency:     "N/A", // Placeholder for now
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(conns)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (r *Relay) handleNicknames(w http.ResponseWriter, req *http.Request) {
	if req.Method == http.MethodPost {
		ip := req.URL.Query().Get("ip")
		name := req.URL.Query().Get("name")
		if ip == "" {
			http.Error(w, "Missing ip parameter", http.StatusBadRequest)
			return
		}
		r.SetNickname(ip, name)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (r *Relay) handleBlocklist(w http.ResponseWriter, req *http.Request) {
	if req.Method == http.MethodPost {
		ip := req.URL.Query().Get("ip")
		if ip == "" {
			http.Error(w, "Missing ip parameter", http.StatusBadRequest)
			return
		}
		r.Block(ip)
		// Also disconnect if currently connected
		// We need to find the full address (IP:Port) for the disconnect map
		// But Disconnect takes the full address key.
		// We need to iterate connections to find matches.
		r.disconnectByIP(ip)
		
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Blocked %s", ip)
		return
	}

	if req.Method == http.MethodDelete {
		ip := req.URL.Query().Get("ip")
		if ip == "" {
			http.Error(w, "Missing ip parameter", http.StatusBadRequest)
			return
		}
		r.Unblock(ip)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Unblocked %s", ip)
		return
	}

	if req.Method == http.MethodGet {
		r.playersMutex.Lock()
		defer r.playersMutex.Unlock()

		type BlockedInfo struct {
			IP        string    `json:"ip"`
			BlockedAt time.Time `json:"blocked_at"`
		}

		blocked := make([]BlockedInfo, 0)
		for ip, t := range r.BlockedIPs {
			blocked = append(blocked, BlockedInfo{
				IP:        ip,
				BlockedAt: t,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(blocked)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (r *Relay) disconnectByIP(targetIP string) {
	var tcpConnsToClose []net.Conn
	var udpSessionsToClose []*bedrockSession

	r.playersMutex.Lock()
	// Check TCP connections
	for addr, conn := range r.TCPConns {
		host, _, _ := net.SplitHostPort(addr)
		if host == targetIP {
			tcpConnsToClose = append(tcpConnsToClose, conn)
		}
	}

	// Check UDP sessions
	for addr, session := range r.UDPSessions {
		host, _, _ := net.SplitHostPort(addr)
		if host == targetIP {
			udpSessionsToClose = append(udpSessionsToClose, session)
		}
	}
	r.playersMutex.Unlock()

	// Close connections outside the lock to avoid deadlocks
	for _, conn := range tcpConnsToClose {
		conn.Close()
	}
	for _, session := range udpSessionsToClose {
		session.stream.Close()
	}
}
