package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	taxonomyCSV := flag.String("taxonomy", "/home/thakala/src/birdnet-v3.0/geomodel/taxonomy.csv", "Path to BirdNET V3.0 taxonomy.csv")
	outDir := flag.String("out", "data/locales", "Directory to output locale JSON files")
	flag.Parse()

	if err := os.MkdirAll(*outDir, 0755); err != nil {
		log.Fatalf("Failed to create output directory: %v", err)
	}

	file, err := os.Open(*taxonomyCSV)
	if err != nil {
		log.Fatalf("Failed to open taxonomy CSV: %v", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	// Some fields might contain unescaped quotes or formatting quirks in large data sets
	reader.LazyQuotes = true
	reader.TrimLeadingSpace = true

	records, err := reader.ReadAll()
	if err != nil {
		log.Fatalf("Failed to read CSV records: %v", err)
	}

	if len(records) < 2 {
		log.Fatalf("CSV has fewer than 2 rows")
	}

	header := records[0]
	// Locate sci_name column
	sciNameIdx := -1
	for i, h := range header {
		if strings.TrimSpace(h) == "sci_name" {
			sciNameIdx = i
			break
		}
	}
	if sciNameIdx == -1 {
		log.Fatalf("sci_name column not found in taxonomy CSV")
	}

	// Identify locale columns
	// Format is usually "common_name_{locale}"
	localeIndices := make(map[int]string)
	for i, h := range header {
		h = strings.TrimSpace(h)
		if strings.HasPrefix(h, "common_name_") {
			locale := strings.TrimPrefix(h, "common_name_")
			// Normalize locale names: replace hyphens with underscores, lower case. 
			// V3.0 uses e.g. zh-CN, pt_PT, etc.
			// BirdNET-Go uses lowercase with underscores: zh_cn, pt_pt
			locale = strings.ToLower(strings.ReplaceAll(locale, "-", "_"))
			localeIndices[i] = locale
		} else if h == "com_name" {
			// Base english
			localeIndices[i] = "en"
		}
	}

	// Map: locale -> map[sciName]commonName
	translationsByLocale := make(map[string]map[string]string)
	for _, locale := range localeIndices {
		translationsByLocale[locale] = make(map[string]string)
	}

	// Process data rows
	for _, row := range records[1:] {
		if len(row) <= sciNameIdx {
			continue
		}
		sciName := strings.TrimSpace(row[sciNameIdx])
		if sciName == "" {
			continue
		}

		for colIdx, locale := range localeIndices {
			if colIdx < len(row) {
				comName := strings.TrimSpace(row[colIdx])
				if comName != "" {
					translationsByLocale[locale][sciName] = comName
				}
			}
		}
	}

	// Write JSON files
	for locale, translations := range translationsByLocale {
		if len(translations) == 0 {
			continue
		}

		outPath := filepath.Join(*outDir, fmt.Sprintf("%s.json", locale))
		outFile, err := os.Create(outPath)
		if err != nil {
			log.Fatalf("Failed to create file for locale %s: %v", locale, err)
		}

		encoder := json.NewEncoder(outFile)
		encoder.SetIndent("", "  ")
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(translations); err != nil {
			log.Fatalf("Failed to write JSON for locale %s: %v", locale, err)
		}
		outFile.Close()
		log.Printf("Wrote %s with %d translations", outPath, len(translations))
	}

	log.Println("Bootstrap complete.")
}
