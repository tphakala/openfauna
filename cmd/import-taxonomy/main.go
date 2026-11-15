// Command import-taxonomy fills OpenFauna with the species, English names and
// locale names of a BirdNET+ Taxonomy CSV, for every scientific name listed in
// the label manifests, and records reviewed rename pairs as aliases.
//
// Contract:
//
//   - Add-only. An existing metadata record, en.json entry or locale value is
//     never changed. The one exception is the merge of a rename pair whose model
//     name is already a full en.json entry (see applyRenames).
//   - Idempotent and resumable. A second run with the same inputs changes no
//     byte, and a run interrupted between file writes is completed by rerunning
//     it: en.json and aliases.json, the files whose state decides what a rerun
//     does, are written last.
//   - Species rows only. Rows of any other record_type, such as sound classes,
//     get no record and no name.
//   - Exact scientific-name matches only. A label known to the taxonomy only as
//     an alias of another record stays unresolved.
//
// Targets are the union of the scientific names in data/validation/labels/*.txt,
// one name per line (the text before the first "_" of a model label, or the
// whole line for class labels). Regenerate a manifest from a model label file
// with: sed 's/_.*//' <labels.txt> | LC_ALL=C sort -u
//
// Rename files are tab-separated without a header. Column 0 is the geomodel
// scientific name and column 1 the model label; lines starting with # and blank
// lines are ignored. The alias is written as model label -> geomodel name.
package main

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tphakala/openfauna/internal/dataset"
)

const (
	speciesRecordType = "species"
	enLocale          = "en"
)

var requiredColumns = []string{"scientific_name", "common_name", "taxon_group", "record_type", "inat_id", "wikidata_qid"}

// lowercaseLocales use a lowercase initial for species names.
var lowercaseLocales = map[string]bool{"fi": true, "sv": true, "no": true}

type stringList []string

func (l *stringList) String() string     { return strings.Join(*l, ",") }
func (l *stringList) Set(v string) error { *l = append(*l, v); return nil }

// taxon is one species row of the taxonomy CSV.
type taxon struct {
	sci, common, group, recType, inat, qid string
	names                                  map[string]string // locale file stem -> name
}

// renamePair is one row of a rename file.
type renamePair struct{ geo, model string }

// state is the part of data/ the importer changes.
type state struct {
	meta    map[string]dataset.SpeciesRecord
	aliases map[string]string
	locales map[string]map[string]string
}

func (s *state) clone() *state {
	c := &state{
		meta:    make(map[string]dataset.SpeciesRecord, len(s.meta)),
		aliases: make(map[string]string, len(s.aliases)),
		locales: make(map[string]map[string]string, len(s.locales)),
	}
	for k, v := range s.meta {
		c.meta[k] = v
	}
	for k, v := range s.aliases {
		c.aliases[k] = v
	}
	for loc, m := range s.locales {
		cm := make(map[string]string, len(m))
		for k, v := range m {
			cm[k] = v
		}
		c.locales[loc] = cm
	}
	return c
}

func main() {
	taxonomyPath := flag.String("taxonomy", "", "Path to BirdNET_Taxonomy_<version>.csv (required)")
	var renames stringList
	flag.Var(&renames, "renames", "Rename TSV, geomodel name then model label (repeatable)")
	dataDir := flag.String("data", "data", "OpenFauna data directory")
	labelsDir := flag.String("labels", "data/validation/labels", "Directory of label manifests (*.txt)")
	dryRun := flag.Bool("dry-run", false, "Report what would change without writing")
	flag.Parse()

	if *taxonomyPath == "" {
		log.Fatal("-taxonomy is required")
	}
	if err := run(*taxonomyPath, renames, *dataDir, *labelsDir, *dryRun); err != nil {
		log.Fatal(err)
	}
}

