package llm

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"iter"
	"net/http"
	"strings"
)

// OpenAI speaks the Responses API.
type OpenAI struct {
	BaseURL string // https://api.openai.com/v1 when empty
	Auth    Auth   // e.g. StaticHeader("Authorization", "Bearer "+key)
	HTTP    *http.Client
	// Codex sends requests to the ChatGPT backend that a ChatGPT sign-in
	// pays for (BaseURL https://chatgpt.com/backend-api when empty). Auth
	// must set the bearer token and chatgpt-account-id.
	Codex         bool
	Provider      string            // catalog provider id for per-model metadata
	Headers       map[string]string // extra static headers
	SessionHeader string            // e.g. "x-opencode-session"
}

const openaiName = "openai"

// Originator names Talos to the ChatGPT backend.
const Originator = "talos"

func (p *OpenAI) Stream(ctx context.Context, req Request) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		header, err := p.Auth(ctx)
		if err != nil {
			yield(Event{}, err)
			return
		}
		if header == nil {
			header = http.Header{}
		} else {
			header = header.Clone()
		}
		for k, v := range p.Headers {
			header.Set(k, v)
		}
		if p.Provider != "" {
			if m, ok := FindModel(p.Provider, req.Model); ok {
				for k, v := range m.Headers {
					header.Set(k, v)
				}
				if !m.Reasoning {
					req.Effort = ""
				} else if req.Effort != "" && len(m.ThinkingLevelMap) > 0 {
					req.Effort = ResolveEffort(req.Effort, m.ThinkingLevelMap)
				}
			}
		}
		if p.SessionHeader != "" && req.SessionID != "" {
			header.Set(p.SessionHeader, req.SessionID)
		}
		base, path, body := p.BaseURL, "/responses", responsesBody(req)
		if p.Codex {
			if base == "" {
				base = "https://chatgpt.com/backend-api"
			}
			path = "/codex/responses"
			codexBody(body, req)
			header.Set("OpenAI-Beta", "responses=experimental")
			header.Set("originator", Originator)
			if req.SessionID != "" {
				header.Set("session_id", req.SessionID)
			}
		} else if base == "" {
			base = "https://api.openai.com/v1"
		}
		resp, err := client{p.HTTP}.post(ctx, strings.TrimSuffix(base, "/")+path, header, body)
		if err != nil {
			yield(Event{}, err)
			return
		}
		defer resp.Body.Close()
		s := responsesStream{yield: yield}
		if err := readSSE(resp.Body, s.event); err != nil {
			yield(Event{}, err)
		} else if !s.stopped && !s.quit {
			yield(Event{}, &Error{Message: "stream ended early", Retryable: true})
		}
	}
}

func dataURL(img Image) string {
	return "data:" + img.MediaType + ";base64," + base64.StdEncoding.EncodeToString(img.Data)
}

func responsesBody(req Request) map[string]any {
	body := map[string]any{
		"model":  req.Model,
		"input":  responsesInput(req.Messages),
		"stream": true,
		"store":  false,
	}
	if req.System != "" {
		body["instructions"] = req.System
	}
	if req.MaxTokens > 0 {
		body["max_output_tokens"] = req.MaxTokens
	}
	if len(req.Tools) > 0 {
		tools := make([]any, len(req.Tools))
		for i, t := range req.Tools {
			tools[i] = map[string]any{"type": "function", "name": t.Name, "description": t.Description, "parameters": toolSchema(t), "strict": false}
		}
		body["tools"] = tools
	}
	if req.Effort != "" {
		body["reasoning"] = map[string]any{"effort": req.Effort, "summary": "auto"}
		body["include"] = []string{"reasoning.encrypted_content"}
	}
	return body
}

// codexBody adapts a Responses body to the ChatGPT backend, which requires
// instructions and keys its cache by session.
func codexBody(body map[string]any, req Request) {
	if req.System == "" {
		body["instructions"] = "You are a helpful assistant."
	}
	body["text"] = map[string]any{"verbosity": "low"}
	body["include"] = []string{"reasoning.encrypted_content"}
	body["tool_choice"] = "auto"
	body["parallel_tool_calls"] = true
	if req.SessionID != "" {
		body["prompt_cache_key"] = req.SessionID
	}
}

