package server

import (
	"net/url"
	"strconv"
	"strings"
)

// otpAuthKey is the parsed content of an otpauth://totp/ URI, the format
// sites encode in their 2FA setup QR codes.
// See https://github.com/google/google-authenticator/wiki/Key-Uri-Format
type otpAuthKey struct {
	Issuer     string
	Account    string
	Secret     string
	Algorithm  string
	Length     uint
	TimePeriod int64
}

func parseOTPAuthURI(raw string) (otpAuthKey, error) {
	var key otpAuthKey

	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return key, RequestError{Message: "uri is not a valid URL"}
	}

	if parsed.Scheme != "otpauth" {
		return key, RequestError{Message: "uri must use the otpauth:// scheme"}
	}

	if parsed.Host != "totp" {
		return key, RequestError{Message: "only otpauth://totp/ URIs are supported"}
	}

	label := strings.TrimPrefix(parsed.Path, "/")

	issuer, account, found := strings.Cut(label, ":")
	if !found {
		issuer, account = "", label
	}

	query := parsed.Query()

	key.Issuer = firstNonEmpty(query.Get("issuer"), strings.TrimSpace(issuer))
	key.Account = strings.TrimSpace(account)
	key.Secret = query.Get("secret")
	key.Algorithm = strings.ToLower(query.Get("algorithm"))

	if digits := query.Get("digits"); digits != "" {
		value, err := strconv.ParseUint(digits, 10, 32)
		if err != nil {
			return key, RequestError{Message: "uri digits must be a positive integer"}
		}

		key.Length = uint(value)
	}

	if period := query.Get("period"); period != "" {
		value, err := strconv.ParseInt(period, 10, 64)
		if err != nil || value <= 0 {
			return key, RequestError{Message: "uri period must be a positive integer"}
		}

		key.TimePeriod = value
	}

	return key, nil
}
