package dataset

import (
	"net/url"
	"path"
	"strings"
)

// FlatMeta is the legacy flat per-species metadata shape.
type FlatMeta struct {
	Class          string `json:"class"`
	Order          string `json:"order"`
	Family         string `json:"family"`
	FamilyCommon   string `json:"family_common"`
	WikipediaURL   string `json:"wikipedia_url"`
	INaturalistURL string `json:"inaturalist_url"`
}

// MigrateFlat converts one legacy flat record into the nested record. iNaturalist
// and Wikipedia ids are extracted deterministically from their URLs; a link is
// omitted when its source URL is empty.
func MigrateFlat(f FlatMeta) SpeciesRecord {
	rec := SpeciesRecord{
		Taxonomy: Taxonomy{
			Class:        f.Class,
			Order:        f.Order,
			Family:       f.Family,
			FamilyCommon: f.FamilyCommon,
		},
	}
	links := map[string]Link{}
	if id := lastPathSegment(f.INaturalistURL); id != "" {
		links["inaturalist"] = Link{ID: id}
	}
	if id := lastPathSegment(f.WikipediaURL); id != "" {
		links["wikipedia"] = Link{ID: id}
	}
	if len(links) > 0 {
		rec.Links = links
	}
	return rec
}

// lastPathSegment returns the final decoded path segment of a URL. It parses
// with net/url so query strings, fragments, and percent-encoding are handled
// correctly (a trailing "?locale=en" or "#section" never leaks into the id, and
// "%20" is decoded). Returns "" for an empty input or a URL with no path.
func lastPathSegment(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Path == "" || u.Path == "/" {
		return ""
	}
	return path.Base(u.Path)
}
