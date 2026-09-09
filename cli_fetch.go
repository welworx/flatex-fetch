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

// orDefault labels a flag value as "(default)" when it's empty, so a
// -verbose settings summary shows what's actually in effect even when the
// user never passed the flag — not just what they explicitly set.
func orDefault(val, label string) string {
	if val != "" {
		return val
	}
	return label + " (default)"
}

// rangeDescription renders the resolved [from, to] for a -verbose settings
// summary, labeling which flag combination produced it.
func rangeDescription(days int, from, to time.Time, explicitRange, sinceLast bool) string {
	r := fmt.Sprintf("%s..%s", from.Format("2006-01-02"), to.Format("2006-01-02"))
	switch {
	case sinceLast:
		return "since last fetch per profile, through today (falls back to -days " + fmt.Sprint(days) + " if no log yet)"
	case explicitRange:
		return r + " (explicit -days/-from/-to)"
	default:
		return r + fmt.Sprintf(" (last %d days, default)", days)
	}
}

// resolveProfiles selects the profile(s) named by -profile/-all-profiles
// and decrypts them (including each one's stored password). Callers must
// validate -profile/-all-profiles aren't both set themselves first (see
// profileFlagsValid) since that's a usage error (exit 2), distinct from the
// runtime errors here (exit 1). Shared by fetch and list, which both need
// exactly this before talking to the portal.
//
// If FLATEX_FETCH_USERNAME/PASSWORD are set, credentials.enc is skipped
// entirely (-profile/-all-profiles are ignored) and a single synthetic
// "from-env" profile is used instead, for cron/CI use without a stored
// profile.
func resolveProfiles(profileName string, allProfiles bool) ([]config.Profile, error) {
	if envUser, envPass := os.Getenv("FLATEX_FETCH_USERNAME"), os.Getenv("FLATEX_FETCH_PASSWORD"); envUser != "" && envPass != "" {
		domain := os.Getenv("FLATEX_FETCH_DOMAIN")
		if domain == "" {
			domain = "flatex.at"
		}
		return []config.Profile{{Name: "from-env", Username: envUser, Domain: domain, Password: envPass}}, nil
	}

	dir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	if !config.CredentialsExist(dir) {
		return nil, errors.New("no profiles configured (run: flatex-fetch profile add <name>)")
	}
	pass, err := readPassphrase(false)
	if err != nil {
		return nil, err
	}
	profiles, err := config.LoadSecrets(dir, pass)
	if err != nil {
		return nil, err
	}
	if len(profiles) == 0 {
		return nil, errors.New("no profiles configured (run: flatex-fetch profile add <name>)")
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
			return nil, fmt.Errorf("no profile %q (run: flatex-fetch profile add %s)", profileName, profileName)
		}
	default:
		profiles = []config.Profile{profiles[0]}
	}
	for _, p := range profiles {
		if p.Domain == "" {
			return nil, fmt.Errorf("profile %q has no domain (run: flatex-fetch profile update %s)", p.Name, p.Name)
		}
	}
	return profiles, nil
}

// portalClient is the subset of *portal.Client that fetchProfile drives.
// ponytail: an interface with one production implementation, which normally
// wouldn't earn its keep — it exists solely because fetchProfile is
// otherwise untestable without a live portal account. The fake in
// cli_fetch_profile_test.go is the second implementation.
type portalClient interface {
	Login(username, password string) error
	ListDocumentsDetailed(from, to time.Time) ([]portal.Document, error)
	Download(from, to time.Time, idx int, resolvePath portal.ResolvePath, seen map[string]bool, overwrite bool) (string, bool, error)
}

// newPortalClient builds the real portal session. Tests swap it to inject a
// fake; nothing else reassigns it.
var newPortalClient = func(domain, userAgent string, log func(string, ...any)) (portalClient, error) {
	c, err := portal.New(domain, userAgent)
	if err != nil {
		return nil, err
	}
	c.Log = log
	return c, nil
}

