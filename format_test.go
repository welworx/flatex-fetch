package main

import (
	"testing"
	"time"
)

func TestRenderPathTemplate(t *testing.T) {
	date := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name     string
		tmpl     string
		wantDir  string
		wantName string
	}{
		{
			name:     "date/filename",
			tmpl:     "<date YYYY-MM-DD>/<original filename>.pdf",
			wantDir:  "2026-07-16",
			wantName: "invoice.pdf",
		},
		{
			name:     "profile/year/date-filename",
			tmpl:     "<profile>/<date YYYY>/<date>-<org filename>.pdf",
			wantDir:  "main/2026",
			wantName: "2026-07-16-invoice.pdf",
		},
		{
			name:     "default date layout",
			tmpl:     "<date>/<filename>.pdf",
			wantDir:  "2026-07-16",
			wantName: "invoice.pdf",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir, name := renderPathTemplate(c.tmpl, "main", date, "invoice")
			if dir != c.wantDir || name != c.wantName {
				t.Fatalf("renderPathTemplate(%q) = (%q, %q), want (%q, %q)", c.tmpl, dir, name, c.wantDir, c.wantName)
			}
		})
	}
}

func TestValidatePathTemplate(t *testing.T) {
	if err := validatePathTemplate("<profile>/<date YYYY>/<filename>.pdf"); err != nil {
		t.Fatalf("valid template rejected: %v", err)
	}
	if err := validatePathTemplate("<profile>/<bogus>/<filename>.pdf"); err == nil {
		t.Fatal("expected error for unknown token")
	}
}
