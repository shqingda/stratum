package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"iter"
	"net/http"
	"net/url"
	"strings"
)

// Gemini speaks Google's Generative Language API.
type Gemini struct {
	BaseURL       string // https://generativelanguage.googleapis.com when empty
	Auth          Auth   // e.g. StaticHeader("x-goog-api-key", key)
	HTTP          *http.Client
	Provider      string
	Headers       map[string]string
	SessionHeader string
}

const geminiName = "google"

func (p *Gemini) Stream(ctx context.Context, req Request) iter.Seq2[Event, error] {
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
		base := p.BaseURL
		if base == "" {
			base = "https://generativelanguage.googleapis.com"
		}
		base = strings.TrimSuffix(base, "/")
		if !strings.HasSuffix(base, "/v1beta") && !strings.HasSuffix(base, "/v1") {
			base += "/v1beta"
		}
		u := base + "/models/" + url.PathEscape(req.Model) + ":streamGenerateContent?alt=sse"
		resp, err := client{p.HTTP}.post(ctx, u, header, geminiBody(req))
		if err != nil {
			yield(Event{}, err)
			return
		}
		defer resp.Body.Close()
		s := geminiStream{yield: yield}
		err = readSSE(resp.Body, s.event)
		if err == nil && !s.quit {
			err = s.finish()
		}
		if err != nil {
			yield(Event{}, err)
		}
	}
}

func geminiBody(req Request) map[string]any {
	var contents []any
	for _, m := range req.Messages {
		role := "user"
		if m.Role == Assistant {
			role = "model"
		}
		var parts []any
		for _, p := range m.Parts {
			switch {
			case p.Text != "":
				parts = append(parts, map[string]any{"text": p.Text})
			case p.Image != nil:
				parts = append(parts, geminiImage(*p.Image))
			case p.Thinking != nil:
				t := p.Thinking
				if t.Provider == geminiName && t.Signature != "" {
					parts = append(parts, map[string]any{"text": t.Text, "thought": true, "thoughtSignature": t.Signature})
				}
			case p.ToolCall != nil:
				var args any = map[string]any{}
				if len(p.ToolCall.Args) > 0 {
					args = p.ToolCall.Args
				}
				part := map[string]any{"functionCall": map[string]any{"name": p.ToolCall.Name, "args": args}}
				if p.ToolCall.Signature != "" {
					part["thoughtSignature"] = p.ToolCall.Signature
				}
				parts = append(parts, part)
			case p.ToolResult != nil:
				r := p.ToolResult
				key := "output"
				if r.IsError {
					key = "error"
				}
				parts = append(parts, map[string]any{"functionResponse": map[string]any{
					"name": r.Name, "response": map[string]any{key: nonEmpty(r.Content)},
				}})
				for _, img := range r.Images {
					parts = append(parts, geminiImage(img))
				}
			}
		}
		if len(parts) > 0 {
			contents = append(contents, map[string]any{"role": role, "parts": parts})
		}
	}
	body := map[string]any{"contents": contents}
	if req.System != "" {
		body["systemInstruction"] = map[string]any{"parts": []any{map[string]any{"text": req.System}}}
	}
	if len(req.Tools) > 0 {
		decls := make([]any, len(req.Tools))
		for i, t := range req.Tools {
			decls[i] = map[string]any{"name": t.Name, "description": t.Description, "parametersJsonSchema": toolSchema(t)}
		}
		body["tools"] = []any{map[string]any{"functionDeclarations": decls}}
	}
	gen := map[string]any{}
	if req.MaxTokens > 0 {
		gen["maxOutputTokens"] = req.MaxTokens
	}
	if req.Effort != "" {
		thinking := map[string]any{"includeThoughts": true}
		if strings.Contains(req.Model, "gemini-2") {
			thinking["thinkingBudget"] = map[string]int{"minimal": 1024, "low": 2048, "medium": 8192, "high": 24576, "xhigh": 24576}[req.Effort]
		} else {
			level := req.Effort
			if level == "medium" {
				level = "high"
			}
			thinking["thinkingLevel"] = level
		}
		gen["thinkingConfig"] = thinking
	}
	if len(gen) > 0 {
		body["generationConfig"] = gen
	}
	return body
}

