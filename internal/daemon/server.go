package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/yamux"
)

const (
	DefaultSocketPath = "/tmp/tunnel-server.sock"
)

// Server represents the tunnel relay server
type Server struct {
	controlPort int
	gamePort    int

	status           string
	publicIP         string
	startTime        time.Time
	activePlayers    int32
	bytesTransferred int64
	logs             []LogEntry
	logsMutex        sync.RWMutex
	maxLogs          int

	tunnelSession *yamux.Session
	tunnelMutex   sync.Mutex

	socketPath string
	listeners  []net.Listener
	wg         sync.WaitGroup
	done       chan struct{}
}

// NewServer creates a new tunnel server
func NewServer(controlPort, gamePort int, socketPath string) *Server {
	if socketPath == "" {
		socketPath = DefaultSocketPath
	}
	return &Server{
		controlPort: controlPort,
		gamePort:    gamePort,
		status:      "Initializing...",
		publicIP:    "Unknown",
		startTime:   time.Now(),
		maxLogs:     100,
		socketPath:  socketPath,
		listeners:   make([]net.Listener, 0),
		done:        make(chan struct{}),
	}
}

// Start starts the server
func (s *Server) Start() error {
	// Fetch public IP
	go s.fetchPublicIP()

	// Start control server
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := s.startControlServer(); err != nil {
			s.logError(fmt.Errorf("control server error: %v", err))
		}
	}()

	// Start game server
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := s.startGameServer(); err != nil {
			s.logError(fmt.Errorf("game server error: %v", err))
		}
	}()

	// Start IPC server
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := s.startIPCServer(); err != nil {
			s.logError(fmt.Errorf("IPC server error: %v", err))
		}
	}()

	return nil
}

// Wait waits for the server to finish
func (s *Server) Wait() {
	s.wg.Wait()
}

// Stop stops the server
func (s *Server) Stop() {
	close(s.done)
	for _, listener := range s.listeners {
		listener.Close()
	}
	// Remove socket file
	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		s.log(fmt.Sprintf("Warning: Failed to remove socket file: %v", err))
	}
}

func (s *Server) fetchPublicIP() {
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("https://api.ipify.org?format=text")
	if err == nil {
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err == nil {
			s.publicIP = string(body)
		} else {
			s.publicIP = "Unknown"
		}
	} else {
		s.publicIP = "Unknown"
	}
}

func (s *Server) log(msg string) {
	s.logsMutex.Lock()
	defer s.logsMutex.Unlock()

	entry := LogEntry{
		Timestamp: time.Now(),
		Message:   msg,
	}
	s.logs = append(s.logs, entry)
	if len(s.logs) > s.maxLogs {
		s.logs = s.logs[1:]
	}
}

func (s *Server) logError(err error) {
	s.log(fmt.Sprintf("Error: %v", err))
}

func (s *Server) startControlServer() error {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", s.controlPort))
	if err != nil {
		return fmt.Errorf("control listener failed: %v", err)
	}
	s.listeners = append(s.listeners, listener)
	s.log(fmt.Sprintf("[Control] Listening on :%d", s.controlPort))
	s.status = "Waiting for Host..."

	for {
		conn, err := listener.Accept()
		if err != nil {
			// Check if listener was closed intentionally
			select {
			case <-s.done:
				return nil
			default:
				s.logError(fmt.Errorf("control accept error: %v", err))
				continue
			}
		}

		s.log(fmt.Sprintf("[Control] Connection from %s", conn.RemoteAddr()))

		// Setup Yamux Server
		config := yamux.DefaultConfig()
		config.KeepAliveInterval = 10 * time.Second

		session, err := yamux.Server(conn, config)
		if err != nil {
			s.logError(fmt.Errorf("yamux session failed: %v", err))
			conn.Close()
			continue
		}

		s.tunnelMutex.Lock()
		if s.tunnelSession != nil {
			s.log("[Control] Overwriting existing session")
			s.tunnelSession.Close()
		}
		s.tunnelSession = session
		s.tunnelMutex.Unlock()

		s.status = "Host Connected"
		s.log("[Control] Tunnel established")
	}
}

