package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hashicorp/yamux"
)

// Styles
var (
	primaryColor   = lipgloss.Color("#3B82F6") // Blue for Server
	secondaryColor = lipgloss.Color("#FAFAFA")
	subtleColor    = lipgloss.Color("#626262")
	highlightColor = lipgloss.Color("#04B575")
	// errorColor     = lipgloss.Color("#FF5555")

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

	focusedStyle = lipgloss.NewStyle().Foreground(primaryColor)
	blurredStyle = lipgloss.NewStyle().Foreground(subtleColor)
	cursorStyle  = focusedStyle
)

// Messages
type statusMsg string
type logMsg string
type errorMsg error
type ipMsg string
type tickMsg time.Time
type playerConnMsg bool // true = connected, false = disconnected

type configReadyMsg struct {
	controlPort int
	gamePort    int
}

// Application State
type appState int

const (
	stateConfig appState = iota
	stateRunning
)

// Model
type model struct {
	state      appState
	inputs     []textinput.Model
	focusIndex int

	// Config
	controlPort int
	gamePort    int

	// Runtime
	status           string
	publicIP         string
	logs             []string
	quitting         bool
	startTime        time.Time
	activePlayers    int
	bytesTransferred int64

	// Channel to signal the network loop
	configChan chan configReadyMsg
}

func initialModel(configChan chan configReadyMsg) model {
	m := model{
		state:      stateConfig,
		inputs:     make([]textinput.Model, 2),
		status:     "Initializing...",
		publicIP:   "Fetching...",
		logs:       []string{},
		configChan: configChan,
	}

	var t textinput.Model
	for i := range m.inputs {
		t = textinput.New()
		t.Cursor.Style = cursorStyle
		t.CharLimit = 64

		switch i {
		case 0:
			t.Placeholder = "Control Port (e.g. 8080)"
			t.SetValue("8080")
			t.Focus()
			t.PromptStyle = focusedStyle
			t.TextStyle = focusedStyle
		case 1:
			t.Placeholder = "Game Port (e.g. 25565)"
			t.SetValue("25565")
			t.PromptStyle = blurredStyle
			t.TextStyle = blurredStyle
		}

		m.inputs[i] = t
	}

	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		tickCmd(),
	)
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}

		// Handle Config State
		if m.state == stateConfig {
			switch msg.String() {
			case "tab", "shift+tab", "enter", "up", "down":
				s := msg.String()

				// Did the user press enter on the last field?
				if s == "enter" && m.focusIndex == len(m.inputs)-1 {
					// Parse inputs
					cPort, _ := strconv.Atoi(m.inputs[0].Value())
					gPort, _ := strconv.Atoi(m.inputs[1].Value())

					if cPort == 0 {
						cPort = 8080
					}
					if gPort == 0 {
						gPort = 25565
					}

					m.controlPort = cPort
					m.gamePort = gPort

					// Switch state
					m.state = stateRunning
					m.startTime = time.Now()

					// Signal network loop to start
					go func() {
						m.configChan <- configReadyMsg{
							controlPort: m.controlPort,
							gamePort:    m.gamePort,
						}
					}()

					return m, nil
				}

				// Cycle indexes
				if s == "up" || s == "shift+tab" {
					m.focusIndex--
				} else {
					m.focusIndex++
				}

				if m.focusIndex > len(m.inputs)-1 {
					m.focusIndex = 0
				} else if m.focusIndex < 0 {
					m.focusIndex = len(m.inputs) - 1
				}

				cmds := make([]tea.Cmd, len(m.inputs))
				for i := 0; i <= len(m.inputs)-1; i++ {
					if i == m.focusIndex {
						// Set focused state
						cmds[i] = m.inputs[i].Focus()
						m.inputs[i].PromptStyle = focusedStyle
						m.inputs[i].TextStyle = focusedStyle
					} else {
						// Remove focused state
						m.inputs[i].Blur()
						m.inputs[i].PromptStyle = blurredStyle
						m.inputs[i].TextStyle = blurredStyle
					}
				}

				return m, tea.Batch(cmds...)
			}
		} else if m.state == stateRunning {
			if msg.String() == "q" {
				m.quitting = true
				return m, tea.Quit
			}
		}

	// Handle Runtime Messages
	case statusMsg:
		m.status = string(msg)
	case logMsg:
		m.logs = append(m.logs, string(msg))
		if len(m.logs) > 10 {
			m.logs = m.logs[1:]
		}
	case errorMsg:
		m.logs = append(m.logs, fmt.Sprintf("Error: %v", msg))
	case ipMsg:
		m.publicIP = string(msg)
	case tickMsg:
		// Update traffic from global counter
		m.bytesTransferred = atomic.LoadInt64(&globalBytes)
		return m, tickCmd()
	case playerConnMsg:
		if bool(msg) {
			m.activePlayers++
		} else {
			m.activePlayers--
		}
	}

	// Handle Input updates
	if m.state == stateConfig {
		cmd := m.updateInputs(msg)
		return m, cmd
	}

	return m, nil
}

