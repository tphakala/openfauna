package dataset

// Taxonomy is the grouped taxonomic rank data for a species.
type Taxonomy struct {
	Class        string `json:"class"`
	Order        string `json:"order"`
	Family       string `json:"family"`
	FamilyCommon string `json:"family_common"`
}

// Link is one external reference for a species. ID is the stable provider
// identifier; URL is an optional full-URL override used only when the source
// registry template cannot construct the link.
type Link struct {
	ID  string `json:"id"`
	URL string `json:"url,omitempty"`
}

// MediaItem is one curated image reference. Fields follow Audubon Core. URLs
// are derived by the consumer from Source + ID via the media registry; no raw
// CDN URL is stored. Media is populated in Plan 2.
type MediaItem struct {
	Source       string  `json:"source"`
	ID           string  `json:"id"`
	Format       string  `json:"format,omitempty"`
	Width        int     `json:"width,omitempty"`
	Height       int     `json:"height,omitempty"`
	AspectRatio  float64 `json:"aspect_ratio,omitempty"`
	Orientation  string  `json:"orientation,omitempty"`
	License      string  `json:"license"`
	Creator      string  `json:"creator,omitempty"`
	RightsHolder string  `json:"rightsHolder,omitempty"`
}

// SpeciesRecord is the nested per-species authored record.
type SpeciesRecord struct {
	Taxonomy Taxonomy        `json:"taxonomy"`
	Links    map[string]Link `json:"links,omitempty"`
	Media    []MediaItem     `json:"media,omitempty"`
}

// LoadMetadata reads the nested authored metadata file.
func LoadMetadata(path string) (map[string]SpeciesRecord, error) {
	var m map[string]SpeciesRecord
	if err := loadJSON(path, &m); err != nil {
		return nil, err
	}
	return m, nil
}
