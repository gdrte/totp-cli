package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/gdrte/totp-go/internal/accountdb"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// bearerTransport adds the API token to every request.
type bearerTransport struct {
	token string
}

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)

	return http.DefaultTransport.RoundTrip(req) //nolint:wrapcheck // test transport
}

func (suite *ServerTestSuite) mcpConnect(token string) (*mcp.ClientSession, error) {
	suite.T().Helper()

	httpServer := httptest.NewServer(suite.handler)
	suite.T().Cleanup(httpServer.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "test-agent", Version: "1"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   httpServer.URL + MCPPath,
		HTTPClient: &http.Client{Transport: bearerTransport{token: token}},
		MaxRetries: -1,
	}

	session, err := client.Connect(context.Background(), transport, nil)
	if err == nil {
		suite.T().Cleanup(func() { _ = session.Close() })
	}

	return session, err //nolint:wrapcheck // test helper
}

func (suite *ServerTestSuite) mcpCall(name string, args any) *mcp.CallToolResult {
	suite.T().Helper()

	session, err := suite.mcpConnect(testToken)
	suite.Require().NoError(err)

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	suite.Require().NoError(err)

	return result
}

func (suite *ServerTestSuite) structured(result *mcp.CallToolResult, target any) {
	suite.T().Helper()
	suite.Require().False(result.IsError, "tool error: %v", result.Content)

	raw, err := json.Marshal(result.StructuredContent)
	suite.Require().NoError(err)
	suite.Require().NoError(json.Unmarshal(raw, target))
}

func (suite *ServerTestSuite) toolErrorText(result *mcp.CallToolResult) string {
	suite.T().Helper()
	suite.Require().True(result.IsError)
	suite.Require().NotEmpty(result.Content)

	text, ok := result.Content[0].(*mcp.TextContent)
	suite.Require().True(ok)

	return text.Text
}

func (suite *ServerTestSuite) TestMCPRequiresToken() {
	_, err := suite.mcpConnect("wrong")
	suite.Require().Error(err)

	rec := suite.do(http.MethodPost, MCPPath, `{}`, false)
	suite.Equal(http.StatusUnauthorized, rec.Code)
}

func (suite *ServerTestSuite) TestMCPListsTools() {
	session, err := suite.mcpConnect(testToken)
	suite.Require().NoError(err)

	tools, err := session.ListTools(context.Background(), nil)
	suite.Require().NoError(err)

	names := make([]string, 0, len(tools.Tools))
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}

	suite.ElementsMatch([]string{"list_accounts", "get_code", "add_account", "delete_account"}, names)
}

func (suite *ServerTestSuite) TestMCPListAccountsOmitsSecrets() {
	result := suite.mcpCall("list_accounts", map[string]any{})

	var out mcpAccountList

	suite.structured(result, &out)
	suite.Require().Len(out.Accounts, 2)
	suite.Equal("prefixed", out.Accounts[0].Account)

	raw, err := json.Marshal(result)
	suite.Require().NoError(err)
	suite.NotContains(string(raw), rfcSecret)
	suite.NotContains(string(raw), "pin-")
}

func (suite *ServerTestSuite) TestMCPGetCodeMatchesRFCVector() {
	result := suite.mcpCall("get_code", map[string]any{"namespace": "staging", "account": "rfc"})

	var out codeResponse

	suite.structured(result, &out)
	suite.Equal("94287082", out.Code)
	suite.Equal(int64(1), out.Remaining)
}

func (suite *ServerTestSuite) TestMCPGetCodeWaitsForMinRemaining() {
	result := suite.mcpCall("get_code", map[string]any{"namespace": "staging", "account": "rfc", "min_remaining": 10})

	var out codeResponse

	suite.structured(result, &out)
	suite.Equal([]time.Duration{time.Second}, suite.waited)
	suite.Equal(int64(30), out.Remaining)
}

func (suite *ServerTestSuite) TestMCPGetCodeErrors() {
	result := suite.mcpCall("get_code", map[string]any{"namespace": "staging", "account": "nope"})
	suite.Equal("staging/nope not found", suite.toolErrorText(result))

	result = suite.mcpCall("get_code", map[string]any{"namespace": "staging", "account": "rfc", "min_remaining": 999})
	suite.Contains(suite.toolErrorText(result), "min_remaining must be")
}

func (suite *ServerTestSuite) TestMCPAddAndDeleteAccount() {
	result := suite.mcpCall("add_account", map[string]any{
		"uri": "otpauth://totp/GitHub:ci@example.com?secret=JBSWY3DPEHPK3PXP&issuer=GitHub",
	})

	var summary accountSummary

	suite.structured(result, &summary)
	suite.Equal("GitHub", summary.Namespace)
	suite.Equal("ci@example.com", summary.Account)

	result = suite.mcpCall("add_account", map[string]any{
		"uri": "otpauth://totp/GitHub:ci@example.com?secret=JBSWY3DPEHPK3PXP&issuer=GitHub",
	})
	suite.Equal("GitHub/ci@example.com already exists", suite.toolErrorText(result))

	result = suite.mcpCall("delete_account", map[string]any{"namespace": "GitHub", "account": "ci@example.com"})
	suite.False(result.IsError)

	_, err := suite.get("GitHub", "ci@example.com")
	suite.Require().ErrorIs(err, accountdb.ErrNotFound)
}

func (suite *ServerTestSuite) TestMCPAddAccountValidation() {
	result := suite.mcpCall("add_account", map[string]any{"namespace": "staging", "account": "x"})
	suite.Equal("secret (or uri) is required", suite.toolErrorText(result))
}

func (suite *ServerTestSuite) TestMCPStoreFailureDoesNotLeakDetails() {
	suite.Require().NoError(suite.store.Close())

	result := suite.mcpCall("list_accounts", map[string]any{})
	suite.Equal("internal error", suite.toolErrorText(result))
}
