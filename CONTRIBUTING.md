# Contributing

This is a personal, educational-use project (see the [README
disclaimer](README.md#disclaimer)), but fixes and improvements are welcome.

## Before opening a PR

1. Tests pass: `go test -race ./...`
2. Code is formatted: `gofmt -l .` is empty
3. Linter passes: `golangci-lint run`

`.pre-commit-config.yaml` runs all three on commit if you want them enforced
automatically (`pip install pre-commit && pre-commit install`).

## Scope

Changes to portal interaction (`internal/portal/`) are the most fragile part
of this codebase — flatex's markup isn't a stable API, and workarounds like
request pacing are deliberate (see `CLAUDE.md` gotchas). If you're changing
behavior there, explain what portal behavior you observed and why the change
is needed.

## Reporting bugs / requesting features

Open a GitHub issue. For security issues, see [SECURITY.md](SECURITY.md)
instead of filing a public issue.
