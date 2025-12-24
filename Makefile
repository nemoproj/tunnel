.PHONY: all build client server clean run-client run-server

# Binary names
CLIENT_BIN := bin/tunnel-client
SERVER_BIN := bin/tunnel-server
BENCH_BIN := bin/relay-bench

# Build flags
GOFLAGS := -ldflags="-s -w"

all: build

build: client server build-bench

client:
	@echo "Building client..."
	@mkdir -p bin
	go build $(GOFLAGS) -o $(CLIENT_BIN) ./cmd/client

server:
	@echo "Building server..."
	@mkdir -p bin
	go build $(GOFLAGS) -o $(SERVER_BIN) ./cmd/server

build-bench:
	@echo "Building benchmark tool..."
	@mkdir -p bin
	go build $(GOFLAGS) -o $(BENCH_BIN) ./cmd/relay-bench

clean:
	@echo "Cleaning..."
	rm -rf bin

run-client: client
	./$(CLIENT_BIN)

run-server: server
	./$(SERVER_BIN)

bench: build-bench
	./$(BENCH_BIN)
