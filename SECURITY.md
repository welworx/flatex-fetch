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

## Document-only requests

Every request passes through a mandatory HTTP transport policy before it
reaches the network. This includes redirects, direct downloads and session
resynchronization. The policy accepts only the known login and document
operations for the selected country. There is no bypass flag.

The shared overview and command endpoints require exact permitted form
fields and values. Unknown operations, fields, query parameters and methods
are rejected. Login excludes `sessionPassword` and transaction authorization
fields. Archive requests require `storeSettings.checked=off`. Order entry,
transfers and account-setting forms are not allowed. Duplicate parameters,
file uploads, ambiguous paths, override headers and unrecognized request
headers are rejected. POST bodies are bounded to 64 KiB and the exact checked
bytes are forwarded.

This is a local code restriction, **not a read-only permission granted by
Flatex**. The normal login still creates a bank session. Login/session
activity and document downloads may update login history or mark documents
as read. Numeric menu/widget identifiers depend on the bank's current user
interface. Changes in their meaning, bank-side behavior, modified binaries,
stolen credentials or a compromised computer are outside this guarantee.

Unknown legitimate portal changes also fail closed. Check them against
observed document-only traffic before updating the policy. Automated tests
cover Austria, Germany and flatex-next using local mock servers. The hardened
binary requires a fresh live document-download test before compatibility
can be claimed for a real account.

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
