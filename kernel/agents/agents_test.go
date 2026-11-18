package agents

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shqingda/talos/kernel/tools"
)

func TestLoadCustomMarkdownAndJSONAgents(t *testing.T) {
	globalDir := t.TempDir()
	cwd := t.TempDir()

	if err := os.MkdirAll(filepath.Join(cwd, ".talos", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	md := `---
description: Writes and maintains project documentation
mode: subagent
model: anthropic/claude-sonnet-4-20250514
max_steps: 10
permissions:
  bash: deny
  file_edit: allow
---

You are a documentation assistant.`
	if err := os.WriteFile(filepath.Join(cwd, ".talos", "agents", "docs-writer.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}

	a, ok := Find(globalDir, cwd, "docs-writer")
	if !ok {
		t.Fatal("expected custom docs-writer agent")
	}
	if a.Mode != "subagent" || a.MaxSteps != 10 || a.Prompt != "You are a documentation assistant." {
		t.Fatalf("unexpected agent: %+v", a)
	}
	filtered := FilterTools(tools.Default(), a)
	if filtered.Get("shell") != nil {
		t.Fatal("expected shell to be filtered out by bash: deny")
	}
	if filtered.Get("write_file") == nil {
		t.Fatal("expected write_file to remain allowed")
	}
}
