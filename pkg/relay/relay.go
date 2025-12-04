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
	ControlPort int
	GamePort    int
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

	// Send Player IP Header
	if _, err := stream.Write([]byte(playerConn.RemoteAddr().String() + "\n")); err != nil {
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