func geminiImage(img Image) map[string]any {
	return map[string]any{"inlineData": map[string]any{"mimeType": img.MediaType, "data": base64.StdEncoding.EncodeToString(img.Data)}}
}

type geminiStream struct {
	yield     func(Event, error) bool
	thinking  strings.Builder
	signature string
	calls     int
	finish_   string
	quit      bool
}

func (s *geminiStream) emit(e Event) bool {
	if !s.yield(e, nil) {
		s.quit = true
		return false
	}
	return true
}

// flushThinking ends the thinking block that text or a call follows.
func (s *geminiStream) flushThinking() bool {
	if s.thinking.Len() == 0 && s.signature == "" {
		return true
	}
	t := &Thinking{Text: s.thinking.String(), Signature: s.signature, Provider: geminiName}
	s.thinking.Reset()
	s.signature = ""
	return s.emit(Event{Type: ThinkingDone, Thinking: t})
}

func (s *geminiStream) event(ev sseEvent) (bool, error) {
	var msg struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text             string `json:"text"`
					Thought          bool   `json:"thought"`
					ThoughtSignature string `json:"thoughtSignature"`
					FunctionCall     *struct {
						ID   string          `json:"id"`
						Name string          `json:"name"`
						Args json.RawMessage `json:"args"`
					} `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		Usage *struct {
			Prompt   int `json:"promptTokenCount"`
			Output   int `json:"candidatesTokenCount"`
			Cached   int `json:"cachedContentTokenCount"`
			Thoughts int `json:"thoughtsTokenCount"`
		} `json:"usageMetadata"`
		Error *struct {
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(ev.Data, &msg); err != nil {
		return false, &Error{Message: "bad stream event: " + err.Error()}
	}
	if msg.Error != nil {
		return false, streamError(msg.Error.Status, msg.Error.Message)
	}
	for _, c := range msg.Candidates {
		for _, p := range c.Content.Parts {
			switch {
			case p.FunctionCall != nil:
				if !s.flushThinking() {
					return false, nil
				}
				s.calls++
				id := p.FunctionCall.ID
				if id == "" {
					id = fmt.Sprintf("call_%d", s.calls)
				}
				args := p.FunctionCall.Args
				if len(args) == 0 || string(args) == "null" {
					args = json.RawMessage("{}")
				}
				if !s.emit(Event{Type: ToolCallDone, ToolCall: &ToolCall{ID: id, Name: p.FunctionCall.Name, Args: args, Signature: p.ThoughtSignature}}) {
					return false, nil
				}
			case p.Thought:
				s.thinking.WriteString(p.Text)
				if p.ThoughtSignature != "" {
					s.signature = p.ThoughtSignature
				}
				if p.Text != "" && !s.emit(Event{Type: ThinkingDelta, Delta: p.Text}) {
					return false, nil
				}
			default:
				if p.ThoughtSignature != "" {
					s.signature = p.ThoughtSignature
				}
				if p.Text == "" {
					continue
				}
				if !s.flushThinking() || !s.emit(Event{Type: TextDelta, Delta: p.Text}) {
					return false, nil
				}
			}
		}
		if c.FinishReason != "" {
			s.finish_ = c.FinishReason
		}
	}
	if u := msg.Usage; u != nil {
		if !s.emit(Event{Type: UsageUpdate, Usage: Usage{Input: u.Prompt, Output: u.Output + u.Thoughts, CacheRead: u.Cached, Reasoning: u.Thoughts}}) {
			return false, nil
		}
	}
	return true, nil
}

func (s *geminiStream) finish() error {
	if !s.flushThinking() {
		return nil
	}
	if s.finish_ == "" {
		return &Error{Message: "stream ended early", Retryable: true}
	}
	stop := StopEnd
	switch {
	case s.calls > 0:
		stop = StopToolUse
	case s.finish_ == "MAX_TOKENS":
		stop = StopMaxTokens
	case s.finish_ == "SAFETY" || s.finish_ == "PROHIBITED_CONTENT" || s.finish_ == "RECITATION":
		stop = StopRefusal
	}
	s.emit(Event{Type: Done, Stop: stop})
	return nil
}
