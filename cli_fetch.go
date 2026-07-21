package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
//
// If FLATEX_FETCH_USERNAME/PASSWORD are set, profiles.json/credentials.enc
// are skipped entirely (-profile/-all-profiles are ignored) and a single
// synthetic "from-env" profile is used instead, for cron/CI use without a
// stored profile.
func resolveProfilesAndCreds(profileName string, allProfiles bool) ([]config.Profile, map[string]string, error) {
	if envUser, envPass := os.Getenv("FLATEX_FETCH_USERNAME"), os.Getenv("FLATEX_FETCH_PASSWORD"); envUser != "" && envPass != "" {
		domain := os.Getenv("FLATEX_FETCH_DOMAIN")
		if domain == "" {
			domain = "flatex.at"
		}
		p := config.Profile{Name: "from-env", Username: envUser, Domain: domain}
		return []config.Profile{p}, map[string]string{"from-env": envPass}, nil
	}

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
	sinceLast := fs.Bool("since-last", false, "fetch documents from each profile's latest already-fetched document date, in <out>/.fetch-log.jsonl, through today (falls back to -days if no log yet); mutually exclusive with -days/-from/-to")
	all := fs.Bool("all", false, "re-download documents that already exist locally")
	verbose := fs.Bool("verbose", false, "print progress to stderr: date ranges queried, documents found, per-document skip/download status")
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
	explicitRange := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "days" || f.Name == "from" || f.Name == "to" {
			explicitRange = true
		}
	})
	if *sinceLast && explicitRange {
		fmt.Fprintln(os.Stderr, "error: -since-last is mutually exclusive with -days/-from/-to")
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
		if err := fetchProfile(p, creds[p.Name], *out, *format, *userAgent, from, to, *sinceLast, *all, *verbose); err != nil {
			fmt.Fprintf(os.Stderr, "profile %s: %v\n", p.Name, err)
			failed = true
		}
	}
	if failed {
		return 1
	}
	return 0
}

// describeDocument identifies a document in an error message. The portal
// has no stable per-document URL (a download is triggered by POSTing the
// row index within the same session, not by fetching a fixed link), so
// date/category/name — everything visible in the portal's own archive
// table — is the closest identifying handle available.
func describeDocument(d portal.Document) string {
	return fmt.Sprintf("row %d (%s, %s, %q)", d.Index, d.Date.Format("2006-01-02"), d.Category, d.Name)
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
// the profile. With sinceLast, from/to are overridden per-profile from that
// profile's own latest already-fetched document date (falling back to the
// given from/to if the profile has no log entries yet). With verbose, progress
// (windows queried, documents found, per-document skip/download outcome)
// is printed to stderr as it happens — useful on a wide date range, where
// otherwise nothing prints until the final summary line.
func fetchProfile(p config.Profile, password, out, format, userAgent string, from, to time.Time, sinceLast, overwrite, verbose bool) error {
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
	logEntries, err := readDownloadLog(out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "profile %s: reading download log: %v\n", p.Name, err)
	}
	if sinceLast {
		if last, ok := lastDocumentDate(logEntries, p.Name); ok {
			from = last
		}
		to = time.Now()
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "profile %s: listing documents %s..%s\n", p.Name, from.Format("2006-01-02"), to.Format("2006-01-02"))
	}
	docs, err := c.ListDocumentsDetailed(from, to)
	if err != nil {
		return err
	}
	// Oldest first: -since-last resumes from the newest document date
	// already fetched (see lastDocumentDate), so if a run is interrupted
	// partway through, that frontier must be gapless — a later, newer
	// document downloaded before an older one would let -since-last skip
	// the older one forever on the next run.
	sort.Slice(docs, func(i, j int) bool { return docs[i].Date.Before(docs[j].Date) })
	if verbose {
		fmt.Fprintf(os.Stderr, "profile %s: %d document(s) in range\n", p.Name, len(docs))
	}
	seen := map[string]bool{}
	downloaded, skipped, failedDocs := 0, 0, 0
	for _, d := range docs {
		if !overwrite {
			if _, ok := alreadyLogged(logEntries, p.Name, d); ok {
				if verbose {
					fmt.Fprintf(os.Stderr, "profile %s: skip (logged): %s\n", p.Name, describeDocument(d))
				}
				skipped++
				continue
			}
		}
		if verbose {
			fmt.Fprintf(os.Stderr, "profile %s: downloading: %s\n", p.Name, describeDocument(d))
		}
		resolvePath := documentPathResolver(out, format, p.Name, d)
		path, wasSkipped, err := c.Download(d.WindowFrom, d.WindowTo, d.Index, resolvePath, seen, overwrite)
		switch {
		case errors.Is(err, portal.ErrChallenged):
			fmt.Fprintf(os.Stderr, "profile %s: %s: blocked by bot-check challenge\n", p.Name, describeDocument(d))
			failedDocs++
		case err != nil:
			fmt.Fprintf(os.Stderr, "profile %s: %s: %v\n", p.Name, describeDocument(d), err)
			failedDocs++
		case wasSkipped:
			if verbose {
				fmt.Fprintf(os.Stderr, "profile %s: skip (on disk): %s\n", p.Name, describeDocument(d))
			}
			skipped++
		default:
			fmt.Println(path)
			if err := logDownload(out, p.Name, path, d); err != nil {
				fmt.Fprintf(os.Stderr, "profile %s: %s: log write failed: %v\n", p.Name, describeDocument(d), err)
			}
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
