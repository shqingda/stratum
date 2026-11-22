// Package llm streams replies from language models behind one interface.
//
// A Provider turns a Request (system prompt, messages, tool schemas) into a
// stream of Events: text and thinking as they arrive, each tool call once
// its arguments are complete, the usage, and why the reply stopped. It knows
// nothing of agents; package kernel/agent builds on it.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"strings"
)

// Provider streams one model reply.
type Provider interface {
	Stream(ctx context.Context, req Request) iter.Seq2[Event, error]
}

// Request is one model call.
type Request struct {
	Model     string
	System    string
	Messages  []Message
	Tools     []Tool
	MaxTokens int
	// Effort asks for reasoning: "", "low", "medium" or "high". "" leaves the
	// model's default; models without reasoning ignore it.
	Effort string
	// SessionID names the conversation, so providers that key their prompt
	// cache by it (the ChatGPT backend) reuse the cached prefix.
	SessionID string
	// Temperature and TopP set the sampling when not nil, as an agent's
	// definition asks. Providers that refuse them while the model reasons
	// leave them out then; the ChatGPT backend never takes them.
	Temperature, TopP *float64
}

// Role is who said a message.
type Role string

const (
	User      Role = "user"
	Assistant Role = "assistant"
)

// Message is one turn. Tool results travel in a user message, as every
// provider's wire format puts them after the assistant's calls.
type Message struct {
	Role  Role   `json:"role"`
	Parts []Part `json:"parts"`
}

// Part is one piece of a message: exactly one of its fields is set.
type Part struct {
	Text       string      `json:"text,omitempty"`
	Thinking   *Thinking   `json:"thinking,omitempty"`
	Image      *Image      `json:"image,omitempty"`
	ToolCall   *ToolCall   `json:"toolCall,omitempty"`
	ToolResult *ToolResult `json:"toolResult,omitempty"`
}

// Thinking is reasoning the model showed. Signature (Anthropic) or Opaque
// (OpenAI's encrypted reasoning) must go back unchanged in later turns.
type Thinking struct {
	Text      string `json:"text,omitempty"`
	Signature string `json:"signature,omitempty"`
	Opaque    string `json:"opaque,omitempty"`
	// Provider names whose wire format Signature and Opaque belong to;
	// another provider drops them.
	Provider string `json:"provider,omitempty"`
}

// Image is an inline image.
type Image struct {
	MediaType string `json:"mediaType"` // image/png, image/jpeg…
	Data      []byte `json:"data"`
}

// ToolCall is the model asking to run a tool.
type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"` // a JSON object
	// Signature is opaque provider state (Gemini's thought signature) that
	// must go back with the call.
	Signature string `json:"signature,omitempty"`
}

// ToolResult answers a ToolCall.
type ToolResult struct {
	CallID  string  `json:"callId"`
	Name    string  `json:"name"`
	Content string  `json:"content"`
	Images  []Image `json:"images,omitempty"`
	IsError bool    `json:"isError,omitempty"`
	// Skipped is a call the user declined: it did not run.
	Skipped bool `json:"skipped,omitempty"`
}

// Tool is a tool offered to the model.
type Tool struct {
	Name        string
	Description string
	Schema      json.RawMessage // JSON Schema of the arguments, an object
}

// Text returns a user message of text.
func Text(role Role, s string) Message { return Message{Role: role, Parts: []Part{{Text: s}}} }

// EventType says what an Event carries.
type EventType int

const (
	TextDelta     EventType = iota + 1 // Delta of the reply's text
	ThinkingDelta                      // Delta of the reasoning
	ThinkingDone                       // a thinking block is complete (Thinking)
	ToolCallDone                       // a tool call is complete (ToolCall)
	UsageUpdate                        // Usage so far
	Done                               // the reply ended (Stop)
)

// Event is one step of a streamed reply.
type Event struct {
	Type     EventType
	Delta    string
	Thinking *Thinking
	ToolCall *ToolCall
	Usage    Usage
	Stop     StopReason
}

// StopReason is why a reply ended.
type StopReason string

const (
	StopEnd       StopReason = "end"
	StopToolUse   StopReason = "tool_use"
	StopMaxTokens StopReason = "max_tokens"
	StopRefusal   StopReason = "refusal"
)

// Usage counts tokens; CacheRead and CacheWrite are parts of Input.
type Usage struct {
	Input      int `json:"input"`
	Output     int `json:"output"`
	CacheRead  int `json:"cacheRead,omitempty"`
	CacheWrite int `json:"cacheWrite,omitempty"`
	Reasoning  int `json:"reasoning,omitempty"`
}

// Add sums usage.
func (u Usage) Add(v Usage) Usage {
	return Usage{u.Input + v.Input, u.Output + v.Output, u.CacheRead + v.CacheRead, u.CacheWrite + v.CacheWrite, u.Reasoning + v.Reasoning}
}

// Error is a failed call. Retryable says whether the same call may succeed
// later (rate limits, overload, a dropped stream).
type Error struct {
	Status    int
	Message   string
	Retryable bool
	// ContextOverflow marks a request too long for the model's window.
	ContextOverflow bool
}

func (e *Error) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("model error %d: %s", e.Status, e.Message)
	}
	return "model error: " + e.Message
}

// Retryable reports whether err is worth retrying.
func Retryable(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Retryable
}

// Reply is a whole reply, as Collect assembles it.
type Reply struct {
	Message Message
	Usage   Usage
	Stop    StopReason
}

// Collect reads a stream to its end, calling each(e) for every event when
// each is not nil, and returns the assistant's message.
func Collect(stream iter.Seq2[Event, error], each func(Event)) (Reply, error) {
	var r Reply
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			r.Message.Parts = append(r.Message.Parts, Part{Text: text.String()})
			text.Reset()
		}
	}
	r.Message.Role = Assistant
	for e, err := range stream {
		if err != nil {
			flush()
			return r, err
		}
		if each != nil {
			each(e)
		}
		switch e.Type {
		case TextDelta:
			text.WriteString(e.Delta)
		case ThinkingDone:
			flush()
			r.Message.Parts = append(r.Message.Parts, Part{Thinking: e.Thinking})
		case ToolCallDone:
			flush()
			r.Message.Parts = append(r.Message.Parts, Part{ToolCall: e.ToolCall})
		case UsageUpdate:
			r.Usage = e.Usage
		case Done:
			r.Stop = e.Stop
		}
	}
	flush()
	if r.Stop == "" {
		return r, &Error{Message: "stream ended without a stop", Retryable: true}
	}
	return r, nil
}

// ToolCalls returns the tool calls in m.
func (m Message) ToolCalls() []ToolCall {
	var calls []ToolCall
	for _, p := range m.Parts {
		if p.ToolCall != nil {
			calls = append(calls, *p.ToolCall)
		}
	}
	return calls
}

// PlainText joins the text parts of m.
func (m Message) PlainText() string {
	var b strings.Builder
	for _, p := range m.Parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

// toolSchema returns a tool's argument schema, an empty object schema when
// it has none: every provider rejects a null one.
func toolSchema(t Tool) json.RawMessage {
	if len(t.Schema) == 0 || string(t.Schema) == "null" {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	return t.Schema
}
