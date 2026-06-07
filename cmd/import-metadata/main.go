package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
)

type GenusTaxonomy struct {
	Genera map[string]GenusInfo `json:"genera"`
}

type GenusInfo struct {
	Class        string   `json:"class"`
	Family       string   `json:"family"`
	FamilyCommon string   `json:"family_common"`
	Order        string   `json:"order"`
	Species      []string `json:"species"`
}

type SpeciesMetadata struct {
	Class        string `json:"class"`
	Order        string `json:"order"`
	Family       string `json:"family"`
	FamilyCommon string `json:"family_common,omitempty"`
	WikipediaURL string `json:"wikipedia_url,omitempty"`
}

func main() {
	sourcePath := "/home/thakala/src/birdnet-go/internal/classifier/data/genus_taxonomy.json"
	outPath := "data/metadata.json"

	data, err := os.ReadFile(sourcePath)
	if err != nil {
		log.Fatalf("Failed to read source taxonomy: %v", err)
	}

	var genusData GenusTaxonomy
	if err := json.Unmarshal(data, &genusData); err != nil {
		log.Fatalf("Failed to parse genus taxonomy: %v", err)
	}

	metadata := make(map[string]SpeciesMetadata)
	speciesCount := 0

	for _, info := range genusData.Genera {
		for _, speciesName := range info.Species {
			wikiURL := "https://en.wikipedia.org/wiki/" + strings.ReplaceAll(speciesName, " ", "_")
			metadata[speciesName] = SpeciesMetadata{
				Class:        info.Class,
				Order:        info.Order,
				Family:       info.Family,
				FamilyCommon: info.FamilyCommon,
				WikipediaURL: wikiURL,
			}
			speciesCount++
		}
	}

	// Ensure data dir exists
	os.MkdirAll(filepath.Dir(outPath), 0755)

	outFile, err := os.Create(outPath)
	if err != nil {
		log.Fatalf("Failed to create %s: %v", outPath, err)
	}
	defer outFile.Close()

	encoder := json.NewEncoder(outFile)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(metadata); err != nil {
		log.Fatalf("Failed to encode metadata JSON: %v", err)
	}

	log.Printf("Successfully imported metadata for %d species into %s", speciesCount, outPath)
}
