// Command migrate-metadata converts data/metadata.json from the legacy flat
// shape to the nested SpeciesRecord shape in place. Run once. Deterministic and
// network-free.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"

	"github.com/tphakala/openfauna/internal/dataset"
)

func main() {
	in := flag.String("in", "data/metadata.json", "flat metadata input")
	out := flag.String("out", "data/metadata.json", "nested metadata output")
	flag.Parse()

	data, err := os.ReadFile(*in)
	if err != nil {
		log.Fatalf("read: %v", err)
	}

	// Guard against re-running on already-migrated data. FlatMeta has no
	// taxonomy/links fields, so a nested file decodes to all-empty records and
	// would be silently overwritten with empty data. Refuse instead.
	var probe map[string]map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err == nil {
		for name, rec := range probe {
			if _, nested := rec["taxonomy"]; nested {
				log.Fatalf("input %s contains nested record %q (taxonomy present); refusing to overwrite", *in, name)
			}
			if _, nested := rec["links"]; nested {
				log.Fatalf("input %s contains nested record %q (links present); refusing to overwrite", *in, name)
			}
		}
	}

	var flat map[string]dataset.FlatMeta
	if err := json.Unmarshal(data, &flat); err != nil {
		log.Fatalf("parse: %v", err)
	}
	nested := make(map[string]dataset.SpeciesRecord, len(flat))
	for name, f := range flat {
		nested[name] = dataset.MigrateFlat(f)
	}
	buf, err := dataset.MarshalJSON(nested)
	if err != nil {
		log.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(*out, buf, 0o644); err != nil {
		log.Fatalf("write: %v", err)
	}
	log.Printf("migrated %d species to nested shape", len(nested))
}