func (s *Server) startGameServer() error {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", s.gamePort))
	if err != nil {
		return fmt.Errorf("game listener failed: %v", err)
	}
	s.listeners = append(s.listeners, listener)
	s.log(fmt.Sprintf("[Game] Listening on :%d", s.gamePort))

	for {
		playerConn, err := listener.Accept()
		if err != nil {
			// Check if listener was closed intentionally
			select {
			case <-s.done:
				return nil
			default:
				s.logError(fmt.Errorf("game accept error: %v", err))
				continue
			}
		}

		go s.handlePlayer(playerConn)
	}
}

func (s *Server) handlePlayer(playerConn net.Conn) {
	defer playerConn.Close()

	s.tunnelMutex.Lock()
	session := s.tunnelSession
	s.tunnelMutex.Unlock()

	if session == nil {
		return
	}

	s.log(fmt.Sprintf("[Game] Player connected: %s", playerConn.RemoteAddr()))
	atomic.AddInt32(&s.activePlayers, 1)
	defer atomic.AddInt32(&s.activePlayers, -1)

	stream, err := session.Open()
	if err != nil {
		s.logError(fmt.Errorf("failed to open stream: %v", err))
		return
	}
	defer stream.Close()

	// Send Player IP Header
	if _, err := stream.Write([]byte(playerConn.RemoteAddr().String() + "\n")); err != nil {
		s.logError(fmt.Errorf("failed to send header: %v", err))
		return
	}

	// Bidirectional copy with traffic counting
	done := make(chan struct{})

	go func() {
		// Stream -> Player
		n, err := io.Copy(playerConn, stream)
		if err != nil && err != io.EOF {
			// Log errors other than EOF and connection resets
			s.log(fmt.Sprintf("[Game] Copy error (stream->player): %v", err))
		}
		atomic.AddInt64(&s.bytesTransferred, n)
		done <- struct{}{}
	}()

	go func() {
		// Player -> Stream
		n, err := io.Copy(stream, playerConn)
		if err != nil && err != io.EOF {
			// Log errors other than EOF and connection resets
			s.log(fmt.Sprintf("[Game] Copy error (player->stream): %v", err))
		}
		atomic.AddInt64(&s.bytesTransferred, n)
		done <- struct{}{}
	}()

	<-done
	s.log(fmt.Sprintf("[Game] Player disconnected: %s", playerConn.RemoteAddr()))
}

func (s *Server) startIPCServer() error {
	// Remove existing socket file if it exists
	os.Remove(s.socketPath)

	// Ensure the directory exists
	dir := filepath.Dir(s.socketPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create socket directory: %v", err)
	}

	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("IPC listener failed: %v", err)
	}
	s.listeners = append(s.listeners, listener)

	// Make socket accessible to user
	if err := os.Chmod(s.socketPath, 0600); err != nil {
		s.log(fmt.Sprintf("Warning: Failed to set socket permissions: %v", err))
	}

	for {
		conn, err := listener.Accept()
		if err != nil {
			// Check if listener was closed intentionally
			select {
			case <-s.done:
				return nil
			default:
				continue
			}
		}

		go s.handleIPCClient(conn)
	}
}

func (s *Server) handleIPCClient(conn net.Conn) {
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		status := s.GetStatus()
		msg := Message{
			Type: "status",
			Data: json.RawMessage{},
		}
		data, err := json.Marshal(status)
		if err != nil {
			continue
		}
		msg.Data = data

		if err := encoder.Encode(msg); err != nil {
			return
		}
	}
}

// GetStatus returns the current server status
func (s *Server) GetStatus() ServerStatus {
	s.logsMutex.RLock()
	defer s.logsMutex.RUnlock()

	// Get last 10 logs for status
	logs := make([]string, 0, 10)
	start := len(s.logs) - 10
	if start < 0 {
		start = 0
	}
	for i := start; i < len(s.logs); i++ {
		logs = append(logs, fmt.Sprintf("[%s] %s",
			s.logs[i].Timestamp.Format("15:04:05"), s.logs[i].Message))
	}

	return ServerStatus{
		Status:           s.status,
		PublicIP:         s.publicIP,
		ControlPort:      s.controlPort,
		GamePort:         s.gamePort,
		Uptime:           int64(time.Since(s.startTime).Seconds()),
		StartTime:        s.startTime,
		ActivePlayers:    int(atomic.LoadInt32(&s.activePlayers)),
		BytesTransferred: atomic.LoadInt64(&s.bytesTransferred),
		Logs:             logs,
	}
}