func runFetch(args []string) int {
	fs := flag.NewFlagSet("fetch", flag.ContinueOnError)
	profileName := fs.String("profile", "", "profile to fetch (default: first configured profile)")
	allProfiles := fs.Bool("all-profiles", false, "fetch every configured profile")
	out := fs.String("out", "", "output directory (default ~/flatex-downloads)")
	format := fs.String("format", "", `output path template relative to -out, e.g. "<date YYYY-MM-DD>/<filename>.pdf" (default: <profile>/<filename>, the portal's own name)`)
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

	profiles, err := resolveProfiles(*profileName, *allProfiles)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	if *verbose {
		fmt.Fprintf(os.Stderr, "settings: out=%s format=%s range=%s all=%t since-last=%t user-agent=%s\n",
			*out, orDefault(*format, "<profile>/<filename> (portal's own name)"),
			rangeDescription(*days, from, to, explicitRange, *sinceLast),
			*all, *sinceLast, orDefault(*userAgent, portal.DefaultUserAgent))
	}

	failed := false
	for _, p := range profiles {
		if err := fetchProfile(p, p.Password, *out, *format, *userAgent, from, to, *sinceLast, *all, *verbose); err != nil {
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
// date/name — visible in the portal's own archive table on both UIs — is
// the closest identifying handle available.
func describeDocument(d portal.Document) string {
	return fmt.Sprintf("row %d (%s, %q)", d.Index, d.Date.Format("2006-01-02"), d.Name)
}

// documentPathResolver builds the portal.ResolvePath used for one
// document's download. With no -format, it reproduces the historical
// layout: out/profile/<portal filename>. With -format, the template is
// rendered using the document's profile/date plus its resolved filename
// (extension stripped, since templates supply their own).
func documentPathResolver(out, format, profile string, d portal.Document) portal.ResolvePath {
	return func(origName string) (string, string) {
		if format == "" {
			return filepath.Join(out, profile), origName
		}
		stem := strings.TrimSuffix(origName, filepath.Ext(origName))
		dir, name := renderPathTemplate(format, profile, d.Date, stem)
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
	var log func(string, ...any)
	if verbose {
		log = func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, "profile %s: "+format+"\n", append([]any{p.Name}, args...)...)
		}
	}
	c, err := newPortalClient(p.Domain, userAgent, log)
	if err != nil {
		return err
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
		fmt.Fprintf(os.Stderr, "profile %s (domain %s): listing documents %s..%s\n", p.Name, p.Domain, from.Format("2006-01-02"), to.Format("2006-01-02"))
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
	// A key unique in the saved log may match multiple rows now, for
	// example when another same-day trade arrives after the last fetch.
	listedKeys := make(map[string]int, len(docs))
	for _, d := range docs {
		listedKeys[logKey(p.Name, d.Date.Format("2006-01-02"), d.Name)]++
	}
	seen := map[string]bool{}
	downloaded, skipped, failedDocs := 0, 0, 0
	// logging goes false as soon as any document fails. Documents are sorted
	// oldest-first, so every document after a failure is newer, and logging
	// one would push -since-last's frontier (lastDocumentDate) past the
	// failure — making the next run start after it and skip the failed
	// document forever. Downloads continue; only the log is held back.
	// Boundary: "newer" here only means newer within this run's own sorted
	// slice. lastDocumentDate takes the max Date over the whole log, so a
	// wide explicit -from/-to backfill that fails on an old document can
	// still sit behind a frontier a previous run already pushed further
	// ahead — this guard can't pull that frontier back.
	//
	// ponytail: an unconditional bool rather than tracking the failed date.
	// The cost is that documents after a failure stay unlogged until a clean
	// run, so the next run re-fetches their bytes before skipping them on
	// disk. That self-heals; a silent permanent gap does not.
	logging := true
	for _, d := range docs {
		key := logKey(p.Name, d.Date.Format("2006-01-02"), d.Name)
		if !overwrite && listedKeys[key] == 1 {
			if path, ok := alreadyLogged(logEntries, p.Name, d); ok {
				seen[path] = true
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
			logging = false
		case err != nil:
			fmt.Fprintf(os.Stderr, "profile %s: %s: %v\n", p.Name, describeDocument(d), err)
			failedDocs++
			logging = false
		case wasSkipped:
			if verbose {
				fmt.Fprintf(os.Stderr, "profile %s: skip (on disk): %s\n", p.Name, describeDocument(d))
			}
			// Not in the log (else alreadyLogged would have skipped
			// before ever calling Download) but already on disk, e.g.
			// from a run before logging existed. Backfill the entry now
			// so future runs recognize it without re-downloading — unless
			// this exact path is already there, which happens when several
			// documents share a date/name (logKey is ambiguous, so
			// alreadyLogged can't match) and would otherwise grow a
			// duplicate line on every single run.
			if logging && !logHasPath(logEntries, path) {
				if err := logDownload(out, p.Name, path, d); err != nil {
					fmt.Fprintf(os.Stderr, "profile %s: %s: log write failed: %v\n", p.Name, describeDocument(d), err)
				}
			}
			skipped++
		default:
			fmt.Println(path)
			if logging {
				if err := logDownload(out, p.Name, path, d); err != nil {
					fmt.Fprintf(os.Stderr, "profile %s: %s: log write failed: %v\n", p.Name, describeDocument(d), err)
				}
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
