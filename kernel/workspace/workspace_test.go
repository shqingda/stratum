package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolve(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	out, _ := filepath.EvalSymlinks(t.TempDir())
	os.MkdirAll(filepath.Join(root, "src"), 0o755)
	os.Symlink(out, filepath.Join(root, "link"))
	for _, c := range []struct {
		in     string
		inside bool
		rel    string
	}{
		{"src/a.go", true, "src/a.go"},
		{"./src/../b.txt", true, "b.txt"},
		{".", true, "."},
		{filepath.Join(root, "new/dir/c"), true, "new/dir/c"}, // not there yet
		{"../x", false, ""},
		{"link/secret", false, ""}, // a symlink out
		{out, false, ""},
	} {
		p, err := Resolve(root, c.in)
		if err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		if p.Inside != c.inside || (c.inside && p.Rel != c.rel) {
			t.Errorf("%s: %+v", c.in, p)
		}
	}
}

func TestSensitive(t *testing.T) {
	for path, want := range map[string]bool{
		"/w/.git/config":      true,
		"/w/.env":             true,
		"/w/.env.local":       true,
		"/h/.ssh/id_ed25519":  true,
		"/w/certs/server.pem": true,
		"/w/api_token.txt":    true,
		"/w/main.go":          false,
		"/w/docs/environment": false,
		"/w/src/keyboard.go":  false,
		"/w/.github/ci.yml":   false,
	} {
		if got := Sensitive(path); got != want {
			t.Errorf("%s: %v", path, got)
		}
	}
}

func TestResolveDanglingInternal(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	for input, rel := range map[string]string{"link": "missing", "link/file": "missing/file"} {
		p, err := Resolve(root, input)
		if err != nil || !p.Inside || p.Abs != filepath.Join(root, rel) || p.Rel != rel {
			t.Errorf("%s: %+v, %v", input, p, err)
		}
	}
	// Cleaning a dangling target can expose a cycle that EvalSymlinks cannot follow.
	if err := os.Symlink("missing/../cycle", filepath.Join(root, "cycle")); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(root, "cycle"); err == nil {
		t.Fatal("resolved a dangling symlink cycle")
	}
}
