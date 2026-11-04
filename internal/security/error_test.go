package security_test

import (
	"testing"

	"github.com/gdrte/totp-go/internal/security"
	"github.com/stretchr/testify/suite"
)

func TestSecurityError(t *testing.T) {
	suite.Run(t, &SecurityErrorTestSuite{})
}

type SecurityErrorTestSuite struct {
	suite.Suite
}

func (suite *SecurityErrorTestSuite) TestOTPErrorMessage() {
	err := security.OTPError{Message: "bad input"}
	suite.Equal("otp error: bad input", err.Error())
}
