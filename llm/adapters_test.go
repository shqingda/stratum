package llm

import (
	"context"
	"encoding/json"
	"testing"
)

func sseLines(events ...string) string {
	s := ""
	for _, e := range events {
		s += "data: " + e + "\n\n"
	}
	return s
}

var toolsReq = Request{
	Model:  "m",
	System: "Be brief.",
	Effort: "high",
	Tools:  []Tool{{Name: "read_file", Description: "Read", Schema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)}},
	Messages: []Message{
		Text(User, "what module?"),
		{Role: Assistant, Parts: []Part{{Text: "Looking."}, {ToolCall: &ToolCall{ID: "c0", Name: "read_file", Args: json.RawMessage(`{"path":"go.mod"}`)}}}},
		{Role: User, Parts: []Part{{ToolResult: &ToolResult{CallID: "c0", Name: "read_file", Content: "module talos"}}}},
	},
}

func TestOpenAIResponses(t *testing.T) {
	srv, got := replay(t, 200, sseLines(
		`{"type":"response.created"}`,
		`{"type":"response.reasoning_summary_text.delta","delta":"Think."}`,
		`{"type":"response.output_item.done","item":{"type":"reasoning","summary":[{"text":"Think."}],"encrypted_content":"enc"}}`,
		`{"type":"response.output_text.delta","delta":"It is "}`,
		`{"type":"response.output_text.delta","delta":"talos."}`,
		`{"type":"response.output_item.done","item":{"type":"function_call","call_id":"c1","name":"read_file","arguments":"{\"path\":\"README.md\"}"}}`,
		`{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":50,"output_tokens":9,"input_tokens_details":{"cached_tokens":40},"output_tokens_details":{"reasoning_tokens":3}}}}`,
	))
	p := &OpenAI{BaseURL: srv.URL, Auth: StaticHeader("Authorization", "Bearer k")}
	r, err := Collect(p.Stream(context.Background(), toolsReq), nil)
	if err != nil {
		t.Fatal(err)
	}
	ps := r.Message.Parts
	if r.Stop != StopToolUse || len(ps) != 3 || ps[0].Thinking.Opaque != "enc" || ps[1].Text != "It is talos." || ps[2].ToolCall.ID != "c1" {
		t.Fatalf("%+v", r)
	}
	if r.Usage != (Usage{Input: 50, Output: 9, CacheRead: 40, Reasoning: 3}) {
		t.Errorf("usage %+v", r.Usage)
	}
	g := *got
	if g["_path"] != "/responses" || g["_bearer"] != "Bearer k" || g["store"] != false || g["instructions"] != "Be brief." {
		t.Errorf("request %v", g)
	}
	if re, _ := g["reasoning"].(map[string]any); re["effort"] != "high" {
		t.Errorf("reasoning %v", g["reasoning"])
	}
	in := g["input"].([]any)
	// user text, assistant text, the call, its output
	if len(in) != 4 || in[2].(map[string]any)["type"] != "function_call" || in[3].(map[string]any)["output"] != "module talos" {
		t.Errorf("input %v", in)
	}

	// Reasoning goes back on the next turn only to OpenAI.
	next := toolsReq
	next.Messages = append(append([]Message{}, toolsReq.Messages...), r.Message)
	if items := responsesInput(next.Messages); items[4].(map[string]any)["encrypted_content"] != "enc" {
		t.Errorf("reasoning not sent back: %v", items[4])
	}
}

func TestOpenAIResponsesFailed(t *testing.T) {
	srv, _ := replay(t, 200, sseLines(`{"type":"response.failed","response":{"error":{"code":"rate_limit_exceeded","message":"slow down"}}}`))
	p := &OpenAI{BaseURL: srv.URL, Auth: StaticHeader("Authorization", "Bearer k")}
	if _, err := Collect(p.Stream(context.Background(), toolsReq), nil); err == nil {
		t.Fatal("no error")
	}
}

