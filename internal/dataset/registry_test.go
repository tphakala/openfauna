package dataset

import "testing"

func TestLoadSources(t *testing.T) {
	got, err := LoadSources("../../data/sources.json")
	if err != nil {
		t.Fatalf("LoadSources: %v", err)
	}
	wiki, ok := got["wikipedia"]
	if !ok {
		t.Fatal("missing wikipedia source")
	}
	if wiki.Name != "Wikipedia" || wiki.Order != 10 {
		t.Fatalf("unexpected wikipedia source: %+v", wiki)
	}
	if err := ValidateSources(got); err != nil {
		t.Fatalf("ValidateSources: %v", err)
	}
}

func TestValidateSourcesRejectsMissingPlaceholder(t *testing.T) {
	bad := map[string]Source{"x": {Name: "X", URL: "https://example.com/no-placeholder"}}
	if err := ValidateSources(bad); err == nil {
		t.Fatal("expected error for url without {id}")
	}
}

func TestLoadMediaSources(t *testing.T) {
	got, err := LoadMediaSources("../../data/media_sources.json")
	if err != nil {
		t.Fatalf("LoadMediaSources: %v", err)
	}
	if _, ok := got["wikimedia"]; !ok {
		t.Fatal("missing wikimedia media source")
	}
}

func TestValidateSourcesRejectsEmptyName(t *testing.T) {
	bad := map[string]Source{"x": {Name: "", URL: "https://example.com/{id}"}}
	if err := ValidateSources(bad); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestValidateMediaSources(t *testing.T) {
	got, err := LoadMediaSources("../../data/media_sources.json")
	if err != nil {
		t.Fatalf("LoadMediaSources: %v", err)
	}
	if err := ValidateMediaSources(got); err != nil {
		t.Fatalf("ValidateMediaSources on real data: %v", err)
	}
	bad := map[string]MediaSource{"x": {Name: "X", Thumb: "https://example.com/no-placeholder"}}
	if err := ValidateMediaSources(bad); err == nil {
		t.Fatal("expected error for thumb without {id}")
	}
}
