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
  flatex-fetch fetch [-profile <name>|-all-profiles] [-out DIR] [-format TEMPLATE] [-user-agent UA] [-days N | -from YYYY-MM-DD -to YYYY-MM-DD] [-all]
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

DISCLAIMER
  Personal, educational use only. Unofficial, not affiliated with flatex.
  Drives flatex's web portal like a browser would, which may not be
  permitted under flatex's Terms of Service — check those before using
  this. Use is entirely at your own risk.

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
  -format TEMPLATE   output path template relative to -out (default <profile>/<filename>)
  -all               re-download documents that already exist locally
  list also:
  -csv               output CSV instead of a table
  -json              output JSON instead of a table

FORMAT TEMPLATE
  -format takes a path template with <token> placeholders, substituted per
  document and split on "/" into directories:
    <profile>       profile name
    <type>          document category, as shown by 'list'
    <filename>      portal's original filename, extension stripped
                    (aliases: <original filename>, <org filename>)
    <date>          document date, YYYY-MM-DD
    <date LAYOUT>   document date with LAYOUT built from YYYY/MM/DD

  Example: -format "<type>/<date YYYY-MM-DD>/<filename>.pdf"
    -> flatex-downloads/Kontoauszug/2026-07-16/invoice.pdf

  Example: -format "<profile>/<date YYYY>/<date>-<type>-<filename>.pdf"
    -> flatex-downloads/main/2026/2026-07-16-Kontoauszug-invoice.pdf

ENVIRONMENT
  FLATEX_FETCH_PASSPHRASE   credentials.enc master passphrase (skip the prompt)
  FLATEX_FETCH_USERNAME     portal username for 'profile add' (skip the prompt);
                            for fetch/list, set with FLATEX_FETCH_PASSWORD to
                            skip profiles.json entirely (-profile/-all-profiles
                            are ignored) and log in as "from-env" instead
  FLATEX_FETCH_PASSWORD     portal password, see FLATEX_FETCH_USERNAME above
  FLATEX_FETCH_DOMAIN       portal domain for the FLATEX_FETCH_USERNAME/PASSWORD
                            login (default flatex.at)

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

  # fetch without a stored profile at all
  FLATEX_FETCH_USERNAME=... FLATEX_FETCH_PASSWORD=... flatex-fetch fetch
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
