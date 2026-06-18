#!/usr/bin/env python3
"""Backfill localized common names from open, license-compatible sources.

Implements the source cascade requested in issue #11: for a target set of
(scientific name, locale) pairs, fetch a genuine common name from GBIF vernacular
names (primary) or Wikidata (secondary), rejecting any candidate that is just the
scientific name. It resolves two kinds of gap:

  * shadows  - a locale whose value equals the scientific name (the "Latin name
               shown instead of a common name" bug). Discovered automatically.
  * missing  - a locale that has no entry for a species at all. Supplied as an
               explicit target list (see MISSING_TARGETS) so the scope stays
               bounded and reviewable.

Cascade per (species, locale):
  1. GBIF /vernacularNames (ISO 639-3), most frequent valid name wins.
  2. Wikidata taxon common name (P1843). The lookup matches the species by its
     scientific name OR an alias/label, so reclassified taxa (e.g. Charadrius
     wilsonia -> Anarhynchus wilsonia) still resolve.
  3. Otherwise leave the entry untouched.

Every candidate passes through the same normalize() used by the IOC/GBIF
backfill: it strips parentheticals and author citations, takes the first of a
packed name list, enforces the expected script for non-Latin locales, and
rejects a name equal to the scientific name.

Safety: additive and curated-safe. It only writes a value that is currently the
scientific name (a shadow) or absent (missing); it never overwrites a genuine
existing name. Allowlisted shadows that get resolved are dropped from
data/validation/shadow_allowlist.json.

Licensing: Wikidata structured data is CC0. GBIF vernacular names carry the
license of their source checklist (CC0 / CC BY / CC BY-NC); the source is shown
in the report so non-commercial records can be spotted. Both are used here as a
supplement consistent with scripts/backfill_v24_open.py and the README.

Usage:
  python scripts/backfill_vernacular.py             # dry run, prints a report
  python scripts/backfill_vernacular.py --apply      # write locale files
  python scripts/backfill_vernacular.py --only-shadows --apply
"""

import argparse
import json
import os
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from concurrent.futures import ThreadPoolExecutor

# Reuse the well-tested GBIF fetch and name-cleaning filters from the IOC/GBIF
# backfill instead of duplicating them.
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from backfill_v24_open import gbif_vernacular, gbif_pick, normalize  # noqa: E402

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
LOCALE_DIR = os.path.join(ROOT, "data", "locales")
SHADOW_ALLOWLIST = os.path.join(ROOT, "data", "validation", "shadow_allowlist.json")
GBIF_CACHE = os.path.join(os.path.expanduser("~"), ".cache", "openfauna", "gbif_vern_cache.json")
WD_CACHE = os.path.join(os.path.expanduser("~"), ".cache", "openfauna", "wikidata_vern_cache.json")

UA = "OpenFaunaBot/1.0 (https://github.com/tphakala/openfauna)"

# OpenFauna locale -> (GBIF ISO 639-3 code, Wikidata language code). Regional
# variants fall back to the base language: a generic Spanish/Portuguese common
# name still beats a bare scientific name in a Spanish/Portuguese locale file.
LOCALE_LANG = {
    "ar": ("ara", "ar"), "bg": ("bul", "bg"), "ca": ("cat", "ca"),
    "cs": ("ces", "cs"), "cy": ("cym", "cy"), "da": ("dan", "da"),
    "de": ("deu", "de"), "el": ("ell", "el"), "es": ("spa", "es"),
    "es_ec": ("spa", "es"), "es_es": ("spa", "es"), "es_mx": ("spa", "es"),
    "et": ("est", "et"), "et_ee": ("est", "et"), "fa": ("fas", "fa"),
    "fi": ("fin", "fi"), "fr": ("fra", "fr"), "he": ("heb", "he"),
    "hi_in": ("hin", "hi"), "hr": ("hrv", "hr"), "hu": ("hun", "hu"),
    "id": ("ind", "id"), "is": ("isl", "is"), "it": ("ita", "it"),
    "ja": ("jpn", "ja"), "ko": ("kor", "ko"), "lt": ("lit", "lt"),
    "lv_lv": ("lav", "lv"), "ml": ("mal", "ml"), "nl": ("nld", "nl"),
    "no": ("nor", "no"), "pl": ("pol", "pl"), "pt": ("por", "pt"),
    "pt_pt": ("por", "pt"), "ro": ("ron", "ro"), "ru": ("rus", "ru"),
    "sk": ("slk", "sk"), "sl": ("slv", "sl"), "sr": ("srp", "sr"),
    "sv": ("swe", "sv"), "th": ("tha", "th"), "tr": ("tur", "tr"),
    "uk": ("ukr", "uk"), "vi_vn": ("vie", "vi"), "zh_cn": ("zho", "zh"),
}

