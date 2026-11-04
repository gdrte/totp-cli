//go:build gui

package gui

import (
	"fmt"
	"image"
	"net"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/gdrte/totp-go/internal/info"
)

// helpTopic is a tab of the help screen.
type helpTopic int

const (
	helpGUI helpTopic = iota
	helpCLI
	helpAPI
	helpMCP
	helpTopicCount
)

func (t helpTopic) label() string {
	switch t {
	case helpGUI:
		return "Desktop app"
	case helpAPI:
		return "REST API"
	case helpMCP:
		return "MCP"
	case helpCLI, helpTopicCount:
	}

	return "Command line"
}

type helpKind int

const (
	helpHeading helpKind = iota
	helpText
	helpCode
)

// helpBlock is one heading, paragraph or copyable code block.
type helpBlock struct {
	text string
	kind helpKind
}

func heading(text string) helpBlock { return helpBlock{kind: helpHeading, text: text} }
func para(text string) helpBlock    { return helpBlock{kind: helpText, text: text} }
func code(text string) helpBlock    { return helpBlock{kind: helpCode, text: text} }

// helpEnv is the live state the help text refers to, so examples match
// this window's database and API address.
type helpEnv struct {
	dbPath  string
	apiAddr string // address the API is serving on, empty when it is not
	listen  string // configured --listen value
	apiNote string
}

// exampleAddr is the API address used in examples.
func (e helpEnv) exampleAddr() string {
	if e.apiAddr != "" {
		return e.apiAddr
	}

	host, port, err := net.SplitHostPort(e.listen)
	if err != nil {
		return defaultExampleAddr
	}

	if host == "" {
		host = "127.0.0.1"
	}

	return net.JoinHostPort(host, port)
}

const defaultExampleAddr = "127.0.0.1:8484"

// serverStatus says whether this window is serving what, e.g.
// "the MCP endpoint at http://127.0.0.1:8484/mcp".
func (e helpEnv) serverStatus(what string) string {
	switch {
	case e.apiAddr != "":
		return "This window serves " + what + "."
	case e.listen == "":
		return "The API and MCP endpoint are disabled in this window (started with --listen ''). " +
			"Run totp-cli serve instead."
	case e.apiNote != "":
		return e.apiNote
	}

	return "The API and MCP endpoint start once the database is unlocked."
}

func helpContent(topic helpTopic, env helpEnv) []helpBlock {
	switch topic {
	case helpGUI:
		return guiHelp(env)
	case helpAPI:
		return apiHelp(env)
	case helpMCP:
		return mcpHelp(env)
	case helpCLI, helpTopicCount:
	}

	return cliHelp()
}

func cliHelp() []helpBlock {
	return []helpBlock{
		heading("Command-line tool"),
		para("totp-cli keeps accounts in an encrypted credentials file (~/.config/totp-cli/credentials, " +
			"or the path in TOTP_CLI_CREDENTIAL_FILE). That file is separate from this app's database. " +
			"Each command asks for its password unless TOTP_PASS is set."),
		code("totp-cli help\ntotp-cli help <command>"),

		heading("Add an account"),
		para("Namespaces group accounts, e.g. personal or staging. Leave out the arguments to be prompted. " +
			"Optional flags: --length, --prefix, --algorithm (sha1, sha256, sha512) and --time-period."),
		code("totp-cli add-token personal github"),
		code("totp-cli add-token --length=8 --algorithm=sha256 personal github"),

		heading("Generate a code"),
		para("--show-remaining prints how long the code stays valid; --follow keeps printing new codes."),
		code("totp-cli generate personal github"),
		code("totp-cli generate --show-remaining personal github"),
		code("totp-cli generate --follow personal github"),
		para("Generate a code from a secret without storing it:"),
		code("TOTP_TOKEN=JBSWY3DPEHPK3PXP totp-cli instant"),

		heading("List, change and delete"),
		code("totp-cli list\ntotp-cli list personal"),
		code("totp-cli update --length=6 personal github\ntotp-cli set-prefix personal github mypin\n" +
			"totp-cli set-prefix --clear personal github"),
		code("totp-cli rename account personal github github-work\ntotp-cli rename namespace personal home"),
		code("totp-cli delete personal github\ntotp-cli delete personal"),
		code("totp-cli change-password"),

		heading("Back up and restore"),
		para("dump writes every account, secrets included, as YAML. import reads the same format and asks " +
			"before overwriting an existing account."),
		code("totp-cli dump --output backup.yaml\ntotp-cli import --input backup.yaml"),

		heading("Serve the REST API"),
		para("serve exposes the app's database over HTTP, the same REST API and MCP endpoint this window serves. " +
			"--import copies the CLI's accounts into the database first. See the REST API tab."),
		code("totp-cli serve\ntotp-cli serve --import --listen 127.0.0.1:8484"),
	}
}

