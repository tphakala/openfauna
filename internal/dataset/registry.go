// Package dataset holds the OpenFauna nested-schema types, loaders, validators,
// and build-artifact emitters shared by cmd/compiler and the migration command.
package dataset

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Source is one external link provider's presentation defaults.
type Source struct {
	Name  string `json:"name"`
	Order int    `json:"order"`
	Icon  string `json:"icon"`
	URL   string `json:"url"`
}

// MediaSource is one image provider's presentation and thumbnail template.
type MediaSource struct {
	Name                string `json:"name"`
	AttributionRequired bool   `json:"attribution_required"`
	Thumb               string `json:"thumb"`
}

// LoadSources reads the link sources registry.
func LoadSources(path string) (map[string]Source, error) {
	var m map[string]Source
	if err := loadJSON(path, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// LoadMediaSources reads the media sources registry.
func LoadMediaSources(path string) (map[string]MediaSource, error) {
	var m map[string]MediaSource
	if err := loadJSON(path, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// ValidateSources rejects registry entries that cannot build a link.
func ValidateSources(s map[string]Source) error {
	for key, src := range s {
		if strings.TrimSpace(src.Name) == "" {
			return fmt.Errorf("source %q: empty name", key)
		}
		if strings.TrimSpace(src.URL) == "" {
			return fmt.Errorf("source %q: empty url", key)
		}
		if !strings.Contains(src.URL, "{id}") {
			return fmt.Errorf("source %q: url missing {id} placeholder", key)
		}
	}
	return nil
}

// ValidateMediaSources rejects media registry entries that cannot build a
// thumbnail URL, mirroring ValidateSources for the media registry.
func ValidateMediaSources(s map[string]MediaSource) error {
	for key, ms := range s {
		if strings.TrimSpace(ms.Name) == "" {
			return fmt.Errorf("media source %q: empty name", key)
		}
		if strings.TrimSpace(ms.Thumb) == "" {
			return fmt.Errorf("media source %q: empty thumb", key)
		}
		if !strings.Contains(ms.Thumb, "{id}") {
			return fmt.Errorf("media source %q: thumb missing {id} placeholder", key)
		}
	}
	return nil
}

func loadJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
