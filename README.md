# tunnel

## Problem Statement
(Based on provided chat log)
- **Target User:** Friend running a Minecraft server.
- **Network Constraints:** 
  - Behind Double NAT.
  - No Public IPv4 address.
  - Has IPv6, but not all players have IPv6 connectivity.
- **Requirements:**
  - Players must be able to connect via IPv4 without installing VPN software (e.g., Hamachi, ZeroTier).
  - No reliance on third-party tunneling services (ngrok, playit.gg, etc.).
  - No paid game hosting services.
  - **Self-hosted** solution.

## Proposed Solution: Multiplexed Reverse Tunnel
To bypass the Double NAT, we will build a "Reverse Tunnel" architecture using a single persistent TCP connection with stream multiplexing.

### Architecture

```mermaid
graph LR
    Player["플레이어 (IPv4)"] -- "TCP:25565" --> Relay["Relay (Public IPv4)"]
    Relay -- "TCP:8080 (Yamux Tunnel)" --> Host["PC (다원)"]
    Host -- "TCP:25565" --> MC["TNS 서버"]
```

1.  **Relay**
    - **Hosted by:** You (VPS/Public IP).
    - **Ports:**
      - `8080`: Tunnel listener (Host connects here).
      - `25565`: Game listener (Players connect here).
    - **Logic:**
      - Waits for Host to connect on `8080`.
      - Establishes a **Yamux** session (multiplexer) over this connection.
      - When a Player connects to `25565`:
        - Opens a new logical **Stream** inside the Yamux session to the Host.
        - Copies data between the Player socket and the Yamux stream.

2.  **Host**
    - **Hosted by:** Friend.
    - **Logic:**
      - Connects outbound to Relay on `8080`.
      - Starts a Yamux session.
      - Listens for incoming streams from the Relay (virtual incoming connections).
      - When a stream arrives:
        - Dials `localhost:25565` (Minecraft Server).
        - Copies data between the Yamux stream and the Minecraft socket.

### Why this approach?
- **No Port Forwarding for Host:** The Host does **NOT** need to open any ports. The connection is *outbound* (like loading a webpage), which works fine behind Double NAT.
- **IPv6 Support:** The Host can connect to the Relay over IPv6 (if the Relay supports it), while the Relay listens for Players on IPv4. This bridges the IPv6-only host to IPv4 players.
- **Robustness:** Yamux handles keepalives and connection health.
- **Simplicity:** No need to manage separate "control" and "data" connections. Every player is just a stream inside the main tunnel.

### Technology Stack
- **Language:** Go (Golang).
- **Library:** `github.com/hashicorp/yamux` for multiplexing.


## Todo
- [ ] Design Protocol (Control channel vs Data channels)
- [ ] Implement Relay
- [ ] Implement Host
- [ ] Add authentication (simple token) to prevent unauthorized usage.
