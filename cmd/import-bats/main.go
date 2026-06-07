package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	labelsDir := "/home/thakala/bats/hf-upload/labels"
	localesDir := "data/locales"

	files, err := os.ReadDir(labelsDir)
	if err != nil {
		log.Fatalf("Failed to read labels directory: %v", err)
	}

	updatesByLocale := make(map[string]int)

	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".txt") {
			continue
		}

		name := file.Name()
		// Only parse files with locales (e.g. BattyBirdNET-EU-256kHz_Labels_de.txt)
		// Skip baseline files without locale (e.g. BattyBirdNET-EU-256kHz_Labels.txt)
		if !strings.Contains(name, "_Labels_") {
			continue
		}

		parts := strings.Split(name, "_Labels_")
		if len(parts) != 2 {
			continue
		}
		
		locale := strings.TrimSuffix(parts[1], ".txt")
		locale = strings.ToLower(strings.ReplaceAll(locale, "-", "_"))

		lines, err := readLines(filepath.Join(labelsDir, name))
		if err != nil {
			log.Printf("Failed to read %s: %v", name, err)
			continue
		}

		// Load existing locale JSON or create new
		localePath := filepath.Join(localesDir, fmt.Sprintf("%s.json", locale))
		var translations map[string]string
		
		data, err := os.ReadFile(localePath)
		if err == nil {
			if err := json.Unmarshal(data, &translations); err != nil {
				log.Fatalf("Failed to parse %s: %v", localePath, err)
			}
		} else if os.IsNotExist(err) {
			translations = make(map[string]string)
		} else {
			log.Fatalf("Failed to read %s: %v", localePath, err)
		}

		added := 0
		for _, line := range lines {
			parts := strings.SplitN(line, "_", 2)
			if len(parts) == 2 {
				sci := strings.TrimSpace(parts[0])
				com := strings.TrimSpace(parts[1])
				
				// Add or update if not empty
				if sci != "" && com != "" {
					if existing, ok := translations[sci]; !ok || existing != com {
						translations[sci] = com
						added++
					}
				}
			}
		}

		if added > 0 {
			// Save back to JSON
			outFile, err := os.Create(localePath)
			if err != nil {
				log.Fatalf("Failed to create %s: %v", localePath, err)
			}
			encoder := json.NewEncoder(outFile)
			encoder.SetIndent("", "  ")
			encoder.SetEscapeHTML(false)
			if err := encoder.Encode(translations); err != nil {
				log.Fatalf("Failed to encode JSON for %s: %v", locale, err)
			}
			outFile.Close()
			updatesByLocale[locale] += added
		}
	}

	for loc, count := range updatesByLocale {
		log.Printf("Added/updated %d bat translations for locale: %s", count, loc)
	}
	log.Println("Bat data import complete.")
}

func readLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines, scanner.Err()
}
