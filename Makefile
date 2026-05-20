BINARY   := gf
CMD      := ./cmd/gf
VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -ldflags "-X main.version=$(VERSION)"
DIST     := dist

.PHONY: all build test lint vet tidy clean run run-headless release

all: build

build:
	go build $(LDFLAGS) -o bin/$(BINARY) $(CMD)

test:
	go test -race ./...

lint:
	golangci-lint run ./...

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -rf bin/ $(DIST)/

# Run with TUI (default)
run: build
	./bin/$(BINARY)

# Run without TUI — IPC only (TCP :7878, WebSocket :7879)
run-headless: build
	./bin/$(BINARY) --headless --ipc-port 7878 --ipc-ws-port 7879

# Cross-compile release binaries into dist/
release:
	mkdir -p $(DIST)
	GOOS=linux   GOARCH=amd64 go build $(LDFLAGS) -o $(DIST)/$(BINARY)-linux-amd64   $(CMD)
	GOOS=linux   GOARCH=arm64 go build $(LDFLAGS) -o $(DIST)/$(BINARY)-linux-arm64   $(CMD)
	GOOS=darwin  GOARCH=amd64 go build $(LDFLAGS) -o $(DIST)/$(BINARY)-darwin-amd64  $(CMD)
	GOOS=darwin  GOARCH=arm64 go build $(LDFLAGS) -o $(DIST)/$(BINARY)-darwin-arm64  $(CMD)
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(DIST)/$(BINARY)-windows-amd64.exe $(CMD)
