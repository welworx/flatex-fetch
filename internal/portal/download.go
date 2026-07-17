package portal

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// ErrChallenged marks a download blocked by the portal's bot check
// (Myra Cloud interstitial) rather than an ordinary HTTP failure.
var ErrChallenged = errors.New("portal bot-check challenge (myracloud)")

// ajaxResponse is the JSON envelope the portal's archive endpoints return.
type ajaxResponse struct {
	Commands []ajaxCommand `json:"commands"`
}

type ajaxCommand struct {
	Command       string `json:"command"`
	Location      string `json:"location"`
	FetchLocation string `json:"fetchLocation"` // present on "fullPageReplace" commands
}

// fullPageReplaceLocation reports whether body is a "fullPageReplace"
// command (the server's signal that this session/page state is stale) and,
// if so, its fetchLocation.
func fullPageReplaceLocation(body string) (string, bool) {
	var resp ajaxResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return "", false
	}
	for _, cmd := range resp.Commands {
		if cmd.Command == "fullPageReplace" && cmd.FetchLocation != "" {
			return cmd.FetchLocation, true
		}
	}
	return "", false
}

// downloadLocation extracts the "download" command's location field from
// an archive-filter response — confirmed shape from live capture:
// {"commands":[...,{"command":"download","location":"/banking-.../downloadData/..."},...]}.
func downloadLocation(body string) (string, error) {
	var resp ajaxResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return "", fmt.Errorf("parsing archive response: %w", err)
	}
	for _, cmd := range resp.Commands {
		if cmd.Command == "download" && cmd.Location != "" {
			return cmd.Location, nil
		}
	}
	return "", errors.New("no download in response (document not found or already removed?)")
}

// Download selects row idx within [from, to]'s filtered window, triggers
// the portal's download response, and writes the resulting file to
// destDir using its resolved filename. Confirmed live against a real
// account: a lone selected row comes back as a single PDF with its real
// portal filename; 2+ rows come back as a zip bundle, which Download
// unzips.
//
// seen tracks names written earlier in this run (within-run collisions get
// _2/_3 suffixes). A file that already exists across runs is skipped
// unless overwrite is set — that skip IS the dedup (see design spec).
func (c *Client) Download(from, to time.Time, idx int, destDir string, seen map[string]bool, overwrite bool) (string, bool, error) {
	if err := c.ensureArchivePage(); err != nil {
		return "", false, err
	}
	form := archiveFilterForm(from, to)
	form.Set(fieldDownloadClicked, "true")
	form.Set(fmt.Sprintf("%s%d].checked", rowSelectionPrefix, idx), "on")

	body, err := c.postForm(c.archiveListPath, form)
	if err != nil {
		return "", false, fmt.Errorf("download row %d: %w", idx, err)
	}
	loc, err := downloadLocation(body)
	if err != nil {
		return "", false, fmt.Errorf("download row %d: %w", idx, err)
	}
	return c.fetchLocation(loc, destDir, seen, overwrite)
}

// fetchLocation GETs a download location returned by the archive endpoint
// and writes the resulting document(s) to destDir — a bare PDF or a zip
// bundle, depending on how many documents the portal packaged.
func (c *Client) fetchLocation(loc, destDir string, seen map[string]bool, overwrite bool) (string, bool, error) {
	u := loc
	if strings.HasPrefix(u, "/") {
		u = c.baseURL + u
	}
	c.pace()
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("User-Agent", c.ua)
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", false, err
	}
	head := body
	if len(head) > 512 {
		head = head[:512]
	}
	if isChallenge(resp, head) {
		return "", false, fmt.Errorf("%s: %w", loc, ErrChallenged)
	}
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("%s: HTTP %d", loc, resp.StatusCode)
	}

	if strings.HasPrefix(string(head), "PK") {
		return writeZipEntry(body, destDir, seen, overwrite, loc)
	}
	if !strings.HasPrefix(string(head), "%PDF") {
		return "", false, fmt.Errorf("%s: response is not a PDF or zip (content-type %s)", loc, resp.Header.Get("Content-Type"))
	}
	name := resolveFilename(resp, u)
	return writeFile(name, body, destDir, seen, overwrite)
}

