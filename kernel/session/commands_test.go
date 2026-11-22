package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCommands(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	os.MkdirAll(filepath.Join(dir, ".stratum", "commands"), 0o755)
	os.MkdirAll(filepath.Join(dir, ".claude", "commands"), 0o755)
	os.WriteFile(filepath.Join(dir, ".stratum", "commands", "ship.md"), []byte("---\ndescription: Ship it\n---\nRelease $ARGUMENTS\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".claude", "commands", "ship.md"), []byte("shadowed\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".claude", "commands", "lint.md"), []byte("# Run the linter\nRun golangci-lint.\n"), 0o644)
	got := commands(dir)
	if len(got) != 2 || got[0].Name != "ship" || got[0].Description != "Ship it" || got[0].Body != "Release $ARGUMENTS" {
		t.Fatalf("%+v", got)
	}
	if got[1].Name != "lint" || got[1].Description != "Run the linter" {
		t.Fatalf("%+v", got[1])
	}
}
