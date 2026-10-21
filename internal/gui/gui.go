//go:build gui

// Package gui is a Gio desktop app on top of the server database. It
// registers accounts, shows live codes, and serves the same REST API as
// `totp-cli serve` while the window is open.
//
// Gio needs cgo on macOS and Linux, so the package only builds with the
// "gui" build tag and the pure-Go CLI is unaffected.
package gui

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gioui.org/app"
	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/gdrte/totp-go/internal/accountdb"
	"github.com/gdrte/totp-go/internal/security"
	"github.com/gdrte/totp-go/internal/server"
)

const (
	modeSecret = "secret"
	modeURI    = "uri"
	// deleteConfirmWindow is how long a Delete click stays armed.
	deleteConfirmWindow = 3 * time.Second
	// codeErrorRetry is how long to wait before retrying a failed code.
	codeErrorRetry = 5 * time.Second
	// curlMinRemaining is the min_remaining used in copied curl commands.
	curlMinRemaining = 5
	// reloadInterval is how often the account list is re-read.
	reloadInterval = 3 * time.Second
	// codeError is shown in place of a code that could not be generated.
	codeError = "error"
)

// Config configures the app.
type Config struct {
	// DBPath is the SQLite database file, shared with `totp-cli serve`.
	DBPath string
	// Listen is the API address. Empty disables the API.
	Listen string
	// APIToken is the API bearer token. A random one is generated when empty.
	APIToken string
	// Password, when set, is tried to unlock the database before asking.
	Password string
}

// Run opens the window and blocks until it is closed. It must not run on
// the main goroutine, which belongs to app.Main.
func Run(ctx context.Context, cfg Config) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	store, err := accountdb.Open(ctx, cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer store.Close()

	initialized, err := store.Initialized(ctx)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	win := new(app.Window)
	win.Option(app.Title("totp-cli"), app.Size(unit.Dp(760), unit.Dp(820)), app.MinSize(unit.Dp(520), unit.Dp(480)))

	ui := newUI(ctx, cfg, store, win, initialized)

	if cfg.Password != "" {
		ui.unlock(cfg.Password)
	}

	ui.loop()
	cancel()

	if ui.apiDone == nil {
		return nil
	}

	return <-ui.apiDone
}

type ui struct {
	ctx context.Context //nolint:containedctx // the window's lifetime, canceled when it closes
	// apiDone reports the API server's shutdown; nil until it starts.
	apiDone     chan error
	store       *accountdb.DB
	win         *app.Window
	th          *material.Theme
	results     chan func()
	nextReload  time.Time
	advanced    widget.Bool
	registerBtn widget.Clickable
	copyTok     widget.Clickable
	unlockBtn   widget.Clickable
	cfg         Config
	status      string
	apiToken    string
	apiAddr     string
	apiNote     string
	uri         widget.Editor
	prefix      widget.Editor
	period      widget.Editor
	digits      widget.Editor
	password    widget.Editor
	namespace   widget.Editor
	account     widget.Editor
	secret      widget.Editor
	confirm     widget.Editor
	algorithm   widget.Enum
	mode        widget.Enum
	rows        []*row
	list        widget.List
	mono        font.Font
	busy        bool
	unlocked    bool
	initialized bool
	loaded      bool
	statusErr   bool
}

// row is one account in the list. Only the generated code is kept; the
// secret is read from the database whenever a new code is needed.
type row struct {
	expiresAt time.Time
	armedAt   time.Time
	clickCode widget.Clickable
	copyCode  widget.Clickable
	copyCurl  widget.Clickable
	del       widget.Clickable
	code      string
	entry     accountdb.Entry
	fetching  bool
}

