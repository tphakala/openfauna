package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tphakala/openfauna/internal/dataset"
	"github.com/tphakala/openfauna/internal/fetch"
)

func exact(class, order, family string) match {
	return match{UsageKey: 1, CanonicalName: "X y", Rank: "SPECIES", Status: "ACCEPTED", MatchType: "EXACT",
		Kingdom: "Animalia", Class: class, Order: order, Family: family}
}

func TestDecide(t *testing.T) {
	with := func(m match, f func(*match)) match { f(&m); return m }
	tests := []struct {
		name       string
		in         dataset.Taxonomy
		m          match
		want       dataset.Taxonomy
		wantReason string
	}{
		{"accepted fills both", dataset.Taxonomy{Class: "Aves"}, exact("Aves", "Passeriformes", "Corvidae"),
			dataset.Taxonomy{Class: "Aves", Order: "Passeriformes", Family: "Corvidae"}, ""},
		{"synonym fills", dataset.Taxonomy{Class: "Aves"},
			with(exact("Aves", "Passeriformes", "Corvidae"), func(m *match) { m.Status = "SYNONYM" }),
			dataset.Taxonomy{Class: "Aves", Order: "Passeriformes", Family: "Corvidae"}, ""},
		{"doubtful status rejected", dataset.Taxonomy{Class: "Aves"},
			with(exact("Aves", "O", "F"), func(m *match) { m.Status = "DOUBTFUL" }),
			dataset.Taxonomy{Class: "Aves"}, "status"},
		{"fuzzy rejected", dataset.Taxonomy{Class: "Aves"},
			with(exact("Aves", "O", "F"), func(m *match) { m.MatchType = "FUZZY" }),
			dataset.Taxonomy{Class: "Aves"}, "not_exact"},
		{"none", dataset.Taxonomy{Class: "Aves"}, match{MatchType: "NONE"},
			dataset.Taxonomy{Class: "Aves"}, "no_match"},
		{"genus rank rejected", dataset.Taxonomy{Class: "Aves"},
			with(exact("Aves", "O", "F"), func(m *match) { m.Rank = "GENUS" }),
			dataset.Taxonomy{Class: "Aves"}, "not_species"},
		{"plant kingdom rejected", dataset.Taxonomy{Class: "Aves"},
			with(exact("Aves", "O", "F"), func(m *match) { m.Kingdom = "Plantae" }),
			dataset.Taxonomy{Class: "Aves"}, "not_animalia"},
		{"class mismatch", dataset.Taxonomy{Class: "Insecta"}, exact("Aves", "O", "F"),
			dataset.Taxonomy{Class: "Insecta"}, "class_mismatch"},
		{"reptile order in class slot", dataset.Taxonomy{Class: "Reptilia"}, exact("Squamata", "", "Colubridae"),
			dataset.Taxonomy{Class: "Reptilia", Order: "Squamata", Family: "Colubridae"}, ""},
		{"reptile class slot with order present", dataset.Taxonomy{Class: "Reptilia"}, exact("Squamata", "X", "Colubridae"),
			dataset.Taxonomy{Class: "Reptilia"}, "class_mismatch"},
		{"reptile class slot Aves", dataset.Taxonomy{Class: "Reptilia"}, exact("Aves", "", "F"),
			dataset.Taxonomy{Class: "Reptilia"}, "class_mismatch"},
		{"fish with null class in Chordata", dataset.Taxonomy{Class: "Actinopterygii"},
			with(exact("", "Perciformes", "Percidae"), func(m *match) { m.Phylum = "Chordata" }),
			dataset.Taxonomy{Class: "Actinopterygii", Order: "Perciformes", Family: "Percidae"}, ""},
		{"fish with null class in Arthropoda", dataset.Taxonomy{Class: "Actinopterygii"},
			with(exact("", "O", "F"), func(m *match) { m.Phylum = "Arthropoda" }),
			dataset.Taxonomy{Class: "Actinopterygii"}, "class_mismatch"},
		{"bird with null class", dataset.Taxonomy{Class: "Aves"},
			with(exact("", "O", "F"), func(m *match) { m.Phylum = "Chordata" }),
			dataset.Taxonomy{Class: "Aves"}, "class_mismatch"},
		{"partial with agreeing order gets family", dataset.Taxonomy{Class: "Aves", Order: "Passeriformes"},
			exact("Aves", "Passeriformes", "Corvidae"),
			dataset.Taxonomy{Class: "Aves", Order: "Passeriformes", Family: "Corvidae"}, ""},
		{"partial with differing order", dataset.Taxonomy{Class: "Aves", Order: "Strigiformes"},
			exact("Aves", "Passeriformes", "Corvidae"),
			dataset.Taxonomy{Class: "Aves", Order: "Strigiformes"}, "order_conflict"},
		{"partial with differing family", dataset.Taxonomy{Class: "Aves", Family: "Strigidae"},
			exact("Aves", "Passeriformes", "Corvidae"),
			dataset.Taxonomy{Class: "Aves", Family: "Strigidae"}, "family_conflict"},
		{"gbif family empty", dataset.Taxonomy{Class: "Aves"}, exact("Aves", "Passeriformes", ""),
			dataset.Taxonomy{Class: "Aves", Order: "Passeriformes"}, "incomplete"},
		{"family_common is kept", dataset.Taxonomy{Class: "Aves", FamilyCommon: "Crows"},
			exact("Aves", "Passeriformes", "Corvidae"),
			dataset.Taxonomy{Class: "Aves", Order: "Passeriformes", Family: "Corvidae", FamilyCommon: "Crows"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := decide(tc.in, tc.m)
			if got != tc.want || reason != tc.wantReason {
				t.Errorf("decide = %+v, %q; want %+v, %q", got, reason, tc.want, tc.wantReason)
			}
		})
	}
}

