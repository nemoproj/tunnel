package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"tunnel/pkg/relay"

	"github.com/charmbracelet/bubbles/textinput"
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

	// Tab Styles
	activeTabStyle = lipgloss.NewStyle().
			Foreground(secondaryColor).
			Background(primaryColor).
			Padding(0, 1).
			Bold(true)

	inactiveTabStyle = lipgloss.NewStyle().
			Foreground(subtleColor).
			Padding(0, 1)
)

// Messages
type statusMsg relay.StatusResponse
type connectionsMsg []ConnectionInfo
type disconnectedMsg string
type logMsg string
type errMsg error
type tickMsg time.Time

type logStreamConnectedMsg struct {
	scanner *bufio.Scanner
}

type ConnectionInfo struct {
	IP          string    `json:"ip"`
	Nickname    string    `json:"nickname"`
	Type        string    `json:"type"`
	ConnectedAt time.Time `json:"connected_at"`
	BytesIn     int64     `json:"bytes_in"`
	BytesOut    int64     `json:"bytes_out"`
	Latency     string    `json:"latency"`
}

type tab int

const (
	tabDashboard tab = iota
	tabConnections
	tabBlocklist
)

type model struct {
	apiPort     int
	status      relay.StatusResponse
	connections []ConnectionInfo
	blockedIPs  []BlockedInfo
	activeTab   tab
	cursor      int
	logs        []string
	err         error
	scanner     *bufio.Scanner
	connected   bool
	viewport    viewport.Model
	
	// Input for nicknames
	inputMode   bool
	textInput   textinput.Model
	targetIP    string

	// Graph data
	trafficHistory []int64
	lastBytes      int64
}

type BlockedInfo struct {
	IP        string    `json:"ip"`
	BlockedAt time.Time `json:"blocked_at"`
}

type blockedMsg []BlockedInfo
type blockActionMsg string
type nicknameSetMsg string

