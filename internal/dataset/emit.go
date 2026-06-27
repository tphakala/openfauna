package dataset

import (
	"bufio"
	"encoding/json"
	"os"
	"sort"
	"strings"
)

// SchemaVersion is the SemVer of the compiled nested schema. Bump the major on
// a breaking change so consumers can gate on it; bump the minor when adding a
// backward-compatible artifact (e.g. aliases.json in 2.1.0) that older consumers
// can safely ignore.
const SchemaVersion = "2.1.0"

// Manifest describes the compiled dataset for the consumer.
type Manifest struct {
	SchemaVersion string `json:"schema_version"`
	SpeciesCount  int    `json:"species_count"`
	AliasCount    int    `json:"alias_count"`
	Source        string `json:"source"`
}

// jsonlLine is a SpeciesRecord plus its scientific name, for one JSONL row.
type jsonlLine struct {
	ScientificName string          `json:"scientific_name"`
	Taxonomy       Taxonomy        `json:"taxonomy"`
	Links          map[string]Link `json:"links,omitempty"`
	Media          []MediaItem     `json:"media,omitempty"`
}

// EmitJSONL writes one compact JSON object per line, sorted by scientific name.
func EmitJSONL(path string, recs map[string]SpeciesRecord) (err error) {
	names := make([]string, 0, len(recs))
	for n := range recs {
		names = append(names, n)
	}
	sort.Strings(names)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	// Capture the Close error (e.g. a delayed write failure on a full disk)
	// when the rest of the function otherwise succeeded.
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)

	for _, n := range names {
		r := recs[n]
		if err = enc.Encode(jsonlLine{
			ScientificName: n,
			Taxonomy:       r.Taxonomy,
			Links:          r.Links,
			Media:          r.Media,
		}); err != nil {
			return err
		}
	}
	// Flush explicitly so a buffered-write failure (e.g. disk full) surfaces
	// instead of silently truncating the artifact.
	return w.Flush()
}

// EmitManifest writes the build manifest. aliasCount is the number of taxonomic
// aliases emitted to aliases.json, so a consumer can sanity-check the alias
// artifact the same way it does the species count.
func EmitManifest(path string, count, aliasCount int, source string) error {
	buf, err := MarshalJSON(Manifest{
		SchemaVersion: SchemaVersion,
		SpeciesCount:  count,
		AliasCount:    aliasCount,
		Source:        source,
	})
	if err != nil {
		return err
	}
	return os.WriteFile(path, buf, 0o644)
}

// FilterAliases returns a clean copy of the authored alias map, dropping
// comment keys (underscore-prefixed, the same convention the translation
// compiler skips) and any entry whose target is empty or identical to its key
// (a no-op self-alias). The result is the scientific-name -> canonical
// scientific-name mapping that consumers apply to normalize reclassified taxa.
func FilterAliases(raw map[string]string) map[string]string {
	out := make(map[string]string, len(raw))
	for alias, canonical := range raw {
		if strings.HasPrefix(alias, "_") {
			continue
		}
		if canonical == "" || alias == canonical {
			continue
		}
		out[alias] = canonical
	}
	return out
}

// EmitAliases writes the compiled taxonomic alias map (legacy scientific name ->
// canonical scientific name) as deterministic, key-sorted JSON, and returns the
// number of aliases written. This is the machine-readable export of
// data/aliases.json: the translation compiler only uses aliases to duplicate
// common names, so without this artifact consumers never receive the
// scientific-to-scientific mapping itself.
func EmitAliases(path string, raw map[string]string) (int, error) {
	aliases := FilterAliases(raw)
	buf, err := MarshalJSON(aliases)
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		return 0, err
	}
	return len(aliases), nil
}

// CopyFile copies a file's bytes verbatim, so a committed build copy of an
// authored registry is byte-identical and deterministically reproducible.
func CopyFile(srcPath, dstPath string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	return os.WriteFile(dstPath, data, 0o644)
}
