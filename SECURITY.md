# Security Policy

## Supported Versions

Only the latest release is supported. This is a personal, educational-use
project (see the [README disclaimer](README.md#disclaimer)) with no formal
support commitment.

## Portal Destinations

Portal clients accept only the exact profile domains `flatex.at` and
`flatex.de`. Login, archive navigation, document downloads, and HTTP
redirects are restricted to the selected `https://konto.<domain>` origin.
Redirects to other hosts, ports, or HTTP are rejected before sending the
request, including redirects that would resend credentials in a POST body.
Server-provided locations containing URL user information are also rejected.

This deliberately fails closed if Flatex changes to a separate login or
download origin. Verify any such change before extending the allowed origins.

## Automated Scanning

CodeQL scans every push/PR to `main` (badge on the README). Dependabot opens
weekly update PRs for Go modules and GitHub Actions (`.github/dependabot.yml`).
Neither replaces a real audit — report anything they miss below.

## Reporting a Vulnerability

Please **do not** open a public GitHub issue for security vulnerabilities
(e.g. credential handling, encryption, or anything that could expose portal
credentials or session data).

Instead, report privately via [GitHub Security
Advisories](https://github.com/welworx/flatex-fetch/security/advisories/new)
or by emailing 3889195+welworx@users.noreply.github.com.

Include steps to reproduce and the affected version/commit. As a solo,
unpaid project there's no guaranteed response time, but reports will be
looked at and fixes released as a new tag when warranted.
