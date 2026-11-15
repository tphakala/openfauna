#!/usr/bin/env python3
"""Tests for scripts/validate.py.

Run with: python3 -m unittest discover -s scripts -p 'test_*.py'

validate.py derives every path from its own location, so each test copies it
into a minimal dataset tree under a temp directory and runs that copy.
"""

import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.dirname(HERE)
sys.path.insert(0, HERE)
import validate  # noqa: E402

BASE = {
    "locales/en.json": {"Aa bb": "Alpha Bird", "Dd ee": "Delta Bird", "Hh cc": "Hotel Bird"},
    "locales/sv.json": {"Aa bb": "alfa lintu"},
    "metadata.json": {"Aa bb": {}},
    "aliases.json": {},
    "sources.json": {},
    "media_sources.json": {},
    "media.json": {},
    "curated_names.json": {"names": {}},
    "validation/shadow_allowlist.json": {"allow": {}},
    "validation/taxonomy_exclusions.json": {"exclude": {}},
    "validation/fa_zwnj_tokens.json": {"tokens": {}},
}


def dump(obj):
    return json.dumps(obj, indent=2, ensure_ascii=False, sort_keys=True) + "\n"


class Tree:
    """A dataset tree with its own copy of validate.py."""

    def __init__(self, test, overrides=None):
        self.root = tempfile.mkdtemp()
        test.addCleanup(shutil.rmtree, self.root, ignore_errors=True)
        os.makedirs(os.path.join(self.root, "scripts"))
        shutil.copy(os.path.join(HERE, "validate.py"), os.path.join(self.root, "scripts", "validate.py"))
        files = dict(BASE)
        files.update(overrides or {})
        for rel, obj in files.items():
            self.write("data/" + rel, obj if isinstance(obj, str) else dump(obj))
        self.write("data/validation/labels/m.txt", "Aa bb\n")
        for flag in ("--write-echo-baseline", "--write-label-baseline"):
            self.run(flag, check=True)

    def write(self, rel, text):
        path = os.path.join(self.root, rel)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w", encoding="utf-8") as fh:
            fh.write(text)

    def run(self, *args, check=False):
        proc = subprocess.run([sys.executable, os.path.join(self.root, "scripts", "validate.py"), *args],
                              cwd=self.root, capture_output=True, text=True)
        if check and proc.returncode != 0:
            raise AssertionError(proc.stdout + proc.stderr)
        return proc


