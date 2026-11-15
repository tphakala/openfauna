package main

import (
	"crypto/sha256"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tphakala/openfauna/internal/dataset"
)

func newState(aliases map[string]string, locales map[string]map[string]string) *state {
	s := &state{
		meta:    map[string]dataset.SpeciesRecord{},
		aliases: aliases,
		locales: locales,
	}
	if s.aliases == nil {
		s.aliases = map[string]string{}
	}
	return s
}

func TestApplyRenames(t *testing.T) {
	en := map[string]string{"Geo1": "One", "Geo2": "Two", "Geo3": "Three", "Mod5": "Five"}
	tests := []struct {
		name    string
		aliases map[string]string
		pairs   []renamePair
		want    map[string]string
		wantErr bool
	}{
		{"new pair adds model to geomodel", nil,
			[]renamePair{{"Geo1", "Mod1"}}, map[string]string{"Mod1": "Geo1"}, false},
		{"pair already present is kept", map[string]string{"Mod2": "Geo2"},
			[]renamePair{{"Geo2", "Mod2"}}, map[string]string{"Mod2": "Geo2"}, false},
		{"reversed pre-existing pair is skipped", map[string]string{"Geo3": "Mod5"},
			[]renamePair{{"Geo3", "Mod5"}}, map[string]string{"Geo3": "Mod5"}, false},
		{"self pair is skipped", nil,
			[]renamePair{{"Geo1", "Geo1"}}, map[string]string{}, false},
		{"model already aliasing elsewhere", map[string]string{"Mod6": "Geo2"},
			[]renamePair{{"Geo1", "Mod6"}}, nil, true},
		{"geomodel name is an alias key", map[string]string{"Geo3": "Geo2"},
			[]renamePair{{"Geo3", "Mod7"}}, nil, true},
		{"model is the value of an alias", map[string]string{"Zed": "Mod8"},
			[]renamePair{{"Geo1", "Mod8"}}, nil, true},
		{"geomodel name absent from en.json", nil,
			[]renamePair{{"Nowhere", "Mod9"}}, nil, true},
		{"empty model cell", nil,
			[]renamePair{{"Geo1", ""}}, nil, true},
		{"empty geomodel cell", nil,
			[]renamePair{{"", "Mod9"}}, nil, true},
		{"one model with two targets", nil,
			[]renamePair{{"Geo1", "Mod10"}, {"Geo2", "Mod10"}}, nil, true},
		{"two rows of one run form a chain", nil,
			[]renamePair{{"Geo2", "Geo3x"}, {"Geo1", "Geo2"}}, nil, true},
		{"contradictory rows", nil,
			[]renamePair{{"Geo1", "Mod11"}, {"Mod11", "Geo1"}}, nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newState(copyMap(tc.aliases), map[string]map[string]string{"en": copyMap(en)})
			before := s.clone()
			err := applyRenames(s, tc.pairs)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want an error, got none; aliases %v", s.aliases)
				}
				if !reflect.DeepEqual(s, before) {
					t.Errorf("state changed despite the error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(s.aliases, tc.want) {
				t.Errorf("aliases = %v, want %v", s.aliases, tc.want)
			}
		})
	}
}

const (
	bareBrown   = "قهوهای"
	joinedBrown = "قهوه\u200cای"
)

func mergeFixture() *state {
	return newState(
		map[string]string{
			"Poo hypo": "Poo hyp",
			"Two a1":   "Two can", "Two a2": "Two can",
			"_comment": "text",
			"Chain z":  "Chain b", "Chain b": "Chain c", // chain: cells under z stay
			"Self s":  "Self s", // self alias: cell and en entry stay
			"Empty e": "",
		},
		map[string]map[string]string{
			"en":  {"Poo hyp": "Warbling-Finch", "Poo hypo": "Warbling Finch", "Self s": "Self", "Chain c": "C"},
			"a":   {"Poo hyp": "Warbling-Finch", "Poo hypo": "Real A"}, // canonical echoes its English name: alias wins
			"sci": {"Poo hyp": "Poo hyp", "Poo hypo": "Real S"},        // canonical echoes its scientific name: alias wins
			"b":   {"Poo hyp": "Real B1", "Poo hypo": "Real B2"},       // both real: canonical kept, conflict
			"c":   {"Poo hypo": "Real C"},                              // only the alias: moved
			"d":   {"Poo hyp": "Real D"},                               // only the canonical: untouched
			"x":   {"Poo hypo": "poo hyp"},                             // only the alias, an echo: dropped
			"y":   {"Poo hypo": "  "},                                  // only the alias, blank: dropped
			"g":   {"Poo hyp": "Real G", "Poo hypo": "real g"},         // case only: canonical kept
			"h":   {"Poo hyp": "Real H", "Poo hypo": "Real H"},         // equal
			"f":   {"Poo hyp": "Real F", "Poo hypo": "Warbling-Finch"}, // alias echoes, canonical real: canonical kept
			"k":   {"Two a1": "First", "Two a2": "Second"},             // two aliases, no canonical: sorted first wins
			"p":   {"Poo hypo": "Warbling Finch"},                      // only the alias, echoing its own English name: dropped
			"q":   {"Poo hypo": "Poo hypo"},                            // only the alias, echoing its own key: dropped
			"fa":  {"Poo hypo": bareBrown},                             // moved in its joined form
			"u":   {"_comment": "kept", "Chain z": "Z", "Self s": "S", "Empty e": "E"},
		})
}

