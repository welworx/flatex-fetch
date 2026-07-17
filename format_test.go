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
			name:     "type/date/filename",
			tmpl:     "<type>/<date YYYY-MM-DD>/<original filename>.pdf",
			wantDir:  "Kontoauszug/2026-07-16",
			wantName: "invoice.pdf",
		},
		{
			name:     "profile/year/date-type-filename",
			tmpl:     "<profile>/<date YYYY>/<date>-<type>-<org filename>.pdf",
			wantDir:  "main/2026",
			wantName: "2026-07-16-Kontoauszug-invoice.pdf",
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
			dir, name := renderPathTemplate(c.tmpl, "main", "Kontoauszug", date, "invoice")
			if dir != c.wantDir || name != c.wantName {
				t.Fatalf("renderPathTemplate(%q) = (%q, %q), want (%q, %q)", c.tmpl, dir, name, c.wantDir, c.wantName)
			}
		})
	}
}

func TestValidatePathTemplate(t *testing.T) {
	if err := validatePathTemplate("<type>/<date YYYY>/<filename>.pdf"); err != nil {
		t.Fatalf("valid template rejected: %v", err)
	}
	if err := validatePathTemplate("<type>/<bogus>/<filename>.pdf"); err == nil {
		t.Fatal("expected error for unknown token")
	}
}
