# Server Daemon Mode Implementation

## Problem
Previously, the tunnel server was tightly coupled to its TUI. When the TUI window was closed, the server would stop, making it impossible to run the server in the background.

## Solution
Decoupled the server from the TUI by introducing daemon mode with IPC communication.

## Changes

### Before
```bash
# Start server with TUI
./bin/tunnel-server
# Closing the window stops the server ❌
```

### After
```bash
# Option 1: Run server as daemon
./bin/tunnel-server -daemon
# Server runs independently in background ✅

# Option 2: Connect TUI to running daemon
./bin/tunnel-server
# Shows live status, can be closed/reopened freely ✅

# Option 3: Run as systemd service
sudo systemctl start tunnel-server
# Server runs as system service ✅
```

## Technical Implementation

### New Components
- `internal/daemon/ipc.go` - IPC protocol definitions
- `internal/daemon/server.go` - Standalone server with IPC
- `tunnel-server.service` - systemd service file

### Modified Components
- `cmd/server/main.go` - Added daemon mode and IPC client

### Features
- **Daemon Mode**: `-daemon` flag runs server in background
- **IPC Communication**: Unix socket at `/tmp/tunnel-server.sock`
- **Auto-detection**: TUI automatically connects to running daemon
- **Fallback Mode**: TUI starts embedded server if no daemon running
- **Live Updates**: Status updates every 500ms via IPC
- **Clean Shutdown**: Graceful handling of signals

## Usage Examples

### Start daemon
```bash
./bin/tunnel-server -daemon -control-port 8080 -game-port 25565
```

### Monitor with TUI
```bash
./bin/tunnel-server  # Connects to daemon
```

### Run as systemd service
```bash
sudo systemctl enable --now tunnel-server
```

## Benefits
- ✅ Server runs independently 24/7
- ✅ TUI can be closed without affecting server
- ✅ SSH disconnections don't stop server
- ✅ Multiple TUI instances can monitor simultaneously
- ✅ Easy integration with systemd
- ✅ Production-ready deployment
