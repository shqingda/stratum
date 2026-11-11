package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"iter"
	"net/http"
	"strings"
)

// Auth returns the headers that authorize a request, refreshing tokens as
// needed; it runs before every call.
type Auth func(ctx context.Context) (http.Header, error)

// StaticHeader authorizes every request with the same header.
func StaticHeader(name, value string) Auth {
	h := http.Header{}
	h.Set(name, value)
	return func(context.Context) (http.Header, error) { return h, nil }
}

// Anthropic speaks the Messages API.
type Anthropic struct {
	BaseURL string // https://api.anthropic.com when empty
	Auth    Auth   // e.g. StaticHeader("x-api-key", key)
	HTTP    *http.Client
}

const anthropicName = "anthropic"

func (p *Anthropic) Stream(ctx context.Context, req Request) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		body := anthropicBody(req)
		header, err := p.Auth(ctx)
		if err != nil {
			yield(Event{}, err)
			return
		}
		header = header.Clone()
		header.Set("anthropic-version", "2023-06-01")
		base := p.BaseURL
		if base == "" {
			base = "https://api.anthropic.com"
		}
		resp, err := client{p.HTTP}.post(ctx, strings.TrimSuffix(base, "/")+"/v1/messages", header, body)
		if err != nil {
			yield(Event{}, err)
			return
		}
		defer resp.Body.Close()
		s := anthropicStream{yield: yield}
		if err := readSSE(resp.Body, s.event); err != nil {
			yield(Event{}, err)
		} else if !s.stopped && !s.quit {
			yield(Event{}, &Error{Message: "stream ended early", Retryable: true})
		}
	}
}

// adaptiveThinking reports whether model takes adaptive thinking and an
// effort, as Claude models from the 4.6 generation on do; older ones take a
// token budget.
func adaptiveThinking(model string) bool {
	m := strings.ToLower(model)
	for _, old := range []string{"claude-3", "-4-0", "-4-1", "-4-5", "sonnet-4-2", "opus-4-2"} {
		if strings.Contains(m, old) {
			return false
		}
	}
	return true
}

func anthropicBody(req Request) map[string]any {
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 32000
	}
	body := map[string]any{
		"model":      req.Model,
		"max_tokens": maxTokens,
		"stream":     true,
		"messages":   anthropicMessages(req.Messages),
	}
	cache := map[string]any{"type": "ephemeral"}
	if req.System != "" {
		body["system"] = []any{map[string]any{"type": "text", "text": req.System, "cache_control": cache}}
	}
	if len(req.Tools) > 0 {
		tools := make([]any, len(req.Tools))
		for i, t := range req.Tools {
			tool := map[string]any{"name": t.Name, "description": t.Description, "input_schema": toolSchema(t)}
			if i == len(req.Tools)-1 {
				tool["cache_control"] = cache
			}
			tools[i] = tool
		}
		body["tools"] = tools
	}
	if req.Effort != "" {
		if adaptiveThinking(req.Model) {
			body["thinking"] = map[string]any{"type": "adaptive", "display": "summarized"}
			body["output_config"] = map[string]any{"effort": req.Effort}
		} else {
			budget := map[string]int{"low": 2048, "medium": 8192, "high": 16384}[req.Effort]
			if budget == 0 {
				budget = 4096
			}
			if budget >= maxTokens {
				body["max_tokens"] = budget + 4096
			}
			body["thinking"] = map[string]any{"type": "enabled", "budget_tokens": budget}
		}
	}
	return body
}

func anthropicMessages(msgs []Message) []any {
	out := make([]any, 0, len(msgs))
	for _, m := range msgs {
		var blocks []map[string]any
		for _, p := range m.Parts {
			switch {
			case p.Text != "":
				blocks = append(blocks, map[string]any{"type": "text", "text": p.Text})
			case p.Image != nil:
				blocks = append(blocks, anthropicImage(*p.Image))
			case p.Thinking != nil:
				t := p.Thinking
				if t.Provider != anthropicName {
					continue // another provider's reasoning cannot be verified here
				}
				if t.Opaque != "" {
					blocks = append(blocks, map[string]any{"type": "redacted_thinking", "data": t.Opaque})
				} else if t.Signature != "" {
					blocks = append(blocks, map[string]any{"type": "thinking", "thinking": t.Text, "signature": t.Signature})
				}
			case p.ToolCall != nil:
				args := p.ToolCall.Args
				if len(args) == 0 {
					args = json.RawMessage("{}")
				}
				blocks = append(blocks, map[string]any{"type": "tool_use", "id": p.ToolCall.ID, "name": p.ToolCall.Name, "input": args})
			case p.ToolResult != nil:
				r := p.ToolResult
				content := []any{map[string]any{"type": "text", "text": nonEmpty(r.Content)}}
				for _, img := range r.Images {
					content = append(content, anthropicImage(img))
				}
				blocks = append(blocks, map[string]any{"type": "tool_result", "tool_use_id": r.CallID, "content": content, "is_error": r.IsError})
			}
		}
		if len(blocks) == 0 {
			continue
		}
		// Roles must alternate: a user's words and the tool results after
		// them go in one message.
		if n := len(out); n > 0 && out[n-1].(map[string]any)["role"] == string(m.Role) {
			prev := out[n-1].(map[string]any)
			prev["content"] = append(prev["content"].([]map[string]any), blocks...)
			continue
		}
		out = append(out, map[string]any{"role": string(m.Role), "content": blocks})
	}
	// Cache the conversation up to its last message; thinking blocks take
	// no cache mark.
	if n := len(out); n > 0 {
		blocks := out[n-1].(map[string]any)["content"].([]map[string]any)
		for i := len(blocks) - 1; i >= 0; i-- {
			if t := blocks[i]["type"]; t != "thinking" && t != "redacted_thinking" {
				blocks[i]["cache_control"] = map[string]any{"type": "ephemeral"}
				break
			}
		}
	}
	return out
}

