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

func TestHooks(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	script := &llm.Script{Turns: []llm.Turn{
		{Calls: []llm.ToolCall{
			llm.Call("c1", "write_file", map[string]any{"path": "blocked.txt", "content": "nope"}),
			llm.Call("c2", "write_file", map[string]any{"path": "allowed.txt", "content": "ok"}),
		}},
		{Text: "done"},
	}}
	m, s := changesManager(t, script)
	ctx := context.Background()
	full := "full"
	if _, err := m.Update(ctx, proto.SessionUpdate{SessionID: s.ID, Mode: &full}); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(s.Cwd, ".stratum"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(s.Cwd, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Flat .stratum/hooks.json: PreToolUse blocks blocked.txt, Stop writes stop.txt.
	hooksJSON := `{
  "PreToolUse": [{"matcher": "write_file|edit_file", "command": "input=$(cat); case \"$input\" in *blocked.txt*) echo 'no blocked.txt' >&2; exit 2;; esac"}],
  "Stop": [{"command": "printf '%s %s' \"$STRATUM_SESSION_ID\" \"$TALOS_SESSION_ID\" > stop.txt"}]
}`
	if err := os.WriteFile(filepath.Join(s.Cwd, ".stratum", "hooks.json"), []byte(hooksJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	// Nested .claude/settings.json: PostToolUse marks post.txt and emits stdout note.
	claudeJSON := `{
  "hooks": {
    "PostToolUse": [{"matcher": "write_file", "hooks": [{"type": "command", "command": "cat > post.json; echo 'checked by hook'"}]}]
  }
}`
	if err := os.WriteFile(filepath.Join(s.Cwd, ".claude", "settings.json"), []byte(claudeJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	ended := make(chan struct{}, 2)
	defer m.Subscribe(func(e proto.Event) {
		if e.Type == proto.EventTurnEnd && e.SessionID == s.ID {
			ended <- struct{}{}
		}
	})()

	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := m.Prompt(runCtx, proto.Prompt{SessionID: s.ID, Text: "write both"}, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-runCtx.Done():
		t.Fatal("the turn did not finish")
	}

	if _, err := os.Stat(filepath.Join(s.Cwd, "blocked.txt")); !os.IsNotExist(err) {
		t.Fatalf("blocked.txt should not have been written, err=%v", err)
	}
	if b, err := os.ReadFile(filepath.Join(s.Cwd, "allowed.txt")); err != nil || string(b) != "ok" {
		t.Fatalf("allowed.txt = %q %v", b, err)
	}
	if b, err := os.ReadFile(filepath.Join(s.Cwd, "stop.txt")); err != nil || string(b) != s.ID+" "+s.ID {
		t.Fatalf("stop.txt = %q %v", b, err)
	}
	if b, err := os.ReadFile(filepath.Join(s.Cwd, "post.json")); err != nil || !strings.Contains(string(b), `"PostToolUse"`) {
		t.Fatalf("post.json = %q %v", b, err)
	}

	// Verify the model received both the PreToolUse block reason and PostToolUse note.
	if len(script.Requests) != 2 {
		t.Fatalf("%d requests", len(script.Requests))
	}
	msgs := script.Requests[1].Messages
	var toolResults []llm.ToolResult
	for _, msg := range msgs {
		for _, part := range msg.Parts {
			if part.ToolResult != nil {
				toolResults = append(toolResults, *part.ToolResult)
			}
		}
	}
	if len(toolResults) != 2 {
		t.Fatalf("tool results %+v", toolResults)
	}
	if !toolResults[0].IsError || !strings.Contains(toolResults[0].Content, "Blocked by PreToolUse hook: no blocked.txt") {
		t.Fatalf("first result = %+v", toolResults[0])
	}
	if toolResults[1].IsError || !strings.Contains(toolResults[1].Content, "[PostToolUse hook]\nchecked by hook") {
		t.Fatalf("second result = %+v", toolResults[1])
	}
}
