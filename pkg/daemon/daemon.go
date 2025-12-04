package daemon

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
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

	// Check if process exists
	process, err := os.FindProcess(pid)
	if err != nil {
		return false, 0
	}

	// On Unix, FindProcess always succeeds, so we need to send signal 0
	err = process.Signal(syscall.Signal(0))
	if err != nil {
		// Process doesn't exist, clean up stale PID file
		os.Remove(pidFile)
		return false, 0
	}

	return true, pid
}

// Start starts the daemon process in the background
func Start(pidFile string, logFile string, args []string) error {
	if running, pid := IsRunning(pidFile); running {
		return fmt.Errorf("%w: PID %d", ErrAlreadyRunning, pid)
	}

	// Get the current executable path
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Prepare arguments: add --daemon flag to indicate running as daemon
	daemonArgs := append([]string{"--daemon"}, args...)

	cmd := exec.Command(executable, daemonArgs...)

	// Open log file for stdout/stderr
	logFd, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	cmd.Stdout = logFd
	cmd.Stderr = logFd
	cmd.Stdin = nil

	// Set up process attributes for daemonization
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true, // Create new session (detach from terminal)
	}

	if err := cmd.Start(); err != nil {
		logFd.Close()
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	fmt.Printf("Daemon started with PID %d\n", cmd.Process.Pid)
	fmt.Printf("Log file: %s\n", logFile)

	// Note: PID file will be written by the daemon process itself
	return nil
}

// Stop stops the daemon process
func Stop(pidFile string) error {
	running, pid := IsRunning(pidFile)
	if !running {
		return ErrNotRunning
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process: %w", err)
	}

	// Send SIGTERM for graceful shutdown
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send signal: %w", err)
	}

	fmt.Printf("Sent shutdown signal to daemon (PID %d)\n", pid)

	// PID file will be removed by the daemon during shutdown
	return nil
}

// Status returns the daemon status
func Status(pidFile string) (bool, int) {
	return IsRunning(pidFile)
}
