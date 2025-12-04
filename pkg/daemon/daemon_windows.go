//go:build windows

package daemon

import (
	"fmt"
	"os"
	"os/exec"
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

	// On Windows, we don't have Setsid, but the process will still run in background
	// when started without a console

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

	// On Windows, Kill() is the way to terminate a process
	if err := process.Kill(); err != nil {
		return fmt.Errorf("failed to kill process: %w", err)
	}

	// Clean up PID file
	os.Remove(pidFile)

	fmt.Printf("Daemon stopped (PID %d)\n", pid)
	return nil
}

// isProcessRunning checks if a process with given PID is running
func isProcessRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// On Windows, we try to open the process to check if it exists
	// FindProcess always succeeds on Windows, so we check by trying to get exit code
	// A running process cannot have its exit code retrieved
	_, err = process.Wait()
	if err != nil {
		// Process is still running
		return true
	}
	return false
}
