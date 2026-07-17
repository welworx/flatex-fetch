package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/welworx/flatex-fetch/internal/config"
	"github.com/welworx/flatex-fetch/internal/portal"
)

// profileFlagsValid reports whether -profile and -all-profiles aren't both
// given. Neither given is valid: it means "use the first configured
// profile".
func profileFlagsValid(profileName string, allProfiles bool) bool {
	return profileName == "" || !allProfiles
}

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

// resolveProfilesAndCreds selects the profile(s) named by -profile/
// -all-profiles and decrypts stored credentials. Callers must validate
// -profile/-all-profiles aren't both set themselves first (see
// profileFlagsValid) since that's a usage error (exit 2), distinct from the
// runtime errors here (exit 1). Shared by fetch and list, which both need
// exactly this before talking to the portal.
func resolveProfilesAndCreds(profileName string, allProfiles bool) ([]config.Profile, map[string]string, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, nil, err
	}
	profiles, err := config.LoadProfiles(dir)
	if err != nil {
		return nil, nil, err
	}
	if len(profiles) == 0 {
		return nil, nil, errors.New("no profiles configured (run: flatex-fetch profile add <name>)")
	}
	switch {
	case allProfiles:
		// use every profile
	case profileName != "":
		found := false
		for _, p := range profiles {
			if p.Name == profileName {
				profiles = []config.Profile{p}
				found = true
				break
			}
		}
		if !found {
			return nil, nil, fmt.Errorf("no profile %q (run: flatex-fetch profile add %s)", profileName, profileName)
		}
	default:
		profiles = []config.Profile{profiles[0]}
	}

	pass, err := readPassphrase(false)
	if err != nil {
		return nil, nil, err
	}
	creds, err := config.LoadCredentials(dir, pass)
	if err != nil {
		return nil, nil, err
	}
	return profiles, creds, nil
}

func runFetch(args []string) int {
	fs := flag.NewFlagSet("fetch", flag.ContinueOnError)
	profileName := fs.String("profile", "", "profile to fetch (default: first configured profile)")
	allProfiles := fs.Bool("all-profiles", false, "fetch every configured profile")
	out := fs.String("out", "", "output directory (default ~/flatex-downloads)")
	format := fs.String("format", "", `output path template relative to -out, e.g. "<type>/<date YYYY-MM-DD>/<filename>.pdf" (default: <profile>/<filename>, the portal's own name)`)
	userAgent := fs.String("user-agent", "", "override the built-in browser User-Agent")
	days := fs.Int("days", 7, "fetch documents from the last N days")
	fromFlag := fs.String("from", "", "start date YYYY-MM-DD (with -to; overrides -days)")
	toFlag := fs.String("to", "", "end date YYYY-MM-DD (with -from)")
	all := fs.Bool("all", false, "re-download documents that already exist locally")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !profileFlagsValid(*profileName, *allProfiles) {
		fmt.Fprintln(os.Stderr, "error: -profile and -all-profiles are mutually exclusive")
		return 2
	}
	if *format != "" {
		if err := validatePathTemplate(*format); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 2
		}
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

	profiles, creds, err := resolveProfilesAndCreds(*profileName, *allProfiles)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	failed := false
	for _, p := range profiles {
		if err := fetchProfile(p, creds[p.Name], *out, *format, *userAgent, from, to, *all); err != nil {
			fmt.Fprintf(os.Stderr, "profile %s: %v\n", p.Name, err)
			failed = true
		}
	}
	if failed {
		return 1
	}
	return 0
}

// documentPathResolver builds the portal.ResolvePath used for one
// document's download. With no -format, it reproduces the historical
// layout: out/profile/<portal filename>. With -format, the template is
// rendered using the document's profile/type/date plus its resolved
// filename (extension stripped, since templates supply their own).
func documentPathResolver(out, format, profile string, d portal.Document) portal.ResolvePath {
	return func(origName string) (string, string) {
		if format == "" {
			return filepath.Join(out, profile), origName
		}
		stem := strings.TrimSuffix(origName, filepath.Ext(origName))
		dir, name := renderPathTemplate(format, profile, d.Category, d.Date, stem)
		return filepath.Join(out, dir), name
	}
}

// fetchProfile logs in and downloads one profile's documents. A single
// failed document is logged and skipped; only login/listing failures abort
// the profile.
func fetchProfile(p config.Profile, password, out, format, userAgent string, from, to time.Time, overwrite bool) error {
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
	docs, err := c.ListDocumentsDetailed(from, to)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	downloaded, skipped, failedDocs := 0, 0, 0
	for _, d := range docs {
		resolvePath := documentPathResolver(out, format, p.Name, d)
		path, wasSkipped, err := c.Download(from, to, d.Index, resolvePath, seen, overwrite)
		switch {
		case errors.Is(err, portal.ErrChallenged):
			fmt.Fprintf(os.Stderr, "profile %s: row %d: blocked by bot-check challenge\n", p.Name, d.Index)
			failedDocs++
		case err != nil:
			fmt.Fprintf(os.Stderr, "profile %s: row %d: %v\n", p.Name, d.Index, err)
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
