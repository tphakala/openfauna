package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const labelsDir = "data/validation/labels"

// modelOrder fixes the row order and display names of the README coverage table.
var modelOrder = []struct{ stem, display string }{
	{"birdnet-v2.4", "BirdNET V2.4"},
	{"birdnet-v3.0-preview3.1", "BirdNET V3.0 preview3.1"},
	{"birdnet-v3.0-preview4", "BirdNET V3.0 preview4"},
	{"birdnet-plus-v3.0", "BirdNET+ V3.0"},
	{"perch-v2", "Perch V2"},
	{"battybirdnet", "BattyBirdNET"},
}

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

	// 2. Model label manifests, in README order; unlisted manifests follow by stem.
	manifests, err := filepath.Glob(filepath.Join(labelsDir, "*.txt"))
	if err != nil {
		log.Fatalf("list manifests: %v", err)
	}
	if len(manifests) == 0 {
		log.Fatalf("no label manifests in %s", labelsDir)
	}
	display := map[string]string{}
	order := map[string]int{}
	for i, mo := range modelOrder {
		display[mo.stem] = mo.display
		order[mo.stem] = i
	}
	stemOf := func(path string) string { return strings.TrimSuffix(filepath.Base(path), ".txt") }
	rank := func(path string) int {
		if i, ok := order[stemOf(path)]; ok {
			return i
		}
		return len(modelOrder)
	}
	sort.SliceStable(manifests, func(i, j int) bool { return rank(manifests[i]) < rank(manifests[j]) })
	for _, m := range manifests {
		names, err := readLines(m)
		if err != nil {
			log.Fatalf("read %s: %v", m, err)
		}
		name := display[stemOf(m)]
		if name == "" {
			name = stemOf(m)
		}
		calcCoverage(name, names)
	}

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
