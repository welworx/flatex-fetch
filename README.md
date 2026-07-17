# flatex-fetch

[![CI](https://github.com/welworx/flatex-fetch/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/welworx/flatex-fetch/actions/workflows/ci.yml)
[![CodeQL](https://github.com/welworx/flatex-fetch/actions/workflows/codeql.yml/badge.svg?branch=main)](https://github.com/welworx/flatex-fetch/actions/workflows/codeql.yml)
[![Release](https://img.shields.io/github/v/release/welworx/flatex-fetch)](https://github.com/welworx/flatex-fetch/releases/latest)
[![Go Version](https://img.shields.io/github/go-mod/go-version/welworx/flatex-fetch)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Logs into the flatex.at broker web portal and downloads new documents
(trade confirmations, dividend notices, …) as raw PDFs. Companion to
[flatex-pdf-cli](https://github.com/welworx/flatex-pdf-cli), which parses
those PDFs into structured JSON — this tool only fetches.

> **Disclaimer:** Built for **personal, educational use only.** This is an
> independent, unofficial tool, not affiliated with, endorsed by, or
> supported by flatex. It drives flatex's web portal using the same requests
> your browser makes, which may not be permitted under flatex's Terms of
> Service — check those before using it. Use is entirely at your own risk;
> see [Disclaimer](#disclaimer) below for the full terms.

## Features

- **Encrypted credentials**: portal password stored locally with
  argon2id + AES-256-GCM, unlocked by a master passphrase (prompt or env var)
- **Multi-profile**: manage several portal logins (`profile add|list|remove`)
  and fetch one or all of them in a single run
- **Incremental by default**: already-downloaded documents are skipped;
  `-all` re-downloads everything in range
- **Flexible date ranges**: `-days`, or explicit `-from`/`-to`

## Install

    go install github.com/welworx/flatex-fetch@latest

or grab a release binary (darwin/linux/windows, amd64/arm64).

## Setup

    flatex-fetch profile add main -domain flatex.at

Prompts for your portal username and password. The password is stored in
`~/.config/flatex-fetch/credentials.enc`, encrypted (argon2id + AES-256-GCM)
with a master passphrase you set on first use. For cron/scripting, provide
the passphrase via the `FLATEX_FETCH_PASSPHRASE` environment variable, and
the portal username/password via `FLATEX_FETCH_USERNAME`/
`FLATEX_FETCH_PASSWORD` to skip the interactive prompts.

## Usage

    flatex-fetch fetch                                # first configured profile, last 7 days
    flatex-fetch fetch -profile main -days 10
    flatex-fetch fetch -profile main -from 2026-01-01 -to 2026-06-30
    flatex-fetch fetch -profile main -all -days 365  # re-download existing too
    flatex-fetch fetch -all-profiles

`-profile` defaults to the first configured profile; use `-all-profiles` to
fetch every profile instead.

PDFs land in `~/flatex-downloads/<profile>/` (`-out` overrides), named by
the portal's own filename. Already-downloaded files are skipped unless
`-all` is set. Exit status is non-zero if any profile or document failed.

`-user-agent` overrides the built-in browser User-Agent string.

## Known Limitations

- flatex.at only; flatex.de is untested (`-domain` exists but unverified).
- No 2FA handling — document access currently doesn't require it.
- Only the first page of archive results is fetched — a very wide
  `-days`/`-from`/`-to` range with many documents may not return everything.

Login, document listing, and download are confirmed working against a real
flatex.at account (2026-07-16).

## Roadmap

- [ ] Pagination via the portal's "load more" control.

## Development

### Running Tests

```bash
go test ./...                 # unit tests (httptest mock backend, no real account)
go test -race ./...           # what CI runs
FLATEX_E2E_USER=... FLATEX_E2E_PASS=... go test -tags e2e ./internal/portal/   # live portal, manual only
```

### Code Quality

The project uses `golangci-lint` for linting. Configuration is in `.golangci.yml`.

```bash
gofmt -l .        # check formatting
go vet ./...
golangci-lint run
```

### Pre-commit Hooks

Optional: `.pre-commit-config.yaml` runs `go fmt`, `go vet`, and `go test` on commit.

```bash
pip install pre-commit
pre-commit install
```

## Project Structure

```
flatex-fetch/
├── main.go                 # CLI entry point and command dispatch
├── cli_profile.go          # profile add/list/remove
├── cli_fetch.go            # fetch command
├── go.mod / go.sum
├── .golangci.yml           # linter configuration
├── .pre-commit-config.yaml
└── internal/
    ├── config/             # credential encryption, profile storage
    └── portal/             # flatex.at HTTP client: login, listing, download
```

## Dependencies

- [golang.org/x/crypto](https://pkg.go.dev/golang.org/x/crypto) — argon2id + AES-256-GCM credential encryption
- [golang.org/x/term](https://pkg.go.dev/golang.org/x/term) — masked password prompt

## Contributing

Contributions are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md).

## License

The code is licensed under the [MIT License](LICENSE): you're free to read,
modify, and redistribute it, provided the copyright notice is retained, and
it comes with no warranty (see the LICENSE file for the full text). That
license covers the code itself — it is not permission to use this tool
against flatex's live portal. This project is intended for **personal,
educational use only**; see [Disclaimer](#disclaimer) for the use
restrictions and risk.

## Support

For issues, feature requests, or questions, please open an issue on GitHub.
For security vulnerabilities, see [SECURITY.md](SECURITY.md) instead.

## Disclaimer

This tool is built strictly for **personal, educational purposes** and is an
unofficial, independent project, not affiliated with, endorsed by, or
supported by flatex. It talks to flatex's web portal using the same requests
your browser makes — flatex could change or block this at any time without
notice.

**Check flatex's Terms of Service before using this tool.** Automated
access to your account may not be permitted under their terms, and you are
solely responsible for ensuring your use complies with them. Use of this
tool is entirely at your own risk; the author(s) accept no liability for
any consequences of using it, including but not limited to account
restrictions, data loss, or financial loss.