# Per-locale capitalization convention for common names, so a verbatim GBIF /
# Wikidata name is normalized to match the rest of the file. Determined from the
# existing data (e.g. cs species names are all lowercase, es_es is sentence case,
# es_ec/es_mx are Title Case). Locales not listed keep the source casing.
#   lower    - whole name lowercase (fi/sv/no/cs convention)
#   sentence - capitalize the first word only, lowercase the rest
#   title    - capitalize each significant word (small connectors stay lowercase)
CASE_STYLE = {
    "fi": "lower", "sv": "lower", "no": "lower", "cs": "lower",
    "es": "sentence", "es_es": "sentence", "pt": "sentence", "pt_pt": "sentence",
    "es_ec": "title", "es_mx": "title",
}
# Connector words that stay lowercase inside a Title Case name.
_CONNECTORS = {"de", "del", "la", "el", "los", "las", "y", "da", "do", "dos",
               "das", "di", "e", "a", "o", "con", "the", "of"}

# Missing-entry targets named in issue #11 (birds missing in it/hu/cs). The cicada
# / cricket gaps the issue also lists are left to discovery: their entries are
# either shadows or simply absent with no open-source name available.
MISSING_TARGETS = {
    "Antilophia galeata": ["it"],
    "Charadrius collaris": ["it"], "Charadrius falklandicus": ["it"],
    "Charadrius javanicus": ["it"], "Charadrius modestus": ["it"],
    "Charadrius peronii": ["it"], "Charadrius veredus": ["it"],
    "Charadrius wilsonia": ["it"],
    "Cranioleuca gutturata": ["it", "hu"], "Cyornis concretus": ["hu"],
    "Herpsilochmus sellowi": ["it", "hu"], "Ixobrychus minutus": ["it"],
    "Laterallus xenopterus": ["it"], "Mirafra affinis": ["it"],
    "Phyllomyias cinereiceps": ["it", "hu"], "Phyllomyias nigrocapillus": ["it", "hu"],
    "Phyllomyias uropygialis": ["it", "hu"],
}


# A species key looks like a binomial; genus-only keys (e.g. "Thylamys") are out
# of scope for a species-name fix and tend to resolve to genus-level labels.
BINOMIAL = re.compile(r"^[A-Z][a-z]+ [a-z]+")

# Vernacular sources sometimes return a rank label instead of a species name:
# Japanese/Chinese 属 (genus), 科 (family), 亜科 (subfamily), or a Latin "sp."/"spp."
# placeholder. These describe a higher rank, not the species, so reject them.
_GENUS_LABEL = re.compile(r"(属|亜?科|族|\bsspp?\.?\b|\bspp?\.$)")


def is_rank_label(name):
    return bool(_GENUS_LABEL.search(name))


def load_json(path):
    with open(path, encoding="utf-8") as f:
        return json.load(f)


def save_json(path, obj):
    with open(path, "w", encoding="utf-8") as f:
        f.write(json.dumps(obj, indent=2, ensure_ascii=False, sort_keys=True) + "\n")


def locale_files():
    return {
        f[:-5]: load_json(os.path.join(LOCALE_DIR, f))
        for f in os.listdir(LOCALE_DIR)
        if f.endswith(".json")
    }


# ---- Wikidata ----

