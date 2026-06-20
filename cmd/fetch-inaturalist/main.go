package main

import (
	"compress/gzip"
	"context"
	"encoding/csv"
	"errors"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"

	"github.com/tphakala/openfauna/internal/dataset"
)

const taxaDumpURL = "https://inaturalist-open-data.s3.amazonaws.com/taxa.csv.gz"

func main() {
	metadataPath := flag.String("metadata", "data/metadata.json", "nested metadata file to update in place")
	flag.Parse()

	metadata, err := dataset.LoadMetadata(*metadataPath)
	if err != nil {
		log.Fatalf("Failed to load metadata: %v", err)
	}

	log.Println("Downloading and streaming iNaturalist taxa dump...")
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, taxaDumpURL, nil)
	if err != nil {
		log.Fatalf("Failed to build request: %v", err)
	}
	// The dump is a gzip file (S3 serves it with Content-Encoding: gzip). Request
	// gzip explicitly so net/http does NOT transparently decompress the body, then
	// own the decompression below. Relying on transparent decompression is
	// fragile: a custom transport, an explicit Accept-Encoding elsewhere, or a
	// server header change would silently yield zero rows with no error.
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("Failed to download taxa dump: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Failed to download taxa dump: status %d", resp.StatusCode)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		log.Fatalf("Failed to open gzip stream: %v", err)
	}
	defer gz.Close()

	csvReader := csv.NewReader(gz)
	csvReader.Comma = '\t'
	csvReader.ReuseRecord = true
	csvReader.LazyQuotes = true
	// The dump occasionally has rows with an irregular field count; do not let one
	// abort the whole scan (the default enforces the header's field count).
	csvReader.FieldsPerRecord = -1

	// Resolve column positions by header name (taxon_id, name) so a future column
	// reorder in the dump cannot silently match or persist the wrong field.
	header, err := csvReader.Read()
	if err != nil {
		log.Fatalf("Failed to read header: %v", err)
	}
	columnIndex := make(map[string]int, len(header))
	for i, column := range header {
		columnIndex[column] = i
	}
	idCol, hasID := columnIndex["taxon_id"]
	nameCol, hasName := columnIndex["name"]
	if !hasID || !hasName {
		log.Fatalf("Unexpected taxa dump header: %v", header)
	}

	scanned, added := 0, 0
	for {
		row, err := csvReader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// A real read/parse error (truncated download, corrupt gzip) is not a
			// clean end of stream: fail instead of saving a partially scanned file.
			log.Fatalf("CSV read error after %d rows: %v", scanned, err)
		}
		scanned++
		if len(row) <= idCol || len(row) <= nameCol {
			continue
		}
		name := row[nameCol]
		rec, exists := metadata[name]
		if !exists {
			continue
		}
		if _, has := rec.Links["inaturalist"]; has {
			continue
		}
		if rec.Links == nil {
			rec.Links = map[string]dataset.Link{}
		}
		rec.Links["inaturalist"] = dataset.Link{ID: row[idCol]}
		metadata[name] = rec
		added++
	}
	if scanned == 0 {
		log.Fatalf("Scanned 0 taxa rows; the dump did not decompress or parse (check the download and gzip handling)")
	}

	log.Printf("Scanned %d taxa rows, added %d new iNaturalist ids. Saving %s...", scanned, added, *metadataPath)
	buf, err := dataset.MarshalJSON(metadata)
	if err != nil {
		log.Fatalf("Failed to encode metadata: %v", err)
	}
	if err := os.WriteFile(*metadataPath, buf, 0o644); err != nil {
		log.Fatalf("Failed to write metadata: %v", err)
	}
	log.Println("Done!")
}
