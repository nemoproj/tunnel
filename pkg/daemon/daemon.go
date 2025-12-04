package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
)

var (
	ErrAlreadyRunning = errors.New("daemon is already running")
	ErrNotRunning     = errors.New("daemon is not running")
)

// DefaultPidFile returns the default PID file path
func DefaultPidFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/tunnel-relay.pid"
	}
	return filepath.Join(home, ".tunnel-relay.pid")
}

// DefaultLogFile returns the default log file path
func DefaultLogFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/tunnel-relay.log"
	}
	return filepath.Join(home, ".tunnel-relay.log")
}

// WritePid writes the current process PID to the pid file
func WritePid(pidFile string) error {
	return os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644)
}

// ReadPid reads the PID from the pid file
func ReadPid(pidFile string) (int, error) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(data))
}

// RemovePid removes the pid file
func RemovePid(pidFile string) error {
	return os.Remove(pidFile)
}

// IsRunning checks if the daemon is already running
func IsRunning(pidFile string) (bool, int) {
	pid, err := ReadPid(pidFile)
	if err != nil {
		return false, 0
	}

	if !isProcessRunning(pid) {
		// Process doesn't exist, clean up stale PID file
		os.Remove(pidFile)
		return false, 0
	}

	return true, pid
}

// Status returns the daemon status
func Status(pidFile string) (bool, int) {
	return IsRunning(pidFile)
}