func (m *model) updateInputs(msg tea.Msg) tea.Cmd {
	cmds := make([]tea.Cmd, len(m.inputs))
	for i := range m.inputs {
		m.inputs[i], cmds[i] = m.inputs[i].Update(msg)
	}
	return tea.Batch(cmds...)
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
	if m.quitting {
		return "Bye!\n"
	}

	var s string

	if m.state == stateConfig {
		s = titleStyle.Render("Tunnel Setup (RELAY SERVER)") + "\n\n"
		s += "Enter the ports to listen on:\n"

		labels := []string{
			"Control Port (for Host connection)",
			"Game Port (for Players)",
		}

		for i := range m.inputs {
			s += labelStyle.Render(labels[i]) + "\n"
			s += m.inputs[i].View() + "\n"
		}

		s += "\n" + lipgloss.NewStyle().Foreground(subtleColor).Render("• Tab/Shift+Tab: Navigate fields") + "\n"
		s += lipgloss.NewStyle().Foreground(subtleColor).Render("• Enter: Start Server") + "\n"
		s += lipgloss.NewStyle().Foreground(subtleColor).Render("• Ctrl+C: Quit") + "\n"
	} else {
		// Running View
		s = titleStyle.Render("Tunnel Relay Server") + "\n\n"

		// Box 1: Server Info
		infoContent := fmt.Sprintf("%s %s\n", labelStyle.Render("Public IP:   "), m.publicIP)
		infoContent += fmt.Sprintf("%s %d\n", labelStyle.Render("Control Port:"), m.controlPort)
		infoContent += fmt.Sprintf("%s %d\n", labelStyle.Render("Game Port:   "), m.gamePort)
		infoContent += fmt.Sprintf("%s %s", labelStyle.Render("Status:      "), statusStyle.Render(m.status))
		infoBox := boxStyle.Render(infoContent)

		// Box 2: Stats
		uptime := time.Since(m.startTime).Round(time.Second)
		statsContent := fmt.Sprintf("%s %s\n", labelStyle.Render("Uptime:        "), uptime.String())
		statsContent += fmt.Sprintf("%s %d\n", labelStyle.Render("Active Players:"), m.activePlayers)
		statsContent += fmt.Sprintf("%s %s\n", labelStyle.Render("Traffic:       "), formatBytes(m.bytesTransferred))
		statsBox := boxStyle.Render(statsContent)

		// Join Boxes
		row1 := lipgloss.JoinHorizontal(lipgloss.Top, infoBox, statsBox)

		// Logs
		var logContent string
		if len(m.logs) == 0 {
			logContent = logStyle.Render("Waiting for activity...")
		} else {
			for _, l := range m.logs {
				logContent += logStyle.Render(l) + "\n"
			}
		}

		s += row1 + "\n"
		s += logBoxStyle.Render(logContent)

		s += "\n\n" + lipgloss.NewStyle().Foreground(subtleColor).Render("Press 'q' to quit.") + "\n"
	}

	return appStyle.Render(s)
}

// Global variable to hold the active tunnel session
var (
	tunnelSession *yamux.Session
	tunnelMutex   sync.Mutex
	globalBytes   int64
)

