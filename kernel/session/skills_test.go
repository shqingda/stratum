package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shqingda/stratum/kernel/store"
	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/proto"
)

func TestSessionInstructionsSkillsAndRefresh(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	global, repo := filepath.Join(base, "global"), filepath.Join(base, "repo")
	cwd := filepath.Join(repo, "nested", "leaf")
	writeSkillFixture(t, filepath.Join(repo, ".git", "HEAD"), "ref: refs/heads/main\n")
	instructions := []struct{ path, text string }{
		{filepath.Join(global, "AGENTS.md"), "GLOBAL_POLICY"},
		{filepath.Join(repo, "AGENTS.md"), "ROOT_POLICY"},
		{filepath.Join(repo, "nested", "AGENTS.md"), "PARENT_POLICY"},
		{filepath.Join(cwd, "AGENTS.md"), "WORKSPACE_POLICY"},
	}
	for _, source := range instructions {
		writeSkillFixture(t, source.path, source.text+"\n")
	}
	globalSkill := filepath.Join(global, "skills", "installed")
	localSkill := filepath.Join(cwd, ".talos", "skills", "local")
	marker := filepath.Join(base, "must-not-execute")
	globalBody := "GLOBAL_SKILL_BODY_ONLY"
	localBody := "LOCAL_SKILL_BODY_ONLY\n$(touch " + marker + ")"
	writeSkillFixture(t, filepath.Join(globalSkill, "SKILL.md"), skillDocument("installed", "Global installed helper.", globalBody))
	writeSkillFixture(t, filepath.Join(localSkill, "SKILL.md"), skillDocument("local", "Local helper metadata.", localBody))
	script := &llm.Script{Turns: []llm.Turn{
		{Calls: []llm.ToolCall{
			llm.Call("global-first", "skill", map[string]string{"name": "installed"}),
			llm.Call("local-first", "skill", map[string]string{"name": "local"}),
		}},
		{Text: "Read both skills."},
		{Calls: []llm.ToolCall{llm.Call("local-updated", "skill", map[string]string{"name": "local"})}},
		{Text: "Read the updated skill."},
	}}
	m := skillsTestManager(t, global, map[string]*llm.Script{"script": script})
	s := skillsTestSession(t, m, cwd, "script")
	runSkillsTurn(t, m, s.ID)
	if len(script.Requests) != 2 {
		t.Fatalf("model requests = %d, want 2", len(script.Requests))
	}
	first := script.Requests[0]
	last := -1
	for _, source := range instructions {
		pathAt, textAt := strings.Index(first.System, source.path), strings.Index(first.System, source.text)
		if pathAt <= last || textAt <= pathAt || strings.Count(first.System, source.text) != 1 {
			t.Errorf("instruction source/order/repetition for %s is wrong:\n%s", source.path, first.System)
		}
		last = textAt
	}
	for _, text := range []string{"installed", "Global installed helper.", "local", "Local helper metadata."} {
		if !strings.Contains(first.System, text) {
			t.Errorf("skill metadata %q is missing from system prompt", text)
		}
	}
	for _, body := range []string{globalBody, localBody} {
		if strings.Contains(first.System, body) {
			t.Error("skill body was loaded into the system prompt")
		}
	}
	if script.Requests[1].System != first.System {
		t.Error("system instructions changed within one turn")
	}
	checkSkillTool(t, first)
	checkSkillResult(t, script.Requests[1], "global-first", "installed", globalBody, globalSkill)
	checkSkillResult(t, script.Requests[1], "local-first", "local", localBody, localSkill)

	writeSkillFixture(t, filepath.Join(cwd, "AGENTS.md"), "UPDATED_WORKSPACE_POLICY\n")
	updatedBody := "UPDATED_LOCAL_SKILL_BODY_ONLY"
	writeSkillFixture(t, filepath.Join(localSkill, "SKILL.md"), skillDocument("local", "Updated local helper metadata.", updatedBody))
	runSkillsTurn(t, m, s.ID)
	if len(script.Requests) != 4 {
		t.Fatalf("model requests after refresh = %d, want 4", len(script.Requests))
	}
	updated := script.Requests[2]
	if !strings.Contains(updated.System, "UPDATED_WORKSPACE_POLICY") || !strings.Contains(updated.System, "Updated local helper metadata.") {
		t.Error("next turn did not reload changed instructions and skill metadata")
	}
	if strings.Contains(updated.System, "\nWORKSPACE_POLICY") || strings.Contains(updated.System, "Local helper metadata.") || strings.Contains(updated.System, updatedBody) {
		t.Error("next turn retained old instructions/metadata or inlined the skill body")
	}
	checkSkillTool(t, updated)
	checkSkillResult(t, script.Requests[3], "local-updated", "local", updatedBody, localSkill)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("skill text was executed: %v", err)
	}
}

