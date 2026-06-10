"""Backfill BirdNET V2.4 localized common names from OPEN, license-clean sources.

OpenFauna is CC BY-SA 4.0 and sources only from compatible providers. This script
closes per-locale coverage gaps for the BirdNET GLOBAL 6K V2.4 label set using:

  1. IOC World Bird List, Multilingual Version (birds; CC BY 3.0, attribution
     required). Primary source: one curated common name per language for every
     bird species. Covers 41 languages.
  2. GBIF Backbone vernacular names API (CC0 / open). Supplement for species or
     locales IOC does not cover (e.g. Vietnamese, Hindi) and for non-bird taxa.

It deliberately does NOT read BirdNET's own per-locale label files: those names are
pulled from eBird/Clements (Cornell, proprietary, non-commercial) and are not
license-compatible with CC BY-SA 4.0.

The script is ADDITIVE ONLY: it never overwrites an existing curated entry, it only
fills species currently missing from a locale file. Names equal to the scientific
name or empty are skipped. Output JSON matches the repo writer (2-space indent,
sort_keys, no ASCII escaping, trailing newline).

Requirements: openpyxl (for the IOC .xlsx). Install with `pip install openpyxl`.

IOC source (download once, pass with --ioc):
  https://www.worldbirdnames.org/Multiling%20IOC%2015.2.xlsx   (Multilingual v15.2, CC BY 3.0)

Usage:
  python scripts/backfill_v24_open.py --ioc /path/to/ioc.xlsx [--locales is,th,...] [--apply]

Without --apply it runs a dry run and prints the per-locale fill report only.
"""

import argparse
import json
import os
import re
import urllib.parse
import urllib.request
from concurrent.futures import ThreadPoolExecutor

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
LOCALE_DIR = os.path.join(ROOT, "data", "locales")
LABELS_V24 = "/home/thakala/src/birdnet-go/internal/classifier/data/labels/V2.4/BirdNET_GLOBAL_6K_V2.4_Labels_en_us.txt"
GBIF_CACHE = "/tmp/openfauna-work/gbif_vern_cache.json"

# OpenFauna locale file -> (IOC column header or None, GBIF ISO-639-3 code or None)
LOCALES = {
    "is":    ("Icelandic",  "isl"),
    "th":    ("Thai",       "tha"),
    "sl":    ("Slovenian",  "slv"),
    "el":    ("Greek",      "ell"),
    "ro":    ("Romanian",   "ron"),
    "it":    ("Italian",    "ita"),
    "ko":    ("Korean",     "kor"),
    "ml":    ("Malayalam",  "mal"),
    "id":    ("Indonesian", "ind"),
    "af":    ("Afrikaans",  "afr"),
    "vi_vn": (None,         "vie"),  # not in IOC; GBIF only
    "he":    ("Hebrew",     "heb"),
    "ar":    ("Arabic",     "ara"),
    # hi_in intentionally omitted: not in IOC, and GBIF's Hindi vernacular data is
    # unreliable (romanized Tamil, wrong-species names). Needs a vetted source.
}

# Expected Unicode script ranges for non-Latin locales. A backfilled name for one of
# these must contain its script and must NOT contain ASCII letters; this drops
# romanized or wrong-language GBIF entries (e.g. romanized Tamil offered for Hindi).
SCRIPT_RANGES = {
    "th": [(0x0E00, 0x0E7F)],
    "ko": [(0xAC00, 0xD7A3), (0x1100, 0x11FF), (0x3130, 0x318F)],
    "he": [(0x0590, 0x05FF), (0xFB1D, 0xFB4F)],
    "ml": [(0x0D00, 0x0D7F)],
    "ar": [(0x0600, 0x06FF), (0x0750, 0x077F), (0xFB50, 0xFDFF), (0xFE70, 0xFEFF)],
    "el": [(0x0370, 0x03FF), (0x1F00, 0x1FFF)],
}