func run(taxonomyPath string, renameFiles []string, dataDir, labelsDir string, dryRun bool) error {
	s, err := loadState(dataDir)
	if err != nil {
		return err
	}
	orig := s.clone()

	targets, err := loadTargets(labelsDir)
	if err != nil {
		return err
	}
	tax, sum, err := loadTaxonomy(taxonomyPath)
	if err != nil {
		return err
	}
	var pairs []renamePair
	for _, f := range renameFiles {
		p, err := loadRenames(f)
		if err != nil {
			return err
		}
		pairs = append(pairs, p...)
	}

	if err := applyRenames(s, pairs); err != nil {
		return err
	}
	rep := importSpecies(s, tax, targets)

	fmt.Printf("taxonomy sha256 %x (%d rows)\n", sum, len(tax))
	fmt.Printf("targets: %d\n", len(targets))
	fmt.Printf("aliases: %d -> %d\n", len(orig.aliases), len(s.aliases))
	fmt.Printf("metadata records added: %d\n", rep.metaAdded)
	fmt.Printf("English names added: %d\n", rep.namesAdded)
	fmt.Printf("locale values added: %d\n", rep.localeValues)
	for _, name := range rep.dashSkipped {
		fmt.Printf("skipped, English name contains a dash: %s\n", name)
	}
	if dryRun {
		fmt.Println("dry run: nothing written")
		return nil
	}
	return writeState(dataDir, s)
}

func loadJSONFile(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func loadState(dataDir string) (*state, error) {
	s := &state{aliases: map[string]string{}, locales: map[string]map[string]string{}}
	var err error
	if s.meta, err = dataset.LoadMetadata(filepath.Join(dataDir, "metadata.json")); err != nil {
		return nil, err
	}
	if err := loadJSONFile(filepath.Join(dataDir, "aliases.json"), &s.aliases); err != nil {
		return nil, err
	}
	paths, err := filepath.Glob(filepath.Join(dataDir, "locales", "*.json"))
	if err != nil {
		return nil, err
	}
	for _, p := range paths {
		m := map[string]string{}
		if err := loadJSONFile(p, &m); err != nil {
			return nil, err
		}
		s.locales[strings.TrimSuffix(filepath.Base(p), ".json")] = m
	}
	if s.locales[enLocale] == nil {
		return nil, fmt.Errorf("%s/locales/en.json is missing", dataDir)
	}
	return s, nil
}

func loadTargets(dir string) ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.txt"))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no label manifests in %s", dir)
	}
	seen := map[string]bool{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(string(b), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				seen[line] = true
			}
		}
	}
	return sortedKeys(seen), nil
}

func loadRenames(path string) ([]renamePair, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pairs []renamePair
	for i, line := range strings.Split(string(b), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) < 2 {
			return nil, fmt.Errorf("%s:%d: want at least 2 tab-separated columns", path, i+1)
		}
		pairs = append(pairs, renamePair{geo: strings.TrimSpace(cols[0]), model: strings.TrimSpace(cols[1])})
	}
	return pairs, nil
}

// localeStem maps a taxonomy column suffix to a locale file stem: es_EC to es_ec,
// zh-CN to zh_cn.
func localeStem(suffix string) string {
	return strings.ToLower(strings.ReplaceAll(suffix, "-", "_"))
}

func loadTaxonomy(path string) (map[string]taxon, [sha256.Size]byte, error) {
	var sum [sha256.Size]byte
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, sum, err
	}
	sum = sha256.Sum256(b)
	rows, err := csv.NewReader(strings.NewReader(string(b))).ReadAll()
	if err != nil {
		return nil, sum, fmt.Errorf("read %s: %w", path, err)
	}
	if len(rows) == 0 {
		return nil, sum, fmt.Errorf("%s is empty", path)
	}
	col := map[string]int{}
	for i, h := range rows[0] {
		col[h] = i
	}
	for _, name := range requiredColumns {
		if _, ok := col[name]; !ok {
			return nil, sum, fmt.Errorf("%s has no %q column", path, name)
		}
	}
	type localeCol struct {
		stem string
		idx  int
	}
	var localeCols []localeCol
	for h, i := range col {
		suffix, ok := strings.CutPrefix(h, "common_name_")
		if !ok || suffix == "en" || suffix == "alt" || suffix == "aliases" {
			continue
		}
		localeCols = append(localeCols, localeCol{localeStem(suffix), i})
	}
	tax := make(map[string]taxon, len(rows))
	for _, r := range rows[1:] {
		t := taxon{
			sci:     strings.TrimSpace(r[col["scientific_name"]]),
			common:  strings.TrimSpace(r[col["common_name"]]),
			group:   strings.TrimSpace(r[col["taxon_group"]]),
			recType: strings.TrimSpace(r[col["record_type"]]),
			inat:    strings.TrimSpace(r[col["inat_id"]]),
			qid:     strings.TrimSpace(r[col["wikidata_qid"]]),
			names:   map[string]string{},
		}
		if t.sci == "" {
			continue
		}
		if _, dup := tax[t.sci]; dup {
			return nil, sum, fmt.Errorf("%s: duplicate scientific_name %q", path, t.sci)
		}
		for _, lc := range localeCols {
			if v := strings.TrimSpace(r[lc.idx]); v != "" {
				t.names[lc.stem] = v
			}
		}
		tax[t.sci] = t
	}
	return tax, sum, nil
}

