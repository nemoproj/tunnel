#!/bin/bash

set -e

echo "Building Tunnel Project..."

# Create bin directory
mkdir -p bin

# Build Server
echo "Building Server..."
go build -ldflags="-s -w" -o bin/tunnel-server ./cmd/server

# Build Client
echo "Building Client..."
go build -ldflags="-s -w" -o bin/tunnel-client ./cmd/client

echo "Build complete!"
echo "Server binary: bin/tunnel-server"
echo "Client binary: bin/tunnel-client"
