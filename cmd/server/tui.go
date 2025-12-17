package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
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
	view          ViewMode
	logFullscreen bool
	err           error
	connected     bool
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
	// Shadcn-inspired Zinc Palette
	cText      = lipgloss.Color("#FAFAFA") // Zinc 50
	cSubtext   = lipgloss.Color("#A1A1AA") // Zinc 400
	cBorder    = lipgloss.Color("#52525B") // Zinc 600
	cFocus     = lipgloss.Color("#FAFAFA") // Zinc 50
	cSuccess   = lipgloss.Color("#22C55E") // Green 500
	cWarning   = lipgloss.Color("#F59E0B") // Amber 500
	cError     = lipgloss.Color("#EF4444") // Red 500
	cBackground = lipgloss.Color("#09090B") // Zinc 950
	cCard      = lipgloss.Color("#18181B") // Zinc 900
)

var (
	styleApp = lipgloss.NewStyle().
			Margin(1, 1).
			Padding(1, 2).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cBorder)

	styleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(cText).
			MarginBottom(1).
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottom(true).
			BorderForeground(cBorder).
			Width(40)

	styleTitleIcon = lipgloss.NewStyle().
			Foreground(cText).
			Bold(true)

	styleLabel = lipgloss.NewStyle().
			Foreground(cSubtext).
			Width(13).
			Bold(false)
	
	styleValue = lipgloss.NewStyle().
			Foreground(cText).
			Bold(true)
	
	styleHelp = lipgloss.NewStyle().
			Foreground(cSubtext).
			MarginTop(1).
			Padding(0, 1)

	styleTabActive = lipgloss.NewStyle().
			Foreground(cBackground).
			Background(cText).
			Padding(0, 2).
			Bold(true).
			MarginRight(1)
	
	styleTabInactive = lipgloss.NewStyle().
			Foreground(cSubtext).
			Padding(0, 2).
			MarginRight(1)

	styleTableHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(cText).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(cBorder).
			BorderBottom(true).
			Padding(0, 1)
	
	styleTableSelected = lipgloss.NewStyle().
			Bold(true).
			Foreground(cText).
			Background(cCard)
	
	styleStatusBadge = lipgloss.NewStyle().
			Bold(true).
			Padding(0, 1).
			MarginLeft(1)
)

// ============================================================================
// Initialization
// ============================================================================

func NewModel(apiPort int) Model {
	// Spinner
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(cText)

	// Viewport for logs
	vp := viewport.New(80, 10)
	vp.SetContent("Waiting for logs...")

	// Text input for nicknames
	ti := textinput.New()
	ti.Placeholder = "Enter nickname..."
	ti.CharLimit = 32
	ti.Width = 30
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(cText)
	ti.PromptStyle = lipgloss.NewStyle().Foreground(cSubtext)
	ti.TextStyle = lipgloss.NewStyle().Foreground(cText)

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
		{Title: "↓ Rx", Width: 12},
		{Title: "↑ Tx", Width: 12},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(10),
	)

	styles := table.DefaultStyles()
	styles.Header = styleTableHeader
	styles.Selected = styleTableSelected
	styles.Cell = lipgloss.NewStyle().Foreground(cText).Padding(0, 1)
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
	styles.Cell = lipgloss.NewStyle().Foreground(cText).Padding(0, 1)
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
	switch {
	case key.Matches(msg, m.keys.Enter):
		m.logFullscreen = !m.logFullscreen
		m.updateLayout()
		return m, nil
	case key.Matches(msg, m.keys.Escape):
		if m.logFullscreen {
			m.logFullscreen = false
			m.updateLayout()
			return m, nil
		}
	}

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
	conns := []ConnectionInfo(msg)
	sort.Slice(conns, func(i, j int) bool {
		return conns[i].IP < conns[j].IP
	})
	m.connections = conns
	m.updateConnectionsTable()
	return m, nil
}

func (m Model) handleBlocklist(msg blockedMsg) (tea.Model, tea.Cmd) {
	blocked := []BlockedInfo(msg)
	sort.Slice(blocked, func(i, j int) bool {
		return blocked[i].IP < blocked[j].IP
	})
	m.blocked = blocked
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
		return lipgloss.NewStyle().
			Foreground(cSubtext).
			Padding(2).
			Render("⚙ Initializing monitor...")
	}

	if m.inputMode {
		return m.renderInputOverlay()
	}

	if m.logFullscreen {
		return m.renderFullscreenLog()
	}

	// Header with icon
	header := lipgloss.NewStyle().
		MarginBottom(1).
		Render(
			lipgloss.JoinHorizontal(
				lipgloss.Center,
				styleTitle.Render(styleTitleIcon.Render(" ")+"TUNNEL RELAY MONITOR"),
			),
		)

	var content string

	if m.err != nil && !m.connected {
		content = m.renderConnectionError()
	} else {
		if m.err != nil {
			errorBanner := lipgloss.NewStyle().
				Foreground(cError).
				Background(cCard).
				Padding(0, 1).
				MarginBottom(1).
				Render("⚠ "+fmt.Sprintf("Error: %v", m.err))
			content = errorBanner + "\n\n"
		}

		content += m.renderTabs() + "\n"

		switch m.view {
		case ViewDashboard:
			content += m.renderDashboard()
		case ViewConnections:
			content += m.renderConnections()
		case ViewBlocklist:
			content += m.renderBlocklist()
		}

		content += "\n" + m.renderHelpBar()
	}

	return styleApp.Render(header + "\n" + content)
}

