package dataset

import (
	"encoding/json"
	"html"
	"math"
	"net/url"
	"regexp"
	"strings"
)

// This file holds the pure, network-free helpers and response parsers used by
// cmd/fetch-wikidata. The tool keeps all HTTP and batching glue in its main
// package; everything that needs a unit test lives here.

var qidPattern = regexp.MustCompile(`^Q[1-9][0-9]*$`)

// IsQID reports whether s is a Wikidata entity id (Q followed by a positive
// integer), distinguishing an already-upgraded link id from a legacy title id.
func IsQID(s string) bool { return qidPattern.MatchString(s) }

// QID cross-check decision statuses.
const (
	QIDAgree      = "agree"      // title sitelink resolved; name cross-check agrees or is absent
	QIDDisagree   = "disagree"   // title and name resolved to different QIDs
	QIDUnresolved = "unresolved" // the title sitelink yielded no QID
)

// ResolveQID applies the cross-check policy for one species. title is the stored
// enwiki article title (used to build the fallback URL); qidTitle and qidName are
// the QIDs resolved from the enwiki sitelink and the P225 SPARQL cross-check. It
// returns the link id to store, an optional url override, and the decision
// status. The title sitelink is authoritative, so a QID is assigned when it is
// present and the name cross-check either agrees or is silent; a disagreement or
// a missing sitelink keeps the working English title link.
func ResolveQID(title, qidTitle, qidName string) (id, urlOverride, status string) {
	switch {
	case qidTitle != "" && (qidName == "" || qidName == qidTitle):
		return qidTitle, "", QIDAgree
	case qidTitle != "" && qidName != "" && qidName != qidTitle:
		return title, WikipediaEnURL(title), QIDDisagree
	default:
		return title, WikipediaEnURL(title), QIDUnresolved
	}
}

// WikipediaEnURL builds the canonical English Wikipedia URL for a stored title id,
// percent-encoding the path so an unusual character cannot produce a broken link.
func WikipediaEnURL(title string) string {
	u := url.URL{Scheme: "https", Host: "en.wikipedia.org", Path: "/wiki/" + title}
	return u.String()
}

// AspectRatio returns width/height rounded to two decimals, or 0 when either
// dimension is missing.
func AspectRatio(w, h int) float64 {
	if w <= 0 || h <= 0 {
		return 0
	}
	return math.Round(float64(w)/float64(h)*100) / 100
}

// Orientation aspect-ratio band. An image reads as "square" when its ratio sits
// within a band around 1:1; the bounds are a reciprocal pair so the band is
// symmetric in log-ratio (orientationPortraitMax ~= 1 / orientationLandscapeMin).
const (
	orientationLandscapeMin = 1.15
	orientationPortraitMax  = 0.87
)

// ClassifyOrientation buckets an image by its aspect ratio. The near-square band
// keeps a roughly 1:1 image labelled "square" while a 4:3-ish image reads as
// landscape.
func ClassifyOrientation(w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	r := float64(w) / float64(h)
	switch {
	case r >= orientationLandscapeMin:
		return "landscape"
	case r <= orientationPortraitMax:
		return "portrait"
	default:
		return "square"
	}
}

