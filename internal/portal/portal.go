// Package portal implements the flatex.at web portal protocol: HTTP login,
// document-archive listing, and PDF download. Pure net/http — no browser.
package portal

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"math/rand/v2"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const defaultUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

// requestDelay paces portal requests to stay under bot detection.
// ponytail: fixed constant, promote to a flag only if it ever needs tuning.
const requestDelay = 750 * time.Millisecond

type Client struct {
	hc                  *http.Client
	baseURL             string // https://konto.flatex.at; tests point this at httptest
	ua                  string
	delay               time.Duration // requestDelay; tests set 0
	archiveListPath     string        // /banking-<domain>/documentArchiveListFormAction.do
	accountOverviewPath string        // /banking-<domain>/accountOverviewFormAction.do
	headerAreaPath      string        // /banking-<domain>/headerAreaFormAction.do
	ajaxCommandPath     string        // /banking-<domain>/ajaxCommandServlet
	tokenID             string        // server-issued, extracted from response bodies
	windowID            string        // client-generated once per Client, not server-issued
	Log                 func(format string, args ...any) // optional progress hook (see windowedDocuments); nil is silent
}

// logf reports windowedDocuments progress to Log, if set. Silent by default
// since a wide date range can issue many paced sub-window requests with no
// other visible feedback.
func (c *Client) logf(format string, args ...any) {
	if c.Log != nil {
		c.Log(format, args...)
	}
}

// New returns a client for a portal domain like "flatex.at" (default and
// only verified target; "flatex.de" is accepted but untested — only the
// banking-app path segment embeds domain, the host itself is always
// portalHost). An empty userAgent selects the built-in browser default.
func New(domain, userAgent string) (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	if userAgent == "" {
		userAgent = defaultUserAgent
	}
	return &Client{
		hc:                  &http.Client{Jar: jar, Timeout: 60 * time.Second},
		baseURL:             "https://" + portalHost,
		ua:                  userAgent,
		delay:               requestDelay,
		archiveListPath:     "/banking-" + domain + "/" + archiveListAction,
		accountOverviewPath: "/banking-" + domain + "/" + accountOverviewAction,
		headerAreaPath:      "/banking-" + domain + "/" + headerAreaAction,
		ajaxCommandPath:     "/banking-" + domain + "/" + ajaxCommandAction,
		windowID:            newWindowID(),
	}, nil
}

// newWindowID mimics the portal's own client-side window-id generator
// (observed pattern: "W" + a 6-digit number, e.g. "W153023").
func newWindowID() string {
	return fmt.Sprintf("W%d", 100000+rand.IntN(900000))
}

func (c *Client) pace() {
	if c.delay == 0 {
		return
	}
	time.Sleep(c.delay + rand.N(c.delay/3))
}

// do sends a request with pacing and UA applied, and rotates tokenId from
// the response body. ajax controls whether the AJAX-layer headers
// (X-Requested-With, X-AJAX, Accept, X-tokenId/X-windowId) are set —
// without them the portal returns the plain full-page HTML instead of the
// {"commands":[...]} JSON envelope. Skip ajax for requests that bypass the
// portal's own jQuery AJAX layer entirely, like the plain-form login
// submit (see Login). Caller gets the body as string.
func (c *Client) do(req *http.Request, ajax bool) (string, error) {
	c.pace()
	req.Header.Set("User-Agent", c.ua)
	if ajax {
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		req.Header.Set("X-AJAX", "true")
		req.Header.Set("Accept", "application/json")
		if c.tokenID != "" {
			req.Header.Set("X-tokenId", c.tokenID)
		}
		req.Header.Set("X-windowId", c.windowID)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s %s: HTTP %d", req.Method, req.URL.Path, resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	body := string(b)
	c.updateToken(body)
	return body, nil
}

func (c *Client) updateToken(body string) {
	if m := reTokenID.FindStringSubmatch(body); m != nil {
		c.tokenID = m[1]
	}
}

// getAjax issues an AJAX-style GET (X-tokenId/X-windowId echoed).
func (c *Client) getAjax(path string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return "", err
	}
	return c.do(req, true)
}

