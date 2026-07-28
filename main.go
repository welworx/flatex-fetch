package main

import (
	"fmt"
	"os"
)

var version = "dev" // set via -ldflags "-X main.version=..."

func main() {
	os.Exit(run(os.Args[1:]))
}

func usage() int {
	fmt.Fprintln(os.Stderr, `usage:
  flatex-fetch [-config-dir DIR] <command> [flags]
  flatex-fetch profile add <name> -domain flatex.at
  flatex-fetch profile list
  flatex-fetch profile update <name> [-domain flatex.at]
  flatex-fetch profile remove <name>
  flatex-fetch profile passphrase
  flatex-fetch fetch [-profile <name>|-all-profiles] [-out DIR] [-format TEMPLATE] [-user-agent UA] [-days N | -from YYYY-MM-DD -to YYYY-MM-DD | -since-last] [-all] [-verbose]
  flatex-fetch list [-profile <name>|-all-profiles] [-user-agent UA] [-days N | -from YYYY-MM-DD -to YYYY-MM-DD] [-csv | -json]
  flatex-fetch upgrade [-check] [-y]
  flatex-fetch -version

Run 'flatex-fetch -help' for a full description and examples.`)
	return 2
}

func help() int {
	fmt.Print(`flatex-fetch - download document-archive PDFs from the flatex.at broker portal

Logs into flatex.at and fetches documents (trade confirmations, dividend
notices, ...) as raw PDFs, or lists them without downloading. Supports
several portal logins ("profiles") in one run.

DISCLAIMER
  Personal, educational use only. Unofficial, not affiliated with flatex.
  Drives flatex's web portal like a browser would, which may not be
  permitted under flatex's Terms of Service — check those before using
  this. Use is entirely at your own risk.

USAGE
  flatex-fetch [-config-dir DIR] <command> [flags]

GLOBAL FLAGS
  -config-dir DIR    use DIR instead of the OS default config location for
                      credentials.enc (same effect as FLATEX_FETCH_CONFIG_DIR;
                      must come before <command>)

COMMANDS
  profile add <name> [-domain flatex.at]   add a profile (prompts for credentials)
  profile list                             list configured profiles
  profile update <name> [-domain flatex.at]
                                            change a profile's username/password/domain
                                            (prompts; blank answer keeps the current value)
  profile remove <name>                    remove a profile
  profile passphrase                       change the master passphrase (re-encrypts everything;
                                            prompts for current + new passphrase, no profile data changes)
  fetch [flags]                            download new documents
  list [flags]                             list documents without downloading
  upgrade [-check] [-y]                    check GitHub for a newer release and install it
  -version                                 print the version

FETCH/LIST FLAGS
  -profile <name>    profile to use (default: first configured profile)
  -all-profiles      use every configured profile instead of one
  -days N            date range: last N days (default 7)
  -from YYYY-MM-DD   date range start (use with -to; overrides -days)
  -to YYYY-MM-DD     date range end (use with -from)
  -user-agent UA     override the built-in browser User-Agent
  -verbose           print progress to stderr: resolved settings (even ones left at their
                      default), date ranges queried, documents found; fetch also prints
                      per-document skip/download status
  fetch also:
  -out DIR           output directory (default ~/flatex-downloads)
  -format TEMPLATE   output path template relative to -out (default <profile>/<filename>)
  -since-last        fetch from each profile's latest already-fetched document date, in
                      <out>/.fetch-log.jsonl, through today (falls back to -days if no log yet;
                      mutually exclusive with -days/-from/-to)
  -all               re-download documents that already exist locally
  list also:
  -csv               output CSV instead of a table
  -json              output JSON instead of a table

UPGRADE FLAGS
  -check              report whether a newer release exists, without installing it;
                       exit code: 0 up to date, 1 upgrade available, 2 on error
  -y                   skip the "Upgrade to vX.Y.Z? [y/N]" confirmation prompt

FORMAT TEMPLATE
  -format takes a path template with <token> placeholders, substituted per
  document and split on "/" into directories:
    <profile>       profile name
    <filename>      portal's original filename, extension stripped
                    (aliases: <original filename>, <org filename>)
    <date>          document date, YYYY-MM-DD
    <date LAYOUT>   document date with LAYOUT built from YYYY/MM/DD

  Example: -format "<date YYYY-MM-DD>/<filename>.pdf"
    -> flatex-downloads/2026-07-16/invoice.pdf

  Example: -format "<profile>/<date YYYY>/<date>-<filename>.pdf"
    -> flatex-downloads/main/2026/2026-07-16-invoice.pdf

DOWNLOAD LOG
  Every file fetch writes is also appended, one JSON object per line, to
  <out>/.fetch-log.jsonl: time, profile, document index/date/name,
  and the local path. On a later run, a listed document is skipped without
  contacting the portal again if the log has exactly one matching entry
  (by date/name) and its file still exists on disk; an ambiguous
  or stale log entry falls back to fetching normally. -all bypasses this
  and the on-disk check both.

ENVIRONMENT
  FLATEX_FETCH_PASSPHRASE   credentials.enc master passphrase (skip the prompt)
  FLATEX_FETCH_USERNAME     portal username for 'profile add'/'profile update' (skip the
                            prompt); for fetch/list, set with FLATEX_FETCH_PASSWORD to
                            skip credentials.enc entirely (-profile/-all-profiles
                            are ignored) and log in as "from-env" instead
  FLATEX_FETCH_PASSWORD     portal password, see FLATEX_FETCH_USERNAME above
  FLATEX_FETCH_DOMAIN       portal domain for the FLATEX_FETCH_USERNAME/PASSWORD
                            login (default flatex.at)
  FLATEX_FETCH_CONFIG_DIR   config directory, overriding the OS default (same as -config-dir)

FILES
  <config dir>/credentials.enc    every profile (name, username, domain,
                                   password), encrypted (argon2id + AES-256-GCM)
                                   under one master passphrase, changeable with
                                   'profile passphrase'
  <config dir> defaults to the OS config location (~/.config/flatex-fetch on
  Linux, ~/Library/Application Support/flatex-fetch on macOS); override with
  -config-dir or FLATEX_FETCH_CONFIG_DIR. Upgrading from an older version:
  the first command run against an existing config dir auto-migrates its
  plaintext profiles.json into credentials.enc and renames it to
  profiles.json.bak.

EXAMPLES
  # first-time setup
  flatex-fetch profile add main -domain flatex.at

  # everyday use: new docs from the last 7 days, first configured profile
  flatex-fetch fetch

  # a specific profile, wider range
  flatex-fetch fetch -profile main -days 90

  # continue from each profile's newest already-fetched document date
  flatex-fetch fetch -since-last -all-profiles

  # a wide range, with progress on stderr instead of silence until the end
  flatex-fetch fetch -profile main -days 1800 -verbose

  # exact date range, re-downloading even what's already on disk
  flatex-fetch fetch -all -from 2026-01-01 -to 2026-06-30

  # every profile in one run
  flatex-fetch fetch -all-profiles

  # inspect what's there without downloading, as JSON
  flatex-fetch list -profile main -json

  # non-interactive setup for cron/CI
  FLATEX_FETCH_PASSPHRASE=... FLATEX_FETCH_USERNAME=... FLATEX_FETCH_PASSWORD=... \
    flatex-fetch profile add main

  # fetch without a stored profile at all
  FLATEX_FETCH_USERNAME=... FLATEX_FETCH_PASSWORD=... flatex-fetch fetch

  # keep credentials.enc off the default disk location
  flatex-fetch -config-dir /Volumes/secure/flatex-fetch profile list

  # rotate your master passphrase (re-encrypts everything, changes nothing else)
  flatex-fetch profile passphrase
`)
	return 0
}

func run(args []string) int {
	if len(args) == 0 {
		return usage()
	}
	if args[0] == "-config-dir" {
		if len(args) < 2 {
			return usage()
		}
		os.Setenv("FLATEX_FETCH_CONFIG_DIR", args[1])
		args = args[2:]
		if len(args) == 0 {
			return usage()
		}
	}
	switch args[0] {
	case "-help", "--help", "help":
		return help()
	case "-version", "--version", "version":
		fmt.Println("flatex-fetch", version)
		return 0
	case "profile":
		return runProfile(args[1:])
	case "fetch":
		return runFetch(args[1:])
	case "list":
		return runList(args[1:])
	case "upgrade":
		return runUpgrade(args[1:])
	default:
		return usage()
	}
}
