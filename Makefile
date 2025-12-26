# Makefile for tproxy project
# Default: Build Linux client and Windows server in Linux environment

# Binary names
CLIENT_BINARY := client
SERVER_BINARY := server

# Build directory
BUILD_DIR := bin

# Go build flags
LDFLAGS := -s -w
DEBUG_FLAGS := -gcflags "all=-N -l"

.PHONY: all build client server debug clean test help install

# Default target - build Linux client and Windows server
all: clean build

# Build Linux client and Windows server (for Linux environment)
build: client server

# Build Linux client
client:
	@echo "Building Linux $(CLIENT_BINARY)..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(CLIENT_BINARY) ./proxy-client
	@echo "$(CLIENT_BINARY) built successfully"

# Build Windows server
server:
	@echo "Building Windows $(SERVER_BINARY)..."
	@mkdir -p $(BUILD_DIR)
	GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(SERVER_BINARY).exe ./proxy-server
	@echo "$(SERVER_BINARY).exe built successfully"

# Build with debug symbols
debug:
	@echo "Building with debug symbols..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 go build $(DEBUG_FLAGS) -o $(BUILD_DIR)/$(CLIENT_BINARY)-debug ./proxy-client
	GOOS=windows GOARCH=amd64 go build $(DEBUG_FLAGS) -o $(BUILD_DIR)/$(SERVER_BINARY)-debug.exe ./proxy-server
	@echo "Debug builds completed"

# Build both client and server for Linux
linux-all:
	@echo "Building both client and server for Linux..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(CLIENT_BINARY)-linux ./proxy-client
	GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(SERVER_BINARY)-linux ./proxy-server
	@echo "Linux builds completed"

# Build both client and server for Windows
windows-all:
	@echo "Building both client and server for Windows..."
	@mkdir -p $(BUILD_DIR)
	GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(CLIENT_BINARY)-windows.exe ./proxy-client
	GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(SERVER_BINARY)-windows.exe ./proxy-server
	@echo "Windows builds completed"

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...
	@echo "Tests completed"

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR)
	@echo "Clean completed"

# Install dependencies
install:
	@echo "Installing dependencies..."
	go mod download
	go mod tidy
	@echo "Dependencies installed"

# Show help
help:
	@echo "Available targets:"
	@echo "  make all       - Clean and build all binaries"
	@echo "  make build     - Build both client and server"
	@echo "  make client    - Build only client"
	@echo "  make server    - Build only server"
	@echo "  make debug       - Clean and build Linux client + Windows server (default)"
	@echo "  make build       - Build Linux client + Windows server"
	@echo "  make client      - Build Linux client only"
	@echo "  make server      - Build Windows server only"
	@echo "  make debug       - Build with debug symbols"
	@echo "  make linux-all   - Build both client and server for Linux"
	@echo "  make windows-all - Build both client and server for Windows"
	@echo "  make test        - Run tests"
	@echo "  make clean       - Remove build artifacts"
	@echo "  make install     - Install/update dependencies"
	@echo "  make help        - Show this help message"
	@echo ""
	@echo "Build outputs:"
	@echo "  bin/$(CLIENT_BINARY)         - Linux client"
	@echo "  bin/$(SERVER_BINARY).exe     - Windows server