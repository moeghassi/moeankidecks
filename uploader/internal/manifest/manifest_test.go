package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildCreatesAndUpdatesSortedManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	first, err := Build(path, Entry{DeckID: "z", DeckName: "Z", Path: "decks/z/deck.json", NoteCount: 2}, []byte("z deck\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, first, 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := Build(path, Entry{DeckID: "a", DeckName: "A", Path: "decks/a/deck.json", NoteCount: 3}, []byte("a deck\n"))
	if err != nil {
		t.Fatal(err)
	}
	var got Manifest
	if err := json.Unmarshal(second, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Decks) != 2 || got.Decks[0].DeckID != "a" || got.Decks[1].DeckID != "z" {
		t.Fatalf("decks = %#v", got.Decks)
	}
	if got.Decks[0].SHA256 == "" || got.Decks[0].NoteCount != 3 {
		t.Fatalf("entry = %#v", got.Decks[0])
	}
}