class ValidateTest(unittest.TestCase):
    def assertFails(self, proc, check, *needles):
        self.assertEqual(proc.returncode, 1, proc.stdout + proc.stderr)
        self.assertNotIn("Traceback", proc.stderr)
        lines = proc.stdout.splitlines()
        self.assertIn(f"FAIL  {check}:", " ".join(line[:20] for line in lines), proc.stdout)
        for needle in needles:
            self.assertIn(needle, proc.stdout)

    def test_minimal_tree_passes(self):
        proc = Tree(self).run()
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)

    def test_structure_covers_validation_files_and_curated_names(self):
        cases = {
            "validation/extra.json": '{"b": 1, "a": 2}\n',
            "validation/sub/nested.json": '{"b": 1, "a": 2}\n',
            "curated_names.json": '{"names": {}}',
        }
        for rel, text in cases.items():
            with self.subTest(rel):
                proc = Tree(self, {rel: text}).run()
                self.assertFails(proc, "structure", os.path.join("data", rel))

    def test_exclusions_rejects_bad_entries(self):
        de = {"locales/de.json": {"Aa bb": "Right"}}
        cases = {
            "present with different case": (
                {"locales/de.json": {"Aa bb": "Wrong"}},
                {"de": {"Aa bb": {"reason": "r", "value": "wrong"}}}),
            "blank reason": (de, {"de": {"Aa bb": {"reason": "  ", "value": "wrong"}}}),
            "blank value": (de, {"de": {"Aa bb": {"reason": "r", "value": " "}}}),
            "padded value": (de, {"de": {"Aa bb": {"reason": "r", "value": " wrong"}}}),
            "cell is a string": (de, {"de": {"Aa bb": "oops"}}),
            "locale is a list": (de, {"de": ["Aa bb"]}),
        }
        for name, (files, exclude) in cases.items():
            with self.subTest(name):
                files = dict(files, **{"validation/taxonomy_exclusions.json": {"exclude": exclude}})
                self.assertFails(Tree(self, files).run(), "exclusions", "de:")

    def test_labels_blank_en_value_is_unnamed(self):
        tree = Tree(self)
        en = dict(BASE["locales/en.json"], **{"Aa bb": "  "})
        tree.write("data/locales/en.json", dump(en))
        proc = tree.run()
        self.assertFails(proc, "labels", "'Aa bb' is newly unresolved (no_name)")

    def test_casing_flags_value_in_other_known_genus(self):
        # "Hh" is a genus of the dataset, but not the key's genus.
        proc = Tree(self, {"locales/sv.json": {"Dd ee": "Hh zz"}}).run()
        self.assertFails(proc, "casing", "'Dd ee'")

    def test_keeps_capital_matches_go_fixture(self):
        path = os.path.join(REPO, "cmd", "import-taxonomy", "testdata", "lowercase_initial.json")
        with open(path, encoding="utf-8") as fh:
            rows = json.load(fh)
        self.assertTrue(rows)
        for row in rows:
            key, value, want = row["key"], row["value"], row["want"]
            with self.subTest(row["name"]):
                lowered = validate.starts_upper(value) and not validate.keeps_capital(key, value)
                self.assertEqual(lowered, want != value)

    def test_aliases_rejects_alias_key_in_any_locale(self):
        files = {"aliases.json": {"Zz yy": "Aa bb"}, "locales/sv.json": {"Aa bb": "alfa", "Zz yy": "alfa"}}
        self.assertFails(Tree(self, files).run(), "aliases", "sv: 'Zz yy'", "'Aa bb'")
        # An alias key that is no locale entry, and a "_" key, are fine.
        files = {"aliases.json": {"_comment": "x", "Zz yy": "Aa bb"}, "locales/sv.json": {"Aa bb": "alfa", "_comment": "x"}}
        proc = Tree(self, files).run()
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)

    def test_fa_zwnj_rejects_bare_token_and_stray_joiner(self):
        tokens = {"tokens": {"اب": "ا\u200cب"}}
        cases = {
            "bare mapped token": "x اب y",
            "bare token next to punctuation": "(اب)",
            "leading joiner": "\u200cجد",
            "trailing joiner": "جد\u200c",
            "doubled joiner": "ج\u200c\u200cد",
            "joiner next to a space": "جد \u200cجد",
        }
        for name, value in cases.items():
            with self.subTest(name):
                files = {"validation/fa_zwnj_tokens.json": tokens, "locales/fa.json": {"Aa bb": value}}
                self.assertFails(Tree(self, files).run(), "fa_zwnj", "fa: 'Aa bb'")

    def test_fa_zwnj_accepts_joined_token_and_skips_odd_cells(self):
        files = {
            "validation/fa_zwnj_tokens.json": {"tokens": {"اب": "ا\u200cب"}},
            "locales/fa.json": {"Aa bb": "ا\u200cب جد", "Dd ee": 5, "Hh cc": "اباب"},
        }
        proc = Tree(self, files).run()
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)

    def test_fa_zwnj_rejects_bad_token_map(self):
        cases = {
            "value changes a letter": {"tokens": {"اب": "ا\u200cج"}},
            "value equals key": {"tokens": {"اب": "اب"}},
            "key is not one word": {"tokens": {"ا ب": "ا\u200c ب"}},
            "stray joiner in value": {"tokens": {"اب": "اب\u200c"}},
            "tokens missing": {"_comment": "x"},
            "value is not a string": {"tokens": {"اب": 5}},
        }
        for name, doc in cases.items():
            with self.subTest(name):
                self.assertFails(Tree(self, {"validation/fa_zwnj_tokens.json": doc}).run(), "fa_zwnj")

    def test_fa_zwnj_requires_token_map(self):
        tree = Tree(self)
        os.remove(os.path.join(tree.root, "data", "validation", "fa_zwnj_tokens.json"))
        self.assertFails(tree.run(), "fa_zwnj", "fa_zwnj_tokens.json is missing")


if __name__ == "__main__":
    unittest.main()