func responsesInput(msgs []Message) []any {
	var items []any
	for _, m := range msgs {
		var content []any // the message being built
		flush := func() {
			if len(content) == 0 {
				return
			}
			if m.Role == Assistant {
				items = append(items, map[string]any{"type": "message", "role": "assistant", "content": content})
			} else {
				items = append(items, map[string]any{"role": "user", "content": content})
			}
			content = nil
		}
		var images []any // images of tool results, sent after them
		for _, p := range m.Parts {
			switch {
			case p.Text != "":
				kind := "input_text"
				if m.Role == Assistant {
					kind = "output_text"
				}
				content = append(content, map[string]any{"type": kind, "text": p.Text})
			case p.Image != nil:
				content = append(content, map[string]any{"type": "input_image", "image_url": dataURL(*p.Image)})
			case p.Thinking != nil:
				t := p.Thinking
				if t.Provider != openaiName || t.Opaque == "" {
					continue
				}
				flush()
				summary := []any{}
				if t.Text != "" {
					summary = append(summary, map[string]any{"type": "summary_text", "text": t.Text})
				}
				items = append(items, map[string]any{"type": "reasoning", "summary": summary, "encrypted_content": t.Opaque})
			case p.ToolCall != nil:
				flush()
				args := string(p.ToolCall.Args)
				if args == "" {
					args = "{}"
				}
				items = append(items, map[string]any{"type": "function_call", "call_id": p.ToolCall.ID, "name": p.ToolCall.Name, "arguments": args})
			case p.ToolResult != nil:
				flush()
				items = append(items, map[string]any{"type": "function_call_output", "call_id": p.ToolResult.CallID, "output": toolOutput(*p.ToolResult)})
				for _, img := range p.ToolResult.Images {
					images = append(images, map[string]any{"type": "input_image", "image_url": dataURL(img)})
				}
			}
		}
		flush()
		if len(images) > 0 {
			items = append(items, map[string]any{"role": "user", "content": images})
		}
	}
	return items
}

func toolOutput(r ToolResult) string {
	s := nonEmpty(r.Content)
	if r.IsError {
		s = "Error: " + s
	}
	return s
}

type responsesStream struct {
	yield   func(Event, error) bool
	calls   bool
	stopped bool
	quit    bool
}

func (s *responsesStream) emit(e Event) bool {
	if !s.yield(e, nil) {
		s.quit = true
		return false
	}
	return true
}

type responsesItem struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Encrypted string `json:"encrypted_content"`
	Summary   []struct {
		Text string `json:"text"`
	} `json:"summary"`
}

func (s *responsesStream) event(ev sseEvent) (bool, error) {
	var msg struct {
		Type     string        `json:"type"`
		Delta    string        `json:"delta"`
		Item     responsesItem `json:"item"`
		Code     string        `json:"code"`
		Message  string        `json:"message"`
		Response struct {
			Status            string `json:"status"`
			IncompleteDetails struct {
				Reason string `json:"reason"`
			} `json:"incomplete_details"`
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
			Usage struct {
				Input   int `json:"input_tokens"`
				Output  int `json:"output_tokens"`
				Details struct {
					Cached int `json:"cached_tokens"`
				} `json:"input_tokens_details"`
				OutDetails struct {
					Reasoning int `json:"reasoning_tokens"`
				} `json:"output_tokens_details"`
			} `json:"usage"`
		} `json:"response"`
	}
	if string(ev.Data) == "[DONE]" {
		return false, nil
	}
	if err := json.Unmarshal(ev.Data, &msg); err != nil {
		return false, &Error{Message: "bad stream event: " + err.Error()}
	}
	switch msg.Type {
	case "response.output_text.delta":
		return s.emit(Event{Type: TextDelta, Delta: msg.Delta}), nil
	case "response.reasoning_summary_text.delta":
		return s.emit(Event{Type: ThinkingDelta, Delta: msg.Delta}), nil
	case "response.reasoning_summary_part.done":
		return s.emit(Event{Type: ThinkingDelta, Delta: "\n\n"}), nil
	case "response.output_item.done":
		it := msg.Item
		switch it.Type {
		case "reasoning":
			var text []string
			for _, p := range it.Summary {
				text = append(text, p.Text)
			}
			return s.emit(Event{Type: ThinkingDone, Thinking: &Thinking{Text: strings.Join(text, "\n\n"), Opaque: it.Encrypted, Provider: openaiName}}), nil
		case "function_call":
			args := json.RawMessage(it.Arguments)
			if len(args) == 0 {
				args = json.RawMessage("{}")
			}
			if !json.Valid(args) {
				return false, &Error{Message: "tool call " + it.Name + " has invalid JSON arguments"}
			}
			s.calls = true
			return s.emit(Event{Type: ToolCallDone, ToolCall: &ToolCall{ID: it.CallID, Name: it.Name, Args: args}}), nil
		}
	case "response.completed", "response.incomplete":
		u := msg.Response.Usage
		if !s.emit(Event{Type: UsageUpdate, Usage: Usage{Input: u.Input, Output: u.Output, CacheRead: u.Details.Cached, Reasoning: u.OutDetails.Reasoning}}) {
			return false, nil
		}
		stop := StopEnd
		switch {
		case s.calls:
			stop = StopToolUse
		case msg.Type == "response.incomplete" && msg.Response.IncompleteDetails.Reason == "max_output_tokens":
			stop = StopMaxTokens
		case msg.Type == "response.incomplete" && msg.Response.IncompleteDetails.Reason == "content_filter":
			stop = StopRefusal
		}
		s.stopped = true
		s.emit(Event{Type: Done, Stop: stop})
		return false, nil
	case "response.failed":
		e := msg.Response.Error
		return false, streamError(e.Code, e.Message)
	case "error":
		return false, streamError(msg.Code, msg.Message)
	}
	return true, nil
}

