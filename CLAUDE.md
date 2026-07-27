# flatex-fetch

## Project Status

Go CLI that logs into flatex.at and downloads document-archive PDFs.

**Live portal verification:** login, listing, download, and windowed
listing across a wide date range are all confirmed working against a real
flatex.at account (2026-07-16; windowing confirmed correct 2026-07-21 —
`TestE2EWindowedListingAndDownload` listed 235 documents across the last
year from 4 sub-windows in 27s, and downloaded one using its own
`WindowFrom`/`WindowTo`). Getting here took two live-caught bugs on the
way: a too-wide single query can come back silently empty (no table
rendered at all, detected via a missing `tableMarker`), and a capped
response doesn't reliably show the UI's own cap-warning text (a live run
trusting that text alone silently missed real documents — row count
hitting `capLimit`, 100, is the reliable signal). `windowedDocuments`
always tries the full requested range first and only splits on one of
those two signals, so the request count adapts to what the portal actually
needs (4 for this account/range) rather than a guessed fixed size. The
portal's "load more" control (`fieldRetrieveMore` in markup.go) was tried
first and abandoned — two separate live failures, documented in
`TestE2EPagination`; nothing production uses it.

**flatex-next support (added 2026-07-27):** the newer React-shell UI
(`next-desktop.at`) is a different frontend on the same backend session
framework. `Client.Login` auto-detects it from where the `/login.at/sso`
POST's redirect chain lands — no flag, no CLI change. Reverse-engineered
from a single live Chrome netlog capture (raw bytes included, so
request/response bodies were visible, not just headers) of one real
account with one ~1-month date range (~200 documents). Login itself
(`internal/portal/portal.go`'s `loginNext`) mirrors the capture
step-for-step, including a `resumeLogin` AJAX command the session doesn't
work without. Listing/download (`internal/portal/next.go`) use a
completely different widget model than the old UI — entries grouped under
per-date headers instead of a flat table, one-document-at-a-time download
instead of batch-select, and pagination via an incrementing
`scrollposition` field instead of date-windowing (`nextListDocuments`
pages until a request stops returning more entries than the last one).
Login, listing, and download are confirmed working against a real
flatex-next account (2026-07-27, a 7-day window — 4 documents, one new
download plus three correctly deduped as already-downloaded). Getting a
working run took one live-caught bug: `archiveListPath` was pointed at
the old UI's action name (`documentArchiveListFormAction.do`) instead of
flatex-next's actual `overviewFormAction.do`, which the unit tests didn't
catch because they used the same wrong constant on both the mock server
and the assertion — every request came back as a `redirect` command to
`error404.html`, caught via `-verbose`'s command-name diagnostics
(`describeCommands` in next.go).
**What's still unverified:** pagination beyond a single batch —
`nextScrollStep` and its behavior at scale, and whether flatex-next has
anything like `capLimit` — since the only live run so far never triggered
the scroll loop (well under one batch). Treat a flatex-next result set
larger than ~50 documents with more suspicion than an old-UI one until
that's been exercised — same pattern as the
old UI's own windowing history above.

**flatex.de host/segment derivation (2026-07-27, GitHub issue #11):** a
flatex.de user hit `HTTP 404` on `banking-flatex.de/accountOverviewFormAction.do`
— the account may be a flatex-next one that our then-hardcoded
`next-desktop.at` detection couldn't recognize, falling through to the old
UI's path and 404ing. Both the portal host (`konto.<domain>`, was a fixed
`konto.flatex.at` literal) and the flatex-next segment
(`nextDesktopSegmentFor` in `internal/portal/portal.go`, was a fixed
`next-desktop.at` literal) are now derived from the profile's `domain`
instead. Re-verified live against the real flatex.at + flatex-next account
after the refactor — no regression from switching host/segment to be
domain-derived. This assumes flatex.de mirrors flatex.at's pattern exactly
(`konto.flatex.de`, `next-desktop.de`) — still **unconfirmed live**; no
flatex.de account has been tested against this. If the issue reporter's
retry still 404s, the actual redirect URL from his run is needed to find
the real pattern.

## Build

    go build -o flatex-fetch

## Test

    go test ./...                 # unit tests (httptest mock backend, no real account)
    go test -race ./...           # what CI runs
    FLATEX_E2E_USER=... FLATEX_E2E_PASS=... go test -tags e2e ./internal/portal/   # live portal, manual only

## Release

Tag `v*` → `.github/workflows/release.yml` builds darwin/linux/windows ×
amd64/arm64 and attaches binaries to the GitHub release.

**The workflow does NOT write release notes** — `softprops/action-gh-release`
is called with only `files:`, no `body`/`generate_release_notes`. Every tag
push creates a release with an EMPTY description unless you write it by
hand afterward. This has been forgotten before (v0.3.0 and v0.4.0 both
shipped with no notes until fixed retroactively) — after pushing a tag,
always follow up with:

    gh release edit vX.Y.Z --notes "$(cat <<'EOF'
    <one-line summary>

    ## Added / Changed / Fixed
    - ...

    **Full Changelog**: https://github.com/welworx/flatex-fetch/compare/vPREV...vX.Y.Z
    EOF
    )"

Match the style already in past releases (`gh release view vX.Y.Z --json body -q .body`
to see examples) — sections drawn from `git log vPREV..vX.Y.Z`, not just the
latest commit message.

## Notes

- CLI flags/env vars/behavior are documented in two places: `main.go`'s
  `-help` text (canonical) and `README.md` (Usage/Setup/ENVIRONMENT). Update
  both in the same change — this has drifted before. Don't add a third copy
  (e.g. a full `-help` dump in the README).
- `internal/portal/markup.go` holds every portal-derived constant (paths,
  form fields, combobox indices, regexes). Filter comboboxes are numeric
  indices, not stable IDs — if flatex reorders a dropdown, fix it there.
- Portal requests are paced (~750ms) — don't "optimize" that away, it's
  bot-detection avoidance.
- Credentials: `~/.config/flatex-fetch/credentials.enc`, master passphrase
  via prompt or `FLATEX_FETCH_PASSPHRASE`.