func TestChatCompletions(t *testing.T) {
	srv, got := replay(t, 200, sseLines(
		`{"choices":[{"delta":{"reasoning_content":"Hmm."}}]}`,
		`{"choices":[{"delta":{"content":"Reading."}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"read_file","arguments":"{\"pa"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":\"a\"}"}}]},"finish_reason":"tool_calls"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":20,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":10}}}`,
		`[DONE]`,
	))
	p := &Chat{BaseURL: srv.URL, Auth: StaticHeader("Authorization", "Bearer k")}
	r, err := Collect(p.Stream(context.Background(), toolsReq), nil)
	if err != nil {
		t.Fatal(err)
	}
	ps := r.Message.Parts
	if r.Stop != StopToolUse || len(ps) != 3 || ps[0].Text != "Reading." || ps[1].Thinking.Text != "Hmm." || string(ps[2].ToolCall.Args) != `{"path":"a"}` {
		t.Fatalf("%+v", ps)
	}
	if r.Usage.Input != 20 || r.Usage.CacheRead != 10 {
		t.Errorf("usage %+v", r.Usage)
	}
	g := *got
	msgs := g["messages"].([]any)
	// system, user, assistant with tool_calls, tool
	if g["_path"] != "/chat/completions" || len(msgs) != 4 || msgs[3].(map[string]any)["role"] != "tool" {
		t.Errorf("messages %v", msgs)
	}
	if msgs[2].(map[string]any)["tool_calls"] == nil {
		t.Errorf("assistant %v", msgs[2])
	}
}

func TestChatCutShort(t *testing.T) {
	srv, _ := replay(t, 200, sseLines(`{"choices":[{"delta":{"content":"Hi"}}]}`))
	p := &Chat{BaseURL: srv.URL}
	if _, err := Collect(p.Stream(context.Background(), toolsReq), nil); !Retryable(err) {
		t.Fatalf("err %v", err)
	}
}

func TestGemini(t *testing.T) {
	srv, got := replay(t, 200, sseLines(
		`{"candidates":[{"content":{"parts":[{"text":"Plan.","thought":true}]}}]}`,
		`{"candidates":[{"content":{"parts":[{"text":"Reading."}]}}]}`,
		`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"read_file","args":{"path":"a"}},"thoughtSignature":"ts"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":30,"candidatesTokenCount":6,"thoughtsTokenCount":4}}`,
	))
	p := &Gemini{BaseURL: srv.URL, Auth: StaticHeader("x-goog-api-key", "k")}
	req := toolsReq
	req.Model = "gemini-3-pro"
	r, err := Collect(p.Stream(context.Background(), req), nil)
	if err != nil {
		t.Fatal(err)
	}
	ps := r.Message.Parts
	if r.Stop != StopToolUse || len(ps) != 3 || ps[0].Thinking.Text != "Plan." || ps[1].Text != "Reading." || ps[2].ToolCall.Signature != "ts" {
		t.Fatalf("%+v", ps)
	}
	if r.Usage.Output != 10 || r.Usage.Reasoning != 4 {
		t.Errorf("usage %+v", r.Usage)
	}
	g := *got
	if g["_path"] != "/v1beta/models/gemini-3-pro:streamGenerateContent" || g["_query"] != "alt=sse" || g["_goog"] != "k" {
		t.Errorf("request %v", g)
	}
	contents := g["contents"].([]any)
	if len(contents) != 3 || contents[1].(map[string]any)["role"] != "model" {
		t.Errorf("contents %v", contents)
	}
	resp := contents[2].(map[string]any)["parts"].([]any)[0].(map[string]any)["functionResponse"].(map[string]any)
	if resp["name"] != "read_file" {
		t.Errorf("function response %v", resp)
	}
	gen := g["generationConfig"].(map[string]any)["thinkingConfig"].(map[string]any)
	if gen["thinkingLevel"] != "high" {
		t.Errorf("thinking %v", gen)
	}
}