# BirdNET non-species sound classes: handled separately, never species-backfilled here.
SOUND_CLASSES = {
    "Environmental", "Gun", "Human non-vocal", "Human vocal",
    "Human whistle", "Power tools",
}


def load_json(path):
    with open(path, encoding="utf-8") as f:
        return json.load(f)


def save_json(path, obj):
    with open(path, "w", encoding="utf-8") as f:
        f.write(json.dumps(obj, indent=2, ensure_ascii=False, sort_keys=True) + "\n")


def v24_species():
    """Canonical V2.4 species set (alias-resolved), excluding noise/sound classes."""
    aliases = load_json(os.path.join(ROOT, "data", "aliases.json"))
    out = []
    seen = set()
    for line in open(LABELS_V24, encoding="utf-8"):
        line = line.strip()
        if not line or "_" not in line:
            continue
        sci = line.split("_", 1)[0].strip()
        if sci.startswith("Noise") or sci == "Unknown" or sci in SOUND_CLASSES:
            continue
        canon = aliases.get(sci, sci)
        if canon not in seen:
            seen.add(canon)
            out.append(canon)
    return out


def load_ioc(path):
    """ioc[scientific_name][column_header] = localized name."""
    import openpyxl
    wb = openpyxl.load_workbook(path, read_only=True, data_only=True)
    ws = wb["List"]
    it = ws.iter_rows(values_only=True)
    hdr = list(next(it))
    sci_col = hdr.index("IOC_15.2")
    col_of = {h: i for i, h in enumerate(hdr) if h}
    ioc = {}
    for row in it:
        sci = row[sci_col]
        if not sci:
            continue
        ioc[str(sci).strip()] = row
    return ioc, col_of


_PAREN_TAIL = re.compile(r"\s*\([^)]*\)\s*$")


def normalize(name, sci, loc):
    """Clean and validate a candidate name for a locale, or return None to skip.

    Handles common GBIF vernacular artifacts: strips a trailing parenthetical
    qualifier, takes the first of comma/semicolon/slash-separated alternatives, and
    unpacks Thai "ฯลฯ" (etc.) name lists. Rejects empties and names equal to the
    scientific name. For non-Latin locales requires the expected script and forbids
    ASCII letters (drops romanized / wrong-language entries). Title-cases the first
    letter to match the repo's localized-name style.
    """
    if name is None:
        return None
    name = str(name).strip()
    if not name or name.lower() == "none":
        return None
    name = _PAREN_TAIL.sub("", name).strip()
    name = re.split(r"[,;/\n]", name)[0].strip()
    if "ฯลฯ" in name:  # Thai "etc."; the field is a packed list, keep the first name
        name = name.split("ฯลฯ")[0].strip().split(" ")[0].strip()
    if not name or name.lower() == sci.lower():
        return None
    ranges = SCRIPT_RANGES.get(loc)
    if ranges:
        has_script = any(any(lo <= ord(c) <= hi for lo, hi in ranges) for c in name)
        has_ascii_alpha = any(("a" <= c <= "z") or ("A" <= c <= "Z") for c in name)
        if not has_script or has_ascii_alpha:
            return None
    return name[:1].upper() + name[1:]


# ---- GBIF ----
_cache = {}


def gbif_vernacular(sci):
    if sci in _cache:
        return _cache[sci]
    out = {}
    try:
        u = "https://api.gbif.org/v1/species/match?" + urllib.parse.urlencode({"name": sci})
        req = urllib.request.Request(u, headers={"User-Agent": "OpenFaunaBot/1.0 (tphakala)"})
        key = json.load(urllib.request.urlopen(req, timeout=30)).get("usageKey")
        if key:
            u = f"https://api.gbif.org/v1/species/{key}/vernacularNames?limit=400"
            req = urllib.request.Request(u, headers={"User-Agent": "OpenFaunaBot/1.0 (tphakala)"})
            for r in json.load(urllib.request.urlopen(req, timeout=30)).get("results", []):
                lang = r.get("language", "")
                nm = (r.get("vernacularName") or "").strip()
                if lang and nm:
                    out.setdefault(lang, []).append(nm)
    except Exception:
        out = {}
    _cache[sci] = out
    return out


