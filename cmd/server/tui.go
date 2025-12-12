package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"tunnel/pkg/relay"

	"github.com/charmbracelet/bubbles/viewport"
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
			Width(14) // Fixed width for alignment

	valueStyle = lipgloss.NewStyle().
			Foreground(secondaryColor)

	statusStyle = lipgloss.NewStyle().
			Foreground(highlightColor).
			Bold(true)

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(subtleColor).
			Padding(0, 1).
			MarginRight(1).
			Width(40).
			Height(7)

	logBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(subtleColor).
			Padding(0, 1).
			MarginTop(1).
			Width(83) // Match the width of two boxes + margin

	logStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A0A0A0"))
	
	helpStyle = lipgloss.NewStyle().
			Foreground(subtleColor).
			MarginTop(1)
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
	viewport  viewport.Model
}

func initialModel(apiPort int) model {
	vp := viewport.New(81, 10)
	vp.SetContent("Waiting for logs...")

	return model{
		apiPort:   apiPort,
		status:    relay.StatusResponse{},
		logs:      []string{},
		connected: false,
		viewport:  vp,
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
			// Keep more logs in memory for scrolling
			if len(m.logs) > 1000 {
				m.logs = m.logs[1:]
			}
			
			// Update viewport content
			var content string
			for _, l := range m.logs {
				content += logStyle.Render(l) + "\n"
			}
			m.viewport.SetContent(content)
			m.viewport.GotoBottom()
		}
		if m.scanner != nil {
			return m, readNextLog(m.scanner)
		}

	case errMsg:
		m.err = msg
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
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
		s += "\n" + helpStyle.Render("Press 'q' to quit.") + "\n"
		return appStyle.Render(s)
	}

	if m.err != nil {
		s += lipgloss.NewStyle().Foreground(errorColor).Render(fmt.Sprintf("Error: %v", m.err)) + "\n\n"
		s += "Retrying connection...\n"
		return appStyle.Render(s)
	}

	// Box 1: Network
	netContent := fmt.Sprintf("%s%s\n", labelStyle.Render("Public IP:"), valueStyle.Render(m.status.PublicIP))
	netContent += fmt.Sprintf("%s%s\n", labelStyle.Render("Control Port:"), valueStyle.Render(fmt.Sprintf("%d", m.status.ControlPort)))
	netContent += fmt.Sprintf("%s%s\n", labelStyle.Render("Game Port:"), valueStyle.Render(fmt.Sprintf("%d", m.status.GamePort)))
	if m.status.BedrockPort > 0 {
		netContent += fmt.Sprintf("%s%s\n", labelStyle.Render("Bedrock Port:"), valueStyle.Render(fmt.Sprintf("%d", m.status.BedrockPort)))
	}
	netBox := boxStyle.Render(netContent)

	// Box 2: Tunnel
	statusText := "Disconnected"
	statusColor := subtleColor
	if m.status.TunnelConnected {
		statusText = "Connected"
		statusColor = highlightColor
	}
	tunContent := fmt.Sprintf("%s%s\n", labelStyle.Render("Status:"), lipgloss.NewStyle().Foreground(statusColor).Bold(true).Render(statusText))
	tunContent += fmt.Sprintf("%s%s\n", labelStyle.Render("Remote:"), valueStyle.Render(m.status.TunnelRemoteAddr))
	tunContent += fmt.Sprintf("%s%s\n", labelStyle.Render("Streams:"), valueStyle.Render(fmt.Sprintf("%d", m.status.NumStreams)))
	tunBox := boxStyle.Render(tunContent)

	// Box 3: Players
	playContent := fmt.Sprintf("%s%s\n", labelStyle.Render("Active:"), valueStyle.Render(fmt.Sprintf("%d", m.status.ActivePlayers)))
	playContent += fmt.Sprintf("%s%s\n", labelStyle.Render("Peak:"), valueStyle.Render(fmt.Sprintf("%d", m.status.PeakPlayers)))
	
	if len(m.status.PlayerList) > 0 {
		playContent += labelStyle.Render("Clients:") + "\n"
		for i, p := range m.status.PlayerList {
			if i >= 2 { // Limit to fit in box
				playContent += valueStyle.Render(fmt.Sprintf(" +%d more...", len(m.status.PlayerList)-2)) + "\n"
				break
			}
			playContent += valueStyle.Render(fmt.Sprintf(" %s", p)) + "\n"
		}
	} else {
		playContent += labelStyle.Render("No players connected") + "\n"
	}
	
	playBox := boxStyle.Render(playContent)

	// Box 4: Traffic
	uptime := time.Duration(m.status.UptimeSeconds) * time.Second
	trafContent := fmt.Sprintf("%s%s\n", labelStyle.Render("Uptime:"), valueStyle.Render(uptime.String()))
	trafContent += fmt.Sprintf("%s%s\n", labelStyle.Render("Total:"), valueStyle.Render(formatBytes(m.status.BytesTransferred)))
	trafContent += fmt.Sprintf("%s%s\n", labelStyle.Render("In (Rx):"), valueStyle.Render(formatBytes(m.status.BytesFromPlayers)))
	trafContent += fmt.Sprintf("%s%s\n", labelStyle.Render("Out (Tx):"), valueStyle.Render(formatBytes(m.status.BytesFromTunnel)))
	trafBox := boxStyle.Render(trafContent)

	// Join Boxes
	row1 := lipgloss.JoinHorizontal(lipgloss.Top, netBox, tunBox)
	row2 := lipgloss.JoinHorizontal(lipgloss.Top, playBox, trafBox)

	s += row1 + "\n" + row2 + "\n"
	s += logBoxStyle.Render(m.viewport.View())

	s += "\n" + helpStyle.Render("Press 'q' to quit, up/down to scroll logs.") + "\n"

	return appStyle.Render(s)
}