func main() {
	configChan := make(chan configReadyMsg)
	p := tea.NewProgram(initialModel(configChan))

	// Run network loop in a goroutine
	go func() {
		// Wait for config
		config := <-configChan

		// Fetch IP
		go func() {
			client := http.Client{Timeout: 2 * time.Second}
			resp, err := client.Get("https://api.ipify.org?format=text")
			if err == nil {
				defer resp.Body.Close()
				body, _ := io.ReadAll(resp.Body)
				p.Send(ipMsg(string(body)))
			} else {
				p.Send(ipMsg("Unknown"))
			}
		}()

		p.Send(statusMsg("Starting listeners..."))

		// Start Servers
		go startControlServer(config.controlPort, p)
		go startGameServer(config.gamePort, p)
	}()

	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
	}
}

func startControlServer(port int, p *tea.Program) {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		p.Send(errorMsg(fmt.Errorf("control listener failed: %v", err)))
		return
	}
	p.Send(logMsg(fmt.Sprintf("[Control] Listening on :%d", port)))
	p.Send(statusMsg("Waiting for Host..."))

	for {
		conn, err := listener.Accept()
		if err != nil {
			p.Send(errorMsg(fmt.Errorf("control accept error: %v", err)))
			continue
		}

		p.Send(logMsg(fmt.Sprintf("[Control] Connection from %s", conn.RemoteAddr())))

		// Setup Yamux Server
		config := yamux.DefaultConfig()
		config.KeepAliveInterval = 10 * time.Second

		session, err := yamux.Server(conn, config)
		if err != nil {
			p.Send(errorMsg(fmt.Errorf("yamux session failed: %v", err)))
			conn.Close()
			continue
		}

		tunnelMutex.Lock()
		if tunnelSession != nil {
			p.Send(logMsg("[Control] Overwriting existing session"))
			tunnelSession.Close()
		}
		tunnelSession = session
		tunnelMutex.Unlock()

		p.Send(statusMsg("Host Connected"))
		p.Send(logMsg("[Control] Tunnel established"))
	}
}

func startGameServer(port int, p *tea.Program) {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		p.Send(errorMsg(fmt.Errorf("game listener failed: %v", err)))
		return
	}
	p.Send(logMsg(fmt.Sprintf("[Game] Listening on :%d", port)))

	for {
		playerConn, err := listener.Accept()
		if err != nil {
			p.Send(errorMsg(fmt.Errorf("game accept error: %v", err)))
			continue
		}

		go handlePlayer(playerConn, p)
	}
}

// CountingReader wraps an io.Reader and counts bytes read
type CountingReader struct {
	r io.Reader
}

func (c *CountingReader) Read(p []byte) (n int, err error) {
	n, err = c.r.Read(p)
	if n > 0 {
		atomic.AddInt64(&globalBytes, int64(n))
	}
	return
}

func handlePlayer(playerConn net.Conn, p *tea.Program) {
	defer playerConn.Close()

	tunnelMutex.Lock()
	session := tunnelSession
	tunnelMutex.Unlock()

	if session == nil {
		return
	}

	p.Send(logMsg(fmt.Sprintf("[Game] Player connected: %s", playerConn.RemoteAddr())))
	p.Send(playerConnMsg(true))
	defer p.Send(playerConnMsg(false))

	stream, err := session.Open()
	if err != nil {
		p.Send(errorMsg(fmt.Errorf("failed to open stream: %v", err)))
		return
	}
	defer stream.Close()

	// Send Player IP Header
	if _, err := stream.Write([]byte(playerConn.RemoteAddr().String() + "\n")); err != nil {
		p.Send(errorMsg(fmt.Errorf("failed to send header: %v", err)))
		return
	}

	// Bidirectional copy with traffic counting
	done := make(chan struct{})

	go func() {
		// Stream -> Player
		io.Copy(playerConn, &CountingReader{r: stream})
		done <- struct{}{}
	}()

	go func() {
		// Player -> Stream
		io.Copy(stream, &CountingReader{r: playerConn})
		done <- struct{}{}
	}()

	<-done
	p.Send(logMsg(fmt.Sprintf("[Game] Player disconnected: %s", playerConn.RemoteAddr())))
}
