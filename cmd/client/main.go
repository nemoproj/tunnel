package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hashicorp/yamux"
)

var bufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 65535)
	},
}

// Styles
var (
	// Shadcn-inspired Zinc Palette
	cText      = lipgloss.Color("#FAFAFA") // Zinc 50
	cSubtext   = lipgloss.Color("#A1A1AA") // Zinc 400
	cBorder    = lipgloss.Color("#52525B") // Zinc 600
	cFocus     = lipgloss.Color("#FAFAFA") // Zinc 50
	cSuccess   = lipgloss.Color("#22C55E") // Green 500
	cError     = lipgloss.Color("#EF4444") // Red 500
	cBackground = lipgloss.Color("#09090B") // Zinc 950

	appStyle = lipgloss.NewStyle().
			Margin(1, 1).
			Padding(1, 2).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cBorder)

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(cText).
			MarginBottom(1).
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottom(true).
			BorderForeground(cBorder).
			Width(60) // Match log box width

	labelStyle = lipgloss.NewStyle().
			Foreground(cSubtext).
			MarginTop(1).
			MarginBottom(0)

	// Input styles
	inputBaseStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cBorder).
			Padding(0, 1).
			Width(58)

	inputFocusedStyle = inputBaseStyle.Copy().
			BorderForeground(cFocus)

	statusStyle = lipgloss.NewStyle().
			Foreground(cSuccess).
			Bold(true)

	logBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cBorder).
			Padding(0, 1).
			MarginTop(1).
			Width(60).
			Height(12)

	logStyle = lipgloss.NewStyle().
			Foreground(cSubtext)
)

// Messages
type statusMsg string
type logMsg string
type errorMsg error
type configReadyMsg struct {
	serverAddr  string
	localAddr   string
	bedrockAddr string
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
	serverAddr  string
	localAddr   string
	bedrockAddr string
	gamePort    int

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
		inputs:     make([]textinput.Model, 4),
		status:     "Initializing...",
		logs:       []string{},
		configChan: configChan,
	}

	var t textinput.Model
	for i := range m.inputs {
		t = textinput.New()
		t.Cursor.Style = lipgloss.NewStyle().Foreground(cText)
		t.CharLimit = 64
		t.Prompt = "" // Clean look, no prompt

		switch i {
		case 0:
			t.Placeholder = "Relay Server (e.g. 134.185.100.194:8080)"
			t.SetValue("134.185.100.194:8080")
			t.Focus()
		case 1:
			t.Placeholder = "Local Java Server (e.g. localhost:25565)"
			t.SetValue("localhost:25565")
		case 2:
			t.Placeholder = "Local Bedrock/Geyser (e.g. localhost:19132)"
			t.SetValue("localhost:19132")
		case 3:
			t.Placeholder = "Public Game Port (e.g. 25565)"
			t.SetValue("25565")
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
					m.bedrockAddr = m.inputs[2].Value()
					portStr := m.inputs[3].Value()
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
							serverAddr:  m.serverAddr,
							localAddr:   m.localAddr,
							bedrockAddr: m.bedrockAddr,
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
						cmds[i] = m.inputs[i].Focus()
					} else {
						m.inputs[i].Blur()
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
		s = titleStyle.Render("Tunnel Setup") + "\n\n"
		s += lipgloss.NewStyle().Foreground(cSubtext).Render("Enter connection details:") + "\n\n"

		labels := []string{
			"Relay Server",
			"Local Java Server",
			"Local Bedrock/Geyser",
			"Public Game Port",
		}

		for i := range m.inputs {
			s += labelStyle.Render(labels[i]) + "\n"

			var style lipgloss.Style
			if i == m.focusIndex {
				style = inputFocusedStyle
			} else {
				style = inputBaseStyle
			}

			s += style.Render(m.inputs[i].View()) + "\n"
		}

		s += "\n" + lipgloss.NewStyle().Foreground(cSubtext).Render("• Tab/Shift+Tab: Navigate  • Enter: Connect  • Ctrl+C: Quit") + "\n"
	} else {
		// Running View
		host, _, _ := net.SplitHostPort(m.serverAddr)
		if host == "" {
			host = m.serverAddr
		}

		s = titleStyle.Render("Tunnel Host") + "\n\n"

		// Info Grid
		keyStyle := lipgloss.NewStyle().Foreground(cSubtext).Width(16)
		valStyle := lipgloss.NewStyle().Foreground(cText)

		s += fmt.Sprintf("%s%s\n", keyStyle.Render("Relay Server"), valStyle.Render(m.serverAddr))
		s += fmt.Sprintf("%s%s\n", keyStyle.Render("Local Server"), valStyle.Render(m.localAddr))
		s += fmt.Sprintf("%s%s\n", keyStyle.Render("Public Address"), valStyle.Render(fmt.Sprintf("%s:%d", host, m.gamePort)))
		s += fmt.Sprintf("%s%s\n\n", keyStyle.Render("Status"), statusStyle.Render(m.status))

		// Logs
		var logContent string
		if len(m.logs) == 0 {
			logContent = logStyle.Render("Waiting for activity...")
		} else {
			for _, l := range m.logs {
				logContent += logStyle.Render(l) + "\n"
			}
		}

		s += labelStyle.Render("Activity Log") + "\n"
		s += logBoxStyle.Render(logContent)

		s += "\n\n" + lipgloss.NewStyle().Foreground(cSubtext).Render("Press 'q' to quit.") + "\n"
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
			runHost(config.serverAddr, config.localAddr, config.bedrockAddr, p)
			p.Send(statusMsg("Disconnected. Retrying in 5s..."))
			time.Sleep(5 * time.Second)
		}
	}()

	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
	}
}

