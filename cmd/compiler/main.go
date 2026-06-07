package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	localesDir := flag.String("locales", "data/locales", "Directory containing locale JSON files")
	outputFile := flag.String("out", "build/translations.csv", "Output CSV file path")
	aliasesFile := flag.String("aliases", "data/aliases.json", "Path to taxonomic aliases JSON file")
	flag.Parse()

	// Ensure output directory exists
	outDir := filepath.Dir(*outputFile)
	if err := os.MkdirAll(outDir, 0755); err != nil {
		log.Fatalf("Failed to create output directory: %v", err)
	}

	out, err := os.Create(*outputFile)
	if err != nil {
		log.Fatalf("Failed to create output file: %v", err)
	}
	defer out.Close()

	writer := csv.NewWriter(out)
	defer writer.Flush()

	// Write header
	if err := writer.Write([]string{"scientific_name", "locale", "common_name"}); err != nil {
		log.Fatalf("Failed to write CSV header: %v", err)
	}

	files, err := os.ReadDir(*localesDir)
	if err != nil {
		log.Fatalf("Failed to read locales directory: %v", err)
	}

	var totalTranslations int

	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}

		locale := strings.TrimSuffix(file.Name(), ".json")
		filePath := filepath.Join(*localesDir, file.Name())

		data, err := os.ReadFile(filePath)
		if err != nil {
			log.Fatalf("Failed to read file %s: %v", filePath, err)
		}

		var translations map[string]string
		if err := json.Unmarshal(data, &translations); err != nil {
			log.Fatalf("Failed to parse JSON in %s: %v", filePath, err)
		}

		// Apply aliases if the canonical name is translated
		if aliasesData, err := os.ReadFile(*aliasesFile); err == nil {
			var aliases map[string]string
			if err := json.Unmarshal(aliasesData, &aliases); err == nil {
				for alias, canonical := range aliases {
					if strings.HasPrefix(alias, "_") {
						continue // skip comments
					}
					if commonName, ok := translations[canonical]; ok {
						if _, exists := translations[alias]; !exists {
							translations[alias] = commonName
						}
					}
				}
			}
		}

		// Sort scientific names for deterministic output
		var sciNames []string
		for name := range translations {
			sciNames = append(sciNames, name)
		}
		sort.Strings(sciNames)

		for _, sciName := range sciNames {
			commonName := translations[sciName]
			if strings.TrimSpace(commonName) == "" {
				continue
			}

			if err := writer.Write([]string{sciName, locale, commonName}); err != nil {
				log.Fatalf("Failed to write CSV row: %v", err)
			}
			totalTranslations++
		}
		log.Printf("Processed locale: %s (%d translations)", locale, len(translations))
	}

	log.Printf("Successfully compiled %d total translations into %s", totalTranslations, *outputFile)

	// Compile Metadata CSV if available
	metadataFile := filepath.Join(filepath.Dir(*aliasesFile), "metadata.json")
	if metadataData, err := os.ReadFile(metadataFile); err == nil {
		outMetadataFile := filepath.Join(filepath.Dir(*outputFile), "metadata.csv")
		outMeta, err := os.Create(outMetadataFile)
		if err != nil {
			log.Fatalf("Failed to create metadata CSV: %v", err)
		}
		defer outMeta.Close()

		metaWriter := csv.NewWriter(outMeta)
		defer metaWriter.Flush()

		if err := metaWriter.Write([]string{"scientific_name", "class", "order", "family", "family_common"}); err != nil {
			log.Fatalf("Failed to write metadata header: %v", err)
		}

		var metadata map[string]struct {
			Class        string `json:"class"`
			Order        string `json:"order"`
			Family       string `json:"family"`
			FamilyCommon string `json:"family_common"`
		}
		if err := json.Unmarshal(metadataData, &metadata); err == nil {
			var sciNames []string
			for name := range metadata {
				sciNames = append(sciNames, name)
			}
			sort.Strings(sciNames)

			for _, sciName := range sciNames {
				info := metadata[sciName]
				metaWriter.Write([]string{sciName, info.Class, info.Order, info.Family, info.FamilyCommon})
			}
			log.Printf("Successfully compiled %d metadata records into %s", len(metadata), outMetadataFile)
		}
	}
}
