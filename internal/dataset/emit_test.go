package dataset

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmitJSONLSortedWithName(t *testing.T) {
	recs := map[string]SpeciesRecord{
		"Bbb": {Taxonomy: Taxonomy{Class: "Aves"}},
		"Aaa": {Taxonomy: Taxonomy{Class: "Mammalia"}, Links: map[string]Link{"gbif": {ID: "1"}}},
	}
	p := filepath.Join(t.TempDir(), "metadata.jsonl")
	if err := EmitJSONL(p, recs); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var names []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var row struct {
			ScientificName string `json:"scientific_name"`
		}
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			t.Fatalf("line not valid json: %v", err)
		}
		names = append(names, row.ScientificName)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(names, ",") != "Aaa,Bbb" {
		t.Fatalf("names not sorted: %v", names)
	}
}

func TestEmitManifest(t *testing.T) {
	p := filepath.Join(t.TempDir(), "manifest.json")
	if err := EmitManifest(p, 42, 7, "openfauna@abc1234 2026-06-20"); err != nil {
		t.Fatal(err)
	}
	var m Manifest
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m.SchemaVersion != SchemaVersion || m.SpeciesCount != 42 || m.AliasCount != 7 || m.Source != "openfauna@abc1234 2026-06-20" {
		t.Fatalf("manifest = %+v", m)
	}
}

func TestFilterAliases(t *testing.T) {
	raw := map[string]string{
		"Streptopelia senegalensis": "Spilopelia senegalensis", // kept
		"Accipiter gentilis":        "Astur gentilis",          // kept
		"_comment":                  "ignored",                 // dropped: comment key
		"Empty target":              "",                        // dropped: empty value
		"Self alias":                "Self alias",              // dropped: no-op
	}
	got := FilterAliases(raw)
	if len(got) != 2 {
		t.Fatalf("expected 2 aliases, got %d: %v", len(got), got)
	}
	if got["Streptopelia senegalensis"] != "Spilopelia senegalensis" {
		t.Errorf("senegalensis alias missing or wrong: %v", got)
	}
	if got["Accipiter gentilis"] != "Astur gentilis" {
		t.Errorf("gentilis alias missing or wrong: %v", got)
	}
	if _, ok := got["_comment"]; ok {
		t.Error("comment key was not dropped")
	}
}

func TestEmitAliases(t *testing.T) {
	raw := map[string]string{
		"Streptopelia senegalensis": "Spilopelia senegalensis",
		"_comment":                  "ignored",
	}
	p := filepath.Join(t.TempDir(), "aliases.json")
	n, err := EmitAliases(p, raw)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 alias written, got %d", n)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("aliases.json not valid json: %v", err)
	}
	if m["Streptopelia senegalensis"] != "Spilopelia senegalensis" {
		t.Errorf("alias mapping wrong: %v", m)
	}
	if _, ok := m["_comment"]; ok {
		t.Error("comment key leaked into aliases.json")
	}
}

func TestEmitJSONLNoHTMLEscape(t *testing.T) {
	recs := map[string]SpeciesRecord{
		"Aaa": {
			Taxonomy: Taxonomy{FamilyCommon: "Hawks & Eagles"},
			Links:    map[string]Link{"wikipedia": {ID: "A&B<C>"}},
		},
	}
	p := filepath.Join(t.TempDir(), "metadata.jsonl")
	if err := EmitJSONL(p, recs); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	// If SetEscapeHTML(false) regressed, the ampersand and angle brackets would
	// be emitted as backslash-u escapes, so these literal substrings would not
	// be found. Their presence proves escaping is off.
	if !strings.Contains(s, "Hawks & Eagles") || !strings.Contains(s, "A&B<C>") {
		t.Errorf("expected literal & < > (no HTML escaping), got: %s", s)
	}
}

func TestEmitJSONLContentAndOmitEmpty(t *testing.T) {
	recs := map[string]SpeciesRecord{
		"Withlinks": {Taxonomy: Taxonomy{Class: "Aves"}, Links: map[string]Link{"gbif": {ID: "1"}}},
		"Nolinks":   {Taxonomy: Taxonomy{Class: "Mammalia"}},
	}
	p := filepath.Join(t.TempDir(), "metadata.jsonl")
	if err := EmitJSONL(p, recs); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows := map[string]jsonlLine{}
	var rawLines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		rawLines = append(rawLines, sc.Text())
		var row jsonlLine
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			t.Fatalf("line not valid json: %v", err)
		}
		rows[row.ScientificName] = row
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if rows["Withlinks"].Taxonomy.Class != "Aves" {
		t.Errorf("taxonomy not emitted: %+v", rows["Withlinks"])
	}
	if rows["Withlinks"].Links["gbif"].ID != "1" {
		t.Errorf("links not emitted: %+v", rows["Withlinks"])
	}
	for _, line := range rawLines {
		if strings.Contains(line, "Nolinks") && strings.Contains(line, `"links"`) {
			t.Errorf("omitempty failed, links present on no-links record: %s", line)
		}
	}
}
