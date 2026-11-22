package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/proto"
)

// turn sends text to s and waits for the turn to end.
func agentTurn(t *testing.T, ctx context.Context, m *Manager, id, text string) {
	t.Helper()
	ended := make(chan struct{}, 4)
	defer m.Subscribe(func(e proto.Event) {
		if e.Type == proto.EventTurnEnd && e.SessionID == id {
			ended <- struct{}{}
		}
	})()
	if _, err := m.Prompt(ctx, proto.Prompt{SessionID: id, Text: text}, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-ctx.Done():
		t.Fatal("the turn did not finish")
	}
}

func toolNames(req llm.Request) []string {
	var names []string
	for _, tool := range req.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func TestPrimaryAgentLeadsTheTurn(t *testing.T) {
	script := &llm.Script{Turns: []llm.Turn{{Text: "built"}, {Text: "planned"}}}
	m, s := changesManager(t, script)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := m.Update(ctx, proto.SessionUpdate{SessionID: s.ID, Agent: ptr("nobody")}); err == nil {
		t.Fatal("an unknown agent was accepted")
	}
	if _, err := m.Update(ctx, proto.SessionUpdate{SessionID: s.ID, Agent: ptr("explore")}); err == nil {
		t.Fatal("a subagent was accepted as primary")
	}
	agentTurn(t, ctx, m, s.ID, "go")
	got, err := m.Update(ctx, proto.SessionUpdate{SessionID: s.ID, Agent: ptr("plan")})
	if err != nil || got.Agent != "plan" {
		t.Fatalf("plan: %+v %v", got, err)
	}
	agentTurn(t, ctx, m, s.ID, "think")
	if len(script.Requests) != 2 {
		t.Fatalf("%d requests", len(script.Requests))
	}
	build, plan := toolNames(script.Requests[0]), toolNames(script.Requests[1])
	if !contains(build, "write_file") || contains(plan, "write_file") || contains(plan, "edit_file") || contains(plan, "shell") {
		t.Fatalf("build %v\nplan %v", build, plan)
	}
	if !strings.Contains(script.Requests[1].System, "You are in Plan mode") {
		t.Fatal("plan prompt missing")
	}
	if back, err := m.Update(ctx, proto.SessionUpdate{SessionID: s.ID, Agent: ptr("build")}); err != nil || back.Agent != "" {
		t.Fatalf("build: %+v %v", back, err)
	}
}

func TestCustomPrimaryAgentSampling(t *testing.T) {
	script := &llm.Script{Turns: []llm.Turn{{Text: "ok"}}}
	m, s := changesManager(t, script)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dir := filepath.Join(s.Cwd, ".stratum", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	md := "---\ndescription: Careful\nmode: primary\ntemperature: 0.1\ntop_p: 0.8\n---\nBe careful."
	if err := os.WriteFile(filepath.Join(dir, "careful.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Update(ctx, proto.SessionUpdate{SessionID: s.ID, Agent: ptr("careful")}); err != nil {
		t.Fatal(err)
	}
	agentTurn(t, ctx, m, s.ID, "go")
	req := script.Requests[0]
	if req.Temperature == nil || *req.Temperature != 0.1 || req.TopP == nil || *req.TopP != 0.8 || !strings.Contains(req.System, "Be careful.") {
		t.Fatalf("request: %v %v %q", req.Temperature, req.TopP, req.System)
	}
}

func TestAtAgentRoutesToTheSubagent(t *testing.T) {
	script := &llm.Script{Turns: []llm.Turn{{Text: "ok"}}}
	m, s := changesManager(t, script)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	agentTurn(t, ctx, m, s.ID, "@explore where is the router?")
	var text string
	for _, p := range script.Requests[0].Messages[len(script.Requests[0].Messages)-1].Parts {
		text += p.Text
	}
	if !strings.Contains(text, `agent "explore"`) {
		t.Fatalf("no routing: %q", text)
	}
}

func TestPluginCommandsAndAgents(t *testing.T) {
	script := &llm.Script{}
	m, s := changesManager(t, script)
	plugin := filepath.Join(s.Cwd, ".stratum", "plugins", "pack")
	for path, body := range map[string]string{
		filepath.Join(plugin, ".stratum-plugin", "plugin.json"): `{"name": "pack", "commands": "./cmds"}`,
		filepath.Join(plugin, "cmds", "ship.md"):                "Ship it",
		filepath.Join(plugin, "agents", "lint.md"):              "---\ndescription: Lints\nmode: subagent\n---\nLint.",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	list, err := m.MCPList(context.Background(), proto.MCPQuery{Cwd: s.Cwd})
	if err != nil {
		t.Fatal(err)
	}
	var cmds, names []string
	for _, c := range list.Commands {
		cmds = append(cmds, c.Name)
	}
	for _, a := range list.Agents {
		names = append(names, a.Name)
	}
	if !contains(cmds, "ship") || !contains(names, "lint") || !contains(names, "plan") {
		t.Fatalf("commands %v agents %v", cmds, names)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func ptr[T any](v T) *T { return &v }
