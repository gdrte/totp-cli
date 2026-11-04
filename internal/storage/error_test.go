package storage_test

import (
	"testing"

	"github.com/gdrte/totp-go/internal/storage"
	"github.com/stretchr/testify/suite"
)

func TestStorageError(t *testing.T) {
	suite.Run(t, &StorageErrorTestSuite{})
}

type StorageErrorTestSuite struct {
	suite.Suite
}

func (suite *StorageErrorTestSuite) TestNotFoundErrorMessage() {
	err := storage.NotFoundError{Type: "namespace", Name: "foo"}
	suite.Equal("namespace not found: foo", err.Error())
}

func (suite *StorageErrorTestSuite) TestBackendErrorMessage() {
	err := storage.BackendError{Message: "disk full"}
	suite.Equal("storage error: disk full", err.Error())
}