func TestSessionSkillCatalogIsolation(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	global := filepath.Join(base, "global")
	scripts := map[string]*llm.Script{}
	for _, account := range []string{"a", "b"} {
		scripts[account] = &llm.Script{Turns: []llm.Turn{
			{Calls: []llm.ToolCall{llm.Call(account+"-skill", "skill", map[string]string{"name": "private"})}},
			{Text: "Read the workspace skill."},
		}}
	}
	scripts["a"].Turns = append(scripts["a"].Turns,
		llm.Turn{Calls: []llm.ToolCall{llm.Call("a-again", "skill", map[string]string{"name": "private"})}},
		llm.Turn{Text: "Read the first workspace again."})
	m := skillsTestManager(t, global, scripts)
	sessions := map[string]proto.Session{}
	for _, account := range []string{"a", "b"} {
		cwd := filepath.Join(base, account)
		writeSkillFixture(t, filepath.Join(cwd, "AGENTS.md"), "POLICY_"+account)
		writeSkillFixture(t, filepath.Join(cwd, ".talos", "skills", "private", "SKILL.md"), skillDocument("private", "Only workspace "+account+" metadata.", "PRIVATE_BODY_"+account))
		sessions[account] = skillsTestSession(t, m, cwd, account)
	}
	for _, account := range []string{"a", "b", "a"} {
		script := scripts[account]
		start := len(script.Requests)
		runSkillsTurn(t, m, sessions[account].ID)
		if len(script.Requests) != start+2 {
			t.Fatalf("workspace %s made %d requests, want 2", account, len(script.Requests)-start)
		}
		req := script.Requests[start]
		other := "a"
		if account == "a" {
			other = "b"
		}
		if !strings.Contains(req.System, "POLICY_"+account) || !strings.Contains(req.System, "Only workspace "+account+" metadata.") || strings.Contains(req.System, "POLICY_"+other) || strings.Contains(req.System, "Only workspace "+other+" metadata.") || strings.Contains(req.System, "PRIVATE_BODY_") {
			t.Fatalf("workspace %s received another catalog or a skill body:\n%s", account, req.System)
		}
		checkSkillTool(t, req)
		callID := account + "-skill"
		if start > 0 {
			callID = "a-again"
		}
		dir := filepath.Join(sessions[account].Cwd, ".talos", "skills", "private")
		checkSkillResult(t, script.Requests[start+1], callID, "private", "PRIVATE_BODY_"+account, dir)
	}
}

func skillsTestManager(t *testing.T, global string, scripts map[string]*llm.Script) *Manager {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "talos.db"))
	if err != nil {
		t.Fatal(err)
	}
	m := New(st, func(account string) (llm.Provider, error) {
		p, ok := scripts[account]
		if !ok {
			return nil, fmt.Errorf("unknown test account %q", account)
		}
		return p, nil
	})
	m.Skills.GlobalDir = global
	t.Cleanup(func() { m.Close(); st.Close() })
	return m
}

