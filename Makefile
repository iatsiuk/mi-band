BINARY := miband
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"

.PHONY: build test lint clean install

build: lint
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(BINARY) ./cmd/miband

test:
	go test -v -race -coverprofile=coverage.out ./...

lint:
	golangci-lint run

clean:
	rm -f $(BINARY) coverage.out

install:
	CGO_ENABLED=0 go install $(LDFLAGS) ./cmd/miband