func hasDash(s string) bool { return strings.ContainsAny(s, "–—") }

// applyRenames records each pair as the alias model -> geomodel. Pairs are
// checked in order against the alias map as already extended by earlier pairs,
// and s is left unchanged unless every pair passes, so the alias map stays a
// single-hop function (no chain, no double target).
//
// When the model name is a full en.json entry (the alias key would break the
// "alias key is not a full entry" rule), its names are merged into the
// geomodel entry first: in en the geomodel value stays; in every other locale a
// value only the model name has is moved over, and when both have one the
// model's replaces the geomodel's only if the geomodel's is an English echo and
// the model's is not. The model entries are then deleted.
func applyRenames(s *state, pairs []renamePair) error {
	work := s.clone()
	before := s.aliases
	for i, p := range pairs {
		g, m := p.geo, p.model
		where := fmt.Sprintf("rename %d (%q -> %q)", i+1, m, g)
		if g == "" || m == "" {
			return fmt.Errorf("%s: empty name", where)
		}
		if m == g || before[g] == m {
			continue
		}
		if work.aliases[g] == m {
			return fmt.Errorf("%s: contradicts an earlier row of this run", where)
		}
		if t, ok := work.aliases[m]; ok && t != g {
			return fmt.Errorf("%s: model name already aliases %q", where, t)
		}
		if _, ok := work.aliases[g]; ok {
			return fmt.Errorf("%s: geomodel name is itself an alias key", where)
		}
		for k, v := range work.aliases {
			if v == m && !strings.HasPrefix(k, "_") {
				return fmt.Errorf("%s: model name is the target of alias %q", where, k)
			}
		}
		en := work.locales[enLocale]
		if _, ok := en[g]; !ok {
			return fmt.Errorf("%s: geomodel name is not an en.json key", where)
		}
		if _, full := en[m]; full {
			mergeEntry(work, g, m)
		}
		work.aliases[m] = g
	}
	*s = *work
	return nil
}

// mergeEntry folds the locale entries of m into g and deletes m.
func mergeEntry(s *state, g, m string) {
	enG, enM := s.locales[enLocale][g], s.locales[enLocale][m]
	isEcho := func(v string) bool { return strings.EqualFold(v, enG) || strings.EqualFold(v, enM) }
	for loc, entries := range s.locales {
		mv, ok := entries[m]
		if !ok {
			continue
		}
		if gv, has := entries[g]; loc != enLocale && (!has || (isEcho(gv) && !isEcho(mv))) {
			entries[g] = mv
		}
		delete(entries, m)
	}
}

type importReport struct {
	metaAdded, namesAdded, localeValues int
	dashSkipped                         []string
}

type rank struct{ class, order, family string }

// genusRanks maps a genus to its single (class, order, family) among records
// that have a family. A genus with more than one distinct triple is left out.
func genusRanks(meta map[string]dataset.SpeciesRecord) map[string]rank {
	ranks := map[string]rank{}
	ambiguous := map[string]bool{}
	for key, rec := range meta {
		if rec.Taxonomy.Family == "" {
			continue
		}
		genus, _, _ := strings.Cut(key, " ")
		r := rank{rec.Taxonomy.Class, rec.Taxonomy.Order, rec.Taxonomy.Family}
		if prev, ok := ranks[genus]; ok && prev != r {
			ambiguous[genus] = true
		}
		ranks[genus] = r
	}
	for g := range ambiguous {
		delete(ranks, g)
	}
	return ranks
}