func TestMergeAliasCells(t *testing.T) {
	fa := zwnjTokens{bareBrown: joinedBrown}
	s := mergeFixture()
	rep := mergeAliasCells(s, fa)

	want := map[string]map[string]string{
		"en":  {"Poo hyp": "Warbling-Finch", "Self s": "Self", "Chain c": "C"},
		"a":   {"Poo hyp": "Real A"},
		"sci": {"Poo hyp": "Real S"},
		"b":   {"Poo hyp": "Real B1"},
		"c":   {"Poo hyp": "Real C"},
		"d":   {"Poo hyp": "Real D"},
		"x":   {},
		"y":   {},
		"p":   {},
		"q":   {},
		"g":   {"Poo hyp": "Real G"},
		"h":   {"Poo hyp": "Real H"},
		"f":   {"Poo hyp": "Real F"},
		"k":   {"Two can": "First"},
		"fa":  {"Poo hyp": joinedBrown},
		"u":   {"_comment": "kept", "Chain z": "Z", "Self s": "S", "Empty e": "E"},
	}
	if !reflect.DeepEqual(s.locales, want) {
		t.Errorf("locales = %v\nwant %v", s.locales, want)
	}
	wantRep := mergeReport{equal: 1, caseOnly: 1, moved: 3, echoReplaced: 2, echoDropped: 4, conflicts: []string{
		`b: "Poo hypo" "Real B2" dropped, "Poo hyp" keeps "Real B1"`,
		`f: "Poo hypo" "Warbling-Finch" dropped, "Poo hyp" keeps "Real F"`,
		`k: "Two a2" "Second" dropped, "Two can" keeps "First"`,
	}}
	if !reflect.DeepEqual(rep, wantRep) {
		t.Errorf("report = %+v\nwant %+v", rep, wantRep)
	}
	if rep.total() != 14 {
		t.Errorf("total = %d, want 14", rep.total())
	}

	// A second pass finds nothing.
	before := s.clone()
	if again := mergeAliasCells(s, fa); again.total() != 0 || !reflect.DeepEqual(s, before) {
		t.Errorf("second pass changed state or reported %+v", again)
	}
}

// A rename pair whose model name has cells only in non-English locales becomes
// an alias over those cells; the merge moves them under the geomodel name.
func TestMergeAliasCells_AfterRename(t *testing.T) {
	s := newState(nil, map[string]map[string]string{
		"en": {"Geo one": "One"},
		"de": {"Mod one": "Eins"},
		"fr": {"Mod one": "Un", "Geo one": "Un bis"},
	})
	if err := applyRenames(s, []renamePair{{"Geo one", "Mod one"}}); err != nil {
		t.Fatal(err)
	}
	if got := s.locales["de"]; !reflect.DeepEqual(got, map[string]string{"Mod one": "Eins"}) {
		t.Fatalf("applyRenames changed locale cells: %v", got)
	}
	rep := mergeAliasCells(s, nil)
	want := map[string]map[string]string{
		"en": {"Geo one": "One"},
		"de": {"Geo one": "Eins"},
		"fr": {"Geo one": "Un bis"},
	}
	if !reflect.DeepEqual(s.locales, want) || rep.moved != 1 || len(rep.conflicts) != 1 {
		t.Errorf("locales = %v, report = %+v", s.locales, rep)
	}
}