func (m Model) renderConnectionError() string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cWarning).
		Padding(2, 4).
		Width(60).
		Align(lipgloss.Center)

	icon := lipgloss.NewStyle().
		Foreground(cWarning).
		Bold(true).
		Render("")
	
	title := lipgloss.NewStyle().
		Foreground(cWarning).
		Bold(true).
		Render("Server Not Reachable")
	
	spinner := m.spinner.View()
	
	message := lipgloss.NewStyle().
		Foreground(cSubtext).
		Align(lipgloss.Center).
		Render(fmt.Sprintf("Attempting to connect to %s...", m.httpAddr))
	
	help := lipgloss.NewStyle().
		Foreground(cSubtext).
		Italic(true).
		Align(lipgloss.Center).
		Render("Start the server with: tunnel-server start")
	
	quit := styleHelp.
		Align(lipgloss.Center).
		Render("Press 'q' to quit")

	content := lipgloss.JoinVertical(
		lipgloss.Center,
		"",
		icon+" "+title,
		"",
		spinner+" "+message,
		"",
		help,
		"",
		quit,
	)

	return box.Render(content)
}

func (m Model) renderTabs() string {
	tabs := []string{}
	
	tabData := []struct {
		mode ViewMode
		name string
		icon string
	}{
		{ViewDashboard, "Dashboard", ""},
		{ViewConnections, "Connections", " "},
		{ViewBlocklist, "Blocklist", ""},
	}
	
	for _, tab := range tabData {
		label := tab.icon + " " + tab.name
		if tab.mode == m.view {
			tabs = append(tabs, styleTabActive.Render(label))
		} else {
			tabs = append(tabs, styleTabInactive.Render(label))
		}
	}
	
	tabBar := lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
	
	// Add a separator line under tabs
	separator := lipgloss.NewStyle().
		Foreground(cBorder).
		Width(m.width - 10).
		Render(strings.Repeat("─", m.width-10))
	
	return tabBar + "\n" + separator
}

