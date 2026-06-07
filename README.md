# OpenFauna

**OpenFauna** is a universal species metadata and translation dictionary built for the global bioacoustics and environmental monitoring community. 

Originally built as a localization layer for [BirdNET-Go](https://github.com/tphakala/birdnet-go), OpenFauna has evolved into a master species encyclopedia that handles biological classification, multi-language common names, and taxonomic aliasing for *any* biological acoustic model (including BirdNET V3, Perch, and custom models like BattyBirdNET).

## Why OpenFauna?

Machine learning models (like Perch or BirdNET) output numeric indices that map to canonical Scientific Names (e.g., `Class 123 -> Turdus merula`). However, user-facing applications need rich presentation: translated common names, family classifications, photos, and Wikipedia links.

OpenFauna decouples the "dumb" AI models from the "smart" presentation layer:
1. You run inference using any ONNX/TFLite bioacoustics model.
2. The model outputs a scientific name.
3. You query OpenFauna for that scientific name to get translations in 30+ languages, taxonomic hierarchy (Order/Family), and external links.

### The Architecture
- **`data/locales/`**: Discrete JSON files mapping scientific names to translated common names. Managing thousands of species across 30+ languages in a single CSV file guarantees massive merge conflicts. This repository stores translations per-language in merge-friendly, sparse JSON formats.
- **`data/aliases.json`**: Centralized mapping of taxonomic reclassifications. When a species is renamed, you add the alias here, and it inherits all translations automatically.
- **`data/metadata.json`**: Contains rich taxonomy (Class, Order, Family) fetched from the GBIF Backbone Taxonomy, as well as deterministic Wikipedia URLs, keyed by scientific name.
- **`cmd/compiler/`**: A build tool that compiles all `[locale].json` and `metadata.json` files into flat, highly-optimized CSVs (`translations.csv` and `metadata.csv`) designed for fast ingestion by applications like BirdNET-Go.

## For Translators

To contribute a new translation:
1. Find your language file in `data/locales/` (e.g., `fr.json` for French). If it doesn't exist, create it.
2. Add the translation as a Key-Value pair where the key is the exact Scientific Name and the value is the Common Name.

```json
{
  "Abeillia abeillei": "Colibri d'Abeillé",
  "Vulpes vulpes": "Renard roux"
}
```

### Taxonomic Aliases

Species get reclassified over time (e.g., *Carduelis hornemanni* becomes *Acanthis hornemanni*). Instead of duplicating common names across all language files, add reclassifications to `data/aliases.json`:
```json
{
  "Carduelis hornemanni": "Acanthis hornemanni"
}
```
The compiler tool automatically resolves this mapping. When it runs, if a translation exists for `Acanthis hornemanni`, it will automatically inject the exact same translation into the output for `Carduelis hornemanni`.

OpenFauna also includes a tool to automatically generate these mappings for older legacy models (like BirdNET V2.4) by cross-referencing legacy labels with modern common names:
```bash
go run ./cmd/auto-alias
```

## For Developers

### Building the Compiled CSVs

To compile the JSON files into flat CSVs for application ingest:

```bash
go run ./cmd/compiler
```

This will generate two artifacts:
1. `build/translations.csv` with the schema: `scientific_name,locale,common_name`.
2. `build/metadata.csv` with the schema detailed below.

### Metadata Schema

The `build/metadata.csv` artifact provides a rich taxonomic and external-link layer for every species, designed to be joined with the translation data in your application's database.

| Column | Description | Source |
|---|---|---|
| `scientific_name` | The canonical scientific name of the species (Primary Key). | Target Models |
| `class` | Taxonomic Class (e.g., *Aves*, *Amphibia*). | GBIF Backbone API |
| `order` | Taxonomic Order (e.g., *Passeriformes*, *Anura*). | GBIF Backbone API |
| `family` | Taxonomic Family (e.g., *Corvidae*, *Hylidae*). | GBIF Backbone API |
| `family_common` | The English common name for the taxonomic family. | GBIF Backbone API |
| `wikipedia_url` | A deterministic link to the species' English Wikipedia article. | Auto-generated |
| `inaturalist_url` | The authoritative iNaturalist taxon URL. | iNaturalist Open Data S3 Dump |

This metadata allows downstream applications to instantly group detections by taxonomic family, build migration charts, and provide users with direct, accurate links to Wikipedia and iNaturalist to learn more about the species they detect.

### Future Metadata Expansion

The OpenFauna metadata schema is designed to be extensible. We are actively planning to expand the dataset to include:

1. **Curated Thumbnails**: A community-driven, human-curated repository of species thumbnails with standardized aspect ratios and dimensions, optimized for mobile and web UI dashboards.
2. **Conservation Status**: Integrating IUCN Red List data to highlight endangered or threatened species in detection streams.
3. **Regional Endemism**: Data mapping species to native geographic continents/regions to improve anomaly detection (e.g., detecting a European bird in North America).

*Note: To keep OpenFauna strictly open-source (CC BY-SA 4.0), we exclusively source taxonomy from CC0 providers like GBIF and iNaturalist Open Data. We do not ingest proprietary eBird or Clements taxonomy due to their non-commercial licensing restrictions.*

## Model Coverage

Currently, OpenFauna provides translation support across the major bioacoustics models:

| Model | Target Species | Supported by OpenFauna | Coverage |
|---|---|---|---|
| BirdNET V2.4 | 6,521 | 6,476 | 99.3% |
| BirdNET V3.0 | 11,560 | 10,806 | 93.5% |
| Perch V2 | 14,795 | 12,027 | 81.3% |
| BattyBirdNET | 88 | 87 | 98.9% |

These CSVs can be natively embedded in your application for rapid database seeding during startup.

### Bootstrapping from Upstream Models
The initial baseline of OpenFauna was bootstrapped from the amazing BirdNET+ V3.0 taxonomy. If you ever need to re-import upstream BirdNET translations:
```bash
go run ./cmd/bootstrap -taxonomy=/path/to/taxonomy.csv -out=data/locales
```

To import regional BattyBirdNET translations (from huggingface labels):
```bash
go run ./cmd/import-bats
```

### Fetching Taxonomy Metadata (GBIF & iNaturalist)
The taxonomy tree (Class, Order, Family) is CC0 Public Domain. To fetch taxonomy from the GBIF Backbone API:
```bash
go run ./cmd/fetch-gbif
```

To extract authoritative iNaturalist taxonomy URLs without querying their rate-limited API (streams directly from AWS Open Data):
```bash
go run ./cmd/fetch-inaturalist
```

## License and Attribution

OpenFauna is licensed under the **Creative Commons Attribution-ShareAlike 4.0 International (CC BY-SA 4.0)** license, matching the upstream BirdNET project.

Please see [ATTRIBUTION.md](ATTRIBUTION.md) for required credits to the original BirdNET authors (Cornell Lab of Ornithology and Chemnitz University of Technology) who provided the baseline translation data.