def _sparql(query, timeout=40, retries=2):
    url = "https://query.wikidata.org/sparql?" + urllib.parse.urlencode(
        {"query": query, "format": "json"}
    )
    req = urllib.request.Request(url, headers={"User-Agent": UA, "Accept": "application/sparql-results+json"})
    for attempt in range(retries + 1):
        try:
            with urllib.request.urlopen(req, timeout=timeout) as resp:
                return json.load(resp)["results"]["bindings"]
        except (urllib.error.URLError, TimeoutError, ConnectionError) as e:
            # The query service is flaky: retry on connection errors/timeouts and on
            # transient HTTP statuses, not only 429.
            is_http = isinstance(e, urllib.error.HTTPError)
            transient = (not is_http) or e.code in {429, 500, 502, 503, 504}
            if transient and attempt < retries:
                wait = 5
                if is_http and e.code == 429:
                    # Retry-After may be an integer or an HTTP-date; fall back on parse failure.
                    try:
                        wait = int(e.headers.get("Retry-After", 5))
                    except (TypeError, ValueError):
                        wait = 5
                time.sleep(min(wait, 15))
                continue
            raise
    return []


def _quote(sci):
    return '"%s"' % sci.replace("\\", "").replace('"', "")


def wikidata_batch(species, chunk=40):
    """Batch-fetch P1843 taxon common names for many species by exact scientific
    name (P225). Returns {sci: {wd_lang: [names]}}. One SPARQL query per chunk
    keeps the query service happy where 275 single lookups would be throttled."""
    out = {}
    for i in range(0, len(species), chunk):
        values = " ".join(_quote(s) for s in species[i:i + chunk])
        q = (
            "SELECT ?sci ?n (LANG(?n) AS ?l) WHERE {"
            " VALUES ?sci { %s }"
            " ?t wdt:P225 ?sci. ?t wdt:P1843 ?n. }" % values
        )
        try:
            for b in _sparql(q):
                sci = b["sci"]["value"]
                lg = b["l"]["value"]
                nm = b["n"]["value"].strip()
                if lg and nm:
                    out.setdefault(sci, {}).setdefault(lg, []).append(nm)
        except Exception as e:
            print(f"warn: wikidata batch {i // chunk} failed: {type(e).__name__}: {e}",
                  file=sys.stderr)
        time.sleep(0.3)
    return out


def _wd_action(params):
    url = "https://www.wikidata.org/w/api.php?" + urllib.parse.urlencode(
        {**params, "format": "json"}
    )
    req = urllib.request.Request(url, headers={"User-Agent": UA})
    with urllib.request.urlopen(req, timeout=20) as resp:
        return json.load(resp)


def wikidata_aliased(sci, langs):
    """Resolve a species' P1843 taxon common names via the Wikidata Action API,
    which searches labels and aliases. A reclassified scientific name (e.g.
    Charadrius wilsonia -> Anarhynchus wilsonia) still resolves because the old
    name survives as an alias. The Action API avoids the query service's rate
    limits. Returns {wd_lang: [names]} restricted to the target languages."""
    out = {}
    langs = set(langs)
    try:
        hits = _wd_action({
            "action": "wbsearchentities", "search": sci,
            "language": "en", "type": "item", "limit": 3,
        }).get("search", [])
        qids = [h["id"] for h in hits]
        if not qids:
            return out
        ents = _wd_action({
            "action": "wbgetentities", "ids": "|".join(qids),
            "props": "claims|labels|aliases",
        }).get("entities", {})
        for ent in ents.values():
            claims = ent.get("claims", {})
            # Trust the item only if it is a taxon (has P225) whose scientific name
            # matches sci either as the current P225 value or as a label/alias (so a
            # reclassified name still resolves, but an unrelated hit is rejected).
            p225 = {
                c.get("mainsnak", {}).get("datavalue", {}).get("value")
                for c in claims.get("P225", [])
            }
            if not p225:
                continue
            names = set(p225)
            names |= {lab.get("value") for lab in ent.get("labels", {}).values()}
            for al in ent.get("aliases", {}).values():
                names |= {a.get("value") for a in al}
            if sci not in names:
                continue
            for c in claims.get("P1843", []):
                dv = c.get("mainsnak", {}).get("datavalue", {}).get("value", {})
                lg, nm = dv.get("language"), (dv.get("text") or "").strip()
                if lg in langs and nm:
                    out.setdefault(lg, []).append(nm)
    except Exception as e:
        print(f"warn: wikidata alias lookup for {sci!r} failed: {type(e).__name__}: {e}",
              file=sys.stderr)
    return out


