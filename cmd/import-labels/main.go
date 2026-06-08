// Command import-labels merges BirdNET-style per-locale label files into OpenFauna
// locale JSON dictionaries. It is the maintenance tool for giving a regional
// variant (such as en_uk) its overrides and for backfilling locales BirdNET ships
// but OpenFauna lacks.
//
// Scientific names in the label file are resolved through data/aliases.json to
// their canonical OpenFauna form before lookup, so legacy BirdNET names line up
// with GBIF-canonical keys. Only species present in the master locale (en) are
// considered, so non-species label tokens and untracked species are ignored.
//
// Modes:
//
//	-delta   write only entries whose label common name differs from master.
//	         Produces a minimal regional delta, e.g. en_uk over en.
//	-verify  do not write; check that the existing target locale, layered over
//	         master, reproduces every label common name. Exits non-zero on any
//	         mismatch. Use as the no-regression gate after an import.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	labelsPath := flag.String("labels", "", "Path to a BirdNET 'Scientific_Common' label .txt file (required)")
	locale := flag.String("locale", "", "Target locale code, e.g. en_uk (required)")
	masterPath := flag.String("master", "data/locales/en.json", "Master locale JSON: the species OpenFauna tracks")
	aliasesPath := flag.String("aliases", "data/aliases.json", "Path to aliases JSON (alias -> canonical)")
	outDir := flag.String("out", "data/locales", "Directory containing/receiving locale JSON files")
	delta := flag.Bool("delta", false, "Delta mode: only write entries differing from master")
	verify := flag.Bool("verify", false, "Verify mode: check no-regression instead of writing")
	flag.Parse()

	if *labelsPath == "" || *locale == "" {
		log.Fatalf("both -labels and -locale are required")
	}

	labelLines, err := readLines(*labelsPath)
	if err != nil {
		log.Fatalf("read labels %s: %v", *labelsPath, err)
	}
	master, err := readLocale(*masterPath)
	if err != nil {
		log.Fatalf("read master %s: %v", *masterPath, err)
	}
	aliases, err := readLocale(*aliasesPath)
	if err != nil {
		log.Fatalf("read aliases %s: %v", *aliasesPath, err)
	}

	targetPath := filepath.Join(*outDir, *locale+".json")
	existing, err := readLocale(targetPath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Fatalf("read target %s: %v", targetPath, err)
		}
		existing = map[string]string{}
	}

	if *verify {
		miss := verifyNoRegression(existing, master, aliases, labelLines)
		for _, m := range miss {
			log.Printf("MISMATCH %s: got %q want %q", m.sci, m.got, m.want)
		}
		if len(miss) > 0 {
			log.Fatalf("%d mismatch(es): %s would not reproduce its BirdNET names", len(miss), *locale)
		}
		log.Printf("OK: %s reproduces every BirdNET label name (layered over master)", *locale)
		return
	}

	res := mergeLabels(existing, master, aliases, labelLines, *delta)
	if err := writeLocale(targetPath, res.merged); err != nil {
		log.Fatalf("write %s: %v", targetPath, err)
	}
	log.Printf("Wrote %s: %d total entries (%d added/updated)", targetPath, len(res.merged), res.added)
}

type importResult struct {
	merged map[string]string
	added  int
}

type mismatch struct{ sci, got, want string }

// canonical resolves a scientific name to its OpenFauna-canonical form via aliases.
func canonical(sci string, aliases map[string]string) string {
	if c, ok := aliases[sci]; ok {
		return c
	}
	return sci
}

// parseLabel splits a "Scientific_Common" line. ok is false for blank or malformed lines.
func parseLabel(line string) (sci, common string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", "", false
	}
	s, c, found := strings.Cut(line, "_")
	if !found {
		return "", "", false
	}
	s, c = strings.TrimSpace(s), strings.TrimSpace(c)
	if s == "" || c == "" {
		return "", "", false
	}
	return s, c, true
}

// mergeLabels merges label lines into a copy of existing. Only species present in
// master (after alias resolution) are considered. In delta mode an entry is
// written only when the label common name differs from master's. Existing entries
// are preserved and updated only when the label disagrees with them.
func mergeLabels(existing, master, aliases map[string]string, labelLines []string, deltaOnly bool) importResult {
	merged := make(map[string]string, len(existing))
	for k, v := range existing {
		merged[k] = v
	}
	added := 0
	for _, line := range labelLines {
		sci, common, ok := parseLabel(line)
		if !ok {
			continue
		}
		canon := canonical(sci, aliases)
		base, known := master[canon]
		if !known {
			continue // OpenFauna does not track this species
		}
		if deltaOnly && common == base {
			continue // identical to master: no override needed
		}
		if cur, ok := merged[canon]; !ok || cur != common {
			merged[canon] = common
			added++
		}
	}
	return importResult{merged: merged, added: added}
}

// verifyNoRegression checks that target layered over master reproduces every
// BirdNET label common name for species OpenFauna tracks. A non-empty result means
// a user of this locale would see a different name than the BirdNET label.
func verifyNoRegression(target, master, aliases map[string]string, labelLines []string) []mismatch {
	var out []mismatch
	for _, line := range labelLines {
		sci, want, ok := parseLabel(line)
		if !ok {
			continue
		}
		canon := canonical(sci, aliases)
		base, known := master[canon]
		if !known {
			continue
		}
		got := base
		if v, ok := target[canon]; ok {
			got = v
		}
		if got != want {
			out = append(out, mismatch{sci: canon, got: got, want: want})
		}
	}
	return out
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines, sc.Err()
}

func readLocale(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return m, nil
}

// writeLocale writes m as 2-space-indented JSON with map keys sorted
// lexicographically (encoding/json sorts string map keys) and HTML escaping off,
// matching the output of cmd/bootstrap and cmd/import-bats for clean diffs.
func writeLocale(path string, m map[string]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(m)
}
