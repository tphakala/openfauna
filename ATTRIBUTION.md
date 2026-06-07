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

The taxonomic metadata containing Class, Order, and Family tree information was derived from two authoritative sources:

- **Bird Taxonomy (eBird / Clements Checklist):** Provided by the Cornell Lab of Ornithology. 
  *License Note:* Data derived from eBird is typically restricted to **Non-commercial use with attribution**. If you intend to use OpenFauna in a commercial application, you must evaluate your usage of the Bird family metadata against the eBird terms of use.
- **Non-Bird Taxonomy (Insects, Amphibians, Mammals):** Derived from the **GBIF Backbone Taxonomy**.
  *License Note:* GBIF Backbone Taxonomy is released into the public domain under **CC0 1.0**. (https://doi.org/10.15468/39omei)

## 3. External Links & Enrichment

As OpenFauna expands to include external identifiers and links, those data points are sourced directly from public, open-access databases:

- **Wikipedia**: Article URLs and text snippets are sourced from Wikipedia and are licensed under the Creative Commons Attribution-ShareAlike License (CC BY-SA).
- **iNaturalist**: Species identifiers and taxonomies map to the iNaturalist open taxonomy. Any thumbnails or photos retrieved using these IDs must be individually verified for open-source (CC) licensing on the iNaturalist platform.

## Summary of Your Obligations

If you use OpenFauna in your application:
1. You **must** provide attribution to the BirdNET project and the Cornell Lab of Ornithology.
2. You **must** share any modified translation files under the same CC BY-SA 4.0 license.
3. You **must not** use the data for prohibited uses outlined in the BirdNET V3.0 terms (e.g., military use, poaching, wildlife exploitation).
4. You **should** be aware of the non-commercial restrictions placed on the eBird/Clements derived family data if operating commercially.
