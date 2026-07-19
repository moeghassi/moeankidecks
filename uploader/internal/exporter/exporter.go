package exporter

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/moeghassi/moeankidecks/uploader/internal/anki"
)

const SchemaVersion = 2

var mediaPattern = regexp.MustCompile(`(?i)(\[sound:|<(?:img|audio|video|source|object)\b|\burl\s*\()`)

type Note struct {
	ID      string   `json:"id"`
	Front   string   `json:"front"`
	Back    string   `json:"back"`
	Reverse bool     `json:"reverse"`
	Tags    []string `json:"tags"`
}

type Deck struct {
	SchemaVersion int    `json:"schema_version"`
	DeckID        string `json:"deck_id"`
	DeckName      string `json:"deck_name"`
	Notes         []Note `json:"notes"`
}

type Progress struct {
	Current  int
	Total    int
	CardType string
	Front    string
}

type ProgressFunc func(Progress)

func Slug(name string) (string, error) {
	var b strings.Builder
	dash := false
	for _, r := range strings.TrimSpace(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(unicode.ToLower(r))
			dash = false
		} else {
			dash = true
		}
	}
	slug := b.String()
	if slug == "" || slug == "." || slug == ".." {
		return "", fmt.Errorf("deck name %q does not produce a safe folder name", name)
	}
	return slug, nil
}

func Build(ctx context.Context, api anki.API, deckName string) (Deck, error) {
	return BuildWithProgress(ctx, api, deckName, nil)
}

func BuildWithProgress(ctx context.Context, api anki.API, deckName string, progress ProgressFunc) (Deck, error) {
	slug, err := Slug(deckName)
	if err != nil {
		return Deck{}, err
	}
	names, err := api.DeckNames(ctx)
	if err != nil {
		return Deck{}, err
	}
	found := false
	for _, name := range names {
		if name == deckName {
			found = true
			break
		}
	}
	if !found {
		return Deck{}, fmt.Errorf("deck %q does not exist", deckName)
	}

	ids, err := api.FindCards(ctx, fmt.Sprintf(`deck:"%s"`, escapeQuery(deckName)))
	if err != nil {
		return Deck{}, err
	}
	allCards, err := cardsInChunks(ctx, api, ids)
	if err != nil {
		return Deck{}, err
	}
	cards := allCards[:0]
	for _, card := range allCards {
		if card.DeckName == deckName {
			cards = append(cards, card)
		}
	}
	if len(cards) == 0 {
		return Deck{}, fmt.Errorf("deck %q contains no exportable cards", deckName)
	}

	notes, err := notesInChunks(ctx, api, anki.UniqueNoteIDs(cards))
	if err != nil {
		return Deck{}, err
	}
	tagsByNote := make(map[int64][]string, len(notes))
	for _, note := range notes {
		tagsByNote[note.NoteID] = note.Tags
	}

	templates := make(map[string][]string)
	type sourceNote struct {
		front    string
		back     string
		tags     []string
		model    string
		ordinals map[int]struct{}
	}
	grouped := make(map[int64]*sourceNote)
	for i, source := range cards {
		if source.CardID == 0 || source.NoteID == 0 || source.ModelName == "" || source.Ordinal < 0 || len(source.Fields) == 0 {
			return Deck{}, fmt.Errorf("card returned incomplete publication data")
		}
		tags, ok := tagsByNote[source.NoteID]
		if !ok {
			return Deck{}, fmt.Errorf("AnkiConnect returned no note information for a card")
		}
		frontField, hasFront := source.Fields["Front"]
		backField, hasBack := source.Fields["Back"]
		if !hasFront || !hasBack || len(source.Fields) != 2 {
			return Deck{}, fmt.Errorf("note type %q must contain exactly Front and Back fields", source.ModelName)
		}
		if mediaPattern.MatchString(frontField.Value) || mediaPattern.MatchString(backField.Value) {
			return Deck{}, fmt.Errorf("card contains unsupported media")
		}
		templateNames, ok := templates[source.ModelName]
		if !ok {
			templateNames, err = api.TemplateNames(ctx, source.ModelName)
			if err != nil {
				return Deck{}, err
			}
			templates[source.ModelName] = templateNames
		}
		cardType, err := templateName(templateNames, source.Ordinal)
		if err != nil {
			return Deck{}, fmt.Errorf("card in note type %q: %w", source.ModelName, err)
		}
		if progress != nil {
			progress(Progress{Current: i + 1, Total: len(cards), CardType: cardType, Front: oneLine(frontField.Value)})
		}

		entry, exists := grouped[source.NoteID]
		if !exists {
			sortedTags := normalizedTags(tags)
			entry = &sourceNote{front: frontField.Value, back: backField.Value, tags: sortedTags, model: source.ModelName, ordinals: make(map[int]struct{})}
			grouped[source.NoteID] = entry
		} else if entry.front != frontField.Value || entry.back != backField.Value || entry.model != source.ModelName {
			return Deck{}, fmt.Errorf("cards for one source note returned inconsistent fields")
		}
		if _, duplicate := entry.ordinals[source.Ordinal]; duplicate {
			return Deck{}, fmt.Errorf("source note contains duplicate card ordinal %d", source.Ordinal)
		}
		entry.ordinals[source.Ordinal] = struct{}{}
	}

	result := Deck{SchemaVersion: SchemaVersion, DeckID: slug, DeckName: deckName, Notes: make([]Note, 0, len(grouped))}
	seenIDs := make(map[string]struct{}, len(grouped))
	for _, source := range grouped {
		reverse, err := reverseMode(source.ordinals)
		if err != nil {
			return Deck{}, fmt.Errorf("note type %q: %w", source.model, err)
		}
		published := Note{Front: source.front, Back: source.back, Reverse: reverse, Tags: source.tags}
		published.ID = contentID(published)
		if _, exists := seenIDs[published.ID]; exists {
			return Deck{}, fmt.Errorf("duplicate note content produces public ID %s", published.ID)
		}
		seenIDs[published.ID] = struct{}{}
		result.Notes = append(result.Notes, published)
	}
	sort.Slice(result.Notes, func(i, j int) bool { return result.Notes[i].ID < result.Notes[j].ID })
	return result, nil
}

