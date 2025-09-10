# Makefile for TF1 HealthCheck project

BINARY_NAME=healthcheck

.PHONY: test build run clean

# Run all tests with race detector (default)
test:
	go test -race -v ./...

# Build the binary
build: clean
	go build -race -o $(BINARY_NAME) main.go

# Run the application
run:
	go run main.go test_urls.txt

# Clean build artifacts
clean:
	rm -f $(BINARY_NAME)