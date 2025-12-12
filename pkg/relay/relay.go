package relay

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/yamux"
)

const nicknamesFile = "nicknames.json"

type Config struct {
	ControlPort int
	GamePort    int // Java Edition TCP port (default 25565)
	BedrockPort int // Bedrock Edition UDP port (default 19132, 0 to disable)
}

type Relay struct {
	Config Config

	// State
	tunnelSession *yamux.Session
	tunnelMutex   sync.Mutex
	GlobalBytes   int64 // Deprecated: use BytesFromPlayers + BytesFromTunnel
	BytesFromPlayers int64
	BytesFromTunnel  int64
	ActivePlayers int64
	PeakPlayers   int64
	PublicIP      string
	TunnelRemoteAddr string
	StartTime     time.Time

	// Players
	playersMutex sync.Mutex
	PlayerIPs    map[string]time.Time
	TCPConns     map[string]net.Conn
	UDPSessions  map[string]*bedrockSession
	PlayerStats  map[string]*PlayerStats
	BlockedIPs   map[string]time.Time
	Nicknames    map[string]string

	// Logging
	logBroadcaster *LogBroadcaster
}

type PlayerStats struct {
	BytesIn  int64
	BytesOut int64
}

func New(cfg Config) *Relay {
	r := &Relay{
		Config:         cfg,
		logBroadcaster: NewLogBroadcaster(),
		PublicIP:       "Fetching...",
		TunnelRemoteAddr: "None",
		StartTime:      time.Now(),
		PlayerIPs:      make(map[string]time.Time),
		TCPConns:       make(map[string]net.Conn),
		UDPSessions:    make(map[string]*bedrockSession),
		PlayerStats:    make(map[string]*PlayerStats),
		BlockedIPs:     make(map[string]time.Time),
		Nicknames:      make(map[string]string),
	}
	r.loadNicknames()
	return r
}

func (r *Relay) loadNicknames() {
	file, err := os.Open(nicknamesFile)
	if err != nil {
		return // File doesn't exist or can't be opened
	}
	defer file.Close()

	json.NewDecoder(file).Decode(&r.Nicknames)
}

func (r *Relay) saveNicknames() {
	file, err := os.Create(nicknamesFile)
	if err != nil {
		r.Log(fmt.Sprintf("Failed to save nicknames: %v", err))
		return
	}
	defer file.Close()

	json.NewEncoder(file).Encode(r.Nicknames)
}

func (r *Relay) SetNickname(ip, name string) {
	r.playersMutex.Lock()
	defer r.playersMutex.Unlock()
	
	if name == "" {
		delete(r.Nicknames, ip)
	} else {
		r.Nicknames[ip] = name
	}
	r.saveNicknames()
}

func (r *Relay) Start() {
	// Fetch IP
	go func() {
		client := http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get("https://api.ipify.org?format=text")
		if err == nil {
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			r.PublicIP = string(body)
			r.Log(fmt.Sprintf("Public IP: %s", r.PublicIP))
		} else {
			r.PublicIP = "Unknown"
			r.Log("Failed to fetch Public IP")
		}
	}()

	r.Log("Starting listeners...")
	go r.startControlServer()
	go r.startGameServer()

	// Start Bedrock UDP server if port is configured
	if r.Config.BedrockPort > 0 {
		go r.startBedrockServer()
	}
}

func (r *Relay) Log(msg string) {
	// Broadcast log to all listeners
	r.logBroadcaster.Broadcast(msg)
}

