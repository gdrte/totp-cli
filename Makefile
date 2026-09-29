GO ?= go

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -s -w \
	-X github.com/yitsushi/totp-cli/internal/info.Version=$(VERSION) \
	-X github.com/yitsushi/totp-cli/internal/info.Commit=$(COMMIT)

.PHONY: build
build:
	CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o bin/totp-cli .

.PHONY: test
test:
	$(GO) test \
		./internal/... \
		-v -count=1 \
		-coverprofile coverage.out \
		-covermode=atomic

.PHONY: coverage
coverage: test
	$(GO) tool cover -html=coverage.out

.PHONY: lint
lint:
	$(GO) vet ./...
	$(GO) tool golint -set_exit_status ./...
	$(GO) tool testifylint -v -enable-all ./...
	$(GO) tool golangci-lint run

.PHONY: install-dependencies
install-dependencies:
	$(GO) install tool
