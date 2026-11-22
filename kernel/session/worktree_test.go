package session

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shqingda/stratum/kernel/store"
	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/proto"
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

func TestWorktreeMidSessionHandoff(t *testing.T) {
	ctx := context.Background()
	repo, _ := filepath.EvalSymlinks(t.TempDir())
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("init")
	runGit("config", "user.email", "test@example.com")
	runGit("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit("add", "main.go")
	runGit("commit", "-m", "init")

	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m := &Manager{Store: st, WorktreeDir: t.TempDir()}

	s, err := m.Create(ctx, proto.SessionCreate{Cwd: repo, Account: "script", Model: "script", Mode: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	// Mark session as started.
	if err := m.record(ctx, s.ID, proto.EventTurnStart, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	// Make local uncommitted edit + untracked file before handing off to worktree.
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n// edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "extra.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	on := true
	sWT, err := m.Update(ctx, proto.SessionUpdate{SessionID: s.ID, Worktree: &on})
	if err != nil || !sWT.Worktree || sWT.Cwd == repo {
		t.Fatalf("mid-session handoff to worktree failed: %+v %v", sWT, err)
	}
	b, err := os.ReadFile(filepath.Join(sWT.Cwd, "main.go"))
	if err != nil || string(b) != "package main\n// edited\n" {
		t.Fatalf("worktree did not receive local edit: %q %v", b, err)
	}
	if b2, err := os.ReadFile(filepath.Join(sWT.Cwd, "extra.txt")); err != nil || string(b2) != "hello" {
		t.Fatalf("worktree did not receive untracked file: %q %v", b2, err)
	}

	// Clean local repo and make a new edit in worktree, then hand off back to local.
	runGit("checkout", "--", "main.go")
	_ = os.Remove(filepath.Join(repo, "extra.txt"))
	if err := os.WriteFile(filepath.Join(sWT.Cwd, "main.go"), []byte("package main\n// from worktree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	off := false
	sLocal, err := m.Update(ctx, proto.SessionUpdate{SessionID: s.ID, Worktree: &off})
	if err != nil || sLocal.Worktree || sLocal.Cwd != repo {
		t.Fatalf("mid-session handoff to local failed: %+v %v", sLocal, err)
	}
	b3, err := os.ReadFile(filepath.Join(repo, "main.go"))
	if err != nil || string(b3) != "package main\n// from worktree\n" {
		t.Fatalf("local repo did not receive worktree edit: %q %v", b3, err)
	}
}
