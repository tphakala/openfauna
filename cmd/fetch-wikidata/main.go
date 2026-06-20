// Command fetch-wikidata upgrades each species' Wikipedia link from an interim
// English article title to a language-agnostic Wikidata QID, cross-checking the
// enwiki sitelink against the P225 (taxon name) SPARQL lookup, and seeds curated
// Wikimedia Commons media (Wikidata P18) for non-bird taxa.
//
// It is a manual maintenance tool, run like cmd/fetch-gbif and
// cmd/fetch-inaturalist; its output (data/metadata.json, data/media.json) is
// committed. The build itself stays network-free. Re-running is safe: links that
// already carry a QID are skipped, unresolved links keep their working English
// title fallback, and media seeding only fills species that have no media yet, so
// hand-curated entries are preserved.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"maps"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strings"
	"time"

	"github.com/tphakala/openfauna/internal/dataset"
)

const (
	enwikiAPI   = "https://en.wikipedia.org/w/api.php"
	wikidataAPI = "https://www.wikidata.org/w/api.php"
	commonsAPI  = "https://commons.wikimedia.org/w/api.php"
	sparqlAPI   = "https://query.wikidata.org/sparql"
	apiBatch    = 50 // MediaWiki / wbgetentities accept up to 50 titles/ids per call
	sparqlBatch = 100
)

func main() {
	metadataPath := flag.String("metadata", "data/metadata.json", "nested metadata file to update in place")
	mediaPath := flag.String("media", "data/media.json", "media file to update in place")
	reportPath := flag.String("report", "data/validation/wikidata_report.json", "QID cross-check report output")
	limit := flag.Int("limit", 0, "process at most this many species (0 = all); for a bounded pilot run")
	skipMedia := flag.Bool("skip-media", false, "resolve QIDs only; do not seed Wikimedia media")
	qps := flag.Float64("qps", 8, "max API requests per second (politeness throttle)")
	userAgent := flag.String("user-agent",
		"OpenFauna-fetch-wikidata/1.0 (https://github.com/tphakala/openfauna; species naming dataset)",
		"HTTP User-Agent (Wikimedia requires a descriptive one)")
	flag.Parse()

	meta, err := dataset.LoadMetadata(*metadataPath)
	if err != nil {
		log.Fatalf("load metadata: %v", err)
	}

	// Work set: species whose wikipedia link is still a title (not yet a QID),
	// sorted for deterministic batching and --limit slicing.
	var work []string
	for name, rec := range meta {
		link, ok := rec.Links["wikipedia"]
		if ok && link.ID != "" && !dataset.IsQID(link.ID) {
			work = append(work, name)
		}
	}
	sort.Strings(work)
	if *limit > 0 && *limit < len(work) {
		work = work[:*limit]
	}
	log.Printf("resolving QIDs for %d species (limit=%d)", len(work), *limit)

	// A signal-aware context lets an interrupted run (Ctrl+C) cancel in-flight
	// requests instead of hanging on a slow endpoint.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	c := &client{http: &http.Client{Timeout: 60 * time.Second}, ua: *userAgent, interval: rateInterval(*qps), ctx: ctx}

	titleQID := c.resolveTitles(meta, work)
	nameQID := c.resolveNames(work)

	rep := newReport()
	for _, name := range work {
		rec := meta[name]
		link := rec.Links["wikipedia"]
		title := link.ID
		id, override, status := dataset.ResolveQID(title, titleQID[name], nameQID[name])
		rec.Links["wikipedia"] = dataset.Link{ID: id, URL: override}
		meta[name] = rec
		rep.record(status, name, title, titleQID[name], nameQID[name])
	}
	log.Printf("QID: %d agreed, %d disagreed, %d unresolved", rep.Agree, len(rep.Disagree), len(rep.Unresolved))

	media, err := dataset.LoadMedia(*mediaPath)
	if err != nil && !os.IsNotExist(err) {
		log.Fatalf("load media: %v", err)
	}
	if media == nil {
		media = map[string][]dataset.MediaItem{}
	}
	if !*skipMedia {
		added, skipped := c.seedMedia(meta, work, *limit, media)
		rep.MediaAdded = added
		rep.MediaSkipped = skipped
		log.Printf("media: %d added, %d skipped for an unclean license", added, skipped)
	}

	if err := writeJSON(*metadataPath, meta); err != nil {
		log.Fatalf("write metadata: %v", err)
	}
	if err := writeJSON(*mediaPath, media); err != nil {
		log.Fatalf("write media: %v", err)
	}
	if err := rep.write(*reportPath); err != nil {
		log.Fatalf("write report: %v", err)
	}
	log.Printf("done; report written to %s", *reportPath)
}