// importSpecies adds, for each target that has a species row, a metadata
// record when none exists and, when the label is neither an alias key nor
// already named in en.json, its English name and the locale names that
// existing locale files lack. Nothing existing is changed.
func importSpecies(s *state, tax map[string]taxon, targets []string) importReport {
	var rep importReport
	ranks := genusRanks(s.meta)
	en := s.locales[enLocale]
	locs := sortedKeys(s.locales)
	for _, target := range sortedUnique(targets) {
		t, ok := tax[target]
		if !ok || t.recType != speciesRecordType {
			continue
		}
		if _, has := s.meta[target]; !has {
			s.meta[target] = newRecord(target, t, ranks)
			rep.metaAdded++
		}
		_, isAlias := s.aliases[target]
		if _, named := en[target]; named || isAlias || t.common == "" {
			continue
		}
		if hasDash(t.common) {
			rep.dashSkipped = append(rep.dashSkipped, target)
			continue
		}
		en[target] = t.common
		rep.namesAdded++
		for _, loc := range locs {
			v, ok := t.names[loc]
			if loc == enLocale || !ok {
				continue
			}
			if _, exists := s.locales[loc][target]; exists {
				continue
			}
			if v == target || strings.EqualFold(v, t.common) || hasDash(v) {
				continue
			}
			if lowercaseLocales[loc] {
				v = lowercaseInitial(target, v)
			}
			s.locales[loc][target] = v
			rep.localeValues++
		}
	}
	return rep
}

func newRecord(sci string, t taxon, ranks map[string]rank) dataset.SpeciesRecord {
	rec := dataset.SpeciesRecord{Taxonomy: dataset.Taxonomy{Class: t.group}}
	genus, _, _ := strings.Cut(sci, " ")
	if r, ok := ranks[genus]; ok && r.class == t.group {
		rec.Taxonomy.Order, rec.Taxonomy.Family = r.order, r.family
	}
	if t.inat != "" || t.qid != "" {
		rec.Links = map[string]dataset.Link{}
		if t.inat != "" {
			rec.Links["inaturalist"] = dataset.Link{ID: t.inat}
		}
		if t.qid != "" {
			rec.Links["wikipedia"] = dataset.Link{ID: t.qid}
		}
	}
	return rec
}

// lowercaseInitial lowercases the first letter of a fi/sv/no species name,
// except for names the casing convention keeps capitalized: a Latin synonym
// (same first word as the key, or sharing its last word) and a separate-word
// genitive eponym ("Stellers sjolejon"). It reads only the key and the value.
func lowercaseInitial(key, value string) string {
	idx := strings.IndexFunc(value, unicode.IsLetter)
	if idx < 0 {
		return value
	}
	r, size := utf8.DecodeRuneInString(value[idx:])
	if !unicode.IsUpper(r) {
		return value
	}
	words, keyWords := strings.Split(value, " "), strings.Split(key, " ")
	if words[0] == keyWords[0] {
		return value
	}
	if len(words) > 1 {
		head := words[0]
		if words[len(words)-1] == keyWords[len(keyWords)-1] ||
			strings.HasSuffix(head, "s") || strings.HasSuffix(head, "s’") || strings.HasSuffix(head, "s'") {
			return value
		}
	}
	return value[:idx] + string(unicode.ToLower(r)) + value[idx+size:]
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedUnique(in []string) []string {
	set := make(map[string]bool, len(in))
	for _, v := range in {
		set[v] = true
	}
	return sortedKeys(set)
}

// writeState writes every changed file. Locale files other than en.json go
// first, then metadata.json, en.json and aliases.json last, so an interrupted
// run is completed by rerunning it.
func writeState(dataDir string, s *state) error {
	for _, loc := range sortedKeys(s.locales) {
		if loc == enLocale {
			continue
		}
		if err := writeIfChanged(filepath.Join(dataDir, "locales", loc+".json"), s.locales[loc]); err != nil {
			return err
		}
	}
	if err := writeIfChanged(filepath.Join(dataDir, "metadata.json"), s.meta); err != nil {
		return err
	}
	if err := writeIfChanged(filepath.Join(dataDir, "locales", enLocale+".json"), s.locales[enLocale]); err != nil {
		return err
	}
	return writeIfChanged(filepath.Join(dataDir, "aliases.json"), s.aliases)
}

func writeIfChanged(path string, v any) error {
	out, err := dataset.MarshalJSON(v)
	if err != nil {
		return err
	}
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if string(old) == string(out) {
		return nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
