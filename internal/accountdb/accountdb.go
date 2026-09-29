// Package accountdb is a SQLite-backed account store for the REST server.
//
// Account metadata is stored in plain columns so it can be listed without
// the password. Each account's token and prefix are encrypted with age to an
// X25519 key generated when the database is created. That key is itself
// stored encrypted with the user's password (age scrypt), so a copy of the
// database file alone reveals no secrets.
package accountdb

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"filippo.io/age"
	_ "github.com/glebarez/go-sqlite" // registers the "sqlite" database/sql driver
	"github.com/yitsushi/totp-cli/internal/storage"
)

const (
	dbFilePermissions = 0o600
	identityMetaKey   = "identity"
	// defaultScryptWorkFactor matches the CLI's storage file: ~100ms to unlock.
	defaultScryptWorkFactor = 15
	dsnPragmas              = "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
)

var (
	// ErrNotFound is returned when the requested account does not exist.
	ErrNotFound = errors.New("account not found")
	// ErrExists is returned when adding an account that already exists.
	ErrExists = errors.New("account already exists")
	// ErrLocked is returned when secrets are accessed before Unlock.
	ErrLocked = errors.New("database is locked")
	// ErrWrongPassword is returned by Unlock when the password is incorrect.
	ErrWrongPassword = errors.New("wrong database password")
	// ErrInvalidPath is returned by Open for in-memory or URI style paths.
	ErrInvalidPath = errors.New("database path must be a plain file path")
)

const schema = `
CREATE TABLE IF NOT EXISTS meta (
	key   TEXT PRIMARY KEY,
	value BLOB NOT NULL
);

CREATE TABLE IF NOT EXISTS accounts (
	namespace   TEXT    NOT NULL,
	name        TEXT    NOT NULL,
	algorithm   TEXT    NOT NULL,
	length      INTEGER NOT NULL,
	time_period INTEGER NOT NULL,
	has_prefix  INTEGER NOT NULL,
	secret      BLOB    NOT NULL,
	created_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
	PRIMARY KEY (namespace, name)
);
`

// Entry is the non-secret metadata of an account.
type Entry struct {
	Namespace  string
	Name       string
	Algorithm  string
	Length     uint
	TimePeriod int64
	HasPrefix  bool
}

// EntryOf returns the metadata of an account under the given namespace.
func EntryOf(namespace string, account *storage.Account) Entry {
	return Entry{
		Namespace:  namespace,
		Name:       account.Name,
		Algorithm:  account.Algorithm,
		Length:     account.Length,
		TimePeriod: account.TimePeriod,
		HasPrefix:  account.Prefix != "",
	}
}

// secretPayload is the encrypted part of an account row.
type secretPayload struct {
	Token  string `json:"token"`
	Prefix string `json:"prefix"`
}

// DB is an account store backed by a SQLite file.
type DB struct {
	conn       *sql.DB
	recipient  age.Recipient
	identity   age.Identity
	workFactor int
}

// Option configures a DB.
type Option func(*DB)

// WithScryptWorkFactor sets the scrypt work factor used when a new database
// key is created. Lower values are only appropriate for tests.
func WithScryptWorkFactor(factor int) Option {
	return func(db *DB) {
		db.workFactor = factor
	}
}

// Open opens (creating if needed) the database at path. The returned DB can
// list accounts right away; call Unlock before reading or writing secrets.
func Open(ctx context.Context, path string, opts ...Option) (*DB, error) {
	err := ensureFile(path)
	if err != nil {
		return nil, err
	}

	conn, err := sql.Open("sqlite", path+dsnPragmas)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	_, err = conn.ExecContext(ctx, schema)
	if err != nil {
		_ = conn.Close()

		return nil, fmt.Errorf("create schema: %w", err)
	}

	store := &DB{conn: conn, workFactor: defaultScryptWorkFactor}

	for _, o := range opts {
		o(store)
	}

	return store, nil
}

// Close closes the database.
func (db *DB) Close() error {
	err := db.conn.Close()
	if err != nil {
		return fmt.Errorf("close database: %w", err)
	}

	return nil
}

