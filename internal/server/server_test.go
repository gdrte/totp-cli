package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/yitsushi/totp-cli/internal/accountdb"
	"github.com/yitsushi/totp-cli/internal/storage"
)

const (
	testToken = "test-token"
	// RFC 6238 SHA1 test secret ("12345678901234567890" in base32).
	rfcSecret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
)

func TestServer(t *testing.T) {
	suite.Run(t, &ServerTestSuite{})
}

type ServerTestSuite struct {
	suite.Suite

	store   *accountdb.DB
	now     time.Time
	waited  []time.Duration
	handler http.Handler
}

func (suite *ServerTestSuite) SetupTest() {
	ctx := context.Background()

	store, err := accountdb.Open(ctx, filepath.Join(suite.T().TempDir(), "server.db"), accountdb.WithScryptWorkFactor(1))
	suite.Require().NoError(err)
	suite.Require().NoError(store.Unlock(ctx, "password"))
	suite.Require().NoError(store.Add(ctx, "staging",
		&storage.Account{Name: "rfc", Token: rfcSecret, Length: 8, TimePeriod: 30}))
	suite.Require().NoError(store.Add(ctx, "staging",
		&storage.Account{Name: "prefixed", Token: rfcSecret, Prefix: "pin-", Length: 6}))

	suite.store = store
	suite.now = time.Unix(59, 0)
	suite.waited = nil

	suite.handler = New(
		suite.store, testToken,
		WithClock(func() time.Time { return suite.now }),
		WithWaitFunc(func(_ context.Context, d time.Duration) error {
			suite.waited = append(suite.waited, d)
			suite.now = suite.now.Add(d)

			return nil
		}),
	).Handler()
}

func (suite *ServerTestSuite) TearDownTest() {
	_ = suite.store.Close()
}

func (suite *ServerTestSuite) get(namespace, name string) (*storage.Account, error) {
	suite.T().Helper()

	return suite.store.Get(context.Background(), namespace, name)
}

func (suite *ServerTestSuite) do(method, path, body string, authed bool) *httptest.ResponseRecorder {
	suite.T().Helper()

	req := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	if authed {
		req.Header.Set("Authorization", "Bearer "+testToken)
	}

	rec := httptest.NewRecorder()
	suite.handler.ServeHTTP(rec, req)

	return rec
}

func (suite *ServerTestSuite) decode(rec *httptest.ResponseRecorder, target any) {
	suite.T().Helper()

	suite.Require().NoError(json.Unmarshal(rec.Body.Bytes(), target))
}

func (suite *ServerTestSuite) TestHealthNeedsNoToken() {
	rec := suite.do(http.MethodGet, "/api/v1/health", "", false)

	suite.Equal(http.StatusOK, rec.Code)
}

func (suite *ServerTestSuite) TestRejectsMissingOrWrongToken() {
	rec := suite.do(http.MethodGet, "/api/v1/accounts", "", false)
	suite.Equal(http.StatusUnauthorized, rec.Code)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/accounts", nil)
	req.Header.Set("Authorization", "Bearer wrong")

	rec = httptest.NewRecorder()
	suite.handler.ServeHTTP(rec, req)
	suite.Equal(http.StatusUnauthorized, rec.Code)
}

func (suite *ServerTestSuite) TestListAccountsOmitsSecrets() {
	rec := suite.do(http.MethodGet, "/api/v1/accounts", "", true)

	suite.Require().Equal(http.StatusOK, rec.Code)
	suite.NotContains(rec.Body.String(), rfcSecret)
	suite.NotContains(rec.Body.String(), "pin-")

	var accounts []accountSummary

	suite.decode(rec, &accounts)
	suite.Require().Len(accounts, 2)
	suite.Equal("prefixed", accounts[0].Account)
	suite.True(accounts[0].HasPrefix)
	suite.Equal("rfc", accounts[1].Account)
	suite.Equal(uint(8), accounts[1].Length)
	suite.Equal("sha1", accounts[1].Algorithm)
}

