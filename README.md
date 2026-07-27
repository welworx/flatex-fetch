# flatex-fetch

[![CI](https://github.com/welworx/flatex-fetch/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/welworx/flatex-fetch/actions/workflows/ci.yml)
[![CodeQL](https://github.com/welworx/flatex-fetch/actions/workflows/codeql.yml/badge.svg?branch=main)](https://github.com/welworx/flatex-fetch/actions/workflows/codeql.yml)
[![Coverage Status](https://coveralls.io/repos/github/welworx/flatex-fetch/badge.svg?branch=main)](https://coveralls.io/github/welworx/flatex-fetch?branch=main)
[![Release](https://img.shields.io/github/v/release/welworx/flatex-fetch)](https://github.com/welworx/flatex-fetch/releases/latest)
[![Go Version](https://img.shields.io/github/go-mod/go-version/welworx/flatex-fetch)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Logs into the flatex.at broker web portal and downloads new documents
(trade confirmations, dividend notices, …) as raw PDFs — headless, so it
drops into cron jobs, CI, or an AI agent's toolchain as-is. Companion to
[flatex-pdf-cli](https://github.com/welworx/flatex-pdf-cli), which parses
those PDFs into structured JSON — chain the two for a fully automated
fetch → parse pipeline.

> **Disclaimer:** Built for **personal, educational use only.** This is an
> independent, unofficial tool, not affiliated with, endorsed by, or
> supported by flatex. It drives flatex's web portal using the same requests
> your browser makes, which may not be permitted under flatex's Terms of
> Service — check those before using it. Use is entirely at your own risk;
> see [Disclaimer](#disclaimer) below for the full terms.

## Features

- **Downloads your flatex documents as PDFs**: trade confirmations,
  dividend notices, and the rest of your document archive, pulled straight
  from the portal
- **Re-running never re-downloads what you already have**: already-fetched
  documents are skipped, via the download log; `-all` forces a re-download
- **Works over any date range, even wide ones**: `-days`, explicit
  `-from`/`-to`, or `-since-last` (continue each profile from its newest
  fetched document) — wide ranges are split automatically so the portal's
  own 100-document result limit doesn't cause silently missing documents
- **Handles multiple accounts**: manage several portal logins
  (`profile add|list|remove`) and fetch one or all of them in a single run
- **Automation-ready**: no interactive prompts (env-var credentials),
  machine-readable output (`list -json`/`-csv`), and exit codes scripts can
  branch on — run unattended from cron/CI, or chain into
  [flatex-pdf-cli](https://github.com/welworx/flatex-pdf-cli)
- **Control where files land**: `-format` templates the download path
  per document (profile, date, filename) instead of a fixed layout
- **Every download is logged**: `<out>/.fetch-log.jsonl` records what was
  fetched and when, so re-runs can tell what's already on disk
- **See what's happening on long runs**: `-verbose` (`fetch` and `list`)
  prints resolved settings, date ranges queried, and documents found/
  downloaded as it goes — instead of silence until the run finishes
- **Your portal password is never stored in plaintext**: encrypted locally
  (argon2id + AES-256-GCM), unlocked by a master passphrase you set once

## Install

    brew install welworx/tap/flatex-fetch

or

    go install github.com/welworx/flatex-fetch@latest

or grab a [release binary](https://github.com/welworx/flatex-fetch/releases/latest)
(darwin/linux/windows, amd64/arm64).

## Setup

    flatex-fetch profile add main -domain flatex.at

Prompts for your portal username and password. The password is stored in
`~/.config/flatex-fetch/credentials.enc`, encrypted (argon2id + AES-256-GCM)
with a master passphrase you set on first use.

For cron/CI, every prompt above can be skipped with environment variables
instead — see [Environment Variables](#environment-variables).

## Environment Variables

| Variable | Purpose |
|---|---|
| `FLATEX_FETCH_PASSPHRASE` | `credentials.enc` master passphrase — skips the prompt |
| `FLATEX_FETCH_USERNAME` | Portal username. For `profile add`, skips the prompt. For `fetch`/`list`, combined with `FLATEX_FETCH_PASSWORD`, see below |
| `FLATEX_FETCH_PASSWORD` | Portal password — see `FLATEX_FETCH_USERNAME` |
| `FLATEX_FETCH_DOMAIN` | Portal domain for `FLATEX_FETCH_USERNAME`/`PASSWORD` logins (default `flatex.at`) |

`fetch`/`list` also accept `FLATEX_FETCH_USERNAME`/`FLATEX_FETCH_PASSWORD`
directly, skipping `profile add` and `profiles.json` entirely — useful for
cron/CI without a stored profile. In that mode `-profile`/`-all-profiles`
are ignored, and the portal domain defaults to `flatex.at` unless
overridden by `FLATEX_FETCH_DOMAIN`.

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
a failed document's message identifies it by date/name (the portal has no
stable per-document URL). `-user-agent` overrides the built-in browser
User-Agent string.

Every file `fetch` writes is also appended, one JSON object per line, to
`<out>/.fetch-log.jsonl` (time, profile, document index/date/name,
local path). On later runs, a listed document whose log entry is
unambiguous (no other document shares its date/name) and whose
file still exists on disk is skipped without contacting the portal again.
Ambiguous or stale entries fall back to the normal fetch-then-check-disk
path. `-all` bypasses both the log and the on-disk check.

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
| `<filename>` (or `<original filename>`, `<org filename>`) | portal's original filename, extension stripped |
| `<date>` | document date, `YYYY-MM-DD` |
| `<date LAYOUT>` | document date with `LAYOUT` built from `YYYY`/`MM`/`DD` |

```
flatex-fetch fetch -format "<date YYYY-MM-DD>/<filename>.pdf"
# -> flatex-downloads/2026-07-16/invoice.pdf

flatex-fetch fetch -format "<profile>/<date YYYY>/<date>-<filename>.pdf"
# -> flatex-downloads/main/2026/2026-07-16-invoice.pdf
```

An unrecognized `<token>` is rejected before login. Incremental skip
(`-all` off) still applies — since date is stable per document, the
rendered path is the same across runs.

## Known Limitations

- flatex.at only; flatex.de is untested (`-domain` exists but unverified).
- No 2FA handling — document access currently doesn't require it.
- The portal caps filtered queries at 100 documents (see "Flexible date
  ranges" under Features for how this is worked around).
- Both the classic portal UI and the newer "flatex-next" UI
  (`next-desktop.at`) are supported — auto-detected at login, no flag
  needed. Confirmed working against a real flatex-next account
  (2026-07-27) for login, listing, and download on a small (7-day, 4
  document) window. Its pagination beyond a single batch of results is
  still unverified — treat a flatex-next result set larger than ~50
  documents with more suspicion than an old-UI one for now.

Login, document listing, and download are confirmed working against a real
flatex.at account (2026-07-16), including windowed listing across a wide
date range (2026-07-21: 235 documents from a year, correctly split into 4
sub-windows).

## Dependencies

- [golang.org/x/crypto](https://pkg.go.dev/golang.org/x/crypto) — argon2id + AES-256-GCM credential encryption
- [golang.org/x/term](https://pkg.go.dev/golang.org/x/term) — masked password prompt

## Contributing

Contributions are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md).

## License

The code is licensed under the [MIT License](LICENSE) — free to read,
modify, and redistribute with the copyright notice retained and no warranty.
That covers the code itself only; see [Disclaimer](#disclaimer) for use
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
