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
  flatex-fetch profile add <name> -domain flatex.at
  flatex-fetch profile list
  flatex-fetch profile remove <name>
  flatex-fetch fetch [-profile <name>|-all-profiles] [-out DIR] [-user-agent UA] [-days N | -from YYYY-MM-DD -to YYYY-MM-DD] [-all]
  flatex-fetch list [-profile <name>|-all-profiles] [-user-agent UA] [-days N | -from YYYY-MM-DD -to YYYY-MM-DD] [-csv | -json]
  flatex-fetch -version

Run 'flatex-fetch -help' for a full description and examples.`)
	return 2
}

func help() int {
	fmt.Print(`flatex-fetch - download document-archive PDFs from the flatex.at broker portal

Logs into flatex.at and fetches documents (trade confirmations, dividend
notices, ...) as raw PDFs, or lists them without downloading. Supports
several portal logins ("profiles") in one run.

USAGE
  flatex-fetch <command> [flags]

COMMANDS
  profile add <name> [-domain flatex.at]   add a profile (prompts for credentials)
  profile list                             list configured profiles
  profile remove <name>                    remove a profile
  fetch [flags]                            download new documents
  list [flags]                             list documents without downloading
  -version                                 print the version

FETCH/LIST FLAGS
  -profile <name>    profile to use (default: first configured profile)
  -all-profiles      use every configured profile instead of one
  -days N            date range: last N days (default 7)
  -from YYYY-MM-DD   date range start (use with -to; overrides -days)
  -to YYYY-MM-DD     date range end (use with -from)
  -user-agent UA     override the built-in browser User-Agent
  fetch also:
  -out DIR           output directory (default ~/flatex-downloads)
  -all               re-download documents that already exist locally
  list also:
  -csv               output CSV instead of a table
  -json              output JSON instead of a table

ENVIRONMENT
  FLATEX_FETCH_PASSPHRASE   credentials.enc master passphrase (skip the prompt)
  FLATEX_FETCH_USERNAME     portal username for 'profile add' (skip the prompt)
  FLATEX_FETCH_PASSWORD     portal password for 'profile add' (skip the prompt)

FILES
  ~/.config/flatex-fetch/profiles.json      profile names, usernames, domains
  ~/.config/flatex-fetch/credentials.enc    encrypted portal passwords

EXAMPLES
  # first-time setup
  flatex-fetch profile add main -domain flatex.at

  # everyday use: new docs from the last 7 days, first configured profile
  flatex-fetch fetch

  # a specific profile, wider range
  flatex-fetch fetch -profile main -days 90

  # exact date range, re-downloading even what's already on disk
  flatex-fetch fetch -all -from 2026-01-01 -to 2026-06-30

  # every profile in one run
  flatex-fetch fetch -all-profiles

  # inspect what's there without downloading, as JSON
  flatex-fetch list -profile main -json

  # non-interactive setup for cron/CI
  FLATEX_FETCH_PASSPHRASE=... FLATEX_FETCH_USERNAME=... FLATEX_FETCH_PASSWORD=... \
    flatex-fetch profile add main
`)
	return 0
}

func run(args []string) int {
	if len(args) == 0 {
		return usage()
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
	default:
		return usage()
	}
}
