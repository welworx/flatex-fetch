package portal

import (
	"fmt"
	"html"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// This file holds flatex-next's document-archive listing, built on the same
// postForm/fullPageReplace/engineStartUp primitives as the old UI (see
// portal.go, markup.go). Confirmed from a single live capture (2026-07-26,
// one account, one ~1-month custom date range, ~200 total documents seen).
// Unlike the old UI's flat table, flatex-next groups entries under
// per-date headers and paginates via an incrementing "scrollposition"
// field rather than date-windowing — see nextListDocuments. The precise
// pagination step size and its termination behavior beyond what one
// capture showed are NOT live-verified; ListDocumentsDetailed's caller
// should treat a large flatex-next result set with extra suspicion until
// this has been exercised against a real account with more documents than
// fit in a couple of scroll steps.

// nextOpenArchive opens the document-archive dialog — confirmed live:
// required before any archive-scoped field (date range, children[N]
// clicks) is recognized, the same role ensureArchivePage plays for the old
// UI.
func (c *Client) nextOpenArchive() error {
	form := url.Values{
		fieldNextOverviewIdx: {idxNextOverviewDefault},
		fieldNextOpenArchive: {"true"},
	}
	_, err := c.postForm(c.archiveListPath, form)
	return err
}

// nextSetCustomDateRange switches the archive dialog to an explicit
// [from, to] range and returns the resulting filtered listing. Confirmed a
// two-step interaction live: first opening the custom-range sub-dialog
// (clicking the date-range widget's "individuell" entry, children[7]),
// then applying explicit dates via that sub-dialog's own nested form — the
// apply response is what actually carries the filtered document list.
func (c *Client) nextSetCustomDateRange(from, to time.Time) (string, error) {
	open := url.Values{
		fieldNextReadStateIdx:    {idxNextReadStateAll},
		fieldNextDateRangeIdx:    {idxNextDateRangeDefault},
		fieldNextDateRangeCustom: {"true"},
	}
	if _, err := c.postForm(c.archiveListPath, open); err != nil {
		return "", fmt.Errorf("opening custom date range: %w", err)
	}
	apply := url.Values{
		fieldNextDateStart: {from.Format("02.01.2006")},
		fieldNextDateEnd:   {to.Format("02.01.2006")},
		fieldNextDateApply: {"true"},
	}
	return c.postForm(c.archiveListPath, apply)
}

// nextScrollStep is how much fieldNextScrollPos advances per pagination
// request. Confirmed live to reliably pull in another batch of entries
// (~50) regardless of its exact value — the portal appears to translate it
// into "how many entries should be visible," not a pixel offset it
// validates precisely. Unverified: whether a smaller true page size exists
// that this capture's date range/account never happened to hit.
const nextScrollStep = 5000

// nextListDocuments lists [from, to] for a flatex-next session: open the
// archive, apply the date range, then keep requesting with an increasing
// scrollposition until a request stops returning more entries than the
// last one — the one directly-measurable "there's nothing further" signal
// available (see nextScrollStep), the same philosophy as the old UI's
// capLimit-based windowing.
func (c *Client) nextListDocuments(from, to time.Time) ([]Document, error) {
	c.logf("querying %s..%s (flatex-next)", from.Format("2006-01-02"), to.Format("2006-01-02"))
	if err := c.nextOpenArchive(); err != nil {
		return nil, fmt.Errorf("opening document archive: %w", err)
	}
	body, err := c.nextSetCustomDateRange(from, to)
	if err != nil {
		return nil, fmt.Errorf("list %s..%s: %w", from.Format("02.01.2006"), to.Format("02.01.2006"), err)
	}
	docs, err := parseNextDocuments(body)
	if err != nil {
		return nil, fmt.Errorf("list %s..%s: %w", from.Format("02.01.2006"), to.Format("02.01.2006"), err)
	}

	for scrollPos := nextScrollStep; ; scrollPos += nextScrollStep {
		form := url.Values{
			fieldNextScrollPos:    {strconv.Itoa(scrollPos)},
			fieldNextReadStateIdx: {idxNextReadStateAll},
			fieldNextDateRangeIdx: {idxNextDateRangeCustom},
			fieldNextReload:       {"true"},
		}
		body, err := c.postForm(c.archiveListPath, form)
		if err != nil {
			return nil, fmt.Errorf("list %s..%s: %w", from.Format("02.01.2006"), to.Format("02.01.2006"), err)
		}
		next, err := parseNextDocuments(body)
		if err != nil {
			return nil, fmt.Errorf("list %s..%s: %w", from.Format("02.01.2006"), to.Format("02.01.2006"), err)
		}
		if len(next) <= len(docs) {
			break
		}
		docs = next
	}

	for i := range docs {
		docs[i].WindowFrom = from
		docs[i].WindowTo = to
	}
	c.logf("  %s..%s: %d document(s)", from.Format("2006-01-02"), to.Format("2006-01-02"), len(docs))
	return docs, nil
}

func parseNextDocuments(body string) ([]Document, error) {
	rowsHTML, err := replacePortionsHTML(body)
	if err != nil {
		return nil, err
	}
	return parseNextEntries(rowsHTML), nil
}

// nextDownload replays [from, to]'s listing (see nextListDocuments) until
// document idx is present, then clicks its btnOpenDocument to trigger
// flatex-next's document viewer, returning the resulting download
// location. idx must come from a Document this same [from, to] range
// produced — matching the old UI's Download contract (see its doc
// comment).
func (c *Client) nextDownload(from, to time.Time, idx int) (string, error) {
	if err := c.nextOpenArchive(); err != nil {
		return "", err
	}
	body, err := c.nextSetCustomDateRange(from, to)
	if err != nil {
		return "", err
	}
	docs, err := parseNextDocuments(body)
	if err != nil {
		return "", err
	}
	for scrollPos := nextScrollStep; !hasDocIndex(docs, idx); scrollPos += nextScrollStep {
		form := url.Values{
			fieldNextScrollPos:    {strconv.Itoa(scrollPos)},
			fieldNextReadStateIdx: {idxNextReadStateAll},
			fieldNextDateRangeIdx: {idxNextDateRangeCustom},
			fieldNextReload:       {"true"},
		}
		body, err = c.postForm(c.archiveListPath, form)
		if err != nil {
			return "", err
		}
		next, err := parseNextDocuments(body)
		if err != nil {
			return "", err
		}
		if len(next) <= len(docs) {
			break // no more growth; idx isn't reachable in this range
		}
		docs = next
	}

	click := url.Values{
		fieldNextReadStateIdx: {idxNextReadStateAll},
		fieldNextDateRangeIdx: {idxNextDateRangeCustom},
	}
	click.Set(fmt.Sprintf(fieldNextOpenDocFmt, idx), "true")
	body, err = c.postForm(c.archiveListPath, click)
	if err != nil {
		return "", err
	}
	return downloadLocationNext(body)
}

func hasDocIndex(docs []Document, idx int) bool {
	for _, d := range docs {
		if d.Index == idx {
			return true
		}
	}
	return false
}

// parseNextEntries walks reNextAnyMarker's matches in document order,
// tracking the most recent date-group header and attaching it to every
// entry until the next one — flatex-next groups entries under per-date
// headers rather than giving each row its own date column (unlike the old
// UI's flat table, see reDocDate). Category isn't parsed: no per-entry
// category text was found in the captured markup, only a separate
// category *filter* widget, so Document.Category is left empty for
// flatex-next results.
func parseNextEntries(rowsHTML string) []Document {
	matches := reNextAnyMarker.FindAllStringSubmatchIndex(rowsHTML, -1)
	var docs []Document
	var currentDate time.Time
	for i, m := range matches {
		if m[2] >= 0 { // date-header alternative matched
			if t, err := time.Parse("02.01.2006", rowsHTML[m[2]:m[3]]); err == nil {
				currentDate = t
			}
			continue
		}
		class := rowsHTML[m[4]:m[5]]
		idx, err := strconv.Atoi(rowsHTML[m[6]:m[7]])
		if err != nil {
			continue
		}
		end := len(rowsHTML)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		slice := rowsHTML[m[1]:min(end, m[1]+2000)]
		doc := Document{Index: idx, Date: currentDate, Read: !strings.Contains(class, "Unread")}
		if dm := reNextDescription.FindStringSubmatch(slice); dm != nil {
			doc.Name = html.UnescapeString(strings.TrimSpace(dm[1]))
		}
		docs = append(docs, doc)
	}
	return docs
}