func newUI(ctx context.Context, cfg Config, store *accountdb.DB, win *app.Window, initialized bool) *ui {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))
	th.TextSize = unit.Sp(14)
	th.ContrastBg = colorAccent

	u := &ui{
		ctx:         ctx,
		cfg:         cfg,
		store:       store,
		win:         win,
		th:          th,
		mono:        font.Font{Typeface: "Go Mono"},
		results:     make(chan func(), 16),
		initialized: initialized,
	}

	u.list.Axis = layout.Vertical
	u.mode.Value = modeSecret

	for _, e := range []*widget.Editor{
		&u.password, &u.confirm, &u.namespace, &u.account, &u.secret, &u.uri, &u.prefix, &u.digits, &u.period,
	} {
		e.SingleLine = true
	}

	u.password.Mask, u.confirm.Mask = '•', '•'
	u.password.Submit, u.confirm.Submit = true, true
	u.digits.Filter, u.period.Filter = "0123456789", "0123456789"

	return u
}

// loop handles window events until the window is closed.
func (u *ui) loop() {
	var ops op.Ops

	for {
		switch e := u.win.Event().(type) {
		case app.DestroyEvent:
			return
		case app.FrameEvent:
			u.drainResults()

			gtx := app.NewContext(&ops, e)
			u.layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

// async runs work off the UI goroutine. The function it returns is applied
// to the UI state on the UI goroutine before the next frame.
func (u *ui) async(work func() func()) {
	go func() {
		apply := work()

		select {
		case u.results <- apply:
			u.win.Invalidate()
		case <-u.ctx.Done():
		}
	}()
}

func (u *ui) drainResults() {
	for {
		select {
		case apply := <-u.results:
			apply()
		default:
			return
		}
	}
}

func (u *ui) setStatus(message string, isErr bool) {
	u.status, u.statusErr = message, isErr
}

func (u *ui) submitPassword() {
	password := u.password.Text()

	if !u.initialized && (password == "" || !security.CheckPasswordConfirm([]byte(password), []byte(u.confirm.Text()))) {
		u.setStatus("Passwords are empty or do not match.", true)

		return
	}

	u.unlock(password)
}

func (u *ui) unlock(password string) {
	u.busy = true
	u.setStatus("Unlocking…", false)

	u.async(func() func() {
		err := u.store.Unlock(u.ctx, password)

		return func() {
			u.busy = false

			if err != nil {
				msg := "Unlock failed: " + err.Error()
				if errors.Is(err, accountdb.ErrWrongPassword) {
					msg = "Wrong password."
				}

				u.setStatus(msg, true)

				return
			}

			u.unlocked, u.initialized = true, true
			u.password.SetText("")
			u.confirm.SetText("")
			u.setStatus("", false)
			u.startAPI()
			u.reload()
		}
	})
}

// startAPI serves the REST API until the window closes. A failure to
// listen is shown in the header; the app keeps working without the API.
func (u *ui) startAPI() {
	if u.cfg.Listen == "" {
		u.apiNote = "API disabled."

		return
	}

	u.apiToken = u.cfg.APIToken
	if u.apiToken == "" {
		token, err := server.RandomToken()
		if err != nil {
			u.apiNote = "API not running: " + err.Error()

			return
		}

		u.apiToken = token
	}

	listener, err := (&net.ListenConfig{}).Listen(u.ctx, "tcp", u.cfg.Listen)
	if err != nil {
		u.apiNote = "API not running: " + err.Error()

		return
	}

	u.apiAddr = listener.Addr().String()
	if !server.IsLoopback(listener.Addr()) {
		u.apiNote = "Listening beyond localhost: anyone with the token can generate codes."
	}

	u.apiDone = make(chan error, 1)
	srv := server.New(u.store, u.apiToken)

	go func() {
		u.apiDone <- srv.Serve(u.ctx, listener)
	}()
}

// reload reads the account list from the database.
func (u *ui) reload() {
	u.nextReload = time.Now().Add(reloadInterval)

	u.async(func() func() {
		entries, err := u.store.List(u.ctx)

		return func() {
			u.loaded = true

			if err != nil {
				u.setStatus("Failed to load accounts: "+err.Error(), true)

				return
			}

			previous := make(map[string]*row, len(u.rows))
			for _, r := range u.rows {
				previous[rowKey(r.entry)] = r
			}

			u.rows = u.rows[:0]

			for _, entry := range entries {
				r, ok := previous[rowKey(entry)]
				if !ok {
					r = &row{}
				}

				r.entry = entry
				u.rows = append(u.rows, r)
			}
		}
	})
}

// refreshCode generates a new code for the row once its current one expires.
func (u *ui) refreshCode(r *row, now time.Time) {
	if r.fetching || now.Before(r.expiresAt) {
		return
	}

	r.fetching = true
	entry := r.entry

	u.async(func() func() {
		var (
			code      string
			remaining int64
			when      = time.Now()
		)

		account, err := u.store.Get(u.ctx, entry.Namespace, entry.Name)
		if err == nil {
			code, remaining, err = server.GenerateCode(account, when)
		}

		return func() {
			r.fetching = false

			if err != nil {
				r.code = codeError
				r.expiresAt = time.Now().Add(codeErrorRetry)

				return
			}

			r.code = code
			r.expiresAt = when.Truncate(time.Second).Add(time.Duration(remaining) * time.Second)
		}
	})
}

func (u *ui) register() {
	req := server.AccountRequest{
		Namespace: strings.TrimSpace(u.namespace.Text()),
		Account:   strings.TrimSpace(u.account.Text()),
		Prefix:    strings.TrimSpace(u.prefix.Text()),
		Algorithm: u.algorithm.Value,
	}

	if u.mode.Value == modeURI {
		req.URI = strings.TrimSpace(u.uri.Text())
	} else {
		req.Secret = strings.TrimSpace(u.secret.Text())
	}

	length, err := parseOptionalUint(u.digits.Text())
	if err != nil {
		u.setStatus("Digits must be a number.", true)

		return
	}

	period, err := parseOptionalInt(u.period.Text())
	if err != nil {
		u.setStatus("Period must be a number.", true)

		return
	}

	req.Length, req.TimePeriod = uint(length), period

	nsName, account, err := req.Build()
	if err != nil {
		u.setStatus("Register failed: "+err.Error(), true)

		return
	}

	u.async(func() func() {
		err := u.store.Add(u.ctx, nsName, account)

		return func() {
			switch {
			case errors.Is(err, accountdb.ErrExists):
				u.setStatus(fmt.Sprintf("%s/%s already exists.", nsName, account.Name), true)
			case err != nil:
				u.setStatus("Register failed: "+err.Error(), true)
			default:
				u.setStatus(fmt.Sprintf("Registered %s/%s.", nsName, account.Name), false)
				u.resetForm()
				u.reload()
			}
		}
	})
}

func (u *ui) resetForm() {
	for _, e := range []*widget.Editor{&u.namespace, &u.account, &u.secret, &u.uri, &u.prefix, &u.digits, &u.period} {
		e.SetText("")
	}

	u.algorithm.Value = ""
}

func (u *ui) deleteAccount(entry accountdb.Entry) {
	u.async(func() func() {
		err := u.store.Delete(u.ctx, entry.Namespace, entry.Name)

		return func() {
			if err != nil {
				u.setStatus("Delete failed: "+err.Error(), true)

				return
			}

			u.setStatus(fmt.Sprintf("Deleted %s/%s.", entry.Namespace, entry.Name), false)
			u.reload()
		}
	})
}

// curlCommand returns a command agents can run to fetch the row's code.
func (u *ui) curlCommand(entry accountdb.Entry) string {
	return fmt.Sprintf(
		`curl -s -H "Authorization: Bearer $TOTP_API_TOKEN" 'http://%s/api/v1/accounts/%s/%s/code?min_remaining=%d'`,
		u.apiAddr, url.PathEscape(entry.Namespace), url.PathEscape(entry.Name), curlMinRemaining,
	)
}

func rowKey(entry accountdb.Entry) string {
	return entry.Namespace + "\x00" + entry.Name
}

// parseOptionalUint parses a numeric field, treating an empty one as zero.
func parseOptionalUint(raw string) (uint64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}

	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %q: %w", raw, err)
	}

	return value, nil
}

// parseOptionalInt parses a numeric field, treating an empty one as zero.
func parseOptionalInt(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}

	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %q: %w", raw, err)
	}

	return value, nil
}