func guiHelp(env helpEnv) []helpBlock {
	return []helpBlock{
		heading("Database"),
		para("This app stores accounts in a SQLite database shared with totp-cli serve. Secrets are encrypted " +
			"with a key protected by the database password. There is no way to recover a forgotten password."),
		para("This window uses:"),
		code(env.dbPath),

		heading("Register a site"),
		para("Choose Secret key and paste the base32 secret the site shows under \"can't scan the QR code\", " +
			"or choose otpauth:// URI and paste the URI from the QR code. With a URI, namespace and account " +
			"default to its issuer and label."),
		para("Advanced sets the algorithm, digits, period and a prefix (a PIN some sites expect before the code). " +
			"Leave them blank for the usual 6-digit, 30-second sha1 codes."),

		heading("Use the codes"),
		para("Click a code or its Copy button to copy it. The bar under the code shows the time left and turns " +
			"red in the last 5 seconds. Delete needs a second click on Confirm within 3 seconds."),
		para("curl copies a command that fetches the account's current code from the API, for tests and agents. " +
			"It reads the token from TOTP_API_TOKEN, so set that first (Copy token is in the header):"),
		code("export TOTP_API_TOKEN=<paste token>"),
		para("Accounts added through the API or totp-cli serve appear here within a few seconds."),

		heading("Launch options"),
		para("Pass these when starting the app from a terminal. Environment variables work too."),
		code("totp-gui --db ~/totp/server.db       # or TOTP_SERVER_DB\n" +
			"totp-gui --listen 127.0.0.1:9000     # API address\n" +
			"totp-gui --listen ''                 # no API\n" +
			"totp-gui --api-token <token>         # or TOTP_API_TOKEN"),
		para("Without --api-token a random token is generated each launch. TOTP_PASS unlocks the database " +
			"without the password screen. Don't run the app and totp-cli serve on the same address."),
	}
}

func apiHelp(env helpEnv) []helpBlock {
	addr := env.exampleAddr()
	base := "http://" + addr + "/api/v1"
	auth := `curl -s -H "Authorization: Bearer $TOTP_API_TOKEN" `

	return []helpBlock{
		heading("REST API"),
		para(env.serverStatus("the API at http://"+addr) + " Test suites and agents use it to register accounts " +
			"and fetch codes."),
		para("Every endpoint except health needs the API token. Copy it from the header and export it:"),
		code("export TOTP_API_TOKEN=<paste token>"),
		para("Anyone with the token can generate codes for every account, so treat it like the password. " +
			"Secrets are never returned."),

		heading("Endpoints"),
		code("GET    /api/v1/health                              no auth\n" +
			"GET    /api/v1/accounts                            list accounts\n" +
			"POST   /api/v1/accounts                            register an account\n" +
			"DELETE /api/v1/accounts/{namespace}/{account}      delete an account\n" +
			"GET    /api/v1/accounts/{namespace}/{account}/code current code\n" +
			"POST   /mcp                                        MCP endpoint for agents"),

		heading("Get a code"),
		para("min_remaining=N (0–120) guarantees the code stays valid for at least N more seconds. If the " +
			"current window ends sooner, the request waits for the next code."),
		code(auth + fmt.Sprintf("'%s/accounts/staging/qa-bot/code?min_remaining=5'", base)),
		code(`{"expiresAt":"2026-09-29T15:39:30Z","namespace":"staging","account":"qa-bot",` +
			`"code":"086405","remaining":27,"timePeriod":30}`),

		heading("Register an account"),
		para("Send a base32 secret, or the otpauth:// URI from the site's QR code. Optional fields: prefix, " +
			"algorithm (sha1, sha256, sha512), length (digits) and timePeriod (seconds). Returns 201, " +
			"or 409 if the account exists."),
		code(auth + "-X POST " + base + "/accounts \\\n" +
			`  -d '{"namespace":"staging","account":"qa-bot","secret":"JBSWY3DPEHPK3PXP"}'`),
		code(auth + "-X POST " + base + "/accounts \\\n" +
			`  -d '{"uri":"otpauth://totp/GitHub:ci@example.com?secret=JBSWY3DPEHPK3PXP&issuer=GitHub"}'`),

		heading("List and delete"),
		code(auth + base + "/accounts"),
		code(auth + "-X DELETE " + base + "/accounts/staging/qa-bot"),
		code("curl -s " + base + "/health"),

		heading("MCP"),
		para("AI agents can use the same accounts as tools through the MCP endpoint at /mcp. " +
			"See the MCP tab."),

		heading("Errors"),
		para(`Errors are JSON, e.g. {"error":"account not found"}: 400 bad request, 401 missing or wrong token, ` +
			"404 unknown account, 409 already exists."),

		heading("Windows"),
		para("The curl examples, and the commands the curl buttons copy, use POSIX shell quoting: run them in " +
			"Git Bash or WSL. In PowerShell, use Invoke-RestMethod:"),
		code("$env:TOTP_API_TOKEN = '<paste token>'\n" +
			`Invoke-RestMethod -Headers @{Authorization = "Bearer $env:TOTP_API_TOKEN"} ` +
			fmt.Sprintf(`"%s/accounts/staging/qa-bot/code?min_remaining=5"`, base)),
	}
}

