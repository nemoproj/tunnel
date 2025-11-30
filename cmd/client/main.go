package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hashicorp/yamux"
)

// Styles
var (
	primaryColor   = lipgloss.Color("#7D56F4")
	secondaryColor = lipgloss.Color("#FAFAFA")
	subtleColor    = lipgloss.Color("#626262")
	highlightColor = lipgloss.Color("#04B575")
	errorColor     = lipgloss.Color("#FF5555")

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

	logBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(subtleColor).
			Padding(0, 1).
			MarginTop(1).
			Width(60)

	logStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A0A0A0"))

	focusedStyle = lipgloss.NewStyle().Foreground(primaryColor)
	blurredStyle = lipgloss.NewStyle().Foreground(subtleColor)
	cursorStyle  = focusedStyle.Copy()
)

// Messages
type statusMsg string
type logMsg string
type errorMsg error
type configReadyMsg struct {
	serverAddr string
	localAddr  string
	gamePort   int
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
	serverAddr string
	localAddr  string
	gamePort   int

	// Runtime
	status   string
	logs     []string
	quitting bool

	// Channel to signal the network loop
	configChan chan configReadyMsg
}

func initialModel(configChan chan configReadyMsg) model {
	m := model{
		state:      stateConfig,
		inputs:     make([]textinput.Model, 3),
		status:     "Initializing...",
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
			t.Placeholder = "Relay Server (e.g. OCI instance 152.67.204.131:8080)"
			t.SetValue("152.67.204.131:8080")
			t.Focus()
			t.PromptStyle = focusedStyle
			t.TextStyle = focusedStyle
		case 1:
			t.Placeholder = "Local Server (e.g. localhost:25565)"
			t.SetValue("localhost:25565")
			t.PromptStyle = blurredStyle
			t.TextStyle = blurredStyle
		case 2:
			t.Placeholder = "Public Game Port (e.g. 25565)"
			t.SetValue("25565")
			t.PromptStyle = blurredStyle
			t.TextStyle = blurredStyle
		}

		m.inputs[i] = t
	}

	return m
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
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
					m.serverAddr = m.inputs[0].Value()
					m.localAddr = m.inputs[1].Value()
					portStr := m.inputs[2].Value()
					port, err := strconv.Atoi(portStr)
					if err != nil {
						port = 25565 // Default fallback
					}
					m.gamePort = port

					// Switch state
					m.state = stateRunning

					// Signal network loop to start
					go func() {
						m.configChan <- configReadyMsg{
							serverAddr: m.serverAddr,
							localAddr:  m.localAddr,
							gamePort:   m.gamePort,
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

func (m model) View() string {
	if m.quitting {
		return "Bye!\n"
	}

	var s string

	if m.state == stateConfig {
		s = titleStyle.Render("Tunnel Setup (SERVER HOST)") + "\n\n"
		s += "Enter the details for your connection:\n"

		labels := []string{
			"Relay Server Control Address",
			"Local Minecraft Server Address",
			"Public Game Port (for display)",
		}

		for i := range m.inputs {
			s += labelStyle.Render(labels[i]) + "\n"
			s += m.inputs[i].View() + "\n"
		}

		s += "\n" + lipgloss.NewStyle().Foreground(subtleColor).Render("• Tab/Shift+Tab: Navigate fields") + "\n"
		s += lipgloss.NewStyle().Foreground(subtleColor).Render("• Enter: Connect to Relay") + "\n"
		s += lipgloss.NewStyle().Foreground(subtleColor).Render("• Ctrl+C: Quit") + "\n"
	} else {
		// Running View
		host, _, _ := net.SplitHostPort(m.serverAddr)
		if host == "" {
			host = m.serverAddr
		}

		s = titleStyle.Render("Tunnel Host") + "\n\n"

		// Info Grid
		s += fmt.Sprintf("%s %s\n", labelStyle.Render("Relay Server:  "), m.serverAddr)
		s += fmt.Sprintf("%s %s\n", labelStyle.Render("Local Server:  "), m.localAddr)
		s += fmt.Sprintf("%s %s:%d\n", labelStyle.Render("Public Address:"), host, m.gamePort)
		s += fmt.Sprintf("%s %s\n\n", labelStyle.Render("Status:        "), statusStyle.Render(m.status))

		// Logs
		var logContent string
		if len(m.logs) == 0 {
			logContent = logStyle.Render("Waiting for activity...")
		} else {
			for _, l := range m.logs {
				logContent += logStyle.Render(l) + "\n"
			}
		}

		s += "Logs:"
		s += logBoxStyle.Render(logContent)

		s += "\n\n" + lipgloss.NewStyle().Foreground(subtleColor).Render("Press 'q' to quit.") + "\n"
	}

	return appStyle.Render(s)
}

func main() {
	configChan := make(chan configReadyMsg)
	p := tea.NewProgram(initialModel(configChan))

	// Run network loop in a goroutine
	go func() {
		// Wait for config
		config := <-configChan

		// Start loop
		for {
			p.Send(statusMsg("Connecting..."))
			runHost(config.serverAddr, config.localAddr, p)
			p.Send(statusMsg("Disconnected. Retrying in 5s..."))
			time.Sleep(5 * time.Second)
		}
	}()

	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
	}
}

func runHost(serverAddr, localAddr string, p *tea.Program) {
	// 1. Connect to the Relay Server
	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		p.Send(errorMsg(err))
		return
	}
	p.Send(statusMsg("Connected to Relay"))
	p.Send(logMsg(fmt.Sprintf("Connected to %s (%s)", serverAddr, conn.RemoteAddr().String())))

	// 2. Setup Yamux Client
	session, err := yamux.Client(conn, nil)
	if err != nil {
		p.Send(errorMsg(err))
		conn.Close()
		return
	}

	// 3. Accept streams from the Relay
	for {
		stream, err := session.Accept()
		if err != nil {
			p.Send(errorMsg(err))
			return
		}

		go handleStream(stream, localAddr, p)
	}
}

func handleStream(stream net.Conn, localAddr string, p *tea.Program) {
	defer stream.Close()

	// 4. Read Player IP Header
	// The Relay sends "IP:PORT\n" as the first bytes
	bufReader := bufio.NewReader(stream)
	playerIP, err := bufReader.ReadString('\n')
	if err != nil {
		p.Send(errorMsg(fmt.Errorf("failed to read player IP: %v", err)))
		return
	}
	playerIP = strings.TrimSpace(playerIP)
	p.Send(logMsg(fmt.Sprintf("Player connected: %s", playerIP)))

	// 5. Connect to Local Minecraft Server
	localConn, err := net.Dial("tcp", localAddr)
	if err != nil {
		p.Send(errorMsg(fmt.Errorf("failed to connect to local MC: %v", err)))
		return
	}
	defer localConn.Close()

	// Bidirectional copy
	done := make(chan struct{})

	// Stream -> Local
	// IMPORTANT: Use bufReader here because it may have buffered some of the player's initial data
	go func() {
		io.Copy(localConn, bufReader)
		done <- struct{}{}
	}()

	// Local -> Stream
	go func() {
		io.Copy(stream, localConn)
		done <- struct{}{}
	}()

	<-done
	p.Send(logMsg(fmt.Sprintf("Player disconnected: %s", playerIP)))
}
