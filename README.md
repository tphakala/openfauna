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
- **`data/metadata.json`**: Per-species record keyed by scientific name, holding nested `taxonomy` (Class, Order, Family from the GBIF Backbone) and `links` (external references stored as stable ids, e.g. an iNaturalist taxon id), plus an optional `media` array.
- **`data/sources.json`** and **`data/media_sources.json`**: Registries that define each external source once (display name, ordering, icon hint, and a URL template with `{id}`/`{lang}` placeholders). Adding a new link source is a data-only change: add it here plus a `links` id per species, with no consumer code change.
- **`cmd/compiler/`**: A build tool that compiles the locale files into `translations.csv` and the nested metadata into `metadata.jsonl` (one JSON object per line) alongside the registries and a versioned `manifest.json`, designed for fast, streaming ingestion by applications like BirdNET-Go.

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

### Building the Compiled Artifacts

To compile the JSON sources into the artifacts applications ingest:

```bash
go run ./cmd/compiler
```

This will generate these artifacts:
1. `build/translations.csv` with the schema: `scientific_name,locale,common_name`.
2. `build/metadata.jsonl` with one nested JSON object per species (schema below).
3. `build/sources.json` and `build/media_sources.json`: copies of the source registries.
4. `build/manifest.json` carrying the `schema_version` (currently `2.0.0`) consumers gate on.

The `build/` artifacts are committed and reproducible; CI fails if they drift from the data.

> Breaking change (schema 2.0.0): the old flat `build/metadata.csv` has been removed. Consumers must read `build/metadata.jsonl` and gate on `build/manifest.json` `schema_version` (major version 2). The previous flat columns now live as nested `taxonomy` and `links` per record.

### Validating the Data

A static validator runs a set of deterministic, network-free checks over the
locale files, `aliases.json` and `metadata.json`:

```bash
python3 scripts/validate.py
```

It checks JSON structure (sorted keys, trailing newline), alias integrity (no
scientific name is both a full entry and an alias), scientific-name
placeholders that would shadow a real English common name, the lowercase naming
convention for `fi`/`sv`/`no` species names, stray em/en dashes, and that every
`data/sources.json` entry has a name and a `{id}` URL template. It exits
non-zero on any problem, and runs automatically on pull requests via
`.github/workflows/validate.yml` (which also verifies the compiled artifacts are
in sync). When a species genuinely has no native common name in a locale, list
it in `data/validation/shadow_allowlist.json` so the placeholder check accepts it.

### Metadata Schema

The `build/metadata.jsonl` artifact provides a rich taxonomic and external-link layer for every species, one JSON object per line, designed for streaming ingestion and joining with the translation data. Each line carries a `scientific_name`, a nested `taxonomy` object, a `links` map, and an optional `media` array:

```json
{"scientific_name":"Aquila chrysaetos","taxonomy":{"class":"Aves","order":"Accipitriformes","family":"Accipitridae","family_common":""},"links":{"inaturalist":{"id":"5074"},"wikipedia":{"id":"Q41181"}}}
{"scientific_name":"Vulpes vulpes","taxonomy":{"class":"Mammalia","order":"Carnivora","family":"Canidae","family_common":""},"links":{"inaturalist":{"id":"42069"},"wikipedia":{"id":"Q8332"}},"media":[{"source":"wikimedia","id":"Fox at the British Wildlife Centre, Newchapel, Surrey - geograph.org.uk - 2221750.jpg","format":"image/jpeg","width":1865,"height":2802,"aspect_ratio":0.67,"orientation":"portrait","license":"https://creativecommons.org/licenses/by-sa/2.0","creator":"Peter Trimming","rightsHolder":"Peter Trimming"}]}
```

