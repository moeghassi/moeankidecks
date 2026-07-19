from __future__ import annotations

import hashlib
import json
import re
import urllib.parse
import urllib.request
from dataclasses import dataclass
from typing import Any, Mapping

from .version import __version__

MANIFEST_SCHEMA_VERSION = 1
DECK_SCHEMA_VERSION = 2
ID_PATTERN = re.compile(r"^sha256:[0-9a-f]{64}$")
DECK_ID_PATTERN = re.compile(r"^[a-z0-9]+(?:-[a-z0-9]+)*$")
MAX_MANIFEST_BYTES = 256 * 1024
MAX_DECK_BYTES = 10 * 1024 * 1024
MAX_NOTES = 100_000


class PublicationError(ValueError):
    pass


@dataclass(frozen=True)
class ManifestEntry:
    deck_id: str
    deck_name: str
    path: str
    sha256: str
    note_count: int


@dataclass(frozen=True)
class PublishedNote:
    shared_id: str
    front: str
    back: str
    reverse: bool
    tags: tuple[str, ...]


@dataclass(frozen=True)
class PublishedDeck:
    entry: ManifestEntry
    notes: tuple[PublishedNote, ...]


@dataclass(frozen=True)
class DownloadedPublication:
    entries: tuple[ManifestEntry, ...]
    changed_decks: tuple[PublishedDeck, ...]