func (m Model) renderDashboard() string {
	contentWidth := m.width - 10
	
	// Calculate dynamic box dimensions
	var boxWidth, boxHeight int
	var numColumns int
	
	if contentWidth >= 120 {
		numColumns = 4
		boxWidth = (contentWidth - 9) / 4
	} else if contentWidth >= 90 {
		numColumns = 3
		boxWidth = (contentWidth - 6) / 3
	} else if contentWidth >= 60 {
		numColumns = 2
		boxWidth = (contentWidth - 3) / 2
	} else {
		numColumns = 1
		boxWidth = contentWidth
	}
	
	boxHeight = 9
	if m.height < 30 {
		boxHeight = 7
	}

	// Network Info Box with icon
	netContent := m.renderInfoRow("Public IP", m.status.PublicIP)
	netContent += m.renderInfoRow("Control", fmt.Sprintf(":%d", m.status.ControlPort))
	netContent += m.renderInfoRow("Game (TCP)", fmt.Sprintf(":%d", m.status.GamePort))
	if m.status.BedrockPort > 0 {
		netContent += m.renderInfoRow("Bedrock (UDP)", fmt.Sprintf(":%d", m.status.BedrockPort))
	}
	netBox := m.renderInfoBox("  NETWORK", netContent, boxWidth, boxHeight, cText)

	// Tunnel Status Box with dynamic status badge
	var statusBadge string
	if m.status.TunnelConnected {
		statusBadge = styleStatusBadge.
			Foreground(cSuccess).
			Background(cCard).
			Render("  CONNECTED")
	} else {
		statusBadge = styleStatusBadge.
			Foreground(cError).
			Background(cCard).
			Render("  OFFLINE")
	}
	
	tunContent := lipgloss.NewStyle().MarginBottom(1).Render(statusBadge)
	tunContent += "\n" + m.renderInfoRow("Remote", m.status.TunnelRemoteAddr)
	tunContent += m.renderInfoRow("Streams", fmt.Sprintf("%d active", m.status.NumStreams))
	
	uptime := time.Duration(m.status.UptimeSeconds) * time.Second
	tunContent += m.renderInfoRow("Uptime", formatDuration(uptime))
	tunBox := m.renderInfoBox("  TUNNEL", tunContent, boxWidth, boxHeight, cSuccess)

	// Players Info Box
	playerCount := len(m.status.PlayerList)
	peakBadge := lipgloss.NewStyle().
		Foreground(cWarning).
		Bold(true).
		Render(fmt.Sprintf("%d", m.status.PeakPlayers))
	
	playContent := m.renderInfoRow("Active", fmt.Sprintf("%d", m.status.ActivePlayers))
	playContent += m.renderInfoRow("Peak", peakBadge)
	
	if playerCount > 0 {
		maxDisplay := boxHeight - 5
		if maxDisplay < 1 {
			maxDisplay = 1
		}
		
		playContent += "\n" + lipgloss.NewStyle().
			Foreground(cSubtext).
			Render("Online:")
		playContent += "\n"
		
		for i, p := range m.status.PlayerList {
			if i >= maxDisplay {
				playContent += lipgloss.NewStyle().
					Foreground(cSubtext).
					Italic(true).
					Render(fmt.Sprintf("  +%d more...", playerCount-maxDisplay)) + "\n"
				break
			}
			playContent += lipgloss.NewStyle().
				Foreground(cText).
				Render("   "+p) + "\n"
		}
	} else {
		playContent += "\n" + lipgloss.NewStyle().
			Foreground(cSubtext).
			Italic(true).
			Align(lipgloss.Center).
			Render("No active players")
	}
	playBox := m.renderInfoBox("  PLAYERS", playContent, boxWidth, boxHeight, cText)

	// Traffic Info Box with better formatting
	trafContent := m.renderInfoRow("Total", formatBytes(m.status.BytesTransferred))
	trafContent += m.renderInfoRow("↓ Received", formatBytes(m.status.BytesFromPlayers))
	trafContent += m.renderInfoRow("↑ Sent", formatBytes(m.status.BytesFromTunnel))
	
	// Calculate rate if possible
	if m.status.UptimeSeconds > 0 {
		ratePerSec := m.status.BytesTransferred / m.status.UptimeSeconds
		trafContent += m.renderInfoRow("Avg Rate", formatBytes(ratePerSec)+"/s")
	}
	trafBox := m.renderInfoBox("  TRAFFIC", trafContent, boxWidth, boxHeight, cText)

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

	// Logs viewport - fixed small size for dashboard
	logHeader := lipgloss.NewStyle().
		Bold(true).
		Foreground(cText).
		Padding(0, 1).
		MarginTop(1).
		MarginBottom(0).
		Width(contentWidth).
		Render(" ACTIVITY LOGS (Enter to expand)")
	
	logBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cBorder).
		Padding(0, 1).
		MarginTop(0).
		Width(contentWidth).
		Height(6). // Fixed height for dashboard view
		Render(m.viewport.View())

	return layout + "\n" + logHeader + "\n" + logBox
}

func (m Model) renderFullscreenLog() string {
	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(cText).
		Padding(0, 2).
		Width(m.width - 10).
		Render(" ACTIVITY LOGS (Esc to exit)")

	return header + "\n" + m.viewport.View()
}

func (m Model) renderConnections() string {
	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(cText).
		Padding(0, 2).
		MarginBottom(1).
		Render(fmt.Sprintf("  Active Connections (%d)", len(m.connections)))
	
	var content string
	if len(m.connections) == 0 {
		content = lipgloss.NewStyle().
			Foreground(cSubtext).
			Italic(true).
			Padding(2, 0).
			Render("No active connections.")
	} else {
		content = m.connTable.View()
	}
	return header + "\n" + content
}

func (m Model) renderBlocklist() string {
	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(cText).
		Padding(0, 2).
		MarginBottom(1).
		Render(fmt.Sprintf("  Blocked IPs (%d)", len(m.blocked)))
	
	var content string
	if len(m.blocked) == 0 {
		content = lipgloss.NewStyle().
			Foreground(cSubtext).
			Italic(true).
			Padding(2, 0).
			Render("No blocked IPs.")
	} else {
		content = m.blockTable.View()
	}
	return header + "\n" + content
}

