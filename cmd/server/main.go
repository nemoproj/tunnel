package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
	"tunnel/internal/daemon"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
type statusUpdateMsg daemon.ServerStatus
type tickMsg time.Time

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
	
	// Socket path for daemon mode
	socketPath string
	socketConn net.Conn
}

func initialModel(configChan chan configReadyMsg, socketPath string) model {
	m := model{
		state:      stateConfig,
		inputs:     make([]textinput.Model, 2),
		status:     "Initializing...",
		publicIP:   "Fetching...",
		logs:       []string{},
		configChan: configChan,
		socketPath: socketPath,
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
			if m.socketConn != nil {
				m.socketConn.Close()
			}
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

					return m, tickCmd()
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
				if m.socketConn != nil {
					m.socketConn.Close()
				}
				return m, tea.Quit
			}
		}

	// Handle Runtime Messages
	case statusUpdateMsg:
		m.status = msg.Status
		m.publicIP = msg.PublicIP
		m.controlPort = msg.ControlPort
		m.gamePort = msg.GamePort
		m.activePlayers = msg.ActivePlayers
		m.bytesTransferred = msg.BytesTransferred
		m.logs = msg.Logs
		if !msg.StartTime.IsZero() {
			m.startTime = msg.StartTime
		}
	case tickMsg:
		return m, tickCmd()
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

func main() {
	var (
		daemonMode  = flag.Bool("daemon", false, "Run in daemon mode (background)")
		controlPort = flag.Int("control-port", 8080, "Port for host control connections")
		gamePort    = flag.Int("game-port", 25565, "Port for player game connections")
		socketPath  = flag.String("socket", daemon.DefaultSocketPath, "Unix socket path for IPC")
	)
	flag.Parse()

	if *daemonMode {
		runDaemon(*controlPort, *gamePort, *socketPath)
	} else {
		runTUI(*socketPath)
	}
}

func runDaemon(controlPort, gamePort int, socketPath string) {
	log.Printf("Starting tunnel server in daemon mode...")
	log.Printf("Control Port: %d, Game Port: %d", controlPort, gamePort)
	log.Printf("Socket Path: %s", socketPath)

	srv := daemon.NewServer(controlPort, gamePort, socketPath)
	if err := srv.Start(); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}

	log.Printf("Server started successfully")

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Printf("Shutting down...")
	srv.Stop()
	srv.Wait()
}

func runTUI(socketPath string) {
	// Check if daemon is already running
	conn, err := net.Dial("unix", socketPath)
	if err == nil {
		// Daemon is running, connect to it directly
		fmt.Println("Connecting to running daemon...")
		p := tea.NewProgram(initialModelRunning())
		
		go runTUIClient(conn, p)
		
		if _, err := p.Run(); err != nil {
			fmt.Printf("Error: %v\n", err)
		}
		return
	}

	// No daemon running, start with config
	configChan := make(chan configReadyMsg)
	p := tea.NewProgram(initialModel(configChan, socketPath))

	// Run network loop in a goroutine
	go func() {
		// Wait for config
		config := <-configChan

		// Start embedded server
		runEmbeddedServer(config.controlPort, config.gamePort, socketPath, p)
	}()

	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
	}
}

func initialModelRunning() model {
	m := model{
		state:      stateRunning,
		status:     "Connecting...",
		publicIP:   "Unknown",
		logs:       []string{},
		startTime:  time.Now(),
	}
	return m
}

func runTUIClient(conn net.Conn, p *tea.Program) {
	defer conn.Close()

	decoder := json.NewDecoder(conn)
	for {
		var msg daemon.Message
		if err := decoder.Decode(&msg); err != nil {
			return
		}

		if msg.Type == "status" {
			var status daemon.ServerStatus
			if err := json.Unmarshal(msg.Data, &status); err == nil {
				p.Send(statusUpdateMsg(status))
			}
		}
	}
}

func runEmbeddedServer(controlPort, gamePort int, socketPath string, p *tea.Program) {
	srv := daemon.NewServer(controlPort, gamePort, socketPath)
	if err := srv.Start(); err != nil {
		log.Printf("Failed to start embedded server: %v", err)
		return
	}

	// Wait for socket to be ready with exponential backoff
	var conn net.Conn
	var err error
	maxRetries := 10
	for i := 0; i < maxRetries; i++ {
		conn, err = net.Dial("unix", socketPath)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(50*(1<<uint(i))) * time.Millisecond) // 50ms, 100ms, 200ms, ...
	}
	
	if err != nil {
		log.Printf("Failed to connect to embedded server after %d retries: %v", maxRetries, err)
		return
	}

	runTUIClient(conn, p)
}
