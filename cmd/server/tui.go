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

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ============================================================================
// Types and Data Models
// ============================================================================

type ViewMode int

const (
	ViewDashboard ViewMode = iota
	ViewConnections
	ViewBlocklist
)

type ConnectionInfo struct {
	IP          string    `json:"ip"`
	Nickname    string    `json:"nickname"`
	Type        string    `json:"type"`
	ConnectedAt time.Time `json:"connected_at"`
	BytesIn     int64     `json:"bytes_in"`
	BytesOut    int64     `json:"bytes_out"`
	Latency     string    `json:"latency"`
}

type BlockedInfo struct {
	IP        string    `json:"ip"`
	BlockedAt time.Time `json:"blocked_at"`
}

type Model struct {
	// Configuration
	apiPort  int
	httpAddr string

	// State
	status      relay.StatusResponse
	connections []ConnectionInfo
	blocked     []BlockedInfo
	logs        []string
	
	// UI State
	view      ViewMode
	err       error
	connected bool
	ready     bool
	showHelp  bool
	width     int
	height    int

	// Input handling
	inputMode   bool
	inputTarget string
	
	// Components
	spinner   spinner.Model
	help      help.Model
	viewport  viewport.Model
	textInput textinput.Model
	connTable table.Model
	blockTable table.Model
	keys      KeyMap
	
	// Log streaming
	logScanner *bufio.Scanner
}

// ============================================================================
// Messages
// ============================================================================

type (
	tickMsg              time.Time
	statusMsg            relay.StatusResponse
	connectionsMsg       []ConnectionInfo
	blockedMsg           []BlockedInfo
	logMsg               string
	errMsg               error
	actionCompleteMsg    string
	logStreamConnectedMsg struct{ scanner *bufio.Scanner }
)

// ============================================================================
// Key Bindings
// ============================================================================

type KeyMap struct {
	Up         key.Binding
	Down       key.Binding
	Tab        key.Binding
	Quit       key.Binding
	Help       key.Binding
	Disconnect key.Binding
	Block      key.Binding
	Unblock    key.Binding
	Nickname   key.Binding
	Enter      key.Binding
	Escape     key.Binding
}

func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Tab, k.Help, k.Quit}
}

func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Tab},
		{k.Disconnect, k.Block, k.Nickname},
		{k.Help, k.Quit},
	}
}

var DefaultKeys = KeyMap{
	Up:         key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:       key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Tab:        key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch view")),
	Quit:       key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Help:       key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Disconnect: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "disconnect")),
	Block:      key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "block")),
	Unblock:    key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "unblock")),
	Nickname:   key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "nickname")),
	Enter:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
	Escape:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
}

// ============================================================================
// Styles
// ============================================================================

var (
	colorPrimary   = lipgloss.Color("#3B82F6")
	colorSecondary = lipgloss.Color("#FAFAFA")
	colorSubtle    = lipgloss.Color("#626262")
	colorSuccess   = lipgloss.Color("#04B575")
	colorError     = lipgloss.Color("#FF5555")
	colorWarning   = lipgloss.Color("#FFAA00")
)

var (
	styleApp = lipgloss.NewStyle().Margin(1, 2)

	styleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorSecondary).
			Background(colorPrimary).
			Padding(0, 1).
			MarginBottom(1)

	styleLabel = lipgloss.NewStyle().Foreground(colorSubtle).Width(14)
	styleValue = lipgloss.NewStyle().Foreground(colorSecondary)
	styleHelp  = lipgloss.NewStyle().Foreground(colorSubtle).MarginTop(1)

	styleTabActive   = lipgloss.NewStyle().Foreground(colorSecondary).Background(colorPrimary).Padding(0, 1).Bold(true)
	styleTabInactive = lipgloss.NewStyle().Foreground(colorSubtle).Padding(0, 1)

	styleTableHeader   = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary).BorderStyle(lipgloss.NormalBorder()).BorderForeground(colorSubtle).BorderBottom(true)
	styleTableSelected = lipgloss.NewStyle().Bold(true).Foreground(colorSuccess)
)

// ============================================================================
// Initialization
// ============================================================================

