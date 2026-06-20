package dataset

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// LoadMedia reads the curated per-species media file: a map from scientific name
// to its list of media items. Media is authored separately from metadata because
// it grows on a different cadence (see the data-model spec).
func LoadMedia(path string) (map[string][]MediaItem, error) {
	var m map[string][]MediaItem
	if err := loadJSON(path, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// ValidateMedia rejects media entries that the consumer cannot render: an unknown
// source key, a missing id, or a missing/non-URI license. The license is
// mandatory to preserve the redistribution-clean posture.
func ValidateMedia(media map[string][]MediaItem, sources map[string]MediaSource) error {
	for name, items := range media {
		for i, it := range items {
			if strings.TrimSpace(it.Source) == "" {
				return fmt.Errorf("media %q[%d]: empty source", name, i)
			}
			if _, ok := sources[it.Source]; !ok {
				return fmt.Errorf("media %q[%d]: unknown source %q", name, i, it.Source)
			}
			if strings.TrimSpace(it.ID) == "" {
				return fmt.Errorf("media %q[%d]: empty id", name, i)
			}
			if !isLicenseURI(it.License) {
				return fmt.Errorf("media %q[%d]: license must be a URI, got %q", name, i, it.License)
			}
		}
	}
	return nil
}

// isLicenseURI reports whether s is a usable absolute http(s) URI. A bare scheme
// like "https://" (no host) is rejected so the "must be a URI" contract is real.
// url.Parse keeps this consistent with AcceptLicense; the explicit host check is
// what rejects a hostless value.
func isLicenseURI(s string) bool {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// MergeMedia attaches each species' media items to its record in place, keyed by
// scientific name. It returns the sorted scientific names that appear in media but
// not in recs, so the caller can warn about stale keys; those entries are dropped
// from the build rather than emitted for a species the dataset does not track.
func MergeMedia(recs map[string]SpeciesRecord, media map[string][]MediaItem) []string {
	var unknown []string
	for name, items := range media {
		rec, ok := recs[name]
		if !ok {
			unknown = append(unknown, name)
			continue
		}
		rec.Media = items
		recs[name] = rec
	}
	sort.Strings(unknown)
	return unknown
}
