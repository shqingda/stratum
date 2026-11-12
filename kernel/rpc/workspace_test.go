package rpc

import (
	"context"
	"os/exec"
	"testing"
)

func TestWorkspace(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	ctx := context.Background()
	if w, err := workspace(ctx, dir); err != nil || w.Branch != "" {
		t.Fatalf("outside git: %+v %v", w, err)
	}
	for _, args := range [][]string{{"init", "-q", "-b", "fix-add"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "x"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v", out, err)
		}
	}
	if w, err := workspace(ctx, dir); err != nil || w.Branch != "fix-add" {
		t.Fatalf("on a branch: %+v %v", w, err)
	}
	if out, err := exec.Command("git", "-C", dir, "checkout", "-q", "--detach").CombinedOutput(); err != nil {
		t.Fatalf("%s: %v", out, err)
	}
	if w, _ := workspace(ctx, dir); len(w.Branch) < 7 {
		t.Fatalf("detached: %+v", w)
	}
	if _, err := workspace(ctx, "relative"); err == nil {
		t.Fatal("a relative cwd was taken")
	}
}
