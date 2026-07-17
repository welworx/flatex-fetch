package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/welworx/flatex-fetch/internal/config"
	"github.com/welworx/flatex-fetch/internal/portal"
)

// dateRange resolves the -days / -from / -to flag semantics. Explicit
// from/to (both required together) overrides days.
func dateRange(days int, from, to string, now time.Time) (time.Time, time.Time, error) {
	if (from == "") != (to == "") {
		return time.Time{}, time.Time{}, errors.New("-from and -to must be used together")
	}
	if from == "" {
		return now.AddDate(0, 0, -days), now, nil
	}
	f, err := time.Parse("2006-01-02", from)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("-from: %w", err)
	}
	t, err := time.Parse("2006-01-02", to)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("-to: %w", err)
	}
	if f.After(t) {
		return time.Time{}, time.Time{}, errors.New("-from is after -to")
	}
	return f, t, nil
}

func runFetch(args []string) int {
	fs := flag.NewFlagSet("fetch", flag.ContinueOnError)
	profileName := fs.String("profile", "", "profile to fetch")
	allProfiles := fs.Bool("all-profiles", false, "fetch every configured profile")
	out := fs.String("out", "", "output directory (default ~/flatex-downloads)")
	userAgent := fs.String("user-agent", "", "override the built-in browser User-Agent")
	days := fs.Int("days", 90, "fetch documents from the last N days")
	fromFlag := fs.String("from", "", "start date YYYY-MM-DD (with -to; overrides -days)")
	toFlag := fs.String("to", "", "end date YYYY-MM-DD (with -from)")
	all := fs.Bool("all", false, "re-download documents that already exist locally")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if (*profileName == "") == !*allProfiles {
		fmt.Fprintln(os.Stderr, "error: exactly one of -profile or -all-profiles is required")
		return 2
	}
	from, to, err := dateRange(*days, *fromFlag, *toFlag, time.Now())
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}
	if *out == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		*out = filepath.Join(home, "flatex-downloads")
	}

	dir, err := config.Dir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	profiles, err := config.LoadProfiles(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if !*allProfiles {
		found := false
		for _, p := range profiles {
			if p.Name == *profileName {
				profiles = []config.Profile{p}
				found = true
				break
			}
		}
		if !found {
			fmt.Fprintf(os.Stderr, "error: no profile %q (run: flatex-fetch profile add %s)\n", *profileName, *profileName)
			return 1
		}
	}
	if len(profiles) == 0 {
		fmt.Fprintln(os.Stderr, "error: no profiles configured")
		return 1
	}

	pass, err := readPassphrase(false)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	creds, err := config.LoadCredentials(dir, pass)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	failed := false
	for _, p := range profiles {
		if err := fetchProfile(p, creds[p.Name], *out, *userAgent, from, to, *all); err != nil {
			fmt.Fprintf(os.Stderr, "profile %s: %v\n", p.Name, err)
			failed = true
		}
	}
	if failed {
		return 1
	}
	return 0
}

// fetchProfile logs in and downloads one profile's documents. A single
// failed document is logged and skipped; only login/listing failures abort
// the profile.
func fetchProfile(p config.Profile, password, out, userAgent string, from, to time.Time, overwrite bool) error {
	if password == "" {
		return errors.New("no stored password (re-add the profile)")
	}
	c, err := portal.New(p.Domain, userAgent)
	if err != nil {
		return err
	}
	if err := c.Login(p.Username, password); err != nil {
		return err
	}
	rows, err := c.ListDocuments(from, to)
	if err != nil {
		return err
	}
	destDir := filepath.Join(out, p.Name)
	seen := map[string]bool{}
	downloaded, skipped, failedDocs := 0, 0, 0
	for _, idx := range rows {
		path, wasSkipped, err := c.Download(from, to, idx, destDir, seen, overwrite)
		switch {
		case errors.Is(err, portal.ErrChallenged):
			fmt.Fprintf(os.Stderr, "profile %s: row %d: blocked by bot-check challenge\n", p.Name, idx)
			failedDocs++
		case err != nil:
			fmt.Fprintf(os.Stderr, "profile %s: row %d: %v\n", p.Name, idx, err)
			failedDocs++
		case wasSkipped:
			skipped++
		default:
			fmt.Println(path)
			downloaded++
		}
	}
	fmt.Fprintf(os.Stderr, "profile %s: %d downloaded, %d already present, %d failed\n",
		p.Name, downloaded, skipped, failedDocs)
	if failedDocs > 0 {
		return fmt.Errorf("%d document(s) failed", failedDocs)
	}
	return nil
}