def wd_pick(names, sci, loc):
    """Most frequent valid Wikidata name; reuses the GBIF picker's tie-break."""
    return gbif_pick(names, sci, loc)


# ---- targets ----

def discover_shadows(data):
    """All (species, locale) where the locale value equals the scientific name and
    a genuine common name exists in at least one other locale (so a real name may
    be findable). Returns {species: set(locales)}."""
    genuine_somewhere = set()
    for loc, d in data.items():
        for k, v in d.items():
            if v and v.strip() and v.strip().lower() != k.lower():
                genuine_somewhere.add(k)
    targets = {}
    for loc, d in data.items():
        for k, v in d.items():
            if v and v.strip().lower() == k.lower() and k in genuine_somewhere and BINOMIAL.match(k):
                targets.setdefault(k, set()).add(loc)
    return targets


def build_targets(data, only_shadows):
    """Return {species: {locale: kind}} where kind is 'shadow' or 'missing'."""
    targets = {}
    for sp, locs in discover_shadows(data).items():
        for loc in locs:
            targets.setdefault(sp, {})[loc] = "shadow"
    if not only_shadows:
        for sp, locs in MISSING_TARGETS.items():
            for loc in locs:
                if loc not in data:
                    continue
                cur = data[loc].get(sp)
                if cur and cur.strip().lower() != sp.lower():
                    continue  # already has a genuine common name; leave it
                targets.setdefault(sp, {})[loc] = "missing"
    return targets


def _titlecase(name):
    parts = name.split(" ")
    out = []
    for i, w in enumerate(parts):
        if w and (i == 0 or w.lower() not in _CONNECTORS):
            out.append(w[0].upper() + w[1:])
        else:
            out.append(w.lower() if i else w)
    return " ".join(out)


