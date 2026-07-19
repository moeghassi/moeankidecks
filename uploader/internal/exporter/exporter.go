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

const SchemaVersion = 1

var mediaPattern = regexp.MustCompile(`(?i)(\[sound:|<(?:img|audio|video|source|object)\b|\burl\s*\()`)

type Field struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Card struct {
	ID          string   `json:"id"`
	NoteType    string   `json:"note_type"`
	CardType    string   `json:"card_type"`
	CardOrdinal int      `json:"card_ordinal"`
	Fields      []Field  `json:"fields"`
	Tags        []string `json:"tags"`
}

type Deck struct {
	SchemaVersion int    `json:"schema_version"`
	DeckID        string `json:"deck_id"`
	DeckName      string `json:"deck_name"`
	Cards         []Card `json:"cards"`
}

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

	ids, err := api.FindCards(ctx, `deck:"`+escapeQuery(deckName)+`"`)
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
	result := Deck{SchemaVersion: SchemaVersion, DeckID: slug, DeckName: deckName, Cards: make([]Card, 0, len(cards))}
	seenIDs := make(map[string]struct{}, len(cards))
	for _, source := range cards {
		if source.CardID == 0 || source.NoteID == 0 || source.ModelName == "" || source.Ordinal < 0 || len(source.Fields) == 0 {
			return Deck{}, fmt.Errorf("card returned incomplete publication data")
		}
		tags, ok := tagsByNote[source.NoteID]
		if !ok {
			return Deck{}, fmt.Errorf("AnkiConnect returned no note information for a card")
		}
		names, ok := templates[source.ModelName]
		if !ok {
			names, err = api.TemplateNames(ctx, source.ModelName)
			if err != nil {
				return Deck{}, err
			}
			templates[source.ModelName] = names
		}
		cardType, err := templateName(names, source.Ordinal)
		if err != nil {
			return Deck{}, fmt.Errorf("card in note type %q: %w", source.ModelName, err)
		}
		fields := orderedFields(source.Fields)
		for _, field := range fields {
			if mediaPattern.MatchString(field.Value) {
				return Deck{}, fmt.Errorf("card field %q contains unsupported media", field.Name)
			}
		}
		sortedTags := append([]string(nil), tags...)
		sort.Strings(sortedTags)
		published := Card{NoteType: source.ModelName, CardType: cardType, CardOrdinal: source.Ordinal, Fields: fields, Tags: sortedTags}
		published.ID = contentID(published)
		if _, exists := seenIDs[published.ID]; exists {
			return Deck{}, fmt.Errorf("duplicate card content produces public ID %s", published.ID)
		}
		seenIDs[published.ID] = struct{}{}
		result.Cards = append(result.Cards, published)
	}
	sort.Slice(result.Cards, func(i, j int) bool { return result.Cards[i].ID < result.Cards[j].ID })
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
	if len(names) == 1 { // Cloze cards share one template across multiple ordinals.
		return names[0], nil
	}
	if ordinal >= len(names) {
		return "", fmt.Errorf("card ordinal %d has no matching template", ordinal)
	}
	return names[ordinal], nil
}

func orderedFields(fields map[string]anki.Field) []Field {
	type ordered struct {
		name  string
		value string
		order int
	}
	items := make([]ordered, 0, len(fields))
	for name, field := range fields {
		items = append(items, ordered{name: name, value: field.Value, order: field.Order})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].order == items[j].order {
			return items[i].name < items[j].name
		}
		return items[i].order < items[j].order
	})
	result := make([]Field, len(items))
	for i, item := range items {
		result[i] = Field{Name: item.name, Value: item.value}
	}
	return result
}

func contentID(card Card) string {
	h := sha256.New()
	writeHashPart(h, "moeankidecks/card-id/v1")
	writeHashPart(h, card.NoteType)
	writeHashPart(h, card.CardType)
	writeHashPart(h, fmt.Sprintf("%d", card.CardOrdinal))
	for _, field := range card.Fields {
		writeHashPart(h, field.Name)
		writeHashPart(h, field.Value)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
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
