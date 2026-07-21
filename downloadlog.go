package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/welworx/flatex-fetch/internal/portal"
)

// downloadLogEntry is one line in <out>/.fetch-log.jsonl — an append-only
// record of what fetch has written, independent of the files themselves
// (so it survives moving or pruning downloads).
type downloadLogEntry struct {
	Time     string `json:"time"`
	Profile  string `json:"profile"`
	Index    int    `json:"index"`
	Date     string `json:"date"`
	Category string `json:"category"`
	Name     string `json:"name"`
	Path     string `json:"path"`
}

// logDownload appends one entry to <out>/.fetch-log.jsonl for a document
// fetch just wrote to path.
func logDownload(out, profile, path string, d portal.Document) error {
	f, err := os.OpenFile(filepath.Join(out, ".fetch-log.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(downloadLogEntry{
		Time:     time.Now().Format(time.RFC3339),
		Profile:  profile,
		Index:    d.Index,
		Date:     d.Date.Format("2006-01-02"),
		Category: d.Category,
		Name:     d.Name,
		Path:     path,
	})
}

// logKey identifies a document the same way across runs: d.Index is only
// stable within one [from, to] listing window (see portal.Document), so it
// can't be used here — date/category/name is the closest we have.
func logKey(profile, date, category, name string) string {
	return profile + "\x00" + date + "\x00" + category + "\x00" + name
}

// readDownloadLog reads <out>/.fetch-log.jsonl, if present, grouped by
// logKey. Malformed lines are skipped rather than failing the read — the
// log is a best-effort optimization, not a source of truth fetch depends
// on for correctness.
func readDownloadLog(out string) (map[string][]downloadLogEntry, error) {
	f, err := os.Open(filepath.Join(out, ".fetch-log.jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	entries := map[string][]downloadLogEntry{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e downloadLogEntry
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		key := logKey(e.Profile, e.Date, e.Category, e.Name)
		entries[key] = append(entries[key], e)
	}
	return entries, sc.Err()
}

// lastDocumentDate returns the latest document Date (not the Time it was
// fetched) across all of profile's log entries, for -since-last: resuming
// from the newest document already fetched, not from when fetch last ran,
// so it also works to continue an interrupted historical backfill rather
// than only catching up on genuinely new documents. Entries with an
// unparseable Date are ignored rather than failing the read (see
// readDownloadLog).
func lastDocumentDate(entries map[string][]downloadLogEntry, profile string) (time.Time, bool) {
	var last time.Time
	found := false
	for _, group := range entries {
		for _, e := range group {
			if e.Profile != profile {
				continue
			}
			t, err := time.Parse("2006-01-02", e.Date)
			if err != nil {
				continue
			}
			if !found || t.After(last) {
				last = t
				found = true
			}
		}
	}
	return last, found
}

// alreadyLogged reports the local path of a previously logged download for
// d, but only when exactly one log entry matches (several documents can
// share the same date/category/name — e.g. same-day purchases with
// identical descriptions — and such a key is ambiguous) and that file
// still exists on disk right now. Otherwise it's not safe to skip
// fetching, and the caller should fetch normally.
func alreadyLogged(entries map[string][]downloadLogEntry, profile string, d portal.Document) (string, bool) {
	matches := entries[logKey(profile, d.Date.Format("2006-01-02"), d.Category, d.Name)]
	if len(matches) != 1 {
		return "", false
	}
	if _, err := os.Stat(matches[0].Path); err != nil {
		return "", false
	}
	return matches[0].Path, true
}