// resolveTitles maps each work species' stored enwiki title to a QID via the
// MediaWiki pageprops API, batched and resolved per batch (normalization and
// redirect maps are batch-local).
func (c *client) resolveTitles(meta map[string]dataset.SpeciesRecord, work []string) map[string]string {
	out := make(map[string]string, len(work))
	for _, batch := range chunk(work, apiBatch) {
		titles := make([]string, len(batch))
		for i, name := range batch {
			titles[i] = meta[name].Links["wikipedia"].ID
		}
		q := url.Values{}
		q.Set("action", "query")
		q.Set("format", "json")
		q.Set("prop", "pageprops")
		q.Set("ppprop", "wikibase_item")
		q.Set("redirects", "1")
		q.Set("titles", strings.Join(titles, "|"))
		body, err := c.get(enwikiAPI + "?" + q.Encode())
		if err != nil {
			log.Printf("pageprops batch failed (%d titles): %v", len(titles), err)
			continue
		}
		res, err := dataset.ParsePageProps(body)
		if err != nil {
			log.Printf("pageprops parse failed: %v", err)
			continue
		}
		for i, name := range batch {
			if qid := res.QIDForTitle(titles[i]); qid != "" {
				out[name] = qid
			}
		}
	}
	return out
}

// resolveNames cross-checks each work species' scientific name against the P225
// (taxon name) SPARQL lookup. An ambiguous name (homonym) yields no entry.
func (c *client) resolveNames(work []string) map[string]string {
	out := make(map[string]string, len(work))
	for _, batch := range chunk(work, sparqlBatch) {
		var b strings.Builder
		b.WriteString("SELECT ?taxon ?name WHERE { VALUES ?name {")
		for _, name := range batch {
			b.WriteString(" \"")
			b.WriteString(sparqlEscape(name))
			b.WriteString("\"")
		}
		b.WriteString(" } ?taxon wdt:P225 ?name. }")
		q := url.Values{}
		q.Set("format", "json")
		q.Set("query", b.String())
		body, err := c.get(sparqlAPI + "?" + q.Encode())
		if err != nil {
			log.Printf("sparql batch failed (%d names): %v", len(batch), err)
			continue
		}
		got, err := dataset.ParseSPARQL(body)
		if err != nil {
			log.Printf("sparql parse failed: %v", err)
			continue
		}
		maps.Copy(out, got)
	}
	return out
}

// seedMedia fetches Wikidata P18 for non-bird species that carry a QID link and
// builds a CC-clean MediaItem from the Commons imageinfo for each. With limit>0
// only the bounded work set is considered; with limit==0 all non-bird QID species
// are. It returns the count added and the count skipped for license reasons.
func (c *client) seedMedia(meta map[string]dataset.SpeciesRecord, work []string, limit int, media map[string][]dataset.MediaItem) (added, skipped int) {
	// Candidate names: non-bird species with a QID wikipedia link.
	var names []string
	if limit > 0 {
		names = work
	} else {
		for name := range meta {
			names = append(names, name)
		}
		sort.Strings(names)
	}
	qidByName := map[string]string{}
	var qids []string
	for _, name := range names {
		rec := meta[name]
		if strings.EqualFold(rec.Taxonomy.Class, "Aves") {
			continue
		}
		link, ok := rec.Links["wikipedia"]
		if !ok || !dataset.IsQID(link.ID) {
			continue
		}
		qidByName[name] = link.ID
		qids = append(qids, link.ID)
	}
	if len(qids) == 0 {
		return 0, 0
	}
	log.Printf("media: checking P18 for %d non-bird QID species", len(qids))

	// QID -> Commons filename via P18.
	fileByQID := map[string]string{}
	for _, batch := range chunk(dedupe(qids), apiBatch) {
		q := url.Values{}
		q.Set("action", "wbgetentities")
		q.Set("format", "json")
		q.Set("props", "claims")
		q.Set("ids", strings.Join(batch, "|"))
		body, err := c.get(wikidataAPI + "?" + q.Encode())
		if err != nil {
			log.Printf("wbgetentities batch failed: %v", err)
			continue
		}
		p18, err := dataset.ParseP18(body)
		if err != nil {
			log.Printf("P18 parse failed: %v", err)
			continue
		}
		maps.Copy(fileByQID, p18)
	}

	// Commons filename -> CommonsFile via imageinfo, keyed by query title.
	fileByTitle := map[string]dataset.CommonsFile{}
	var fileTitles []string
	seenFile := map[string]bool{}
	for _, file := range fileByQID {
		title := "File:" + file
		if !seenFile[title] {
			seenFile[title] = true
			fileTitles = append(fileTitles, title)
		}
	}
	sort.Strings(fileTitles)
	for _, batch := range chunk(fileTitles, apiBatch) {
		q := url.Values{}
		q.Set("action", "query")
		q.Set("format", "json")
		q.Set("prop", "imageinfo")
		q.Set("iiprop", "size|mime|extmetadata")
		q.Set("redirects", "1") // a P18 value can name a since-renamed Commons file
		q.Set("titles", strings.Join(batch, "|"))
		body, err := c.get(commonsAPI + "?" + q.Encode())
		if err != nil {
			log.Printf("imageinfo batch failed: %v", err)
			continue
		}
		res, err := dataset.ParseImageInfo(body)
		if err != nil {
			log.Printf("imageinfo parse failed: %v", err)
			continue
		}
		for _, title := range batch {
			if f, ok := res.FileFor(title); ok {
				fileByTitle[title] = f
			}
		}
	}

	// Build media items, deterministically by name.
	var sortedNames []string
	for name := range qidByName {
		sortedNames = append(sortedNames, name)
	}
	sort.Strings(sortedNames)
	for _, name := range sortedNames {
		// Preserve any existing (possibly hand-curated) media; this pass only
		// seeds species that have none yet.
		if len(media[name]) > 0 {
			continue
		}
		file, ok := fileByQID[qidByName[name]]
		if !ok {
			continue
		}
		cf, ok := fileByTitle["File:"+file]
		if !ok {
			continue
		}
		item, ok := dataset.BuildWikimediaMedia(cf)
		if !ok {
			skipped++
			continue
		}
		media[name] = []dataset.MediaItem{item}
		added++
	}
	return added, skipped
}

