package relay

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/yamux"
)

type Config struct {
	ControlPort  int
	GamePort     int    // Java Edition TCP port (default 25565)
	BedrockPort  int    // Bedrock Edition UDP port (default 19132, 0 to disable)
}

type Relay struct {
	Config Config

	// State
	tunnelSession *yamux.Session
	tunnelMutex   sync.Mutex
	GlobalBytes   int64
	ActivePlayers int64
	PublicIP      string
	StartTime     time.Time

	// Logging
	logBroadcaster *LogBroadcaster
}

func New(cfg Config) *Relay {
	return &Relay{
		Config:         cfg,
		logBroadcaster: NewLogBroadcaster(),
		PublicIP:       "Fetching...",
		StartTime:      time.Now(),
	}
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

	r.Log(fmt.Sprintf("[Game] Player connected: %s", playerConn.RemoteAddr()))
	atomic.AddInt64(&r.ActivePlayers, 1)
	defer atomic.AddInt64(&r.ActivePlayers, -1)

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
		// Stream -> Player
		io.Copy(playerConn, &CountingReader{r: stream, counter: &r.GlobalBytes})
		done <- struct{}{}
	}()

	go func() {
		// Player -> Stream
		io.Copy(stream, &CountingReader{r: playerConn, counter: &r.GlobalBytes})
		done <- struct{}{}
	}()

	<-done
	r.Log(fmt.Sprintf("[Game] Player disconnected: %s", playerConn.RemoteAddr()))
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

	// Track active Bedrock sessions
	sessions := make(map[string]*bedrockSession)
	sessionsMutex := sync.Mutex{}

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

		sessionsMutex.Lock()
		session, exists := sessions[key]
		if !exists {
			// New Bedrock player
			session = r.createBedrockSession(conn, remoteAddr, sessions, &sessionsMutex)
			if session == nil {
				sessionsMutex.Unlock()
				continue
			}
			sessions[key] = session
		}
		sessionsMutex.Unlock()

		// Forward packet to tunnel
		session.sendToTunnel(data)
	}
}

type bedrockSession struct {
	relay      *Relay
	udpConn    *net.UDPConn
	remoteAddr *net.UDPAddr
	stream     net.Conn
	done       chan struct{}
}

func (r *Relay) createBedrockSession(udpConn *net.UDPConn, remoteAddr *net.UDPAddr, sessions map[string]*bedrockSession, mutex *sync.Mutex) *bedrockSession {
	r.tunnelMutex.Lock()
	tunnelSession := r.tunnelSession
	r.tunnelMutex.Unlock()

	if tunnelSession == nil {
		return nil
	}

	r.Log(fmt.Sprintf("[Bedrock] Player connected: %s", remoteAddr.String()))
	atomic.AddInt64(&r.ActivePlayers, 1)

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
		done:       make(chan struct{}),
	}

	// Start goroutine to read from tunnel and send back to UDP client
	go session.readFromTunnel(sessions, mutex)

	return session
}

func (s *bedrockSession) sendToTunnel(data []byte) {
	// Write length-prefixed packet to stream
	lenBuf := make([]byte, 2)
	lenBuf[0] = byte(len(data) >> 8)
	lenBuf[1] = byte(len(data) & 0xFF)

	s.stream.Write(lenBuf)
	s.stream.Write(data)
	atomic.AddInt64(&s.relay.GlobalBytes, int64(len(data)+2))
}

func (s *bedrockSession) readFromTunnel(sessions map[string]*bedrockSession, mutex *sync.Mutex) {
	defer func() {
		s.stream.Close()
		atomic.AddInt64(&s.relay.ActivePlayers, -1)
		s.relay.Log(fmt.Sprintf("[Bedrock] Player disconnected: %s", s.remoteAddr.String()))

		mutex.Lock()
		delete(sessions, s.remoteAddr.String())
		mutex.Unlock()

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

		atomic.AddInt64(&s.relay.GlobalBytes, int64(pktLen+2))

		// Send back to UDP client
		s.udpConn.WriteToUDP(data, s.remoteAddr)
	}
}

// CountingReader wraps an io.Reader and counts bytes read
type CountingReader struct {
	r       io.Reader
	counter *int64
}

func (c *CountingReader) Read(p []byte) (n int, err error) {
	n, err = c.r.Read(p)
	if n > 0 {
		atomic.AddInt64(c.counter, int64(n))
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
