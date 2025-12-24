package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"tunnel/pkg/relay"

	"github.com/hashicorp/yamux"
)

func main() {
	var (
		duration   = flag.Duration("duration", 10*time.Second, "Duration of the benchmark")
		numClients = flag.Int("clients", 10, "Number of concurrent clients")
		packetSize = flag.Int("size", 4096, "Size of packets in bytes")
		controlPort = flag.Int("control-port", 20000, "Port for control connection")
		gamePort    = flag.Int("game-port", 20001, "Port for game connection")
	)
	flag.Parse()

	// Start Relay
	cfg := relay.Config{
		ControlPort: *controlPort,
		GamePort:    *gamePort,
		BedrockPort: 0, // Disable Bedrock for this bench
	}
	r := relay.New(cfg)
	r.Start()
	
	// Give it a moment to start listeners
	time.Sleep(100 * time.Millisecond)

	// Start Tunnel Agent (Simulated)
	ready := make(chan struct{})
	go func() {
		conn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", *controlPort))
		if err != nil {
			log.Fatalf("Failed to connect to control port: %v", err)
		}
		
		// Send magic header
		if _, err := conn.Write([]byte("TUN\n")); err != nil {
			log.Fatalf("Failed to send magic header: %v", err)
		}

		session, err := yamux.Client(conn, nil)
		if err != nil {
			log.Fatalf("Failed to create yamux client: %v", err)
		}

		close(ready)

		for {
			stream, err := session.Accept()
			if err != nil {
				return
			}
			go func(s net.Conn) {
				defer s.Close()
				// Read header (e.g. "tcp:127.0.0.1:54321\n")
				buf := make([]byte, 1024)
				n, err := s.Read(buf)
				if err != nil {
					return
				}
				// Echo back the initial chunk (header + any payload read)
				s.Write(buf[:n])

				// Echo back for round-trip latency measurement
				io.Copy(s, s)
			}(stream)
		}
	}()

	<-ready
	fmt.Printf("Benchmark started with %d clients, %v duration, %d byte packets\n", *numClients, *duration, *packetSize)

	var wg sync.WaitGroup
	start := time.Now()
	done := make(chan struct{})

	// Stats collection
	type ClientStats struct {
		Bytes        int64
		Requests     int64
		TotalLatency time.Duration
		MinLatency   time.Duration
		MaxLatency   time.Duration
	}
	statsChan := make(chan ClientStats, *numClients)

	// Timer to stop
	go func() {
		time.Sleep(*duration)
		close(done)
	}()

	// Start Clients
	for i := 0; i < *numClients; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", *gamePort))
			if err != nil {
				log.Printf("Failed to connect to game port: %v", err)
				return
			}
			defer conn.Close()

			stats := ClientStats{
				MinLatency: time.Hour, // Initialize with a large value
			}
			
			buf := make([]byte, *packetSize)
			readBuf := make([]byte, *packetSize)
			
			for {
				select {
				case <-done:
					statsChan <- stats
					return
				default:
					reqStart := time.Now()
					
					// Write
					n, err := conn.Write(buf)
					if err != nil {
						log.Printf("Client write error: %v", err)
						return
					}
					
					// Read back (wait for echo)
					_, err = io.ReadFull(conn, readBuf)
					if err != nil {
						log.Printf("Client read error: %v", err)
						return
					}
					
					latency := time.Since(reqStart)
					
					stats.Bytes += int64(n)
					stats.Requests++
					stats.TotalLatency += latency
					if latency < stats.MinLatency {
						stats.MinLatency = latency
					}
					if latency > stats.MaxLatency {
						stats.MaxLatency = latency
					}
				}
			}
		}()
	}

	wg.Wait()
	close(statsChan)
	elapsed := time.Since(start)

	// Aggregate stats
	var totalBytes int64
	var totalRequests int64
	var grandTotalLatency time.Duration
	var minLatency time.Duration = time.Hour
	var maxLatency time.Duration

	for s := range statsChan {
		totalBytes += s.Bytes
		totalRequests += s.Requests
		grandTotalLatency += s.TotalLatency
		if s.MinLatency < minLatency {
			minLatency = s.MinLatency
		}
		if s.MaxLatency > maxLatency {
			maxLatency = s.MaxLatency
		}
	}

	if totalRequests == 0 {
		fmt.Println("No requests completed.")
		return
	}

	avgLatency := grandTotalLatency / time.Duration(totalRequests)
	mb := float64(totalBytes) / 1024 / 1024
	mbps := (mb * 8) / elapsed.Seconds()

	fmt.Printf("Total transferred: %.2f MB\n", mb)
	fmt.Printf("Throughput: %.2f Mbps\n", mbps)
	fmt.Printf("Total Requests: %d\n", totalRequests)
	fmt.Printf("Latency (Min/Avg/Max): %v / %v / %v\n", minLatency, avgLatency, maxLatency)
	fmt.Printf("Elapsed: %v\n", elapsed)
}