func (r *Relay) startControlServer() {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", r.Config.ControlPort))
	if err != nil {
		r.Log(fmt.Sprintf("[Control] Listener failed: %v", err))
		return
	}
	r.Log(fmt.Sprintf("[Control] Listening on :%d", r.Config.ControlPort))

	for {
		conn, err := listener.Accept()
		if err != nil {
			r.Log(fmt.Sprintf("[Control] Accept error: %v", err))
			continue
		}

		r.Log(fmt.Sprintf("[Control] Connection from %s", conn.RemoteAddr()))

		config := yamux.DefaultConfig()
		config.KeepAliveInterval = 10 * time.Second

		session, err := yamux.Server(conn, config)
		if err != nil {
			r.Log(fmt.Sprintf("[Control] Yamux session failed: %v", err))
			conn.Close()
			continue
		}

		r.tunnelMutex.Lock()
		if r.tunnelSession != nil {
			r.Log("[Control] Overwriting existing session")
			r.tunnelSession.Close()
		}
		r.tunnelSession = session
		r.TunnelRemoteAddr = conn.RemoteAddr().String()
		r.tunnelMutex.Unlock()

		r.Log("[Control] Tunnel established")
	}
}

func (r *Relay) startGameServer() {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", r.Config.GamePort))
	if err != nil {
		r.Log(fmt.Sprintf("[Game] Listener failed: %v", err))
		return
	}
	r.Log(fmt.Sprintf("[Game] Listening on :%d", r.Config.GamePort))

	for {
		playerConn, err := listener.Accept()
		if err != nil {
			r.Log(fmt.Sprintf("[Game] Accept error: %v", err))
			continue
		}

		go r.handlePlayer(playerConn)
	}
}

func (r *Relay) handlePlayer(playerConn net.Conn) {
	defer playerConn.Close()

	r.tunnelMutex.Lock()
	session := r.tunnelSession
	r.tunnelMutex.Unlock()

	if session == nil {
		return
	}

	ip, _, _ := net.SplitHostPort(playerConn.RemoteAddr().String())
	r.playersMutex.Lock()
	if _, blocked := r.BlockedIPs[ip]; blocked {
		r.playersMutex.Unlock()
		r.Log(fmt.Sprintf("[Game] Blocked connection attempt from %s", ip))
		return
	}
	r.playersMutex.Unlock()

	r.Log(fmt.Sprintf("[Game] Player connected: %s", playerConn.RemoteAddr()))
	
	stats := &PlayerStats{}
	r.playersMutex.Lock()
	r.PlayerIPs[playerConn.RemoteAddr().String()] = time.Now()
	r.TCPConns[playerConn.RemoteAddr().String()] = playerConn
	r.PlayerStats[playerConn.RemoteAddr().String()] = stats
	r.playersMutex.Unlock()

	newActive := atomic.AddInt64(&r.ActivePlayers, 1)
	
	// Update peak players
	for {
		peak := atomic.LoadInt64(&r.PeakPlayers)
		if newActive <= peak {
			break
		}
		if atomic.CompareAndSwapInt64(&r.PeakPlayers, peak, newActive) {
			break
		}
	}

	defer atomic.AddInt64(&r.ActivePlayers, -1)
	defer func() {
		r.playersMutex.Lock()
		delete(r.PlayerIPs, playerConn.RemoteAddr().String())
		delete(r.TCPConns, playerConn.RemoteAddr().String())
		delete(r.PlayerStats, playerConn.RemoteAddr().String())
		r.playersMutex.Unlock()
	}()

	stream, err := session.Open()
	if err != nil {
		r.Log(fmt.Sprintf("[Game] Failed to open stream: %v", err))
		return
	}
	defer stream.Close()

	// Send Player IP Header with protocol type
	// Format: "tcp:<IP:PORT>\n" for Java, "udp:<IP:PORT>\n" for Bedrock
	if _, err := stream.Write([]byte("tcp:" + playerConn.RemoteAddr().String() + "\n")); err != nil {
		r.Log(fmt.Sprintf("[Game] Failed to send header: %v", err))
		return
	}

	// Bidirectional copy with traffic counting
	done := make(chan struct{})

	go func() {
		// Stream -> Player (BytesOut)
		io.Copy(playerConn, &CountingReader{r: stream, counters: []*int64{&r.BytesFromTunnel, &stats.BytesOut}})
		done <- struct{}{}
	}()

	go func() {
		// Player -> Stream (BytesIn)
		io.Copy(stream, &CountingReader{r: playerConn, counters: []*int64{&r.BytesFromPlayers, &stats.BytesIn}})
		done <- struct{}{}
	}()

	<-done
	r.Log(fmt.Sprintf("[Game] Player disconnected: %s", playerConn.RemoteAddr()))
}

