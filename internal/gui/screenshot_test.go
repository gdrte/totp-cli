//go:build gui && screenshots

package gui

// Renders the README screenshots offscreen with sample accounts:
//
//	make screenshots
//
// It writes PNGs to the directory in SCREENSHOT_DIR (docs/screenshots).

import (
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"github.com/gdrte/totp-go/internal/accountdb"
	"github.com/gdrte/totp-go/internal/server"
	"github.com/gdrte/totp-go/internal/storage"
)

const (
	shotScale = 2 // pixels per dp, for sharp images on high-DPI screens
	shotWidth = 760
)

func TestScreenshots(t *testing.T) {
	dir := os.Getenv("SCREENSHOT_DIR")
	if dir == "" {
		t.Skip("SCREENSHOT_DIR not set")
	}

	ctx := context.Background()

	store, err := accountdb.Open(ctx, filepath.Join(t.TempDir(), "server.db"), accountdb.WithScryptWorkFactor(1))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.Unlock(ctx, "password"); err != nil {
		t.Fatal(err)
	}

	samples := []struct {
		namespace string
		account   *storage.Account
	}{
		{"staging", &storage.Account{Name: "qa-bot@example.com", Token: "JBSWY3DPEHPK3PXP"}},
		{"staging", &storage.Account{Name: "admin@example.com", Token: "GEZDGNBVGY3TQOJQ", Algorithm: "sha256", Length: 8}},
		{"GitHub", &storage.Account{Name: "ci@example.com", Token: "KRUGKIDROVUWG2ZAMJZG653OEBTG66BA"}},
		{"personal", &storage.Account{Name: "bank", Token: "MFRGGZDFMZTWQ2LK", Prefix: "4821"}},
	}

	for _, s := range samples {
		if err := store.Add(ctx, s.namespace, s.account); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 11, 4, 9, 41, 0, 0, time.UTC)
	cfg := Config{DBPath: "~/.config/totp-cli/server.db", Listen: "127.0.0.1:8484"}
	u := newUI(ctx, cfg, store, nil, true)
	u.unlocked, u.loaded = true, true
	u.apiAddr, u.apiToken = "127.0.0.1:8484", "example-token"
	u.nextReload = now.Add(time.Hour)

	// Stagger the countdowns so one bar shows the red, about-to-expire state.
	lefts := []time.Duration{22 * time.Second, 3 * time.Second, 14 * time.Second, 27 * time.Second}

	for i, entry := range entries {
		account, err := store.Get(ctx, entry.Namespace, entry.Name)
		if err != nil {
			t.Fatal(err)
		}

		code, _, err := server.GenerateCode(account, now)
		if err != nil {
			t.Fatal(err)
		}

		u.rows = append(u.rows, &row{entry: entry, code: code, expiresAt: now.Add(lefts[i%len(lefts)])})
	}

	shoot(t, u, now, 712, filepath.Join(dir, "desktop-app.png"))

	u.showHelp = true
	u.helpTopic = helpMCP
	shoot(t, u, now, 900, filepath.Join(dir, "help-mcp.png"))
}

// shoot renders the UI at shotWidth by height dp and saves it as a PNG.
func shoot(t *testing.T, u *ui, now time.Time, height int, path string) {
	t.Helper()

	size := image.Pt(shotWidth*shotScale, height*shotScale)

	win, err := headless.NewWindow(size.X, size.Y)
	if err != nil {
		t.Skip(err)
	}
	defer win.Release()

	var (
		ops    op.Ops
		router input.Router
	)

	gtx := layout.Context{
		Ops:         &ops,
		Now:         now,
		Metric:      unit.Metric{PxPerDp: shotScale, PxPerSp: shotScale},
		Constraints: layout.Exact(size),
		Source:      router.Source(),
	}
	u.layout(gtx)

	if err := win.Frame(&ops); err != nil {
		t.Fatal(err)
	}

	img := image.NewRGBA(image.Rectangle{Max: size})
	if err := win.Screenshot(img); err != nil {
		t.Fatal(err)
	}

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