func Marshal(deck Deck) ([]byte, error) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(deck); err != nil {
		return nil, fmt.Errorf("encode deck snapshot: %w", err)
	}
	return []byte(b.String()), nil
}

func escapeQuery(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}

func templateName(names []string, ordinal int) (string, error) {
	if len(names) == 1 {
		return names[0], nil
	}
	if ordinal >= len(names) {
		return "", fmt.Errorf("card ordinal %d has no matching template", ordinal)
	}
	return names[ordinal], nil
}

func reverseMode(ordinals map[int]struct{}) (bool, error) {
	if len(ordinals) == 1 {
		if _, ok := ordinals[0]; ok {
			return false, nil
		}
	}
	if len(ordinals) == 2 {
		_, forward := ordinals[0]
		_, reverse := ordinals[1]
		if forward && reverse {
			return true, nil
		}
	}
	return false, fmt.Errorf("fixed Front/Back export supports only card ordinal 0, optionally with ordinal 1")
}

func contentID(note Note) string {
	h := sha256.New()
	writeHashPart(h, "moeankidecks/note-id/v2")
	writeHashPart(h, note.Front)
	writeHashPart(h, note.Back)
	writeHashPart(h, fmt.Sprintf("%t", note.Reverse))
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func oneLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func normalizedTags(tags []string) []string {
	result := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		if strings.TrimSpace(tag) == "" {
			continue
		}
		if _, duplicate := seen[tag]; duplicate {
			continue
		}
		seen[tag] = struct{}{}
		result = append(result, tag)
	}
	sort.Strings(result)
	return result
}

type writer interface{ Write([]byte) (int, error) }

func writeHashPart(w writer, value string) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	w.Write(size[:])
	w.Write([]byte(value))
}

const chunkSize = 500

func cardsInChunks(ctx context.Context, api anki.API, ids []int64) ([]anki.Card, error) {
	var result []anki.Card
	for start := 0; start < len(ids); start += chunkSize {
		end := min(start+chunkSize, len(ids))
		cards, err := api.CardsInfo(ctx, ids[start:end])
		if err != nil {
			return nil, err
		}
		result = append(result, cards...)
	}
	return result, nil
}

func notesInChunks(ctx context.Context, api anki.API, ids []int64) ([]anki.Note, error) {
	var result []anki.Note
	for start := 0; start < len(ids); start += chunkSize {
		end := min(start+chunkSize, len(ids))
		notes, err := api.NotesInfo(ctx, ids[start:end])
		if err != nil {
			return nil, err
		}
		result = append(result, notes...)
	}
	return result, nil
}
