package app

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/proto"
)

// agentUpdates drains the commands the app sent and returns the agents
// that session updates asked for.
func agentUpdates(t *testing.T, a *App) []string {
	t.Helper()
	var out []string
	for {
		cmds, ok := next(a)
		if !ok {
			return out
		}
		for _, cmd := range cmds {
			if cmd.Method != proto.MethodSessionUpdate {
				continue
			}
			var u proto.SessionUpdate
			if err := json.Unmarshal(cmd.Params, &u); err != nil {
				t.Fatal(err)
			}
			if u.Agent != nil {
				out = append(out, *u.Agent)
			}
		}
	}
}

func TestTabCyclesPrimaryAgents(t *testing.T) {
	a := task8App()
	tt := task8Tester(a, false)
	if !tt.HasText("构建") {
		t.Fatal("the agent pill does not show build")
	}
	agentUpdates(t, a)
	tt.Key(0, ui.KeyTab)
	if got := agentUpdates(t, a); len(got) != 1 || got[0] != "plan" {
		t.Fatalf("Tab asked for %v", got)
	}
	a.C.Current.Agent = "plan"
	tt.Frame()
	if !tt.HasText("规划") {
		t.Fatal("the agent pill does not show plan")
	}
	tt.Key(0, ui.KeyTab)
	if got := agentUpdates(t, a); len(got) != 1 || got[0] != "build" {
		t.Fatalf("Tab from plan asked for %v", got)
	}
}

func TestMentionListsSubagents(t *testing.T) {
	a := task8App()
	a.C.MCP = &proto.MCPList{Agents: []proto.AgentInfo{
		{Name: "build", Mode: "primary", BuiltIn: true},
		{Name: "explore", Mode: "subagent", BuiltIn: true},
		{Name: "secret", Mode: "subagent", Hidden: true},
		{Name: "docs", Mode: "all", Description: "Writes docs"},
	}}
	a.C.MCPFor = a.C.Current.Cwd
	a.mention.query = ""
	var names []string
	for _, it := range a.mentionItems() {
		if it.kind == "agent" {
			names = append(names, it.name)
		}
	}
	if len(names) != 2 || names[0] != "explore" || names[1] != "docs" {
		t.Fatalf("agents in @ list: %v", names)
	}
	a.mention.query = "doc"
	for _, it := range a.mentionItems() {
		if it.kind == "agent" && it.name != "docs" {
			t.Fatalf("query doc listed %s", it.name)
		}
	}
}

func TestSettingsListsAgentsAndFormatters(t *testing.T) {
	a := task8App()
	a.C.MCP = &proto.MCPList{
		Agents: []proto.AgentInfo{
			{Name: "build", Mode: "primary", BuiltIn: true},
			{Name: "plan", Mode: "primary", BuiltIn: true},
			{Name: "explore", Mode: "subagent", BuiltIn: true},
			{Name: "docs", Mode: "subagent", Description: "Writes docs", Source: "/w/.stratum/agents/docs.md"},
		},
		Formatters: []proto.FormatterInfo{{Name: "gofmt", Command: []string{"gofmt", "-w"}, Extensions: []string{".go"}, BuiltIn: true}},
	}
	a.C.MCPFor = a.C.Current.Cwd
	a.settingsOpen, a.settingsPage = true, "mcp"
	a.mcpUI.tab = 1
	tt := task8Tester(a, false)
	for _, want := range []string{"主代理", "子代理", "构建", "规划", "docs", "Writes docs", "/w/.stratum/agents/docs.md"} {
		if !tt.HasText(want) {
			t.Errorf("agents tab lacks %q", want)
		}
	}
	a.mcpUI.tab = 4
	tt.Frame()
	if !tt.HasText("gofmt -w") || !tt.HasText(".go") {
		t.Error("formatters tab lacks gofmt")
	}
}
