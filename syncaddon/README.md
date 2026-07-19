# Moe Anki Deck Sync

Current version: `0.1.1`

Desktop Anki add-on that checks the published manifest on GitHub and adds notes
that are not already present in the current collection.

The add-on stores the last successfully synchronized SHA-256 digest for every
deck in Anki's per-collection configuration. A deck is downloaded only when its
manifest digest differs. Existing and removed notes are never changed.

Build the installable package with:

```sh
python3 syncaddon/build.py
```

Install `syncaddon/dist/moeankidecks.ankiaddon` using Anki Desktop's add-on
installer. The same package supports macOS, Windows, and Linux.
