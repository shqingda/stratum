package agents

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shqingda/stratum/kernel/perm"
	"github.com/shqingda/stratum/kernel/tools"
	"github.com/shqingda/stratum/kernel/workspace"
)

func TestLoadCustomMarkdownAndJSONAgents(t *testing.T) {
	globalDir := t.TempDir()
	cwd := t.TempDir()

	if err := os.MkdirAll(filepath.Join(cwd, ".stratum", "agents"), 0o755); err != nil {
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
	if err := os.WriteFile(filepath.Join(cwd, ".stratum", "agents", "docs-writer.md"), []byte(md), 0o644); err != nil {
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

func TestPermissionRules(t *testing.T) {
	cwd := t.TempDir()
	md := `---
description: Reviews changes
mode: subagent
temperature: 0.2
top_p: 0.9
permission:
  edit: deny
  bash:
    "*": ask
    "git status": allow
    "git log*": allow
    "git push": deny
  webfetch: allow
---
Review.`
	if err := os.MkdirAll(filepath.Join(cwd, ".stratum", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".stratum", "agents", "review.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	a, ok := Find(t.TempDir(), cwd, "review")
	if !ok {
		t.Fatal("review agent missing")
	}
	if a.Temperature == nil || *a.Temperature != 0.2 || a.TopP == nil || *a.TopP != 0.9 {
		t.Fatalf("sampling: %+v", a)
	}
	if a.Permissions["edit"] != "deny" || a.Permissions["webfetch"] != "allow" || len(a.Bash) != 4 {
		t.Fatalf("permission: %+v %+v", a.Permissions, a.Bash)
	}
	inside := []workspace.Path{{Abs: filepath.Join(cwd, "a.go"), Inside: true, Rel: "a.go"}}
	for _, tt := range []struct {
		cmd  string
		base perm.Decision
		want perm.Decision
	}{
		{"git status", perm.AskUser, perm.Allow},
		{"git status --short", perm.AskUser, perm.Allow},
		{"git log --oneline", perm.AskUser, perm.Allow},
		{"git push origin main", perm.Allow, perm.Deny},
		{"git status && git push", perm.Allow, perm.Deny},
		{"git status && rm -rf x", perm.Allow, perm.AskUser},
		{"git status > out.txt", perm.AskUser, perm.AskUser},
		{"go test ./...", perm.Allow, perm.AskUser},
	} {
		req := perm.Request{Tool: "shell", Kind: perm.Exec, Command: tt.cmd}
		if got := a.Decide(req, tt.base, true); got != tt.want {
			t.Errorf("%q from %s: got %s, want %s", tt.cmd, tt.base, got, tt.want)
		}
	}
	if got := a.Decide(perm.Request{Tool: "shell", Kind: perm.Exec, Command: "git status"}, perm.AskUser, false); got != perm.AskUser {
		t.Errorf("allow lifted a command with no sandbox: %s", got)
	}
	if got := a.Decide(perm.Request{Tool: "edit_file", Kind: perm.Write, Paths: inside}, perm.Allow, true); got != perm.Deny {
		t.Errorf("edit: %s", got)
	}
	outside := []workspace.Path{{Abs: "/etc/hosts"}}
	if got := a.Decide(perm.Request{Tool: "web_fetch", Kind: perm.Net, Paths: outside}, perm.AskUser, true); got != perm.AskUser {
		t.Errorf("allow lifted a path outside: %s", got)
	}
	if got := a.Decide(perm.Request{Tool: "web_fetch", Kind: perm.Net}, perm.AskUser, true); got != perm.Allow {
		t.Errorf("webfetch: %s", got)
	}
}

func TestOpenCodeJSONC(t *testing.T) {
	cwd := t.TempDir()
	cfg := `{
  // OpenCode names the map "agent"
  "agent": {
    "triage": {
      "description": "Sorts issues", /* short */
      "mode": "primary",
      "model": "openai/gpt-6",
      "top_p": 0.5,
      "permission": {"bash": {"gh issue": "allow", "*": "ask"}, "edit": "ask"},
    },
  },
}`
	if err := os.WriteFile(filepath.Join(cwd, "opencode.jsonc"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	a, ok := Find(t.TempDir(), cwd, "triage")
	if !ok || !a.Primary() || a.Sub() || a.Model != "openai/gpt-6" || a.TopP == nil || *a.TopP != 0.5 {
		t.Fatalf("triage: %+v %v", a, ok)
	}
	if a.Bash["gh issue"] != "allow" || a.Permissions["edit"] != "ask" {
		t.Fatalf("permission: %+v %+v", a.Permissions, a.Bash)
	}
}

func TestPluginAgentsYieldToTheUsers(t *testing.T) {
	cwd, plugin := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(plugin, "lint.md"), []byte("---\ndescription: From the plugin\n---\nLint."), 0o644); err != nil {
		t.Fatal(err)
	}
	a, ok := Find(t.TempDir(), cwd, "lint", plugin)
	if !ok || a.Description != "From the plugin" {
		t.Fatalf("plugin agent: %+v %v", a, ok)
	}
	if err := os.MkdirAll(filepath.Join(cwd, ".stratum", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".stratum", "agents", "lint.md"), []byte("---\ndescription: The folder's own\n---\nLint."), 0o644); err != nil {
		t.Fatal(err)
	}
	if a, _ := Find(t.TempDir(), cwd, "lint", plugin); a.Description != "The folder's own" {
		t.Fatalf("folder agent should win: %+v", a)
	}
}
