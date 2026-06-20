package dataset

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMetadataNested(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "metadata.json")
	content := `{
  "Aquila chrysaetos": {
    "taxonomy": {"class": "Aves", "order": "Accipitriformes", "family": "Accipitridae", "family_common": "Hawks, Eagles, and Kites"},
    "links": {"wikipedia": {"id": "Golden_eagle"}, "inaturalist": {"id": "5305"}}
  }
}
`
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadMetadata(p)
	if err != nil {
		t.Fatalf("LoadMetadata: %v", err)
	}
	rec := got["Aquila chrysaetos"]
	if rec.Taxonomy.Class != "Aves" {
		t.Fatalf("class = %q", rec.Taxonomy.Class)
	}
	if rec.Links["inaturalist"].ID != "5305" {
		t.Fatalf("inat id = %q", rec.Links["inaturalist"].ID)
	}
}

func TestLoadMetadataError(t *testing.T) {
	if _, err := LoadMetadata(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("expected error for missing file")
	}
}