def gbif_pick(names, sci, loc):
    """Pick the most frequent valid vernacular name; tie-break shortest then alpha."""
    cand = {}
    for n in names:
        c = normalize(n, sci, loc)
        if c:
            cand[c] = cand.get(c, 0) + 1
    if not cand:
        return None
    return sorted(cand, key=lambda x: (-cand[x], len(x), x))[0]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--ioc", default="/tmp/openfauna-work/ioc.xlsx", help="IOC Multilingual .xlsx path")
    ap.add_argument("--locales", default=",".join(LOCALES), help="comma list of OF locale codes")
    ap.add_argument("--apply", action="store_true", help="write changes (default: dry run)")
    ap.add_argument("--workers", type=int, default=8)
    args = ap.parse_args()

    targets = [l for l in args.locales.split(",") if l in LOCALES]
    species = v24_species()
    print(f"V2.4 canonical species: {len(species)}; target locales: {targets}")

    ioc, col_of = load_ioc(args.ioc)
    print(f"IOC rows: {len(ioc)}")

    of_data = {l: load_json(os.path.join(LOCALE_DIR, f"{l}.json")) for l in targets}

    # Phase 1: IOC fill (birds). Record which (locale, sci) still need GBIF.
    added = {l: {"IOC": 0, "GBIF": 0} for l in targets}
    pending = {}  # sci -> set(locales still missing)
    for l in targets:
        ioc_hdr, _iso = LOCALES[l]
        ci = col_of.get(ioc_hdr) if ioc_hdr else None
        for sci in species:
            if sci in of_data[l]:
                continue
            name = None
            if ci is not None and sci in ioc:
                name = normalize(ioc[sci][ci], sci, l)
            if name:
                of_data[l][sci] = name
                added[l]["IOC"] += 1
            else:
                pending.setdefault(sci, set()).add(l)

    # Phase 2: GBIF for the residue (concurrent, cached).
    if os.path.exists(GBIF_CACHE):
        _cache.update(load_json(GBIF_CACHE))
    need = [s for s in pending if any(LOCALES[l][1] for l in pending[s])]
    print(f"GBIF lookups needed: {len(need)} species (cache has {len(_cache)})")
    todo = [s for s in need if s not in _cache]
    with ThreadPoolExecutor(max_workers=args.workers) as ex:
        for i, _ in enumerate(ex.map(gbif_vernacular, todo)):
            if i % 100 == 0 and i:
                print(f"  gbif {i}/{len(todo)}")
    os.makedirs(os.path.dirname(GBIF_CACHE), exist_ok=True)
    save_json(GBIF_CACHE, _cache)

    for sci in need:
        vern = _cache.get(sci, {})
        for l in list(pending[sci]):
            iso = LOCALES[l][1]
            if not iso or sci in of_data[l]:
                continue
            pick = gbif_pick(vern.get(iso, []), sci, l)
            if pick:
                of_data[l][sci] = pick
                added[l]["GBIF"] += 1

    # Report
    print(f"\n{'loc':6} {'IOC':>6} {'GBIF':>6} {'total':>6} {'now/V2.4':>10}")
    total_added = 0
    for l in targets:
        have = sum(1 for s in species if s in of_data[l])
        t = added[l]["IOC"] + added[l]["GBIF"]
        total_added += t
        print(f"{l:6} {added[l]['IOC']:6} {added[l]['GBIF']:6} {t:6} {have:5}/{len(species):<4}")
    print(f"total added: {total_added}")

    if args.apply:
        for l in targets:
            save_json(os.path.join(LOCALE_DIR, f"{l}.json"), of_data[l])
        print("\nWROTE locale files.")
    else:
        print("\nDRY RUN (use --apply to write).")


if __name__ == "__main__":
    main()
