VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
PKG     := github.com/bolgov0zero/linager/internal/version
LDFLAGS := -s -w -X $(PKG).Version=$(VERSION) -X $(PKG).Commit=$(COMMIT) -X $(PKG).Date=$(DATE)

.PHONY: all web build test lint clean

all: build

web:
	cd web && npm ci && npm run build

build: web
	mkdir -p bin
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/linager_linux_amd64 ./cmd/linager

test:
	go vet ./... && go test -race ./...

lint:
	golangci-lint run
	shellcheck install.sh

clean:
	rm -rf bin web/dist/* && touch web/dist/.gitkeep
