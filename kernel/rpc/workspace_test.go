package rpc

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shqingda/talos/kernel/session"
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
	if w, _ := workspace(ctx, dir); !w.Dirty || w.Added != 2 || len(w.Files) != 1 || w.Files[0].Path != filepath.Join(dir, "a.txt") {
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
	if w, _ := workspace(ctx, dir); !w.Dirty || w.Added != 2 || w.Removed != 1 || len(w.Files) != 1 || w.Files[0].Path != filepath.Join(dir, "a.txt") {
		t.Fatalf("edited: %+v", w)
	}
	d, err := workspaceDiff(ctx, proto.WorkspaceQuery{Cwd: dir, Path: filepath.Join(dir, "a.txt")})
	if err != nil || d.Before == nil || *d.Before != "1\n2\n" || d.After == nil || *d.After != "1\n3\n4\n" {
		t.Fatalf("diff %+v %v", d, err)
	}
	if d, _ := workspaceDiff(ctx, proto.WorkspaceQuery{Cwd: dir, Path: filepath.Join(dir, "new.txt")}); d.Before != nil || d.After != nil {
		t.Fatalf("a missing file: %+v", d)
	}
	if _, err := workspaceDiff(ctx, proto.WorkspaceQuery{Cwd: dir, Path: "/etc/hosts"}); err == nil {
		t.Fatal("a file outside the repository was read")
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

func TestLoginCancel(t *testing.T) {
	started := make(chan struct{})
	s := &Server{Sessions: session.New(nil, nil), Login: func(ctx context.Context, q proto.Login, progress func(proto.LoginProgress)) (proto.Account, error) {
		close(started)
		<-ctx.Done()
		return proto.Account{}, ctx.Err()
	}}
	done := make(chan error, 1)
	go func() {
		_, err := s.handle(context.Background(), proto.Command{Method: proto.MethodLogin, Params: []byte(`{"provider":"codex","method":"codex"}`)})
		done <- err
	}()
	<-started
	if _, err := s.handle(context.Background(), proto.Command{Method: proto.MethodLoginCancel}); err != nil {
		t.Fatal(err)
	}
	var pe *proto.Error
	if err := <-done; !errors.As(err, &pe) || pe.Code != "cancelled" {
		t.Fatalf("cancelled login: %v", err)
	}
}

// Against its branch, a folder's changes are those since it left main,
// committed or not.
func TestBranchBase(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir, ctx := t.TempDir(), context.Background()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v", out, err)
		}
	}
	a := filepath.Join(dir, "a.txt")
	run("init", "-q", "-b", "main")
	os.WriteFile(a, []byte("1\n"), 0o644)
	run("add", "a.txt")
	run("commit", "-q", "-m", "a")
	run("checkout", "-q", "-b", "feature")
	os.WriteFile(a, []byte("1\n2\n"), 0o644)
	run("commit", "-q", "-am", "two")
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644)
	w, err := workspace(ctx, dir)
	if err != nil || w.Base != "main" || len(w.Files) != 1 || len(w.BaseFiles) != 2 || w.BaseFiles[0].Added != 1 {
		t.Fatalf("workspace %+v %v", w, err)
	}
	d, err := workspaceDiff(ctx, proto.WorkspaceQuery{Cwd: dir, Path: a, Base: true})
	if err != nil || d.Before == nil || *d.Before != "1\n" || *d.After != "1\n2\n" {
		t.Fatalf("diff %+v %v", d, err)
	}
}

func TestPushCreatePRAndChecks(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	ctx := context.Background()
	origin := t.TempDir()
	if out, err := exec.Command("git", "init", "--bare", "-q", origin).CombinedOutput(); err != nil {
		t.Fatalf("bare init: %s %v", out, err)
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s %v", args, out, err)
		}
	}
	run("init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("1\n"), 0o644)
	run("add", "a.txt")
	run("commit", "-q", "-m", "init")
	run("remote", "add", "origin", origin)
	run("push", "-q", "-u", "origin", "main")
	run("checkout", "-q", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("2\n"), 0o644)
	run("commit", "-q", "-am", "feat commit")

	var prCreated bool
	var ghCalls []string
	oldGH := ghRun
	ghRun = func(_ context.Context, cwd string, args ...string) ([]byte, error) {
		ghCalls = append(ghCalls, strings.Join(args, " "))
		if len(args) >= 2 && args[0] == "pr" && args[1] == "create" {
			prCreated = true
			return []byte("https://github.com/example/repo/pull/42\n"), nil
		}
		if len(args) >= 2 && args[0] == "pr" && args[1] == "view" && prCreated {
			return []byte(`{"number":42,"title":"Add feature","url":"https://github.com/example/repo/pull/42","state":"OPEN","statusCheckRollup":[{"conclusion":"SUCCESS"},{"status":"COMPLETED","conclusion":"FAILURE"}]}`), nil
		}
		return nil, errors.New("no pr")
	}
	defer func() { ghRun = oldGH }()

	w, err := workspace(ctx, dir)
	if err != nil || w.Ahead != 1 || w.PR != nil {
		t.Fatalf("before push: %+v %v", w, err)
	}
	w, err = push(ctx, proto.WorkspaceQuery{Cwd: dir})
	if err != nil || w.Ahead != 0 {
		t.Fatalf("after push: %+v %v", w, err)
	}
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("3\n"), 0o644)
	run("commit", "-q", "-am", "second commit")
	w, err = createPR(ctx, proto.CreatePR{Cwd: dir, Title: "Add feature"})
	if err != nil || !prCreated || w.Ahead != 0 || w.PR == nil || w.PR.Number != 42 || w.PR.Checks != "fail" {
		t.Fatalf("after createPR: %+v %v calls=%v", w, err, ghCalls)
	}

	if len(w.Branches) != 2 || w.Branch != "feat" {
		t.Fatalf("branches = %v current %q", w.Branches, w.Branch)
	}
	w, err = checkout(ctx, proto.Checkout{Cwd: dir, Branch: "main"})
	if err != nil || w.Branch != "main" {
		t.Fatalf("checkout main: %+v %v", w, err)
	}
}