def _json_object(data: bytes, label: str) -> dict[str, Any]:
    try:
        value = json.loads(data.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise PublicationError(f"{label} is not valid UTF-8 JSON: {exc}") from exc
    if not isinstance(value, dict):
        raise PublicationError(f"{label} must be a JSON object")
    return value


def parse_manifest(data: bytes) -> tuple[ManifestEntry, ...]:
    if len(data) > MAX_MANIFEST_BYTES:
        raise PublicationError("manifest exceeds the size limit")
    value = _json_object(data, "manifest")
    if value.get("schema_version") != MANIFEST_SCHEMA_VERSION:
        raise PublicationError("unsupported manifest schema version")
    decks = value.get("decks")
    if not isinstance(decks, list):
        raise PublicationError("manifest decks must be an array")
    entries: list[ManifestEntry] = []
    seen: set[str] = set()
    for raw in decks:
        if not isinstance(raw, dict):
            raise PublicationError("manifest deck entry must be an object")
        deck_id = raw.get("deck_id")
        deck_name = raw.get("deck_name")
        path = raw.get("path")
        digest = raw.get("sha256")
        note_count = raw.get("note_count")
        if not isinstance(deck_id, str) or not DECK_ID_PATTERN.fullmatch(deck_id):
            raise PublicationError("manifest contains an invalid deck_id")
        if deck_id in seen:
            raise PublicationError(f"manifest contains duplicate deck_id {deck_id!r}")
        seen.add(deck_id)
        if not isinstance(deck_name, str) or not deck_name.strip():
            raise PublicationError(f"deck {deck_id!r} has an invalid name")
        expected_path = f"decks/{deck_id}/deck.json"
        if path != expected_path:
            raise PublicationError(f"deck {deck_id!r} must use path {expected_path!r}")
        if not isinstance(digest, str) or not ID_PATTERN.fullmatch(digest):
            raise PublicationError(f"deck {deck_id!r} has an invalid sha256")
        if isinstance(note_count, bool) or not isinstance(note_count, int) or not 0 < note_count <= MAX_NOTES:
            raise PublicationError(f"deck {deck_id!r} has an invalid note_count")
        entries.append(ManifestEntry(deck_id, deck_name, path, digest, note_count))
    return tuple(entries)


def parse_deck(data: bytes, entry: ManifestEntry) -> PublishedDeck:
    if len(data) > MAX_DECK_BYTES:
        raise PublicationError(f"deck {entry.deck_id!r} exceeds the size limit")
    actual_digest = "sha256:" + hashlib.sha256(data).hexdigest()
    if actual_digest != entry.sha256:
        raise PublicationError(f"deck {entry.deck_id!r} does not match its manifest sha256")
    value = _json_object(data, f"deck {entry.deck_id!r}")
    if value.get("schema_version") != DECK_SCHEMA_VERSION:
        raise PublicationError(f"deck {entry.deck_id!r} has an unsupported schema version")
    if value.get("deck_id") != entry.deck_id or value.get("deck_name") != entry.deck_name:
        raise PublicationError(f"deck {entry.deck_id!r} metadata does not match the manifest")
    raw_notes = value.get("notes")
    if not isinstance(raw_notes, list) or len(raw_notes) != entry.note_count:
        raise PublicationError(f"deck {entry.deck_id!r} note count does not match the manifest")
    notes: list[PublishedNote] = []
    seen: set[str] = set()
    for raw in raw_notes:
        if not isinstance(raw, dict):
            raise PublicationError(f"deck {entry.deck_id!r} contains a non-object note")
        shared_id = raw.get("id")
        front = raw.get("front")
        back = raw.get("back")
        reverse = raw.get("reverse")
        tags = raw.get("tags")
        if not isinstance(shared_id, str) or not ID_PATTERN.fullmatch(shared_id):
            raise PublicationError(f"deck {entry.deck_id!r} contains an invalid note ID")
        if shared_id in seen:
            raise PublicationError(f"deck {entry.deck_id!r} contains duplicate note ID {shared_id}")
        seen.add(shared_id)
        if not isinstance(front, str) or not front.strip() or not isinstance(back, str) or not back.strip():
            raise PublicationError(f"note {shared_id!r} must have nonempty front and back strings")
        if not isinstance(reverse, bool):
            raise PublicationError(f"note {shared_id!r} reverse must be boolean")
        if not isinstance(tags, list) or any(not isinstance(tag, str) or not tag for tag in tags):
            raise PublicationError(f"note {shared_id!r} tags must be nonempty strings")
        if tags != sorted(set(tags)):
            raise PublicationError(f"note {shared_id!r} tags must be unique and sorted")
        notes.append(PublishedNote(shared_id, front, back, reverse, tuple(tags)))
    return PublishedDeck(entry, tuple(notes))


def changed_entries(entries: tuple[ManifestEntry, ...], successful_sha256: Mapping[str, str]) -> tuple[ManifestEntry, ...]:
    return tuple(entry for entry in entries if successful_sha256.get(entry.deck_id) != entry.sha256)


def record_successful_digest(successful_sha256: Mapping[str, str], entry: ManifestEntry) -> dict[str, str]:
    updated = dict(successful_sha256)
    updated[entry.deck_id] = entry.sha256
    return updated


def resolve_deck_url(manifest_url: str, entry: ManifestEntry) -> str:
    parsed = urllib.parse.urlparse(manifest_url)
    if parsed.scheme != "https" or not parsed.netloc:
        raise PublicationError("manifest_url must be an absolute HTTPS URL")
    return urllib.parse.urljoin(manifest_url, entry.path)


def fetch_bytes(url: str, timeout: int, maximum: int) -> bytes:
    request = urllib.request.Request(
        url,
        headers={"User-Agent": f"MoeAnkiDecks/{__version__}", "Cache-Control": "no-cache"},
    )
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            length = response.headers.get("Content-Length")
            if length is not None and int(length) > maximum:
                raise PublicationError(f"response from {url} exceeds the size limit")
            data = response.read(maximum + 1)
    except PublicationError:
        raise
    except Exception as exc:
        raise PublicationError(f"download failed for {url}: {exc}") from exc
    if len(data) > maximum:
        raise PublicationError(f"response from {url} exceeds the size limit")
    return data


def download_publication(manifest_url: str, timeout: int, successful_sha256: Mapping[str, str]) -> DownloadedPublication:
    entries = parse_manifest(fetch_bytes(manifest_url, timeout, MAX_MANIFEST_BYTES))
    decks = tuple(
        parse_deck(fetch_bytes(resolve_deck_url(manifest_url, entry), timeout, MAX_DECK_BYTES), entry)
        for entry in changed_entries(entries, successful_sha256)
    )
    return DownloadedPublication(entries, decks)
