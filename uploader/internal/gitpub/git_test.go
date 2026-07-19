package gitpub

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareAndPublish(t *testing.T) {
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	seed := filepath.Join(base, "seed")
	work := filepath.Join(base, "work")
	runGit(t, base, "init", "--bare", remote)
	runGit(t, base, "init", "-b", "main", seed)
	runGit(t, seed, "config", "user.name", "Uploader Test")
	runGit(t, seed, "config", "user.email", "uploader@example.invalid")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, seed, "add", "README.md")
	runGit(t, seed, "commit", "-m", "initial")
	runGit(t, seed, "remote", "add", "origin", remote)
	runGit(t, seed, "push", "-u", "origin", "main")
	runGit(t, base, "clone", "-b", "main", remote, work)
	runGit(t, work, "config", "user.name", "Uploader Test")
	runGit(t, work, "config", "user.email", "uploader@example.invalid")

	p := Publisher{Root: work}
	if err := p.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(work, "decks", "french-a1", "deck.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := p.Publish(context.Background(), "decks/french-a1/deck.json", "French A1"); err != nil {
		t.Fatal(err)
	}
	message := strings.TrimSpace(runGit(t, work, "log", "-1", "--pretty=%s"))
	if message != "Publish deck: French A1" {
		t.Fatalf("commit message = %q", message)
	}
	local := strings.TrimSpace(runGit(t, work, "rev-parse", "HEAD"))
	remoteHead := strings.TrimSpace(runGit(t, work, "ls-remote", remote, "refs/heads/main"))
	if !strings.HasPrefix(remoteHead, local+"\t") {
		t.Fatalf("remote head %q does not match %q", remoteHead, local)
	}
}

func TestPrepareRejectsDirtyWorktree(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "dirty.txt"), []byte("dirty"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := (Publisher{Root: dir}).Prepare(context.Background())
	if err == nil || !strings.Contains(err.Error(), "clean") {
		t.Fatalf("expected clean-worktree error, got %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}
