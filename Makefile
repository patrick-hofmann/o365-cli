# o365-cli Makefile
# Cross-Platform Build Support

BINARY_NAME=o365-cli
VERSION=2.1.1
BUILD_DIR=dist
MAIN_PATH=./cmd/o365-cli

# Build flags
LDFLAGS=-ldflags "-s -w -X main.Version=$(VERSION)"

# Alle Plattformen
PLATFORMS=darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64

.PHONY: all build clean test build-all release help

# Standard: Für aktuelle Plattform bauen
build:
	go build $(LDFLAGS) -o $(BINARY_NAME) $(MAIN_PATH)

# Alle Plattformen bauen
build-all: clean
	@mkdir -p $(BUILD_DIR)
	@for platform in $(PLATFORMS); do \
		GOOS=$${platform%/*} GOARCH=$${platform#*/} \
		go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-$${platform%/*}-$${platform#*/}$$([ "$${platform%/*}" = "windows" ] && echo ".exe") $(MAIN_PATH) && \
		echo "✓ Built for $${platform}"; \
	done

# Einzelne Plattformen
build-linux:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 $(MAIN_PATH)

build-linux-arm:
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 $(MAIN_PATH)

build-macos:
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 $(MAIN_PATH)

build-macos-arm:
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 $(MAIN_PATH)

build-windows:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe $(MAIN_PATH)

# Tests
test:
	go test -v ./...

test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Dependencies
deps:
	go mod download
	go mod verify

tidy:
	go mod tidy

# Linting
lint:
	@which golangci-lint > /dev/null || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run

# Aufräumen
clean:
	rm -rf $(BUILD_DIR)
	rm -f $(BINARY_NAME)
	rm -f coverage.out coverage.html

# Installation (lokal)
install: build
	cp $(BINARY_NAME) $(GOPATH)/bin/

# Release erstellen (mit Checksums)
release: build-all
	@cd $(BUILD_DIR) && \
	for file in *; do \
		sha256sum "$$file" >> checksums.txt; \
	done
	@echo "✓ Release files in $(BUILD_DIR)/"
	@ls -la $(BUILD_DIR)/

# Docker Build (optional)
docker-build:
	docker build -t $(BINARY_NAME):$(VERSION) .

# Hilfe
help:
	@echo "o365-cli Build System"
	@echo ""
	@echo "Targets:"
	@echo "  build         - Build für aktuelle Plattform"
	@echo "  build-all     - Build für alle Plattformen"
	@echo "  build-linux   - Build für Linux (amd64)"
	@echo "  build-macos   - Build für macOS (amd64)"
	@echo "  build-windows - Build für Windows (amd64)"
	@echo "  test          - Tests ausführen"
	@echo "  lint          - Linter ausführen"
	@echo "  clean         - Build-Artefakte löschen"
	@echo "  release       - Release mit Checksums erstellen"
	@echo "  install       - Lokal installieren"
	@echo "  deps          - Dependencies herunterladen"
	@echo "  help          - Diese Hilfe anzeigen"
