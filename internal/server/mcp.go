package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/gdrte/totp-go/internal/info"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPPath is where the MCP endpoint (streamable HTTP transport) is served.
// It takes the same bearer token as the REST API.
const MCPPath = "/mcp"

const mcpInstructions = `Generates TOTP two-factor codes for accounts stored on this server, so you can
complete 2FA logins in tests and automations. Accounts are identified by namespace and account name;
call list_accounts to find them. When a code is about to be typed or submitted, call get_code with
min_remaining (5 is a good default) so the code does not expire before the site checks it.
Secrets are never returned.`

type mcpAccountRef struct {
	Namespace string `json:"namespace" jsonschema:"Namespace of the account, e.g. staging."`
	Account   string `json:"account"   jsonschema:"Account name within the namespace."`
}

type mcpGetCodeInput struct {
	Namespace    string `json:"namespace"               jsonschema:"Namespace of the account, e.g. staging."`
	Account      string `json:"account"                 jsonschema:"Account name within the namespace."`
	MinRemaining int64  `json:"min_remaining,omitempty" jsonschema:"Seconds (0-120) the code must stay valid for. If the current code expires sooner, the call waits for the next one."` //nolint:lll // schema description
}

type mcpAddAccountInput struct {
	Namespace  string `json:"namespace,omitempty"  jsonschema:"Namespace to store the account in. Defaults to the URI's issuer."`
	Account    string `json:"account,omitempty"    jsonschema:"Account name. Defaults to the URI's label."`
	Secret     string `json:"secret,omitempty"     jsonschema:"Base32 TOTP secret. Required unless uri is given."`
	URI        string `json:"uri,omitempty"        jsonschema:"otpauth://totp/... URI from the site's setup QR code."`
	Prefix     string `json:"prefix,omitempty"     jsonschema:"Text to put in front of every code, for sites that expect a PIN before it."`
	Algorithm  string `json:"algorithm,omitempty"  jsonschema:"HMAC algorithm: sha1 (default), sha256 or sha512."`
	Length     uint   `json:"length,omitempty"     jsonschema:"Number of digits. Default 6."`
	TimePeriod int64  `json:"timePeriod,omitempty" jsonschema:"Seconds each code is valid for. Default 30."`
}

type mcpAccountList struct {
	Accounts []accountSummary `json:"accounts"`
}

// mcpHandler returns the MCP endpoint. It is stateless: every request is
// served on its own and answered with JSON, so no sessions are kept.
func (s *Server) mcpHandler() http.Handler {
	srv := mcp.NewServer(
		&mcp.Implementation{Name: info.Name, Title: "TOTP Authenticator", Version: info.Version},
		&mcp.ServerOptions{Instructions: mcpInstructions},
	)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_accounts",
		Description: "List the stored accounts (namespace, name, algorithm, digits, period). Secrets are not included.",
		Annotations: &mcp.ToolAnnotations{Title: "List accounts", ReadOnlyHint: true},
	}, s.mcpListAccounts)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "get_code",
		Description: "Get the current TOTP code for an account, with the seconds it stays valid. " +
			"Pass min_remaining to be sure the code doesn't expire before it is used.",
		Annotations: &mcp.ToolAnnotations{Title: "Get a code", ReadOnlyHint: true},
	}, s.mcpGetCode)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "add_account",
		Description: "Register an account from a base32 secret or an otpauth:// URI. " +
			"Explicit fields override values from the URI. Fails if the account already exists.",
		Annotations: &mcp.ToolAnnotations{Title: "Add an account", DestructiveHint: new(false)},
	}, s.mcpAddAccount)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "delete_account",
		Description: "Delete an account and its secret. This cannot be undone.",
		Annotations: &mcp.ToolAnnotations{Title: "Delete an account", DestructiveHint: new(true), IdempotentHint: true},
	}, s.mcpDeleteAccount)

	return mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, MaxRequestBodyBytes: maxRequestBodyBytes},
	)
}

func (s *Server) mcpListAccounts(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, mcpAccountList, error) {
	accounts, err := s.listAccounts(ctx)
	if err != nil {
		return nil, mcpAccountList{}, toolError(err)
	}

	return nil, mcpAccountList{Accounts: accounts}, nil
}

func (s *Server) mcpGetCode(ctx context.Context, _ *mcp.CallToolRequest, in mcpGetCodeInput) (*mcp.CallToolResult, codeResponse, error) {
	code, err := s.currentCode(ctx, in.Namespace, in.Account, in.MinRemaining)
	if err != nil {
		return nil, codeResponse{}, toolError(err)
	}

	return nil, code, nil
}

func (s *Server) mcpAddAccount(ctx context.Context, _ *mcp.CallToolRequest, in mcpAddAccountInput) (*mcp.CallToolResult, accountSummary, error) {
	summary, err := s.addAccount(ctx, AccountRequest(in))
	if err != nil {
		return nil, accountSummary{}, toolError(err)
	}

	return nil, summary, nil
}

func (s *Server) mcpDeleteAccount(ctx context.Context, _ *mcp.CallToolRequest, in mcpAccountRef) (*mcp.CallToolResult, mcpAccountRef, error) {
	err := s.deleteAccount(ctx, in.Namespace, in.Account)
	if err != nil {
		return nil, mcpAccountRef{}, toolError(err)
	}

	return nil, in, nil
}

// toolError turns an operation error into the message the agent sees, with
// the same wording and redaction as the REST API.
func toolError(err error) error {
	if errors.Is(err, errWaitAborted) {
		return err
	}

	_, message := publicError(err)

	return errors.New(message) //nolint:err113 // message is already redacted for clients
}