// postForm issues an AJAX-style form POST (X-tokenId/X-windowId echoed) as
// multipart/form-data — the archive endpoints are submitted via the
// portal's ajaxEngine.submitForm(), which builds a native FormData object
// and sets contentType:false, so the browser sends multipart, not
// application/x-www-form-urlencoded (confirmed from the button onclick
// handlers' own JS: applyFilterButton/btnDocumentDownload both route
// through submitForm, not the plain-object sendServerCommand path).
//
// If the server replies with a "fullPageReplace" command — its way of
// saying this session/page state is stale — postForm follows it exactly
// like a real browser would (a plain GET on fetchLocation, mirroring
// window.location.href = fetchLocation in the portal's own JS; confirmed
// from live capture that this specific request carries no AJAX headers)
// and retries the original submission once.
func (c *Client) postForm(path string, form url.Values) (string, error) {
	body, err := c.postFormOnce(path, form)
	if err != nil {
		return "", err
	}
	if loc, ok := fullPageReplaceLocation(body); ok {
		// fetchLocation's windowId query param is the id the server wants
		// us to use going forward — confirmed via live capture that every
		// fullPageReplace we ignored this on issued a *new, incrementing*
		// windowId each time (W341438, W341439, ...): resyncing under the
		// server-given id but then reverting to our own stale one on the
		// next request looks identical, server-side, to opening yet
		// another brand-new unrecognized window.
		if wid := windowIDFromLocation(loc); wid != "" {
			c.windowID = wid
		}
		if _, err := c.plainGet(loc); err != nil {
			return "", fmt.Errorf("resyncing session: %w", err)
		}
		// The resync GET is itself a full page load; the portal's own JS
		// runs engineStartUp on every full page load ($(document).ready) to
		// (re-)register this window with the server.
		if err := c.engineStartUp(); err != nil {
			return "", fmt.Errorf("resyncing session: %w", err)
		}
		body, err = c.postFormOnce(path, form)
		if err != nil {
			return "", err
		}
	}
	return body, nil
}

func (c *Client) postFormOnce(path string, form url.Values) (string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for key, vals := range form {
		for _, v := range vals {
			if err := mw.WriteField(key, v); err != nil {
				return "", err
			}
		}
	}
	if err := mw.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return c.do(req, true)
}

