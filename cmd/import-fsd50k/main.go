package main

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	perch, _ := readLines("/home/thakala/src/Perch-v2/labels.txt")
	var fsdLabels []string
	for _, s := range perch {
		if s == "inat2024_fsd50k" || s == "Unknown" || strings.HasPrefix(s, "Noise") {
			continue
		}
		isFSD50k := strings.Contains(s, "_") || strings.Contains(s, "(") || !strings.Contains(s, " ")
		if isFSD50k {
			fsdLabels = append(fsdLabels, s)
		}
	}

	locales, _ := os.ReadDir("data/locales")
	for _, locFile := range locales {
		if !strings.HasSuffix(locFile.Name(), ".json") {
			continue
		}
		localeName := strings.TrimSuffix(locFile.Name(), ".json")
		if strings.HasPrefix(localeName, "en") {
			continue
		}

		tl := localeName
		if tl == "zh_cn" {
			tl = "zh-CN"
		} else if tl == "pt_pt" {
			tl = "pt-PT"
		} else if strings.Contains(tl, "_") {
			tl = strings.Split(tl, "_")[0]
		}

		path := filepath.Join("data/locales", locFile.Name())
		data, _ := os.ReadFile(path)
		var loc map[string]string
		json.Unmarshal(data, &loc)

		var toTranslate []string
		var originalKeys []string

		for _, s := range fsdLabels {
			if _, exists := loc[s]; !exists {
				toTranslate = append(toTranslate, strings.ReplaceAll(s, "_", " "))
				originalKeys = append(originalKeys, s)
			}
		}

		if len(toTranslate) > 0 {
			log.Printf("Translating %d labels for %s (tl=%s)...", len(toTranslate), localeName, tl)
			translatedLines := translateBatch(toTranslate, tl)
			if len(translatedLines) == len(originalKeys) {
				for i, k := range originalKeys {
					loc[k] = translatedLines[i]
				}
				
				outFile, _ := os.Create(path)
				encoder := json.NewEncoder(outFile)
				encoder.SetIndent("", "  ")
				encoder.Encode(loc)
				outFile.Close()
				log.Printf("Saved %s", path)
			} else {
				log.Printf("Error: translated %d lines, expected %d", len(translatedLines), len(originalKeys))
			}
			time.Sleep(2 * time.Second)
		}
	}
}

func translateBatch(texts []string, tl string) []string {
	combined := strings.Join(texts, "\n")
	apiURL := "https://translate.googleapis.com/translate_a/single?client=gtx&sl=en&tl=" + tl + "&dt=t"
	
	req, _ := http.NewRequest("POST", apiURL, strings.NewReader("q="+url.QueryEscape(combined)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	
	body, _ := io.ReadAll(resp.Body)
	
	var result []interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil
	}
	
	var translated []string
	if len(result) > 0 {
		lines := result[0].([]interface{})
		fullText := ""
		for _, line := range lines {
			if lineArr, ok := line.([]interface{}); ok && len(lineArr) > 0 {
				if s, ok := lineArr[0].(string); ok {
					fullText += s
				}
			}
		}
		
		for _, l := range strings.Split(fullText, "\n") {
			translated = append(translated, strings.TrimSpace(l))
		}
	}
	
	return translated
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