// Chat speaks the Chat Completions API that most other providers and local
// servers offer: OpenRouter, DeepSeek, Groq, Ollama, LM Studio…
type Chat struct {
	BaseURL       string // the URL before /chat/completions, e.g. https://openrouter.ai/api/v1
	Auth          Auth
	HTTP          *http.Client
	Compat        ChatCompat
	Provider      string
	Headers       map[string]string
	SessionHeader string
}

// ChatCompat covers how servers that speak Chat Completions differ.
type ChatCompat struct {
	// MaxTokensField names the output limit: "max_tokens" when empty, or
	// "max_completion_tokens".
	MaxTokensField string
	// Thinking is how a request asks for reasoning:
	//   ""           reasoning_effort, as OpenAI does
	//   "none"       not at all
	//   "deepseek"   thinking {type: enabled} and reasoning_effort
	//   "zai"        thinking {type: enabled, clear_thinking: false} and reasoning_effort
	//   "qwen"       enable_thinking and reasoning_effort
	//   "openrouter" reasoning {effort}
	//   "together"   reasoning {enabled: true} and reasoning_effort
	//   "ant-ling"   reasoning {effort}
	//   "baseten"    chat_template_args and reasoning_effort
	Thinking string
	// ReplayReasoning sends an assistant message's reasoning back as
	// reasoning_content (DeepSeek requires it after tool calls).
	ReplayReasoning            bool
	NoReasoningEffort          bool
	ZaiToolStream              bool
	ChatTemplateArgs           map[string]any
	ThinkingLevelMap           map[string]*string
	SendSessionAffinityHeaders bool
	SessionAffinityFormat      string
}

func (p *Chat) Stream(ctx context.Context, req Request) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		header := http.Header{}
		if p.Auth != nil {
			h, err := p.Auth(ctx)
			if err != nil {
				yield(Event{}, err)
				return
			}
			if h != nil {
				header = h.Clone()
			}
		}
		for k, v := range p.Headers {
			header.Set(k, v)
		}
		compat := p.Compat
		if p.Provider != "" {
			if m, ok := FindModel(p.Provider, req.Model); ok {
				for k, v := range m.Headers {
					header.Set(k, v)
				}
				if !m.Reasoning {
					req.Effort = ""
				}
				if len(m.ThinkingLevelMap) > 0 {
					compat.ThinkingLevelMap = m.ThinkingLevelMap
				}
				if mc := m.Compat; mc != nil {
					if mc.MaxTokensField != "" {
						compat.MaxTokensField = mc.MaxTokensField
					}
					if mc.ThinkingFormat != "" {
						compat.Thinking = mc.ThinkingFormat
					}
					if mc.RequiresReasoningContentOnAssistantMessages {
						compat.ReplayReasoning = true
					}
					if mc.SupportsReasoningEffort != nil {
						compat.NoReasoningEffort = !*mc.SupportsReasoningEffort
					}
					if mc.ZaiToolStream {
						compat.ZaiToolStream = true
					}
					if len(mc.ChatTemplateArgs) > 0 {
						compat.ChatTemplateArgs = mc.ChatTemplateArgs
					}
					if mc.SendSessionAffinityHeaders {
						compat.SendSessionAffinityHeaders = true
					}
					if mc.SessionAffinityFormat != "" {
						compat.SessionAffinityFormat = mc.SessionAffinityFormat
					}
				}
			}
		}
		if p.SessionHeader != "" && req.SessionID != "" {
			header.Set(p.SessionHeader, req.SessionID)
		}
		if compat.SendSessionAffinityHeaders && req.SessionID != "" {
			if compat.SessionAffinityFormat == "openrouter" {
				header.Set("x-session-id", req.SessionID)
			} else {
				header.Set("x-client-request-id", req.SessionID)
				header.Set("x-session-affinity", req.SessionID)
				if compat.SessionAffinityFormat != "openai-nosession" {
					header.Set("session_id", req.SessionID)
				}
			}
		}
		resp, err := client{p.HTTP}.post(ctx, strings.TrimSuffix(p.BaseURL, "/")+"/chat/completions", header, chatBody(req, compat))
		if err != nil {
			yield(Event{}, err)
			return
		}
		defer resp.Body.Close()
		s := chatStream{yield: yield, calls: map[int]*chatCall{}}
		err = readSSE(resp.Body, s.event)
		if err == nil && !s.quit {
			err = s.finish()
		}
		if err != nil {
			yield(Event{}, err)
		}
	}
}

