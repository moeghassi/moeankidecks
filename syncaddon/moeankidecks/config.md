# Moe Anki Deck Sync

- `manifest_url`: HTTPS URL of the published deck manifest.
- `auto_sync`: check for changed decks after an Anki profile opens.
- `timeout_seconds`: network timeout for each GitHub request.

Existing notes are never updated or deleted. A changed published note has a new
ID and is added as a new local note.
