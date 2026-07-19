package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAtomicWriteAndCollision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deck", "deck.json")
	data := []byte("{\"deck_name\":\"French A1\"}\n")
	if err := AtomicWrite(path, data); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(data) {
		t.Fatalf("read = %q, %v", got, err)
	}
	if err := CheckCollision(path, "French A1"); err != nil {
		t.Fatal(err)
	}
	if err := CheckCollision(path, "French-A1"); err == nil || !strings.Contains(err.Error(), "collision") {
		t.Fatalf("expected collision, got %v", err)
	}
}
