package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"tunnel/pkg/daemon"
	"tunnel/pkg/relay"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// Flags
	controlPort := flag.Int("control-port", 8080, "Control port for Host connection")
	gamePort := flag.Int("game-port", 25565, "Game port for Java Edition players (TCP)")
	bedrockPort := flag.Int("bedrock-port", 0, "Game port for Bedrock Edition players via Geyser (UDP, 0 to disable)")
	apiPort := flag.Int("api-port", 6060, "API port for status/logs")
	isDaemon := flag.Bool("daemon", false, "Run as daemon (internal use)")

	flag.Parse()

	pidFile := daemon.DefaultPidFile()
	logFile := daemon.DefaultLogFile()

	// Check for subcommand
	args := flag.Args()
	if len(args) > 0 {
		switch args[0] {
		case "start":
			handleStart(pidFile, logFile, *controlPort, *gamePort, *bedrockPort, *apiPort)
			return
		case "stop":
			handleStop(pidFile)
			return
		case "status":
			handleStatus(pidFile, *apiPort)
			return
		case "monitor":
			runMonitor(*apiPort)
			return
		case "help":
			printHelp()
			return
		default:
			fmt.Printf("Unknown command: %s\n", args[0])
			printHelp()
			os.Exit(1)
		}
	}

	// If running as daemon (forked process)
	if *isDaemon {
		runDaemon(pidFile, *controlPort, *gamePort, *bedrockPort, *apiPort)
		return
	}

	// Default: show help
	printHelp()
}

func printHelp() {
	fmt.Println("Tunnel Relay Server")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  tunnel-server start    Start the relay server in the background")
	fmt.Println("  tunnel-server stop     Stop the relay server")
	fmt.Println("  tunnel-server status   Show server status")
	fmt.Println("  tunnel-server monitor  Open the TUI monitor (attach to running server)")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  --control-port int   Control port for Host connection (default 8080)")
	fmt.Println("  --game-port int      Game port for Java Edition players (default 25565)")
	fmt.Println("  --bedrock-port int   Game port for Bedrock Edition via Geyser (default 0, disabled)")
	fmt.Println("  --api-port int       API port for status/logs (default 6060)")
	fmt.Println()
	fmt.Println("Geyser Support:")
	fmt.Println("  To enable Bedrock Edition support via Geyser, use --bedrock-port=19132")
	fmt.Println("  This opens a UDP port for Bedrock players to connect through.")
}

func handleStart(pidFile, logFile string, controlPort, gamePort, bedrockPort, apiPort int) {
	// Build args to pass to daemon
	args := []string{
		fmt.Sprintf("--control-port=%d", controlPort),
		fmt.Sprintf("--game-port=%d", gamePort),
		fmt.Sprintf("--bedrock-port=%d", bedrockPort),
		fmt.Sprintf("--api-port=%d", apiPort),
	}

	if err := daemon.Start(pidFile, logFile, args); err != nil {
		fmt.Printf("Failed to start daemon: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("API available at http://localhost:%d\n", apiPort)
	if bedrockPort > 0 {
		fmt.Printf("Bedrock/Geyser port: %d (UDP)\n", bedrockPort)
	}
	fmt.Println("Use 'tunnel-server monitor' to view status")
}

func handleStop(pidFile string) {
	if err := daemon.Stop(pidFile); err != nil {
		fmt.Printf("Failed to stop daemon: %v\n", err)
		os.Exit(1)
	}
}

func handleStatus(pidFile string, apiPort int) {
	running, pid := daemon.Status(pidFile)
	if running {
		fmt.Printf("Tunnel Relay Server is running (PID %d)\n", pid)
		fmt.Printf("API: http://localhost:%d/status\n", apiPort)
	} else {
		fmt.Println("Tunnel Relay Server is not running")
	}
}

func runMonitor(apiPort int) {
	p := tea.NewProgram(initialModel(apiPort))
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running monitor: %v\n", err)
		os.Exit(1)
	}
}

func runDaemon(pidFile string, controlPort, gamePort, bedrockPort, apiPort int) {
	// Write PID file
	if err := daemon.WritePid(pidFile); err != nil {
		fmt.Printf("Failed to write PID file: %v\n", err)
		os.Exit(1)
	}

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		<-sigChan
		fmt.Println("Shutting down...")
		daemon.RemovePid(pidFile)
		os.Exit(0)
	}()

	fmt.Println("Starting Tunnel Relay Server (daemon mode)...")
	fmt.Printf("Control Port:  %d\n", controlPort)
	fmt.Printf("Game Port:     %d (Java/TCP)\n", gamePort)
	if bedrockPort > 0 {
		fmt.Printf("Bedrock Port:  %d (Geyser/UDP)\n", bedrockPort)
	}
	fmt.Printf("API Port:      %d\n", apiPort)

	cfg := relay.Config{
		ControlPort: controlPort,
		GamePort:    gamePort,
		BedrockPort: bedrockPort,
	}

	r := relay.New(cfg)
	r.Start()

	// Start API
	go r.StartAPI(apiPort)

	// Block forever
	select {}
}
