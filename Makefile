.PHONY: all web build run test clean

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

# nodynamic: decode JPEG XL covers via the pure-Go wazero backend (no
# purego/CGO), so every target stays CGO-free.
export GOFLAGS := -tags=nodynamic

all: build

web:
	cd web && npm install && npm run build

build: web
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/polka ./cmd/polka
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/polka-desktop ./cmd/polka-desktop

# Installers: deb+rpm (nfpm) and the Windows installer (makensis)
package: build
	mkdir -p dist/windows
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/windows/polka.exe ./cmd/polka
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS) -H=windowsgui' -o dist/windows/polka-desktop.exe ./cmd/polka-desktop
	VERSION=$(VERSION) go run github.com/goreleaser/nfpm/v2/cmd/nfpm@latest package -f packaging/nfpm.yaml -p deb -t dist/
	VERSION=$(VERSION) go run github.com/goreleaser/nfpm/v2/cmd/nfpm@latest package -f packaging/nfpm.yaml -p rpm -t dist/
	cd packaging && makensis -DVERSION=$(VERSION) installer.nsi

run: build
	./bin/polka

test:
	go test ./...

clean:
	rm -rf bin web/dist web/node_modules