func NewModel(apiPort int) Model {
	// Spinner
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(colorPrimary)

	// Viewport for logs
	vp := viewport.New(80, 10)
	vp.SetContent("Waiting for logs...")

	// Text input for nicknames
	ti := textinput.New()
	ti.Placeholder = "Enter nickname..."
	ti.CharLimit = 32
	ti.Width = 30

	// Tables
	connTable := newConnectionsTable()
	blockTable := newBlocklistTable()

	return Model{
		apiPort:    apiPort,
		httpAddr:   fmt.Sprintf("http://localhost:%d", apiPort),
		spinner:    s,
		viewport:   vp,
		textInput:  ti,
		connTable:  connTable,
		blockTable: blockTable,
		help:       help.New(),
		keys:       DefaultKeys,
		width:      80,
		height:     24,
	}
}

func newConnectionsTable() table.Model {
	columns := []table.Column{
		{Title: "IP Address", Width: 20},
		{Title: "Nickname", Width: 15},
		{Title: "Type", Width: 8},
		{Title: "Connected", Width: 20},
		{Title: "Rx", Width: 12},
		{Title: "Tx", Width: 12},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(10),
	)

	styles := table.DefaultStyles()
	styles.Header = styleTableHeader
	styles.Selected = styleTableSelected
	t.SetStyles(styles)

	return t
}

func newBlocklistTable() table.Model {
	columns := []table.Column{
		{Title: "IP Address", Width: 30},
		{Title: "Blocked At", Width: 30},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(10),
	)

	styles := table.DefaultStyles()
	styles.Header = styleTableHeader
	styles.Selected = styleTableSelected
	t.SetStyles(styles)

	return t
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
		fetchStatus(m.httpAddr),
		fetchConnections(m.httpAddr),
		fetchBlocklist(m.httpAddr),
		connectLogStream(m.httpAddr),
		m.spinner.Tick,
	)
}

