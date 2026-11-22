package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shqingda/stratum/kernel/perm"
	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/proto"
)

func TestRules(t *testing.T) {
	exec := func(cmd string) perm.Request { return perm.Request{Tool: "shell", Kind: perm.Exec, Command: cmd} }
	r := ruleFor(exec("go test ./..."))
	if r.Prefix != "go test" {
		t.Fatalf("rule %+v", r)
	}
	for cmd, want := range map[string]bool{
		"go test -run X ./...":      true,
		"go test":                   true,
		"go testing":                false,
		"go vet ./...":              false,
		"go test ./... && rm -rf ~": false,
		"go test $(evil)":           false,
	} {
		if got := allows(r, exec(cmd)); got != want {
			t.Errorf("%q: %v", cmd, got)
		}
	}
	if allows(r, perm.Request{Tool: "shell", Kind: perm.Exec, Command: "go test", Unrestricted: true}) {
		t.Error("a rule in the sandbox let a command out of it")
	}
	if p := prefix("mkdir build"); p != "mkdir" {
		t.Errorf("prefix %q", p)
	}
	w := ruleFor(perm.Request{Tool: "edit_file", Kind: perm.Write})
	if !allows(w, perm.Request{Tool: "edit_file", Kind: perm.Write}) || allows(w, perm.Request{Tool: "write_file", Kind: perm.Write}) {
		t.Error("write rule")
	}
}

// approvals runs a session whose turns call the tools given, answering
// each approval with answer, and returns what the tools returned.
func approvals(t *testing.T, calls []llm.ToolCall, answer func(proto.Approval) proto.ApprovalAnswer) (*Manager, []proto.Approval, []llm.ToolResult) {
	t.Helper()
	var turns []llm.Turn
	for _, c := range calls {
		turns = append(turns, llm.Turn{Calls: []llm.ToolCall{c}})
	}
	script := &llm.Script{Turns: append(turns, llm.Turn{Text: "好"})}
	m, s := changesManager(t, script)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var asked []proto.Approval
	ended := make(chan struct{}, 1)
	defer m.Subscribe(func(e proto.Event) {
		switch e.Type {
		case proto.EventApprovalRequest:
			var a proto.Approval
			json.Unmarshal(e.Data, &a)
			asked = append(asked, a)
			ans := answer(a)
			ans.SessionID, ans.ApprovalID = s.ID, a.ID
			go m.Answer(ans)
		case proto.EventTurnEnd:
			ended <- struct{}{}
		}
	})()
	if _, err := m.Prompt(ctx, proto.Prompt{SessionID: s.ID, Text: "做"}, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-ctx.Done():
		t.Fatal("the turn did not end")
	}
	var results []llm.ToolResult
	last := script.Requests[len(script.Requests)-1]
	for _, msg := range last.Messages {
		for _, part := range msg.Parts {
			if part.ToolResult != nil {
				results = append(results, *part.ToolResult)
			}
		}
	}
	return m, asked, results
}

func shellCall(id, cmd string) llm.ToolCall {
	return llm.Call(id, "shell", map[string]any{"command": cmd})
}

// Allowed for the session, a command's like run unasked; another does not.
func TestAllowForSession(t *testing.T) {
	_, asked, _ := approvals(t, []llm.ToolCall{shellCall("a", "mkdir one"), shellCall("b", "mkdir two"), shellCall("c", "touch x")},
		func(a proto.Approval) proto.ApprovalAnswer { return proto.ApprovalAnswer{Allow: true, Always: true} })
	if len(asked) != 2 || asked[0].Rule == nil || asked[0].Rule.Prefix != "mkdir" || asked[1].Command != "touch x" {
		t.Fatalf("asked %+v", asked)
	}
}

// Allowed always, the rule goes into the settings.
func TestAllowForever(t *testing.T) {
	m, _, _ := approvals(t, []llm.ToolCall{shellCall("a", "mkdir one")},
		func(a proto.Approval) proto.ApprovalAnswer { return proto.ApprovalAnswer{Allow: true, Forever: true} })
	set, _ := m.Settings(context.Background())
	if len(set.Rules) != 1 || set.Rules[0].Prefix != "mkdir" {
		t.Fatalf("rules %+v", set.Rules)
	}
}

func TestDeclineWithWords(t *testing.T) {
	_, _, res := approvals(t, []llm.ToolCall{shellCall("a", "mkdir one")},
		func(a proto.Approval) proto.ApprovalAnswer {
			return proto.ApprovalAnswer{Text: "别建目录，直接写文件"}
		})
	if len(res) != 1 || !res[0].Skipped || !strings.Contains(res[0].Content, "They said: 别建目录，直接写文件") {
		t.Fatalf("results %+v", res)
	}
}

func TestChangedCommand(t *testing.T) {
	_, _, res := approvals(t, []llm.ToolCall{shellCall("a", "mkdir one")},
		func(a proto.Approval) proto.ApprovalAnswer {
			return proto.ApprovalAnswer{Allow: true, Command: "mkdir two"}
		})
	if len(res) != 1 || res[0].IsError || !strings.HasPrefix(res[0].Content, "The user changed the command before it ran. It ran:\nmkdir two") {
		t.Fatalf("results %+v", res)
	}
}

// A session's pin and mark stay with it, and keep its place by time.
func TestFlag(t *testing.T) {
	m, _, _ := approvals(t, nil, nil) // one turn that only answers
	ctx := context.Background()
	var s proto.Session
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		list, _ := m.List(ctx)
		if s = list[0]; s.State == "" && s.Mark != "" {
			break
		}
	}
	if s.Mark != "unread" {
		t.Fatalf("after a turn: %+v", s)
	}
	on, none := true, ""
	if _, err := m.Flag(ctx, proto.SessionFlag{SessionID: s.ID, Pinned: &on, Mark: &none}); err != nil {
		t.Fatal(err)
	}
	list, _ := m.List(ctx)
	if got := list[0]; !got.Pinned || got.Mark != "" {
		t.Fatalf("flagged %+v, was %+v", got, s)
	}
	bad := "starred"
	if _, err := m.Flag(ctx, proto.SessionFlag{SessionID: s.ID, Mark: &bad}); err == nil {
		t.Fatal("an unknown mark was kept")
	}
}
