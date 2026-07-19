from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from anki.collection import Collection, OpChanges
from aqt import gui_hooks, mw
from aqt.operations import CollectionOp, QueryOp
from aqt.qt import QAction
from aqt.utils import qconnect, showInfo, showWarning, tooltip

from .core import DownloadedPublication, PublishedDeck, download_publication, record_successful_digest

NOTE_TYPE_NAME = "Moe Shared Note v1"
STATE_KEY = "moeankidecks.sync_state.v1"
FIELD_NAMES = ["SharedID", "SourceDeckID", "Front", "Back", "Reverse"]

_sync_running = False


@dataclass(frozen=True)
class SyncResult:
    added_notes: int
    added_cards: int
    unchanged_decks: int
    deck_additions: tuple[tuple[str, int], ...]


@dataclass(frozen=True)
class AppliedSync:
    changes: OpChanges
    summary: SyncResult


def _config() -> dict[str, Any]:
    config = mw.addonManager.getConfig(__name__) or {}
    return config


def _state() -> dict[str, str]:
    if mw.col is None:
        return {}
    value = mw.col.get_config(STATE_KEY, {})
    if not isinstance(value, dict):
        return {}
    return {str(key): str(digest) for key, digest in value.items()}


def _ensure_note_type(col: Collection) -> dict[str, Any]:
    model = col.models.by_name(NOTE_TYPE_NAME)
    if model is not None:
        fields = [field["name"] for field in model["flds"]]
        templates = [template["name"] for template in model["tmpls"]]
        if fields != FIELD_NAMES or templates != ["Forward", "Reverse"]:
            raise RuntimeError(f"existing note type {NOTE_TYPE_NAME!r} is incompatible")
        return model

    model = col.models.new(NOTE_TYPE_NAME)
    for name in FIELD_NAMES:
        col.models.add_field(model, col.models.new_field(name))
    forward = col.models.new_template("Forward")
    forward["qfmt"] = "{{Front}}"
    forward["afmt"] = '{{FrontSide}}<hr id="answer">{{Back}}'
    col.models.add_template(model, forward)
    reverse = col.models.new_template("Reverse")
    reverse["qfmt"] = "{{#Reverse}}{{Back}}{{/Reverse}}"
    reverse["afmt"] = '{{FrontSide}}<hr id="answer">{{Front}}'
    col.models.add_template(model, reverse)
    model["css"] = ".card { font-family: Arial; font-size: 20px; text-align: center; color: black; background: white; }"
    col.models.add(model)
    return model


def _existing_keys(col: Collection) -> set[tuple[str, str]]:
    keys: set[tuple[str, str]] = set()
    for note_id in col.find_notes(f'note:"{NOTE_TYPE_NAME}"'):
        note = col.get_note(note_id)
        try:
            keys.add((note["SourceDeckID"], note["SharedID"]))
        except KeyError:
            continue
    return keys


def _add_deck(col: Collection, published: PublishedDeck, model: dict[str, Any], existing: set[tuple[str, str]]) -> tuple[int, int]:
    deck_id = col.decks.id(f"Moe Shared::{published.entry.deck_name}")
    added_notes = 0
    added_cards = 0
    for source in published.notes:
        key = (published.entry.deck_id, source.shared_id)
        if key in existing:
            continue
        note = col.new_note(model)
        note["SharedID"] = source.shared_id
        note["SourceDeckID"] = published.entry.deck_id
        note["Front"] = source.front
        note["Back"] = source.back
        note["Reverse"] = "1" if source.reverse else ""
        note.tags = sorted(set(source.tags) | {"moeankidecks", f"moeankidecks::{published.entry.deck_id}"})
        col.add_note(note, deck_id)
        existing.add(key)
        added_notes += 1
        added_cards += 2 if source.reverse else 1
    return added_notes, added_cards


def _apply(col: Collection, publication: DownloadedPublication) -> AppliedSync:
    state = col.get_config(STATE_KEY, {})
    if not isinstance(state, dict):
        state = {}
    if not publication.changed_decks:
        raise RuntimeError("collection sync was started without any changed decks")
    undo_entry = col.add_custom_undo_entry("Sync Moe Anki Decks")
    model = _ensure_note_type(col)
    existing = _existing_keys(col)
    total_notes = 0
    total_cards = 0
    additions: list[tuple[str, int]] = []
    for deck in publication.changed_decks:
        added_notes, added_cards = _add_deck(col, deck, model, existing)
        total_notes += added_notes
        total_cards += added_cards
        additions.append((deck.entry.deck_name, added_notes))
        # Persist only after this deck has been completely applied.
        state = record_successful_digest(state, deck.entry)
        col.set_config(STATE_KEY, state)
    changes = col.merge_undo_entries(undo_entry)
    summary = SyncResult(total_notes, total_cards, len(publication.entries) - len(publication.changed_decks), tuple(additions))
    return AppliedSync(changes, summary)


def _finish() -> None:
    global _sync_running
    _sync_running = False


def _failure(error: Exception, manual: bool) -> None:
    _finish()
    if manual:
        showWarning(f"Moe Anki Deck Sync failed:\n\n{error}")
    else:
        tooltip(f"Moe Anki Deck Sync failed: {error}", period=6000)


def _success(result: SyncResult, manual: bool) -> None:
    _finish()
    if manual:
        lines = ["Moe Anki Deck Sync", ""]
        lines.extend(f"{name}: {count} new notes" for name, count in result.deck_additions)
        lines.extend(["", f"Total new notes: {result.added_notes}", f"Total new cards: {result.added_cards}"])
        if not result.deck_additions:
            lines.append("Everything is already up to date.")
        showInfo("\n".join(lines))
    elif result.added_notes:
        tooltip(f"Moe Anki Deck Sync added {result.added_notes} notes ({result.added_cards} cards).", period=5000)


def _downloaded(publication: DownloadedPublication, manual: bool) -> None:
    if not publication.changed_decks:
        _success(SyncResult(0, 0, len(publication.entries), ()), manual)
        return
    CollectionOp(parent=mw, op=lambda col: _apply(col, publication)).success(
        lambda result: _success(result.summary, manual)
    ).failure(lambda error: _failure(error, manual)).run_in_background()


def start_sync(manual: bool = False) -> None:
    global _sync_running
    if _sync_running:
        if manual:
            tooltip("Moe Anki Deck Sync is already running.")
        return
    if mw.col is None:
        return
    config = _config()
    manifest_url = str(config.get("manifest_url", ""))
    timeout = int(config.get("timeout_seconds", 15))
    successful_sha256 = _state()
    _sync_running = True
    QueryOp(
        parent=mw,
        op=lambda _col: download_publication(manifest_url, timeout, successful_sha256),
        success=lambda publication: _downloaded(publication, manual),
    ).failure(lambda error: _failure(error, manual)).without_collection().run_in_background()


def _profile_opened() -> None:
    if bool(_config().get("auto_sync", True)):
        start_sync(manual=False)


def register() -> None:
    action = QAction("Sync Moe Anki Decks", mw)
    qconnect(action.triggered, lambda: start_sync(manual=True))
    mw.form.menuTools.addAction(action)
    gui_hooks.profile_did_open.append(_profile_opened)