func runHost(serverAddr, localAddr, bedrockAddr string, p *tea.Program) {
	// 1. Connect to the Relay Server
	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		p.Send(errorMsg(err))
		return
	}
	p.Send(statusMsg("Connected to Relay"))
	p.Send(logMsg(fmt.Sprintf("Connected to %s (%s)", serverAddr, conn.RemoteAddr().String())))

	// Enable TCP Keepalives
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(30 * time.Second)
	}

	// 2. Setup Yamux Client
	// Capture yamux logs to the UI
	r, w := io.Pipe()
	defer w.Close()

	go func() {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			p.Send(logMsg("Yamux: " + scanner.Text()))
		}
	}()

	config := yamux.DefaultConfig()
	config.KeepAliveInterval = 10 * time.Second
	config.MaxStreamWindowSize = 4 * 1024 * 1024 // 4MB for better throughput on high-latency links
	config.LogOutput = w

	session, err := yamux.Client(conn, config)
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

		go handleStream(stream, localAddr, bedrockAddr, p)
	}
}

func handleStream(stream net.Conn, localAddr, bedrockAddr string, p *tea.Program) {
	defer stream.Close()

	// 4. Read Player IP Header
	// The Relay sends "protocol:IP:PORT\n" as the first bytes
	// protocol is "tcp" for Java Edition or "udp" for Bedrock Edition
	stream.SetReadDeadline(time.Now().Add(5 * time.Second))
	bufReader := bufio.NewReader(stream)
	header, err := bufReader.ReadString('\n')
	stream.SetReadDeadline(time.Time{}) // Reset deadline

	if err != nil {
		p.Send(errorMsg(fmt.Errorf("failed to read player header: %v", err)))
		return
	}
	header = strings.TrimSpace(header)

	// Parse protocol and player IP
	var protocol, playerIP string
	if strings.HasPrefix(header, "tcp:") {
		protocol = "tcp"
		playerIP = strings.TrimPrefix(header, "tcp:")
	} else if strings.HasPrefix(header, "udp:") {
		protocol = "udp"
		playerIP = strings.TrimPrefix(header, "udp:")
	} else {
		// Backwards compatibility: assume TCP if no prefix
		protocol = "tcp"
		playerIP = header
	}

	p.Send(logMsg(fmt.Sprintf("[%s] Player connected: %s", strings.ToUpper(protocol), playerIP)))

	if protocol == "udp" {
		// Handle UDP/Bedrock traffic
		if bedrockAddr == "" {
			p.Send(errorMsg(fmt.Errorf("Bedrock player connected but no local Bedrock address configured")))
			return
		}
		handleUDPStream(stream, bufReader, bedrockAddr, playerIP, p)
		return
	}

	// 5. Connect to Local Minecraft Server (TCP)
	localConn, err := net.Dial("tcp", localAddr)
	if err != nil {
		p.Send(errorMsg(fmt.Errorf("failed to connect to local MC: %v", err)))
		return
	}
	defer localConn.Close()

	// Enable TCP NoDelay for low latency on local connection
	if tcpConn, ok := localConn.(*net.TCPConn); ok {
		tcpConn.SetNoDelay(true)
	}

	// Bidirectional copy
	done := make(chan struct{}, 2)

	// Stream -> Local
	// IMPORTANT: Use bufReader here because it may have buffered some of the player's initial data
	go func() {
		buf := bufferPool.Get().([]byte)
		defer bufferPool.Put(buf)
		io.CopyBuffer(localConn, bufReader, buf)
		localConn.Close() // Signal EOF to other direction
		done <- struct{}{}
	}()

	// Local -> Stream
	go func() {
		buf := bufferPool.Get().([]byte)
		defer bufferPool.Put(buf)
		io.CopyBuffer(stream, localConn, buf)
		stream.Close() // Signal EOF to other direction
		done <- struct{}{}
	}()

	<-done
	<-done
	p.Send(logMsg(fmt.Sprintf("[TCP] Player disconnected: %s", playerIP)))
}