func chatBody(req Request, compat ChatCompat) map[string]any {
	var msgs []any
	if req.System != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": req.System})
	}
	for _, m := range req.Messages {
		var text strings.Builder
		var parts []any
		var calls []any
		var images bool
		var reasoning strings.Builder
		for _, p := range m.Parts {
			switch {
			case p.Thinking != nil:
				if p.Thinking.Provider == "" || p.Thinking.Provider == chatName {
					reasoning.WriteString(p.Thinking.Text)
				}
			case p.Text != "":
				text.WriteString(p.Text)
				parts = append(parts, map[string]any{"type": "text", "text": p.Text})
			case p.Image != nil:
				images = true
				parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": dataURL(*p.Image)}})
			case p.ToolCall != nil:
				args := string(p.ToolCall.Args)
				if args == "" {
					args = "{}"
				}
				calls = append(calls, map[string]any{"id": p.ToolCall.ID, "type": "function", "function": map[string]any{"name": p.ToolCall.Name, "arguments": args}})
			case p.ToolResult != nil:
				msgs = append(msgs, map[string]any{"role": "tool", "tool_call_id": p.ToolResult.CallID, "content": toolOutput(*p.ToolResult)})
			}
		}
		if m.Role == Assistant {
			if text.Len() == 0 && len(calls) == 0 {
				continue
			}
			msg := map[string]any{"role": "assistant", "content": text.String()}
			if compat.ReplayReasoning {
				msg["reasoning_content"] = reasoning.String()
			}
			if len(calls) > 0 {
				msg["tool_calls"] = calls
			}
			msgs = append(msgs, msg)
		} else if images {
			msgs = append(msgs, map[string]any{"role": "user", "content": parts})
		} else if text.Len() > 0 {
			msgs = append(msgs, map[string]any{"role": "user", "content": text.String()})
		}
	}
	body := map[string]any{
		"model":          req.Model,
		"messages":       msgs,
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
	}
	if req.MaxTokens > 0 {
		body[cmp.Or(compat.MaxTokensField, "max_tokens")] = req.MaxTokens
	}
	chatThinking(body, req.Effort, compat)
	if len(req.Tools) > 0 {
		tools := make([]any, len(req.Tools))
		for i, t := range req.Tools {
			tools[i] = map[string]any{"type": "function", "function": map[string]any{"name": t.Name, "description": t.Description, "parameters": toolSchema(t)}}
		}
		body["tools"] = tools
		if compat.ZaiToolStream {
			body["tool_stream"] = true
		}
	}
	return body
}

// chatThinking asks for reasoning the way the server understands. An empty
// effort leaves the server's default.
func chatThinking(body map[string]any, effort string, compat ChatCompat) {
	if effort == "" {
		return
	}
	mapped := ResolveEffort(effort, compat.ThinkingLevelMap)
	switch compat.Thinking {
	case "none":
	case "deepseek":
		body["thinking"] = map[string]any{"type": "enabled"}
		if !compat.NoReasoningEffort && mapped != "" {
			body["reasoning_effort"] = mapped
		}
	case "zai":
		body["thinking"] = map[string]any{"type": "enabled", "clear_thinking": false}
		if !compat.NoReasoningEffort && mapped != "" {
			body["reasoning_effort"] = mapped
		}
	case "qwen":
		body["enable_thinking"] = true
		if !compat.NoReasoningEffort && mapped != "" {
			body["reasoning_effort"] = mapped
		}
	case "openrouter":
		if mapped != "" {
			body["reasoning"] = map[string]any{"effort": mapped}
		}
	case "together":
		body["reasoning"] = map[string]any{"enabled": true}
		if !compat.NoReasoningEffort && mapped != "" {
			body["reasoning_effort"] = mapped
		}
	case "ant-ling":
		if mapped != "" {
			body["reasoning"] = map[string]any{"effort": mapped}
		}
	case "baseten":
		if len(compat.ChatTemplateArgs) > 0 {
			args := make(map[string]any, len(compat.ChatTemplateArgs))
			for k, v := range compat.ChatTemplateArgs {
				if obj, ok := v.(map[string]any); ok && obj["$var"] == "thinking.enabled" {
					args[k] = true
				} else {
					args[k] = v
				}
			}
			body["chat_template_args"] = args
		}
		if !compat.NoReasoningEffort && mapped != "" {
			body["reasoning_effort"] = mapped
		}
	default:
		if !compat.NoReasoningEffort && mapped != "" {
			body["reasoning_effort"] = mapped
		}
	}
}

