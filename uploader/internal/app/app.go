package app

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/moeghassi/moeankidecks/uploader/internal/anki"
	"github.com/moeghassi/moeankidecks/uploader/internal/exporter"
	"github.com/moeghassi/moeankidecks/uploader/internal/gitpub"
	"github.com/moeghassi/moeankidecks/uploader/internal/storage"
)

const endpoint = "http://127.0.0.1:8765"

func Run(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("uploader", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	push := flags.Bool("push", false, "commit and push the generated snapshot")
	if err := flags.Parse(args); err != nil {
		return usageError()
	}
	if flags.NArg() != 1 || flags.Arg(0) == "" {
		return usageError()
	}
	deckName := flags.Arg(0)
	root, err := repositoryRoot()
	if err != nil {
		return err
	}
	publisher := gitpub.Publisher{Root: root}
	if *push {
		if err := publisher.Prepare(ctx); err != nil {
			return err
		}
	}

	deck, err := exporter.Build(ctx, anki.NewClient(endpoint), deckName)
	if err != nil {
		return err
	}
	data, err := exporter.Marshal(deck)
	if err != nil {
		return err
	}
	relativePath := filepath.Join("decks", deck.DeckID, "deck.json")
	path := filepath.Join(root, relativePath)
	if err := storage.CheckCollision(path, deckName); err != nil {
		return err
	}
	same, err := storage.Same(path, data)
	if err != nil {
		return fmt.Errorf("compare existing snapshot: %w", err)
	}
	if same {
		fmt.Fprintf(out, "Deck %q is unchanged (%d cards); no file or Git changes were made.\n", deckName, len(deck.Cards))
		return nil
	}
	if err := storage.AtomicWrite(path, data); err != nil {
		return err
	}
	fmt.Fprintf(out, "Wrote %d cards to %s\n", len(deck.Cards), path)
	if *push {
		if err := publisher.Publish(ctx, relativePath, deckName); err != nil {
			return err
		}
		fmt.Fprintf(out, "Committed and pushed deck %q.\n", deckName)
	}
	return nil
}

func usageError() error { return fmt.Errorf("usage: uploader [--push] \"<deck name>\"") }

func repositoryRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if info, err := os.Stat(filepath.Join(dir, ".git")); err == nil && info.IsDir() {
			if _, err := os.Stat(filepath.Join(dir, "uploader", "go.mod")); err == nil {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("could not locate the moeankidecks repository root")
}
