package main

import (
	"encoding/csv"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/tphakala/openfauna/internal/dataset"
)

func main() {
	metadataPath := "data/metadata.json"
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		log.Fatalf("Failed to read metadata: %v", err)
	}

	var metadata map[string]dataset.SpeciesRecord
	if err := json.Unmarshal(data, &metadata); err != nil {
		log.Fatalf("Failed to parse metadata: %v", err)
	}

	log.Println("Downloading and streaming iNaturalist taxa dump...")
	url := "https://inaturalist-open-data.s3.amazonaws.com/taxa.csv.gz"
	resp, err := http.Get(url)
	if err != nil {
		log.Fatalf("Failed to download taxa dump: %v", err)
	}
	defer resp.Body.Close()

	csvReader := csv.NewReader(resp.Body)
	csvReader.Comma = '\t'
	csvReader.ReuseRecord = true
	csvReader.LazyQuotes = true

	// Skip header
	if _, err := csvReader.Read(); err != nil {
		log.Fatalf("Failed to read header: %v", err)
	}

	count := 0
	for {
		row, err := csvReader.Read()
		if err != nil {
			break // EOF or error
		}

		if len(row) < 5 {
			continue
		}

		taxonID := row[0]
		name := row[4]

		// Add the iNaturalist taxon id only for species we track that do not
		// already carry one. Reading from a nil Links map is safe.
		rec, exists := metadata[name]
		if !exists {
			continue
		}
		if _, has := rec.Links["inaturalist"]; has {
			continue
		}
		if rec.Links == nil {
			rec.Links = map[string]dataset.Link{}
		}
		rec.Links["inaturalist"] = dataset.Link{ID: taxonID}
		metadata[name] = rec
		count++
	}

	log.Printf("Added %d new iNaturalist ids. Saving metadata.json...", count)

	buf, err := dataset.MarshalJSON(metadata)
	if err != nil {
		log.Fatalf("Failed to encode metadata: %v", err)
	}
	if err := os.WriteFile(metadataPath, buf, 0o644); err != nil {
		log.Fatalf("Failed to write metadata: %v", err)
	}

	log.Println("Done!")
}
