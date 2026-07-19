package anki

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestClientUsesOnlyReadActionsAndPreservesTemplateOrder(t *testing.T) {
	allowed := map[string]bool{"deckNames": true, "findCards": true, "cardsInfo": true, "notesInfo": true, "modelTemplates": true}
	var actions []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Action  string         `json:"action"`
			Version int            `json:"version"`
			Params  map[string]any `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if !allowed[req.Action] {
			t.Errorf("unexpected or mutating action %q", req.Action)
		}
		if req.Version != apiVersion {
			t.Errorf("version = %d", req.Version)
		}
		if req.Action == "findCards" && req.Params["query"] != `deck:"French A1"` {
			t.Errorf("findCards query = %q", req.Params["query"])
		}
		actions = append(actions, req.Action)
		w.Header().Set("Content-Type", "application/json")
		switch req.Action {
		case "deckNames":
			fmt.Fprint(w, `{"result":["French A1"],"error":null}`)
		case "findCards":
			fmt.Fprint(w, `{"result":[10],"error":null}`)
		case "cardsInfo":
			fmt.Fprint(w, `{"result":[{"cardId":10,"deckName":"French A1","modelName":"French","note":5,"ord":1,"fields":{"Word":{"value":"été","order":0}},"interval":99,"due":42,"reps":7}],"error":null}`)
		case "notesInfo":
			fmt.Fprint(w, `{"result":[{"noteId":5,"tags":["shared"]}],"error":null}`)
		case "modelTemplates":
			fmt.Fprint(w, `{"result":{"Forward":{"Front":"{{Word}}"},"Reverse":{"Front":"{{Translation}}"}},"error":null}`)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()
	decks, err := client.DeckNames(ctx)
	if err != nil || !reflect.DeepEqual(decks, []string{"French A1"}) {
		t.Fatalf("DeckNames = %#v, %v", decks, err)
	}
	if _, err := client.FindCards(ctx, `deck:\"French A1\"`); err != nil {
		t.Fatal(err)
	}
	cards, err := client.CardsInfo(ctx, []int64{10})
	if err != nil || len(cards) != 1 || cards[0].Fields["Word"].Value != "été" {
		t.Fatalf("CardsInfo = %#v, %v", cards, err)
	}
	if _, err := client.NotesInfo(ctx, []int64{5}); err != nil {
		t.Fatal(err)
	}
	templates, err := client.TemplateNames(ctx, "French")
	if err != nil || !reflect.DeepEqual(templates, []string{"Forward", "Reverse"}) {
		t.Fatalf("TemplateNames = %#v, %v", templates, err)
	}
	if len(actions) != 5 {
		t.Fatalf("actions = %#v", actions)
	}
}

func TestClientSurfacesAnkiError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"result":null,"error":"boom"}`)
	}))
	defer server.Close()
	if _, err := NewClient(server.URL).DeckNames(context.Background()); err == nil {
		t.Fatal("expected AnkiConnect error")
	}
}
