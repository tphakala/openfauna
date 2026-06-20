package dataset

import "testing"

func TestMigrateFlat(t *testing.T) {
	in := FlatMeta{
		Class: "Aves", Order: "Accipitriformes", Family: "Accipitridae",
		WikipediaURL:   "https://en.wikipedia.org/wiki/Golden_eagle",
		INaturalistURL: "https://www.inaturalist.org/taxa/5305",
	}
	got := MigrateFlat(in)
	if got.Taxonomy.Class != "Aves" {
		t.Fatalf("class = %q", got.Taxonomy.Class)
	}
	if got.Links["wikipedia"].ID != "Golden_eagle" {
		t.Fatalf("wikipedia id = %q", got.Links["wikipedia"].ID)
	}
	if got.Links["inaturalist"].ID != "5305" {
		t.Fatalf("inat id = %q", got.Links["inaturalist"].ID)
	}
}

func TestMigrateFlatOmitsEmpty(t *testing.T) {
	got := MigrateFlat(FlatMeta{Class: "Mammalia"})
	if _, ok := got.Links["wikipedia"]; ok {
		t.Fatal("expected no wikipedia link when URL empty")
	}
	if _, ok := got.Links["inaturalist"]; ok {
		t.Fatal("expected no inaturalist link when URL empty")
	}
}

func TestLastPathSegment(t *testing.T) {
	cases := map[string]string{
		"https://www.inaturalist.org/taxa/5305":              "5305",
		"https://www.inaturalist.org/taxa/5305/":             "5305",         // trailing slash
		"https://www.inaturalist.org/taxa/5305?locale=en":    "5305",         // query string
		"https://en.wikipedia.org/wiki/Golden_eagle#Section": "Golden_eagle", // fragment
		"https://en.wikipedia.org/wiki/Canis%20lupus":        "Canis lupus",  // percent-encoding
		"  https://en.wikipedia.org/wiki/Golden_eagle  ":     "Golden_eagle", // whitespace
		"5305":                 "5305", // no slash
		"https://example.com":  "",     // no path
		"https://example.com/": "",     // root path only
		"":                     "",
		"   ":                  "",
	}
	for in, want := range cases {
		if got := lastPathSegment(in); got != want {
			t.Errorf("lastPathSegment(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMigrateFlatStripsQueryString(t *testing.T) {
	got := MigrateFlat(FlatMeta{INaturalistURL: "https://www.inaturalist.org/taxa/5305?locale=en"})
	if got.Links["inaturalist"].ID != "5305" {
		t.Fatalf("inat id = %q, want 5305", got.Links["inaturalist"].ID)
	}
}
