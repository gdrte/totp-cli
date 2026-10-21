package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/yitsushi/totp-cli/internal/accountdb"
	"github.com/yitsushi/totp-cli/internal/security"
	"github.com/yitsushi/totp-cli/internal/security/algo"
	"github.com/yitsushi/totp-cli/internal/storage"
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
	entries, err := s.store.List(req.Context())
	if err != nil {
		writeStoreError(resp, err, "")

		return
	}

	summaries := make([]accountSummary, 0, len(entries))
	for _, entry := range entries {
		summaries = append(summaries, summarize(entry))
	}

	writeJSON(resp, http.StatusOK, summaries)
}

// handleGetCode returns the current code. With ?min_remaining=N it
// guarantees the returned code stays valid for at least N more seconds,
// waiting for the next window if needed, so tests don't race expiry.
func (s *Server) handleGetCode(resp http.ResponseWriter, req *http.Request) {
	nsName, accName := req.PathValue("namespace"), req.PathValue("account")

	minRemaining, err := parseMinRemaining(req.URL.Query().Get("min_remaining"))
	if err != nil {
		writeError(resp, http.StatusBadRequest, err.Error())

		return
	}

	account, err := s.store.Get(req.Context(), nsName, accName)
	if err != nil {
		writeStoreError(resp, err, nsName+"/"+accName)

		return
	}

	if minRemaining >= EffectivePeriod(account.TimePeriod) {
		writeError(resp, http.StatusBadRequest, "min_remaining must be less than the account's time period")

		return
	}

	code, remaining, err := GenerateCode(account, s.now())
	if err != nil {
		writeError(resp, http.StatusInternalServerError, err.Error())

		return
	}

	if remaining < minRemaining {
		err = s.wait(req.Context(), time.Duration(remaining)*time.Second)
		if err != nil {
			return
		}

		code, remaining, err = GenerateCode(account, s.now())
		if err != nil {
			writeError(resp, http.StatusInternalServerError, err.Error())

			return
		}
	}

	writeJSON(resp, http.StatusOK, codeResponse{
		Namespace:  nsName,
		Account:    accName,
		Code:       code,
		Remaining:  remaining,
		TimePeriod: EffectivePeriod(account.TimePeriod),
		ExpiresAt:  s.now().Add(time.Duration(remaining) * time.Second).Truncate(time.Second).UTC(),
	})
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

	nsName, account, err := payload.Build()
	if err != nil {
		writeError(resp, http.StatusBadRequest, err.Error())

		return
	}

	err = s.store.Add(req.Context(), nsName, account)
	if err != nil {
		writeStoreError(resp, err, nsName+"/"+account.Name)

		return
	}

	writeJSON(resp, http.StatusCreated, summarize(accountdb.EntryOf(nsName, account)))
}

func (s *Server) handleDeleteAccount(resp http.ResponseWriter, req *http.Request) {
	nsName, accName := req.PathValue("namespace"), req.PathValue("account")

	err := s.store.Delete(req.Context(), nsName, accName)
	if err != nil {
		writeStoreError(resp, err, nsName+"/"+accName)

		return
	}

	resp.WriteHeader(http.StatusNoContent)
}

// writeStoreError maps store errors to HTTP statuses. Unexpected errors are
// logged rather than returned, so database details don't leak to clients.
func writeStoreError(resp http.ResponseWriter, err error, subject string) {
	switch {
	case errors.Is(err, accountdb.ErrNotFound):
		writeError(resp, http.StatusNotFound, subject+" not found")
	case errors.Is(err, accountdb.ErrExists):
		writeError(resp, http.StatusConflict, subject+" already exists")
	default:
		log.Printf("store error: %v", err)
		writeError(resp, http.StatusInternalServerError, "internal error")
	}
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
	if err != nil || value < 0 || value > maxMinRemaining {
		return 0, RequestError{Message: "min_remaining must be an integer between 0 and " + strconv.Itoa(maxMinRemaining)}
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
