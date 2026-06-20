package dataset

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMedia(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "media.json")
	content := `{
  "Aquila chrysaetos": [
    {"source": "wikimedia", "id": "Golden eagle.jpg", "format": "image/jpeg",
     "width": 1600, "height": 900, "aspect_ratio": 1.78, "orientation": "landscape",
     "license": "https://creativecommons.org/licenses/by-sa/4.0/", "creator": "Jane Doe", "rightsHolder": "Jane Doe"}
  ]
}
`
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadMedia(p)
	if err != nil {
		t.Fatalf("LoadMedia: %v", err)
	}
	items := got["Aquila chrysaetos"]
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	if items[0].Source != "wikimedia" || items[0].ID != "Golden eagle.jpg" {
		t.Fatalf("unexpected item: %+v", items[0])
	}
}

func TestValidateMedia(t *testing.T) {
	srcs := map[string]MediaSource{"wikimedia": {Name: "Wikimedia Commons", Thumb: "https://x/{id}"}}
	ok := MediaItem{Source: "wikimedia", ID: "f.jpg", License: "https://creativecommons.org/licenses/by-sa/4.0/"}

	if err := ValidateMedia(map[string][]MediaItem{"A": {ok}}, srcs); err != nil {
		t.Fatalf("valid media rejected: %v", err)
	}

	cases := map[string]MediaItem{
		"unknown source":   {Source: "nope", ID: "f.jpg", License: "https://creativecommons.org/x"},
		"empty id":         {Source: "wikimedia", ID: "", License: "https://creativecommons.org/x"},
		"empty source":     {Source: "", ID: "f.jpg", License: "https://creativecommons.org/x"},
		"non-uri license":  {Source: "wikimedia", ID: "f.jpg", License: "CC BY-SA"},
		"empty license":    {Source: "wikimedia", ID: "f.jpg", License: ""},
		"hostless license": {Source: "wikimedia", ID: "f.jpg", License: "https://"},
	}
	for name, item := range cases {
		if err := ValidateMedia(map[string][]MediaItem{"A": {item}}, srcs); err == nil {
			t.Errorf("%s: expected validation error, got nil", name)
		}
	}
}

func TestMergeMedia(t *testing.T) {
	recs := map[string]SpeciesRecord{
		"Aquila chrysaetos": {Taxonomy: Taxonomy{Class: "Aves"}},
	}
	media := map[string][]MediaItem{
		"Aquila chrysaetos": {{Source: "wikimedia", ID: "g.jpg", License: "https://creativecommons.org/licenses/by-sa/4.0/"}},
		"Ghost species":     {{Source: "wikimedia", ID: "x.jpg", License: "https://creativecommons.org/x"}},
	}
	unknown := MergeMedia(recs, media)
	if got := recs["Aquila chrysaetos"].Media; len(got) != 1 || got[0].ID != "g.jpg" {
		t.Fatalf("media not attached: %+v", got)
	}
	if len(unknown) != 1 || unknown[0] != "Ghost species" {
		t.Fatalf("unknown = %v, want [Ghost species]", unknown)
	}
}

func TestMergeMediaUnknownSortedAndAllUnknown(t *testing.T) {
	recs := map[string]SpeciesRecord{"Known one": {Taxonomy: Taxonomy{Class: "Mammalia"}}}
	media := map[string][]MediaItem{
		"Zzz ghost": {{Source: "wikimedia", ID: "z.jpg", License: "https://creativecommons.org/x"}},
		"Aaa ghost": {{Source: "wikimedia", ID: "a.jpg", License: "https://creativecommons.org/x"}},
	}
	unknown := MergeMedia(recs, media)
	if len(unknown) != 2 || unknown[0] != "Aaa ghost" || unknown[1] != "Zzz ghost" {
		t.Fatalf("unknown not sorted: %v", unknown)
	}
	if _, ok := recs["Known one"]; !ok || recs["Known one"].Media != nil {
		t.Fatalf("untouched record should keep nil media: %+v", recs["Known one"])
	}
	if len(recs) != 1 {
		t.Fatalf("no ghost record should be created, got %d records", len(recs))
	}
}

func TestMergeMediaEmpty(t *testing.T) {
	recs := map[string]SpeciesRecord{"A": {}}
	if unknown := MergeMedia(recs, map[string][]MediaItem{}); len(unknown) != 0 {
		t.Fatalf("empty media should report no unknowns, got %v", unknown)
	}
}

func TestLoadMediaError(t *testing.T) {
	if _, err := LoadMedia("/nonexistent/dir/media.json"); err == nil {
		t.Error("LoadMedia should error on a missing file")
	}
}

func TestValidateMediaErrorIdentity(t *testing.T) {
	srcs := map[string]MediaSource{"wikimedia": {Name: "Wikimedia Commons", Thumb: "https://x/{id}"}}
	good := MediaItem{Source: "wikimedia", ID: "ok.jpg", License: "https://creativecommons.org/x"}
	bad := MediaItem{Source: "wikimedia", ID: "", License: "https://creativecommons.org/x"}
	err := ValidateMedia(map[string][]MediaItem{"Panthera leo": {good, bad}}, srcs)
	if err == nil {
		t.Fatal("expected error for the empty id at index 1")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Panthera leo") || !strings.Contains(msg, "[1]") {
		t.Fatalf("error should name the species and the offending index, got %q", msg)
	}

	// A whitespace-only id is rejected like an empty one (Go trims).
	ws := MediaItem{Source: "wikimedia", ID: "   ", License: "https://creativecommons.org/x"}
	if err := ValidateMedia(map[string][]MediaItem{"A": {ws}}, srcs); err == nil {
		t.Error("whitespace-only id should be rejected")
	}
}