func seqFixture() (*state, map[string]taxon, []string, []string) {
	s := newState(
		map[string]string{"Genus2 alias": "Genus2 canon"},
		map[string]map[string]string{
			"en": {"Genus1 existing": "Existing Bird", "Genus1 newmeta": "New Meta Bird", "Genus2 canon": "Canon Bird"},
			"de": {"Genus1 existing": "Bestehend", "Genus1 nameonly": "Alt"},
			"fr": {},
			"fi": {},
		})
	s.meta["Genus1 existing"] = dataset.SpeciesRecord{Taxonomy: dataset.Taxonomy{Class: "Aves", Order: "Ord", Family: "Fam"}}
	s.meta["Genus1 nameonly"] = dataset.SpeciesRecord{Taxonomy: dataset.Taxonomy{Class: "Aves", Order: "Ord", Family: "Fam"}}
	tax := map[string]taxon{
		"Genus1 newmeta": {sci: "Genus1 newmeta", common: "New Meta Bird", group: "Aves", recType: "species", inat: "1", qid: "Q1"},
		"Genus1 nameonly": {sci: "Genus1 nameonly", common: "Name Only Bird", group: "Aves", recType: "species", inat: "2",
			names: map[string]string{"de": "Nur Name", "fr": "Oiseau", "fi": "Harmaa lintu", "it": "Uccello"}},
		"Genus2 alias":    {sci: "Genus2 alias", common: "Alias Bird", group: "Aves", recType: "species", inat: "3", names: map[string]string{"de": "Alias"}},
		"Soundy":          {sci: "Soundy", common: "Soundy", group: "Sound", recType: "sound_class"},
		"Genus1 mammal":   {sci: "Genus1 mammal", common: "Mammal Name", group: "Mammalia", recType: "species", qid: "Q6"},
		"Genus3 a":        {sci: "Genus3 a", common: "Three A", group: "Aves", recType: "species", inat: "8"},
		"Genus3 b":        {sci: "Genus3 b", common: "Three B", group: "Aves", recType: "species", inat: "9"},
		"Genus5 nocommon": {sci: "Genus5 nocommon", common: "", group: "Aves", recType: "species", names: map[string]string{"de": "Ohne"}},
		"Genus6 dashed":   {sci: "Genus6 dashed", common: "Dash\u2013Bird", group: "Aves", recType: "species", names: map[string]string{"de": "Strich"}},
		"Genus4 echo": {sci: "Genus4 echo", common: "Echo Bird", group: "Aves", recType: "species",
			names: map[string]string{"de": "genus4 Echo", "fr": "echo bird", "fi": "Toinen lintu"}},
	}
	a := []string{"Genus1 newmeta", "Genus1 nameonly", "Genus4 echo"}
	b := []string{"Genus2 alias", "Genus1 mammal", "Genus3 a", "Genus3 b", "Genus5 nocommon", "Genus6 dashed", "Soundy", "No row"}
	return s, tax, a, b
}

