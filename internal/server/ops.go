package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gdrte/totp-go/internal/accountdb"
)

// The account operations behind both the REST API and the MCP tools. They
// return errors that publicError turns into what a client may see.

// errWaitAborted means a code request stopped waiting for the next window,
// usually because the client went away.
var errWaitAborted = errors.New("stopped waiting for the next code")

// storeError is a store failure while working on subject (an account path,
// or empty for the whole store).
type storeError struct {
	err     error
	subject string
}

func (e storeError) Error() string { return e.err.Error() }
func (e storeError) Unwrap() error { return e.err }

// generateError is a failure to compute a code from a stored account.
type generateError struct {
	err error
}

func (e generateError) Error() string { return e.err.Error() }
func (e generateError) Unwrap() error { return e.err }

// publicError maps an operation error to an HTTP status and the message
// shown to the client. Unexpected errors are logged and reported as
// "internal error", so database details don't leak.
func publicError(err error) (int, string) {
	var (
		reqErr   RequestError
		genErr   generateError
		storeErr storeError
	)

	switch {
	case errors.As(err, &reqErr):
		return http.StatusBadRequest, reqErr.Message
	case errors.As(err, &genErr):
		return http.StatusInternalServerError, genErr.Error()
	case errors.As(err, &storeErr) && errors.Is(err, accountdb.ErrNotFound):
		return http.StatusNotFound, storeErr.subject + " not found"
	case errors.As(err, &storeErr) && errors.Is(err, accountdb.ErrExists):
		return http.StatusConflict, storeErr.subject + " already exists"
	}

	log.Printf("store error: %v", err)

	return http.StatusInternalServerError, "internal error"
}

func (s *Server) listAccounts(ctx context.Context) ([]accountSummary, error) {
	entries, err := s.store.List(ctx)
	if err != nil {
		return nil, storeError{err: err}
	}

	summaries := make([]accountSummary, 0, len(entries))
	for _, entry := range entries {
		summaries = append(summaries, summarize(entry))
	}

	return summaries, nil
}

// currentCode returns the account's current code. With minRemaining > 0 it
// guarantees the code stays valid for at least that many more seconds,
// waiting for the next window if needed, so callers don't race expiry.
func (s *Server) currentCode(ctx context.Context, nsName, accName string, minRemaining int64) (codeResponse, error) {
	if minRemaining < 0 || minRemaining > maxMinRemaining {
		return codeResponse{}, errMinRemainingRange
	}

	account, err := s.store.Get(ctx, nsName, accName)
	if err != nil {
		return codeResponse{}, storeError{err: err, subject: nsName + "/" + accName}
	}

	period := EffectivePeriod(account.TimePeriod)
	if minRemaining >= period {
		return codeResponse{}, RequestError{Message: "min_remaining must be less than the account's time period"}
	}

	code, remaining, err := GenerateCode(account, s.now())
	if err != nil {
		return codeResponse{}, generateError{err: err}
	}

	if remaining < minRemaining {
		err = s.wait(ctx, time.Duration(remaining)*time.Second)
		if err != nil {
			return codeResponse{}, errors.Join(errWaitAborted, err)
		}

		code, remaining, err = GenerateCode(account, s.now())
		if err != nil {
			return codeResponse{}, generateError{err: err}
		}
	}

	return codeResponse{
		Namespace:  nsName,
		Account:    accName,
		Code:       code,
		Remaining:  remaining,
		TimePeriod: period,
		ExpiresAt:  s.now().Add(time.Duration(remaining) * time.Second).Truncate(time.Second).UTC(),
	}, nil
}

func (s *Server) addAccount(ctx context.Context, req AccountRequest) (accountSummary, error) {
	nsName, account, err := req.Build()
	if err != nil {
		var reqErr RequestError
		if !errors.As(err, &reqErr) {
			err = RequestError{Message: err.Error()}
		}

		return accountSummary{}, err
	}

	err = s.store.Add(ctx, nsName, account)
	if err != nil {
		return accountSummary{}, storeError{err: err, subject: nsName + "/" + account.Name}
	}

	return summarize(accountdb.EntryOf(nsName, account)), nil
}

func (s *Server) deleteAccount(ctx context.Context, nsName, accName string) error {
	err := s.store.Delete(ctx, nsName, accName)
	if err != nil {
		return storeError{err: err, subject: nsName + "/" + accName}
	}

	return nil
}

var errMinRemainingRange = RequestError{
	Message: "min_remaining must be an integer between 0 and " + strconv.Itoa(maxMinRemaining),
}
