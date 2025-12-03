package daemon

import (
	"encoding/json"
	"time"
)

// ServerStatus represents the current state of the server
type ServerStatus struct {
	Status           string    `json:"status"`
	PublicIP         string    `json:"public_ip"`
	ControlPort      int       `json:"control_port"`
	GamePort         int       `json:"game_port"`
	Uptime           int64     `json:"uptime"` // Duration in seconds
	StartTime        time.Time `json:"start_time"`
	ActivePlayers    int       `json:"active_players"`
	BytesTransferred int64     `json:"bytes_transferred"`
	Logs             []string  `json:"logs"`
}

// Message is the IPC message format
type Message struct {
	Type string          `json:"type"` // "status", "log", "error"
	Data json.RawMessage `json:"data"`
}

// LogEntry represents a log message
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
}
