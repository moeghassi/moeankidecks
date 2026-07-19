package anki

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

const apiVersion = 6

type Field struct {
	Value string `json:"value"`
	Order int    `json:"order"`
}

type Card struct {
	CardID    int64            `json:"cardId"`
	DeckName  string           `json:"deckName"`
	ModelName string           `json:"modelName"`
	NoteID    int64            `json:"note"`
	Ordinal   int              `json:"ord"`
	Fields    map[string]Field `json:"fields"`
}

type Note struct {
	NoteID int64    `json:"noteId"`
	Tags   []string `json:"tags"`
}

// API contains only the read operations used by the exporter.
type API interface {
	DeckNames(context.Context) ([]string, error)
	FindCards(context.Context, string) ([]int64, error)
	CardsInfo(context.Context, []int64) ([]Card, error)
	NotesInfo(context.Context, []int64) ([]Note, error)
	TemplateNames(context.Context, string) ([]string, error)
}

type Client struct {
	endpoint string
	http     *http.Client
}

func NewClient(endpoint string) *Client {
	return &Client{endpoint: endpoint, http: &http.Client{Timeout: 30 * time.Second}}
}

type request struct {
	Action  string `json:"action"`
	Version int    `json:"version"`
	Params  any    `json:"params,omitempty"`
}

type response struct {
	Result json.RawMessage `json:"result"`
	Error  *string         `json:"error"`
}

func (c *Client) invoke(ctx context.Context, action string, params, result any) error {
	body, err := json.Marshal(request{Action: action, Version: apiVersion, Params: params})
	if err != nil {
		return fmt.Errorf("encode %s request: %w", action, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create %s request: %w", action, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call AnkiConnect action %s: %w", action, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("AnkiConnect action %s returned HTTP %s", action, resp.Status)
	}
	var envelope response
	dec := json.NewDecoder(io.LimitReader(resp.Body, 64<<20))
	if err := dec.Decode(&envelope); err != nil {
		return fmt.Errorf("decode AnkiConnect action %s: %w", action, err)
	}
	if envelope.Error != nil {
		return fmt.Errorf("AnkiConnect action %s: %s", action, *envelope.Error)
	}
	if envelope.Result == nil || bytes.Equal(envelope.Result, []byte("null")) {
		return fmt.Errorf("AnkiConnect action %s returned no result", action)
	}
	if err := json.Unmarshal(envelope.Result, result); err != nil {
		return fmt.Errorf("decode AnkiConnect %s result: %w", action, err)
	}
	return nil
}

func (c *Client) DeckNames(ctx context.Context) ([]string, error) {
	var names []string
	if err := c.invoke(ctx, "deckNames", nil, &names); err != nil {
		return nil, err
	}
	return names, nil
}

func (c *Client) FindCards(ctx context.Context, query string) ([]int64, error) {
	var ids []int64
	// Accept escaped outer quotes from callers, but send Anki's actual search syntax.
	query = strings.ReplaceAll(query, "\\\"", "\"")
	err := c.invoke(ctx, "findCards", map[string]any{"query": query}, &ids)
	return ids, err
}

func (c *Client) CardsInfo(ctx context.Context, ids []int64) ([]Card, error) {
	var cards []Card
	err := c.invoke(ctx, "cardsInfo", map[string]any{"cards": ids}, &cards)
	return cards, err
}

func (c *Client) NotesInfo(ctx context.Context, ids []int64) ([]Note, error) {
	var notes []Note
	err := c.invoke(ctx, "notesInfo", map[string]any{"notes": ids}, &notes)
	return notes, err
}

// TemplateNames preserves AnkiConnect's object member order. Anki emits model
// templates in ordinal order, while decoding into a Go map would lose it.
func (c *Client) TemplateNames(ctx context.Context, modelName string) ([]string, error) {
	var raw json.RawMessage
	if err := c.invoke(ctx, "modelTemplates", map[string]any{"modelName": modelName}, &raw); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, fmt.Errorf("modelTemplates for %q is not an object", modelName)
	}
	var names []string
	for dec.More() {
		name, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("decode template name for %q: %w", modelName, err)
		}
		var ignored json.RawMessage
		if err := dec.Decode(&ignored); err != nil {
			return nil, fmt.Errorf("decode template %q for %q: %w", name, modelName, err)
		}
		names = append(names, name.(string))
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("note type %q has no card templates", modelName)
	}
	return names, nil
}

func UniqueNoteIDs(cards []Card) []int64 {
	seen := make(map[int64]struct{}, len(cards))
	for _, card := range cards {
		seen[card.NoteID] = struct{}{}
	}
	ids := make([]int64, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}
