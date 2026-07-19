package exporter

import (
	"context"
	"strings"
	"testing"

	"github.com/moeghassi/moeankidecks/uploader/internal/anki"
)

type fakeAPI struct {
	decks     []string
	query     string
	cards     []anki.Card
	notes     []anki.Note
	templates map[string][]string
}

func (f *fakeAPI) DeckNames(context.Context) ([]string, error) { return f.decks, nil }
func (f *fakeAPI) FindCards(_ context.Context, query string) ([]int64, error) {
	f.query = query
	ids := make([]int64, len(f.cards))
	for i := range f.cards {
		ids[i] = f.cards[i].CardID
	}
	return ids, nil
}
func (f *fakeAPI) CardsInfo(context.Context, []int64) ([]anki.Card, error) { return f.cards, nil }
func (f *fakeAPI) NotesInfo(context.Context, []int64) ([]anki.Note, error) { return f.notes, nil }
func (f *fakeAPI) TemplateNames(_ context.Context, model string) ([]string, error) {
	return f.templates[model], nil
}

func sampleAPI() *fakeAPI {
	fields := map[string]anki.Field{
		"Front": {Value: "bruyant / bruyante", Order: 0},
		"Back":  {Value: "noisy", Order: 1},
	}
	return &fakeAPI{
		decks: []string{"French A1", "French A1::Unit 1"},
		cards: []anki.Card{
			{CardID: 10, DeckName: "French A1", ModelName: "Basic (and reversed card)", NoteID: 5, Ordinal: 0, Fields: fields},
			{CardID: 11, DeckName: "French A1", ModelName: "Basic (and reversed card)", NoteID: 5, Ordinal: 1, Fields: fields},
			{CardID: 12, DeckName: "French A1::Unit 1", ModelName: "Basic", NoteID: 6, Ordinal: 0, Fields: fields},
		},
		notes: []anki.Note{
			{NoteID: 5, Tags: []string{"shared", "adjective"}},
			{NoteID: 6},
		},
		templates: map[string][]string{
			"Basic (and reversed card)": {"Card 1", "Card 2"},
			"Basic":                     {"Card 1"},
		},
	}
}

func TestBuildNoteSchemaExactDeckAndDeterministic(t *testing.T) {
	api := sampleAPI()
	var progress []Progress
	deck, err := BuildWithProgress(context.Background(), api, "French A1", func(item Progress) {
		progress = append(progress, item)
	})
	if err != nil {
		t.Fatal(err)
	}
	if api.query != `deck:"French A1"` {
		t.Fatalf("query = %q", api.query)
	}
	if deck.SchemaVersion != 2 || len(deck.Notes) != 1 {
		t.Fatalf("deck = %#v", deck)
	}
	note := deck.Notes[0]
	if note.Front != "bruyant / bruyante" || note.Back != "noisy" || !note.Reverse {
		t.Fatalf("note = %#v", note)
	}
	if note.Tags[0] != "adjective" || len(progress) != 2 || progress[1].CardType != "Card 2" {
		t.Fatalf("tags/progress = %#v / %#v", note.Tags, progress)
	}
	if progress[0].Current != 1 || progress[1].Current != 2 || progress[0].Total != 2 {
		t.Fatalf("progress counts = %#v", progress)
	}
	first, _ := Marshal(deck)
	second, _ := Marshal(deck)
	if string(first) != string(second) {
		t.Fatal("snapshot is not deterministic")
	}
	if !strings.Contains(string(first), "bruyant / bruyante") || strings.Contains(string(first), `"cards"`) {
		t.Fatal("schema v2 content is wrong")
	}
}

func TestSingleCardNoteIsNotReversed(t *testing.T) {
	api := sampleAPI()
	api.cards = api.cards[2:]
	api.cards[0].DeckName = "French A1"
	deck, err := Build(context.Background(), api, "French A1")
	if err != nil {
		t.Fatal(err)
	}
	if len(deck.Notes) != 1 || deck.Notes[0].Reverse {
		t.Fatalf("notes = %#v", deck.Notes)
	}
}

func TestTagsDoNotChangeIdentityButFieldsDo(t *testing.T) {
	api := sampleAPI()
	first, err := Build(context.Background(), api, "French A1")
	if err != nil {
		t.Fatal(err)
	}
	api.notes[0].Tags = []string{"changed"}
	second, _ := Build(context.Background(), api, "French A1")
	if first.Notes[0].ID != second.Notes[0].ID {
		t.Fatal("tags changed identity")
	}
	for i := 0; i < 2; i++ {
		api.cards[i].Fields["Front"] = anki.Field{Value: "silencieux", Order: 0}
	}
	third, _ := Build(context.Background(), api, "French A1")
	if first.Notes[0].ID == third.Notes[0].ID {
		t.Fatal("field edit did not change identity")
	}
}

func TestRejectsMediaUnsupportedFieldsAndEmptyDeck(t *testing.T) {
	media := []string{`[sound:word.mp3]`, `<img src="word.png">`, `<AUDIO src="word.mp3">`, `style="background:url(word.png)"`}
	for _, value := range media {
		api := sampleAPI()
		api.cards[0].Fields["Front"] = anki.Field{Value: value, Order: 0}
		if _, err := Build(context.Background(), api, "French A1"); err == nil || !strings.Contains(err.Error(), "media") {
			t.Fatalf("expected media error for %q, got %v", value, err)
		}
	}
	api := sampleAPI()
	api.cards[0].Fields["Extra"] = anki.Field{Value: "unsupported", Order: 2}
	if _, err := Build(context.Background(), api, "French A1"); err == nil || !strings.Contains(err.Error(), "exactly Front and Back") {
		t.Fatalf("expected field error, got %v", err)
	}
	api = sampleAPI()
	api.cards = nil
	if _, err := Build(context.Background(), api, "French A1"); err == nil || !strings.Contains(err.Error(), "no exportable cards") {
		t.Fatalf("expected empty error, got %v", err)
	}
}

func TestRejectsDuplicateNoteContent(t *testing.T) {
	api := sampleAPI()
	for _, source := range api.cards[:2] {
		copyCard := source
		copyCard.CardID += 10
		copyCard.NoteID = 7
		api.cards = append(api.cards, copyCard)
	}
	api.notes = append(api.notes, anki.Note{NoteID: 7, Tags: []string{"other"}})
	if _, err := Build(context.Background(), api, "French A1"); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestOneLine(t *testing.T) {
	if got := oneLine("front\nwith\tspaces"); got != "front with spaces" {
		t.Fatalf("oneLine = %q", got)
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{"French A1": "french-a1", " French--A1 ": "french-a1", "Été 2026": "été-2026"}
	for input, want := range cases {
		got, err := Slug(input)
		if err != nil || got != want {
			t.Fatalf("Slug(%q) = %q, %v", input, got, err)
		}
	}
}
