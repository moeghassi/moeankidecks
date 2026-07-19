# Moe Anki Decks

This repository contains:

- `uploader/`: the read-only Go command that exports an Anki deck.
- `decks/`: deterministic public deck snapshots.
- `syncaddon/`: cross-platform Anki Desktop synchronization add-on.
- `manifest.json`: published deck index and exact snapshot digests.

## Uploader

Keep Anki Desktop running with AnkiConnect available at
`http://127.0.0.1:8765`, then run from the repository root:

```sh
go run ./uploader/cmd/uploader "French A1"
```

Add `-v` to print one progress line for each source card:

```sh
go run ./uploader/cmd/uploader -v "French A1"
```

To require a clean, synchronized Git branch and publish the generated snapshot:

```sh
go run ./uploader/cmd/uploader --push "French A1"
```

The uploader only calls read operations in AnkiConnect. It does not change notes,
cards, decks, or review scheduling.

Each successful export also updates the root `manifest.json` with the exact
SHA-256 digest and note count of the generated snapshot.
