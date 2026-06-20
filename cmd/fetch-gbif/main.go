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

	"github.com/tphakala/openfauna/internal/dataset"
)

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

	var metadata map[string]dataset.SpeciesRecord
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

	// 3. Update metadata taxonomy
	for species, rec := range metadata {
		rec.Taxonomy.FamilyCommon = "" // Always clear proprietary eBird family-common names
		parts := strings.Split(species, " ")
		if len(parts) > 0 {
			genus := parts[0]
			if gbif, ok := genusData[genus]; ok {
				rec.Taxonomy.Class = gbif.Class
				rec.Taxonomy.Order = gbif.Order
				rec.Taxonomy.Family = gbif.Family
			}
		}
		metadata[species] = rec
	}

	// 4. Save back
	buf, err := dataset.MarshalJSON(metadata)
	if err != nil {
		log.Fatalf("Failed to encode metadata: %v", err)
	}
	if err := os.WriteFile(metadataPath, buf, 0o644); err != nil {
		log.Fatalf("Failed to create %s: %v", metadataPath, err)
	}

	log.Println("Metadata successfully overwritten with pure GBIF (CC0) data!")
}