// plainGet issues a non-AJAX GET — no X-Requested-With/X-AJAX/X-tokenId/
// X-windowId headers — matching a real browser navigation rather than an
// XHR call (confirmed from live capture: the fetchCachedPage resync GET
// carries none of those headers).
func (c *Client) plainGet(path string) (string, error) {
	u := path
	if strings.HasPrefix(u, "/") {
		u = c.baseURL + u
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	return c.do(req, false)
}

// windowIDFromLocation extracts the windowId query parameter from a
// fullPageReplace's fetchLocation, if present.
func windowIDFromLocation(loc string) string {
	u, err := url.Parse(loc)
	if err != nil {
		return ""
	}
	return u.Query().Get("windowId")
}

// postAjaxCommand issues an AJAX-style command POST (X-tokenId/X-windowId
// echoed) as application/x-www-form-urlencoded — the generic
// ajaxCommandServlet dispatcher is driven via the portal's own
// sendServerCommand(), which (unlike the submitForm()-routed archive
// endpoints — see postForm) uses jQuery's default encoding.
func (c *Client) postAjaxCommand(command string, extra url.Values) (string, error) {
	form := url.Values{fieldCommand: {command}}
	for k, v := range extra {
		form[k] = v
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+c.ajaxCommandPath, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.do(req, true)
}

// engineStartUp registers this client's windowId with the server — every
// fresh full-page load runs this first via $(document).ready(startUp) in
// the portal's own JS. Without it, the server never associates our
// (client-generated) windowId with an initialized session, so every
// subsequent archive request looks unrecognized and gets bounced with a
// fresh "fullPageReplace" — confirmed live: even immediately after a
// successful resync GET, skipping this still failed the retry.
func (c *Client) engineStartUp() error {
	dd, err := deviceDataJSON(c.ua)
	if err != nil {
		return err
	}
	extra := url.Values{
		fieldWindowIDPreviouslyUsed: {c.windowID},
		fieldDeviceData:             {dd},
	}
	if _, err := c.postAjaxCommand(cmdEngineStartUp, extra); err != nil {
		return fmt.Errorf("engineStartUp: %w", err)
	}
	return nil
}

// deviceDetails mirrors the JSON blob the real login form (and
// engineStartUp) sends alongside credentials/window registration
// (WebcoreUtils.getDeviceDetails() in the portal's own JS).
type deviceDetails struct {
	WindowWidth    int    `json:"windowWidth"`
	WindowHeight   int    `json:"windowHeight"`
	ScreenWidth    int    `json:"screenWidth"`
	ScreenHeight   int    `json:"screenHeight"`
	UserAgent      string `json:"userAgent"`
	BrowserName    string `json:"browserName"`
	BrowserVersion string `json:"browserVersion"`
	Platform       string `json:"platform"`
	TouchDevice    bool   `json:"touchDevice"`
	PDFSupport     bool   `json:"pdfSupport"`
	Time           int64  `json:"time"`
}

func deviceDataJSON(ua string) (string, error) {
	dd, err := json.Marshal(deviceDetails{
		WindowWidth: 1470, WindowHeight: 801, ScreenWidth: 1470, ScreenHeight: 956,
		UserAgent: ua, BrowserName: "chrome", BrowserVersion: "126.0.0.0",
		Platform: "MacIntel", PDFSupport: true, Time: time.Now().UnixMilli(),
	})
	return string(dd), err
}

// Login POSTs credentials to /login.at/sso. This is a plain HTML form
// submit, not an AJAX call: the real form's onsubmit handler calls
// event.preventDefault() only to defer to the native $form[0].submit(),
// which bypasses jQuery's AJAX layer entirely (confirmed from the login
// form's own HTML/JS) — so this request carries none of the
// X-Requested-With/X-tokenId/X-windowId headers the archive endpoints use.
//
// After a session cookie is granted, the account overview page is loaded
// to confirm the login actually succeeded and to re-seed tokenId for the
// banking-app context.
func (c *Client) Login(username, password string) error {
	if _, err := c.getAjax(pathLoginPage); err != nil {
		return fmt.Errorf("login: loading login page: %w", err)
	}

	dd, err := deviceDataJSON(c.ua)
	if err != nil {
		return err
	}
	form := url.Values{
		fieldUserID:        {username},
		fieldPassword:      {password},
		fieldDeviceDetails: {dd},
		fieldWindowWidth:   {"1470"},
		fieldWindowHeight:  {"956"},
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+pathSSO, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if _, err := c.do(req, false); err != nil {
		return fmt.Errorf("login: %w", err)
	}
	if !c.hasSessionCookie() {
		return errors.New("login: no session cookie granted (wrong credentials?)")
	}

	if _, err := c.getAjax(c.accountOverviewPath); err != nil {
		return fmt.Errorf("login: loading account overview: %w", err)
	}
	if c.tokenID == "" {
		return errors.New("login: could not extract tokenId after login (wrong credentials?)")
	}
	// The account overview page is itself a full page load — register our
	// windowId with the server now (see engineStartUp) so it's already
	// recognized before the first archive request, rather than needing a
	// fullPageReplace round-trip to discover this.
	if err := c.engineStartUp(); err != nil {
		return fmt.Errorf("login: %w", err)
	}
	return nil
}

func (c *Client) hasSessionCookie() bool {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return false
	}
	return len(c.hc.Jar.Cookies(u)) > 0
}

// ensureArchivePage navigates into the document-archive page context via
// the top nav menu — confirmed the real mechanism from live capture
// (menu.items[4].items[0].clicked=true on headerAreaFormAction.do,
// immediately followed in the real session by the archive page's own
// widget scripts loading). Without this, the server rejects an archive
// filter/download POST as an unrecognized page state and replies with a
// "fullPageReplace" command instead of running the filter — confirmed live:
// a bare GET on archiveListPath itself (tried first) did not fix this,
// only the actual menu click does.
func (c *Client) ensureArchivePage() error {
	form := url.Values{
		fieldSearchEditField:            {""},
		fieldMenuDocumentArchiveClicked: {"true"},
	}
	if _, err := c.postForm(c.headerAreaPath, form); err != nil {
		return fmt.Errorf("navigating to document archive: %w", err)
	}
	return nil
}

// filterArchive POSTs the archive filter for [from, to] and returns the raw
// response body.
func (c *Client) filterArchive(from, to time.Time) (string, error) {
	if err := c.ensureArchivePage(); err != nil {
		return "", err
	}
	form := archiveFilterForm(from, to)
	form.Set(fieldApplyFilter, "true")
	body, err := c.postForm(c.archiveListPath, form)
	if err != nil {
		return "", fmt.Errorf("list %s..%s: %w", from.Format("02.01.2006"), to.Format("02.01.2006"), err)
	}
	return body, nil
}

// archiveFilterForm builds the shared filter fields every archive-list
// request needs (confirmed field names/values from live capture).
func archiveFilterForm(from, to time.Time) url.Values {
	return url.Values{
		fieldAccount:         {idxAccountDefault},
		fieldCategory:        {idxCategoryAll},
		fieldReadState:       {idxReadStateAll},
		fieldRetrievalPeriod: {idxRetrievalPeriodCustom},
		fieldDateFrom:        {from.Format("02.01.2006")},
		fieldDateTo:          {to.Format("02.01.2006")},
		fieldStoreSettings:   {"off"},
		fieldSelectAllRows:   {"off"},
	}
}

// ListDocuments returns every archived document's row index in [from, to].
// The real per-row content (names, dates) is in the same response but is
// deliberately not parsed — only the row-index markers, present regardless
// of document content, are used. Known gap: only the first page of results
// is returned (unlike ListDocumentsDetailed, which windows around this —
// see its doc comment — ListDocuments isn't used by fetch/list in
// production, so it hasn't been worth the same treatment).
func (c *Client) ListDocuments(from, to time.Time) ([]int, error) {
	body, err := c.filterArchive(from, to)
	if err != nil {
		return nil, err
	}
	return rowIndices(body), nil
}

// Document is one archived document's metadata, as shown in the portal's
// archive table. Index addresses the row within the exact [WindowFrom,
// WindowTo] filter window it was found in — not necessarily the outer
// range originally requested, since ListDocumentsDetailed splits a wide
// range into several narrower queries (see windowedDocuments). Download
// must be given WindowFrom/WindowTo, not the outer range, or it will
// select the wrong row (or none) against a differently-windowed query.
type Document struct {
	Index      int
	Name       string
	Date       time.Time
	Category   string
	Read       bool
	WindowFrom time.Time
	WindowTo   time.Time
}

// ListDocumentsDetailed returns every archived document's metadata in
// [from, to] — name, date, category, and read status — for a list-only
// view, splitting the range further (see windowedDocuments) whenever a
// query comes back capped or empty so a wide request doesn't silently
// come back truncated.
func (c *Client) ListDocumentsDetailed(from, to time.Time) ([]Document, error) {
	return c.windowedDocuments(from, to)
}

// capLimit is the portal's own cap on documents returned by one query
// (confirmed live: "Es werden nur die ersten 100 Dokumente dargestellt.").
// A response with exactly this many rows is treated as capped regardless
// of whether capWarning's exact text is present — that text was only
// confirmed from an unusual UI state (an empty-dates, custom-period
// query), not a genuine wide explicit date range with real results, and a
// live test showed it does NOT reliably appear when a real capped range
// silently returns exactly 100 rows. The row count itself is the one
// signal that's always measurable, so it's the primary check; capWarning
// is kept as a secondary one in case it does appear in some response
// shape not yet seen.
const capLimit = 100

// windowedDocuments lists [from, to], bisecting into narrower sub-windows
// whenever a query's response signals it couldn't return everything —
// capLimit rows (or capWarning) meaning capped, or a missing tableMarker
// meaning the portal silently declined to render results at all for too
// wide a custom range (confirmed via a live HAR capture: a several-year
// range came back with neither the table nor any warning, just the
// date-picker widgets re-rendering). Each returned Document carries the
// exact sub-window it came from (WindowFrom/WindowTo), since its Index is
// only valid within that window. Always tries the full requested range
// first rather than pre-chunking to a guessed-safe size, so the number of
// requests adapts to what the portal actually needs, not a fixed number.
func (c *Client) windowedDocuments(from, to time.Time) ([]Document, error) {
	if to.Before(from) {
		return nil, nil
	}
	c.logf("querying %s..%s", from.Format("2006-01-02"), to.Format("2006-01-02"))
	body, err := c.filterArchive(from, to)
	if err != nil {
		return nil, err
	}
	days := int(to.Sub(from).Hours() / 24)
	if !strings.Contains(body, tableMarker) {
		if days == 0 {
			// Can't narrow further and still no usable result — return
			// what we have (nothing) rather than fail the whole listing
			// over an extreme edge case.
			c.logf("  %s..%s: no results", from.Format("2006-01-02"), to.Format("2006-01-02"))
			return nil, nil
		}
		c.logf("  %s..%s: no table rendered, splitting", from.Format("2006-01-02"), to.Format("2006-01-02"))
		return c.splitDocuments(from, to, days)
	}
	rowsHTML, err := replacePortionsHTML(body)
	if err != nil {
		return nil, err
	}
	docs := parseDocuments(rowsHTML)
	if days > 0 && (len(docs) >= capLimit || strings.Contains(body, capWarning)) {
		c.logf("  %s..%s: capped at %d, splitting", from.Format("2006-01-02"), to.Format("2006-01-02"), len(docs))
		return c.splitDocuments(from, to, days)
	}
	c.logf("  %s..%s: %d document(s)", from.Format("2006-01-02"), to.Format("2006-01-02"), len(docs))
	for i := range docs {
		docs[i].WindowFrom = from
		docs[i].WindowTo = to
	}
	return docs, nil
}

// splitDocuments bisects [from, to] (days = its span, days >= 1) into two
// non-overlapping halves and concatenates their results. half is at least
// 1 so the split always makes progress: for days == 1 (a 2-day span),
// days/2 truncates to 0, which would otherwise make the "right" half
// identical to the original range and recurse forever.
func (c *Client) splitDocuments(from, to time.Time, days int) ([]Document, error) {
	half := days / 2
	if half < 1 {
		half = 1
	}
	mid := from.AddDate(0, 0, half)
	left, err := c.windowedDocuments(from, mid.AddDate(0, 0, -1))
	if err != nil {
		return nil, err
	}
	right, err := c.windowedDocuments(mid, to)
	if err != nil {
		return nil, err
	}
	return append(left, right...), nil
}

func parseDocuments(rowsHTML string) []Document {
	starts := reDocRow.FindAllStringSubmatchIndex(rowsHTML, -1)
	var docs []Document
	for i, m := range starts {
		rowEnd := len(rowsHTML)
		if i+1 < len(starts) {
			rowEnd = starts[i+1][0]
		}
		row := rowsHTML[m[0]:rowEnd]
		class := rowsHTML[m[2]:m[3]]
		idx, err := strconv.Atoi(rowsHTML[m[4]:m[5]])
		if err != nil {
			continue
		}
		doc := Document{Index: idx, Read: !strings.Contains(class, "Unread")}
		if dm := reDocDate.FindStringSubmatch(row); dm != nil {
			if t, err := time.Parse("02.01.2006", strings.TrimSpace(dm[1])); err == nil {
				doc.Date = t
			}
		}
		if cm := reDocCategory.FindStringSubmatch(row); cm != nil {
			doc.Category = html.UnescapeString(strings.TrimSpace(cm[1]))
		}
		if nm := reDocName.FindStringSubmatch(row); nm != nil {
			doc.Name = html.UnescapeString(strings.TrimSpace(nm[1]))
		}
		docs = append(docs, doc)
	}
	return docs
}

func rowIndices(body string) []int {
	seen := map[int]bool{}
	var idxs []int
	for _, m := range reRowSelection.FindAllStringSubmatch(body, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil || seen[n] {
			continue
		}
		seen[n] = true
		idxs = append(idxs, n)
	}
	sort.Ints(idxs)
	return idxs
}