// ============================================================================
// Commands (API calls)
// ============================================================================

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func fetchStatus(addr string) tea.Cmd {
	return func() tea.Msg {
		client := http.Client{Timeout: 500 * time.Millisecond}
		resp, err := client.Get(addr + "/status")
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

func fetchConnections(addr string) tea.Cmd {
	return func() tea.Msg {
		client := http.Client{Timeout: 500 * time.Millisecond}
		resp, err := client.Get(addr + "/connections")
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

func fetchBlocklist(addr string) tea.Cmd {
	return func() tea.Msg {
		client := http.Client{Timeout: 500 * time.Millisecond}
		resp, err := client.Get(addr + "/blocklist")
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

func connectLogStream(addr string) tea.Cmd {
	return func() tea.Msg {
		resp, err := http.Get(addr + "/logs")
		if err != nil {
			return errMsg(err)
		}
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
			return logMsg("")
		}
		if err := scanner.Err(); err != nil {
			return errMsg(err)
		}
		return errMsg(fmt.Errorf("log stream closed"))
	}
}

func disconnectPlayer(addr, ip string) tea.Cmd {
	return func() tea.Msg {
		client := http.Client{Timeout: 500 * time.Millisecond}
		req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/connections?ip=%s", addr, ip), nil)
		if err != nil {
			return errMsg(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return errMsg(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return errMsg(fmt.Errorf("disconnect failed: %s", resp.Status))
		}
		return actionCompleteMsg("disconnected")
	}
}

func blockIP(addr, ip string) tea.Cmd {
	return func() tea.Msg {
		client := http.Client{Timeout: 500 * time.Millisecond}
		req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/blocklist?ip=%s", addr, ip), nil)
		if err != nil {
			return errMsg(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return errMsg(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return errMsg(fmt.Errorf("block failed: %s", resp.Status))
		}
		return actionCompleteMsg("blocked")
	}
}

func unblockIP(addr, ip string) tea.Cmd {
	return func() tea.Msg {
		client := http.Client{Timeout: 500 * time.Millisecond}
		req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/blocklist?ip=%s", addr, ip), nil)
		if err != nil {
			return errMsg(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return errMsg(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return errMsg(fmt.Errorf("unblock failed: %s", resp.Status))
		}
		return actionCompleteMsg("unblocked")
	}
}

func setNickname(addr, ip, name string) tea.Cmd {
	return func() tea.Msg {
		client := http.Client{Timeout: 500 * time.Millisecond}
		req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/nicknames?ip=%s&name=%s", addr, ip, name), nil)
		if err != nil {
			return errMsg(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return errMsg(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return errMsg(fmt.Errorf("nickname failed: %s", resp.Status))
		}
		return actionCompleteMsg("nickname_set")
	}
}

// ============================================================================
// Update
// ============================================================================

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Input mode takes priority
	if m.inputMode {
		return m.handleInputMode(msg)
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyPress(msg)
	case tea.WindowSizeMsg:
		return m.handleResize(msg)
	case tickMsg:
		return m.handleTick()
	case statusMsg:
		return m.handleStatus(msg)
	case connectionsMsg:
		return m.handleConnections(msg)
	case blockedMsg:
		return m.handleBlocklist(msg)
	case logStreamConnectedMsg:
		return m.handleLogStreamConnected(msg)
	case logMsg:
		return m.handleLog(msg)
	case actionCompleteMsg:
		return m.handleActionComplete()
	case errMsg:
		return m.handleError(msg)
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m Model) handleInputMode(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Enter):
			m.inputMode = false
			name := m.textInput.Value()
			m.textInput.Reset()
			return m, setNickname(m.httpAddr, m.inputTarget, name)
		case key.Matches(msg, m.keys.Escape):
			m.inputMode = false
			m.textInput.Reset()
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m Model) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Quit) {
		return m, tea.Quit
	}
	if key.Matches(msg, m.keys.Help) {
		m.showHelp = !m.showHelp
		return m, nil
	}
	if key.Matches(msg, m.keys.Tab) {
		m.view = (m.view + 1) % 3
		return m, nil
	}

	switch m.view {
	case ViewDashboard:
		return m.handleDashboardKeys(msg)
	case ViewConnections:
		return m.handleConnectionsKeys(msg)
	case ViewBlocklist:
		return m.handleBlocklistKeys(msg)
	}

	return m, nil
}

func (m Model) handleDashboardKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m Model) handleConnectionsKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Up):
		m.connTable.MoveUp(1)
	case key.Matches(msg, m.keys.Down):
		m.connTable.MoveDown(1)
	case key.Matches(msg, m.keys.Disconnect):
		if ip := m.getSelectedConnectionIP(); ip != "" {
			return m, disconnectPlayer(m.httpAddr, ip)
		}
	case key.Matches(msg, m.keys.Block):
		if ip := m.getSelectedConnectionIP(); ip != "" {
			return m, blockIP(m.httpAddr, extractIP(ip))
		}
	case key.Matches(msg, m.keys.Nickname):
		if ip := m.getSelectedConnectionIP(); ip != "" {
			cursor := m.connTable.Cursor()
			m.inputTarget = extractIP(ip)
			m.inputMode = true
			m.textInput.Focus()
			if cursor < len(m.connections) {
				m.textInput.SetValue(m.connections[cursor].Nickname)
			}
			return m, nil
		}
	}
	return m, nil
}

func (m Model) handleBlocklistKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Up):
		m.blockTable.MoveUp(1)
	case key.Matches(msg, m.keys.Down):
		m.blockTable.MoveDown(1)
	case key.Matches(msg, m.keys.Unblock), key.Matches(msg, m.keys.Disconnect):
		if ip := m.getSelectedBlockedIP(); ip != "" {
			return m, unblockIP(m.httpAddr, ip)
		}
	}
	return m, nil
}

func (m Model) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	m.updateLayout()
	m.ready = true
	return m, nil
}

func (m Model) handleTick() (tea.Model, tea.Cmd) {
	return m, tea.Batch(
		fetchStatus(m.httpAddr),
		fetchConnections(m.httpAddr),
		fetchBlocklist(m.httpAddr),
		tickCmd(),
	)
}

func (m Model) handleStatus(msg statusMsg) (tea.Model, tea.Cmd) {
	m.status = relay.StatusResponse(msg)
	m.err = nil
	m.connected = true
	return m, nil
}

func (m Model) handleConnections(msg connectionsMsg) (tea.Model, tea.Cmd) {
	m.connections = []ConnectionInfo(msg)
	m.updateConnectionsTable()
	return m, nil
}

func (m Model) handleBlocklist(msg blockedMsg) (tea.Model, tea.Cmd) {
	m.blocked = []BlockedInfo(msg)
	m.updateBlocklistTable()
	return m, nil
}

func (m Model) handleLogStreamConnected(msg logStreamConnectedMsg) (tea.Model, tea.Cmd) {
	m.logScanner = msg.scanner
	return m, readNextLog(m.logScanner)
}

func (m Model) handleLog(msg logMsg) (tea.Model, tea.Cmd) {
	if string(msg) != "" {
		m.logs = append(m.logs, string(msg))
		if len(m.logs) > 1000 {
			m.logs = m.logs[1:]
		}
		m.updateLogViewport()
	}
	if m.logScanner != nil {
		return m, readNextLog(m.logScanner)
	}
	return m, nil
}

func (m Model) handleActionComplete() (tea.Model, tea.Cmd) {
	return m, tea.Batch(
		fetchConnections(m.httpAddr),
		fetchBlocklist(m.httpAddr),
	)
}

func (m Model) handleError(msg errMsg) (tea.Model, tea.Cmd) {
	m.err = error(msg)
	return m, nil
}

// ============================================================================
// View
// ============================================================================

func (m Model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	if m.inputMode {
		return m.renderInputOverlay()
	}

	s := styleTitle.Render("Tunnel Relay Monitor") + "\n\n"

	if m.err != nil && !m.connected {
		return styleApp.Render(s + m.renderConnectionError())
	}

	if m.err != nil {
		s += lipgloss.NewStyle().Foreground(colorError).Render(fmt.Sprintf("Error: %v", m.err)) + "\n\n"
	}

	s += m.renderTabs() + "\n\n"

	switch m.view {
	case ViewDashboard:
		s += m.renderDashboard()
	case ViewConnections:
		s += m.renderConnections()
	case ViewBlocklist:
		s += m.renderBlocklist()
	}

	s += "\n" + m.renderHelpBar()

	return styleApp.Render(s)
}

func (m Model) renderConnectionError() string {
	s := lipgloss.NewStyle().Foreground(colorWarning).Render("Server not running or not reachable") + "\n\n"
	s += m.spinner.View() + " " + lipgloss.NewStyle().Foreground(colorSubtle).Render(fmt.Sprintf("Connecting to %s...\n", m.httpAddr))
	s += lipgloss.NewStyle().Foreground(colorSubtle).Render("Start the server with: tunnel-server start\n")
	s += "\n" + styleHelp.Render("Press 'q' to quit.")
	return s
}

func (m Model) renderTabs() string {
	tabs := []string{}
	for i := 0; i < 3; i++ {
		var name string
		switch ViewMode(i) {
		case ViewDashboard:
			name = "Dashboard"
		case ViewConnections:
			name = "Connections"
		case ViewBlocklist:
			name = "Blocklist"
		}
		
		if ViewMode(i) == m.view {
			tabs = append(tabs, styleTabActive.Render(name))
		} else {
			tabs = append(tabs, styleTabInactive.Render(name))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
}

func (m Model) renderDashboard() string {
	contentWidth := m.width - 6
	
	// Calculate dynamic box dimensions
	var boxWidth, boxHeight int
	var numColumns int
	
	if contentWidth >= 120 {
		numColumns = 4
		boxWidth = (contentWidth - 3) / 4
	} else if contentWidth >= 90 {
		numColumns = 3
		boxWidth = (contentWidth - 2) / 3
	} else if contentWidth >= 60 {
		numColumns = 2
		boxWidth = (contentWidth - 1) / 2
	} else {
		numColumns = 1
		boxWidth = contentWidth
	}
	
	boxHeight = 8
	if m.height < 30 {
		boxHeight = 6
	}

	// Network Info Box
	netContent := m.renderInfoRow("Public IP:", m.status.PublicIP)
	netContent += m.renderInfoRow("Control Port:", fmt.Sprintf("%d", m.status.ControlPort))
	netContent += m.renderInfoRow("Game Port:", fmt.Sprintf("%d", m.status.GamePort))
	if m.status.BedrockPort > 0 {
		netContent += m.renderInfoRow("Bedrock Port:", fmt.Sprintf("%d", m.status.BedrockPort))
	}
	netBox := m.renderDynamicBox("NETWORK", netContent, boxWidth, boxHeight, colorPrimary)

	// Tunnel Status Box
	statusText := "Disconnected"
	statusColor := colorError
	statusIcon := "●"
	if m.status.TunnelConnected {
		statusText = "Connected"
		statusColor = colorSuccess
		statusIcon = "●"
	}
	tunContent := styleLabel.Render("Status:") + " " + 
		lipgloss.NewStyle().Foreground(statusColor).Bold(true).Render(statusIcon+" "+statusText) + "\n"
	tunContent += m.renderInfoRow("Remote:", m.status.TunnelRemoteAddr)
	tunContent += m.renderInfoRow("Streams:", fmt.Sprintf("%d", m.status.NumStreams))
	
	uptime := time.Duration(m.status.UptimeSeconds) * time.Second
	tunContent += m.renderInfoRow("Uptime:", uptime.String())
	tunBox := m.renderDynamicBox("TUNNEL", tunContent, boxWidth, boxHeight, colorSuccess)

	// Players Info Box
	playerCount := len(m.status.PlayerList)
	playContent := m.renderInfoRow("Active/Peak:", fmt.Sprintf("%d / %d", m.status.ActivePlayers, m.status.PeakPlayers))
	
	if playerCount > 0 {
		maxDisplay := boxHeight - 4
		if maxDisplay < 1 {
			maxDisplay = 1
		}
		
		playContent += styleLabel.Render("Connected:") + "\n"
		for i, p := range m.status.PlayerList {
			if i >= maxDisplay {
				playContent += lipgloss.NewStyle().Foreground(colorSubtle).Render(
					fmt.Sprintf("  +%d more...", playerCount-maxDisplay)) + "\n"
				break
			}
			playContent += lipgloss.NewStyle().Foreground(colorSecondary).Render(fmt.Sprintf("  %s", p)) + "\n"
		}
	} else {
		playContent += "\n" + lipgloss.NewStyle().Foreground(colorSubtle).Italic(true).Render("No active players")
	}
	playBox := m.renderDynamicBox("PLAYERS", playContent, boxWidth, boxHeight, colorWarning)

	// Traffic Info Box
	trafContent := m.renderInfoRow("Total:", formatBytes(m.status.BytesTransferred))
	trafContent += m.renderInfoRow("In (Rx):", formatBytes(m.status.BytesFromPlayers))
	trafContent += m.renderInfoRow("Out (Tx):", formatBytes(m.status.BytesFromTunnel))
	
	// Calculate rate if possible (approximate)
	if m.status.UptimeSeconds > 0 {
		ratePerSec := m.status.BytesTransferred / m.status.UptimeSeconds
		trafContent += m.renderInfoRow("Avg Rate:", formatBytes(ratePerSec)+"/s")
	}
	trafBox := m.renderDynamicBox("TRAFFIC", trafContent, boxWidth, boxHeight, colorPrimary)

	// Layout boxes based on number of columns
	var layout string
	boxes := []string{netBox, tunBox, playBox, trafBox}
	
	switch numColumns {
	case 4:
		layout = lipgloss.JoinHorizontal(lipgloss.Top, boxes...)
	case 3:
		row1 := lipgloss.JoinHorizontal(lipgloss.Top, boxes[0], boxes[1], boxes[2])
		layout = row1 + "\n" + boxes[3]
	case 2:
		row1 := lipgloss.JoinHorizontal(lipgloss.Top, boxes[0], boxes[1])
		row2 := lipgloss.JoinHorizontal(lipgloss.Top, boxes[2], boxes[3])
		layout = row1 + "\n" + row2
	case 1:
		layout = boxes[0] + "\n" + boxes[1] + "\n" + boxes[2] + "\n" + boxes[3]
	}

	// Logs viewport - dynamically sized
	logViewportHeight := m.height - boxHeight*((4+numColumns-1)/numColumns) - 12
	if logViewportHeight < 5 {
		logViewportHeight = 5
	}
	
	logHeader := lipgloss.NewStyle().
		Bold(true).
		Foreground(colorPrimary).
		MarginTop(1).
		Render("LOGS")
	
	logBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorSubtle).
		Padding(0, 1).
		MarginTop(0).
		Width(contentWidth).
		Height(logViewportHeight).
		Render(m.viewport.View())

	return layout + "\n" + logHeader + "\n" + logBox
}

func (m Model) renderConnections() string {
	s := lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("Active Connections (%d)", len(m.connections))) + "\n\n"
	if len(m.connections) == 0 {
		s += lipgloss.NewStyle().Foreground(colorSubtle).Render("No active connections.")
	} else {
		s += m.connTable.View()
	}
	return s
}

func (m Model) renderBlocklist() string {
	s := lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("Blocked IPs (%d)", len(m.blocked))) + "\n\n"
	if len(m.blocked) == 0 {
		s += lipgloss.NewStyle().Foreground(colorSubtle).Render("No blocked IPs.")
	} else {
		s += m.blockTable.View()
	}
	return s
}

func (m Model) renderHelpBar() string {
	if m.showHelp {
		return m.help.View(m.keys)
	}

	switch m.view {
	case ViewDashboard:
		return styleHelp.Render("Tab: Switch | ?: Help | q: Quit | ↑/↓: Scroll")
	case ViewConnections:
		return styleHelp.Render("Tab: Switch | ?: Help | q: Quit | x: Disconnect | b: Block | n: Nickname")
	case ViewBlocklist:
		return styleHelp.Render("Tab: Switch | ?: Help | q: Quit | u/x: Unblock")
	}
	return ""
}

func (m Model) renderInputOverlay() string {
	content := lipgloss.NewStyle().Bold(true).Render("Set Nickname for "+m.inputTarget) + "\n\n"
	content += m.textInput.View() + "\n\n"
	content += lipgloss.NewStyle().Foreground(colorSubtle).Render("Enter: Save | Esc: Cancel")

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorPrimary).
		Padding(1).
		Width(40).
		Render(content)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) renderInfoRow(label, value string) string {
	return fmt.Sprintf("%s %s\n", 
		lipgloss.NewStyle().Foreground(colorSubtle).Width(12).Render(label), 
		styleValue.Render(value))
}

