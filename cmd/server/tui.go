package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"tunnel/pkg/relay"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Styles
var (
	primaryColor   = lipgloss.Color("#3B82F6") // Blue for Server
	secondaryColor = lipgloss.Color("#FAFAFA")
	subtleColor    = lipgloss.Color("#626262")
	highlightColor = lipgloss.Color("#04B575")
	errorColor     = lipgloss.Color("#FF5555")
	warningColor   = lipgloss.Color("#FFAA00")

	appStyle = lipgloss.NewStyle().
			Margin(1, 2)

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(secondaryColor).
			Background(primaryColor).
			Padding(0, 1).
			MarginBottom(1)

	labelStyle = lipgloss.NewStyle().
			Foreground(subtleColor).
			MarginTop(1)

	statusStyle = lipgloss.NewStyle().
			Foreground(highlightColor).
			Bold(true)

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(subtleColor).
			Padding(0, 1).
			MarginRight(1)

	logBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(subtleColor).
			Padding(0, 1).
			MarginTop(1).
			Width(82) // Wider for logs

	logStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A0A0A0"))
)

// Messages
type statusMsg relay.StatusResponse
type logMsg string
type errMsg error
type tickMsg time.Time

type logStreamConnectedMsg struct {
	scanner *bufio.Scanner
}

type model struct {
	apiPort   int
	status    relay.StatusResponse
	logs      []string
	err       error
	scanner   *bufio.Scanner
	connected bool
}

func initialModel(apiPort int) model {
	return model{
		apiPort:   apiPort,
		status:    relay.StatusResponse{},
		logs:      []string{},
		connected: false,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
		connectLogStream(m.apiPort),
	)
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func getStatus(apiPort int) tea.Cmd {
	return func() tea.Msg {
		client := http.Client{Timeout: 500 * time.Millisecond}
		resp, err := client.Get(fmt.Sprintf("http://localhost:%d/status", apiPort))
		if err != nil {
			return errMsg(err)
		}
		defer resp.Body.Close()

		var status relay.StatusResponse
		if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
			return errMsg(err)
		}
		return statusMsg(status)
	}
}

func connectLogStream(apiPort int) tea.Cmd {
	return func() tea.Msg {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/logs", apiPort))
		if err != nil {
			return errMsg(err)
		}
		// Note: We are not closing body here, it stays open for streaming
		scanner := bufio.NewScanner(resp.Body)
		return logStreamConnectedMsg{scanner: scanner}
	}
}

func readNextLog(scanner *bufio.Scanner) tea.Cmd {
	return func() tea.Msg {
		if scanner.Scan() {
			text := scanner.Text()
			if strings.HasPrefix(text, "data: ") {
				return logMsg(strings.TrimPrefix(text, "data: "))
			}
			return logMsg("") // Skip non-data lines
		}
		if err := scanner.Err(); err != nil {
			return errMsg(err)
		}
		return errMsg(fmt.Errorf("log stream closed"))
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

	case tickMsg:
		return m, tea.Batch(getStatus(m.apiPort), tickCmd())

	case statusMsg:
		m.status = relay.StatusResponse(msg)
		m.err = nil
		m.connected = true

	case logStreamConnectedMsg:
		m.scanner = msg.scanner
		return m, readNextLog(m.scanner)

	case logMsg:
		if string(msg) != "" {
			m.logs = append(m.logs, string(msg))
			if len(m.logs) > 20 {
				m.logs = m.logs[1:]
			}
		}
		if m.scanner != nil {
			return m, readNextLog(m.scanner)
		}

	case errMsg:
		m.err = msg
	}

	return m, nil
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func (m model) View() string {
	s := titleStyle.Render("Tunnel Relay Monitor") + "\n\n"

	if m.err != nil && !m.connected {
		s += lipgloss.NewStyle().Foreground(warningColor).Render("⚠ Server not running or not reachable") + "\n\n"
		s += lipgloss.NewStyle().Foreground(subtleColor).Render(fmt.Sprintf("Trying to connect to http://localhost:%d...\n", m.apiPort))
		s += lipgloss.NewStyle().Foreground(subtleColor).Render("Start the server with: tunnel-server start\n")
		s += "\n" + lipgloss.NewStyle().Foreground(subtleColor).Render("Press 'q' to quit.") + "\n"
		return appStyle.Render(s)
	}

	if m.err != nil {
		s += lipgloss.NewStyle().Foreground(errorColor).Render(fmt.Sprintf("Error: %v", m.err)) + "\n\n"
		s += "Retrying connection...\n"
		return appStyle.Render(s)
	}

	// Box 1: Server Info
	infoContent := fmt.Sprintf("%s %s\n", labelStyle.Render("Public IP:   "), m.status.PublicIP)
	infoContent += fmt.Sprintf("%s %d\n", labelStyle.Render("Control Port:"), m.status.ControlPort)
	infoContent += fmt.Sprintf("%s %d\n", labelStyle.Render("Game Port:   "), m.status.GamePort)

	statusText := "Disconnected"
	statusColor := subtleColor
	if m.status.TunnelConnected {
		statusText = "Connected"
		statusColor = highlightColor
	}
	infoContent += fmt.Sprintf("%s %s", labelStyle.Render("Tunnel:      "), lipgloss.NewStyle().Foreground(statusColor).Bold(true).Render(statusText))

	infoBox := boxStyle.Render(infoContent)

	// Box 2: Stats
	uptime := time.Duration(m.status.UptimeSeconds) * time.Second
	statsContent := fmt.Sprintf("%s %s\n", labelStyle.Render("Uptime:        "), uptime.String())
	statsContent += fmt.Sprintf("%s %d\n", labelStyle.Render("Active Players:"), m.status.ActivePlayers)
	statsContent += fmt.Sprintf("%s %s\n", labelStyle.Render("Traffic:       "), formatBytes(m.status.BytesTransferred))
	statsBox := boxStyle.Render(statsContent)

	// Join Boxes
	row1 := lipgloss.JoinHorizontal(lipgloss.Top, infoBox, statsBox)

	// Logs
	var logContent string
	if len(m.logs) == 0 {
		logContent = logStyle.Render("Waiting for logs...")
	} else {
		for _, l := range m.logs {
			logContent += logStyle.Render(l) + "\n"
		}
	}

	s += row1 + "\n"
	s += logBoxStyle.Render(logContent)

	s += "\n\n" + lipgloss.NewStyle().Foreground(subtleColor).Render("Press 'q' to quit.") + "\n"

	return appStyle.Render(s)
}