func (r *Relay) Disconnect(ip string) bool {
	r.playersMutex.Lock()
	defer r.playersMutex.Unlock()

	// Check TCP connections
	if conn, ok := r.TCPConns[ip]; ok {
		conn.Close()
		// Cleanup happens in handlePlayer defer
		return true
	}

	// Check UDP sessions
	if session, ok := r.UDPSessions[ip]; ok {
		session.stream.Close()
		// Cleanup happens in readFromTunnel defer
		return true
	}

	return false
}

func (r *Relay) Block(ip string) {
	r.playersMutex.Lock()
	defer r.playersMutex.Unlock()
	r.BlockedIPs[ip] = time.Now()
	r.Log(fmt.Sprintf("Blocked IP: %s", ip))
}

func (r *Relay) Unblock(ip string) {
	r.playersMutex.Lock()
	defer r.playersMutex.Unlock()
	delete(r.BlockedIPs, ip)
	r.Log(fmt.Sprintf("Unblocked IP: %s", ip))
}


// startBedrockServer starts the UDP listener for Bedrock Edition players (Geyser)
func (r *Relay) startBedrockServer() {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", r.Config.BedrockPort))
	if err != nil {
		r.Log(fmt.Sprintf("[Bedrock] Failed to resolve address: %v", err))
		return
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		r.Log(fmt.Sprintf("[Bedrock] Listener failed: %v", err))
		return
	}
	defer conn.Close()

	r.Log(fmt.Sprintf("[Bedrock] Listening on :%d (UDP)", r.Config.BedrockPort))

	buffer := make([]byte, 65535) // Max UDP packet size

	for {
		n, remoteAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			r.Log(fmt.Sprintf("[Bedrock] Read error: %v", err))
			continue
		}

		key := remoteAddr.String()
		data := make([]byte, n)
		copy(data, buffer[:n])

		r.playersMutex.Lock()
		session, exists := r.UDPSessions[key]
		if !exists {
			// New Bedrock player
			session = r.createBedrockSession(conn, remoteAddr)
			if session == nil {
				r.playersMutex.Unlock()
				continue
			}
			r.UDPSessions[key] = session
		}
		r.playersMutex.Unlock()

		// Forward packet to tunnel
		session.sendToTunnel(data)
	}
}

type bedrockSession struct {
	relay      *Relay
	udpConn    *net.UDPConn
	remoteAddr *net.UDPAddr
	stream     net.Conn
	stats      *PlayerStats
	done       chan struct{}
}

func (r *Relay) createBedrockSession(udpConn *net.UDPConn, remoteAddr *net.UDPAddr) *bedrockSession {
	r.tunnelMutex.Lock()
	tunnelSession := r.tunnelSession
	r.tunnelMutex.Unlock()

	if tunnelSession == nil {
		return nil
	}

	ip := remoteAddr.IP.String()
	// Note: Caller holds playersMutex, but we need to check BlockedIPs.
	// Since BlockedIPs is protected by playersMutex (we decided to use the same mutex for simplicity),
	// we can check it directly.
	if _, blocked := r.BlockedIPs[ip]; blocked {
		r.Log(fmt.Sprintf("[Bedrock] Blocked connection attempt from %s", ip))
		return nil
	}

	r.Log(fmt.Sprintf("[Bedrock] Player connected: %s", remoteAddr.String()))
	
	// Note: Caller holds playersMutex when calling this.
	// We are just updating PlayerIPs which is protected by playersMutex.
	r.PlayerIPs[remoteAddr.String()] = time.Now()
	
	stats := &PlayerStats{}
	r.PlayerStats[remoteAddr.String()] = stats

	newActive := atomic.AddInt64(&r.ActivePlayers, 1)

	// Update peak players
	for {
		peak := atomic.LoadInt64(&r.PeakPlayers)
		if newActive <= peak {
			break
		}
		if atomic.CompareAndSwapInt64(&r.PeakPlayers, peak, newActive) {
			break
		}
	}

	stream, err := tunnelSession.Open()
	if err != nil {
		r.Log(fmt.Sprintf("[Bedrock] Failed to open stream: %v", err))
		atomic.AddInt64(&r.ActivePlayers, -1)
		return nil
	}

	// Send Player IP Header with UDP protocol marker
	if _, err := stream.Write([]byte("udp:" + remoteAddr.String() + "\n")); err != nil {
		r.Log(fmt.Sprintf("[Bedrock] Failed to send header: %v", err))
		stream.Close()
		atomic.AddInt64(&r.ActivePlayers, -1)
		return nil
	}

	session := &bedrockSession{
		relay:      r,
		udpConn:    udpConn,
		remoteAddr: remoteAddr,
		stream:     stream,
		stats:      stats,
		done:       make(chan struct{}),
	}

	// Start goroutine to read from tunnel and send back to UDP client
	go session.readFromTunnel()

	return session
}

