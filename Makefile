.PHONY: all build client server clean run-client run-server

# Binary names
CLIENT_BIN := bin/tunnel-client
SERVER_BIN := bin/tunnel-server

# Build flags
GOFLAGS := -ldflags="-s -w"

all: build

build: client server

client:
	@echo "Building client..."
	@mkdir -p bin
	go build $(GOFLAGS) -o $(CLIENT_BIN) ./cmd/client

server:
	@echo "Building server..."
	@mkdir -p bin
	go build $(GOFLAGS) -o $(SERVER_BIN) ./cmd/server

clean:
	@echo "Cleaning..."
	rm -rf bin

run-client: client
	./$(CLIENT_BIN)

run-server: server
	./$(SERVER_BIN)
