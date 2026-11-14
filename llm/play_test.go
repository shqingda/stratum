package llm

import (
	"context"
	"strings"
	"testing"
)

func TestPlay(t *testing.T) {
	reply := func(msgs ...Message) Message {
		t.Helper()
		r, err := Collect(Play{}.Stream(context.Background(), Request{Messages: msgs}), nil)
		if err != nil {
			t.Fatal(err)
		}
		return r.Message
	}
	calls := func(m Message) (names []string) {
		for _, p := range m.Parts {
			if p.ToolCall != nil {
				names = append(names, p.ToolCall.Name)
			}
		}
		return names
	}
	result := func(id, content string) Message {
		return Message{Role: User, Parts: []Part{{ToolResult: &ToolResult{CallID: id, Content: content}}}}
	}

	// Fixing walks read, test, edit, test, report, one step a reply.
	say := Text(User, "修复 Add 测试")
	first := reply(say)
	if got := strings.Join(calls(first), " "); got != "read_file read_file search" {
		t.Fatalf("first step called %q", got)
	}
	second := reply(say, first, result("f1", "…"))
	if got := strings.Join(calls(second), " "); got != "shell" {
		t.Fatalf("second step called %q", got)
	}

	// A question's answer comes back in the reply.
	ask := Text(User, "帮我选一下")
	q := reply(ask)
	if got := strings.Join(calls(q), " "); got != "ask_user" {
		t.Fatalf("asking called %q", got)
	}
	if a := reply(ask, q, result("q1", "任意多个")).PlainText(); !strings.Contains(a, "任意多个") {
		t.Fatalf("answer not used: %q", a)
	}

	// A sub-agent reports, whatever its prompt says.
	r, _ := Collect(Play{}.Stream(context.Background(), Request{System: "You are a sub-agent", Messages: []Message{Text(User, "出错")}}), nil)
	if got := strings.Join(calls(r.Message), " "); got != "list_dir search" {
		t.Fatalf("sub-agent called %q", got)
	}

	// The error scene fails its second request.
	e := Text(User, "演示出错")
	first = reply(e)
	if _, err := Collect(Play{}.Stream(context.Background(), Request{Messages: []Message{e, first, result("e1", "")}}), nil); err == nil {
		t.Fatal("the error scene did not fail")
	}
}
