package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	// 1. Load OpenFauna Species
	openfauna := make(map[string]bool)

	locales, _ := os.ReadDir("data/locales")
	for _, locFile := range locales {
		if strings.HasSuffix(locFile.Name(), ".json") {
			data, err := os.ReadFile(filepath.Join("data/locales", locFile.Name()))
			if err == nil {
				var loc map[string]string
				if err := json.Unmarshal(data, &loc); err == nil {
					for k := range loc {
						openfauna[k] = true
					}
				}
			}
		}
	}

	aliasesData, _ := os.ReadFile("data/aliases.json")
	var aliases map[string]string
	if err := json.Unmarshal(aliasesData, &aliases); err == nil {
		for k, v := range aliases {
			if openfauna[v] {
				openfauna[k] = true
			}
		}
	}

	results := []struct {
		Model    string
		Total    int
		Covered  int
		Coverage float64
	}{}

	// Function to calculate coverage
	calcCoverage := func(name string, species []string) {
		total := 0
		covered := 0
		for _, s := range species {
			// Skip noise/unknown classes
			if strings.HasPrefix(s, "Noise") || s == "Unknown" || s == "inat2024_fsd50k" {
				continue
			}
			total++
			if openfauna[s] {
				covered++
			}
		}

		var pct float64
		if total > 0 {
			pct = float64(covered) / float64(total) * 100.0
		}

		results = append(results, struct {
			Model    string
			Total    int
			Covered  int
			Coverage float64
		}{name, total, covered, pct})
	}

	// 2. BirdNET 2.4
	bn24, _ := readLines("/home/thakala/src/birdnet-go/internal/classifier/data/labels/V2.4/BirdNET_GLOBAL_6K_V2.4_Labels_en_us.txt")
	var s24 []string
	for _, l := range bn24 {
		p := strings.Split(l, "_")
		s24 = append(s24, strings.TrimSpace(p[0]))
	}
	calcCoverage("BirdNET V2.4", s24)

	// 3. BirdNET 3.0
	bn30, _ := readLines("/home/thakala/src/birdnet-v3.0/acoustic/BirdNET+_V3.0-preview3_Global_11K_Labels.csv")
	var s30 []string
	for i, l := range bn30 {
		if i == 0 {
			continue
		} // skip header
		p := strings.Split(l, ";")
		if len(p) > 2 {
			s30 = append(s30, strings.TrimSpace(p[2]))
		}
	}
	calcCoverage("BirdNET V3.0", s30)

	// 4. Perch V2
	perch, _ := readLines("/home/thakala/src/Perch-v2/labels.txt")
	calcCoverage("Perch V2", perch)

	// 5. BattyBirdNET
	var batSpecies []string
	batFiles, _ := os.ReadDir("/home/thakala/bats/hf-upload/labels")
	for _, f := range batFiles {
		if strings.HasSuffix(f.Name(), "_Labels.txt") {
			lines, _ := readLines(filepath.Join("/home/thakala/bats/hf-upload/labels", f.Name()))
			for _, l := range lines {
				p := strings.Split(l, "_")
				if len(p) > 0 {
					batSpecies = append(batSpecies, strings.TrimSpace(p[0]))
				}
			}
		}
	}
	// unique bats
	uniqBats := make(map[string]bool)
	var finalBats []string
	for _, b := range batSpecies {
		if !uniqBats[b] {
			uniqBats[b] = true
			finalBats = append(finalBats, b)
		}
	}
	calcCoverage("BattyBirdNET", finalBats)

	// 6. Output Table
	fmt.Println("## Model Coverage")
	fmt.Println()
	fmt.Println("| Model | Target Species | Supported by OpenFauna | Coverage |")
	fmt.Println("|---|---|---|---|")
	for _, r := range results {
		fmt.Printf("| %s | %d | %d | %.1f%% |\n", r.Model, r.Total, r.Covered, r.Coverage)
	}
	fmt.Println()
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
