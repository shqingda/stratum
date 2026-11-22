package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/proto"
)

func TestParseGraph(t *testing.T) {
	bad := map[string]string{
		"cycle":   `{"tasks":[{"id":"a","description":"a","prompt":"p","after":["b"]},{"id":"b","description":"b","prompt":"p","after":["a"]}]}`,
		"unknown": `{"tasks":[{"id":"a","description":"a","prompt":"p","after":["z"]}]}`,
		"twice":   `{"tasks":[{"id":"a","description":"a","prompt":"p"},{"id":"a","description":"a","prompt":"p"}]}`,
		"empty":   `{"tasks":[]}`,
	}
	for name, args := range bad {
		if _, err := parseGraph(json.RawMessage(args)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestRunTasks(t *testing.T) {
	script := &llm.Script{Turns: []llm.Turn{
		{Calls: []llm.ToolCall{llm.Call("g", "run_tasks", map[string]any{"tasks": []map[string]any{
			{"id": "find", "description": "找调用方", "prompt": "Find callers of Add."},
			{"id": "fix", "description": "改调用方", "prompt": "Fix the callers.", "after": []string{"find"}},
		}})}},
		{Text: "callers: main.go"},
		{Text: "fixed main.go"},
		{Text: "都改好了。"},
	}}
	m, s := changesManager(t, script)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var states []string
	ended := make(chan struct{}, 1)
	defer m.Subscribe(func(e proto.Event) {
		switch e.Type {
		case proto.EventTaskState:
			var st proto.TaskState
			json.Unmarshal(e.Data, &st)
			states = append(states, st.Task+":"+st.State)
		case proto.EventTurnEnd:
			ended <- struct{}{}
		}
	})()
	if _, err := m.Prompt(ctx, proto.Prompt{SessionID: s.ID, Text: "改 Add 的调用方"}, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-ctx.Done():
		t.Fatal("the turn did not end")
	}
	if got := strings.Join(states, " "); got != "find:run find:done fix:run fix:done" {
		t.Fatalf("states %s", got)
	}
	if p := script.Requests[2].Messages[0].PlainText(); !strings.Contains(p, "Fix the callers.") || !strings.Contains(p, "callers: main.go") {
		t.Fatalf("the second task was not given the first's report: %q", p)
	}
	last := script.Requests[3].Messages
	out := last[len(last)-1].Parts[0].ToolResult.Content
	if !strings.Contains(out, "## find (done)\ncallers: main.go") || !strings.Contains(out, "## fix (done)\nfixed main.go") {
		t.Fatalf("result %q", out)
	}
}