func initialModel(apiPort int) model {
	vp := viewport.New(81, 10)
	vp.SetContent("Waiting for logs...")

	ti := textinput.New()
	ti.Placeholder = "Enter nickname..."
	ti.CharLimit = 32
	ti.Width = 30

	return model{
		apiPort:     apiPort,
		status:      relay.StatusResponse{},
		connections: []ConnectionInfo{},
		blockedIPs:  []BlockedInfo{},
		activeTab:   tabDashboard,
		cursor:      0,
		logs:        []string{},
		connected:   false,
		viewport:    vp,
		textInput:   ti,
		trafficHistory: make([]int64, 0),
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
		connectLogStream(m.apiPort),
		getConnections(m.apiPort),
		getBlocklist(m.apiPort),
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

func getConnections(apiPort int) tea.Cmd {
	return func() tea.Msg {
		client := http.Client{Timeout: 500 * time.Millisecond}
		resp, err := client.Get(fmt.Sprintf("http://localhost:%d/connections", apiPort))
		if err != nil {
			return errMsg(err)
		}
		defer resp.Body.Close()

		var conns []ConnectionInfo
		if err := json.NewDecoder(resp.Body).Decode(&conns); err != nil {
			return errMsg(err)
		}
		return connectionsMsg(conns)
	}
}

func getBlocklist(apiPort int) tea.Cmd {
	return func() tea.Msg {
		client := http.Client{Timeout: 500 * time.Millisecond}
		resp, err := client.Get(fmt.Sprintf("http://localhost:%d/blocklist", apiPort))
		if err != nil {
			return errMsg(err)
		}
		defer resp.Body.Close()

		var blocked []BlockedInfo
		if err := json.NewDecoder(resp.Body).Decode(&blocked); err != nil {
			return errMsg(err)
		}
		return blockedMsg(blocked)
	}
}

func blockIP(apiPort int, ip string) tea.Cmd {
	return func() tea.Msg {
		client := http.Client{Timeout: 500 * time.Millisecond}
		req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://localhost:%d/blocklist?ip=%s", apiPort, ip), nil)
		if err != nil {
			return errMsg(err)
		}
		
		resp, err := client.Do(req)
		if err != nil {
			return errMsg(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return errMsg(fmt.Errorf("failed to block: %s", resp.Status))
		}
		return blockActionMsg(ip)
	}
}

func unblockIP(apiPort int, ip string) tea.Cmd {
	return func() tea.Msg {
		client := http.Client{Timeout: 500 * time.Millisecond}
		req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("http://localhost:%d/blocklist?ip=%s", apiPort, ip), nil)
		if err != nil {
			return errMsg(err)
		}
		
		resp, err := client.Do(req)
		if err != nil {
			return errMsg(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return errMsg(fmt.Errorf("failed to unblock: %s", resp.Status))
		}
		return blockActionMsg(ip)
	}
}

func setNickname(apiPort int, ip, name string) tea.Cmd {
	return func() tea.Msg {
		client := http.Client{Timeout: 500 * time.Millisecond}
		// URL encode the name
		req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://localhost:%d/nicknames?ip=%s&name=%s", apiPort, ip, name), nil)
		if err != nil {
			return errMsg(err)
		}
		
		resp, err := client.Do(req)
		if err != nil {
			return errMsg(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return errMsg(fmt.Errorf("failed to set nickname: %s", resp.Status))
		}
		return nicknameSetMsg(ip)
	}
}

func disconnectPlayer(apiPort int, ip string) tea.Cmd {
	return func() tea.Msg {
		client := http.Client{Timeout: 500 * time.Millisecond}
		req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("http://localhost:%d/connections?ip=%s", apiPort, ip), nil)
		if err != nil {
			return errMsg(err)
		}
		
		resp, err := client.Do(req)
		if err != nil {
			return errMsg(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return errMsg(fmt.Errorf("failed to disconnect: %s", resp.Status))
		}
		return disconnectedMsg(ip)
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
	// Handle Input Mode
	if m.inputMode {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "enter":
				m.inputMode = false
				name := m.textInput.Value()
				m.textInput.Reset()
				return m, setNickname(m.apiPort, m.targetIP, name)
			case "esc":
				m.inputMode = false
				m.textInput.Reset()
				return m, nil
			}
		}
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		if msg.String() == "tab" || msg.String() == "shift+tab" {
			if m.activeTab == tabDashboard {
				m.activeTab = tabConnections
			} else if m.activeTab == tabConnections {
				m.activeTab = tabBlocklist
			} else {
				m.activeTab = tabDashboard
			}
			m.cursor = 0 // Reset cursor on tab switch
			return m, nil
		}

		if m.activeTab == tabConnections {
			switch msg.String() {
			case "up", "k":
				if m.cursor > 0 {
					m.cursor--
				}
			case "down", "j":
				if m.cursor < len(m.connections)-1 {
					m.cursor++
				}
			case "x":
				if len(m.connections) > 0 && m.cursor < len(m.connections) {
					addr := m.connections[m.cursor].IP
					// disconnectPlayer expects the full address (IP:Port)
					return m, disconnectPlayer(m.apiPort, addr)
				}
			case "b":
				if len(m.connections) > 0 && m.cursor < len(m.connections) {
					addr := m.connections[m.cursor].IP
					ip, _, _ := net.SplitHostPort(addr)
					if ip == "" {
						ip = addr // Fallback if no port
					}
					return m, blockIP(m.apiPort, ip)
				}
			case "n":
				if len(m.connections) > 0 && m.cursor < len(m.connections) {
					addr := m.connections[m.cursor].IP
					ip, _, _ := net.SplitHostPort(addr)
					if ip == "" {
						ip = addr // Fallback if no port
					}
					m.targetIP = ip
					m.inputMode = true
					m.textInput.Focus()
					// Pre-fill with existing nickname if any
					m.textInput.SetValue(m.connections[m.cursor].Nickname)
					return m, nil
				}
			}
		} else if m.activeTab == tabBlocklist {
			switch msg.String() {
			case "up", "k":
				if m.cursor > 0 {
					m.cursor--
				}
			case "down", "j":
				if m.cursor < len(m.blockedIPs)-1 {
					m.cursor++
				}
			case "u", "x":
				if len(m.blockedIPs) > 0 && m.cursor < len(m.blockedIPs) {
					ip := m.blockedIPs[m.cursor].IP
					return m, unblockIP(m.apiPort, ip)
				}
			}
		} else {
			// Dashboard specific keys (scrolling logs)
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}

	case tickMsg:
		return m, tea.Batch(
			getStatus(m.apiPort),
			getConnections(m.apiPort),
			getBlocklist(m.apiPort),
			tickCmd(),
		)

	case statusMsg:
		// Update traffic history
		currentBytes := int64(msg.BytesTransferred)
		if m.lastBytes == 0 {
			// First data point - just initialize, don't calculate delta
			m.lastBytes = currentBytes
		} else {
			delta := currentBytes - m.lastBytes
			if delta < 0 {
				delta = 0
			}
			m.trafficHistory = append(m.trafficHistory, delta)
			// Keep last 80 points (approx width of graph)
			if len(m.trafficHistory) > 80 {
				m.trafficHistory = m.trafficHistory[1:]
			}
			m.lastBytes = currentBytes
		}

		m.status = relay.StatusResponse(msg)
		m.err = nil
		m.connected = true

	case connectionsMsg:
		m.connections = []ConnectionInfo(msg)
		// Adjust cursor if list shrank
		if m.activeTab == tabConnections && m.cursor >= len(m.connections) && len(m.connections) > 0 {
			m.cursor = len(m.connections) - 1
		}

	case blockedMsg:
		m.blockedIPs = []BlockedInfo(msg)
		// Adjust cursor if list shrank
		if m.activeTab == tabBlocklist && m.cursor >= len(m.blockedIPs) && len(m.blockedIPs) > 0 {
			m.cursor = len(m.blockedIPs) - 1
		}

	case blockActionMsg:
		// Refresh lists immediately
		return m, tea.Batch(
			getConnections(m.apiPort),
			getBlocklist(m.apiPort),
		)

	case nicknameSetMsg:
		return m, getConnections(m.apiPort)

	case disconnectedMsg:
		// Refresh connections immediately
		return m, getConnections(m.apiPort)

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
			if m.activeTab == tabDashboard {
				m.viewport.GotoBottom()
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
		s += "\n" + helpStyle.Render("Press 'q' to quit.") + "\n"
		return appStyle.Render(s)
	}

	if m.err != nil {
		s += lipgloss.NewStyle().Foreground(errorColor).Render(fmt.Sprintf("Error: %v", m.err)) + "\n\n"
		s += "Retrying connection...\n"
		return appStyle.Render(s)
	}

	// Tabs
	var dashboardTab, connectionsTab, blocklistTab string
	if m.activeTab == tabDashboard {
		dashboardTab = activeTabStyle.Render("Dashboard")
		connectionsTab = inactiveTabStyle.Render("Connections")
		blocklistTab = inactiveTabStyle.Render("Blocklist")
	} else if m.activeTab == tabConnections {
		dashboardTab = inactiveTabStyle.Render("Dashboard")
		connectionsTab = activeTabStyle.Render("Connections")
		blocklistTab = inactiveTabStyle.Render("Blocklist")
	} else {
		dashboardTab = inactiveTabStyle.Render("Dashboard")
		connectionsTab = inactiveTabStyle.Render("Connections")
		blocklistTab = activeTabStyle.Render("Blocklist")
	}
	s += lipgloss.JoinHorizontal(lipgloss.Top, dashboardTab, connectionsTab, blocklistTab) + "\n\n"

	if m.activeTab == tabDashboard {
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

		// Traffic Graph
		if len(m.trafficHistory) > 0 {
			graphContent := "Traffic (Bytes/sec)\n" + renderGraph(m.trafficHistory, 79, 4)
			s += logBoxStyle.Render(graphContent) + "\n"
		}

		s += logBoxStyle.Render(m.viewport.View())

		s += "\n" + helpStyle.Render("Tab: Switch View • q: Quit • ↑/↓: Scroll Logs") + "\n"
	} else if m.activeTab == tabConnections {
		// Connections View
		s += lipgloss.NewStyle().Bold(true).Render("Active Connections") + "\n\n"

		if len(m.connections) == 0 {
			s += lipgloss.NewStyle().Foreground(subtleColor).Render("No active connections.") + "\n"
		} else {
			// Header
			s += fmt.Sprintf("%-25s %-8s %-20s %-12s %-12s\n", "IP Address (Nickname)", "Type", "Connected At", "In (Rx)", "Out (Tx)")
			s += lipgloss.NewStyle().Foreground(subtleColor).Render(strings.Repeat("-", 80)) + "\n"

			for i, conn := range m.connections {
				cursor := "  "
				style := lipgloss.NewStyle()
				if m.cursor == i {
					cursor = "> "
					style = style.Foreground(highlightColor).Bold(true)
				}

				displayName := conn.IP
				if conn.Nickname != "" {
					displayName = fmt.Sprintf("%s (%s)", conn.IP, conn.Nickname)
				}

				line := fmt.Sprintf("%-25s %-8s %-20s %-12s %-12s", 
					displayName, 
					strings.ToUpper(conn.Type), 
					conn.ConnectedAt.Format(time.RFC822),
					formatBytes(conn.BytesIn),
					formatBytes(conn.BytesOut),
				)
				s += style.Render(cursor + line) + "\n"
			}
		}

		s += "\n" + helpStyle.Render("Tab: Switch View • q: Quit • ↑/↓: Select • x: Disconnect • b: Block • n: Nickname") + "\n"
	} else {
		// Blocklist View
		s += lipgloss.NewStyle().Bold(true).Render("Blocked IPs") + "\n\n"

		if len(m.blockedIPs) == 0 {
			s += lipgloss.NewStyle().Foreground(subtleColor).Render("No blocked IPs.") + "\n"
		} else {
			// Header
			s += fmt.Sprintf("%-25s %-25s\n", "IP Address", "Blocked At")
			s += lipgloss.NewStyle().Foreground(subtleColor).Render(strings.Repeat("-", 50)) + "\n"

			for i, blocked := range m.blockedIPs {
				cursor := "  "
				style := lipgloss.NewStyle()
				if m.cursor == i {
					cursor = "> "
					style = style.Foreground(errorColor).Bold(true)
				}

				line := fmt.Sprintf("%-25s %-25s", blocked.IP, blocked.BlockedAt.Format(time.RFC822))
				s += style.Render(cursor + line) + "\n"
			}
		}

		s += "\n" + helpStyle.Render("Tab: Switch View • q: Quit • ↑/↓: Select • u/x: Unblock") + "\n"
	}

	// Input Overlay
	if m.inputMode {
		// Simple overlay
		inputBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(primaryColor).
			Padding(1).
			Width(40).
			Render(
				lipgloss.NewStyle().Bold(true).Render("Set Nickname for "+m.targetIP) + "\n\n" +
				m.textInput.View() + "\n\n" +
				lipgloss.NewStyle().Foreground(subtleColor).Render("Enter: Save • Esc: Cancel"),
			)
		
		// Center the box (hacky centering)
		// lines := strings.Split(s, "\n")
		// height := len(lines)
		// overlayLines := strings.Split(inputBox, "\n")
		// overlayHeight := len(overlayLines)
		
		// Just append it at the bottom for now as true centering requires more complex layout logic
		// or replacing the whole view.
		// A better way is to just return the input box if in input mode, hiding the rest, 
		// or using a proper overlay if bubbletea supported z-index (it doesn't really).
		// Let's just replace the view content with the input box for simplicity and clarity.
		return lipgloss.Place(80, 20, lipgloss.Center, lipgloss.Center, inputBox)
	}

	return appStyle.Render(s)
}

func renderGraph(data []int64, width, height int) string {
	if len(data) == 0 {
		return ""
	}
	max := int64(0)
	for _, v := range data {
		if v > max {
			max = v
		}
	}
	if max == 0 {
		max = 1
	}

	bars := []rune{'\u2581', '\u2582', '\u2583', '\u2584', '\u2585', '\u2586', '\u2587', '\u2588'}
	lines := make([][]rune, height)
	for i := range lines {
		lines[i] = make([]rune, width)
		for j := range lines[i] {
			lines[i][j] = ' '
		}
	}

	// We want to plot the last `width` points
	start := 0
	if len(data) > width {
		start = len(data) - width
	}

	for x, val := range data[start:] {
		if x >= width {
			break
		}

		totalLevels := int(float64(val) / float64(max) * float64(height*8))
		if val > 0 && totalLevels == 0 {
			totalLevels = 1
		}

		fullBlocks := totalLevels / 8
		remainder := totalLevels % 8

		for y := 0; y < height; y++ {
			row := height - 1 - y
			if y < fullBlocks {
				lines[row][x] = '█'
			} else if y == fullBlocks && remainder > 0 {
				lines[row][x] = bars[remainder-1]
			} else {
				lines[row][x] = ' '
			}
		}
	}

	var sb strings.Builder
	for _, line := range lines {
		sb.WriteString(string(line) + "\n")
	}
	return sb.String()
}
