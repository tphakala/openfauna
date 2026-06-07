# BirdNET i18n Data

This repository manages internationalization (i18n) data for BirdNET-Go and associated models (like BirdNET V3.0, Perch, BattyBirdNET). It stores species translations in a sparse, merge-friendly format optimized for developer and community contributions.

## Why this repository exists

1. **Merge Conflict Prevention**: Managing thousands of species across 30+ languages in a single CSV file (like BirdNET V3.0's `taxonomy.csv`) guarantees massive merge conflicts. This repository stores translations per-language in discrete JSON files.
2. **Sparse Data**: Translators often only translate a subset of species (e.g., local birds). Per-language JSON files allow sparse data where you only add keys for the species you know.
3. **Multi-Model Support**: This repository combines labels from BirdNET V2.4, BirdNET V3.0, custom bat models, and the Perch model into one massive multi-language dictionary.

## Directory Structure

* `data/locales/`: Contains `[locale].json` files. This is the source of truth for translations.
* `cmd/compiler/`: A build tool that compiles all `[locale].json` files into a single, highly-optimized `translations.csv` format designed for fast ingestion by BirdNET-Go.
* `cmd/bootstrap/`: A one-time tool used to bootstrap the `data/locales` directory from an existing `taxonomy.csv`.

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

## For Developers

### Building the Compiled CSV

To compile the JSON files into a flat CSV for application ingest:

```bash
go run ./cmd/compiler -locales=data/locales -out=build/translations.csv
```

This will generate `build/translations.csv` with the schema: `scientific_name,locale,common_name`. This CSV can be natively embedded in BirdNET-Go for rapid database seeding during startup/migration.

### Bootstrapping from V3.0

If you ever need to re-import upstream translations from BirdNET V3.0's massive `taxonomy.csv`:

```bash
go run ./cmd/bootstrap -taxonomy=/path/to/taxonomy.csv -out=data/locales
```
Note: Bootstrapping will completely overwrite existing JSON files, so it should only be used to re-sync upstream data or as an initial seed.

## Locale Code Convention

We standardize locale codes to be lowercase with underscores instead of hyphens (e.g., `en_us`, `pt_br`, `zh_cn`) to align with BirdNET-Go's internal locale representation.
