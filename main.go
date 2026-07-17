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
  flatex-fetch fetch -profile <name>|-all-profiles [-out DIR] [-user-agent UA] [-days N | -from YYYY-MM-DD -to YYYY-MM-DD] [-all]
  flatex-fetch list -profile <name>|-all-profiles [-user-agent UA] [-days N | -from YYYY-MM-DD -to YYYY-MM-DD] [-csv | -json]
  flatex-fetch -version`)
	return 2
}

func run(args []string) int {
	if len(args) == 0 {
		return usage()
	}
	switch args[0] {
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
