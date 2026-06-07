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
2. `build/metadata.csv` with the schema: `scientific_name,class,order,family,family_common,wikipedia_url`.

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

### Fetching Taxonomy Metadata (GBIF)
The taxonomy tree (Class, Order, Family) is completely CC0 Public Domain. If you add new species, you can automatically fetch their taxonomy from the GBIF Backbone API:
```bash
go run ./cmd/fetch-gbif
```

## License and Attribution

OpenFauna is licensed under the **Creative Commons Attribution-ShareAlike 4.0 International (CC BY-SA 4.0)** license, matching the upstream BirdNET project.

Please see [ATTRIBUTION.md](ATTRIBUTION.md) for required credits to the original BirdNET authors (Cornell Lab of Ornithology and Chemnitz University of Technology) who provided the baseline translation data.
