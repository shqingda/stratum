package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func TestMatch(t *testing.T) {
	files := []string{"app/composer.go", "app/composer_test.go", "kernel/session/compact.go", "README.md", "client/client.go"}
	if got := Match(files, "comp", 3); got[0] != "app/composer.go" {
		t.Errorf("comp → %q", got)
	}
	if got := Match(files, "cmpct", 5); !slices.Equal(got, []string{"kernel/session/compact.go"}) {
		t.Errorf("cmpct → %q", got)
	}
	if got := Match(files, "readme", 5); !slices.Equal(got, []string{"README.md"}) {
		t.Errorf("readme → %q", got)
	}
	if got := Match(files, "", 2); len(got) != 2 {
		t.Errorf("empty → %q", got)
	}
	if got := Match(files, "zzz", 5); len(got) != 0 {
		t.Errorf("zzz → %q", got)
	}
}

func TestFiles(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"a.go", "sub/b.go", "node_modules/x.js", ".hidden/y"} {
		os.MkdirAll(filepath.Join(root, filepath.Dir(f)), 0o755)
		os.WriteFile(filepath.Join(root, f), nil, 0o644)
	}
	got := Files(context.Background(), root)
	slices.Sort(got)
	if !slices.Equal(got, []string{"a.go", "sub/b.go"}) {
		t.Errorf("no git: %q", got)
	}
	if exec.Command("git", "-C", root, "init", "-q").Run() != nil {
		t.Skip("no git")
	}
	os.WriteFile(filepath.Join(root, ".gitignore"), []byte("sub/\n"), 0o644)
	got = Files(context.Background(), root)
	slices.Sort(got)
	if !slices.Equal(got, []string{".gitignore", ".hidden/y", "a.go", "node_modules/x.js"}) {
		t.Errorf("git: %q", got)
	}
}
