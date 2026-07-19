package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

const SchemaVersion = 1

type Entry struct {
	DeckID    string `json:"deck_id"`
	DeckName  string `json:"deck_name"`
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	NoteCount int    `json:"note_count"`
}

type Manifest struct {
	SchemaVersion int     `json:"schema_version"`
	Decks         []Entry `json:"decks"`
}

func Build(existingPath string, entry Entry, deckJSON []byte) ([]byte, error) {
	manifest := Manifest{SchemaVersion: SchemaVersion, Decks: []Entry{}}
	data, err := os.ReadFile(existingPath)
	if err == nil {
		if err := json.Unmarshal(data, &manifest); err != nil {
			return nil, fmt.Errorf("decode existing manifest: %w", err)
		}
		if manifest.SchemaVersion != SchemaVersion {
			return nil, fmt.Errorf("existing manifest schema version %d is unsupported", manifest.SchemaVersion)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read existing manifest: %w", err)
	}

	digest := sha256.Sum256(deckJSON)
	entry.SHA256 = "sha256:" + hex.EncodeToString(digest[:])
	updated := false
	seen := make(map[string]struct{}, len(manifest.Decks))
	for i, current := range manifest.Decks {
		if current.DeckID == "" || current.DeckName == "" || current.Path == "" || current.SHA256 == "" || current.NoteCount < 1 {
			return nil, fmt.Errorf("existing manifest contains an invalid deck entry")
		}
		if _, duplicate := seen[current.DeckID]; duplicate {
			return nil, fmt.Errorf("existing manifest contains duplicate deck ID %q", current.DeckID)
		}
		seen[current.DeckID] = struct{}{}
		if current.DeckID == entry.DeckID {
			manifest.Decks[i] = entry
			updated = true
		}
	}
	if !updated {
		manifest.Decks = append(manifest.Decks, entry)
	}
	sort.Slice(manifest.Decks, func(i, j int) bool { return manifest.Decks[i].DeckID < manifest.Decks[j].DeckID })

	var out strings.Builder
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(manifest); err != nil {
		return nil, fmt.Errorf("encode manifest: %w", err)
	}
	return []byte(out.String()), nil
}