func (s *bedrockSession) sendToTunnel(data []byte) {
	// Write length-prefixed packet to stream
	lenBuf := make([]byte, 2)
	lenBuf[0] = byte(len(data) >> 8)
	lenBuf[1] = byte(len(data) & 0xFF)

	s.stream.Write(lenBuf)
	s.stream.Write(data)
	n := int64(len(data) + 2)
	atomic.AddInt64(&s.relay.BytesFromPlayers, n)
	atomic.AddInt64(&s.stats.BytesIn, n)
}

func (s *bedrockSession) readFromTunnel() {
	defer func() {
		s.stream.Close()
		atomic.AddInt64(&s.relay.ActivePlayers, -1)
		
		s.relay.playersMutex.Lock()
		delete(s.relay.PlayerIPs, s.remoteAddr.String())
		delete(s.relay.UDPSessions, s.remoteAddr.String())
		delete(s.relay.PlayerStats, s.remoteAddr.String())
		s.relay.playersMutex.Unlock()

		s.relay.Log(fmt.Sprintf("[Bedrock] Player disconnected: %s", s.remoteAddr.String()))

		close(s.done)
	}()

	lenBuf := make([]byte, 2)
	for {
		// Read length prefix
		_, err := io.ReadFull(s.stream, lenBuf)
		if err != nil {
			return
		}

		pktLen := int(lenBuf[0])<<8 | int(lenBuf[1])
		if pktLen > 65535 {
			return
		}

		// Read packet data
		data := make([]byte, pktLen)
		_, err = io.ReadFull(s.stream, data)
		if err != nil {
			return
		}

		n := int64(pktLen + 2)
		atomic.AddInt64(&s.relay.BytesFromTunnel, n)
		atomic.AddInt64(&s.stats.BytesOut, n)

		// Send back to UDP client
		s.udpConn.WriteToUDP(data, s.remoteAddr)
	}
}

// CountingReader wraps an io.Reader and counts bytes read
type CountingReader struct {
	r        io.Reader
	counters []*int64
}

func (c *CountingReader) Read(p []byte) (n int, err error) {
	n, err = c.r.Read(p)
	if n > 0 {
		for _, counter := range c.counters {
			atomic.AddInt64(counter, int64(n))
		}
	}
	return
}

// LogBroadcaster handles multiple subscribers for logs
type LogBroadcaster struct {
	subscribers []chan string
	mu          sync.Mutex
}

func NewLogBroadcaster() *LogBroadcaster {
	return &LogBroadcaster{
		subscribers: make([]chan string, 0),
	}
}

func (b *LogBroadcaster) Subscribe() chan string {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan string, 100)
	b.subscribers = append(b.subscribers, ch)
	return ch
}

func (b *LogBroadcaster) Unsubscribe(ch chan string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, sub := range b.subscribers {
		if sub == ch {
			b.subscribers = append(b.subscribers[:i], b.subscribers[i+1:]...)
			close(ch)
			break
		}
	}
}

func (b *LogBroadcaster) Broadcast(msg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subscribers {
		select {
		case ch <- msg:
		default:
			// Drop message if channel is full to prevent blocking
		}
	}
}