// client wraps an HTTP client with a descriptive User-Agent and a politeness
// throttle. Wikimedia requires the User-Agent and rate-limits aggressive clients.
type client struct {
	http     *http.Client
	ua       string
	interval time.Duration
	last     time.Time
	ctx      context.Context
}

func rateInterval(qps float64) time.Duration {
	if qps <= 0 {
		return 0
	}
	return time.Duration(float64(time.Second) / qps)
}

// get fetches a URL, throttling to the configured rate and retrying transient
// failures (a connection error, a body-read error on an otherwise-200 response,
// or a 429/5xx status) with a short backoff. A GET is idempotent, so a read
// error is safe to retry.
func (c *client) get(rawURL string) ([]byte, error) {
	const maxAttempts = 4
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if c.interval > 0 {
			if wait := c.interval - time.Since(c.last); wait > 0 {
				if err := c.sleep(wait); err != nil {
					return nil, err
				}
			}
		}
		c.last = time.Now()

		req, err := http.NewRequestWithContext(c.ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", c.ua)
		req.Header.Set("Accept", "application/json")
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			if serr := c.sleep(backoff(attempt)); serr != nil {
				return nil, serr
			}
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			if serr := c.sleep(backoff(attempt)); serr != nil {
				return nil, serr
			}
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
			if serr := c.sleep(backoff(attempt)); serr != nil {
				return nil, serr
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("status %d", resp.StatusCode)
		}
		return body, nil
	}
	return nil, fmt.Errorf("giving up after %d attempts: %w", maxAttempts, lastErr)
}

// sleep waits for d or until the context is cancelled, so an interrupt (Ctrl+C)
// returns promptly instead of blocking for the full throttle or backoff delay.
func (c *client) sleep(d time.Duration) error {
	if d <= 0 {
		return nil
	}
	select {
	case <-c.ctx.Done():
		return c.ctx.Err()
	case <-time.After(d):
		return nil
	}
}

func backoff(attempt int) time.Duration {
	return time.Duration(attempt) * time.Second
}

// report tallies the QID cross-check outcome for the maintainer to review.
type report struct {
	Agree        int        `json:"agree"`
	MediaAdded   int        `json:"media_added"`
	MediaSkipped int        `json:"media_skipped"`
	Disagree     []conflict `json:"disagree"`
	Unresolved   []conflict `json:"unresolved"`
}

type conflict struct {
	Name     string `json:"name"`
	Title    string `json:"title"`
	QIDTitle string `json:"qid_title,omitempty"`
	QIDName  string `json:"qid_name,omitempty"`
}

func newReport() *report {
	return &report{Disagree: []conflict{}, Unresolved: []conflict{}}
}

func (r *report) record(status, name, title, qidTitle, qidName string) {
	switch status {
	case dataset.QIDAgree:
		r.Agree++
	case dataset.QIDDisagree:
		r.Disagree = append(r.Disagree, conflict{name, title, qidTitle, qidName})
	case dataset.QIDUnresolved:
		r.Unresolved = append(r.Unresolved, conflict{name, title, qidTitle, qidName})
	}
}

func (r *report) write(path string) error {
	buf, err := dataset.MarshalJSON(r)
	if err != nil {
		return err
	}
	return os.WriteFile(path, buf, 0o644)
}

func writeJSON(path string, v any) error {
	buf, err := dataset.MarshalJSON(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, buf, 0o644)
}

func chunk(ss []string, n int) [][]string {
	var out [][]string
	for i := 0; i < len(ss); i += n {
		end := min(i+n, len(ss))
		out = append(out, ss[i:end])
	}
	return out
}

func dedupe(ss []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func sparqlEscape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}
