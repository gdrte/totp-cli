package cmd

import (
	"fmt"
	"time"

	"github.com/yitsushi/totp-cli/internal/security"
	"github.com/yitsushi/totp-cli/internal/security/algo"
	s "github.com/yitsushi/totp-cli/internal/storage"
)

func formatCode(code string, remaining int64, showRemaining bool) string {
	if showRemaining {
		return fmt.Sprintf("%s (remaining time: %ds)", code, remaining)
	}

	return code
}

func generateCode(account *s.Account) (string, int64, error) {
	code, remaining, err := security.GenerateOTPCode(security.GenerateOptions{
		Token:      account.Token,
		When:       time.Now(),
		Length:     account.Length,
		Algorithm:  algo.ByName(account.Algorithm),
		TimePeriod: account.TimePeriod,
	})
	if err != nil {
		return "", 0, err
	}

	if account.Prefix != "" {
		code = account.Prefix + code
	}

	return code, remaining, nil
}
