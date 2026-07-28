package main

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/welworx/flatex-fetch/internal/config"
	"github.com/welworx/flatex-fetch/internal/portal"
)

func runList(args []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	profileName := fs.String("profile", "", "profile to list (default: first configured profile)")
	allProfiles := fs.Bool("all-profiles", false, "list every configured profile")
	userAgent := fs.String("user-agent", "", "override the built-in browser User-Agent")
	days := fs.Int("days", 7, "list documents from the last N days")
	fromFlag := fs.String("from", "", "start date YYYY-MM-DD (with -to; overrides -days)")
	toFlag := fs.String("to", "", "end date YYYY-MM-DD (with -from)")
	csvOut := fs.Bool("csv", false, "output CSV instead of a table")
	jsonOut := fs.Bool("json", false, "output JSON instead of a table")
	verbose := fs.Bool("verbose", false, "print progress to stderr: date ranges queried, documents found")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "error: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if !profileFlagsValid(*profileName, *allProfiles) {
		fmt.Fprintln(os.Stderr, "error: -profile and -all-profiles are mutually exclusive")
		return 2
	}
	if *csvOut && *jsonOut {
		fmt.Fprintln(os.Stderr, "error: -csv and -json are mutually exclusive")
		return 2
	}
	explicitRange := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "days" || f.Name == "from" || f.Name == "to" {
			explicitRange = true
		}
	})
	from, to, err := dateRange(*days, *fromFlag, *toFlag, time.Now())
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}

	profiles, err := resolveProfiles(*profileName, *allProfiles)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	if *verbose {
		fmt.Fprintf(os.Stderr, "settings: format=%s range=%s user-agent=%s\n",
			outputFormatDescription(*csvOut, *jsonOut),
			rangeDescription(*days, from, to, explicitRange, false),
			orDefault(*userAgent, portal.DefaultUserAgent))
	}

	failed := false
	for _, p := range profiles {
		if len(profiles) > 1 {
			fmt.Printf("== %s ==\n", p.Name)
		}
		if err := listProfile(p, p.Password, *userAgent, from, to, *csvOut, *jsonOut, *verbose); err != nil {
			fmt.Fprintf(os.Stderr, "profile %s: %v\n", p.Name, err)
			failed = true
		}
	}
	if failed {
		return 1
	}
	return 0
}

// outputFormatDescription labels list's chosen output format for a
// -verbose settings summary, including when it's the unflagged default.
func outputFormatDescription(csvOut, jsonOut bool) string {
	switch {
	case csvOut:
		return "csv"
	case jsonOut:
		return "json"
	default:
		return "table (default)"
	}
}

// listProfile logs in and prints one profile's document listing.
func listProfile(p config.Profile, password, userAgent string, from, to time.Time, csvOut, jsonOut, verbose bool) error {
	if password == "" {
		return errors.New("no stored password (re-add the profile)")
	}
	c, err := portal.New(p.Domain, userAgent)
	if err != nil {
		return err
	}
	if verbose {
		c.Log = func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, "profile %s: "+format+"\n", append([]any{p.Name}, args...)...)
		}
	}
	if err := c.Login(p.Username, password); err != nil {
		return err
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "profile %s (domain %s): listing documents %s..%s\n", p.Name, p.Domain, from.Format("2006-01-02"), to.Format("2006-01-02"))
	}
	docs, err := c.ListDocumentsDetailed(from, to)
	if err != nil {
		return err
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "profile %s: %d document(s) in range\n", p.Name, len(docs))
	}
	switch {
	case jsonOut:
		return writeDocumentsJSON(os.Stdout, docs, p.Name)
	case csvOut:
		return writeDocumentsCSV(os.Stdout, docs, p.Name)
	default:
		writeDocumentsTable(os.Stdout, docs, p.Name)
		return nil
	}
}

type documentRow struct {
	Profile string `json:"profile"`
	Index   int    `json:"index"`
	Name    string `json:"name"`
	Date    string `json:"date"`
	Read    bool   `json:"read"`
}

// toDocumentRows attaches profile so CSV/JSON output stays unambiguous when
// -all-profiles combines multiple profiles' documents into one stream.
func toDocumentRows(docs []portal.Document, profile string) []documentRow {
	rows := make([]documentRow, len(docs))
	for i, d := range docs {
		rows[i] = documentRow{
			Profile: profile,
			Index:   d.Index,
			Name:    d.Name,
			Date:    d.Date.Format("2006-01-02"),
			Read:    d.Read,
		}
	}
	return rows
}

func writeDocumentsTable(w *os.File, docs []portal.Document, profile string) {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "PROFILE\tINDEX\tDATE\tREAD\tNAME")
	for _, d := range toDocumentRows(docs, profile) {
		fmt.Fprintf(tw, "%s\t%d\t%s\t%t\t%s\n", d.Profile, d.Index, d.Date, d.Read, d.Name)
	}
	tw.Flush()
}

func writeDocumentsCSV(w *os.File, docs []portal.Document, profile string) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"profile", "index", "date", "read", "name"}); err != nil {
		return err
	}
	for _, d := range toDocumentRows(docs, profile) {
		if err := cw.Write([]string{
			d.Profile, fmt.Sprint(d.Index), d.Date, fmt.Sprint(d.Read), d.Name,
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func writeDocumentsJSON(w *os.File, docs []portal.Document, profile string) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(toDocumentRows(docs, profile))
}