func TestAliasFill(t *testing.T) {
	complete := dataset.Taxonomy{Class: "Aves", Order: "Accipitriformes", Family: "Accipitridae"}
	classOnly := dataset.Taxonomy{Class: "Aves"}
	rec := func(t dataset.Taxonomy) dataset.SpeciesRecord { return dataset.SpeciesRecord{Taxonomy: t} }
	tests := []struct {
		name    string
		meta    map[string]dataset.SpeciesRecord
		aliases map[string]string
		want    dataset.Taxonomy
		wantOK  bool
	}{
		{"record is the alias key", map[string]dataset.SpeciesRecord{"New": rec(classOnly), "Old": rec(complete)},
			map[string]string{"New": "Old"}, complete, true},
		{"record is the alias value", map[string]dataset.SpeciesRecord{"New": rec(classOnly), "Old": rec(complete)},
			map[string]string{"Old": "New"}, complete, true},
		{"different class", map[string]dataset.SpeciesRecord{"New": rec(dataset.Taxonomy{Class: "Mammalia"}), "Old": rec(complete)},
			map[string]string{"Old": "New"}, dataset.Taxonomy{}, false},
		{"partner incomplete", map[string]dataset.SpeciesRecord{"New": rec(classOnly), "Old": rec(dataset.Taxonomy{Class: "Aves", Order: "O"})},
			map[string]string{"Old": "New"}, dataset.Taxonomy{}, false},
		{"partner without a record", map[string]dataset.SpeciesRecord{"New": rec(classOnly)},
			map[string]string{"Old": "New"}, dataset.Taxonomy{}, false},
		{"partial keeps its order and takes the family",
			map[string]dataset.SpeciesRecord{"New": rec(dataset.Taxonomy{Class: "Aves", Order: "Accipitriformes"}), "Old": rec(complete)},
			map[string]string{"Old": "New"}, complete, true},
		{"partner order differs from the record's",
			map[string]dataset.SpeciesRecord{"New": rec(dataset.Taxonomy{Class: "Aves", Order: "Strigiformes"}), "Old": rec(complete)},
			map[string]string{"Old": "New"}, dataset.Taxonomy{}, false},
		{"partners disagree",
			map[string]dataset.SpeciesRecord{"New": rec(classOnly), "A": rec(complete),
				"B": rec(dataset.Taxonomy{Class: "Aves", Order: "Falconiformes", Family: "Falconidae"})},
			map[string]string{"A": "New", "B": "New"}, dataset.Taxonomy{}, false},
		{"partners agree",
			map[string]dataset.SpeciesRecord{"New": rec(classOnly), "A": rec(complete), "B": rec(complete)},
			map[string]string{"A": "New", "B": "New"}, complete, true},
		{"family_common is kept",
			map[string]dataset.SpeciesRecord{"New": rec(dataset.Taxonomy{Class: "Aves", FamilyCommon: "Hawks"}), "Old": rec(complete)},
			map[string]string{"Old": "New"},
			dataset.Taxonomy{Class: "Aves", Order: "Accipitriformes", Family: "Accipitridae", FamilyCommon: "Hawks"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := aliasFill(tc.meta, buildPartners(tc.aliases), "New")
			if ok != tc.wantOK || (ok && got != tc.want) {
				t.Errorf("aliasFill = %+v, %v; want %+v, %v", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

// fakeGBIF serves canned matches keyed by the name query parameter and records
// what it was asked.
type fakeGBIF struct {
	mu      sync.Mutex
	matches map[string]match
	failOn  string
	names   []string
	strict  []string
	uas     []string
}

func (f *fakeGBIF) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	name := r.URL.Query().Get("name")
	f.names = append(f.names, name)
	f.strict = append(f.strict, r.URL.Query().Get("strict"))
	f.uas = append(f.uas, r.Header.Get("User-Agent"))
	if name == f.failOn {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	m, ok := f.matches[name]
	if !ok {
		m = match{MatchType: "NONE"}
	}
	_ = json.NewEncoder(w).Encode(m)
}

func (f *fakeGBIF) take() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.names
	f.names = nil
	return n
}

type env struct {
	cfg config
	srv *fakeGBIF
	cl  *fetch.Client
}

func newEnv(t *testing.T, meta map[string]dataset.SpeciesRecord, aliases map[string]string, matches map[string]match) *env {
	t.Helper()
	dir := t.TempDir()
	f := &fakeGBIF{matches: matches}
	ts := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(ts.Close)
	e := &env{srv: f, cfg: config{
		metadataPath: filepath.Join(dir, "metadata.json"),
		aliasesPath:  filepath.Join(dir, "aliases.json"),
		reportPath:   filepath.Join(dir, "report.json"),
		api:          ts.URL,
	}, cl: &fetch.Client{HTTP: ts.Client(), UserAgent: "test-agent/1", Ctx: context.Background(),
		Backoff: func(int) time.Duration { return 0 }}}
	e.writeMeta(t, meta)
	buf, err := dataset.MarshalJSON(aliases)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.cfg.aliasesPath, buf, 0o644); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) writeMeta(t *testing.T, meta map[string]dataset.SpeciesRecord) {
	t.Helper()
	buf, err := dataset.MarshalJSON(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.cfg.metadataPath, buf, 0o644); err != nil {
		t.Fatal(err)
	}
}

func (e *env) meta(t *testing.T) map[string]dataset.SpeciesRecord {
	t.Helper()
	m, err := dataset.LoadMetadata(e.cfg.metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func (e *env) bytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	return b
}

func (e *env) run(t *testing.T) {
	t.Helper()
	if err := execute(e.cfg, e.cl); err != nil {
		t.Fatalf("execute: %v", err)
	}
}

// checkCanonical fails unless the report equals its own decode and re-encode.
func checkCanonical(t *testing.T, b []byte) {
	t.Helper()
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("report is not JSON: %v", err)
	}
	again, err := dataset.MarshalJSON(v)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b, again) {
		t.Errorf("report is not in canonical form:\n%s", b)
	}
}

func seqFixture() (map[string]dataset.SpeciesRecord, map[string]string, map[string]match) {
	meta := map[string]dataset.SpeciesRecord{
		"Aaa complete": {
			Taxonomy: dataset.Taxonomy{Class: "Aves", Order: "Ord", Family: "Fam", FamilyCommon: "Common"},
			Links:    map[string]dataset.Link{"wikipedia": {ID: "Q1"}},
			Media:    []dataset.MediaItem{{Source: "commons", ID: "f.jpg", License: "CC0"}},
		},
		"Bbb noclass":   {},
		"Ccc match":     {Taxonomy: dataset.Taxonomy{Class: "Mammalia"}},
		"Ddd none":      {Taxonomy: dataset.Taxonomy{Class: "Aves"}},
		"Eee partial":   {Taxonomy: dataset.Taxonomy{Class: "Aves", Order: "Passeriformes"}},
		"Fff aliased":   {Taxonomy: dataset.Taxonomy{Class: "Aves"}},
		"Ggg canon":     {Taxonomy: dataset.Taxonomy{Class: "Aves", Order: "Falconiformes", Family: "Falconidae"}},
		"Hhh unrelated": {Taxonomy: dataset.Taxonomy{Class: "Aves", Order: "O2", Family: "F2"}},
	}
	aliases := map[string]string{"_comment": "x", "Fff aliased": "Ggg canon"}
	matches := map[string]match{
		"Ccc match":   exact("Mammalia", "Carnivora", "Felidae"),
		"Eee partial": exact("Aves", "Passeriformes", "Corvidae"),
		"Iii new":     exact("Insecta", "Diptera", "Culicidae"),
	}
	return meta, aliases, matches
}

func TestExecute_Sequences(t *testing.T) {
	meta, aliases, matches := seqFixture()
	e := newEnv(t, meta, aliases, matches)

	// 1. First run.
	e.run(t)
	if got, want := e.srv.take(), []string{"Ccc match", "Ddd none", "Eee partial"}; !slices.Equal(got, want) {
		t.Fatalf("requested %v, want %v (alias-filled and complete records need no request)", got, want)
	}
	for _, s := range e.srv.strict {
		if s != "true" {
			t.Errorf("strict = %q", s)
		}
	}
	for _, ua := range e.srv.uas {
		if ua != "test-agent/1" {
			t.Errorf("User-Agent = %q", ua)
		}
	}
	got := e.meta(t)
	for _, name := range []string{"Aaa complete", "Bbb noclass", "Ddd none", "Ggg canon", "Hhh unrelated"} {
		if !reflect.DeepEqual(got[name], meta[name]) {
			t.Errorf("%s changed: %+v", name, got[name])
		}
	}
	if g := got["Ccc match"].Taxonomy; g != (dataset.Taxonomy{Class: "Mammalia", Order: "Carnivora", Family: "Felidae"}) {
		t.Errorf("Ccc match = %+v", g)
	}
	if g := got["Eee partial"].Taxonomy; g != (dataset.Taxonomy{Class: "Aves", Order: "Passeriformes", Family: "Corvidae"}) {
		t.Errorf("Eee partial = %+v", g)
	}
	if g := got["Fff aliased"].Taxonomy; g != (dataset.Taxonomy{Class: "Aves", Order: "Falconiformes", Family: "Falconidae"}) {
		t.Errorf("Fff aliased = %+v", g)
	}
	rep1 := e.bytes(t, e.cfg.reportPath)
	checkCanonical(t, rep1)
	var r report
	if err := json.Unmarshal(rep1, &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Rejects) != 1 || r.Rejects[0].Name != "Ddd none" || r.Rejects[0].Reason != "no_match" ||
		r.Rejects[0].Class != "Aves" || r.Rejects[0].GBIF != nil {
		t.Errorf("rejects = %+v", r.Rejects)
	}
	meta1 := e.bytes(t, e.cfg.metadataPath)

	// 2. Rerun: only the unfilled name is requested, bytes are stable.
	e.run(t)
	if got, want := e.srv.take(), []string{"Ddd none"}; !slices.Equal(got, want) {
		t.Errorf("rerun requested %v, want %v", got, want)
	}
	if !bytes.Equal(meta1, e.bytes(t, e.cfg.metadataPath)) || !bytes.Equal(rep1, e.bytes(t, e.cfg.reportPath)) {
		t.Errorf("rerun changed the output")
	}

	// 3. A class-only record added later is filled, the rest is untouched, and
	// the state equals a single run over the full set.
	later := e.meta(t)
	later["Iii new"] = dataset.SpeciesRecord{Taxonomy: dataset.Taxonomy{Class: "Insecta"}}
	e.writeMeta(t, later)
	e.run(t)
	e.srv.take()
	after := e.meta(t)
	if g := after["Iii new"].Taxonomy; g != (dataset.Taxonomy{Class: "Insecta", Order: "Diptera", Family: "Culicidae"}) {
		t.Errorf("Iii new = %+v", g)
	}
	for name, rec := range got {
		if !reflect.DeepEqual(after[name], rec) {
			t.Errorf("%s changed when another record was added", name)
		}
	}
	full, _, _ := seqFixture()
	full["Iii new"] = dataset.SpeciesRecord{Taxonomy: dataset.Taxonomy{Class: "Insecta"}}
	single := newEnv(t, full, aliases, matches)
	single.run(t)
	if !bytes.Equal(e.bytes(t, e.cfg.metadataPath), single.bytes(t, single.cfg.metadataPath)) ||
		!bytes.Equal(e.bytes(t, e.cfg.reportPath), single.bytes(t, single.cfg.reportPath)) {
		t.Errorf("incremental runs differ from a single run")
	}

	// 4. Removing the unmatched record empties the report.
	pruned := e.meta(t)
	delete(pruned, "Ddd none")
	e.writeMeta(t, pruned)
	e.run(t)
	rep := e.bytes(t, e.cfg.reportPath)
	checkCanonical(t, rep)
	if !strings.Contains(string(rep), `"rejects": []`) {
		t.Errorf("report after removal = %s", rep)
	}
}

func TestExecute_AliasPartnerFilledByGBIF(t *testing.T) {
	cls := dataset.Taxonomy{Class: "Aves"}
	meta := map[string]dataset.SpeciesRecord{"Old name": {Taxonomy: cls}, "New name": {Taxonomy: cls}}
	e := newEnv(t, meta, map[string]string{"Old name": "New name"},
		map[string]match{"Old name": exact("Aves", "Passeriformes", "Corvidae")})
	e.run(t)
	want := dataset.Taxonomy{Class: "Aves", Order: "Passeriformes", Family: "Corvidae"}
	got := e.meta(t)
	if got["Old name"].Taxonomy != want || got["New name"].Taxonomy != want {
		t.Errorf("got %+v and %+v", got["Old name"].Taxonomy, got["New name"].Taxonomy)
	}
	if !strings.Contains(string(e.bytes(t, e.cfg.reportPath)), `"rejects": []`) {
		t.Errorf("report should be empty: %s", e.bytes(t, e.cfg.reportPath))
	}
	e.srv.take()
	e.run(t)
	if n := e.srv.take(); len(n) != 0 {
		t.Errorf("rerun requested %v", n)
	}
}

func TestExecute_NoWriteModes(t *testing.T) {
	tests := []struct {
		name         string
		mutate       func(*config)
		wantChanged  int // records whose taxonomy differs from the input
		wantRequests int
	}{
		// The alias record is filled by the network-free pass in every mode that writes.
		{"dry run", func(c *config) { c.dryRun = true }, 0, 3},
		{"limit one", func(c *config) { c.limit = 1 }, 2, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			meta, aliases, matches := seqFixture()
			e := newEnv(t, meta, aliases, matches)
			sentinel := []byte("sentinel report\n")
			if err := os.WriteFile(e.cfg.reportPath, sentinel, 0o644); err != nil {
				t.Fatal(err)
			}
			before := e.bytes(t, e.cfg.metadataPath)
			tc.mutate(&e.cfg)
			e.run(t)
			if n := len(e.srv.take()); n != tc.wantRequests {
				t.Errorf("requests = %d, want %d", n, tc.wantRequests)
			}
			if !bytes.Equal(e.bytes(t, e.cfg.reportPath), sentinel) {
				t.Errorf("report was rewritten")
			}
			changed := 0
			after := e.meta(t)
			for name, rec := range after {
				if rec.Taxonomy != meta[name].Taxonomy {
					changed++
				}
			}
			if changed != tc.wantChanged {
				t.Errorf("changed records = %d, want %d", changed, tc.wantChanged)
			}
			if tc.wantChanged == 0 && !bytes.Equal(before, e.bytes(t, e.cfg.metadataPath)) {
				t.Errorf("dry run wrote metadata")
			}
			if tc.wantChanged > 0 && after["Ccc match"].Taxonomy.Order == "" {
				t.Errorf("first sorted name was not filled")
			}
		})
	}
}

func TestExecute_FetchErrorWritesNothing(t *testing.T) {
	meta, aliases, matches := seqFixture()
	e := newEnv(t, meta, aliases, matches)
	e.srv.failOn = "Ddd none"
	sentinel := []byte("sentinel report\n")
	if err := os.WriteFile(e.cfg.reportPath, sentinel, 0o644); err != nil {
		t.Fatal(err)
	}
	before := e.bytes(t, e.cfg.metadataPath)
	err := execute(e.cfg, e.cl)
	if err == nil || !strings.Contains(err.Error(), "Ddd none") {
		t.Fatalf("error = %v, want one naming Ddd none", err)
	}
	if !bytes.Equal(before, e.bytes(t, e.cfg.metadataPath)) || !bytes.Equal(sentinel, e.bytes(t, e.cfg.reportPath)) {
		t.Errorf("files changed after a failed run")
	}
}

func TestExecute_RejectReportCarriesGBIFMatch(t *testing.T) {
	meta := map[string]dataset.SpeciesRecord{
		"Aaa fuzzy":    {Taxonomy: dataset.Taxonomy{Class: "Aves"}},
		"Bbb mismatch": {Taxonomy: dataset.Taxonomy{Class: "Insecta"}},
	}
	fuzzy := exact("Aves", "Passeriformes", "Corvidae")
	fuzzy.MatchType, fuzzy.CanonicalName, fuzzy.UsageKey = "FUZZY", "Aaa fuzzi", 42
	mismatch := exact("Aves", "Passeriformes", "Corvidae")
	mismatch.CanonicalName, mismatch.UsageKey, mismatch.Phylum = "Bbb mismatch", 7, "Chordata"
	e := newEnv(t, meta, nil, map[string]match{"Aaa fuzzy": fuzzy, "Bbb mismatch": mismatch})
	e.run(t)
	rep := e.bytes(t, e.cfg.reportPath)
	checkCanonical(t, rep)
	var r report
	if err := json.Unmarshal(rep, &r); err != nil {
		t.Fatal(err)
	}
	want := []reject{
		{Class: "Aves", Name: "Aaa fuzzy", Reason: "not_exact", GBIF: &gbifMatch{CanonicalName: "Aaa fuzzi", Class: "Aves",
			Family: "Corvidae", Kingdom: "Animalia", MatchType: "FUZZY", Order: "Passeriformes", Rank: "SPECIES", Status: "ACCEPTED", UsageKey: 42}},
		{Class: "Insecta", Name: "Bbb mismatch", Reason: "class_mismatch", GBIF: &gbifMatch{CanonicalName: "Bbb mismatch", Class: "Aves",
			Family: "Corvidae", Kingdom: "Animalia", MatchType: "EXACT", Order: "Passeriformes", Phylum: "Chordata", Rank: "SPECIES", Status: "ACCEPTED", UsageKey: 7}},
	}
	if !reflect.DeepEqual(r.Rejects, want) {
		t.Errorf("rejects = %+v", r.Rejects)
	}
	if got := e.meta(t); !reflect.DeepEqual(got, meta) {
		t.Errorf("rejected records changed: %+v", got)
	}
}

// Two aliases share a canonical name. Only one alias is found in GBIF, so the
// canonical record and then the other alias can only be filled from partners
// that change later in the same run; one run must reach the fixed point.
func TestExecute_AliasChainReachesFixedPoint(t *testing.T) {
	cls := dataset.Taxonomy{Class: "Aves"}
	meta := map[string]dataset.SpeciesRecord{"A sp": {Taxonomy: cls}, "B sp": {Taxonomy: cls}, "C sp": {Taxonomy: cls}}
	e := newEnv(t, meta, map[string]string{"A sp": "C sp", "B sp": "C sp"},
		map[string]match{"B sp": exact("Aves", "Passeriformes", "Corvidae")})
	e.run(t)
	want := dataset.Taxonomy{Class: "Aves", Order: "Passeriformes", Family: "Corvidae"}
	for name, rec := range e.meta(t) {
		if rec.Taxonomy != want {
			t.Errorf("%s = %+v", name, rec.Taxonomy)
		}
	}
	if !strings.Contains(string(e.bytes(t, e.cfg.reportPath)), `"rejects": []`) {
		t.Errorf("report should be empty: %s", e.bytes(t, e.cfg.reportPath))
	}
}

// The same chain resolved before any request: B is already complete, so C and
// then A fill from partners and GBIF is never asked.
func TestExecute_AliasChainNeedsNoRequest(t *testing.T) {
	cls := dataset.Taxonomy{Class: "Aves"}
	full := dataset.Taxonomy{Class: "Aves", Order: "Passeriformes", Family: "Corvidae"}
	meta := map[string]dataset.SpeciesRecord{"A sp": {Taxonomy: cls}, "B sp": {Taxonomy: full}, "C sp": {Taxonomy: cls}}
	e := newEnv(t, meta, map[string]string{"A sp": "C sp", "B sp": "C sp"}, nil)
	e.run(t)
	if n := e.srv.take(); len(n) != 0 {
		t.Errorf("requested %v", n)
	}
	for name, rec := range e.meta(t) {
		if rec.Taxonomy != full {
			t.Errorf("%s = %+v", name, rec.Taxonomy)
		}
	}
}
