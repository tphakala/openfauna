#!/usr/bin/env python3
"""Static data-quality validator for the OpenFauna dataset.

Runs a set of deterministic, network-free checks over data/locales/*.json,
data/metadata.json and data/aliases.json, and exits non-zero if any check
fails. Designed to run in CI on every pull request.

Checks (each can be skipped with --skip <name>):
  structure  JSON parses, keys sorted ascending, file ends with a newline.
  aliases    No scientific name is both a full locale entry and an alias key;
             every alias canonical resolves (exists in en.json or is an
             allowlisted control label).
  shadowing  No locale uses the scientific name as the common-name value while
             base 'en' has a real common name, unless listed in
             data/validation/shadow_allowlist.json. This is the class of bug
             that hid bat common names behind their Latin names.
  casing     In lowercase-convention locales (fi, sv, no) species common names
             start with a lowercase letter, except separate-word genitive
             proper nouns (e.g. "Stellers sjolejon", "Thomas' galago"), which
             those languages keep capitalized.
  dashes     No common-name value contains an em dash or en dash.

Usage:
  python3 scripts/validate.py                 # run all checks
  python3 scripts/validate.py --skip casing   # run all but the casing check
  python3 scripts/validate.py --quiet         # only print failures
"""

from __future__ import annotations

import argparse
import glob
import json
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
LOCALE_DIR = os.path.join(ROOT, "data", "locales")
ALIASES_FILE = os.path.join(ROOT, "data", "aliases.json")
METADATA_FILE = os.path.join(ROOT, "data", "metadata.json")
SHADOW_ALLOWLIST = os.path.join(ROOT, "data", "validation", "shadow_allowlist.json")

# Locales whose vernacular species names use a lowercase initial. Verified
# against the national authorities (Kotus/Luomus, Spakradet/Artdatabanken,
# Artsdatabanken/NNKF) and cross-checked with iNaturalist and eBird. da and is
# use Title Case by convention and are intentionally excluded.
LOWERCASE_LOCALES = ("fi", "sv", "no")

# English sound-event labels whose keys are binomial-shaped and would otherwise
# pollute the genus set (e.g. "Human vocal", "Power tools"). These are not
# species; their first token is not a real genus.
PSEUDO_GENERA = {"Human", "Power"}

# Alias canonicals that legitimately do not exist as a translated species
# (control-label aliases). The compiler treats them as no-ops.
ALIAS_CANONICAL_EXCEPTIONS = {"Noise"}

BINOMIAL_PREFIX = re.compile(r"^[A-Z][a-z]+ [a-z]+")


def read_text(path: str) -> str:
    with open(path, encoding="utf-8") as fh:
        return fh.read()


def load_json(path: str):
    return json.loads(read_text(path))


def locale_paths() -> list[str]:
    return sorted(glob.glob(os.path.join(LOCALE_DIR, "*.json")))


def locale_name(path: str) -> str:
    return os.path.basename(path)[:-5]


def starts_upper(value: str) -> bool:
    for ch in value:
        if ch.isalpha():
            return ch.isupper()
    return False


class Report:
    def __init__(self, quiet: bool) -> None:
        self.quiet = quiet
        self.failures = 0

    def ok(self, check: str, msg: str) -> None:
        if not self.quiet:
            print(f"PASS  {check}: {msg}")

    def fail(self, check: str, lines: list[str]) -> None:
        self.failures += len(lines)
        print(f"FAIL  {check}: {len(lines)} problem(s)")
        for line in lines[:50]:
            print(f"        {line}")
        if len(lines) > 50:
            print(f"        ... and {len(lines) - 50} more")


def check_structure(report: Report) -> None:
    problems: list[str] = []
    files = locale_paths() + [ALIASES_FILE, METADATA_FILE, SHADOW_ALLOWLIST]
    for path in files:
        rel = os.path.relpath(path, ROOT)
        try:
            raw = read_text(path)
            data = json.loads(raw)
        except FileNotFoundError:
            problems.append(f"{rel}: file is missing")
            continue
        except json.JSONDecodeError as exc:
            problems.append(f"{rel}: invalid JSON ({exc})")
            continue
        if not raw.endswith("\n"):
            problems.append(f"{rel}: missing trailing newline")
        if not isinstance(data, dict):
            problems.append(f"{rel}: root is {type(data).__name__}, expected a JSON object")
            continue
        keys = list(data.keys())
        if keys != sorted(keys):
            problems.append(f"{rel}: keys are not sorted")
    if problems:
        report.fail("structure", problems)
    else:
        report.ok("structure", f"{len(files)} files well-formed, sorted, newline-terminated")


def check_aliases(report: Report, en: dict) -> None:
    aliases = load_json(ALIASES_FILE)
    problems: list[str] = []
    for alias, canonical in aliases.items():
        if alias.startswith("_"):
            continue
        if alias in en:
            problems.append(
                f"{alias!r} is both an alias and a full en.json entry (dedupe: keep the canonical only)"
            )
        if canonical not in en and canonical not in ALIAS_CANONICAL_EXCEPTIONS:
            problems.append(
                f"{alias!r} -> {canonical!r}: canonical missing from en.json (alias cannot inherit a translation)"
            )
    if problems:
        report.fail("aliases", problems)
    else:
        report.ok("aliases", f"{len(aliases)} aliases consistent, no duplicates")


