package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/shqingda/stratum/kernel/store"
	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/proto"
)

func TestHooksEditorAndPluginSystem(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	globalDir := t.TempDir()
	cwd := t.TempDir()
	m := New(st, func(string) (llm.Provider, error) { return &llm.Script{}, nil })
	m.Skills.GlobalDir = globalDir
	defer m.Close()

	// Save project hooks and read them back.
	hl, err := m.SaveHooks(ctx, proto.HooksSave{
		Cwd:     cwd,
		Project: true,
		Hooks: []proto.HookEntry{
			{Event: "PreToolUse", Matcher: "shell", Command: "echo pre"},
			{Event: "Stop", Command: "echo stop"},
		},
	})
	if err != nil {
		t.Fatalf("SaveHooks: %v", err)
	}
	if len(hl.Hooks) != 2 || !hl.Hooks[0].Editable || !hl.Hooks[0].Project {
		t.Fatalf("unexpected hooks list: %+v", hl)
	}

	// Build a local plugin package with manifest, skill, and hook.
	srcPlugin := filepath.Join(t.TempDir(), "my-pack")
	_ = os.MkdirAll(filepath.Join(srcPlugin, ".talos-plugin"), 0o755)
	_ = os.MkdirAll(filepath.Join(srcPlugin, "skills", "lint"), 0o755)
	_ = os.MkdirAll(filepath.Join(srcPlugin, "hooks"), 0o755)
	_ = os.WriteFile(filepath.Join(srcPlugin, ".talos-plugin", "plugin.json"), []byte(`{"name":"my-pack","version":"1.0.0","description":"Demo pack","mcpServers":{"pack-srv":{"command":"echo","args":["hi"]}}}`), 0o644)
	_ = os.WriteFile(filepath.Join(srcPlugin, "skills", "lint", "SKILL.md"), []byte("---\nname: lint\ndescription: lint skill\n---\nRun lint."), 0o644)
	_ = os.WriteFile(filepath.Join(srcPlugin, "hooks", "hooks.json"), []byte(`{"hooks":{"PostToolUse":[{"matcher":"write_file","hooks":[{"type":"command","command":"echo plugin-post"}]}]}}`), 0o644)

	pl, err := m.InstallPlugin(ctx, proto.PluginInstall{Cwd: cwd, Source: srcPlugin, Project: false})
	if err != nil {
		t.Fatalf("InstallPlugin: %v", err)
	}
	if len(pl.Plugins) != 1 || pl.Plugins[0].Name != "my-pack" || !pl.Plugins[0].Enabled || pl.Plugins[0].Skills != 1 || pl.Plugins[0].Hooks != 1 {
		t.Fatalf("unexpected plugin list: %+v", pl)
	}

	// Enabled plugin hook should be loaded by loadHooks and ListHooks.
	specs := m.loadHooks(cwd, "PostToolUse")
	if len(specs) != 1 || specs[0].command != "echo plugin-post" {
		t.Fatalf("expected plugin hook loaded, got %+v", specs)
	}
	if out := m.runPostToolHooks(ctx, cwd, "s1", "write_file", json.RawMessage(`{}`), "ok"); out != "plugin-post" {
		t.Fatalf("unexpected post hook output: %q", out)
	}
	// Enabled plugin skill and MCP server should be discovered automatically.
	cat := m.Skills.LoadAll(cwd)
	if _, ok := cat.Find("lint"); !ok {
		t.Fatalf("expected plugin skill 'lint' to be loaded, got %+v", cat.Skills)
	}
	if mcpEntries := m.pluginMCPEntries(cwd); len(mcpEntries) != 1 || mcpEntries[0].Name != "pack-srv" {
		t.Fatalf("expected plugin MCP server 'pack-srv', got %+v", mcpEntries)
	}

	// Disable plugin -> its hook should no longer load.
	pl2, err := m.TogglePlugin(ctx, proto.PluginToggle{Cwd: cwd, Name: "my-pack", Enabled: false})
	if err != nil || pl2.Plugins[0].Enabled {
		t.Fatalf("TogglePlugin: %+v %v", pl2, err)
	}
	if specs2 := m.loadHooks(cwd, "PostToolUse"); len(specs2) != 0 {
		t.Fatalf("disabled plugin hook still loaded: %+v", specs2)
	}

	// Remove plugin.
	pl3, err := m.RemovePlugin(ctx, proto.PluginRemove{Cwd: cwd, Name: "my-pack"})
	if err != nil || len(pl3.Plugins) != 0 {
		t.Fatalf("RemovePlugin: %+v %v", pl3, err)
	}
}