func anthropicImage(img Image) map[string]any {
	return map[string]any{"type": "image", "source": map[string]any{
		"type": "base64", "media_type": img.MediaType, "data": base64.StdEncoding.EncodeToString(img.Data),
	}}
}

func nonEmpty(s string) string {
	if s == "" {
		return "(empty)"
	}
	return s
}

// anthropicStream turns Messages API events into Events.
type anthropicStream struct {
	yield   func(Event, error) bool
	usage   Usage
	blocks  map[int]*anthropicBlock
	stopped bool
	quit    bool
}

type anthropicBlock struct {
	kind      string // text, thinking, redacted_thinking, tool_use
	id, name  string
	text      strings.Builder
	signature string
	data      string
}

func (s *anthropicStream) emit(e Event) bool {
	if !s.yield(e, nil) {
		s.quit = true
		return false
	}
	return true
}

func (s *anthropicStream) event(ev sseEvent) (bool, error) {
	var msg struct {
		Type    string `json:"type"`
		Index   int    `json:"index"`
		Message struct {
			Usage anthropicUsage `json:"usage"`
		} `json:"message"`
		ContentBlock struct {
			Type string `json:"type"`
			ID   string `json:"id"`
			Name string `json:"name"`
			Data string `json:"data"`
		} `json:"content_block"`
		Delta struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			Thinking    string `json:"thinking"`
			Signature   string `json:"signature"`
			PartialJSON string `json:"partial_json"`
			StopReason  string `json:"stop_reason"`
		} `json:"delta"`
		Usage anthropicUsage `json:"usage"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(ev.Data, &msg); err != nil {
		return false, &Error{Message: "bad stream event: " + err.Error()}
	}
	if s.blocks == nil {
		s.blocks = map[int]*anthropicBlock{}
	}
	switch msg.Type {
	case "message_start":
		s.usage = msg.Message.Usage.usage(s.usage)
		return s.emit(Event{Type: UsageUpdate, Usage: s.usage}), nil
	case "content_block_start":
		b := msg.ContentBlock
		s.blocks[msg.Index] = &anthropicBlock{kind: b.Type, id: b.ID, name: b.Name, data: b.Data}
	case "content_block_delta":
		b := s.blocks[msg.Index]
		if b == nil {
			return true, nil
		}
		d := msg.Delta
		switch d.Type {
		case "text_delta":
			return s.emit(Event{Type: TextDelta, Delta: d.Text}), nil
		case "thinking_delta":
			b.text.WriteString(d.Thinking)
			return s.emit(Event{Type: ThinkingDelta, Delta: d.Thinking}), nil
		case "signature_delta":
			b.signature += d.Signature
		case "input_json_delta":
			b.text.WriteString(d.PartialJSON)
		}
	case "content_block_stop":
		b := s.blocks[msg.Index]
		delete(s.blocks, msg.Index)
		if b == nil {
			return true, nil
		}
		switch b.kind {
		case "thinking":
			return s.emit(Event{Type: ThinkingDone, Thinking: &Thinking{Text: b.text.String(), Signature: b.signature, Provider: anthropicName}}), nil
		case "redacted_thinking":
			return s.emit(Event{Type: ThinkingDone, Thinking: &Thinking{Opaque: b.data, Provider: anthropicName}}), nil
		case "tool_use":
			args := json.RawMessage(b.text.String())
			if len(args) == 0 {
				args = json.RawMessage("{}")
			}
			if !json.Valid(args) {
				return false, &Error{Message: "tool call " + b.name + " has invalid JSON arguments"}
			}
			return s.emit(Event{Type: ToolCallDone, ToolCall: &ToolCall{ID: b.id, Name: b.name, Args: args}}), nil
		}
	case "message_delta":
		s.usage = msg.Usage.usage(s.usage)
		if !s.emit(Event{Type: UsageUpdate, Usage: s.usage}) {
			return false, nil
		}
		if msg.Delta.StopReason != "" {
			s.stopped = true
			return s.emit(Event{Type: Done, Stop: anthropicStop(msg.Delta.StopReason)}), nil
		}
	case "message_stop":
		return false, nil
	case "error":
		return false, streamError(msg.Error.Type, msg.Error.Message)
	}
	return true, nil
}

type anthropicUsage struct {
	Input      *int `json:"input_tokens"`
	Output     *int `json:"output_tokens"`
	CacheRead  *int `json:"cache_read_input_tokens"`
	CacheWrite *int `json:"cache_creation_input_tokens"`
}

// usage updates u with the counts present: message_delta repeats some.
func (a anthropicUsage) usage(u Usage) Usage {
	if a.CacheRead != nil {
		u.CacheRead = *a.CacheRead
	}
	if a.CacheWrite != nil {
		u.CacheWrite = *a.CacheWrite
	}
	if a.Input != nil {
		u.Input = *a.Input + u.CacheRead + u.CacheWrite
	}
	if a.Output != nil {
		u.Output = *a.Output
	}
	return u
}

func anthropicStop(r string) StopReason {
	switch r {
	case "tool_use":
		return StopToolUse
	case "max_tokens", "model_context_window_exceeded":
		return StopMaxTokens
	case "refusal":
		return StopRefusal
	}
	return StopEnd
}