func (m Model) renderHelpBar() string {
	if m.showHelp {
		return lipgloss.NewStyle().
			Foreground(cSubtext).
			Padding(1, 2).
			MarginTop(1).
			Render(m.help.View(m.keys))
	}

	var helpText string
	keyStyle := lipgloss.NewStyle().
		Foreground(cText).
		Bold(true)
	
	divider := lipgloss.NewStyle().
		Foreground(cSubtext).
		Render(" • ")

	switch m.view {
	case ViewDashboard:
		helpText = keyStyle.Render("Tab") + " Switch View" + divider +
			keyStyle.Render("↑/↓") + " Scroll" + divider +
			keyStyle.Render("?") + " Help" + divider +
			keyStyle.Render("q") + " Quit"
	case ViewConnections:
		helpText = keyStyle.Render("Tab") + " Switch View" + divider +
			keyStyle.Render("x") + " Disconnect" + divider +
			keyStyle.Render("b") + " Block" + divider +
			keyStyle.Render("n") + " Nickname" + divider +
			keyStyle.Render("?") + " Help" + divider +
			keyStyle.Render("q") + " Quit"
	case ViewBlocklist:
		helpText = keyStyle.Render("Tab") + " Switch View" + divider +
			keyStyle.Render("u/x") + " Unblock" + divider +
			keyStyle.Render("?") + " Help" + divider +
			keyStyle.Render("q") + " Quit"
	}
	
	return lipgloss.NewStyle().
		Foreground(cSubtext).
		Padding(0, 2).
		MarginTop(1).
		Width(m.width - 10).
		Render(helpText)
}

func (m Model) renderInputOverlay() string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(cText).
		Render("Set Nickname")
	
	subtitle := lipgloss.NewStyle().
		Foreground(cSubtext).
		Render("for " + m.inputTarget)
	
	content := title + "\n" + subtitle + "\n\n"
	content += m.textInput.View() + "\n\n"
	
	helpKeys := lipgloss.NewStyle().
		Foreground(cSubtext).
		Render(
			lipgloss.NewStyle().Foreground(cText).Render("Enter") + " Save • " +
				lipgloss.NewStyle().Foreground(cText).Render("Esc") + " Cancel",
		)
	content += helpKeys

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cBorder).
		Padding(2, 3).
		Width(50).
		Render(content)

	return lipgloss.Place(m.width-10, m.height-6, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) renderInfoRow(label, value string) string {
	return fmt.Sprintf("%s %s\n",
		styleLabel.Render(label+":"),
		styleValue.Render(value))
}

func (m Model) renderInfoBox(title, content string, width, height int, accentColor lipgloss.Color) string {
	titleBar := lipgloss.NewStyle().
		Bold(true).
		Foreground(accentColor).
		Padding(0, 0).
		Width(width - 4).
		Render(title)
	
	boxContent := titleBar + "\n" + content
	
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cBorder).
		Padding(1, 2).
		MarginRight(2).
		Width(width - 6).
		Height(height).
		Render(boxContent)
}

// ============================================================================
// Helpers
// ============================================================================

func (m *Model) updateLayout() {
	contentWidth := m.width - 10
	contentHeight := m.height - 8

	// Viewport
	if m.logFullscreen {
		m.viewport.Width = m.width - 10
		m.viewport.Height = m.height - 2 // Minus header
	} else {
		m.viewport.Width = contentWidth
		m.viewport.Height = 6 // Fixed height for dashboard
	}

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
	w := m.width - 10

	// Connections table
	connCols := []table.Column{
		{Title: "IP Address", Width: w * 20 / 100},
		{Title: "Nickname", Width: w * 15 / 100},
		{Title: "Type", Width: w * 10 / 100},
		{Title: "Connected", Width: w * 20 / 100},
		{Title: "↓ Rx", Width: w * 15 / 100},
		{Title: "↑ Tx", Width: w * 15 / 100},
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
			nickname = lipgloss.NewStyle().Foreground(cSubtext).Italic(true).Render("(unnamed)")
		} else {
			nickname = lipgloss.NewStyle().Foreground(cText).Render(nickname)
		}
		
		connType := strings.ToUpper(conn.Type)
		if connType == "TCP" {
			connType = lipgloss.NewStyle().Foreground(cText).Render(" TCP")
		} else if connType == "UDP" {
			connType = lipgloss.NewStyle().Foreground(cWarning).Render(" UDP")
		}
		
		rows = append(rows, table.Row{
			conn.IP,
			nickname,
			connType,
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
		// Color-code log levels
		logLine := l
		if strings.Contains(l, "ERROR") || strings.Contains(l, "error") {
			logLine = lipgloss.NewStyle().Foreground(cError).Render(l)
		} else if strings.Contains(l, "WARN") || strings.Contains(l, "warn") {
			logLine = lipgloss.NewStyle().Foreground(cWarning).Render(l)
		} else if strings.Contains(l, "INFO") || strings.Contains(l, "info") {
			logLine = lipgloss.NewStyle().Foreground(cText).Render(l)
		} else {
			logLine = lipgloss.NewStyle().Foreground(cSubtext).Render(l)
		}
		content += logLine + "\n"
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

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	return fmt.Sprintf("%dd %dh", days, hours)
}
