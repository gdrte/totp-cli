package accountdb

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gdrte/totp-go/internal/storage"
	"github.com/stretchr/testify/suite"
)

const (
	testPassword = "correct horse"
	// Minimum scrypt cost keeps the suite fast; production uses 15.
	testWorkFactor = 1
)

func TestAccountDB(t *testing.T) {
	suite.Run(t, &AccountDBTestSuite{})
}

type AccountDBTestSuite struct {
	suite.Suite

	path string
	db   *DB
}

func (suite *AccountDBTestSuite) SetupTest() {
	suite.path = filepath.Join(suite.T().TempDir(), "server.db")
	suite.db = suite.open()
	suite.Require().NoError(suite.db.Unlock(context.Background(), testPassword))
}

func (suite *AccountDBTestSuite) TearDownTest() {
	suite.Require().NoError(suite.db.Close())
}

func (suite *AccountDBTestSuite) open() *DB {
	suite.T().Helper()

	db, err := Open(context.Background(), suite.path, WithScryptWorkFactor(testWorkFactor))
	suite.Require().NoError(err)

	return db
}

func (suite *AccountDBTestSuite) TestFilePermissions() {
	info, err := os.Stat(suite.path)
	suite.Require().NoError(err)
	suite.Equal(os.FileMode(dbFilePermissions), info.Mode().Perm())
}

func (suite *AccountDBTestSuite) TestRoundTripPersistsAcrossReopen() {
	ctx := context.Background()
	account := &storage.Account{
		Name: "bot", Token: "JBSWY3DPEHPK3PXP", Prefix: "pin-",
		Algorithm: "sha256", Length: 8, TimePeriod: 60,
	}

	suite.Require().NoError(suite.db.Add(ctx, "staging", account))

	reopened := suite.open()
	defer reopened.Close()

	initialized, err := reopened.Initialized(ctx)
	suite.Require().NoError(err)
	suite.True(initialized)

	suite.Require().NoError(reopened.Unlock(ctx, testPassword))

	got, err := reopened.Get(ctx, "staging", "bot")
	suite.Require().NoError(err)
	suite.Equal(account, got)
}

func (suite *AccountDBTestSuite) TestSecretsAreNotStoredInPlaintext() {
	suite.Require().NoError(suite.db.Add(context.Background(), "staging",
		&storage.Account{Name: "bot", Token: "JBSWY3DPEHPK3PXP", Prefix: "mysecretprefix"}))
	suite.Require().NoError(suite.db.Close())

	raw, err := os.ReadFile(suite.path)
	suite.Require().NoError(err)

	wal, _ := os.ReadFile(suite.path + "-wal")
	raw = append(raw, wal...)

	suite.NotContains(string(raw), "JBSWY3DPEHPK3PXP")
	suite.NotContains(string(raw), "mysecretprefix")

	suite.db = suite.open()
}

func (suite *AccountDBTestSuite) TestWrongPasswordIsRejected() {
	reopened := suite.open()
	defer reopened.Close()

	err := reopened.Unlock(context.Background(), "wrong")
	suite.Require().ErrorIs(err, ErrWrongPassword)
}

func (suite *AccountDBTestSuite) TestLockedDatabase() {
	ctx := context.Background()
	suite.Require().NoError(suite.db.Add(ctx, "ns", &storage.Account{Name: "a", Token: "JBSWY3DPEHPK3PXP"}))

	locked := suite.open()
	defer locked.Close()

	entries, err := locked.List(ctx)
	suite.Require().NoError(err)
	suite.Len(entries, 1)

	_, err = locked.Get(ctx, "ns", "a")
	suite.Require().ErrorIs(err, ErrLocked)

	err = locked.Add(ctx, "ns", &storage.Account{Name: "b", Token: "JBSWY3DPEHPK3PXP"})
	suite.Require().ErrorIs(err, ErrLocked)
}

func (suite *AccountDBTestSuite) TestAddDuplicate() {
	ctx := context.Background()
	account := &storage.Account{Name: "a", Token: "JBSWY3DPEHPK3PXP"}

	suite.Require().NoError(suite.db.Add(ctx, "ns", account))
	suite.Require().ErrorIs(suite.db.Add(ctx, "ns", account), ErrExists)
	suite.Require().NoError(suite.db.Add(ctx, "other", account))
}

func (suite *AccountDBTestSuite) TestListOrderedWithoutSecrets() {
	ctx := context.Background()
	suite.Require().NoError(suite.db.Add(ctx, "b", &storage.Account{Name: "z", Token: "JBSWY3DPEHPK3PXP"}))
	suite.Require().NoError(suite.db.Add(ctx, "a", &storage.Account{Name: "y", Token: "JBSWY3DPEHPK3PXP", Prefix: "p"}))
	suite.Require().NoError(suite.db.Add(ctx, "b", &storage.Account{Name: "x", Token: "JBSWY3DPEHPK3PXP"}))

	entries, err := suite.db.List(ctx)
	suite.Require().NoError(err)
	suite.Equal([]Entry{
		{Namespace: "a", Name: "y", HasPrefix: true},
		{Namespace: "b", Name: "x"},
		{Namespace: "b", Name: "z"},
	}, entries)
}

func (suite *AccountDBTestSuite) TestDelete() {
	ctx := context.Background()
	suite.Require().NoError(suite.db.Add(ctx, "ns", &storage.Account{Name: "a", Token: "JBSWY3DPEHPK3PXP"}))

	suite.Require().NoError(suite.db.Delete(ctx, "ns", "a"))
	suite.Require().ErrorIs(suite.db.Delete(ctx, "ns", "a"), ErrNotFound)

	_, err := suite.db.Get(ctx, "ns", "a")
	suite.Require().ErrorIs(err, ErrNotFound)
}

func (suite *AccountDBTestSuite) TestRejectsNonFilePaths() {
	_, err := Open(context.Background(), ":memory:")
	suite.Require().ErrorIs(err, ErrInvalidPath)
}
