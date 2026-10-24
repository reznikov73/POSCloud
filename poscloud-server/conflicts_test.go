package main

import "testing"

func TestConflictOriginalName(t *testing.T) {
	cases := map[string]string{
		"docs/report (конфликт 2026-10-24_12-00-00).docx": "docs/report.docx",
		"note.txt (конфликт 2026-10-24_12-00-00)":         "note.txt",
		"plain.txt": "",
	}
	for in, want := range cases {
		if got := conflictOriginalName(in); got != want {
			t.Fatalf("conflictOriginalName(%q)=%q, ожидалось %q", in, got, want)
		}
	}
}
