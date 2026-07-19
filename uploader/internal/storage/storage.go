package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Header struct {
	DeckName string `json:"deck_name"`
}

func CheckCollision(path, deckName string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read existing snapshot: %w", err)
	}
	var header Header
	if err := json.Unmarshal(data, &header); err != nil {
		return fmt.Errorf("existing snapshot %s is invalid: %w", path, err)
	}
	if header.DeckName != deckName {
		return fmt.Errorf("deck slug collision: %s belongs to %q, not %q", path, header.DeckName, deckName)
	}
	return nil
}

func Same(path string, data []byte) (bool, error) {
	existing, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return bytes.Equal(existing, data), nil
}

func AtomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create deck output directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".deck-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temporary snapshot: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return fmt.Errorf("set temporary snapshot permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temporary snapshot: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temporary snapshot: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary snapshot: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace snapshot atomically: %w", err)
	}
	return nil
}
