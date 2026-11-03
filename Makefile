VERSION ?= dev
LDFLAGS := -s -w -X panel/internal/shared/version.Version=$(VERSION)

.PHONY: all web build build-linux test dev clean

all: web build

web:
	cd web && npm ci && npm run build

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/panel ./cmd/panel

build-linux:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/panel-linux-amd64 ./cmd/panel
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/panel-linux-arm64 ./cmd/panel

test:
	go vet ./...
	go test ./...
	cd web && npm run typecheck

dev:
	go run ./cmd/panel -dev -data-dir data-dev

clean:
	rm -rf bin web/dist/assets web/dist/index.html
