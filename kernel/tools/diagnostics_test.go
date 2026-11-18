package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiagnosticsGoWorkspace(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/diag\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Printf(\"%d\", \"bad\") }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	env := Env{Root: dir}
	if _, err := Diagnostics.Access(env, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	out, err := Diagnostics.Run(context.Background(), env, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Printf") {
		t.Fatalf("expected vet diagnostic for Printf, got:\n%s", out)
	}

	// Fix main.go and verify clean output.
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"ok\") }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	out, err = Diagnostics.Run(context.Background(), env, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if out != "No diagnostics." {
		t.Fatalf("expected No diagnostics., got: %q", out)
	}
}
