package main

import (
	"bufio"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	// 1. Load current aliases
	aliasesPath := "data/aliases.json"
	aliasesData, _ := os.ReadFile(aliasesPath)
	var aliases map[string]string
	json.Unmarshal(aliasesData, &aliases)
	if aliases == nil {
		aliases = make(map[string]string)
	}

	// 2. Load en.json to create reverse mapping
	enData, _ := os.ReadFile("data/locales/en.json")
	var enLoc map[string]string
	json.Unmarshal(enData, &enLoc)

	reverseEn := make(map[string]string)
	for sci, com := range enLoc {
		reverseEn[com] = sci
	}

	// 3. Load OpenFauna total keys to know what's missing
	openfauna := make(map[string]bool)
	locales, _ := os.ReadDir("data/locales")
	for _, locFile := range locales {
		if strings.HasSuffix(locFile.Name(), ".json") {
			data, _ := os.ReadFile(filepath.Join("data/locales", locFile.Name()))
			var loc map[string]string
			if err := json.Unmarshal(data, &loc); err == nil {
				for k := range loc {
					openfauna[k] = true
				}
			}
		}
	}
	for k, v := range aliases {
		if openfauna[v] {
			openfauna[k] = true
		}
	}

	// 4. Resolve V2.4 Gaps
	bn24Path := "/home/thakala/src/birdnet-go/internal/classifier/data/labels/V2.4/BirdNET_GLOBAL_6K_V2.4_Labels_en_us.txt"
	bn24, _ := readLines(bn24Path)

	resolvedCount := 0
	for _, l := range bn24 {
		parts := strings.SplitN(l, "_", 2)
		if len(parts) == 2 {
			oldSci := strings.TrimSpace(parts[0])
			com := strings.TrimSpace(parts[1])

			if strings.HasPrefix(oldSci, "Noise") || oldSci == "Unknown" {
				continue
			}

			// If it's missing in openfauna
			if !openfauna[oldSci] {
				// Try to find modern scientific name by matching the common name
				if modernSci, ok := reverseEn[com]; ok {
					aliases[oldSci] = modernSci
					openfauna[oldSci] = true
					resolvedCount++
				} else {
					// Hardcoded edge case fallback: try to find common name in lowercase
					for s, c := range enLoc {
						if strings.ToLower(c) == strings.ToLower(com) {
							aliases[oldSci] = s
							openfauna[oldSci] = true
							resolvedCount++
							break
						}
					}
				}
			}
		}
	}

	// Also add noise-us for BattyBirdNET
	aliases["noise-us"] = "Noise"

	// 5. Save aliases
	outFile, _ := os.Create(aliasesPath)
	encoder := json.NewEncoder(outFile)
	encoder.SetIndent("", "  ")
	encoder.Encode(aliases)
	outFile.Close()

	log.Printf("Successfully auto-resolved %d taxonomic gaps via common name matching", resolvedCount)
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		l := strings.TrimSpace(scanner.Text())
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines, scanner.Err()
}
