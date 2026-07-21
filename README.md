# flatex-fetch

[![CI](https://github.com/welworx/flatex-fetch/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/welworx/flatex-fetch/actions/workflows/ci.yml)
[![CodeQL](https://github.com/welworx/flatex-fetch/actions/workflows/codeql.yml/badge.svg?branch=main)](https://github.com/welworx/flatex-fetch/actions/workflows/codeql.yml)
[![Coverage Status](https://coveralls.io/repos/github/welworx/flatex-fetch/badge.svg?branch=main)](https://coveralls.io/github/welworx/flatex-fetch?branch=main)
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
- **Incremental by default**: already-downloaded documents are skipped —
  unambiguous ones without even re-contacting the portal, via the download
  log; `-all` re-downloads everything in range
- **Flexible date ranges**: `-days`, explicit `-from`/`-to`, or `-since-last`
  (per profile, from its latest already-fetched document date through
  today) — a wide range is adaptively split into as many sub-windows as
  the portal actually needs, so its 100-document cap on filtered queries
  doesn't silently truncate results
- **Configurable output paths**: `-format` templates the download path per
  document (profile, category, date, filename), instead of the fixed
  `<profile>/<filename>` layout
- **Download log**: every file written is appended, with metadata, to
  `<out>/.fetch-log.jsonl`
- **Verbose progress**: `-verbose` prints date ranges queried, documents
  found, and per-document skip/download status to stderr — useful on a
  wide range, where otherwise nothing prints until the run finishes

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

`fetch`/`list` also accept `FLATEX_FETCH_USERNAME`/`FLATEX_FETCH_PASSWORD`
directly, skipping `profile add` and profiles.json entirely — useful for
cron/CI without a stored profile. `-profile`/`-all-profiles` are ignored in
that mode; the portal domain defaults to `flatex.at`, overridable via
`FLATEX_FETCH_DOMAIN`.

## Usage

    flatex-fetch fetch                                # first configured profile, last 7 days
    flatex-fetch fetch -profile main -days 10
    flatex-fetch fetch -profile main -from 2026-01-01 -to 2026-06-30
    flatex-fetch fetch -profile main -all -days 365  # re-download existing too
    flatex-fetch fetch -all-profiles
    flatex-fetch fetch -since-last -all-profiles      # continue each profile from its newest already-fetched document date

    # list without downloading, as a table, CSV, or JSON
    flatex-fetch list -profile main
    flatex-fetch list -all-profiles -csv
    flatex-fetch list -all-profiles -json

`-profile` defaults to the first configured profile; use `-all-profiles` to
fetch (or list) every profile instead. When multiple profiles are processed,
`list`'s table gets a `PROFILE` column and its CSV/JSON rows get a
`profile` field, so output from `-all-profiles` stays attributable per row.

PDFs land in `~/flatex-downloads/<profile>/` (`-out` overrides), named by
the portal's own filename. Already-downloaded files are skipped unless
`-all` is set. Exit status is non-zero if any profile or document failed;
a failed document's message identifies it by date/category/name (the
portal has no stable per-document URL).

Every file `fetch` writes is also appended, one JSON object per line, to
`<out>/.fetch-log.jsonl` (time, profile, document index/date/category/name,
local path). On later runs, a listed document whose log entry is
unambiguous (no other document shares its date/category/name) and whose
file still exists on disk is skipped without contacting the portal again —
so re-running `fetch` over an overlapping range doesn't re-pay the paced
request cost for documents you already have. Ambiguous or stale entries
just fall back to the normal fetch-then-check-disk path. `-all` bypasses
both the log and the on-disk check.

`-user-agent` overrides the built-in browser User-Agent string.

    # check GitHub for a newer release, and install it
    flatex-fetch upgrade
    flatex-fetch upgrade -check   # report only, no download/prompt
    flatex-fetch upgrade -y       # skip the confirmation prompt

`upgrade` fetches the latest GitHub release, and if it's newer than the
running binary, prompts `Upgrade to vX.Y.Z? [y/N]` before downloading and
verifying it (SHA-256, against the release's `SHA256SUMS.txt`) and replacing
the binary in place. The current process keeps running the old binary until
restarted. `-check` reports availability without downloading or prompting,
with exit codes scripts can branch on: `0` up to date, `1` upgrade
available, `2` on error. `-y` skips the prompt; combined with `-check`,
`-check` wins (no download either way). A local/dev build (`flatex-fetch
-version` prints `dev`) is always treated as upgradable.

### Output path templates

`-format` replaces the fixed `<profile>/<filename>` layout with a template,
still rooted at `-out`. Placeholders are substituted per document and split
on `/` into directories:

| Token | Value |
|---|---|
| `<profile>` | profile name |
| `<type>` | document category, as shown by `list` |
| `<filename>` (or `<original filename>`, `<org filename>`) | portal's original filename, extension stripped |
| `<date>` | document date, `YYYY-MM-DD` |
| `<date LAYOUT>` | document date with `LAYOUT` built from `YYYY`/`MM`/`DD` |

```
flatex-fetch fetch -format "<type>/<date YYYY-MM-DD>/<filename>.pdf"
# -> flatex-downloads/Kontoauszug/2026-07-16/invoice.pdf

flatex-fetch fetch -format "<profile>/<date YYYY>/<date>-<type>-<filename>.pdf"
# -> flatex-downloads/main/2026/2026-07-16-Kontoauszug-invoice.pdf
```

An unrecognized `<token>` is rejected before login. Incremental skip
(`-all` off) still applies — since category/date are stable per document,
the rendered path is the same across runs.

## Known Limitations

- flatex.at only; flatex.de is untested (`-domain` exists but unverified).
- No 2FA handling — document access currently doesn't require it.
- A custom date-range filter caps the portal's own results at 100
  documents — not always with a UI warning to detect, so this is caught by
  row count too — and a too-wide range can silently return nothing at all
  instead. `fetch`/`list` work around both by adaptively splitting a wide
  range into as many sub-windows as the portal actually needs.

Login, document listing, and download are confirmed working against a real
flatex.at account (2026-07-16), including windowed listing across a wide
date range (2026-07-21: 235 documents from a year, correctly split into 4
sub-windows).

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
