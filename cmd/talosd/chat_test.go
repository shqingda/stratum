package main

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"talos/llm"
)

func TestTurnRunsTools(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/go.mod", []byte("module demo\n"), 0o644)
	root, _ := os.OpenRoot(dir)
	defer root.Close()
	s := &llm.Script{Turns: []llm.Turn{
		{Text: "Looking.", Calls: []llm.ToolCall{
			llm.Call("c1", "read_file", map[string]string{"path": "go.mod"}),
			llm.Call("c2", "read_file", map[string]string{"path": "../etc/passwd"}),
		}},
		{Text: "The module is demo."},
	}}
	c := &conversation{p: s, root: root, out: io.Discard, req: llm.Request{Tools: tools}}
	if err := c.turn(context.Background(), "which module?"); err != nil {
		t.Fatal(err)
	}
	if len(c.req.Messages) != 4 {
		t.Fatalf("%d messages", len(c.req.Messages))
	}
	res := c.req.Messages[2].Parts
	if res[0].ToolResult.Content != "module demo\n" || !res[1].ToolResult.IsError {
		t.Errorf("results %+v %+v", res[0].ToolResult, res[1].ToolResult)
	}
	if !strings.Contains(c.req.Messages[3].PlainText(), "demo") {
		t.Error(c.req.Messages[3])
	}

	// A failed request drops the whole turn.
	if err := c.turn(context.Background(), "again"); err == nil || len(c.req.Messages) != 4 {
		t.Fatalf("%v, %d messages", err, len(c.req.Messages))
	}
}
