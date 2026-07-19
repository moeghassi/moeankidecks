from __future__ import annotations

import hashlib
import json
import pathlib
import sys
import unittest
from unittest.mock import patch

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1]))

from moeankidecks.core import (  # noqa: E402
    ManifestEntry,
    PublicationError,
    changed_entries,
    download_publication,
    parse_deck,
    parse_manifest,
    record_successful_digest,
    resolve_deck_url,
)


def deck_bytes() -> bytes:
    value = {
        "schema_version": 2,
        "deck_id": "a1-2",
        "deck_name": "A1.2",
        "notes": [
            {
                "id": "sha256:" + "1" * 64,
                "front": "bruyant",
                "back": "noisy",
                "reverse": True,
                "tags": ["français"],
            }
        ],
    }
    return (json.dumps(value, ensure_ascii=False, separators=(",", ":")) + "\n").encode()


def entry_for(data: bytes) -> ManifestEntry:
    return ManifestEntry(
        "a1-2",
        "A1.2",
        "decks/a1-2/deck.json",
        "sha256:" + hashlib.sha256(data).hexdigest(),
        1,
    )


class CoreTests(unittest.TestCase):
    def test_manifest_and_deck_validation(self) -> None:
        data = deck_bytes()
        entry = entry_for(data)
        manifest = json.dumps({"schema_version": 1, "decks": [entry.__dict__]}).encode()
        parsed = parse_manifest(manifest)
        self.assertEqual(parsed, (entry,))
        deck = parse_deck(data, entry)
        self.assertEqual(deck.notes[0].front, "bruyant")
        self.assertTrue(deck.notes[0].reverse)

    def test_changed_entries_uses_last_successful_digest(self) -> None:
        entry = entry_for(deck_bytes())
        self.assertEqual(changed_entries((entry,), {}), (entry,))
        self.assertEqual(changed_entries((entry,), {"a1-2": entry.sha256}), ())
        self.assertEqual(changed_entries((entry,), {"a1-2": "sha256:" + "0" * 64}), (entry,))

    def test_record_successful_digest_preserves_other_decks(self) -> None:
        entry = entry_for(deck_bytes())
        original = {"other": "sha256:" + "9" * 64, "a1-2": "sha256:" + "0" * 64}
        updated = record_successful_digest(original, entry)
        self.assertEqual(updated["a1-2"], entry.sha256)
        self.assertEqual(updated["other"], original["other"])
        self.assertNotEqual(original["a1-2"], entry.sha256)

    def test_unchanged_manifest_digest_skips_deck_download(self) -> None:
        data = deck_bytes()
        entry = entry_for(data)
        manifest = json.dumps({"schema_version": 1, "decks": [entry.__dict__]}).encode()
        with patch("moeankidecks.core.fetch_bytes", return_value=manifest) as fetch:
            publication = download_publication(
                "https://example.test/repo/main/manifest.json",
                15,
                {entry.deck_id: entry.sha256},
            )
        self.assertEqual(publication.changed_decks, ())
        self.assertEqual(fetch.call_count, 1)

    def test_changed_manifest_digest_downloads_and_validates_deck(self) -> None:
        data = deck_bytes()
        entry = entry_for(data)
        manifest = json.dumps({"schema_version": 1, "decks": [entry.__dict__]}).encode()
        with patch("moeankidecks.core.fetch_bytes", side_effect=[manifest, data]) as fetch:
            publication = download_publication(
                "https://example.test/repo/main/manifest.json",
                15,
                {},
            )
        self.assertEqual(len(publication.changed_decks), 1)
        self.assertEqual(fetch.call_count, 2)

    def test_rejects_digest_mismatch_duplicate_ids_and_bad_path(self) -> None:
        data = deck_bytes()
        entry = entry_for(data)
        with self.assertRaises(PublicationError):
            parse_deck(data + b" ", entry)
        duplicate = json.loads(data)
        duplicate["notes"].append(duplicate["notes"][0])
        duplicate_data = (json.dumps(duplicate) + "\n").encode()
        duplicate_entry = ManifestEntry(entry.deck_id, entry.deck_name, entry.path, "sha256:" + hashlib.sha256(duplicate_data).hexdigest(), 2)
        with self.assertRaises(PublicationError):
            parse_deck(duplicate_data, duplicate_entry)
        bad_manifest = json.dumps({"schema_version": 1, "decks": [{**entry.__dict__, "path": "../deck.json"}]}).encode()
        with self.assertRaises(PublicationError):
            parse_manifest(bad_manifest)

    def test_resolves_relative_deck_url(self) -> None:
        entry = entry_for(deck_bytes())
        url = resolve_deck_url("https://example.test/repo/main/manifest.json", entry)
        self.assertEqual(url, "https://example.test/repo/main/decks/a1-2/deck.json")
        with self.assertRaises(PublicationError):
            resolve_deck_url("http://example.test/manifest.json", entry)


if __name__ == "__main__":
    unittest.main()
