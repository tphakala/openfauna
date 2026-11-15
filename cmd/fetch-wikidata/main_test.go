package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tphakala/openfauna/internal/dataset"
)

// TestReportWrite_SortedKeys pins the report's key order: scripts/validate.py
// requires sorted keys, and the order comes from the struct field order.
func TestReportWrite_SortedKeys(t *testing.T) {
	r := newReport()
	r.Agree = 3
	r.MediaAdded = 2
	r.MediaSkipped = 1
	r.record(dataset.QIDDisagree, "Genus a", "Title", "QIDTitle", "QIDName")
	r.record(dataset.QIDUnresolved, "Genus b", "Title b", "", "")
	path := filepath.Join(t.TempDir(), "report.json")
	if err := r.write(path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "agree": 3,
  "disagree": [
    {
      "name": "Genus a",
      "qid_name": "QIDName",
      "qid_title": "QIDTitle",
      "title": "Title"
    }
  ],
  "media_added": 2,
  "media_skipped": 1,
  "unresolved": [
    {
      "name": "Genus b",
      "title": "Title b"
    }
  ]
}
`
	if string(got) != want {
		t.Errorf("report =\n%s\nwant\n%s", got, want)
	}
}