func (suite *ServerTestSuite) TestGetCodeMatchesRFCVector() {
	rec := suite.do(http.MethodGet, "/api/v1/accounts/staging/rfc/code", "", true)

	suite.Require().Equal(http.StatusOK, rec.Code)

	var resp codeResponse

	suite.decode(rec, &resp)
	suite.Equal("94287082", resp.Code)
	suite.Equal(int64(1), resp.Remaining)
	suite.Equal(int64(30), resp.TimePeriod)
	suite.Equal(time.Unix(60, 0).UTC(), resp.ExpiresAt)
	suite.Empty(suite.waited)
}

func (suite *ServerTestSuite) TestGetCodeAppliesPrefix() {
	rec := suite.do(http.MethodGet, "/api/v1/accounts/staging/prefixed/code", "", true)

	var resp codeResponse

	suite.decode(rec, &resp)
	suite.True(strings.HasPrefix(resp.Code, "pin-"))
	suite.Len(resp.Code, len("pin-")+6)
}

func (suite *ServerTestSuite) TestGetCodeWaitsForMinRemaining() {
	rec := suite.do(http.MethodGet, "/api/v1/accounts/staging/rfc/code?min_remaining=10", "", true)

	suite.Require().Equal(http.StatusOK, rec.Code)
	suite.Equal([]time.Duration{time.Second}, suite.waited)

	var resp codeResponse

	suite.decode(rec, &resp)
	suite.Equal(int64(30), resp.Remaining)
	suite.NotEqual("94287082", resp.Code)
}

func (suite *ServerTestSuite) TestGetCodeRejectsBadMinRemaining() {
	for _, value := range []string{"abc", "-1", "30", "999"} {
		rec := suite.do(http.MethodGet, "/api/v1/accounts/staging/rfc/code?min_remaining="+value, "", true)
		suite.Equal(http.StatusBadRequest, rec.Code, value)
	}
}

func (suite *ServerTestSuite) TestGetCodeUnknownAccount() {
	rec := suite.do(http.MethodGet, "/api/v1/accounts/staging/nope/code", "", true)
	suite.Equal(http.StatusNotFound, rec.Code)

	rec = suite.do(http.MethodGet, "/api/v1/accounts/nope/rfc/code", "", true)
	suite.Equal(http.StatusNotFound, rec.Code)
}

func (suite *ServerTestSuite) TestAddAccountWithSecret() {
	rec := suite.do(http.MethodPost, "/api/v1/accounts",
		`{"namespace":"prod","account":"bot","secret":"jbsw y3dp ehpk 3pxp","algorithm":"sha256"}`, true)

	suite.Require().Equal(http.StatusCreated, rec.Code, rec.Body.String())

	acc, err := suite.get("prod", "bot")
	suite.Require().NoError(err)
	suite.Equal("jbsw y3dp ehpk 3pxp", acc.Token)
	suite.Equal("sha256", acc.Algorithm)
	suite.Equal(uint(6), acc.Length)
	suite.Equal(int64(30), acc.TimePeriod)
}

func (suite *ServerTestSuite) TestAddAccountWithURI() {
	body := `{"uri":"otpauth://totp/Acme%20Corp:alice@example.com?secret=JBSWY3DPEHPK3PXP&issuer=Acme%20Corp&algorithm=SHA512&digits=8&period=60"}`
	rec := suite.do(http.MethodPost, "/api/v1/accounts", body, true)

	suite.Require().Equal(http.StatusCreated, rec.Code, rec.Body.String())

	var created accountSummary

	suite.decode(rec, &created)
	suite.Equal(accountSummary{
		Namespace: "Acme Corp", Account: "alice@example.com",
		Algorithm: "sha512", Length: 8, TimePeriod: 60,
	}, created)
}