func TestImportSpecies_Sequences(t *testing.T) {
	var fullReport importReport
	full := func() *state {
		s, tax, a, b := seqFixture()
		fullReport = importSpecies(s, tax, append(append([]string{}, a...), b...), nil, nil)
		return s
	}()
	initial, tax, a, b := seqFixture()

	// One run: spot-check the rules.
	if got := full.meta["Genus1 newmeta"]; got.Taxonomy != (dataset.Taxonomy{Class: "Aves", Order: "Ord", Family: "Fam"}) ||
		got.Links["inaturalist"].ID != "1" || got.Links["wikipedia"].ID != "Q1" {
		t.Errorf("newmeta record = %+v", got)
	}
	if got := full.meta["Genus1 mammal"]; got.Taxonomy != (dataset.Taxonomy{Class: "Mammalia"}) || len(got.Links) != 1 {
		t.Errorf("mammal record (class differs from congener) = %+v", got)
	}
	if got := full.meta["Genus3 b"]; got.Taxonomy != (dataset.Taxonomy{Class: "Aves"}) {
		t.Errorf("record in a new genus = %+v", got)
	}
	if _, ok := full.meta["Genus2 alias"]; !ok {
		t.Errorf("alias-key target got no metadata record")
	}
	for _, sci := range []string{"Genus5 nocommon", "Genus6 dashed"} {
		if got := full.locales["en"][sci]; got != sci {
			t.Errorf("unusable English name of %q became %q, want the scientific name", sci, got)
		}
	}
	if got := full.locales["de"]["Genus5 nocommon"]; got != "Ohne" {
		t.Errorf("de of a species without an English name = %q", got)
	}
	if got := full.locales["de"]["Genus6 dashed"]; got != "Strich" {
		t.Errorf("de of a species with a dashed English name = %q", got)
	}
	if want := []fallback{{"Genus5 nocommon", "no English name"}, {"Genus6 dashed", "English name contains a dash"}}; !reflect.DeepEqual(fullReport.sciFallback, want) {
		t.Errorf("sciFallback = %v, want %v", fullReport.sciFallback, want)
	}
	if _, ok := full.meta["Genus5 nocommon"]; !ok {
		t.Errorf("species with an empty common name got no metadata record")
	}
	if _, ok := full.meta["Soundy"]; ok {
		t.Errorf("sound_class row produced a metadata record")
	}
	if _, ok := full.meta["No row"]; ok {
		t.Errorf("target without a taxonomy row produced a record")
	}
	if _, ok := full.locales["en"]["Genus2 alias"]; ok {
		t.Errorf("alias key got an English name")
	}
	if got := full.locales["en"]["Genus1 nameonly"]; got != "Name Only Bird" {
		t.Errorf("English name = %q", got)
	}
	if got := full.locales["de"]["Genus1 nameonly"]; got != "Alt" {
		t.Errorf("existing de entry replaced by %q", got)
	}
	if got := full.locales["fr"]["Genus1 nameonly"]; got != "Oiseau" {
		t.Errorf("fr = %q", got)
	}
	if got := full.locales["fi"]["Genus1 nameonly"]; got != "harmaa lintu" {
		t.Errorf("fi = %q, want lowercase initial", got)
	}
	if _, ok := full.locales["de"]["Genus4 echo"]; ok {
		t.Errorf("de value equal to the scientific name was written")
	}
	if _, ok := full.locales["fr"]["Genus4 echo"]; ok {
		t.Errorf("fr value equal to the English name was written")
	}
	if got := full.locales["fi"]["Genus4 echo"]; got != "toinen lintu" {
		t.Errorf("fi = %q", got)
	}
	if _, ok := full.locales["it"]; ok {
		t.Errorf("a locale without a data file was created")
	}
	if got := full.locales["de"]["Genus1 newmeta"]; got != "" {
		t.Errorf("names written for a species that already had an English name: %q", got)
	}

	// No existing value changed.
	for k, v := range initial.meta {
		if !reflect.DeepEqual(full.meta[k], v) {
			t.Errorf("existing record %q changed", k)
		}
	}
	for loc, m := range initial.locales {
		for k, v := range m {
			if full.locales[loc][k] != v {
				t.Errorf("existing %s[%q] = %q changed to %q", loc, k, v, full.locales[loc][k])
			}
		}
	}

	// A second run changes nothing.
	again := full.clone()
	if rep := importSpecies(again, tax, append(append([]string{}, a...), b...), nil, nil); len(rep.sciFallback) != 0 {
		t.Errorf("second run reported %v", rep.sciFallback)
	}
	if !reflect.DeepEqual(again, full) {
		t.Errorf("second run changed the state")
	}

	// import(A u B) equals import(A) then import(B), in both orders.
	for _, order := range [][2][]string{{a, b}, {b, a}} {
		s, tax, _, _ := seqFixture()
		importSpecies(s, tax, order[0], nil, nil)
		importSpecies(s, tax, order[1], nil, nil)
		if !reflect.DeepEqual(s, full) {
			t.Errorf("sequenced run %v then %v differs from the union run", order[0], order[1])
		}
	}

	// A run interrupted after the non-English locale files were written is completed by a rerun.
	s, tax, a, b := seqFixture()
	for loc, m := range full.locales {
		if loc != "en" {
			s.locales[loc] = copyMap(m)
		}
	}
	importSpecies(s, tax, append(append([]string{}, a...), b...), nil, nil)
	if !reflect.DeepEqual(s, full) {
		t.Errorf("resumed run differs from the uninterrupted run")
	}
}

// TestLowercaseInitial reads the fixture scripts/test_validate.py also reads, so
// the Go rule and the validator's keeps_capital cannot drift apart.
func TestLowercaseInitial(t *testing.T) {
	var tests []struct{ Name, Key, Value, Want string }
	b, err := os.ReadFile("testdata/lowercase_initial.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &tests); err != nil {
		t.Fatal(err)
	}
	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			if got := lowercaseInitial(tc.Key, tc.Value); got != tc.Want {
				t.Errorf("lowercaseInitial(%q, %q) = %q, want %q", tc.Key, tc.Value, got, tc.Want)
			}
		})
	}
}

func copyMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func TestLocaleStem(t *testing.T) {
	for in, want := range map[string]string{"es_EC": "es_ec", "pt_PT": "pt_pt", "zh-CN": "zh_cn", "de": "de"} {
		if got := localeStem(in); got != want {
			t.Errorf("localeStem(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestImportSpecies_ExcludedCells(t *testing.T) {
	s := newState(nil, map[string]map[string]string{"en": {}, "de": {}, "fr": {}})
	tax := map[string]taxon{
		"Genus1 badlocale":  {sci: "Genus1 badlocale", common: "Locale Bird", group: "Aves", recType: "species", names: map[string]string{"de": "Falsch", "fr": "Oiseau"}},
		"Genus1 badenglish": {sci: "Genus1 badenglish", common: "Wrong Bird", group: "Aves", recType: "species", names: map[string]string{"de": "Vogel"}},
		"Genus1 dashed":     {sci: "Genus1 dashed", common: "Wrong\u2013Bird", group: "Aves", recType: "species", names: map[string]string{"de": "Gestrichelt"}},
		"Genus1 casevar":    {sci: "Genus1 casevar", common: "Case Bird", group: "Aves", recType: "species", names: map[string]string{"de": "FALSCH", "fr": "Oiseau"}},
		"Genus1 stale":      {sci: "Genus1 stale", common: "Fixed Bird", group: "Aves", recType: "species", names: map[string]string{"de": "Neu"}},
	}
	excl := exclusions{
		"de": {
			"Genus1 badlocale": {Value: "Falsch", Reason: "names another species"},
			"Genus1 casevar":   {Value: "falsch", Reason: "names another species"},
			"Genus1 stale":     {Value: "Alt", Reason: "names another species"},
		},
		"en": {
			"Genus1 badenglish": {Value: "Wrong Bird", Reason: "names another animal"},
			"Genus1 dashed":     {Value: "Wrong\u2013Bird", Reason: "names another animal"},
			"Genus1 stale":      {Value: "Broken Bird", Reason: "names another animal"},
		},
	}
	report := importSpecies(s, tax, []string{"Genus1 badlocale", "Genus1 badenglish", "Genus1 dashed", "Genus1 casevar", "Genus1 stale"}, excl, nil)

	if len(report.sciFallback) != 0 {
		t.Errorf("an excluded English cell was reported as a fallback: %v", report.sciFallback)
	}

	if _, ok := s.locales["de"]["Genus1 casevar"]; ok {
		t.Errorf("a cell that differs from the listed value only by case was imported")
	}
	if got := s.locales["fr"]["Genus1 casevar"]; got != "Oiseau" {
		t.Errorf("other locale of the case-variant species = %q", got)
	}
	if got := s.locales["de"]["Genus1 stale"]; got != "Neu" {
		t.Errorf("de cell whose listed value no longer matches = %q, want it imported", got)
	}
	if got := s.locales["en"]["Genus1 stale"]; got != "Fixed Bird" {
		t.Errorf("English cell whose listed value no longer matches = %q, want it imported", got)
	}

	if _, ok := s.locales["de"]["Genus1 badlocale"]; ok {
		t.Errorf("excluded de cell was imported: %q", s.locales["de"]["Genus1 badlocale"])
	}
	if got := s.locales["fr"]["Genus1 badlocale"]; got != "Oiseau" {
		t.Errorf("other locale of the same species = %q, want it imported", got)
	}
	if got := s.locales["en"]["Genus1 badlocale"]; got != "Locale Bird" {
		t.Errorf("English name = %q", got)
	}
	if got := s.locales["en"]["Genus1 badenglish"]; got != "Genus1 badenglish" {
		t.Errorf("excluded English cell = %q, want the scientific name", got)
	}
	if got := s.locales["de"]["Genus1 badenglish"]; got != "Vogel" {
		t.Errorf("locale value of a species with an excluded English cell = %q", got)
	}
	if got := s.locales["en"]["Genus1 dashed"]; got != "Genus1 dashed" {
		t.Errorf("excluded English cell with a dash = %q, want the scientific name", got)
	}
	if got := s.locales["de"]["Genus1 dashed"]; got != "Gestrichelt" {
		t.Errorf("locale value of a species whose excluded English cell has a dash = %q", got)
	}
}

func TestExclusions_Validate(t *testing.T) {
	locales := map[string]map[string]string{"en": {}, "de": {}}
	if err := (exclusions{"de": {"A b": {Value: "x", Reason: "r"}}}).validate(locales); err != nil {
		t.Errorf("known locale: %v", err)
	}
	if err := (exclusions{"fa_IR": {"A b": {Value: "x", Reason: "r"}}}).validate(locales); err == nil {
		t.Errorf("unknown locale accepted")
	}
	if err := (exclusions{"de": nil}).validate(locales); err == nil {
		t.Errorf("null locale list accepted")
	}
	if err := (exclusions{"de": {"A b": {Value: "x"}}}).validate(locales); err == nil {
		t.Errorf("entry without a reason accepted")
	}
	if err := (exclusions{"de": {"A b": {Reason: "r"}}}).validate(locales); err == nil {
		t.Errorf("entry without a value accepted")
	}
	for name, x := range map[string]exclusion{
		"blank value":  {Value: " \t", Reason: "r"},
		"blank reason": {Value: "x", Reason: "  "},
		"padded value": {Value: " x", Reason: "r"},
	} {
		if err := (exclusions{"de": {"A b": x}}).validate(locales); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestLoadExclusions(t *testing.T) {
	excl, err := loadExclusions("../../data/validation/taxonomy_exclusions.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(excl) == 0 {
		t.Fatal("committed exclusion list is empty")
	}
	st, err := loadState("../../data")
	if err != nil {
		t.Fatal(err)
	}
	if err := excl.validate(st.locales); err != nil {
		t.Errorf("committed exclusion list: %v", err)
	}

	bad := filepath.Join(t.TempDir(), "x.json")
	if err := os.WriteFile(bad, []byte(`{"excludes": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadExclusions(bad); err == nil {
		t.Errorf("a file without an exclude object was accepted")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadTaxonomy_LocaleColumns(t *testing.T) {
	// A BOM before a quoted first header, regional English columns, and a
	// locale column listed before its neighbours in no particular order.
	csv := "\xef\xbb\xbf\"scientific_name\",common_name,taxon_group,record_type,inat_id,wikidata_qid,common_name_alt,common_name_en,common_name_en_UK,common_name_en-US,common_name_es_EC,common_name_zh-CN,common_name_de\n" +
		"Genus1 a,Bird,Aves,species,1,Q1,Alt,En,Brit,Yank,Pajaro,Niao,Vogel\n"
	path := filepath.Join(t.TempDir(), "tax.csv")
	writeFile(t, path, csv)
	tax, sum, err := loadTaxonomy(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"es_ec": "Pajaro", "zh_cn": "Niao", "de": "Vogel"}
	if got := tax["Genus1 a"].names; !reflect.DeepEqual(got, want) {
		t.Errorf("names = %v, want %v", got, want)
	}
	if wantSum := sha256.Sum256([]byte(csv)); sum != wantSum {
		t.Errorf("sum is not the sha256 of the file bytes")
	}
}

func TestLoadTaxonomy_Errors(t *testing.T) {
	const hdr = "scientific_name,common_name,taxon_group,record_type,inat_id,wikidata_qid"
	tests := []struct {
		name, csv, want string
	}{
		{"empty file", "", "empty"},
		{"missing required column", "scientific_name,common_name,taxon_group,record_type,inat_id\nA b,c,d,e,f\n", "wikidata_qid"},
		{"two columns for one locale", hdr + ",common_name_es_EC,common_name_es-ec\nA b,c,d,e,f,g,h,i\n", "common_name_es-ec"},
		{"duplicate required header", hdr + ",common_name\nA b,c,d,e,f,g,h\n", "duplicate column"},
		{"duplicate scientific name", hdr + "\nA b,c,d,e,f,g\nA b,c,d,e,f,g\n", "duplicate scientific_name"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "tax.csv")
			writeFile(t, path, tc.csv)
			_, _, err := loadTaxonomy(path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want one containing %q", err, tc.want)
			}
		})
	}
	if _, _, err := loadTaxonomy(filepath.Join(t.TempDir(), "missing.csv")); err == nil {
		t.Errorf("missing file accepted")
	}
}

func TestLoadRenames(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.tsv")
	writeFile(t, good, "# comment\r\n\r\n Geo a \t Mod a \t note\r\nGeo b\tMod b\n")
	got, err := loadRenames(good)
	if err != nil {
		t.Fatal(err)
	}
	if want := []renamePair{{"Geo a", "Mod a"}, {"Geo b", "Mod b"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("pairs = %v, want %v", got, want)
	}

	short := filepath.Join(dir, "short.tsv")
	writeFile(t, short, "Geo a\tMod a\nonlyone\n")
	if _, err := loadRenames(short); err == nil || !strings.Contains(err.Error(), "short.tsv:2:") {
		t.Errorf("one-column line: err = %v, want one naming line 2", err)
	}
	if _, err := loadRenames(filepath.Join(dir, "missing.tsv")); err == nil {
		t.Errorf("missing file accepted")
	}
}

func TestLoadTargets(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "B b\r\n\nA a\n")
	writeFile(t, filepath.Join(dir, "b.txt"), " A a \nC c")
	got, err := loadTargets(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"A a", "B b", "C c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("targets = %v, want %v", got, want)
	}
	if _, err := loadTargets(t.TempDir()); err == nil {
		t.Errorf("directory without manifests accepted")
	}
}

func TestExclusions_Mismatches(t *testing.T) {
	tax := map[string]taxon{
		"A b": {sci: "A b", common: "Real Bird", names: map[string]string{"de": "Vogel", "sv": "gammal"}},
		"C d": {sci: "C d", common: "", names: map[string]string{}},
	}
	tests := []struct {
		name string
		excl exclusions
		want int
	}{
		{"equal ignoring case", exclusions{"sv": {"A b": {Value: "GAMMAL", Reason: "r"}}}, 0},
		{"different non-empty cell", exclusions{"de": {"A b": {Value: "Falsch", Reason: "r"}}}, 1},
		{"different English cell", exclusions{"en": {"A b": {Value: "Wrong Bird", Reason: "r"}}}, 1},
		{"empty upstream cell", exclusions{"de": {"C d": {Value: "x", Reason: "r"}}, "en": {"C d": {Value: "x", Reason: "r"}}}, 0},
		{"no row", exclusions{"de": {"E f": {Value: "x", Reason: "r"}}}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.excl.mismatches(tax); len(got) != tc.want {
				t.Errorf("mismatches = %q, want %d", got, tc.want)
			}
		})
	}
}

// runFixture writes a small data directory, taxonomy, manifest, rename file and
// exclusion list, and returns the arguments of run.
type runFixture struct {
	taxonomy, labels, exclusions, zwnj, data string
	renames                                  []string
}

func newRunFixture(t *testing.T) runFixture {
	t.Helper()
	d := t.TempDir()
	f := runFixture{
		taxonomy:   filepath.Join(d, "tax.csv"),
		labels:     filepath.Join(d, "labels"),
		exclusions: filepath.Join(d, "excl.json"),
		zwnj:       filepath.Join(d, "zwnj.json"),
		data:       filepath.Join(d, "data"),
		renames:    []string{filepath.Join(d, "renames.tsv")},
	}
	writeFile(t, f.taxonomy, "scientific_name,common_name,taxon_group,record_type,inat_id,wikidata_qid,common_name_de\n"+
		"Genus1 new,New Bird,Aves,species,1,Q1,Neuer Vogel\n"+
		"Genus1 old,Old Bird,Aves,species,2,Q2,Alter Vogel\n"+
		"Genus1 blank,,Aves,species,3,Q3,Leerer Vogel\n")
	writeFile(t, filepath.Join(f.labels, "m.txt"), "Genus1 new\nGenus1 blank\n")
	writeFile(t, f.renames[0], "Genus1 old\tGenus1 oldish\n")
	writeFile(t, f.exclusions, `{"exclude": {"de": {"Genus1 old": {"value": "x", "reason": "r"}}}}`)
	writeFile(t, f.zwnj, `{"tokens": {"`+bareBrown+`": "`+joinedBrown+`"}}`)
	writeFile(t, filepath.Join(f.data, "metadata.json"), "{}\n")
	writeFile(t, filepath.Join(f.data, "aliases.json"), "{}\n")
	writeFile(t, filepath.Join(f.data, "locales", "en.json"), "{\n  \"Genus1 old\": \"Old Bird\"\n}\n")
	// Cells under the alias key of the rename pair.
	writeFile(t, filepath.Join(f.data, "locales", "de.json"), "{\n  \"Genus1 oldish\": \"Alt\"\n}\n")
	writeFile(t, filepath.Join(f.data, "locales", "fa.json"), "{\n  \"Genus1 oldish\": \""+bareBrown+"\"\n}\n")
	return f
}

func (f runFixture) run(dry bool) error {
	return run(f.taxonomy, f.renames, f.data, f.labels, f.exclusions, f.zwnj, dry)
}

func readTree(t *testing.T, root string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		rel, _ := filepath.Rel(root, p)
		tree[rel] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// captureStdout returns what fn prints to os.Stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	os.Stdout = old
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return <-done
}

func TestRun_DryRunThenWriteThenRerun(t *testing.T) {
	f := newRunFixture(t)
	before := readTree(t, f.data)
	var runErr error
	out := captureStdout(t, func() { runErr = f.run(true) })
	if runErr != nil {
		t.Fatal(runErr)
	}
	for _, want := range []string{
		"English name set to the scientific name (no English name): Genus1 blank",
		"exclusion does not match the taxonomy, update or remove it: de \"Genus1 old\"",
		"alias cells merged: 2 (0 equal, 0 case only, 2 moved,",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if !reflect.DeepEqual(readTree(t, f.data), before) {
		t.Fatalf("dry run changed the data directory")
	}

	if err := f.run(false); err != nil {
		t.Fatal(err)
	}
	st, err := loadState(f.data)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.meta["Genus1 new"]; !ok {
		t.Errorf("no metadata record for the new species")
	}
	if got := st.locales["en"]["Genus1 new"]; got != "New Bird" {
		t.Errorf("en = %q", got)
	}
	if got := st.locales["de"]["Genus1 new"]; got != "Neuer Vogel" {
		t.Errorf("de = %q", got)
	}
	if st.aliases["Genus1 oldish"] != "Genus1 old" {
		t.Errorf("aliases = %v", st.aliases)
	}
	if got := st.locales["de"]; !reflect.DeepEqual(got, map[string]string{"Genus1 old": "Alt", "Genus1 new": "Neuer Vogel", "Genus1 blank": "Leerer Vogel"}) {
		t.Errorf("de = %v, want the alias cell moved under the canonical key", got)
	}
	if got := st.locales["fa"]; !reflect.DeepEqual(got, map[string]string{"Genus1 old": joinedBrown}) {
		t.Errorf("fa = %v, want the alias cell moved in its joined form", got)
	}
	after := readTree(t, f.data)
	for p := range after {
		if strings.HasSuffix(p, ".tmp") {
			t.Errorf("leftover %s", p)
		}
	}

	if err := f.run(false); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(readTree(t, f.data), after) {
		t.Errorf("second run changed the data directory")
	}
}

func TestRun_InterruptedWriteIsCompletedByRerun(t *testing.T) {
	ref := newRunFixture(t)
	if err := ref.run(false); err != nil {
		t.Fatal(err)
	}
	want := readTree(t, ref.data)
	f := newRunFixture(t)
	before := readTree(t, f.data)
	obstacle := filepath.Join(f.data, "metadata.json.tmp")
	if err := os.Mkdir(obstacle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := f.run(false); err == nil {
		t.Fatal("run succeeded despite the obstacle")
	}
	mid := readTree(t, f.data)
	if mid["locales/de.json"] == before["locales/de.json"] {
		t.Errorf("de.json was not written before the failure")
	}
	for _, name := range []string{"locales/en.json", "aliases.json"} {
		if mid[name] != before[name] {
			t.Errorf("%s was written before metadata.json", name)
		}
	}
	if err := os.Remove(obstacle); err != nil {
		t.Fatal(err)
	}
	if err := f.run(false); err != nil {
		t.Fatal(err)
	}
	if got := readTree(t, f.data); !reflect.DeepEqual(got, want) {
		t.Errorf("rerun after an interrupted run differs from an uninterrupted run")
	}
}

func TestImportSpecies_NormalizesPersian(t *testing.T) {
	s := newState(nil, map[string]map[string]string{"en": {}, "de": {}, "fa": {}})
	tax := map[string]taxon{
		"Genus1 one": {sci: "Genus1 one", common: "One", group: "Aves", recType: "species",
			names: map[string]string{"fa": bareBrown, "de": bareBrown}},
		"Genus1 two": {sci: "Genus1 two", common: "Two", group: "Aves", recType: "species",
			names: map[string]string{"fa": "\u200c"}},
	}
	rep := importSpecies(s, tax, []string{"Genus1 one", "Genus1 two"}, nil, zwnjTokens{bareBrown: joinedBrown})
	if got := s.locales["fa"]["Genus1 one"]; got != joinedBrown {
		t.Errorf("fa = %q, want %q", got, joinedBrown)
	}
	if got := s.locales["de"]["Genus1 one"]; got != bareBrown {
		t.Errorf("de = %q, want it untouched", got)
	}
	if v, ok := s.locales["fa"]["Genus1 two"]; ok {
		t.Errorf("fa cell written for a name that is only U+200C: %q", v)
	}
	if rep.localeValues != 2 {
		t.Errorf("localeValues = %d, want 2", rep.localeValues)
	}
}
