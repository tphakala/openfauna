package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type SpeciesMetadata struct {
	Class        string `json:"class"`
	Order        string `json:"order"`
	Family       string `json:"family"`
	FamilyCommon string `json:"family_common,omitempty"`
	WikipediaURL string `json:"wikipedia_url,omitempty"`
}

type GBIFResponse struct {
	Class  string `json:"class"`
	Order  string `json:"order"`
	Family string `json:"family"`
}

func main() {
	metadataPath := "data/metadata.json"

	data, err := os.ReadFile(metadataPath)
	if err != nil {
		log.Fatalf("Failed to read %s: %v", metadataPath, err)
	}

	var metadata map[string]SpeciesMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		log.Fatalf("Failed to parse metadata: %v", err)
	}

	// 1. Extract unique genera
	genera := make(map[string]bool)
	for species := range metadata {
		parts := strings.Split(species, " ")
		if len(parts) > 0 {
			genera[parts[0]] = true
		}
	}

	var generaList []string
	for g := range genera {
		generaList = append(generaList, g)
	}
	log.Printf("Found %d unique genera to fetch from GBIF", len(generaList))

	// 2. Fetch GBIF taxonomy for each genus concurrently
	genusData := make(map[string]GBIFResponse)
	var mu sync.Mutex
	var wg sync.WaitGroup

	sem := make(chan struct{}, 10) // 10 concurrent requests
	client := &http.Client{Timeout: 10 * time.Second}

	for i, g := range generaList {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, genus string) {
			defer wg.Done()
			defer func() { <-sem }()

			apiURL := fmt.Sprintf("https://api.gbif.org/v1/species/match?name=%s", url.QueryEscape(genus))
			resp, err := client.Get(apiURL)
			if err != nil {
				log.Printf("Failed to fetch %s: %v", genus, err)
				return
			}
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)
			var gbif GBIFResponse
			if err := json.Unmarshal(body, &gbif); err == nil {
				mu.Lock()
				genusData[genus] = gbif
				mu.Unlock()
			}

			if idx%100 == 0 {
				log.Printf("Fetched %d/%d genera...", idx, len(generaList))
			}
		}(i, g)
	}

	wg.Wait()
	log.Printf("Successfully fetched data for %d genera from GBIF", len(genusData))

	// 3. Update metadata
	for species, meta := range metadata {
		parts := strings.Split(species, " ")
		if len(parts) > 0 {
			genus := parts[0]
			if gbif, ok := genusData[genus]; ok {
				meta.Class = gbif.Class
				meta.Order = gbif.Order
				meta.Family = gbif.Family
				meta.FamilyCommon = "" // Clear proprietary eBird common names
				metadata[species] = meta
			}
		}
	}

	// 4. Save back
	outFile, err := os.Create(metadataPath)
	if err != nil {
		log.Fatalf("Failed to create %s: %v", metadataPath, err)
	}
	defer outFile.Close()

	encoder := json.NewEncoder(outFile)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(metadata); err != nil {
		log.Fatalf("Failed to encode metadata: %v", err)
	}

	log.Println("Metadata successfully overwritten with pure GBIF (CC0) data!")
}