func mcpHelp(env helpEnv) []helpBlock {
	url := "http://" + env.exampleAddr() + "/mcp"

	return []helpBlock{
		heading("MCP server"),
		para(env.serverStatus("the MCP endpoint at "+url) + " It lets AI agents such as Claude Code fetch " +
			"2FA codes as tools, so they can finish logins in tests and automations without curl."),
		para("It uses the Streamable HTTP transport and the same token as the REST API. Copy the token " +
			"from the header and export it:"),
		code("export TOTP_API_TOKEN=<paste token>"),
		para("Anyone with the token can generate codes for every account, so only give it to agents you trust."),

		heading("Connect Claude Code"),
		code(`claude mcp add --transport http totp ` + url + ` --header "Authorization: Bearer $TOTP_API_TOKEN"`),
		para("The token is saved in Claude Code's config when you run this. Add --scope user to make it " +
			"available in every project, and check the connection with /mcp inside Claude Code. " +
			"A generated token changes every launch; pass --api-token (or set TOTP_API_TOKEN before " +
			"starting the app) to keep it stable."),

		heading("Other MCP clients"),
		para("Clients that connect to HTTP servers take a JSON entry like this:"),
		code(`{"mcpServers": {"totp": {` + "\n" +
			`  "type": "http",` + "\n" +
			`  "url": "` + url + `",` + "\n" +
			`  "headers": {"Authorization": "Bearer <paste token>"}}}}`),
		para("Clients that can only launch a local command (such as Claude Desktop) can bridge to it with " +
			"mcp-remote, which needs Node.js:"),
		code(`{"mcpServers": {"totp": {` + "\n" +
			`  "command": "npx",` + "\n" +
			`  "args": ["mcp-remote", "` + url + `", "--header", "Authorization:${AUTH_HEADER}"],` + "\n" +
			`  "env": {"AUTH_HEADER": "Bearer <paste token>"}}}}`),

		heading("Tools"),
		para("list_accounts — every account's namespace, name, algorithm, digits and period. " +
			"Secrets are never returned."),
		para("get_code(namespace, account, min_remaining?) — the current code, the seconds it stays " +
			"valid and when it expires. With min_remaining (0–120) the code is guaranteed to stay valid " +
			"that long: if the current one expires sooner, the call waits for the next one."),
		para("add_account(secret or uri, namespace?, account?, prefix?, algorithm?, length?, timePeriod?) — " +
			"register a site from its base32 secret or the otpauth:// URI in its QR code. Namespace and " +
			"account default to the URI's issuer and label. Fails if the account exists."),
		para("delete_account(namespace, account) — delete an account and its secret. This cannot be undone."),
		para("Errors use the same wording as the REST API, e.g. \"staging/qa-bot not found\"."),

		heading("Example prompts"),
		code("List my TOTP accounts.\n" +
			"Log in to staging as qa-bot; when asked for 2FA, use get_code with min_remaining 5.\n" +
			"Register this otpauth URI under the namespace staging: otpauth://totp/..."),

		heading("Troubleshooting"),
		para("401: the token is missing or wrong (a generated token changes every launch). " +
			"403: the request reached localhost with another host name; use 127.0.0.1 or localhost " +
			"in the URL. To check the endpoint without an agent:"),
		code("curl -s " + url + " \\\n" +
			"  -H \"Authorization: Bearer $TOTP_API_TOKEN\" \\\n" +
			"  -H 'Content-Type: application/json' \\\n" +
			"  -H 'Accept: application/json, text/event-stream' \\\n" +
			`  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'`),
	}
}

