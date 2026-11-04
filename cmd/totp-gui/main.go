//go:build gui

// Command totp-gui is the desktop app: it registers accounts, shows live
// codes, and serves the REST API of `totp-cli serve` while it is open.
//
// Build it with `make build-gui` (Gio needs cgo on macOS and Linux).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"gioui.org/app"
	"github.com/gdrte/totp-go/internal/gui"
	"github.com/gdrte/totp-go/internal/storage"
)

const (
	defaultListenAddress = "127.0.0.1:8484"
	exitUsage            = 2
)

func main() {
	cfg, err := parseFlags()
	if err != nil {
		fmt.Fprintf(os.Stderr, " !! Error: %s\n", err)
		os.Exit(exitUsage)
	}

	go func() {
		err := gui.Run(context.Background(), cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, " !! Error: %s\n", err)
			os.Exit(1)
		}

		os.Exit(0)
	}()

	app.Main()
}

func parseFlags() (gui.Config, error) {
	cfg := gui.Config{Password: os.Getenv("TOTP_PASS")}

	flag.StringVar(&cfg.DBPath, "db", os.Getenv("TOTP_SERVER_DB"),
		"SQLite database file, shared with `totp-cli serve`. (env TOTP_SERVER_DB, default ~/.config/totp-cli/server.db)")
	flag.StringVar(&cfg.Listen, "listen", defaultListenAddress,
		"API address. Empty disables the API. Binding beyond localhost exposes every stored secret's codes.")
	flag.StringVar(&cfg.APIToken, "api-token", os.Getenv("TOTP_API_TOKEN"),
		"Bearer token required by the API. A random one is generated when empty. (env TOTP_API_TOKEN)")
	flag.Parse()

	if cfg.DBPath == "" {
		dir, err := storage.ConfigDir()
		if err != nil {
			return cfg, fmt.Errorf("locate config directory: %w", err)
		}

		cfg.DBPath = filepath.Join(dir, "server.db")
	}

	return cfg, nil
}
