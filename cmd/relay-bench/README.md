# Relay Benchmark Tool

This tool allows you to objectively measure the performance of the relay component separately from the main project.

## Usage

```bash
go run cmd/relay-bench/main.go [flags]
```

## Flags

- `-duration`: Duration of the benchmark (default `10s`)
- `-clients`: Number of concurrent clients (default `10`)
- `-size`: Size of packets in bytes (default `4096`)
- `-control-port`: Port for control connection (default `20000`)
- `-game-port`: Port for game connection (default `20001`)

## Example

```bash
go run cmd/relay-bench/main.go -duration 30s -clients 50 -size 8192
```

## How it works

1. Starts a standalone Relay instance.
2. Connects a simulated Tunnel Agent to the Control Port.
3. Connects multiple simulated Players to the Game Port.
4. Players send data as fast as possible.
5. Tunnel Agent consumes the data.
6. Measures total bytes transferred and calculates throughput.
7. Measures round-trip latency (Min/Avg/Max).

## Output

```text
Benchmark started with 10 clients, 10s duration, 4096 byte packets
Total transferred: 2211.28 MB
Throughput: 1768.97 Mbps
Total Requests: 566087
Latency (Min/Avg/Max): 36.166µs / 176.534µs / 4.3535ms
Elapsed: 10.000286791s
```