// copiedFeedback is how long a code block's button reads "Copied".
const copiedFeedback = 2 * time.Second

func (u *ui) helpEnv() helpEnv {
	return helpEnv{dbPath: u.cfg.DBPath, apiAddr: u.apiAddr, listen: u.cfg.Listen, apiNote: u.apiNote}
}

// layoutHelp draws the help screen: a Back button, one tab per topic and
// the selected topic's text with copyable examples.
func (u *ui) layoutHelp(gtx C) D {
	if u.helpBack.Clicked(gtx) {
		u.showHelp = false
	}

	for i := range u.helpTabs {
		if u.helpTabs[i].Clicked(gtx) && helpTopic(i) != u.helpTopic {
			u.helpTopic = helpTopic(i)
			u.helpList.Position = layout.Position{}
			u.helpCopyAt = time.Time{}
		}
	}

	blocks := helpContent(u.helpTopic, u.helpEnv())

	codes := 0

	for _, b := range blocks {
		if b.kind == helpCode {
			codes++
		}
	}

	if len(u.helpCopy) < codes {
		u.helpCopy = make([]widget.Clickable, codes)
	}

	items := []layout.Widget{u.layoutHelpHeader}
	codeIdx := 0

	for _, b := range blocks {
		switch b.kind {
		case helpHeading:
			items = append(items, func(gtx C) D {
				return layout.Inset{Top: 10}.Layout(gtx, material.H6(u.th, b.text).Layout)
			})
		case helpText:
			items = append(items, material.Body2(u.th, b.text).Layout)
		case helpCode:
			idx := codeIdx
			codeIdx++

			if u.helpCopy[idx].Clicked(gtx) {
				copyText(gtx, b.text)
				u.helpCopied, u.helpCopyAt = idx, gtx.Now
			}

			items = append(items, func(gtx C) D { return u.layoutCodeBlock(gtx, b.text, idx) })
		}
	}

	if until := u.helpCopyAt.Add(copiedFeedback); gtx.Now.Before(until) {
		gtx.Execute(op.InvalidateCmd{At: until})
	}

	return u.layoutColumn(gtx, &u.helpList, items)
}

func (u *ui) layoutHelpHeader(gtx C) D {
	tabs := make([]layout.FlexChild, 0, 2*len(u.helpTabs))

	for i := range u.helpTabs {
		topic := helpTopic(i)

		bg := colorSubtle
		if topic == u.helpTopic {
			bg = colorAccent
		}

		tabs = append(
			tabs,
			layout.Rigid(func(gtx C) D { return u.button(gtx, &u.helpTabs[i], topic.label(), bg) }),
			hgap(6),
		)
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(
		gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(
				gtx,
				layout.Rigid(func(gtx C) D {
					title := material.H5(u.th, "Help")
					title.Font.Weight = font.Bold

					return title.Layout(gtx)
				}),
				layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Min.X, 0)} }),
				layout.Rigid(func(gtx C) D { return u.button(gtx, &u.helpBack, "Back", colorSubtle) }),
			)
		}),
		layout.Rigid(u.muted(appTitle+" "+info.Version)),
		gap(10),
		layout.Rigid(func(gtx C) D { return layout.Flex{}.Layout(gtx, tabs...) }),
	)
}

func (u *ui) layoutCodeBlock(gtx C, text string, idx int) D {
	label := "Copy"
	if idx == u.helpCopied && gtx.Now.Sub(u.helpCopyAt) < copiedFeedback {
		label = "Copied"
	}

	return card(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(
			gtx,
			layout.Flexed(1, func(gtx C) D {
				l := material.Body2(u.th, text)
				l.Font = u.mono

				return l.Layout(gtx)
			}),
			hgap(10),
			layout.Rigid(func(gtx C) D { return u.button(gtx, &u.helpCopy[idx], label, colorSubtle) }),
		)
	})
}
