[![Go Report Card](https://goreportcard.com/badge/github.com/gdrte/totp-go)](https://goreportcard.com/report/github.com/gdrte/totp-go)
[![Actions Status](https://github.com/gdrte/totp-go/actions/workflows/quality-check.yaml/badge.svg)](https://github.com/gdrte/totp-go/actions/workflows/quality-check.yaml)
[![Coverage Status](https://coveralls.io/repos/github/gdrte/totp-go/badge.svg?branch=main)](https://coveralls.io/github/gdrte/totp-go?branch=main)

This is a simple TOTP _(Time-based One-time Password)_ CLI tool.
TOTP is the most common mechanism for 2FA _(Two-Factor-Authentication)_.
You can manage and organize your accounts with namespaces
and protect your data with a password.

It is a fork of [yitsushi/totp-cli](https://github.com/yitsushi/totp-cli)
that adds a REST API and a desktop app for automated tests and agents, backed
by an encrypted SQLite database. The winget, MacPorts and Homebrew packages
named `totp-cli` install the upstream tool, which has none of these.

### Install

Download the latest version of the application
from the [releases page](https://github.com/gdrte/totp-go/releases/latest) or using the `go` tool:

```shell
go install github.com/gdrte/totp-go@latest
```

`go install` names the binary `totp-go`; the release downloads and `make build`
name it `totp-cli`, as used in the examples below.

Or build from source:

```shell
make build               # bin/totp-cli
make build-gui           # bin/totp-gui (desktop app, needs cgo)
make build-windows       # bin/totp-cli.exe
make build-gui-windows   # bin/totp-gui.exe
```

#### Signing key

On release, there is a checksum file for all generated artefacts, signed with
the GPG key configured for this repository's releases:

```
gpg --verify totp-cli_{{.Version}}_checksums.txt.sig
```

#### Upgrading from totp-cli v1.2.7 or below

Starting with totp-cli v1.2.8 a [more secure storage
format](https://github.com/FiloSottile/age) is used. The storage will be
upgraded the first time it is written to by totp-cli. You can force this to
occur by running `totp-cli change-password`.

### Help output

```shell
totp-cli help
```

```
NAME:
   totp-cli - Authy/Google Authenticator like TOTP CLI tool written in Go.

USAGE:
   totp-cli [global options] command [command options]

VERSION:
   dev

AUTHORS:
   Devanand GangiReddy
   Efertone (original totp-cli) <victoria@efertone.me>

COMMANDS:
   add-token, add   Add new token.
   change-password  Change password.
   delete           Delete an account or a whole namespace.
   dump             Dump all available accounts under all namespaces.
   generate, g      Generate a specific OTP
   import           Import tokens from a yaml file.
   instant          Generate an OTP from TOTP_TOKEN or stdin without the Storage backend.
   list             List all available namespaces or accounts under a namespace.
   serve            Serve a REST API and MCP endpoint for generating codes and registering accounts.
   set-prefix       Set prefix for a token.
   set-length       Set length for a token.
   rename           Rename an account or namespace
   update           Update the TOTP token options.
   help, h          Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --help, -h     show help
   --version, -v  print the version
```

### Usage

When you run the application for the first time, it will ask
for your password. **DO NOT FORGET IT!** There is no way to
recover your password if you forget it.

Your first command _(after `help`)_ would be `add-token`. You can get
your token read a TOTP QR Code.

```shell
totp-cli add-token
```

```
Namespace: personal
Account: digitalocean
Token: XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
Password: ***
```

You can specify the namespace and the account name as a parameter:

```shell
totp-cli add-token personal randomaccount
```

```
Token: XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
Password: ***
```

If the provider uses a different length for tokens, you can set it on
`add-token` or set with `set-length`.

```
totp-cli add-token --length=8 personal randomaccount
totp-cli set-length personal randomaccount 6
```

If you want to delete `randomaccount` _(because it was a test for example)_,
you can use `delete`:

```shell
totp-cli delete personal.randomaccount
```

```
Password: ***
You want to delete 'personal.randomaccount' account.
Are you sure? yes
```

After few accounts, it's a bit hard to remember what did you added,
so you can list namespaces:

```shell
totp-cli list
```

```
Password: ***
company1 (Number of accounts: 3)
company2 (Number of accounts: 5)
personal (Number of accounts: 8)
```

or you can list your accounts under a specific namespace:

```shell
totp-cli list personal
```

```
Password: ***
personal.evernote
personal.google
personal.github
personal.ifttt
personal.digitalocean
personal.dropbox
personal.facebook
```

If you want to change your password, you can do it with the `change-password`
command.

Some providers require the user to prefix the generated token with their
password or passphrase. You can set a prefix for each account with `set-prefix`,
or set with `add-token`.

```
totp-cli set-prefix ns account
Prefix: myprefix

totp-cli add-token --prefix=asd personal randomaccount
totp-cli set-prefix personal randomaccount asd

# Clear prefix
totp-cli set-prefix ns account --clear
```

To generate an OTP, you simply use the `generate` command like:

```shell
totp-cli generate namespace account
Password: ***
889840
```

You can also use the `--follow` flag on the `generate` command if you want to 
have the OTP token automatically refreshed once expired:

```shell
totp-cli generate --follow namespace account
Password: ***
889840
343555
463346
```

If the provider is very strict with the code, with the `--show-remaining` flag
will add extra information about how long the code will be valid.

```
totp-cli generate --show-remaining namespace account
Password: ***
316762 (remaining time: 17s)
```

### REST API and desktop app (for automated tests and agents)

`serve` exposes accounts over HTTP, as a REST API and an MCP endpoint, so
test suites and agents can fetch codes without the interactive prompt.

```shell
export TOTP_PASS=...                # database password (or you'll be prompted)
export TOTP_API_TOKEN=$(openssl rand -hex 32)
totp-cli serve                      # http://127.0.0.1:8484
```

The server keeps its accounts in a SQLite database (default
`~/.config/totp-cli/server.db`, set with `--db` or `TOTP_SERVER_DB`), separate
from the CLI credentials file. Every request reads the database directly, so
nothing is cached in memory. Account names and settings are stored in plain
columns. Each token and prefix is encrypted with [age](https://github.com/FiloSottile/age)
to a key created with the database, and that key is itself encrypted with your
password. A copy of the database file alone reveals no secrets. To copy your
CLI accounts into the database, run `totp-cli serve --import` (accounts that
already exist are skipped).

If `--api-token` / `TOTP_API_TOKEN` is not set, a random token is generated
and printed on startup. By default the server listens on localhost only
(`--listen` changes it). Anyone holding the token can generate codes for
every stored account, so treat it like the password.

Every `/api/v1` endpoint except `health` requires
`Authorization: Bearer <token>`. Secrets are never returned.

| Method   | Path                                           | Description                              |
|----------|------------------------------------------------|------------------------------------------|
| `GET`    | `/api/v1/health`                               | Liveness check (no auth).                |
| `GET`    | `/api/v1/accounts`                             | List accounts (without secrets).         |
| `POST`   | `/api/v1/accounts`                             | Register an account.                     |
| `DELETE` | `/api/v1/accounts/{namespace}/{account}`       | Delete an account.                       |
| `GET`    | `/api/v1/accounts/{namespace}/{account}/code`  | Current code.                            |

Get a code. Pass `min_remaining=N` to make sure the code stays valid for at
least `N` more seconds. If the current window is closer than that to expiring,
the request waits for the next one, so a test never submits a code that has
already expired:

```shell
curl -s -H "Authorization: Bearer $TOTP_API_TOKEN" \
  'http://127.0.0.1:8484/api/v1/accounts/staging/qa-bot/code?min_remaining=5'
```

```json
{"expiresAt":"2026-09-29T15:39:30Z","namespace":"staging","account":"qa-bot","code":"086405","remaining":27,"timePeriod":30}
```

Register an account with a base32 secret, or with the `otpauth://` URI that
sites encode in their setup QR code. Namespace and account default to the
URI's issuer and label, and explicit fields override values from the URI:

```shell
curl -s -H "Authorization: Bearer $TOTP_API_TOKEN" -X POST \
  http://127.0.0.1:8484/api/v1/accounts \
  -d '{"namespace":"staging","account":"qa-bot","secret":"JBSWY3DPEHPK3PXP"}'

curl -s -H "Authorization: Bearer $TOTP_API_TOKEN" -X POST \
  http://127.0.0.1:8484/api/v1/accounts \
  -d '{"uri":"otpauth://totp/GitHub:ci@example.com?secret=JBSWY3DPEHPK3PXP&issuer=GitHub"}'
```

Optional fields: `prefix`, `algorithm` (`sha1`, `sha256`, `sha512`),
`length` (digits) and `timePeriod` (seconds).

#### MCP endpoint

The server also speaks the [Model Context Protocol](https://modelcontextprotocol.io)
at `/mcp` (streamable HTTP), with the same bearer token, so agents can use the
accounts as tools instead of shelling out to `curl`:

| Tool             | Description                                                        |
|------------------|--------------------------------------------------------------------|
| `list_accounts`  | List accounts (without secrets).                                   |
| `get_code`       | Current code; `min_remaining` waits for a fresh one, as in the API. |
| `add_account`    | Register by base32 `secret` or `otpauth://` `uri`.                 |
| `delete_account` | Delete an account.                                                 |

Add it to Claude Code:

```shell
claude mcp add --transport http totp http://127.0.0.1:8484/mcp \
  --header "Authorization: Bearer $TOTP_API_TOKEN"
```

or to any MCP client that takes a JSON config:

```json
{
  "mcpServers": {
    "totp": {
      "type": "http",
      "url": "http://127.0.0.1:8484/mcp",
      "headers": { "Authorization": "Bearer <token>" }
    }
  }
}
```

The endpoint is stateless (no sessions; each request is answered with JSON),
and requests that reach a localhost address with a non-localhost `Host` header
are rejected to block DNS rebinding.

#### Desktop app

`totp-gui` is a desktop app built with [Gio](https://gioui.org) on the same
database. It registers sites (secret key or `otpauth://` URI), shows live codes
with a countdown, and copies a code or a ready-made `curl` command for agents.
While it is open it also serves the REST API and MCP endpoint above, so tests
and agents can fetch codes from it as they would from `totp-cli serve`.

Gio needs cgo on macOS and Linux, so the app is a separate binary built with
the `gui` build tag (on Linux, install Gio's
[system dependencies](https://gioui.org/doc/install/linux) first):

```shell
make build-gui                      # bin/totp-gui
./bin/totp-gui                      # API on http://127.0.0.1:8484
```

On macOS, `make bundle-gui` builds `bin/TOTP Authenticator.app` with the app
icon, which you can drag into `/Applications`. The icon's source is
`cmd/totp-gui/appicon.svg`; after editing it, run `make appicon` (needs
`rsvg-convert` from librsvg) to regenerate `appicon.png`.

On Windows, `make build-gui-windows` cross-compiles `bin/totp-gui.exe` (with
the app icon and no console window) from any OS, since Gio needs no cgo there.
`make build-windows` builds the CLI as `bin/totp-cli.exe`. Both default to
amd64; set `WINDOWS_ARCH=arm64` for ARM machines.

The **Help** link in the window header (and on the unlock screen) opens usage
notes for the desktop app, the CLI and the REST API, with copyable examples
that use the window's own database path and API address.

It takes the same `--db`, `--listen` and `--api-token` options (and
`TOTP_SERVER_DB`, `TOTP_API_TOKEN` and `TOTP_PASS` variables) as `serve`.
The password is asked for in the window unless `TOTP_PASS` is set. Pass
`--listen ''` to run without the API. A generated token can be copied from the
window header. Don't run it and `serve` on the same address at once.

### Changing the location of the credentials file

Simply put this into your `.zshrc` (or `.{YourShell}rc` or `.profile`):

```shell
export TOTP_CLI_CREDENTIAL_FILE="/mnt/mydrive/totp-credentials"
```

Or call the client with `TOTP_CLI_CREDENTIAL_FILE`:

```shell
$ TOTP_CLI_CREDENTIAL_FILE=/mnt/mydrive/totp-credentials totp-cli list
```

The default location is `${HOME}/.config/totp-cli/credentials`.

**Note:** It's a filename not just a directory.

**Note:** It does not traverse through the given path,
      parent directory has to be there already.

### Import

You can import tokens from a YAML file. The syntax is the same as the output of
the `dump` command.

```yaml
- name: ns1
  accounts:
    - name: acc1
      token: updatedtoken
    - name: acc2
      token: mytoken
    - name: acc3
      token: tokenish
- name: ns2
  accounts:
    - name: acc1
      token: token
      prefix: myprefix
```

If a token already exists, it will ask you if you want to overwrite it or not.

```shell
totp-cli import list.yaml
```

### Shell Completion

Disclaimer: I don't have much expertise with auto-complete integrations. The
following instructions should be enough, but it is possible they are not. Feel
free to open a Pull Request with any additional suggestions/fixes either in the
docs or the autocomplete scripts.

* Bash: `autocomplete/bash_autocomplete`
* Zsh: `autocomplete/zsh_autocomplete`

#### Zsh

A function to provide tab-completion for zsh is in the file
`autocomplete/zsh_autocomplete`. When installing or packaging totp-cli this
should preferably be installed in `$prefix/share/zsh/site-functions`. Otherwise,
it can be installed by copying to a directory where zsh searches for completion
functions (the `$fpath` array). If you, for example, put all completion
functions into the folder `~/.zsh/completions` you must add the following to
your zsh main config file (`.zshrc`):

```shell
cp autocomplete/zsh_autocomplete ~/.zsh/functions/_totp_cli
fpath=( ~/.zsh/completions $fpath )
autoload -U compinit
compinit
```

#### Bash

```
mkdir -p ~/.local/share/bash-completion/completions
cp autocomplete/bash_autocomplete ~/.local/share/bash-completion/completions/totp-cli
```

## About the password

The password should never be passed directly to any applications to unlock it.
If you save it in a variable it can be exposed if your `ENV` is exposed somehow,
if you directly type in the password in the command line, it can end up in your
bash/zsh/whatevershell history.

Mostly to support CI/CD automation, there is an option to set the
password/passphrase as an environment variable. **Please use it only if you know
the system is safe to store passwords in environment variables.**

If you really want to skip the password prompt, it reads from `stdin`, so you
can pipe the password.

```
❯ age \
  --encrypt \
  --armor \
  --recipient age15velesv0zwpsc5w0n4da5tv64u9fzuhl8hjpvdmeayjg00fdf4wsxl834c \
  > "${HOME}/.config/totp-cli/totp-password.age"
myapssword
^D

❯ age --decrypt --identity ~/.age/efertone.txt ~/.config/totp-cli/totp-password.age | totp-cli list
Password: ***
....list of namespaces

❯ age --decrypt --identity ~/.age/efertone.txt ~/.config/totp-cli/totp-password.age | totp-cli generate xxxxx xxxxx
Password: ***
166307

❯ alias totp-pass-in="age --decrypt --identity ~/.age/efertone.txt ~/.config/totp-cli/totp-password.age"

❯ totp-pass-in| totp-cli generate xxxxx xxxxx
Password: ***
889840
```

Other option is to use an environment variable:

```
❯ age \
  --encrypt \
  --armor \
  --recipient age15velesv0zwpsc5w0n4da5tv64u9fzuhl8hjpvdmeayjg00fdf4wsxl834c \
  > "${HOME}/.config/totp-cli/totp-password.age"
myapssword
^D

❯ export TOTP_PASS=$(age --decrypt --identity ~/.age/efertone.txt ~/.config/totp-cli/totp-password.age)
Password: ***

❯ totp-cli generate xxxxx xxxxx
166307
```

But I'm really against it, it's a password that can access all your stored 2FA
tokens. With the password (even without the `totp-cli` application) and the
credentials files, that file is not really encrypted anymore as it can be
decrypted with the password.

**Please, never store your password as clear-text. Never. Pretty please.**