`taxonomy` carries `class`/`order`/`family`/`family_common` (GBIF Backbone). `links` is keyed by source id; each entry stores a stable `id` (and an optional `url` override), which the consumer turns into a URL at render time using the matching `data/sources.json` template (so a link can resolve to the reader's language). The Wikipedia `id` is a language-agnostic Wikidata QID that the registry template resolves to the reader's language via `Special:GoToLinkedPage`; the roughly 13% of species that have no confident QID keep a `url` override pointing at the English article, so Wikipedia coverage stays complete. `media` holds curated, CC-clean image references (license, attribution, dimensions, aspect ratio) for non-bird taxa, by Commons filename, with the thumbnail URL derived at render time. Storing ids rather than baked URLs is what lets a new source be added as data only.

The presentation of each source lives once in the registries:

| File | Purpose |
|---|---|
| `build/sources.json` | Per link source: `name`, `order`, `icon`, and a `url` template with `{id}`/`{lang}`. |
| `build/media_sources.json` | Per image source: `name`, `attribution_required`, and a `thumb` template. |
| `build/manifest.json` | `schema_version` (SemVer), `species_count`, and `source`; consumers read it first and gate on the major version. |

### Future Metadata Expansion

The OpenFauna metadata schema is designed to be extensible. The `links` registry already makes a new external link source a data-only addition. Landed: language-agnostic Wikidata QID Wikipedia links, and the `media` array seeded with CC-clean Wikimedia Commons images for non-bird taxa (stable Commons filename plus license, attribution, dimensions, and aspect ratio, aligned to Audubon Core, with URLs derived at render time). Planned next:

1. **Broader Curated Media**: extend media to iNaturalist Open Data sources, add hand-curated portrait/landscape pairs, and widen non-bird coverage beyond the first Wikimedia P18 pass.
2. **Conservation Status**: Integrating IUCN Red List data to highlight endangered or threatened species in detection streams.
3. **Regional Endemism**: Data mapping species to native geographic continents/regions to improve anomaly detection (e.g., detecting a European bird in North America).

*Note: To keep OpenFauna strictly open-source (CC BY-SA 4.0), we source taxonomy from CC0 providers (GBIF, iNaturalist Open Data) and multilingual common names from the IOC World Bird List (CC BY 3.0), GBIF and Wikidata. We do not ingest proprietary eBird or Clements taxonomy, nor BirdNET's eBird-derived localized label files, due to their non-commercial licensing restrictions.*

## Model Coverage

Currently, OpenFauna provides translation support across the major bioacoustics models:

| Model | Target Species | Supported by OpenFauna | Coverage |
|---|---|---|---|
| BirdNET V2.4 | 6,521 | 6,521 | 100.0% |
| BirdNET V3.0 | 11,560 | 10,932 | 94.6% |
| Perch V2 | 14,795 | 12,580 | 85.0% |
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

To upgrade each Wikipedia link from an English article title to a language-agnostic Wikidata QID (cross-checking the enwiki sitelink against the P225 taxon-name SPARQL lookup) and to seed CC-clean Wikimedia Commons images for non-bird taxa from the Wikidata P18 claim:
```bash
go run ./cmd/fetch-wikidata
```
It updates `data/metadata.json` and `data/media.json` in place and writes a cross-check report to `data/validation/wikidata_report.json`. Re-running is safe: links that already carry a QID are skipped, and any species without a confident QID keeps its working English-title fallback. Use `--limit N` for a bounded pilot run and `--skip-media` to resolve QIDs only.

### Backfilling Localized Common Names (IOC & GBIF)

Per-locale common names are populated from open, license-compatible sources, never from a model's own (often eBird-derived) label files. `scripts/backfill_v24_open.py` fills the names a model needs but OpenFauna is missing, using the IOC World Bird List Multilingual file (CC BY 3.0) as the primary source and GBIF Backbone vernacular names (CC0) as a supplement. It is additive only and never overwrites a curated entry.

```bash
# Requires: pip install openpyxl, plus the IOC Multilingual .xlsx (CC BY 3.0):
#   https://www.worldbirdnames.org/Multiling%20IOC%2015.2.xlsx
python scripts/backfill_v24_open.py \
    --ioc /path/to/ioc.xlsx \
    --labels /path/to/BirdNET_GLOBAL_6K_V2.4_Labels_en_us.txt \
    --apply
```

Without `--apply` it runs a dry run and prints a per-locale fill report. Quality filters reject machine-translation artifacts such as wrong-script names, embedded fragments, and packed name lists. After running it, recompile the CSVs with `go run ./cmd/compiler`.

## License and Attribution

OpenFauna is licensed under the **Creative Commons Attribution-ShareAlike 4.0 International (CC BY-SA 4.0)** license, matching the upstream BirdNET project.

Please see [ATTRIBUTION.md](ATTRIBUTION.md) for required credits to the original BirdNET authors (Cornell Lab of Ornithology and Chemnitz University of Technology) who provided the baseline translation data.
