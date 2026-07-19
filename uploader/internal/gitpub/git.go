package gitpub

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type Publisher struct{ Root string }

func (p Publisher) Prepare(ctx context.Context) error {
	status, err := p.output(ctx, "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "" {
		return fmt.Errorf("--push requires a completely clean Git worktree")
	}
	branch, err := p.output(ctx, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || strings.TrimSpace(branch) == "" {
		return fmt.Errorf("--push requires a non-detached Git branch")
	}
	if _, err := p.output(ctx, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); err != nil {
		return fmt.Errorf("current branch must track an upstream branch: %w", err)
	}
	if _, err := p.output(ctx, "fetch", "origin"); err != nil {
		return fmt.Errorf("fetch origin: %w", err)
	}
	head, err := p.output(ctx, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	upstream, err := p.output(ctx, "rev-parse", "@{u}")
	if err != nil {
		return err
	}
	if strings.TrimSpace(head) != strings.TrimSpace(upstream) {
		return fmt.Errorf("local branch must exactly match its upstream before --push")
	}
	return nil
}

func (p Publisher) Publish(ctx context.Context, relativePaths []string, deckName string) error {
	if len(relativePaths) == 0 {
		return fmt.Errorf("no publication paths provided")
	}
	pathArgs := append([]string{"add", "--"}, relativePaths...)
	if _, err := p.output(ctx, pathArgs...); err != nil {
		return err
	}
	diffArgs := append([]string{"diff", "--cached", "--quiet", "--"}, relativePaths...)
	cmd := exec.CommandContext(ctx, "git", diffArgs...)
	cmd.Dir = p.Root
	if err := cmd.Run(); err == nil {
		return nil
	} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		return fmt.Errorf("check staged snapshot: %w", err)
	}
	commitArgs := append([]string{"commit", "--only", "-m", "Publish deck: " + deckName, "--"}, relativePaths...)
	if _, err := p.output(ctx, commitArgs...); err != nil {
		return fmt.Errorf("commit snapshot: %w", err)
	}
	branch, err := p.output(ctx, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return err
	}
	if _, err := p.output(ctx, "push", "origin", strings.TrimSpace(branch)); err != nil {
		return fmt.Errorf("push snapshot (the local commit was retained): %w", err)
	}
	return nil
}

func (p Publisher) output(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = p.Root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), message)
	}
	return stdout.String(), nil
}