func (m Model) renderBox(content string, width, height int) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorSubtle).
		Padding(0, 1).
		MarginRight(1).
		Width(width).
		Height(height).
		Render(content)
}

func (m Model) renderDynamicBox(title, content string, width, height int, accentColor lipgloss.Color) string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(accentColor).
		Width(width - 4)
	
	boxContent := titleStyle.Render(title) + "\n" + content
	
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accentColor).
		Padding(0, 1).
		MarginRight(1).
		Width(width).
		Height(height).
		Render(boxContent)
}

// ============================================================================
// Helpers
// ============================================================================

func (m *Model) updateLayout() {
	contentWidth := m.width - 6
	contentHeight := m.height - 8

	// Viewport
	viewportHeight := contentHeight - 20
	if viewportHeight < 5 {
		viewportHeight = 5
	}
	m.viewport.Width = contentWidth
	m.viewport.Height = viewportHeight

	// Tables
	tableHeight := contentHeight - 5
	if tableHeight < 5 {
		tableHeight = 5
	}
	m.connTable.SetHeight(tableHeight)
	m.blockTable.SetHeight(tableHeight)

	// Table columns
	m.updateTableColumns()
}

func (m *Model) updateTableColumns() {
	w := m.width - 6

	// Connections table
	connCols := []table.Column{
		{Title: "IP Address", Width: w * 20 / 100},
		{Title: "Nickname", Width: w * 15 / 100},
		{Title: "Type", Width: w * 10 / 100},
		{Title: "Connected", Width: w * 20 / 100},
		{Title: "Rx", Width: w * 15 / 100},
		{Title: "Tx", Width: w * 15 / 100},
	}
	m.connTable.SetColumns(connCols)

	// Blocklist table
	blockCols := []table.Column{
		{Title: "IP Address", Width: w * 40 / 100},
		{Title: "Blocked At", Width: w * 40 / 100},
	}
	m.blockTable.SetColumns(blockCols)
}

