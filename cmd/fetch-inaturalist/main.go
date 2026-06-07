package main

import (
	"encoding/csv"
	"encoding/json"
	"log"
	"net/http"
	"os"
)

type Metadata struct {
	Class          string `json:"class,omitempty"`
	Order          string `json:"order,omitempty"`
	Family         string `json:"family,omitempty"`
	FamilyCommon   string `json:"family_common,omitempty"`
	WikipediaURL   string `json:"wikipedia_url,omitempty"`
	INaturalistURL string `json:"inaturalist_url,omitempty"`
}

func main() {
	metadataPath := "data/metadata.json"
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		log.Fatalf("Failed to read metadata: %v", err)
	}

	var metadata map[string]Metadata
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
	_, err = csvReader.Read()
	if err != nil {
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

		// Check if we track this species
		if info, exists := metadata[name]; exists {
			if info.INaturalistURL == "" {
				info.INaturalistURL = "https://www.inaturalist.org/taxa/" + taxonID
				metadata[name] = info
				count++
			}
		}
	}

	log.Printf("Added %d new iNaturalist URLs. Saving metadata.json...", count)

	outFile, err := os.Create(metadataPath)
	if err != nil {
		log.Fatalf("Failed to open metadata for writing: %v", err)
	}
	defer outFile.Close()

	encoder := json.NewEncoder(outFile)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(metadata); err != nil {
		log.Fatalf("Failed to write metadata: %v", err)
	}

	log.Println("Done!")
}
