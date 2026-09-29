// Package server exposes the TOTP storage over a small REST API with an
// embedded web UI, so automated tests and agents can fetch codes without
// driving the interactive CLI.
package server

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/yitsushi/totp-cli/internal/accountdb"
	"github.com/yitsushi/totp-cli/internal/storage"
)

const (
	maxRequestBodyBytes = 64 * 1024
	bearerPrefix        = "Bearer "
)

//go:embed ui
var uiFiles embed.FS

// WaitFunc blocks for the given duration or until the context is done.
type WaitFunc func(ctx context.Context, d time.Duration) error

// Store is the account persistence the server needs. It is implemented by
// accountdb.DB, and must be safe for concurrent use.
type Store interface {
	List(ctx context.Context) ([]accountdb.Entry, error)
	Get(ctx context.Context, namespace, name string) (*storage.Account, error)
	Add(ctx context.Context, namespace string, account *storage.Account) error
	Delete(ctx context.Context, namespace, name string) error
}

// Server serves the REST API and web UI on top of a Store. The store is
// queried on every request, so there is no in-memory copy to go stale.
type Server struct {
	store    Store
	now      func() time.Time
	wait     WaitFunc
	apiToken string
}

// Option configures a Server.
type Option func(*Server)

// WithClock overrides the time source. Intended for tests.
func WithClock(now func() time.Time) Option {
	return func(s *Server) {
		s.now = now
	}
}

// WithWaitFunc overrides how the server sleeps while waiting for a fresh
// code window. Intended for tests.
func WithWaitFunc(wait WaitFunc) Option {
	return func(s *Server) {
		s.wait = wait
	}
}

// New creates a Server. Every /api/v1 endpoint except health requires the
// request to carry "Authorization: Bearer <apiToken>".
func New(store Store, apiToken string, opts ...Option) *Server {
	srv := &Server{
		store:    store,
		apiToken: apiToken,
		now:      time.Now,
		wait:     sleepContext,
	}

	for _, o := range opts {
		o(srv)
	}

	return srv
}

// Handler returns the HTTP handler for the API and UI.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.Handle("GET /api/v1/accounts", s.requireToken(s.handleListAccounts))
	mux.Handle("POST /api/v1/accounts", s.requireToken(s.handleAddAccount))
	mux.Handle("DELETE /api/v1/accounts/{namespace}/{account}", s.requireToken(s.handleDeleteAccount))
	mux.Handle("GET /api/v1/accounts/{namespace}/{account}/code", s.requireToken(s.handleGetCode))

	uiRoot, _ := fs.Sub(uiFiles, "ui")
	mux.Handle("GET /", http.FileServerFS(uiRoot))

	return securityHeaders(mux)
}

func (s *Server) requireToken(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		header := req.Header.Get("Authorization")

		token, found := strings.CutPrefix(header, bearerPrefix)
		if !found || subtle.ConstantTimeCompare([]byte(token), []byte(s.apiToken)) != 1 {
			writeError(resp, http.StatusUnauthorized, "missing or invalid bearer token")

			return
		}

		next(resp, req)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		headers := resp.Header()
		headers.Set("X-Content-Type-Options", "nosniff")
		headers.Set("X-Frame-Options", "DENY")
		headers.Set("Referrer-Policy", "no-referrer")
		headers.Set("Cache-Control", "no-store")
		headers.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'")

		next.ServeHTTP(resp, req)
	})
}

func writeJSON(resp http.ResponseWriter, status int, body any) {
	encoded, err := json.Marshal(body)
	if err != nil {
		http.Error(resp, "failed to encode response", http.StatusInternalServerError)

		return
	}

	resp.Header().Set("Content-Type", "application/json")
	resp.WriteHeader(status)

	_, _ = resp.Write(append(encoded, '\n'))
}

func writeError(resp http.ResponseWriter, status int, message string) {
	writeJSON(resp, status, errorResponse{Error: message})
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return fmt.Errorf("wait interrupted: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}
