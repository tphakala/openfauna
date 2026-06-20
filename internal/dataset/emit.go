package dataset

import (
	"bufio"
	"encoding/json"
	"os"
	"sort"
)

// SchemaVersion is the SemVer of the compiled nested schema. Bump the major on
// a breaking change so consumers can gate on it.
const SchemaVersion = "2.0.0"

// Manifest describes the compiled dataset for the consumer.
type Manifest struct {
	SchemaVersion string `json:"schema_version"`
	SpeciesCount  int    `json:"species_count"`
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

// EmitManifest writes the build manifest.
func EmitManifest(path string, count int, source string) error {
	buf, err := MarshalJSON(Manifest{
		SchemaVersion: SchemaVersion,
		SpeciesCount:  count,
		Source:        source,
	})
	if err != nil {
		return err
	}
	return os.WriteFile(path, buf, 0o644)
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