func (suite *ServerTestSuite) TestAddAccountExplicitFieldsOverrideURI() {
	body := `{"namespace":"qa","length":6,"uri":"otpauth://totp/Acme:alice?secret=JBSWY3DPEHPK3PXP&digits=8"}`
	rec := suite.do(http.MethodPost, "/api/v1/accounts", body, true)

	suite.Require().Equal(http.StatusCreated, rec.Code, rec.Body.String())

	var created accountSummary

	suite.decode(rec, &created)
	suite.Equal("qa", created.Namespace)
	suite.Equal("alice", created.Account)
	suite.Equal(uint(6), created.Length)
}

func (suite *ServerTestSuite) TestAddAccountConflict() {
	rec := suite.do(http.MethodPost, "/api/v1/accounts",
		`{"namespace":"staging","account":"rfc","secret":"JBSWY3DPEHPK3PXP"}`, true)

	suite.Equal(http.StatusConflict, rec.Code)
	suite.Contains(rec.Body.String(), "staging/rfc already exists")
}

func (suite *ServerTestSuite) TestAddAccountValidation() {
	cases := map[string]string{
		"missing namespace": `{"account":"a","secret":"JBSWY3DPEHPK3PXP"}`,
		"missing account":   `{"namespace":"n","secret":"JBSWY3DPEHPK3PXP"}`,
		"missing secret":    `{"namespace":"n","account":"a"}`,
		"bad base32":        `{"namespace":"n","account":"a","secret":"not-base32!"}`,
		"bad algorithm":     `{"namespace":"n","account":"a","secret":"JBSWY3DPEHPK3PXP","algorithm":"md5"}`,
		"too long":          `{"namespace":"n","account":"a","secret":"JBSWY3DPEHPK3PXP","length":11}`,
		"unknown field":     `{"namespace":"n","account":"a","secret":"JBSWY3DPEHPK3PXP","token":"x"}`,
		"hotp uri":          `{"uri":"otpauth://hotp/A:b?secret=JBSWY3DPEHPK3PXP"}`,
		"not json":          `nope`,
	}

	for name, body := range cases {
		rec := suite.do(http.MethodPost, "/api/v1/accounts", body, true)
		suite.Equal(http.StatusBadRequest, rec.Code, name)
	}

	entries, err := suite.store.List(context.Background())
	suite.Require().NoError(err)
	suite.Len(entries, 2)
}

func (suite *ServerTestSuite) TestStoreFailureDoesNotLeakDetails() {
	suite.Require().NoError(suite.store.Close())

	rec := suite.do(http.MethodGet, "/api/v1/accounts", "", true)

	suite.Equal(http.StatusInternalServerError, rec.Code)
	suite.JSONEq(`{"error":"internal error"}`, rec.Body.String())
}

func (suite *ServerTestSuite) TestDeleteAccount() {
	rec := suite.do(http.MethodDelete, "/api/v1/accounts/staging/rfc", "", true)

	suite.Require().Equal(http.StatusNoContent, rec.Code)

	_, err := suite.get("staging", "rfc")
	suite.Require().ErrorIs(err, accountdb.ErrNotFound)

	_, err = suite.get("staging", "prefixed")
	suite.Require().NoError(err)
}

func (suite *ServerTestSuite) TestAccountNamesWithSlashes() {
	rec := suite.do(http.MethodPost, "/api/v1/accounts",
		`{"namespace":"a/b","account":"c/d","secret":"JBSWY3DPEHPK3PXP"}`, true)
	suite.Require().Equal(http.StatusCreated, rec.Code)

	rec = suite.do(http.MethodGet, "/api/v1/accounts/a%2Fb/c%2Fd/code", "", true)
	suite.Equal(http.StatusOK, rec.Code, rec.Body.String())
}

func (suite *ServerTestSuite) TestDeleteUnknown() {
	rec := suite.do(http.MethodDelete, "/api/v1/accounts/staging/nope", "", true)

	suite.Equal(http.StatusNotFound, rec.Code)
}

func (suite *ServerTestSuite) TestServesUI() {
	rec := suite.do(http.MethodGet, "/", "", false)

	suite.Require().Equal(http.StatusOK, rec.Code)
	suite.Contains(rec.Body.String(), "Register a site")
	suite.Equal("DENY", rec.Header().Get("X-Frame-Options"))
}
