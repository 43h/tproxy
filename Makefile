# Makefile for Go project with build tags

# Variables
BINARY_NAME := client

all: clean
	go build -o $(BINARY_NAME) .

debug:
	go build -gcflags "all=-N -l" -tags debug -o $(BINARY_NAME) .
	go build -o bin\tproxy-client .\tproxy-client
    go build -o bin\tproxy-server .\tproxy-server

clean:
	rm -f $(BINARY_NAME)