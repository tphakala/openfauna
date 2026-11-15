// Command fetch-gbif fills the missing order and family of species records that
// already have a class, using the GBIF Backbone Taxonomy (CC0).
//
// It is a manual maintenance tool, run like cmd/fetch-wikidata; its output
// (data/metadata.json and the report) is committed, and the build stays
// network-free. It never overwrites: a non-empty class, order or family is left
// as it is, class is never written, and family_common, links and media are not
// touched. Re-running is safe: a record that is complete is not queried again.
//
// A record is first completed from its taxonomic alias partner (data/aliases.json)
// when the partner has an order and family and the same class, which needs no
// request. The remaining records are looked up by species name with a strict
// GBIF match, and only an exact species-level match in kingdom Animalia whose
// class agrees with the record is used. Records that could not be filled are
// listed in the report, which only a full run (no -limit, no -dry-run) rewrites.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"maps"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"slices"
	"sort"
	"time"

	"github.com/tphakala/openfauna/internal/dataset"
	"github.com/tphakala/openfauna/internal/fetch"
)

const gbifMatchAPI = "https://api.gbif.org/v1/species/match"

// match is the part of a GBIF species/match response the tool reads.
type match struct {
	UsageKey      int    `json:"usageKey"`
	CanonicalName string `json:"canonicalName"`
	Rank          string `json:"rank"`
	Status        string `json:"status"`
	MatchType     string `json:"matchType"`
	Kingdom       string `json:"kingdom"`
	Phylum        string `json:"phylum"`
	Class         string `json:"class"`
	Order         string `json:"order"`
	Family        string `json:"family"`
}

// report lists what is still missing after a full pass. Fields are declared in
// the alphabetical order of their JSON keys so dataset.MarshalJSON emits the
// repo's canonical sorted form.
type report struct {
	Comment string   `json:"_comment"`
	Rejects []reject `json:"rejects"`
}

type reject struct {
	Class  string     `json:"class"`
	GBIF   *gbifMatch `json:"gbif,omitempty"`
	Name   string     `json:"name"`
	Reason string     `json:"reason"`
}

type gbifMatch struct {
	CanonicalName string `json:"canonical_name"`
	Class         string `json:"class"`
	Family        string `json:"family"`
	Kingdom       string `json:"kingdom"`
	MatchType     string `json:"match_type"`
	Order         string `json:"order"`
	Phylum        string `json:"phylum"`
	Rank          string `json:"rank"`
	Status        string `json:"status"`
	UsageKey      int    `json:"usage_key"`
}

const reportComment = "Records with a class that fetch-gbif could not give an order and family: GBIF has no exact species match, or the match failed a safety check. Rewritten only by a full run (no -limit, no -dry-run). Rerunning with unchanged data leaves this file as it is."

type config struct {
	metadataPath, aliasesPath, reportPath, api string
	limit                                      int
	dryRun                                     bool
}

func main() {
	var cfg config
	flag.StringVar(&cfg.metadataPath, "metadata", "data/metadata.json", "nested metadata file to update in place")
	flag.StringVar(&cfg.aliasesPath, "aliases", "data/aliases.json", "alias map used to copy order and family from an alias partner")
	flag.StringVar(&cfg.reportPath, "report", "data/validation/gbif_taxonomy_report.json", "report of records that could not be filled")
	flag.IntVar(&cfg.limit, "limit", 0, "query at most this many species (0 = all, first N by name); a limited run does not rewrite the report")
	flag.BoolVar(&cfg.dryRun, "dry-run", false, "query and log, write nothing")
	qps := flag.Float64("qps", 4, "max GBIF requests per second (politeness throttle)")
	userAgent := flag.String("user-agent", "fetch-gbif/1.0 (species taxonomy maintenance tool)", "HTTP User-Agent")
	flag.Parse()
	cfg.api = gbifMatchAPI

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	c := &fetch.Client{HTTP: &http.Client{Timeout: 30 * time.Second}, UserAgent: *userAgent,
		Interval: fetch.RateInterval(*qps), Ctx: ctx}
	if err := execute(cfg, c); err != nil {
		log.Fatal(err)
	}
}

// inScope reports whether a record has a class but lacks an order or family.
func inScope(t dataset.Taxonomy) bool {
	return t.Class != "" && (t.Order == "" || t.Family == "")
}

// decide applies a GBIF match to a record's taxonomy. It returns the taxonomy to
// store, with only an empty order or family set, and a reason; the reason is empty
// when the record is now complete.
func decide(t dataset.Taxonomy, m match) (dataset.Taxonomy, string) {
	switch {
	case m.MatchType == "NONE":
		return t, "no_match"
	case m.MatchType != "EXACT":
		return t, "not_exact"
	case m.Rank != "SPECIES":
		return t, "not_species"
	case m.Kingdom != "Animalia":
		return t, "not_animalia"
	case m.Status != "ACCEPTED" && m.Status != "SYNONYM":
		return t, "status"
	}
	order := m.Order
	switch {
	case m.Class == t.Class:
	case t.Class == "Reptilia" && m.Order == "" && slices.Contains([]string{"Squamata", "Testudines", "Crocodylia"}, m.Class):
		// GBIF ranks these reptile orders as classes.
		order = m.Class
	case t.Class == "Actinopterygii" && m.Class == "" && m.Phylum == "Chordata":
	default:
		return t, "class_mismatch"
	}
	if t.Order != "" && order != "" && t.Order != order {
		return t, "order_conflict"
	}
	if t.Family != "" && m.Family != "" && t.Family != m.Family {
		return t, "family_conflict"
	}
	if t.Order == "" {
		t.Order = order
	}
	if t.Family == "" {
		t.Family = m.Family
	}
	if t.Order == "" || t.Family == "" {
		return t, "incomplete"
	}
	return t, ""
}

