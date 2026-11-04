GO ?= go

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -s -w \
	-X github.com/yitsushi/totp-cli/internal/info.Version=$(VERSION) \
	-X github.com/yitsushi/totp-cli/internal/info.Commit=$(COMMIT)

.PHONY: build
build:
	CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o bin/totp-cli .

# The desktop app needs cgo (Gio uses the native windowing APIs).
.PHONY: build-gui
build-gui:
	CGO_ENABLED=1 $(GO) build -tags gui -ldflags "$(LDFLAGS)" -o bin/totp-gui ./cmd/totp-gui

# macOS app bundle for the desktop app, with cmd/totp-gui/appicon.png as its
# icon. Must run on macOS (it uses iconutil).
GUI_APP := bin/TOTP Authenticator.app

.PHONY: bundle-gui
bundle-gui:
	rm -rf "$(GUI_APP)"
	$(GO) run gioui.org/cmd/gogio@v0.10.3 -target macos -arch $(shell $(GO) env GOARCH) -tags gui -ldflags "$(LDFLAGS)" \
		-appid com.github.yitsushi.totp-cli -o "$(GUI_APP)" ./cmd/totp-gui

# Windows builds cross-compile from any OS (Gio needs no cgo on Windows).
# Set WINDOWS_ARCH=arm64 for ARM machines.
WINDOWS_ARCH ?= amd64

.PHONY: build-windows
build-windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=$(WINDOWS_ARCH) $(GO) build -ldflags "$(LDFLAGS)" -o bin/totp-cli.exe .

# The desktop app as a Windows .exe with the app icon and no console window.
.PHONY: build-gui-windows
build-gui-windows:
	$(GO) run gioui.org/cmd/gogio@v0.10.3 -target windows -arch $(WINDOWS_ARCH) -tags gui -ldflags "$(LDFLAGS)" \
		-appid com.github.yitsushi.totp-cli -o bin/totp-gui.exe ./cmd/totp-gui

# Regenerate the app icon from its SVG source (needs librsvg).
.PHONY: appicon
appicon:
	rsvg-convert -w 1024 -h 1024 cmd/totp-gui/appicon.svg -o cmd/totp-gui/appicon.png

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
	$(GO) tool golangci-lint run --build-tags gui ./internal/gui/... ./cmd/...

.PHONY: install-dependencies
install-dependencies:
	$(GO) install tool
