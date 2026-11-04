GO ?= go

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -s -w \
	-X github.com/gdrte/totp-go/internal/info.Version=$(VERSION) \
	-X github.com/gdrte/totp-go/internal/info.Commit=$(COMMIT)

# The OS and architecture `make gui` and `make install-gui` build for.
HOST_OS ?= $(shell $(GO) env GOOS)
HOST_ARCH ?= $(shell $(GO) env GOARCH)

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
	$(GO) run gioui.org/cmd/gogio@v0.10.3 -target macos -arch $(HOST_ARCH) -tags gui -ldflags "$(LDFLAGS)" \
		-appid com.github.gdrte.totp-go -o "$(GUI_APP)" ./cmd/totp-gui

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
		-appid com.github.gdrte.totp-go -o bin/totp-gui.exe ./cmd/totp-gui

# Build the desktop app for this OS: the .app bundle on macOS, the .exe on
# Windows, the binary on Linux.
.PHONY: gui gui-darwin gui-linux gui-windows
gui: gui-$(HOST_OS)
gui-darwin: bundle-gui
gui-linux: build-gui
gui-windows:
	$(MAKE) build-gui-windows WINDOWS_ARCH=$(HOST_ARCH)

# Build and install the desktop app for this OS:
#   macOS    "TOTP Authenticator.app" into MACOS_APPS (default /Applications)
#   Linux    totp-gui, its icon and a menu entry under PREFIX (default ~/.local)
#   Windows  totp-gui.exe into %LOCALAPPDATA%\Programs, plus a Start menu entry
MACOS_APPS ?= /Applications
PREFIX ?= $(HOME)/.local
LINUX_ICON := $(PREFIX)/share/icons/hicolor/scalable/apps/totp-authenticator.svg
LINUX_DESKTOP := $(PREFIX)/share/applications/totp-authenticator.desktop

.PHONY: install-gui install-gui-darwin install-gui-linux install-gui-windows
install-gui: install-gui-$(HOST_OS)

install-gui-darwin: gui-darwin
	rm -rf "$(MACOS_APPS)/TOTP Authenticator.app"
	ditto "$(GUI_APP)" "$(MACOS_APPS)/TOTP Authenticator.app"
	@echo "Installed $(MACOS_APPS)/TOTP Authenticator.app"

install-gui-linux: gui-linux
	install -Dm755 bin/totp-gui "$(PREFIX)/bin/totp-gui"
	install -Dm644 cmd/totp-gui/appicon.svg "$(LINUX_ICON)"
	mkdir -p "$(dir $(LINUX_DESKTOP))"
	printf '%s\n' '[Desktop Entry]' 'Type=Application' 'Name=TOTP Authenticator' \
		'Comment=2FA codes for you, your tests and your agents' \
		'Exec=$(PREFIX)/bin/totp-gui' 'Icon=totp-authenticator' \
		'Categories=Utility;Security;' 'Terminal=false' > "$(LINUX_DESKTOP)"
	@echo "Installed $(PREFIX)/bin/totp-gui and its menu entry"

install-gui-windows: gui-windows
	powershell -NoProfile -ExecutionPolicy Bypass -File scripts/install-gui-windows.ps1

.PHONY: uninstall-gui uninstall-gui-darwin uninstall-gui-linux uninstall-gui-windows
uninstall-gui: uninstall-gui-$(HOST_OS)

uninstall-gui-darwin:
	rm -rf "$(MACOS_APPS)/TOTP Authenticator.app"

uninstall-gui-linux:
	rm -f "$(PREFIX)/bin/totp-gui" "$(LINUX_ICON)" "$(LINUX_DESKTOP)"

uninstall-gui-windows:
	powershell -NoProfile -ExecutionPolicy Bypass -File scripts/install-gui-windows.ps1 -Uninstall

# Any other OS has no desktop app target.
gui-% install-gui-% uninstall-gui-%:
	@echo "The desktop app has no build or install target for $*." >&2; exit 1

# Re-render the README screenshots of the desktop app (offscreen, sample data).
.PHONY: screenshots
screenshots:
	mkdir -p docs/screenshots
	SCREENSHOT_DIR=$(CURDIR)/docs/screenshots CGO_ENABLED=1 $(GO) test -tags 'gui screenshots' \
		-run TestScreenshots -count=1 ./internal/gui

# Copy README.md to docs/index.md for GitHub Pages: paths become relative to
# docs/, and {{ is escaped so Jekyll's Liquid templating leaves it alone.
.PHONY: docs
docs:
	sed -e 's#docs/screenshots/#screenshots/#g' \
		-e 's#](LICENSE)#](https://github.com/gdrte/totp-go/blob/main/LICENSE)#g' \
		-e 's#{{#{{ "{{" }}#g' \
		README.md > docs/index.md

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
