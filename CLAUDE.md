# flatex-fetch

## Project Status

Go CLI that logs into flatex.at and downloads document-archive PDFs.

**Live portal verification:** login, listing, and download are confirmed
working against a real flatex.at account (2026-07-16). Known gap: only the
first page of archive results is fetched — pagination via the portal's
"load more" control isn't implemented yet.

## Build

    go build -o flatex-fetch

## Test

    go test ./...                 # unit tests (httptest mock backend, no real account)
    go test -race ./...           # what CI runs
    FLATEX_E2E_USER=... FLATEX_E2E_PASS=... go test -tags e2e ./internal/portal/   # live portal, manual only

## Release

Tag `v*` → `.github/workflows/release.yml` builds darwin/linux/windows ×
amd64/arm64 and attaches binaries to the GitHub release.

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