// Initialized reports whether the database already has a key, i.e. whether
// Unlock will check the password rather than set it.
func (db *DB) Initialized(ctx context.Context) (bool, error) {
	var count int

	err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM meta WHERE key = ?`, identityMetaKey).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("read meta: %w", err)
	}

	return count > 0, nil
}

// Unlock decrypts the database key with password. On a new database it
// generates the key and protects it with password.
func (db *DB) Unlock(ctx context.Context, password string) error {
	err := db.createIdentityIfMissing(ctx, password)
	if err != nil {
		return err
	}

	var encrypted []byte

	err = db.conn.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, identityMetaKey).Scan(&encrypted)
	if err != nil {
		return fmt.Errorf("read database key: %w", err)
	}

	scryptIdentity, err := age.NewScryptIdentity(password)
	if err != nil {
		return fmt.Errorf("prepare password: %w", err)
	}

	plain, err := decrypt(encrypted, scryptIdentity)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrWrongPassword, err)
	}

	identity, err := age.ParseX25519Identity(string(plain))
	if err != nil {
		return fmt.Errorf("parse database key: %w", err)
	}

	db.identity = identity
	db.recipient = identity.Recipient()

	return nil
}

// List returns the metadata of every account, ordered by namespace and name.
// It does not require Unlock.
func (db *DB) List(ctx context.Context) ([]Entry, error) {
	rows, err := db.conn.QueryContext(ctx, `
		SELECT namespace, name, algorithm, length, time_period, has_prefix
		FROM accounts ORDER BY namespace, name`)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	defer rows.Close()

	entries := []Entry{}

	for rows.Next() {
		var entry Entry

		err = rows.Scan(&entry.Namespace, &entry.Name, &entry.Algorithm,
			&entry.Length, &entry.TimePeriod, &entry.HasPrefix)
		if err != nil {
			return nil, fmt.Errorf("list accounts: %w", err)
		}

		entries = append(entries, entry)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}

	return entries, nil
}

// Get returns the account, including its decrypted token and prefix.
func (db *DB) Get(ctx context.Context, namespace, name string) (*storage.Account, error) {
	if db.identity == nil {
		return nil, ErrLocked
	}

	account := &storage.Account{Name: name}

	var encrypted []byte

	err := db.conn.QueryRowContext(
		ctx, `
		SELECT algorithm, length, time_period, secret
		FROM accounts WHERE namespace = ? AND name = ?`, namespace, name,
	).Scan(&account.Algorithm, &account.Length, &account.TimePeriod, &encrypted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("get account: %w", err)
	}

	plain, err := decrypt(encrypted, db.identity)
	if err != nil {
		return nil, fmt.Errorf("decrypt account secret: %w", err)
	}

	var payload secretPayload

	err = json.Unmarshal(plain, &payload)
	if err != nil {
		return nil, fmt.Errorf("decode account secret: %w", err)
	}

	account.Token = payload.Token
	account.Prefix = payload.Prefix

	return account, nil
}

// Add stores a new account. It returns ErrExists if the namespace already
// has an account with the same name.
func (db *DB) Add(ctx context.Context, namespace string, account *storage.Account) error {
	if db.recipient == nil {
		return ErrLocked
	}

	plain, err := json.Marshal(secretPayload{Token: account.Token, Prefix: account.Prefix})
	if err != nil {
		return fmt.Errorf("encode account secret: %w", err)
	}

	encrypted, err := encrypt(plain, db.recipient)
	if err != nil {
		return err
	}

	result, err := db.conn.ExecContext(ctx, `
		INSERT INTO accounts (namespace, name, algorithm, length, time_period, has_prefix, secret)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (namespace, name) DO NOTHING`,
		namespace, account.Name, account.Algorithm, account.Length, account.TimePeriod,
		account.Prefix != "", encrypted)
	if err != nil {
		return fmt.Errorf("add account: %w", err)
	}

	return requireAffected(result, ErrExists)
}

// Delete removes an account. It returns ErrNotFound if it does not exist.
func (db *DB) Delete(ctx context.Context, namespace, name string) error {
	result, err := db.conn.ExecContext(ctx,
		`DELETE FROM accounts WHERE namespace = ? AND name = ?`, namespace, name)
	if err != nil {
		return fmt.Errorf("delete account: %w", err)
	}

	return requireAffected(result, ErrNotFound)
}

// createIdentityIfMissing stores a new password-protected key unless one
// exists. ON CONFLICT keeps the first key if two processes race.
func (db *DB) createIdentityIfMissing(ctx context.Context, password string) error {
	initialized, err := db.Initialized(ctx)
	if err != nil || initialized {
		return err
	}

	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return fmt.Errorf("generate database key: %w", err)
	}

	scryptRecipient, err := age.NewScryptRecipient(password)
	if err != nil {
		return fmt.Errorf("prepare password: %w", err)
	}

	scryptRecipient.SetWorkFactor(db.workFactor)

	encrypted, err := encrypt([]byte(identity.String()), scryptRecipient)
	if err != nil {
		return err
	}

	_, err = db.conn.ExecContext(ctx,
		`INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT (key) DO NOTHING`,
		identityMetaKey, encrypted)
	if err != nil {
		return fmt.Errorf("store database key: %w", err)
	}

	return nil
}

func requireAffected(result sql.Result, errIfNone error) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read affected rows: %w", err)
	}

	if affected == 0 {
		return errIfNone
	}

	return nil
}

func encrypt(plain []byte, recipient age.Recipient) ([]byte, error) {
	var out bytes.Buffer

	writer, err := age.Encrypt(&out, recipient)
	if err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}

	_, err = writer.Write(plain)
	if err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}

	err = writer.Close()
	if err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}

	return out.Bytes(), nil
}

func decrypt(encrypted []byte, identity age.Identity) ([]byte, error) {
	reader, err := age.Decrypt(bytes.NewReader(encrypted), identity)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	plain, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	return plain, nil
}

// ensureFile creates the database file with owner-only permissions so
// SQLite (and its -wal/-shm files, which copy the mode) never makes it
// world-readable.
func ensureFile(path string) error {
	if strings.HasPrefix(path, ":memory:") || strings.HasPrefix(path, "file:") {
		return ErrInvalidPath
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, dbFilePermissions)
	if err != nil {
		return fmt.Errorf("create database file: %w", err)
	}

	err = file.Close()
	if err != nil {
		return fmt.Errorf("create database file: %w", err)
	}

	return nil
}
