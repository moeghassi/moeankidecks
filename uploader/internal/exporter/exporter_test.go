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
		"Translation": {Value: "noisy", Order: 1},
		"Word":        {Value: "bruyant / bruyante", Order: 0},
	}
	return &fakeAPI{
		decks: []string{"French A1", "French A1::Unit 1"},
		cards: []anki.Card{
			{CardID: 10, DeckName: "French A1", ModelName: "French", NoteID: 5, Ordinal: 1, Fields: fields},
			{CardID: 11, DeckName: "French A1::Unit 1", ModelName: "French", NoteID: 6, Ordinal: 0, Fields: fields},
		},
		notes:     []anki.Note{{NoteID: 5, Tags: []string{"shared", "adjective"}}, {NoteID: 6}},
		templates: map[string][]string{"French": {"Forward", "Reverse"}},
	}
}

func TestBuildExactDeckDeterministic(t *testing.T) {
	api := sampleAPI()
	deck, err := Build(context.Background(), api, "French A1")
	if err != nil {
		t.Fatal(err)
	}
	if api.query != `deck:"French A1"` {
		t.Fatalf("query = %q", api.query)
	}
	if len(deck.Cards) != 1 {
		t.Fatalf("cards = %d", len(deck.Cards))
	}
	card := deck.Cards[0]
	if card.CardType != "Reverse" || card.CardOrdinal != 1 {
		t.Fatalf("card type = %#v", card)
	}
	if card.Fields[0].Name != "Word" || card.Tags[0] != "adjective" {
		t.Fatalf("ordering failed: %#v", card)
	}
	first, _ := Marshal(deck)
	second, _ := Marshal(deck)
	if string(first) != string(second) {
		t.Fatal("snapshot is not deterministic")
	}
	if !strings.Contains(string(first), "bruyant / bruyante") {
		t.Fatal("UTF-8 content missing")
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
	if first.Cards[0].ID != second.Cards[0].ID {
		t.Fatal("tags changed identity")
	}
	api.cards[0].Fields["Word"] = anki.Field{Value: "silencieux", Order: 0}
	third, _ := Build(context.Background(), api, "French A1")
	if first.Cards[0].ID == third.Cards[0].ID {
		t.Fatal("field edit did not change identity")
	}
}

func TestRejectsMediaAndEmptyDeck(t *testing.T) {
	media := []string{`[sound:word.mp3]`, `<img src="word.png">`, `<AUDIO src="word.mp3">`, `style="background:url(word.png)"`}
	for _, value := range media {
		api := sampleAPI()
		api.cards[0].Fields["Word"] = anki.Field{Value: value, Order: 0}
		if _, err := Build(context.Background(), api, "French A1"); err == nil || !strings.Contains(err.Error(), "media") {
			t.Fatalf("expected media error for %q, got %v", value, err)
		}
	}
	api := sampleAPI()
	api.cards = nil
	if _, err := Build(context.Background(), api, "French A1"); err == nil || !strings.Contains(err.Error(), "no exportable cards") {
		t.Fatalf("expected empty error, got %v", err)
	}
}

func TestRejectsDuplicateContent(t *testing.T) {
	api := sampleAPI()
	copyCard := api.cards[0]
	copyCard.CardID = 12
	copyCard.NoteID = 7
	api.cards = append(api.cards, copyCard)
	api.notes = append(api.notes, anki.Note{NoteID: 7, Tags: []string{"other"}})
	if _, err := Build(context.Background(), api, "French A1"); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate error, got %v", err)
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
