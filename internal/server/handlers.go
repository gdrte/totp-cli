package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gdrte/totp-go/internal/accountdb"
	"github.com/gdrte/totp-go/internal/security"
	"github.com/gdrte/totp-go/internal/security/algo"
	"github.com/gdrte/totp-go/internal/storage"
)

const (
	maxCodeLength = 10
	// maxMinRemaining bounds how long a code request may block waiting for
	// the next window.
	maxMinRemaining = 120
)

type errorResponse struct {
	Error string `json:"error"`
}

type accountSummary struct {
	Namespace  string `json:"namespace"`
	Account    string `json:"account"`
	Algorithm  string `json:"algorithm"`
	Length     uint   `json:"length"`
	TimePeriod int64  `json:"timePeriod"`
	HasPrefix  bool   `json:"hasPrefix"`
}

type codeResponse struct {
	ExpiresAt  time.Time `json:"expiresAt"`
	Namespace  string    `json:"namespace"`
	Account    string    `json:"account"`
	Code       string    `json:"code"`
	Remaining  int64     `json:"remaining"`
	TimePeriod int64     `json:"timePeriod"`
}

// AccountRequest describes an account to register, by base32 secret or by
// otpauth:// URI. It is the body of POST /api/v1/accounts.
type AccountRequest struct {
	Namespace  string `json:"namespace"`
	Account    string `json:"account"`
	Secret     string `json:"secret"`
	URI        string `json:"uri"`
	Prefix     string `json:"prefix"`
	Algorithm  string `json:"algorithm"`
	Length     uint   `json:"length"`
	TimePeriod int64  `json:"timePeriod"`
}

func (s *Server) handleHealth(resp http.ResponseWriter, _ *http.Request) {
	writeJSON(resp, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleListAccounts(resp http.ResponseWriter, req *http.Request) {
	summaries, err := s.listAccounts(req.Context())
	if err != nil {
		writeOpError(resp, err)

		return
	}

	writeJSON(resp, http.StatusOK, summaries)
}

// handleGetCode returns the current code. With ?min_remaining=N it
// guarantees the returned code stays valid for at least N more seconds.
func (s *Server) handleGetCode(resp http.ResponseWriter, req *http.Request) {
	minRemaining, err := parseMinRemaining(req.URL.Query().Get("min_remaining"))
	if err != nil {
		writeOpError(resp, err)

		return
	}

	code, err := s.currentCode(req.Context(), req.PathValue("namespace"), req.PathValue("account"), minRemaining)
	if errors.Is(err, errWaitAborted) {
		return
	}

	if err != nil {
		writeOpError(resp, err)

		return
	}

	writeJSON(resp, http.StatusOK, code)
}

func (s *Server) handleAddAccount(resp http.ResponseWriter, req *http.Request) {
	var payload AccountRequest

	decoder := json.NewDecoder(http.MaxBytesReader(resp, req.Body, maxRequestBodyBytes))
	decoder.DisallowUnknownFields()

	err := decoder.Decode(&payload)
	if err != nil {
		writeError(resp, http.StatusBadRequest, "invalid JSON body: "+err.Error())

		return
	}

	summary, err := s.addAccount(req.Context(), payload)
	if err != nil {
		writeOpError(resp, err)

		return
	}

	writeJSON(resp, http.StatusCreated, summary)
}

func (s *Server) handleDeleteAccount(resp http.ResponseWriter, req *http.Request) {
	err := s.deleteAccount(req.Context(), req.PathValue("namespace"), req.PathValue("account"))
	if err != nil {
		writeOpError(resp, err)

		return
	}

	resp.WriteHeader(http.StatusNoContent)
}

func writeOpError(resp http.ResponseWriter, err error) {
	status, message := publicError(err)
	writeError(resp, status, message)
}

// Build merges the optional otpauth URI with the explicit fields (explicit
// fields win) and validates the result. It returns the namespace and the
// account to store; validation failures are RequestErrors.
func (req AccountRequest) Build() (string, *storage.Account, error) {
	nsName := req.Namespace
	account := &storage.Account{
		Name:       req.Account,
		Token:      req.Secret,
		Prefix:     req.Prefix,
		Algorithm:  req.Algorithm,
		Length:     req.Length,
		TimePeriod: req.TimePeriod,
	}

	if req.URI != "" {
		parsed, err := parseOTPAuthURI(req.URI)
		if err != nil {
			return "", nil, err
		}

		nsName = firstNonEmpty(nsName, parsed.Issuer)
		account.Name = firstNonEmpty(account.Name, parsed.Account)
		account.Token = firstNonEmpty(account.Token, parsed.Secret)
		account.Algorithm = firstNonEmpty(account.Algorithm, parsed.Algorithm)

		if account.Length == 0 {
			account.Length = parsed.Length
		}

		if account.TimePeriod == 0 {
			account.TimePeriod = parsed.TimePeriod
		}
	}

	err := validateAccount(nsName, account)
	if err != nil {
		return "", nil, err
	}

	return nsName, account, nil
}

func validateAccount(nsName string, account *storage.Account) error {
	switch {
	case nsName == "":
		return RequestError{Message: "namespace is required"}
	case account.Name == "":
		return RequestError{Message: "account is required"}
	case account.Token == "":
		return RequestError{Message: "secret (or uri) is required"}
	case !algo.IsValidName(account.Algorithm):
		return RequestError{Message: "algorithm must be one of sha1, sha256, sha512"}
	case account.Length > maxCodeLength:
		return RequestError{Message: "length must be at most " + strconv.Itoa(maxCodeLength)}
	case account.TimePeriod < 0:
		return RequestError{Message: "timePeriod must be positive"}
	}

	if account.Length == 0 {
		account.Length = security.DefaultLength
	}

	if account.TimePeriod == 0 {
		account.TimePeriod = security.DefaultTimePeriod
	}

	_, _, err := GenerateCode(account, time.Now())
	if err != nil {
		return RequestError{Message: "secret is not valid base32"}
	}

	return nil
}

func parseMinRemaining(raw string) (int64, error) {
	if raw == "" {
		return 0, nil
	}

	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, errMinRemainingRange
	}

	return value, nil
}

// GenerateCode returns the account's code (with its prefix) at the given
// time and the seconds left until it expires.
func GenerateCode(account *storage.Account, when time.Time) (string, int64, error) {
	code, remaining, err := security.GenerateOTPCode(security.GenerateOptions{
		Token:      account.Token,
		When:       when,
		Length:     account.Length,
		Algorithm:  algo.ByName(account.Algorithm),
		TimePeriod: account.TimePeriod,
	})
	if err != nil {
		return "", 0, err
	}

	return account.Prefix + code, remaining, nil
}

func summarize(entry accountdb.Entry) accountSummary {
	length := entry.Length
	if length == 0 {
		length = security.DefaultLength
	}

	return accountSummary{
		Namespace:  entry.Namespace,
		Account:    entry.Name,
		Algorithm:  firstNonEmpty(entry.Algorithm, "sha1"),
		Length:     length,
		TimePeriod: EffectivePeriod(entry.TimePeriod),
		HasPrefix:  entry.HasPrefix,
	}
}

// EffectivePeriod returns the account's time period, or the default when
// none is set.
func EffectivePeriod(period int64) int64 {
	if period <= 0 {
		return security.DefaultTimePeriod
	}

	return period
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}

	return ""
}