// buildPartners maps each name in the alias map to the names on the other side
// of its alias entries, sorted.
func buildPartners(aliases map[string]string) map[string][]string {
	p := map[string][]string{}
	for from, to := range dataset.FilterAliases(aliases) {
		p[from] = append(p[from], to)
		p[to] = append(p[to], from)
	}
	for _, v := range p {
		sort.Strings(v)
	}
	return p
}

// aliasFill completes name's empty order and family from its alias partners.
// A partner counts when it has a record with the same class and both an order
// and a family. It declines when the partners disagree, or when one disagrees
// with a value the record already has.
func aliasFill(meta map[string]dataset.SpeciesRecord, partners map[string][]string, name string) (dataset.Taxonomy, bool) {
	t := meta[name].Taxonomy
	var order, family string
	for _, pn := range partners[name] {
		pt := meta[pn].Taxonomy // zero value, so skipped, when pn has no record
		if pt.Class != t.Class || pt.Order == "" || pt.Family == "" {
			continue
		}
		if (t.Order != "" && t.Order != pt.Order) || (t.Family != "" && t.Family != pt.Family) {
			return t, false
		}
		if order != "" && (order != pt.Order || family != pt.Family) {
			return t, false
		}
		order, family = pt.Order, pt.Family
	}
	if order == "" {
		return t, false
	}
	t.Order, t.Family = order, family
	return t, true
}

func toGBIFMatch(m match) *gbifMatch {
	return &gbifMatch{CanonicalName: m.CanonicalName, Class: m.Class, Family: m.Family, Kingdom: m.Kingdom,
		MatchType: m.MatchType, Order: m.Order, Phylum: m.Phylum, Rank: m.Rank, Status: m.Status, UsageKey: m.UsageKey}
}

// pending returns the in-scope record names, sorted.
func pending(meta map[string]dataset.SpeciesRecord) []string {
	var names []string
	for name, rec := range meta {
		if inScope(rec.Taxonomy) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func writeJSON(path string, v any) error {
	buf, err := dataset.MarshalJSON(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, buf, 0o644)
}

func execute(cfg config, c *fetch.Client) error {
	meta, err := dataset.LoadMetadata(cfg.metadataPath)
	if err != nil {
		return fmt.Errorf("load metadata: %w", err)
	}
	data, err := os.ReadFile(cfg.aliasesPath)
	if err != nil {
		return fmt.Errorf("load aliases: %w", err)
	}
	var rawAliases map[string]string
	if err := json.Unmarshal(data, &rawAliases); err != nil {
		return fmt.Errorf("parse aliases: %w", err)
	}
	partners := buildPartners(rawAliases)

	// Fills are applied to meta in memory and written only after the last
	// request succeeds, so a failed run leaves the files as they were.
	aliasFilled, gbifFilled := 0, 0
	set := func(name string, t dataset.Taxonomy) {
		rec := meta[name]
		rec.Taxonomy = t
		meta[name] = rec
	}
	fromAlias := func(name string) bool {
		t, ok := aliasFill(meta, partners, name)
		if ok {
			set(name, t)
			aliasFilled++
			log.Printf("alias %s: order=%s family=%s", name, t.Order, t.Family)
		}
		return ok
	}

	inScopeNames := pending(meta)
	var work []string
	for _, name := range inScopeNames {
		if !fromAlias(name) {
			work = append(work, name)
		}
	}
	if cfg.limit > 0 && cfg.limit < len(work) {
		work = work[:cfg.limit]
	}
	rejects := map[string]reject{}
	for _, name := range work {
		q := url.Values{"name": {name}, "strict": {"true"}}
		body, err := c.Get(cfg.api + "?" + q.Encode())
		if err != nil {
			return fmt.Errorf("fetch %s: %w", name, err)
		}
		var m match
		if err := json.Unmarshal(body, &m); err != nil {
			return fmt.Errorf("decode response for %s: %w", name, err)
		}
		t, reason := decide(meta[name].Taxonomy, m)
		set(name, t)
		if reason == "" {
			gbifFilled++
			log.Printf("fill %s: order=%s family=%s status=%s", name, t.Order, t.Family, m.Status)
			continue
		}
		log.Printf("reject %s: %s", name, reason)
		r := reject{Class: t.Class, Name: name, Reason: reason}
		if m.MatchType != "NONE" {
			r.GBIF = toGBIFMatch(m)
		}
		rejects[name] = r
	}

	// A rejected record may now have a partner that GBIF filled in this run.
	for _, name := range slices.Sorted(maps.Keys(rejects)) {
		if fromAlias(name) {
			delete(rejects, name)
		}
	}
	log.Printf("in scope %d: %d from alias partners, %d from GBIF, %d rejected",
		len(inScopeNames), aliasFilled, gbifFilled, len(rejects))

	if cfg.dryRun {
		return nil
	}
	if err := writeJSON(cfg.metadataPath, meta); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	if cfg.limit > 0 {
		return nil
	}
	rep := report{Comment: reportComment, Rejects: []reject{}}
	for _, name := range slices.Sorted(maps.Keys(rejects)) {
		rep.Rejects = append(rep.Rejects, rejects[name])
	}
	if err := writeJSON(cfg.reportPath, rep); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}
