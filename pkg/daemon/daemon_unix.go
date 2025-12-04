//go:build !windows

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

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

	// Set up process attributes for daemonization (Unix only)
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

// isProcessRunning checks if a process with given PID is running
func isProcessRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// On Unix, FindProcess always succeeds, so we need to send signal 0
	err = process.Signal(syscall.Signal(0))
	return err == nil
}
