package rpc

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shqingda/talos/proto"
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
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("1\n2\n"), 0o644)
	if w, _ := workspace(ctx, dir); !w.Dirty || w.Added != 0 {
		t.Fatalf("untracked: %+v", w)
	}
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@t")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@t")
	if w, err := commit(ctx, proto.Commit{Cwd: dir, Message: "add a"}); err != nil || w.Dirty {
		t.Fatalf("commit: %+v %v", w, err)
	}
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("1\n3\n4\n"), 0o644)
	if w, _ := workspace(ctx, dir); !w.Dirty || w.Added != 2 || w.Removed != 1 {
		t.Fatalf("edited: %+v", w)
	}
	if _, err := commit(ctx, proto.Commit{Cwd: dir, Message: " "}); err == nil {
		t.Fatal("an empty message was taken")
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
