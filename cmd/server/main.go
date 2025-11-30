package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"

	"github.com/hashicorp/yamux"
)

// Global variable to hold the active tunnel session
var tunnelSession *yamux.Session
var tunnelMutex sync.Mutex

func main() {
	port := flag.Int("port", 25565, "Public port to listen on for players")
	controlPort := flag.Int("control", 8080, "Control port for the Host to connect to")
	flag.Parse()

	// Print Server Info
	publicIP := "Unknown"
	resp, err := http.Get("https://api.ipify.org?format=text")
	if err == nil {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		publicIP = string(body)
	}

	fmt.Println("┌───────────────────────────────────────────────────┐")
	fmt.Println("│           Minecraft Tunnel Relay Server           │")
	fmt.Println("├───────────────────────────────────────────────────┤")
	fmt.Printf("│  Public IP:    %-34s │\n", publicIP)
	fmt.Printf("│  Control Port: %-34d │\n", *controlPort)
	fmt.Printf("│  Game Port:    %-34d │\n", *port)
	fmt.Println("└───────────────────────────────────────────────────┘")
	fmt.Println()

	// 1. Start the Control Server (Where the Host connects)
	go startControlServer(*controlPort)

	// 2. Start the Game Server (Where Players connect)
	startGameServer(*port)
}

func startControlServer(port int) {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("Failed to start control listener: %v", err)
	}
	log.Printf("[Control] Listening for Host on :%d", port)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("[Control] Accept error: %v", err)
			continue
		}

		log.Printf("[Control] New connection from %s", conn.RemoteAddr())

		// Setup Yamux Server on this connection
		session, err := yamux.Server(conn, nil)
		if err != nil {
			log.Printf("[Control] Failed to create yamux session: %v", err)
			conn.Close()
			continue
		}

		// Store the session so the Game Server can use it
		tunnelMutex.Lock()
		if tunnelSession != nil {
			log.Printf("[Control] Warning: Overwriting existing tunnel session!")
			tunnelSession.Close()
		}
		tunnelSession = session
		tunnelMutex.Unlock()

		log.Printf("[Control] Tunnel established with Host!")
	}
}

func startGameServer(port int) {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("Failed to start game listener: %v", err)
	}
	log.Printf("[Game] Listening for Players on :%d", port)

	for {
		playerConn, err := listener.Accept()
		if err != nil {
			log.Printf("[Game] Accept error: %v", err)
			continue
		}

		go handlePlayer(playerConn)
	}
}

func handlePlayer(playerConn net.Conn) {
	defer playerConn.Close()

	tunnelMutex.Lock()
	session := tunnelSession
	tunnelMutex.Unlock()

	if session == nil {
		log.Printf("[Game] Player connected, but no Host tunnel is active. Dropping.")
		return
	}

	// Open a new stream (virtual connection) to the Host
	stream, err := session.Open()
	if err != nil {
		log.Printf("[Game] Failed to open stream to Host: %v", err)
		return
	}
	defer stream.Close()

	log.Printf("[Game] Player %s connected. Bridging to Host...", playerConn.RemoteAddr())

	// Send Player IP to Host so it can log it
	// We use a simple text protocol: "IP:PORT\n"
	if _, err := stream.Write([]byte(playerConn.RemoteAddr().String() + "\n")); err != nil {
		log.Printf("[Game] Failed to send player IP header: %v", err)
		return
	}

	// Bidirectional copy
	// We use a channel to wait for both copies to finish
	done := make(chan struct{})

	// Copy Player -> Host
	go func() {
		io.Copy(stream, playerConn)
		done <- struct{}{}
	}()

	// Copy Host -> Player
	go func() {
		io.Copy(playerConn, stream)
		done <- struct{}{}
	}()

	// Wait for one side to close
	<-done
	log.Printf("[Game] Connection closed for %s", playerConn.RemoteAddr())
}