func skillsTestSession(t *testing.T, m *Manager, cwd, account string) proto.Session {
	t.Helper()
	s, err := m.Create(context.Background(), proto.SessionCreate{Cwd: cwd, Account: account, Model: "offline", Mode: "ask"})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func runSkillsTurn(t *testing.T, m *Manager, sessionID string) {
	t.Helper()
	events := make(chan proto.Event, 4)
	unsub := m.Subscribe(func(e proto.Event) {
		if e.SessionID == sessionID && (e.Type == proto.EventApprovalRequest || e.Type == proto.EventTurnEnd) {
			events <- e
		}
	})
	defer unsub()
	res, err := m.Prompt(context.Background(), proto.Prompt{SessionID: sessionID, Text: "Read the installed skills."}, "")
	if err != nil || res.Queued {
		t.Fatalf("start skill turn: %+v, %v", res, err)
	}
	select {
	case event := <-events:
		if event.Type == proto.EventApprovalRequest {
			m.Cancel(sessionID)
			t.Fatal("reading an installed skill asked for approval")
		}
		var end proto.TurnEnd
		if err := json.Unmarshal(event.Data, &end); err != nil {
			t.Fatal(err)
		}
		if end.Error != "" || end.Stopped {
			t.Fatalf("skill turn failed: %+v", end)
		}
	case <-time.After(5 * time.Second):
		m.Cancel(sessionID)
		t.Fatal("skill turn did not finish")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		snap, err := m.Get(context.Background(), sessionID)
		if err != nil {
			t.Fatal(err)
		}
		if !snap.Running {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("skill turn remained active after turn.end")
		}
		time.Sleep(time.Millisecond)
	}
}

func checkSkillTool(t *testing.T, req llm.Request) {
	t.Helper()
	count := 0
	for _, tool := range req.Tools {
		if tool.Name == "skill" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("skill tool count = %d, want 1", count)
	}
}

func checkSkillResult(t *testing.T, req llm.Request, callID, name, body, dir string) {
	t.Helper()
	for _, message := range req.Messages {
		for _, part := range message.Parts {
			if result := part.ToolResult; result != nil && result.CallID == callID {
				if result.Name != "skill" || result.IsError || !strings.Contains(result.Content, body) || !strings.Contains(result.Content, dir) || strings.Contains(result.Content, "name: "+name) || strings.Contains(result.Content, "description:") {
					t.Fatalf("unexpected skill result: %+v", result)
				}
				return
			}
		}
	}
	t.Fatalf("skill result %s is missing from the next model request", callID)
}

func writeSkillFixture(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

func skillDocument(name, description, body string) string {
	return "---\nname: " + name + "\ndescription: " + description + "\n---\n" + body + "\n"
}

func TestSkillAndMCPManagement(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	global, cwd := filepath.Join(base, "global"), filepath.Join(base, "repo")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	m := skillsTestManager(t, global, map[string]*llm.Script{"script": {}})
	m.UseMCP(filepath.Join(global, "mcp.json"))
	ctx := context.Background()

	// Save & remove MCP server.
	list, err := m.MCPSave(ctx, proto.MCPSave{Cwd: cwd, Name: "custom", Project: true, Command: "echo-mcp", Args: []string{"stdio"}})
	if err != nil || len(list.Servers) != 1 || list.Servers[0].Name != "custom" || !list.Servers[0].Project {
		t.Fatalf("MCPSave = %+v %v", list, err)
	}
	list, err = m.MCPRemove(ctx, proto.MCPRemove{Cwd: cwd, Name: "custom", Project: true})
	if err != nil || len(list.Servers) != 0 {
		t.Fatalf("MCPRemove = %+v %v", list, err)
	}

	// Create a skill, install another from a local folder, toggle, and remove.
	list, err = m.SkillSave(ctx, proto.SkillSave{Cwd: cwd, Name: "review-go", Description: "Review Go code", Body: "Check tests.", Project: true})
	if err != nil || len(list.Skills) != 1 || list.Skills[0].Name != "review-go" || !list.Skills[0].Project {
		t.Fatalf("SkillSave = %+v %v", list, err)
	}
	srcDir := filepath.Join(base, "src-skill")
	writeSkillFixture(t, filepath.Join(srcDir, "SKILL.md"), skillDocument("imported", "Imported skill", "Imported body"))
	list, err = m.SkillSave(ctx, proto.SkillSave{Cwd: cwd, Source: srcDir, Project: false})
	if err != nil || len(list.Skills) != 2 {
		t.Fatalf("SkillSave install = %+v %v", list, err)
	}
	list, err = m.SkillToggle(ctx, proto.SkillToggle{Cwd: cwd, Name: "review-go", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Skills.Load(cwd).Find("review-go"); ok {
		t.Fatal("disabled skill stayed in Load")
	}
	if _, ok := m.Skills.LoadAll(cwd).Find("review-go"); !ok {
		t.Fatal("disabled skill disappeared from LoadAll")
	}
	list, err = m.SkillRemove(ctx, proto.SkillRemove{Cwd: cwd, Name: "review-go", Project: true})
	if err != nil || len(list.Skills) != 1 || list.Skills[0].Name != "imported" {
		t.Fatalf("SkillRemove = %+v %v", list, err)
	}
}