func (m *Model) updateConnectionsTable() {
	rows := []table.Row{}
	for _, conn := range m.connections {
		nickname := conn.Nickname
		if nickname == "" {
			nickname = "-"
		}
		rows = append(rows, table.Row{
			conn.IP,
			nickname,
			strings.ToUpper(conn.Type),
			conn.ConnectedAt.Format(time.Kitchen),
			formatBytes(conn.BytesIn),
			formatBytes(conn.BytesOut),
		})
	}
	m.connTable.SetRows(rows)
}

func (m *Model) updateBlocklistTable() {
	rows := []table.Row{}
	for _, blocked := range m.blocked {
		rows = append(rows, table.Row{
			blocked.IP,
			blocked.BlockedAt.Format(time.RFC822),
		})
	}
	m.blockTable.SetRows(rows)
}

func (m *Model) updateLogViewport() {
	var content string
	for _, l := range m.logs {
		content += lipgloss.NewStyle().Foreground(lipgloss.Color("#A0A0A0")).Render(l) + "\n"
	}
	m.viewport.SetContent(content)
	if m.view == ViewDashboard {
		m.viewport.GotoBottom()
	}
}

func (m Model) getSelectedConnectionIP() string {
	cursor := m.connTable.Cursor()
	if cursor < len(m.connections) {
		return m.connections[cursor].IP
	}
	return ""
}

func (m Model) getSelectedBlockedIP() string {
	cursor := m.blockTable.Cursor()
	if cursor < len(m.blocked) {
		return m.blocked[cursor].IP
	}
	return ""
}

func extractIP(addr string) string {
	ip, _, _ := net.SplitHostPort(addr)
	if ip == "" {
		return addr
	}
	return ip
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