const chatName = "chat"

type chatCall struct {
	id, name string
	args     strings.Builder
}

type chatStream struct {
	yield     func(Event, error) bool
	calls     map[int]*chatCall
	order     []int
	finish_   string
	usage     *Usage
	reasoning strings.Builder
	quit      bool
}

func (s *chatStream) emit(e Event) bool {
	if !s.yield(e, nil) {
		s.quit = true
		return false
	}
	return true
}

func (s *chatStream) event(ev sseEvent) (bool, error) {
	if string(ev.Data) == "[DONE]" {
		return false, nil
	}
	var msg struct {
		Choices []struct {
			Delta struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
				Reasoning        string `json:"reasoning"`
				ToolCalls        []struct {
					Index    int    `json:"index"`
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage *struct {
			Prompt     int `json:"prompt_tokens"`
			Completion int `json:"completion_tokens"`
			Details    struct {
				Cached int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
			OutDetails struct {
				Reasoning int `json:"reasoning_tokens"`
			} `json:"completion_tokens_details"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
			Code    any    `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(ev.Data, &msg); err != nil {
		return false, &Error{Message: "bad stream event: " + err.Error()}
	}
	if msg.Error != nil {
		return false, streamError("error", msg.Error.Message)
	}
	if u := msg.Usage; u != nil {
		s.usage = &Usage{Input: u.Prompt, Output: u.Completion, CacheRead: u.Details.Cached, Reasoning: u.OutDetails.Reasoning}
	}
	for _, c := range msg.Choices {
		d := c.Delta
		if r := d.ReasoningContent + d.Reasoning; r != "" {
			s.reasoning.WriteString(r)
			if !s.emit(Event{Type: ThinkingDelta, Delta: r}) {
				return false, nil
			}
		}
		if d.Content != "" {
			if !s.emit(Event{Type: TextDelta, Delta: d.Content}) {
				return false, nil
			}
		}
		for _, tc := range d.ToolCalls {
			call := s.calls[tc.Index]
			if call == nil {
				call = &chatCall{}
				s.calls[tc.Index] = call
				s.order = append(s.order, tc.Index)
			}
			if tc.ID != "" {
				call.id = tc.ID
			}
			if tc.Function.Name != "" {
				call.name = tc.Function.Name
			}
			call.args.WriteString(tc.Function.Arguments)
		}
		if c.FinishReason != "" {
			s.finish_ = c.FinishReason
		}
	}
	return true, nil
}

// finish sends what the stream held back to its end: the reasoning, the
// tool calls, the usage and the stop.
func (s *chatStream) finish() error {
	if s.reasoning.Len() > 0 && !s.emit(Event{Type: ThinkingDone, Thinking: &Thinking{Text: s.reasoning.String(), Provider: chatName}}) {
		return nil
	}
	for _, i := range s.order {
		c := s.calls[i]
		args := json.RawMessage(c.args.String())
		if len(args) == 0 {
			args = json.RawMessage("{}")
		}
		if !json.Valid(args) {
			return &Error{Message: "tool call " + c.name + " has invalid JSON arguments"}
		}
		if !s.emit(Event{Type: ToolCallDone, ToolCall: &ToolCall{ID: c.id, Name: c.name, Args: args}}) {
			return nil
		}
	}
	if s.usage != nil && !s.emit(Event{Type: UsageUpdate, Usage: *s.usage}) {
		return nil
	}
	if s.finish_ == "" {
		return &Error{Message: "stream ended early", Retryable: true}
	}
	stop := StopEnd
	switch {
	case len(s.order) > 0 || s.finish_ == "tool_calls":
		stop = StopToolUse
	case s.finish_ == "length":
		stop = StopMaxTokens
	case s.finish_ == "content_filter":
		stop = StopRefusal
	}
	s.emit(Event{Type: Done, Stop: stop})
	return nil
}
