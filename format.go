package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var templateTokenRe = regexp.MustCompile(`<([^>]+)>`)

// validTemplateToken matches the token names renderPathTemplate knows how to
// substitute. "date" alone or "date <layout>" (layout using YYYY/MM/DD
// placeholders) are both accepted.
var validTemplateToken = regexp.MustCompile(`^(profile|filename|original filename|org filename|date|date .+)$`)

// validatePathTemplate rejects unknown tokens and paths outside -out
// before logging in. Token values cannot introduce directory separators.
func validatePathTemplate(tmpl string) error {
	for _, m := range templateTokenRe.FindAllStringSubmatch(tmpl, -1) {
		if !validTemplateToken.MatchString(m[1]) {
			return fmt.Errorf("unknown -format token <%s>", m[1])
		}
	}
	// Validate the rendered shape too: date layouts may themselves introduce
	// directories, while profile/filename substitutions are single components.
	rendered := templateTokenRe.ReplaceAllStringFunc(tmpl, func(tok string) string {
		return renderToken(tok[1:len(tok)-1], "profile", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), "filename")
	})
	if !localOutputPath(rendered) {
		return fmt.Errorf("-format must be a relative path without parent-directory components")
	}
	return nil
}

// localOutputPath rejects traversal and portable absolute-path spellings.
func localOutputPath(s string) bool {
	if !filepath.IsLocal(s) || strings.ContainsAny(s, "\\:\x00") {
		return false
	}
	for _, component := range strings.Split(s, "/") {
		if component == ".." {
			return false
		}
	}
	return true
}

func validateProfileName(name string) error {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(name) != name || name == "." || !localOutputPath(name) || strings.Contains(name, "/") {
		return fmt.Errorf("profile name %q must be a single directory name (not . or ..), without surrounding spaces", name)
	}
	return nil
}

// renderPathTemplate substitutes tmpl's <token> placeholders using profile,
// date, and filenameStem (the resolved document filename with its
// extension stripped), then splits the result into a directory and base
// filename. Recognized tokens: <profile>, <filename> (aliases
// <original filename>, <org filename>), and <date> or <date LAYOUT> where
// LAYOUT uses YYYY/MM/DD placeholders (default YYYY-MM-DD).
func renderPathTemplate(tmpl, profile string, date time.Time, filenameStem string) (dir, name string) {
	rendered := templateTokenRe.ReplaceAllStringFunc(tmpl, func(tok string) string {
		return renderToken(tok[1:len(tok)-1], profile, date, filenameStem)
	})
	rendered = filepath.FromSlash(rendered)
	return filepath.Dir(rendered), filepath.Base(rendered)
}

func renderToken(token, profile string, date time.Time, filenameStem string) string {
	switch {
	case token == "profile":
		return pathSafe(profile)
	case token == "filename" || token == "original filename" || token == "org filename":
		return pathSafe(filenameStem)
	case token == "date" || strings.HasPrefix(token, "date "):
		layout := strings.TrimSpace(strings.TrimPrefix(token, "date"))
		if layout == "" {
			layout = "YYYY-MM-DD"
		}
		return formatDate(date, layout)
	default:
		return "<" + token + ">"
	}
}

func formatDate(t time.Time, layout string) string {
	r := strings.NewReplacer(
		"YYYY", fmt.Sprintf("%04d", t.Year()),
		"MM", fmt.Sprintf("%02d", t.Month()),
		"DD", fmt.Sprintf("%02d", t.Day()),
	)
	return r.Replace(layout)
}

// pathSafe strips path separators from a substituted token value so it
// can't escape the directory it's placed in.
func pathSafe(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	s = strings.ReplaceAll(s, ":", "_")
	if s == "" || s == "." || s == ".." {
		s = "unknown"
	}
	return s
}