func isChallenge(resp *http.Response, head []byte) bool {
	return resp.StatusCode == http.StatusServiceUnavailable ||
		strings.Contains(strings.ToLower(string(head)), "myracloud")
}

// writeZipEntry unpacks a single-document zip bundle. More than one entry
// is unexpected for a one-row download and is treated as an error rather
// than silently dropping data.
func writeZipEntry(body []byte, destDir string, seen map[string]bool, overwrite bool, loc string) (string, bool, error) {
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return "", false, fmt.Errorf("%s: reading zip: %w", loc, err)
	}
	if len(zr.File) != 1 {
		return "", false, fmt.Errorf("%s: expected 1 document in zip, got %d", loc, len(zr.File))
	}
	f := zr.File[0]
	if strings.Contains(f.Name, "..") {
		return "", false, fmt.Errorf("%s: unsafe zip entry name %q", loc, f.Name)
	}
	rc, err := f.Open()
	if err != nil {
		return "", false, fmt.Errorf("%s: opening zip entry: %w", loc, err)
	}
	defer rc.Close()
	content, err := io.ReadAll(rc)
	if err != nil {
		return "", false, fmt.Errorf("%s: reading zip entry: %w", loc, err)
	}
	name := sanitize(path.Base(f.Name))
	if name == "" {
		name = "doc-" + hashStem(loc) + ".pdf"
	}
	return writeFile(name, content, destDir, seen, overwrite)
}

// resolveFilename picks a filename by priority: Content-Disposition header,
// filename/id in the URL query, URL path tail, hash-derived stem.
func resolveFilename(resp *http.Response, rawURL string) string {
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if fn := sanitize(params["filename"]); fn != "" {
				return fn
			}
		}
	}
	if u, err := url.Parse(rawURL); err == nil {
		for _, key := range []string{"filename", "file", "id"} {
			if v := sanitize(u.Query().Get(key)); v != "" {
				if !strings.HasSuffix(strings.ToLower(v), ".pdf") {
					v += ".pdf"
				}
				return v
			}
		}
		if tail := sanitize(path.Base(u.Path)); tail != "" && tail != "/" && tail != "." &&
			strings.HasSuffix(strings.ToLower(tail), ".pdf") {
			return tail
		}
	}
	return "doc-" + hashStem(rawURL) + ".pdf"
}

func hashStem(rawURL string) string {
	sum := sha256.Sum256([]byte(rawURL))
	return hex.EncodeToString(sum[:6])
}

// sanitize strips path separators and rejects "." / ".." so a resolved name
// can never point outside destDir when joined with filepath.Join.
func sanitize(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	if name == "." || name == ".." {
		return ""
	}
	return name
}

func suffixed(name string, i int) string {
	ext := filepath.Ext(name)
	return fmt.Sprintf("%s_%d%s", strings.TrimSuffix(name, ext), i, ext)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// writeFile resolves within-run/cross-run dedup for name and, if not
// skipped, writes content to destDir 0700/0600 — same posture as the
// credentials store, since these are financial documents.
func writeFile(name string, content []byte, destDir string, seen map[string]bool, overwrite bool) (string, bool, error) {
	exists := fileExists(filepath.Join(destDir, name))
	switch {
	case seen[name]:
		for i := 2; ; i++ {
			cand := suffixed(name, i)
			if !seen[cand] && !fileExists(filepath.Join(destDir, cand)) {
				name = cand
				break
			}
		}
	case exists && !overwrite:
		return filepath.Join(destDir, name), true, nil
	}

	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return "", false, err
	}
	dest := filepath.Join(destDir, name)
	if err := os.WriteFile(dest, content, 0o600); err != nil {
		os.Remove(dest)
		return "", false, err
	}
	seen[name] = true
	return dest, false, nil
}
