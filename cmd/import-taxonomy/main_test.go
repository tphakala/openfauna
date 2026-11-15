package main

import (
	"os"
	"path/filepath"
	"reflect"
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

func mergeFixture() *state {
	return newState(nil, map[string]map[string]string{
		"en": {"Poo hyp": "Warbling-Finch", "Poo hypo": "Warbling Finch"},
		"a":  {"Poo hyp": "Warbling-Finch", "Poo hypo": "Real A"}, // canonical echo: duplicate wins
		"b":  {"Poo hyp": "Real B1", "Poo hypo": "Real B2"},       // both real: canonical wins
		"c":  {"Poo hypo": "Real C"},                              // only the duplicate: moved
		"d":  {"Poo hyp": "Real D"},                               // only the canonical: untouched
		"e":  {"Poo hyp": "Warbling Finch", "Poo hypo": "Real E"}, // canonical echoes the duplicate's English name
		"f":  {"Poo hyp": "Real F", "Poo hypo": "Warbling-Finch"}, // duplicate is an echo: canonical stays
	})
}

func TestApplyRenames_MergesFullEntry(t *testing.T) {
	want := map[string]map[string]string{
		"en": {"Poo hyp": "Warbling-Finch"},
		"a":  {"Poo hyp": "Real A"},
		"b":  {"Poo hyp": "Real B1"},
		"c":  {"Poo hyp": "Real C"},
		"d":  {"Poo hyp": "Real D"},
		"e":  {"Poo hyp": "Real E"},
		"f":  {"Poo hyp": "Real F"},
	}
	pairs := []renamePair{{"Poo hyp", "Poo hypo"}}

	s := mergeFixture()
	if err := applyRenames(s, pairs); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.locales, want) {
		t.Errorf("locales = %v, want %v", s.locales, want)
	}
	if s.aliases["Poo hypo"] != "Poo hyp" {
		t.Errorf("alias not set: %v", s.aliases)
	}

	// State after an interrupted run: the alias exists, the duplicate is still a full entry.
	s = mergeFixture()
	s.aliases["Poo hypo"] = "Poo hyp"
	if err := applyRenames(s, pairs); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.locales, want) {
		t.Errorf("resumed locales = %v, want %v", s.locales, want)
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
		"Genus4 echo": {sci: "Genus4 echo", common: "Echo Bird", group: "Aves", recType: "species",
			names: map[string]string{"de": "Genus4 echo", "fr": "echo bird", "fi": "Toinen lintu"}},
	}
	a := []string{"Genus1 newmeta", "Genus1 nameonly", "Genus4 echo"}
	b := []string{"Genus2 alias", "Genus1 mammal", "Genus3 a", "Genus3 b", "Genus5 nocommon", "Soundy", "No row"}
	return s, tax, a, b
}

func TestImportSpecies_Sequences(t *testing.T) {
	full := func() *state {
		s, tax, a, b := seqFixture()
		importSpecies(s, tax, append(append([]string{}, a...), b...), nil)
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
	if _, ok := full.locales["en"]["Genus5 nocommon"]; ok {
		t.Errorf("an empty English name was written")
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
	importSpecies(again, tax, append(append([]string{}, a...), b...), nil)
	if !reflect.DeepEqual(again, full) {
		t.Errorf("second run changed the state")
	}

	// import(A u B) equals import(A) then import(B), in both orders.
	for _, order := range [][2][]string{{a, b}, {b, a}} {
		s, tax, _, _ := seqFixture()
		importSpecies(s, tax, order[0], nil)
		importSpecies(s, tax, order[1], nil)
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
	importSpecies(s, tax, append(append([]string{}, a...), b...), nil)
	if !reflect.DeepEqual(s, full) {
		t.Errorf("resumed run differs from the uninterrupted run")
	}
}

func TestLowercaseInitial(t *testing.T) {
	tests := []struct{ name, key, value, want string }{
		{"plain capitalized name is lowered", "Pica pica", "Harmaa lintu", "harmaa lintu"},
		{"eponym keeps its capital", "Pica pica", "Stellers sjolejon", "Stellers sjolejon"},
		{"synonym sharing the key's epithet", "Pica pica", "Garrulus pica", "Garrulus pica"},
		{"value in the key's genus", "Osteopilus ocellatus", "Osteopilus brunneus", "Osteopilus brunneus"},
		{"first letter follows a non-letter", "Pica pica", "“Harmaa” lintu", "“harmaa” lintu"},
		{"already lowercase", "Pica pica", "harmaa lintu", "harmaa lintu"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := lowercaseInitial(tc.key, tc.value); got != tc.want {
				t.Errorf("lowercaseInitial(%q, %q) = %q, want %q", tc.key, tc.value, got, tc.want)
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

func TestApplyRenames_ErrorAfterMergeRestoresLocales(t *testing.T) {
	s := mergeFixture()
	err := applyRenames(s, []renamePair{{"Poo hyp", "Poo hypo"}, {"Nowhere", "Mod"}})
	if err == nil {
		t.Fatal("want an error from the second row")
	}
	if want := mergeFixture(); !reflect.DeepEqual(s, want) {
		t.Errorf("locales changed by a run that failed after a merge: %v", s.locales)
	}
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
	}
	excl := exclusions{
		"de": {"Genus1 badlocale": {Value: "Falsch", Reason: "names another species"}},
		"en": {"Genus1 badenglish": {Value: "Wrong Bird", Reason: "names another animal"}},
	}
	importSpecies(s, tax, []string{"Genus1 badlocale", "Genus1 badenglish"}, excl)

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
}

func TestExclusions_Validate(t *testing.T) {
	locales := map[string]map[string]string{"en": {}, "de": {}}
	if err := (exclusions{"de": {"A b": {Value: "x", Reason: "r"}}}).validate(locales); err != nil {
		t.Errorf("known locale: %v", err)
	}
	if err := (exclusions{"fa_IR": {"A b": {Value: "x", Reason: "r"}}}).validate(locales); err == nil {
		t.Errorf("unknown locale accepted")
	}
	if err := (exclusions{"de": {"A b": {Value: "x"}}}).validate(locales); err == nil {
		t.Errorf("entry without a reason accepted")
	}
	if err := (exclusions{"de": {"A b": {Reason: "r"}}}).validate(locales); err == nil {
		t.Errorf("entry without a value accepted")
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

func TestLoadTaxonomy_LocaleColumns(t *testing.T) {
	csv := "scientific_name,common_name,taxon_group,record_type,inat_id,wikidata_qid,common_name_alt,common_name_en,common_name_es_EC,common_name_zh-CN,common_name_de\n" +
		"Genus1 a,Bird,Aves,species,1,Q1,Alt,En,Pajaro,Niao,Vogel\n"
	path := filepath.Join(t.TempDir(), "tax.csv")
	if err := os.WriteFile(path, []byte(csv), 0o644); err != nil {
		t.Fatal(err)
	}
	tax, _, err := loadTaxonomy(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"es_ec": "Pajaro", "zh_cn": "Niao", "de": "Vogel"}
	if got := tax["Genus1 a"].names; !reflect.DeepEqual(got, want) {
		t.Errorf("names = %v, want %v", got, want)
	}
}
