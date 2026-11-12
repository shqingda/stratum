package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, root, line string) (string, error) {
	t.Helper()
	cmd, tmp, err := Command(context.Background(), root, line)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestSandbox(t *testing.T) {
	if !Available() {
		t.Skip("no sandbox-exec")
	}
	root := t.TempDir()
	outside := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".git"), 0o755)
	os.Symlink(outside, filepath.Join(root, "escape"))

	// Allowed: read and write the workspace, use $TMPDIR, run tools.
	if out, err := run(t, root, `echo hi > a.txt && cat a.txt && echo t > "$TMPDIR/x" && pwd`); err != nil || !strings.Contains(out, "hi") {
		t.Fatalf("workspace write: %v %s", err, out)
	}
	if out, err := run(t, root, `git --version`); err != nil {
		t.Errorf("git: %v %s", err, out)
	}

	// Refused.
	denied := map[string]string{
		"outside":   "echo x > " + filepath.Join(outside, "x"),
		"symlink":   "echo x > escape/x",
		"dotdot":    "echo x > ../talos-escape-test",
		".git":      "echo x > .git/config",
		"network":   "/usr/bin/curl -sS -m 5 https://example.com",
		"home file": "echo x > ~/talos-sandbox-test",
	}
	for name, line := range denied {
		if out, err := run(t, root, line); err == nil {
			t.Errorf("%s: allowed: %s", name, out)
		}
	}
	for _, p := range []string{filepath.Join(outside, "x"), filepath.Join(filepath.Dir(root), "talos-escape-test")} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s was written", p)
		}
	}
	home, _ := os.UserHomeDir()
	if _, err := os.Stat(filepath.Join(home, "talos-sandbox-test")); err == nil {
		os.Remove(filepath.Join(home, "talos-sandbox-test"))
		t.Error("wrote the home directory")
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh")); err == nil {
		if out, err := run(t, root, "ls ~/.ssh"); err == nil {
			t.Errorf("read ~/.ssh: %s", out)
		}
	}
}

func TestEnvDropsSecrets(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "x")
	t.Setenv("GH_TOKEN", "x")
	for _, kv := range env() {
		if strings.HasPrefix(kv, "OPENAI_API_KEY=") || strings.HasPrefix(kv, "GH_TOKEN=") {
			t.Error(kv)
		}
	}
}

func TestGoTestInSandbox(t *testing.T) {
	if !Available() || testing.Short() {
		t.Skip()
	}
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n\ngo 1.22\n"), 0o644)
	os.WriteFile(filepath.Join(root, "x_test.go"), []byte("package x\nimport \"testing\"\nfunc TestX(t *testing.T) {}\n"), 0o644)
	if out, err := run(t, root, "go test ./..."); err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("%v %s", err, out)
	}
}
