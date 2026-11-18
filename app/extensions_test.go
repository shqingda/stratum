package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/client"
	"github.com/shqingda/talos/proto"
)

func TestPreviewPaneAndAutomationsAndHooksPlugins(t *testing.T) {
	// 1. Preview pane
	a := working()
	a.out = make(chan []proto.Command, 16)
	a.rv.open, a.rv.tab = true, "preview"
	a.C.Preview = &proto.PreviewSnapshot{
		URL:     "http://localhost:3000",
		Title:   "My App",
		Status:  200,
		Console: []proto.PreviewConsoleEntry{{Level: "error", Text: "ReferenceError: foo is not defined"}},
	}
	tt := ui.NewTester(a.View, 1280, 800)
	tt.Frame()
	if !tt.HasText("My App") || !tt.HasText("HTTP 200") || !tt.HasText("ReferenceError: foo is not defined") {
		t.Fatalf("preview pane missing expected content")
	}

	// 2. Automations view
	a.automationsOpen = true
	a.C.Automations = &proto.AutomationList{Items: []proto.Automation{{
		ID:         "a1",
		Name:       "Daily Vet",
		Prompt:     "Run go vet ./...",
		Cwd:        "/tmp/repo",
		Schedule:   "@daily",
		Enabled:    true,
		LastStatus: "ok",
	}}}
	tt.Frame()
	if !tt.HasText("Daily Vet") || !tt.HasText("@daily") {
		t.Fatalf("automations view missing expected item")
	}
	a.automationsOpen = false

	// 3. Hooks & Plugins tabs in MCP/Extensions settings
	a.settingsOpen, a.settingsPage = true, "mcp"
	a.mcpUI.tab = 1
	a.C.Hooks = &proto.HooksList{Hooks: []proto.HookEntry{{
		Event: "PreToolUse", Matcher: "shell", Command: "echo check", Project: true, Editable: true,
	}}}
	tt.Frame()
	if !tt.HasText("PreToolUse") || !tt.HasText("echo check") {
		t.Fatalf("hooks tab missing expected hook")
	}

	a.mcpUI.tab = 2
	a.C.Plugins = &proto.PluginList{Plugins: []proto.PluginInfo{{
		Name: "demo-pack", Version: "1.0.0", Enabled: true, Skills: 2, Hooks: 1,
	}}}
	tt.Frame()
	if !tt.HasText("demo-pack") || !tt.HasText("v1.0.0") {
		t.Fatalf("plugins tab missing expected plugin")
	}
	a.settingsOpen = false

	// 4. Mid-session /worktree slash item
	a.C.Busy = false
	a.draft = "/"
	a.C.Workspace.Branch = "main"
	a.C.Entries = []*client.Entry{{Role: client.User, Text: "hi"}}
	items := a.slashItems(nil)
	foundWT := false
	for _, it := range items {
		if it.name == "worktree" {
			foundWT = true
			break
		}
	}
	if !foundWT {
		t.Fatalf("expected /worktree handoff item for started session")
	}
}