def clean_name(name, loc):
    """Final cleanup before storing: replace em/en dashes with a plain hyphen
    (the dataset and its validator forbid them) and normalize capitalization to
    the locale's convention (see CASE_STYLE)."""
    name = name.replace("—", "-").replace("–", "-").replace("‑", "-")
    name = " ".join(name.split())
    if not name:
        return name
    style = CASE_STYLE.get(loc)
    if style == "lower":
        name = name.lower()
    elif style == "sentence":
        # First word capital, the rest lowercase. This matches the dominant
        # convention (e.g. es_es "Mosquitero carirrufo"); a genuine proper noun
        # in a later word would be lowercased, so the dry-run output is reviewed
        # by hand before --apply.
        name = name[0].upper() + name[1:].lower()
    elif style == "title":
        name = _titlecase(name)
    return name


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--apply", action="store_true", help="write locale files (default: dry run)")
    ap.add_argument("--only-shadows", action="store_true", help="skip the issue #11 missing-entry targets")
    ap.add_argument("--workers", type=int, default=8)
    ap.add_argument("--report", help="write a markdown change report to this path")
    args = ap.parse_args()

    data = locale_files()
    targets = build_targets(data, args.only_shadows)
    species = sorted(targets)
    npairs = sum(len(v) for v in targets.values())
    print(f"targets: {len(species)} species, {npairs} (species, locale) pairs")

    # GBIF (cached, concurrent).
    gbif_cache = load_json(GBIF_CACHE) if os.path.exists(GBIF_CACHE) else {}
    todo = [s for s in species if s not in gbif_cache]
    print(f"GBIF lookups: {len(todo)} (cache has {len(gbif_cache)})")
    with ThreadPoolExecutor(max_workers=args.workers) as ex:
        for sci, out, ok in ex.map(gbif_vernacular, todo):
            if ok:
                gbif_cache[sci] = out

    # Wikidata (cached). Batch by P225 for the whole target set first; persist the
    # expensive GBIF + batch work before the slower alias-aware step so a re-run
    # resumes from cache.
    wd_cache = load_json(WD_CACHE) if os.path.exists(WD_CACHE) else {}
    wd_todo = [s for s in species if s not in wd_cache]
    print(f"Wikidata batch lookups: {len(wd_todo)} (cache has {len(wd_cache)})")
    batch = wikidata_batch(wd_todo)
    for sci in wd_todo:
        wd_cache[sci] = batch.get(sci, {})
    for path, cache in ((GBIF_CACHE, gbif_cache), (WD_CACHE, wd_cache)):
        os.makedirs(os.path.dirname(path), exist_ok=True)
        save_json(path, cache)

    # Alias-aware single queries for the named missing birds, whose scientific
    # names have been reclassified and would not match P225 directly. Filtered to
    # each species' target languages to keep the query fast.
    aliased = [s for s in MISSING_TARGETS if s in targets]
    print(f"Wikidata alias-aware lookups: {len(aliased)}")
    for sci in aliased:
        langs = {LOCALE_LANG[loc][1] for loc in targets[sci] if loc in LOCALE_LANG}
        if not langs:
            continue
        for lg, names in wikidata_aliased(sci, langs).items():
            bucket = wd_cache.setdefault(sci, {}).setdefault(lg, [])
            for n in names:
                if n not in bucket:
                    bucket.append(n)
        time.sleep(0.2)
    save_json(WD_CACHE, wd_cache)

    # Resolve.
    changes = []  # (species, locale, kind, new_name, source)
    for sci in species:
        gv = gbif_cache.get(sci, {})
        wv = wd_cache.get(sci, {})
        for loc, kind in sorted(targets[sci].items()):
            iso3, wdl = LOCALE_LANG.get(loc, (None, None))
            name = source = None
            if iso3:
                pick = gbif_pick(gv.get(iso3, []), sci, loc)
                if pick:
                    name, source = pick, "GBIF"
            if not name and wdl:
                pick = wd_pick(wv.get(wdl, []), sci, loc)
                if pick:
                    name, source = pick, "Wikidata"
            if not name or is_rank_label(name):
                continue
            name = clean_name(name, loc)
            # Curated-safe: only write over a shadow (value == scientific name,
            # case-insensitively) or into an absent entry, never a curated name.
            cur = data[loc].get(sci)
            if cur is not None and cur.strip().lower() != sci.lower():
                continue
            changes.append((sci, loc, kind, name, source))

    changes.sort()
    print(f"\nresolved {len(changes)} of {npairs} pairs:")
    for sci, loc, kind, name, source in changes:
        print(f"  {loc:6} {kind:7} {sci:30} -> {name}  [{source}]")

    unresolved = npairs - len(changes)
    print(f"\nunresolved (no open-source common name): {unresolved}")

    if args.report:
        lines = ["# Vernacular backfill report", "",
                 f"- targets: {len(species)} species, {npairs} (species, locale) pairs",
                 f"- resolved: {len(changes)}", f"- unresolved: {unresolved}", "",
                 "| locale | kind | scientific name | filled name | source |",
                 "| --- | --- | --- | --- | --- |"]
        for sci, loc, kind, name, source in changes:
            lines.append(f"| {loc} | {kind} | {sci} | {name} | {source} |")
        with open(args.report, "w", encoding="utf-8") as f:
            f.write("\n".join(lines) + "\n")
        print(f"wrote report to {args.report}")

    if not args.apply:
        print("\nDRY RUN (use --apply to write).")
        return

    touched = set()
    for sci, loc, kind, name, source in changes:
        data[loc][sci] = name
        touched.add(loc)
    for loc in sorted(touched):
        save_json(os.path.join(LOCALE_DIR, f"{loc}.json"), data[loc])

    # Drop resolved shadows from the allowlist.
    resolved = {(loc, sci) for sci, loc, kind, _, _ in changes if kind == "shadow"}
    allow_doc = load_json(SHADOW_ALLOWLIST)
    changed = False
    for loc in list(allow_doc.get("allow", {})):
        kept = [s for s in allow_doc["allow"][loc] if (loc, s) not in resolved]
        if len(kept) != len(allow_doc["allow"][loc]):
            changed = True
            if kept:
                allow_doc["allow"][loc] = sorted(kept)
            else:
                del allow_doc["allow"][loc]
    if changed:
        save_json(SHADOW_ALLOWLIST, allow_doc)
        print("updated shadow_allowlist.json")

    print(f"\nWROTE {len(touched)} locale files.")


if __name__ == "__main__":
    main()
