package portal

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var errDocumentOnly = errors.New("blocked request outside document-only policy")

// documentTransport is the final outbound boundary, including http.Client's
// redirects and direct download calls. Unknown operations fail closed. There
// is deliberately no command-line switch for disabling this policy.
type documentTransport struct {
	client                  *Client
	next                    http.RoundTripper
	login, banking, desktop string
}

func (t *documentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.client.checkDestination(req.URL); err != nil {
		return nil, err
	}
	if err := t.check(req); err != nil {
		return nil, fmt.Errorf("%s %q: %w", req.Method, req.URL.Path, err)
	}
	return t.next.RoundTrip(req)
}

func (t *documentTransport) check(req *http.Request) error {
	u := req.URL
	// Reject ambiguous path encodings rather than trusting different server
	// normalization rules. All known operation paths and download IDs are ASCII.
	if u.RawPath != "" || u.Fragment != "" || strings.ContainsAny(u.Path, "%\\;\x00") || strings.Contains(u.Path, "//") {
		return errDocumentOnly
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == "." || part == ".." {
			return errDocumentOnly
		}
	}
	if req.Host != "" && req.Host != u.Host {
		return errDocumentOnly
	}
	if len(req.Trailer) != 0 || len(req.TransferEncoding) != 0 {
		return errDocumentOnly
	}
	for name, values := range req.Header {
		if name != http.CanonicalHeaderKey(name) || len(values) != 1 {
			return errDocumentOnly
		}
		switch name {
		case "User-Agent", "X-Requested-With", "X-Ajax", "Accept", "X-Tokenid", "X-Windowid", "Cookie", "Content-Type", "Referer":
		default:
			return errDocumentOnly
		}
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || !singleValues(query) {
		return errDocumentOnly
	}
	if req.Method == http.MethodGet {
		if req.ContentLength != 0 || (req.Body != nil && req.Body != http.NoBody) {
			return errDocumentOnly
		}
		if t.allowedGet(u.Path, query) {
			return nil
		}
		return errDocumentOnly
	}
	if req.Method != http.MethodPost {
		return errDocumentOnly
	}
	form, err := requestFields(req)
	if err != nil || !singleValues(form) {
		return errDocumentOnly
	}
	// If a German classic login redirect preserves the SSO POST, the
	// query token and the original login body must each match their own
	// exact schema; never merge them into one command parameter set.
	if t.banking == "/banking-flatex/" && u.Path == t.banking+loginCommandAction {
		if fieldsMatch(query, map[string]string{"loginData": "@token"}) && t.allowedPost(t.login+ssoAction, form) {
			return nil
		}
		return errDocumentOnly
	}
	if len(query) == 0 && t.allowedPost(u.Path, form) {
		return nil
	}
	return errDocumentOnly
}

func (t *documentTransport) allowedGet(p string, q url.Values) bool {
	if p == t.login+loginPageAction {
		return len(q) == 0
	}
	if p == t.desktop+loginCommandAction || (t.banking == "/banking-flatex/" && p == t.banking+loginCommandAction) {
		return fieldsMatch(q, map[string]string{"loginData": "@token"})
	}
	// German classic also lands on this page after loginCommand (observed
	// 2026-09-09). It is only a bodyless GET, never a login/action form POST.
	if t.banking == "/banking-flatex/" && p == t.banking+loginProgressAction {
		return len(q) == 0
	}
	if p == t.banking+accountOverviewAction || p == t.banking+archiveListAction || p == t.desktop+nextArchiveAction || p == t.desktop+loginProgressAction {
		return len(q) == 0
	}
	for _, prefix := range []string{t.banking, t.desktop} {
		if p == prefix+fetchCachedPageAction {
			return fieldsMatch(q, map[string]string{"windowId": "@window"})
		}
		if strings.HasPrefix(p, prefix+"downloadData/") {
			return len(q) == 0 && safeDownloadPath.MatchString(strings.TrimPrefix(p, prefix+"downloadData/"))
		}
	}
	return false
}

func (t *documentTransport) allowedPost(p string, f url.Values) bool {
	if p == t.login+ssoAction {
		return fieldsMatch(f, map[string]string{fieldUserID: "@text", fieldPassword: "@text", fieldDeviceDetails: "@device", fieldWindowWidth: "1470", fieldWindowHeight: "956"})
	}
	if p == t.banking+ajaxCommandAction || p == t.desktop+ajaxCommandAction {
		if fieldsMatch(f, map[string]string{fieldCommand: cmdEngineStartUp, fieldWindowIDPreviouslyUsed: "@window", fieldDeviceData: "@device"}) {
			return true
		}
		return p == t.desktop+ajaxCommandAction && fieldsMatch(f, map[string]string{fieldCommand: cmdResumeLogin})
	}
	if p == t.banking+headerAreaAction {
		return fieldsMatch(f, map[string]string{fieldSearchEditField: "", fieldMenuDocumentArchiveClicked: "true"})
	}
	if p == t.banking+archiveListAction {
		rules := map[string]string{fieldAccount: idxAccountDefault, fieldCategory: idxCategoryAll, fieldReadState: idxReadStateAll, fieldRetrievalPeriod: idxRetrievalPeriodCustom, fieldDateFrom: "@date", fieldDateTo: "@date", fieldStoreSettings: "off", fieldSelectAllRows: "off"}
		if f.Get(fieldApplyFilter) == "true" {
			rules[fieldApplyFilter] = "true"
		} else {
			rules[fieldDownloadClicked] = "true"
			count := 0
			for key := range f {
				if rowField.MatchString(key) {
					rules[key] = "on"
					count++
				}
			}
			if count != 1 {
				return false
			}
		}
		return fieldsMatch(f, rules)
	}
	if p != t.desktop+nextArchiveAction {
		return false
	}
	if fieldsMatch(f, map[string]string{fieldNextOverviewIdx: idxNextOverviewDefault, fieldNextOpenArchive: "true"}) ||
		fieldsMatch(f, map[string]string{fieldNextReadStateIdx: idxNextReadStateAll, fieldNextDateRangeIdx: idxNextDateRangeDefault, fieldNextDateRangeCustom: "true"}) ||
		fieldsMatch(f, map[string]string{fieldNextDateStart: "@date", fieldNextDateEnd: "@date", fieldNextDateApply: "true"}) ||
		fieldsMatch(f, map[string]string{fieldNextScrollPos: "@integer", fieldNextReadStateIdx: idxNextReadStateAll, fieldNextDateRangeIdx: idxNextDateRangeCustom, fieldNextReload: "true"}) {
		return true
	}
	for key := range f {
		if nextDocField.MatchString(key) {
			return fieldsMatch(f, map[string]string{fieldNextReadStateIdx: idxNextReadStateAll, fieldNextDateRangeIdx: idxNextDateRangeCustom, key: "true"})
		}
	}
	return false
}

func singleValues(f url.Values) bool {
	for _, values := range f {
		if len(values) != 1 {
			return false
		}
	}
	return true
}

func fieldsMatch(f url.Values, rules map[string]string) bool {
	if len(f) != len(rules) {
		return false
	}
	for key, rule := range rules {
		values, ok := f[key]
		if !ok || len(values) != 1 {
			return false
		}
		v := values[0]
		switch rule {
		case "@text":
			if v == "" {
				return false
			}
		case "@token":
			if v == "" || strings.ContainsAny(v, "\r\n\x00") {
				return false
			}
		case "@window":
			if !windowValue.MatchString(v) {
				return false
			}
		case "@integer":
			if !integerValue.MatchString(v) {
				return false
			}
		case "@date":
			parsed, err := time.Parse("02.01.2006", v)
			if err != nil || parsed.Format("02.01.2006") != v {
				return false
			}
		case "@device":
			var device deviceDetails
			if json.Unmarshal([]byte(v), &device) != nil {
				return false
			}
			// Only this client's canonical device schema, no extra JSON commands or
			// duplicate fields. User-agent text stays a JSON string.
			canonical, err := json.Marshal(device)
			if err != nil || string(canonical) != v {
				return false
			}
		default:
			if v != rule {
				return false
			}
		}
	}
	return true
}

// Parse a bounded copy, then restore the exact bytes for the real transport.
// Do not use Request.ParseForm: silently ignored parse errors, file parts or
// merged query/body values would undermine the allowlist.
func requestFields(req *http.Request) (url.Values, error) {
	if req.Body == nil {
		return nil, errDocumentOnly
	}
	const limit = 64 * 1024
	body, err := io.ReadAll(io.LimitReader(req.Body, limit+1))
	_ = req.Body.Close()
	if err != nil || len(body) > limit || req.ContentLength != int64(len(body)) {
		return nil, errDocumentOnly
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	contentType, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if err != nil {
		return nil, errDocumentOnly
	}
	if contentType == "application/x-www-form-urlencoded" {
		if len(params) != 0 {
			return nil, errDocumentOnly
		}
		return url.ParseQuery(string(body))
	}
	if contentType != "multipart/form-data" || len(params) != 1 || params["boundary"] == "" {
		return nil, errDocumentOnly
	}
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	form := make(url.Values)
	for {
		part, err := reader.NextRawPart()
		if err == io.EOF {
			return form, nil
		}
		if err != nil {
			return nil, errDocumentOnly
		}
		disposition, attrs, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		if err != nil || disposition != "form-data" || len(attrs) != 1 || attrs["name"] == "" || len(part.Header) != 1 || len(part.Header.Values("Content-Disposition")) != 1 || part.Header.Get("Content-Disposition") != fmt.Sprintf("form-data; name=%q", attrs["name"]) {
			return nil, errDocumentOnly
		}
		value, err := io.ReadAll(part)
		if err != nil {
			return nil, errDocumentOnly
		}
		form.Add(attrs["name"], string(value))
	}
}