// AcceptLicense reports whether a Commons license URL is redistribution-clean
// (a Creative Commons license or a public-domain dedication). The host must be
// exactly creativecommons.org and the path one of the clean prefixes; a substring
// match would let a spoof URL like https://evil.example/?x=creativecommons.org/licenses/
// through. An empty or unparseable license is rejected so only clean media is
// ingested.
func AcceptLicense(licenseURL string) bool {
	raw := strings.TrimSpace(licenseURL)
	if raw == "" {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "creativecommons.org" && host != "www.creativecommons.org" {
		return false
	}
	path := strings.ToLower(parsed.EscapedPath())
	return strings.HasPrefix(path, "/licenses/") || strings.HasPrefix(path, "/publicdomain/")
}

var htmlTagPattern = regexp.MustCompile(`<[^>]*>`)

// SanitizeCreator turns a Commons Artist field (often an HTML anchor) into a
// plain-text attribution string. strings.Fields splits on Unicode whitespace, so
// a non-breaking space from a decoded &nbsp; collapses to a normal space.
func SanitizeCreator(s string) string {
	s = htmlTagPattern.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	return strings.Join(strings.Fields(s), " ")
}

// CommonsFile is the subset of a Commons imageinfo result that we ingest.
type CommonsFile struct {
	Filename   string // bare file name without the "File:" prefix; the media id
	Width      int
	Height     int
	MIME       string
	LicenseURL string
	Artist     string // raw Artist field; may contain HTML
}

// BuildWikimediaMedia turns a Commons file into a MediaItem, returning ok=false
// when the license is not redistribution-clean (the license is the curation gate
// for this ingest pass).
func BuildWikimediaMedia(f CommonsFile) (MediaItem, bool) {
	if !AcceptLicense(f.LicenseURL) {
		return MediaItem{}, false
	}
	creator := SanitizeCreator(f.Artist)
	return MediaItem{
		Source:       "wikimedia",
		ID:           f.Filename,
		Format:       f.MIME,
		Width:        f.Width,
		Height:       f.Height,
		AspectRatio:  AspectRatio(f.Width, f.Height),
		Orientation:  ClassifyOrientation(f.Width, f.Height),
		License:      f.LicenseURL,
		Creator:      creator,
		RightsHolder: creator,
	}, true
}

// PagePropsResult is a parsed MediaWiki pageprops response. Normalized and
// Redirects map a queried title onto the title the API actually keyed pages by.
type PagePropsResult struct {
	Normalized map[string]string // from -> to
	Redirects  map[string]string // from -> to
	TitleQID   map[string]string // page title -> wikibase_item
}

// ParsePageProps parses a MediaWiki action=query&prop=pageprops response.
func ParsePageProps(data []byte) (PagePropsResult, error) {
	var raw struct {
		Query struct {
			Normalized []struct {
				From string `json:"from"`
				To   string `json:"to"`
			} `json:"normalized"`
			Redirects []struct {
				From string `json:"from"`
				To   string `json:"to"`
			} `json:"redirects"`
			Pages map[string]struct {
				Title     string `json:"title"`
				PageProps struct {
					WikibaseItem string `json:"wikibase_item"`
				} `json:"pageprops"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return PagePropsResult{}, err
	}
	res := PagePropsResult{
		Normalized: map[string]string{},
		Redirects:  map[string]string{},
		TitleQID:   map[string]string{},
	}
	for _, n := range raw.Query.Normalized {
		res.Normalized[n.From] = n.To
	}
	for _, r := range raw.Query.Redirects {
		res.Redirects[r.From] = r.To
	}
	for _, p := range raw.Query.Pages {
		// Only store a well-formed QID, so an unexpected MediaWiki token never
		// flows into a stored link id and breaks QID-based resolution.
		if IsQID(p.PageProps.WikibaseItem) {
			res.TitleQID[p.Title] = p.PageProps.WikibaseItem
		}
	}
	return res, nil
}

// QIDForTitle resolves a queried title to its QID, following normalization and a
// single redirect hop as reported by the API.
func (r PagePropsResult) QIDForTitle(orig string) string {
	t := orig
	if n, ok := r.Normalized[t]; ok {
		t = n
	}
	if rd, ok := r.Redirects[t]; ok {
		t = rd
	}
	return r.TitleQID[t]
}

// ParseSPARQL parses a Wikidata SPARQL JSON response binding ?name to ?taxon and
// returns a name -> QID map. A name that resolves to more than one taxon
// (homonym) is omitted, so an ambiguous cross-check never forces a disagreement.
func ParseSPARQL(data []byte) (map[string]string, error) {
	var raw struct {
		Results struct {
			Bindings []map[string]struct {
				Value string `json:"value"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	seen := map[string]map[string]bool{}
	for _, b := range raw.Results.Bindings {
		nameField, okN := b["name"]
		taxonField, okT := b["taxon"]
		if !okN || !okT {
			continue
		}
		qid := lastURISegment(taxonField.Value)
		if !IsQID(qid) {
			continue
		}
		if seen[nameField.Value] == nil {
			seen[nameField.Value] = map[string]bool{}
		}
		seen[nameField.Value][qid] = true
	}
	out := map[string]string{}
	for name, set := range seen {
		if len(set) == 1 {
			for qid := range set {
				out[name] = qid
			}
		}
	}
	return out, nil
}

func lastURISegment(u string) string {
	if i := strings.LastIndexByte(u, '/'); i >= 0 {
		return u[i+1:]
	}
	return u
}

// ParseP18 parses a Wikidata wbgetentities (props=claims) response and returns a
// QID -> Commons filename map for entities that carry a P18 image claim. When an
// entity has several P18 values, the first is used.
func ParseP18(data []byte) (map[string]string, error) {
	var raw struct {
		Entities map[string]struct {
			Claims struct {
				P18 []struct {
					Mainsnak struct {
						DataValue struct {
							Value string `json:"value"`
						} `json:"datavalue"`
					} `json:"mainsnak"`
				} `json:"P18"`
			} `json:"claims"`
		} `json:"entities"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for qid, e := range raw.Entities {
		if len(e.Claims.P18) > 0 {
			if v := e.Claims.P18[0].Mainsnak.DataValue.Value; v != "" {
				out[qid] = v
			}
		}
	}
	return out, nil
}

// ImageInfoResult is a parsed Commons imageinfo response, keyed by page title.
// Normalized and Redirects map a queried file title onto the title the API keyed
// the page by (a P18 value can point at a since-renamed file, which the API
// resolves via a redirect when asked).
type ImageInfoResult struct {
	Normalized map[string]string      // from -> to
	Redirects  map[string]string      // from -> to
	Files      map[string]CommonsFile // page title (e.g. "File:Name.jpg") -> file
}

// ParseImageInfo parses a Commons action=query&prop=imageinfo response.
func ParseImageInfo(data []byte) (ImageInfoResult, error) {
	var raw struct {
		Query struct {
			Normalized []struct {
				From string `json:"from"`
				To   string `json:"to"`
			} `json:"normalized"`
			Redirects []struct {
				From string `json:"from"`
				To   string `json:"to"`
			} `json:"redirects"`
			Pages map[string]struct {
				Title     string `json:"title"`
				ImageInfo []struct {
					Width   int    `json:"width"`
					Height  int    `json:"height"`
					MIME    string `json:"mime"`
					ExtMeta struct {
						LicenseURL struct {
							Value string `json:"value"`
						} `json:"LicenseUrl"`
						Artist struct {
							Value string `json:"value"`
						} `json:"Artist"`
					} `json:"extmetadata"`
				} `json:"imageinfo"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return ImageInfoResult{}, err
	}
	res := ImageInfoResult{
		Normalized: map[string]string{},
		Redirects:  map[string]string{},
		Files:      map[string]CommonsFile{},
	}
	for _, n := range raw.Query.Normalized {
		res.Normalized[n.From] = n.To
	}
	for _, r := range raw.Query.Redirects {
		res.Redirects[r.From] = r.To
	}
	for _, p := range raw.Query.Pages {
		if len(p.ImageInfo) == 0 {
			continue
		}
		ii := p.ImageInfo[0]
		res.Files[p.Title] = CommonsFile{
			Filename:   strings.TrimPrefix(p.Title, "File:"),
			Width:      ii.Width,
			Height:     ii.Height,
			MIME:       ii.MIME,
			LicenseURL: ii.ExtMeta.LicenseURL.Value,
			Artist:     ii.ExtMeta.Artist.Value,
		}
	}
	return res, nil
}

// FileFor resolves a queried file title to its parsed CommonsFile, following
// normalization and a single redirect hop as reported by the API (a P18 value can
// name a since-renamed file).
func (r ImageInfoResult) FileFor(origTitle string) (CommonsFile, bool) {
	t := origTitle
	if n, ok := r.Normalized[t]; ok {
		t = n
	}
	if rd, ok := r.Redirects[t]; ok {
		t = rd
	}
	f, ok := r.Files[t]
	return f, ok
}
