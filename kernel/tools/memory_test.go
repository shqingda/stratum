package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shqingda/stratum/kernel/perm"
	"github.com/shqingda/stratum/kernel/skills"
)

func TestMemoryToolAndLoader(t *testing.T) {
	root := t.TempDir()
	global := t.TempDir()
	t.Setenv("STRATUM_MEMORY_DIR", global)

	env := Env{Root: root}
	out, err := call(t, env, Memory, map[string]any{"action": "read"})
	if err != nil || out != "(empty)" {
		t.Fatalf("empty read = %q %v", out, err)
	}
	if _, err := call(t, env, Memory, map[string]any{"action": "append", "content": "Always run go vet."}); err != nil {
		t.Fatal(err)
	}
	if _, err := call(t, env, Memory, map[string]any{"action": "append", "content": "Keep functions small."}); err != nil {
		t.Fatal(err)
	}
	got, err := call(t, env, Memory, map[string]any{"action": "read"})
	if err != nil || !strings.Contains(got, "Always run go vet.\n\nKeep functions small.") {
		t.Fatalf("read after append = %q %v", got, err)
	}
	if _, err := call(t, env, Memory, map[string]any{"action": "replace", "scope": "global", "content": "Global rule"}); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(global, "MEMORY.md")); err != nil || string(b) != "Global rule\n" {
		t.Fatalf("global memory = %q %v", b, err)
	}

	req, err := Memory.Access(env, []byte(`{"action":"append","content":"hi"}`))
	if err != nil || req.Kind != perm.Write || perm.Decide(perm.Auto, req, true) != perm.Allow {
		t.Fatalf("project access = %+v %v", req, err)
	}
	reqG, err := Memory.Access(env, []byte(`{"action":"append","scope":"global","content":"hi"}`))
	if err != nil || reqG.Kind != perm.Write || perm.Decide(perm.Auto, reqG, true) != perm.AskUser {
		t.Fatalf("global access = %+v %v", reqG, err)
	}

	loader := skills.Loader{GlobalDir: global}
	cat := loader.Load(root)
	if !strings.Contains(cat.Prompt, "Always run go vet.") || !strings.Contains(cat.Prompt, "Global rule") {
		t.Fatalf("loader prompt missing memory:\n%s", cat.Prompt)
	}
}
