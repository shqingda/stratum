package session

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shqingda/talos/kernel/store"
	"github.com/shqingda/talos/llm"
	"github.com/shqingda/talos/proto"
)

func TestWorktreeLifecycle(t *testing.T) {
	repo := t.TempDir()
	runGit := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	runGit("init")
	runGit("config", "user.name", "demo")
	runGit("config", "user.email", "demo@example.test")
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(".env\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".worktreeinclude"), []byte("# copy secret\n.env\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".env"), []byte("KEY=secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit("add", "main.go", ".gitignore", ".worktreeinclude")
	runGit("commit", "-m", "init")

	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m := New(st, func(string) (llm.Provider, error) { return &llm.Script{}, nil })
	m.WorktreeDir = t.TempDir()

	ctx := context.Background()
	s, err := m.Create(ctx, proto.SessionCreate{Cwd: repo, Account: "script", Model: "script", Mode: "auto", Worktree: true})
	if err != nil {
		t.Fatal(err)
	}
	if !s.Worktree || s.Repo == "" || s.Cwd == repo {
		t.Fatalf("expected worktree session, got %+v", s)
	}
	wtPath := s.Cwd
	if b, err := os.ReadFile(filepath.Join(wtPath, ".env")); err != nil || string(b) != "KEY=secret\n" {
		t.Fatalf(".worktreeinclude did not copy .env: %q %v", b, err)
	}

	// Switch back to local before any turn starts: cleans up worktree and restores cwd.
	off := false
	s2, err := m.Update(ctx, proto.SessionUpdate{SessionID: s.ID, Worktree: &off})
	if err != nil {
		t.Fatal(err)
	}
	if s2.Worktree || s2.Repo != "" || s2.Cwd != s.Repo {
		t.Fatalf("expected local session, got %+v", s2)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Fatalf("worktree path %s still exists", wtPath)
	}

	// Enable again and delete session: cleans up worktree on Delete.
	on := true
	s3, err := m.Update(ctx, proto.SessionUpdate{SessionID: s.ID, Worktree: &on})
	if err != nil || !s3.Worktree {
		t.Fatalf("re-enable worktree: %+v %v", s3, err)
	}
	wtPath2 := s3.Cwd
	if err := m.Delete(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wtPath2); !os.IsNotExist(err) {
		t.Fatalf("deleted session left worktree %s", wtPath2)
	}
}
