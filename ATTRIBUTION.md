# Attribution and Data Sources

OpenFauna is an open-source biological metadata and translation dictionary built for the global bioacoustics and environmental monitoring community. 

This project aggregates data from several incredible scientific institutions and open-source projects. We deeply respect their terms of use and require all users of OpenFauna to provide similar attribution in their derived works.

## 1. Baseline Translations (BirdNET & BattyBirdNET)

The baseline multi-language common name translations (spanning thousands of species across 30+ languages) were bootstrapped directly from the **BirdNET+ V3.0** developer preview taxonomy dataset and the **BattyBirdNET** open-source datasets.

These translations are released under the **Creative Commons Attribution-ShareAlike 4.0 International (CC BY-SA 4.0)** license.

**Contributors & Institutions:**
- K. Lisa Yang Center for Conservation Bioacoustics (Cornell University)
- Chemnitz University of Technology
- Museum für Naturkunde Berlin
- Stefan Kahl and the extended BirdNET research team
- The BattyBirdNET community

**References:**
- Kahl et al. (2021): *BirdNET: A deep learning solution for avian diversity monitoring.*
- Lasseck (2018): *Audio-based Bird Species Identification with Deep Convolutional Neural Networks.*

## 2. Taxonomy Classification Metadata (Class, Order, Family)

The taxonomic metadata containing Class, Order, and Family tree information is derived entirely from the **GBIF Backbone Taxonomy**.

*License Note:* The GBIF Backbone Taxonomy is released into the public domain under **CC0 1.0**. (https://doi.org/10.15468/39omei)
This means the taxonomic tree data (Class, Order, Family) inside OpenFauna has absolutely no commercial restrictions.

## 3. External Links & Enrichment

As OpenFauna expands to include external identifiers and links, those data points are sourced directly from public, open-access databases:

- **Wikidata**: Each species' Wikipedia link is stored as a language-agnostic Wikidata QID (CC0 1.0) that the consumer resolves to the reader's language at render time. Wikidata is also the source of the curated-media image references (the P18 image claim).
- **Wikipedia**: Article content reached through the Wikidata QID is licensed under the Creative Commons Attribution-ShareAlike License (CC BY-SA).
- **iNaturalist**: Species identifiers and taxonomies map to the iNaturalist open taxonomy. Any thumbnails or photos retrieved using these IDs must be individually verified for open-source (CC) licensing on the iNaturalist platform.
- **Wikimedia Commons**: Curated species images for non-bird taxa are referenced by their stable Commons filename. Each image stores its own license (only Creative Commons or public-domain images are ingested) plus creator and rights-holder attribution; no image bytes are stored and no raw CDN URL is baked in.

## 4. Localized Common Names (IOC World Bird List, GBIF & Wikidata)

The localized bird common names backfilled to close per-locale coverage gaps are sourced from open, redistribution-compatible providers:

- **IOC World Bird List, Multilingual Version (v15.2)**: the primary source of curated multilingual bird common names, covering 41 languages. Edited by Frank Gill, David Donsker and Pamela Rasmussen (Eds). Released under the **Creative Commons Attribution 3.0 license (CC BY 3.0)**, which is forward-compatible with CC BY-SA 4.0. (https://www.worldbirdnames.org/)
- **GBIF Backbone vernacular names** (CC0 1.0) and **Wikidata** (CC0 1.0): used to supplement locales and non-avian taxa that IOC does not cover.

*Deliberate exclusion:* OpenFauna does **not** ingest BirdNET's own non-English label files as a name source. Those localized names derive from eBird/Clements, whose terms restrict commercial redistribution and are not compatible with CC BY-SA 4.0.

## 5. Aotearoa New Zealand Names (mi, en_nz)

The te reo Māori (`mi`) and English (New Zealand) (`en_nz`) locales carry the official names used in Aotearoa New Zealand:

- **Checklist of the Birds of New Zealand, 5th edition (2022)**, Checklist Committee, Ornithological Society of New Zealand | Te Kāhui Mātai Manu o Aotearoa (Birds New Zealand). Used as the factual reference for each species' official te reo Māori and New Zealand English common name, including macron orthography, via the society's openly published checklist database (https://www.birdsnz.org.nz/society-publications/checklist/). Individual name facts were curated per species; the society's database itself is not redistributed here.
- Spellings were additionally verified against the checklist's companion research database of recorded Māori and Moriori bird names (Appendix 3 supplementary materials), and cross-checked against the names used in **RNZ's bird collection** (https://www.rnz.co.nz/collections/birds).
- The `en_nz` locale follows the checklist's own dual-naming convention, Māori name first (e.g. "Pīwakawaka | New Zealand Fantail"); where the te reo name is also the accepted English name (Kea, Weka, Tūī) a single name is used.

## Summary of Your Obligations

If you use OpenFauna in your application:
1. You **must** provide attribution to the BirdNET project and the Cornell Lab of Ornithology.
2. You **must** provide attribution to the IOC World Bird List (Gill, Donsker & Rasmussen, Eds) for the multilingual common names.
3. You **must** share any modified translation files under the same CC BY-SA 4.0 license.
4. You **must not** use the data for prohibited uses outlined in the BirdNET V3.0 terms (e.g., military use, poaching, wildlife exploitation).
