package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shqingda/talos/kernel/perm"
	"github.com/shqingda/talos/kernel/sandbox"
)

func call(t *testing.T, env Env, tool *Tool, args any) (string, error) {
	t.Helper()
	b, _ := json.Marshal(args)
	if _, err := tool.Access(env, b); err != nil {
		return "", err
	}
	return tool.Run(context.Background(), env, b)
}

func TestFileTools(t *testing.T) {
	env := Env{Root: t.TempDir()}
	if out, err := call(t, env, WriteFile, map[string]any{"path": "src/a.go", "content": "package a\n\nfunc A() {}\nfunc A() {}\n"}); err != nil || !strings.HasPrefix(out, "Created") {
		t.Fatal(out, err)
	}
	out, err := call(t, env, ReadFile, map[string]any{"path": "src/a.go", "offset": 3, "limit": 1})
	if err != nil || !strings.Contains(out, "     3\tfunc A() {}") || !strings.Contains(out, "offset 4") {
		t.Errorf("%q %v", out, err)
	}
	if _, err := call(t, env, EditFile, map[string]any{"path": "src/a.go", "old_text": "func A() {}", "new_text": "func B() {}"}); err == nil || !strings.Contains(err.Error(), "2 times") {
		t.Errorf("ambiguous edit: %v", err)
	}
	if _, err := call(t, env, EditFile, map[string]any{"path": "src/a.go", "old_text": "nope", "new_text": "x"}); err == nil {
		t.Error("edited missing text")
	}
	if out, err := call(t, env, EditFile, map[string]any{"path": "src/a.go", "old_text": "func A() {}", "new_text": "func B() {}", "replace_all": true}); err != nil || !strings.Contains(out, "2 replacements") {
		t.Error(out, err)
	}
	if b, _ := os.ReadFile(filepath.Join(env.Root, "src/a.go")); strings.Contains(string(b), "A()") {
		t.Error(string(b))
	}
	if out, _ := call(t, env, ListDir, map[string]any{"path": "."}); out != "src/\n" {
		t.Errorf("%q", out)
	}
	if out, err := call(t, env, Search, map[string]any{"pattern": "func B", "glob": "*.go"}); err != nil || !strings.HasPrefix(out, "src/a.go:3:") {
		t.Errorf("%q %v", out, err)
	}
	if out, _ := call(t, env, Search, map[string]any{"pattern": "zzz"}); out != "No matches." {
		t.Errorf("%q", out)
	}
	if _, err := call(t, env, ReadFile, map[string]any{}); err == nil {
		t.Error("read without a path")
	}
	os.WriteFile(filepath.Join(env.Root, "bin"), []byte{0, 1, 2}, 0o644)
	if _, err := call(t, env, ReadFile, map[string]any{"path": "bin"}); err == nil {
		t.Error("read a binary file")
	}
}

func TestAccess(t *testing.T) {
	env := Env{Root: t.TempDir()}
	req, _ := WriteFile.Access(env, json.RawMessage(`{"path":"../x","content":""}`))
	if req.Kind != perm.Write || req.Paths[0].Inside {
		t.Errorf("%+v", req)
	}
	req, _ = Shell.Access(env, json.RawMessage(`{"command":"go test","unrestricted":true}`))
	if req.Kind != perm.Exec || !req.Unrestricted || req.Command != "go test" {
		t.Errorf("%+v", req)
	}
	if _, err := Shell.Access(env, json.RawMessage(`{"command":""}`)); err == nil {
		t.Error("empty command")
	}
	if _, err := ReadFile.Access(env, json.RawMessage(`{"path":`)); err == nil {
		t.Error("bad json")
	}
}

func TestShellAndGit(t *testing.T) {
	env := Env{Root: t.TempDir(), Sandboxed: sandbox.Available()}
	if out, _ := call(t, env, Shell, map[string]any{"command": "echo hi; exit 3"}); out != "hi\n[exit status 3]" {
		t.Errorf("%q", out)
	}
	if out, _ := call(t, env, Shell, map[string]any{"command": "sleep 5", "timeout": 1}); !strings.Contains(out, "killed after 1s") {
		t.Errorf("%q", out)
	}
	if env.Sandboxed {
		if out, _ := call(t, env, Shell, map[string]any{"command": "echo x > ../escape"}); !strings.Contains(out, "exit status") {
			t.Errorf("sandbox let a write out: %q", out)
		}
	}
	if _, err := exec.LookPath("git"); err != nil {
		return
	}
	exec.Command("git", "-C", env.Root, "init", "-q").Run()
	os.WriteFile(filepath.Join(env.Root, "f"), []byte("x\n"), 0o644)
	if out, _ := call(t, env, GitStatus, map[string]any{}); !strings.Contains(out, "?? f") {
		t.Errorf("%q", out)
	}
	if out, _ := call(t, env, GitDiff, map[string]any{}); out != "No changes." {
		t.Errorf("%q", out)
	}
}

func TestClip(t *testing.T) {
	s := strings.Repeat("a", 100) + strings.Repeat("中", 100)
	c := clip(s, 50)
	if len(c) > 120 || !strings.Contains(c, "cut") {
		t.Error(c)
	}
}