def check_shadowing(report: Report, en: dict) -> None:
    allow_doc = load_json(SHADOW_ALLOWLIST)
    allow = {loc: set(names) for loc, names in allow_doc.get("allow", {}).items()}
    has_en_name = {k for k, v in en.items() if v != k}
    problems: list[str] = []
    for path in locale_paths():
        loc = locale_name(path)
        if loc == "en":
            continue
        allowed = allow.get(loc, set())
        for key, value in load_json(path).items():
            if value == key and key in has_en_name and BINOMIAL_PREFIX.match(key):
                if key not in allowed:
                    problems.append(
                        f"{loc}: {key!r} uses the scientific name as its value while en has {en[key]!r}"
                    )
    if problems:
        report.fail("shadowing", problems)
        print("        (if a species genuinely has no native common name, "
              "add it to data/validation/shadow_allowlist.json)")
    else:
        report.ok("shadowing", "no scientific-name placeholders shadow a real en common name")


def build_genus_set(en: dict) -> set[str]:
    return {k.split(" ")[0] for k in en if BINOMIAL_PREFIX.match(k)} - PSEUDO_GENERA


def is_taxon_key(key: str, genus: set[str], en: dict, meta: dict) -> bool:
    if key.startswith("Human"):
        return False
    parts = key.split(" ")
    if len(parts) >= 2 and parts[0] in genus:
        return True
    if len(parts) == 1 and "_" in key:
        return False
    if len(parts) == 1 and en.get(key) and en[key] != key and not key.isupper():
        return True
    return key in meta


def is_eponym_first(value: str) -> bool:
    parts = value.split(" ")
    if len(parts) < 2:
        return False
    head = parts[0]
    return head.endswith("s") or head.endswith("s’") or head.endswith("s'")


def should_be_lowercase(key: str, value: str, genus: set[str], en: dict, meta: dict) -> bool:
    """True when a fi/sv/no taxon value ought to start lowercase but does not."""
    if not is_taxon_key(key, genus, en, meta):
        return False
    if value == key or not starts_upper(value):
        return False
    if value == en.get(key) and " " in value:
        return False  # multi-word English-phrase fallback, separate concern
    head = value.split(" ")[0]
    if head in genus:
        return False  # value is itself a scientific (Latin) name
    if " " in value and value.split(" ")[-1] == key.split(" ")[-1]:
        return False  # scientific synonym sharing the key's epithet
    if is_eponym_first(value):
        return False  # separate-word genitive proper noun keeps its capital
    return True


def check_casing(report: Report, en: dict, meta: dict) -> None:
    genus = build_genus_set(en)
    problems: list[str] = []
    for loc in LOWERCASE_LOCALES:
        path = os.path.join(LOCALE_DIR, f"{loc}.json")
        if not os.path.exists(path):
            continue
        for key, value in load_json(path).items():
            if should_be_lowercase(key, value, genus, en, meta):
                problems.append(f"{loc}: {key!r} -> {value!r} should start lowercase")
    if problems:
        report.fail("casing", problems)
    else:
        report.ok("casing", f"{', '.join(LOWERCASE_LOCALES)} species names follow the lowercase convention")


def check_dashes(report: Report) -> None:
    problems: list[str] = []
    for path in locale_paths():
        loc = locale_name(path)
        for key, value in load_json(path).items():
            if isinstance(value, str) and ("—" in value or "–" in value):
                problems.append(f"{loc}: {key!r} -> {value!r} contains an em/en dash")
    if problems:
        report.fail("dashes", problems)
    else:
        report.ok("dashes", "no em/en dashes in any common-name value")


CHECKS = ("structure", "aliases", "shadowing", "casing", "dashes")


def main() -> int:
    parser = argparse.ArgumentParser(description="Validate OpenFauna dataset quality.")
    parser.add_argument("--skip", action="append", default=[], choices=CHECKS,
                        help="skip a named check (repeatable)")
    parser.add_argument("--quiet", action="store_true", help="only print failures")
    args = parser.parse_args()

    report = Report(args.quiet)

    if "structure" not in args.skip:
        check_structure(report)
        if report.failures:
            # Semantic checks assume well-formed dict files; do not run them on broken input.
            print(f"\nFAILED: {report.failures} structural problem(s). Skipping semantic checks.")
            return 1

    try:
        en = load_json(os.path.join(LOCALE_DIR, "en.json"))
        meta = load_json(METADATA_FILE)
    except (json.JSONDecodeError, FileNotFoundError) as exc:
        print(f"CRITICAL: failed to load essential data files (en.json or metadata.json): {exc}")
        return 1

    if "aliases" not in args.skip:
        check_aliases(report, en)
    if "shadowing" not in args.skip:
        check_shadowing(report, en)
    if "casing" not in args.skip:
        check_casing(report, en, meta)
    if "dashes" not in args.skip:
        check_dashes(report)

    if report.failures:
        print(f"\nFAILED: {report.failures} problem(s) found.")
        return 1
    if not args.quiet:
        print("\nOK: all data-quality checks passed.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
