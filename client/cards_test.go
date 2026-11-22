package client

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/proto"
)

func TestToolArgsInReplayAndLive(t *testing.T) {
	call := llm.ToolCall{ID: "edit", Name: "edit_file", Args: json.RawMessage(`{"path":"a.go","old_text":"a\n","new_text":"b\n"}`)}
	message := llm.Message{Role: llm.Assistant, Parts: []llm.Part{{ToolCall: &call}}}
	for _, replay := range []bool{false, true} {
		t.Run(map[bool]string{false: "live", true: "replay"}[replay], func(t *testing.T) {
			var c *Client
			if replay {
				c, _ = connected(t, []proto.Event{event(ev(1, proto.EventMessage, message))})
			} else {
				c, _ = connected(t, []proto.Event{})
				c.Receive(ev(0, proto.EventToolStart, proto.ToolEvent{Call: call}))
				c.Receive(ev(1, proto.EventMessage, message))
			}
			c.Receive(ev(0, proto.EventToolEnd, proto.ToolEvent{Call: call, Result: &llm.ToolResult{CallID: call.ID, Content: "Edited a.go (1 replacement)"}}))
			if len(c.Entries) != 1 {
				t.Fatalf("entries = %+v", c.Entries)
			}
			e := c.Entries[0]
			if !bytes.Equal(e.Args, call.Args) || e.Detail != "a.go" || !e.Done || e.Output != "Edited a.go (1 replacement)" {
				t.Fatalf("tool entry = %+v, args = %s", e, e.Args)
			}
		})
	}
}

func TestToolKeepsMalformedArgs(t *testing.T) {
	c := New("/w")
	args := json.RawMessage(`{broken`)
	e := c.tool(llm.ToolCall{ID: "bad", Name: "edit_file", Args: args})
	if !bytes.Equal(e.Args, args) || e.Detail != "" {
		t.Fatalf("entry = %+v", e)
	}
}
