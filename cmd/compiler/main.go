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

	"github.com/tphakala/openfauna/internal/dataset"
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

	// Load the alias map once; it is identical for every locale. A malformed
	// aliases file is fatal rather than silently dropping every alias.
	var aliases map[string]string
	if aliasesData, err := os.ReadFile(*aliasesFile); err == nil {
		if err := json.Unmarshal(aliasesData, &aliases); err != nil {
			log.Fatalf("Failed to parse %s: %v", *aliasesFile, err)
		}
	} else if !os.IsNotExist(err) {
		log.Fatalf("Failed to read %s: %v", *aliasesFile, err)
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

		// Apply aliases if the canonical name is translated.
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

	// Compile nested metadata, registries, and the versioned manifest.
	buildDir := filepath.Dir(*outputFile)
	dataDir := filepath.Dir(*aliasesFile)

	recs, err := dataset.LoadMetadata(filepath.Join(dataDir, "metadata.json"))
	if err != nil {
		log.Fatalf("Failed to load metadata: %v", err)
	}
	srcs, err := dataset.LoadSources(filepath.Join(dataDir, "sources.json"))
	if err != nil {
		log.Fatalf("Failed to load sources: %v", err)
	}
	if err := dataset.ValidateSources(srcs); err != nil {
		log.Fatalf("Invalid sources registry: %v", err)
	}
	mediaSrcs, err := dataset.LoadMediaSources(filepath.Join(dataDir, "media_sources.json"))
	if err != nil {
		log.Fatalf("Failed to load media sources: %v", err)
	}
	if err := dataset.ValidateMediaSources(mediaSrcs); err != nil {
		log.Fatalf("Invalid media sources registry: %v", err)
	}
	media, err := dataset.LoadMedia(filepath.Join(dataDir, "media.json"))
	if err != nil {
		log.Fatalf("Failed to load media: %v", err)
	}
	if err := dataset.ValidateMedia(media, mediaSrcs); err != nil {
		log.Fatalf("Invalid media: %v", err)
	}
	if unknown := dataset.MergeMedia(recs, media); len(unknown) > 0 {
		log.Printf("Warning: %d media entries reference unknown species and were dropped: %v", len(unknown), unknown)
	}

	if err := dataset.EmitJSONL(filepath.Join(buildDir, "metadata.jsonl"), recs); err != nil {
		log.Fatalf("Failed to emit metadata.jsonl: %v", err)
	}
	// Export the taxonomic alias map as a machine-readable artifact. The
	// translation loop above only uses aliases to duplicate common names; this
	// is the only place the scientific-to-scientific mapping itself reaches
	// consumers (e.g. for range-filter and detection name normalization).
	aliasCount, err := dataset.EmitAliases(filepath.Join(buildDir, "aliases.json"), aliases)
	if err != nil {
		log.Fatalf("Failed to emit aliases.json: %v", err)
	}
	if err := dataset.CopyFile(filepath.Join(dataDir, "sources.json"), filepath.Join(buildDir, "sources.json")); err != nil {
		log.Fatalf("Failed to copy sources.json: %v", err)
	}
	if err := dataset.CopyFile(filepath.Join(dataDir, "media_sources.json"), filepath.Join(buildDir, "media_sources.json")); err != nil {
		log.Fatalf("Failed to copy media_sources.json: %v", err)
	}
	// OpenFauna has no self-provenance string; the consumer's refresh-data.sh
	// overrides the vendored manifest source with the openfauna commit SHA.
	if err := dataset.EmitManifest(filepath.Join(buildDir, "manifest.json"), len(recs), aliasCount, "openfauna-build"); err != nil {
		log.Fatalf("Failed to emit manifest: %v", err)
	}
	log.Printf("Compiled %d nested metadata records and %d taxonomic aliases", len(recs), aliasCount)
}