// handleUDPStream handles Bedrock Edition UDP traffic over the yamux stream
func handleUDPStream(stream net.Conn, bufReader *bufio.Reader, bedrockAddr string, playerIP string, p *tea.Program) {
	// Resolve UDP address
	udpAddr, err := net.ResolveUDPAddr("udp", bedrockAddr)
	if err != nil {
		p.Send(errorMsg(fmt.Errorf("failed to resolve UDP address: %v", err)))
		return
	}

	// Connect to local Bedrock/Geyser server
	localConn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		p.Send(errorMsg(fmt.Errorf("failed to connect to local Bedrock server: %v", err)))
		return
	}
	defer localConn.Close()

	done := make(chan struct{})

	// Stream -> Local UDP (read length-prefixed packets from stream)
	go func() {
		defer func() { done <- struct{}{} }()
		lenBuf := make([]byte, 2)
		for {
			// Read length prefix
			_, err := io.ReadFull(bufReader, lenBuf)
			if err != nil {
				return
			}

			pktLen := int(lenBuf[0])<<8 | int(lenBuf[1])
			if pktLen > 65535 {
				return
			}

			// Read packet data
			buf := bufferPool.Get().([]byte)
			_, err = io.ReadFull(bufReader, buf[:pktLen])
			if err != nil {
				bufferPool.Put(buf)
				return
			}

			// Send to local UDP server
			localConn.Write(buf[:pktLen])
			bufferPool.Put(buf)
		}
	}()

	// Local UDP -> Stream (send length-prefixed packets to stream)
	go func() {
		defer func() { done <- struct{}{} }()
		buffer := bufferPool.Get().([]byte)
		defer bufferPool.Put(buffer)
		
		for {
			localConn.SetReadDeadline(time.Now().Add(30 * time.Second))
			// Read into buffer[2:] to reserve space for length prefix
			n, err := localConn.Read(buffer[2:])
			if err != nil {
				return
			}

			// Write length prefix
			buffer[0] = byte(n >> 8)
			buffer[1] = byte(n & 0xFF)

			// Set write deadline to avoid blocking on stalled stream
			stream.SetWriteDeadline(time.Now().Add(5 * time.Second))
			_, err = stream.Write(buffer[:n+2])
			stream.SetWriteDeadline(time.Time{})
			if err != nil {
				return
			}
		}
	}()

	<-done
	p.Send(logMsg(fmt.Sprintf("[UDP] Player disconnected: %s", playerIP)))
}
