package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/gdrte/totp-go/internal/accountdb"
	"github.com/gdrte/totp-go/internal/security"
	"github.com/gdrte/totp-go/internal/server"
	s "github.com/gdrte/totp-go/internal/storage"
	"github.com/gdrte/totp-go/internal/terminal"
	"github.com/urfave/cli/v2"
)

const defaultListenAddress = "127.0.0.1:8484"

// ServeCommand is the subcommand that starts the REST API.
func ServeCommand() *cli.Command {
	return &cli.Command{
		Name:  "serve",
		Usage: "Serve a REST API for generating codes and registering accounts.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "listen",
				Value: defaultListenAddress,
				Usage: "Address to listen on. Binding beyond localhost exposes every stored secret's codes.",
			},
			&cli.StringFlag{
				Name:    "api-token",
				EnvVars: []string{"TOTP_API_TOKEN"},
				Usage:   "Bearer token required by the API. A random one is generated when empty.",
			},
			&cli.StringFlag{
				Name:    "db",
				EnvVars: []string{"TOTP_SERVER_DB"},
				Usage:   "SQLite database file. (default: ~/.config/totp-cli/server.db)",
			},
			&cli.BoolFlag{
				Name:  "import",
				Usage: "Copy accounts from the CLI credentials file into the database before serving.",
			},
		},
		Action: func(ctx *cli.Context) error {
			term := terminal.New(os.Stdin, os.Stdout, os.Stderr)

			store, err := openServerDB(ctx.Context, term, ctx.String("db"))
			if err != nil {
				return err
			}
			defer store.Close()

			if ctx.Bool("import") {
				err = importCLIAccounts(ctx.Context, term, store)
				if err != nil {
					return err
				}
			}

			apiToken := ctx.String("api-token")
			if apiToken == "" {
				apiToken, err = server.RandomToken()
				if err != nil {
					return CommandError{Message: err.Error()}
				}

				fmt.Fprintf(os.Stderr, "Generated API token: %s\n", apiToken)
			}

			return runServer(ctx.Context, ctx.String("listen"), server.New(store, apiToken))
		},
	}
}

// openServerDB opens and unlocks the server database. The password comes
// from TOTP_PASS or a prompt, which asks twice when the database is new.
func openServerDB(ctx context.Context, term terminal.Terminal, path string) (*accountdb.DB, error) {
	if path == "" {
		dir, err := s.ConfigDir()
		if err != nil {
			return nil, err
		}

		path = filepath.Join(dir, "server.db")
	}

	store, err := accountdb.Open(ctx, path)
	if err != nil {
		return nil, CommandError{Message: err.Error()}
	}

	password, err := serverDBPassword(ctx, term, store)
	if err == nil {
		err = store.Unlock(ctx, password)
	}

	if err != nil {
		_ = store.Close()

		return nil, CommandError{Message: err.Error()}
	}

	fmt.Fprintf(os.Stderr, "Using database %s\n", path)

	return store, nil
}

func serverDBPassword(ctx context.Context, term terminal.Terminal, store *accountdb.DB) (string, error) {
	password := os.Getenv("TOTP_PASS")
	if password != "" {
		return password, nil
	}

	initialized, err := store.Initialized(ctx)
	if err != nil {
		return "", CommandError{Message: err.Error()}
	}

	if initialized {
		return term.Hidden("Database password:")
	}

	password, err = term.Hidden("New database password (do not forget it):")
	if err != nil {
		return "", err
	}

	confirm, err := term.Hidden("Confirm password:")
	if err != nil {
		return "", err
	}

	if password == "" || !security.CheckPasswordConfirm([]byte(password), []byte(confirm)) {
		return "", CommandError{Message: "passwords are empty or do not match"}
	}

	return password, nil
}

// importCLIAccounts copies every account from the CLI credentials file into
// the database, skipping ones that already exist there.
func importCLIAccounts(ctx context.Context, term terminal.Terminal, store *accountdb.DB) error {
	storage := prepareStorage(term)

	err := storage.Prepare()
	if err != nil {
		return err
	}

	imported, skipped := 0, 0

	for _, namespace := range storage.ListNamespaces() {
		for _, account := range namespace.Accounts {
			err = store.Add(ctx, namespace.Name, account)

			switch {
			case errors.Is(err, accountdb.ErrExists):
				skipped++
			case err != nil:
				return CommandError{Message: err.Error()}
			default:
				imported++
			}
		}
	}

	fmt.Fprintf(os.Stderr, "Imported %d accounts (%d already present).\n", imported, skipped)

	return nil
}

func runServer(parent context.Context, address string, srv *server.Server) error {
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", address)
	if err != nil {
		return CommandError{Message: err.Error()}
	}

	if !server.IsLoopback(listener.Addr()) {
		fmt.Fprintln(os.Stderr, "WARNING: listening on a non-loopback address; anyone with the token can generate codes.")
	}

	fmt.Fprintf(os.Stderr, "Serving API on http://%s (Ctrl+C to stop)\n", listener.Addr())

	err = srv.Serve(ctx, listener)
	if err != nil {
		return CommandError{Message: err.Error()}
	}

	return nil
}
