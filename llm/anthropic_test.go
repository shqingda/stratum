package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// replay serves body as an event stream and records the request body.
func replay(t *testing.T, status int, body string) (*httptest.Server, *map[string]any) {
	t.Helper()
	got := map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &got); err != nil {
			t.Errorf("request body %s: %v", b, err)
		}
		got["_path"] = r.URL.Path
		got["_key"] = r.Header.Get("x-api-key")
		got["_bearer"] = r.Header.Get("Authorization")
		got["_goog"] = r.Header.Get("x-goog-api-key")
		got["_query"] = r.URL.RawQuery
		got["_version"] = r.Header.Get("anthropic-version")
		got["_hdr"] = r.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

const anthropicToolTurn = `event: message_start
data: {"type":"message_start","message":{"id":"m1","usage":{"input_tokens":12,"cache_read_input_tokens":100,"cache_creation_input_tokens":5,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Need the file."}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig=="}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}

event: ping
data: {"type":"ping"}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Let me "}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"look."}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: content_block_start
data: {"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"tu_1","name":"read_file","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}

event: content_block_delta
data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"\"go.mod\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":2}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":42}}

event: message_stop
data: {"type":"message_stop"}

`

func anthropicFor(url string) *Anthropic {
	return &Anthropic{BaseURL: url, Auth: StaticHeader("x-api-key", "k")}
}

func TestAnthropicToolTurn(t *testing.T) {
	srv, got := replay(t, 200, anthropicToolTurn)
	req := Request{
		Model:  "claude-sonnet-4-5",
		System: "Be brief.",
		Effort: "low",
		Tools:  []Tool{{Name: "read_file", Description: "Read a file", Schema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)}, {Name: "noop"}},
		Messages: []Message{
			Text(User, "what module?"),
			{Role: Assistant, Parts: []Part{{Thinking: &Thinking{Text: "other", Signature: "x", Provider: "openai"}}, {Text: "ok"}}},
			Text(User, "and?"),
			{Role: User, Parts: []Part{{ToolResult: &ToolResult{CallID: "tu_0"}}}},
		},
	}
	var thinking string
	r, err := Collect(anthropicFor(srv.URL).Stream(context.Background(), req), func(e Event) {
		if e.Type == ThinkingDelta {
			thinking += e.Delta
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if thinking != "Need the file." || r.Stop != StopToolUse {
		t.Errorf("thinking %q stop %q", thinking, r.Stop)
	}
	if u := r.Usage; u.Input != 117 || u.Output != 42 || u.CacheRead != 100 || u.CacheWrite != 5 {
		t.Errorf("usage %+v", u)
	}
	ps := r.Message.Parts
	if len(ps) != 3 || ps[0].Thinking == nil || ps[0].Thinking.Signature != "sig==" || ps[1].Text != "Let me look." || ps[2].ToolCall == nil {
		t.Fatalf("parts %+v", ps)
	}
	if c := ps[2].ToolCall; c.ID != "tu_1" || string(c.Args) != `{"path":"go.mod"}` {
		t.Errorf("call %+v", c)
	}

	g := *got
	if g["_path"] != "/v1/messages" || g["_key"] != "k" || g["_version"] == "" || g["stream"] != true {
		t.Errorf("request %v", g)
	}
	if th, _ := g["thinking"].(map[string]any); th["budget_tokens"] != 2048.0 {
		t.Errorf("an older model takes a budget: %v", g["thinking"])
	}
	tools := g["tools"].([]any)
	if s := tools[1].(map[string]any)["input_schema"]; s == nil {
		t.Error("a tool without a schema sent null")
	}
	msgs := g["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("roles not merged: %v", msgs)
	}
	if asst := msgs[1].(map[string]any)["content"].([]any); len(asst) != 1 {
		t.Errorf("foreign reasoning kept: %v", asst)
	}
	last := msgs[2].(map[string]any)["content"].([]any)
	if len(last) != 2 || last[1].(map[string]any)["cache_control"] == nil {
		t.Errorf("last message %v", last)
	}
}

func TestAnthropicAdaptiveThinking(t *testing.T) {
	srv, got := replay(t, 200, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	_, _ = Collect(anthropicFor(srv.URL).Stream(context.Background(), Request{Model: "claude-opus-5-5", Effort: "high", Messages: []Message{Text(User, "hi")}}), nil)
	g := *got
	if th, _ := g["thinking"].(map[string]any); th["type"] != "adaptive" {
		t.Errorf("thinking %v", g["thinking"])
	}
	if oc, _ := g["output_config"].(map[string]any); oc["effort"] != "high" {
		t.Errorf("output_config %v", g["output_config"])
	}
}

func TestAnthropicErrors(t *testing.T) {
	srv, _ := replay(t, 529, `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)
	_, err := Collect(anthropicFor(srv.URL).Stream(context.Background(), Request{Model: "m", Messages: []Message{Text(User, "hi")}}), nil)
	var e *Error
	if !errors.As(err, &e) || e.Status != 529 || !e.Retryable {
		t.Fatalf("err %#v", err)
	}

	// A stream cut short is a retryable error, not a reply.
	srv2, _ := replay(t, 200, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{}}}\n\n")
	if _, err := Collect(anthropicFor(srv2.URL).Stream(context.Background(), Request{Model: "m", Messages: []Message{Text(User, "hi")}}), nil); !Retryable(err) {
		t.Fatalf("err %v", err)
	}

	// An error event mid-stream.
	srv3, _ := replay(t, 200, "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"busy\"}}\n\n")
	if _, err := Collect(anthropicFor(srv3.URL).Stream(context.Background(), Request{Model: "m", Messages: []Message{Text(User, "hi")}}), nil); err == nil {
		t.Fatal("no error")
	}
}

func TestStopReadingEarly(t *testing.T) {
	srv, _ := replay(t, 200, anthropicToolTurn)
	n := 0
	for e, err := range anthropicFor(srv.URL).Stream(context.Background(), Request{Model: "m", Messages: []Message{Text(User, "hi")}}) {
		if err != nil {
			t.Fatal(err)
		}
		if e.Type == TextDelta {
			n++
			break
		}
	}
	if n != 1 {
		t.Fatal(n)
	}
}

func TestScript(t *testing.T) {
	s := &Script{Turns: []Turn{
		{Thinking: "plan", Text: "reading", Calls: []ToolCall{Call("c1", "read_file", map[string]string{"path": "a"})}},
		{Text: "done"},
	}}
	var p Provider = s
	r, err := Collect(p.Stream(context.Background(), Request{Messages: []Message{Text(User, "go")}}), nil)
	if err != nil || r.Stop != StopToolUse || len(r.Message.ToolCalls()) != 1 || r.Message.PlainText() != "reading" {
		t.Fatalf("%+v %v", r, err)
	}
	r, err = Collect(p.Stream(context.Background(), Request{}), nil)
	if err != nil || r.Stop != StopEnd || r.Message.PlainText() != "done" {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := Collect(p.Stream(context.Background(), Request{}), nil); err == nil {
		t.Fatal("ran past the script")
	}
	if len(s.Requests) != 3 {
		t.Fatal(len(s.Requests))
	}
}
