// Package agent runs the loop of a turn: ask the model, run the tools it
// calls, hand back their results, until it answers. It knows nothing of
// storage, files or the protocol; it talks to four interfaces.
package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/shqingda/stratum/llm"
)

// Tools runs tool calls, deciding permission and asking the user as needed.
// A call that fails or is refused yields a result marked IsError; Run
// returns an error only when ctx ends.
type Tools interface {
	Specs() []llm.Tool
	Run(ctx context.Context, call llm.ToolCall) (llm.ToolResult, error)
}

// Sink receives what happens during a turn.
type Sink interface {
	Emit(Event)
}

// Clock sleeps between retries; tests make it instant.
type Clock interface {
	Sleep(ctx context.Context, d time.Duration) error
}

// EventType names an Event.
type EventType string

const (
	// Message is a message added to the conversation, to persist.
	Message EventType = "message"
	// TextDelta and ThinkingDelta stream the reply as it is written.
	TextDelta     EventType = "text.delta"
	ThinkingDelta EventType = "thinking.delta"
	// ToolStart and ToolEnd bracket a tool call.
	ToolStart EventType = "tool.start"
	ToolEnd   EventType = "tool.end"
	// Usage reports one request's tokens.
	Usage EventType = "usage"
	// Retry says a failed request is tried again; what streamed of it is
	// void.
	Retry EventType = "retry"
)

// Event is one thing that happened.
type Event struct {
	Type    EventType       `json:"type"`
	Delta   string          `json:"delta,omitempty"`
	Message *llm.Message    `json:"message,omitempty"`
	Call    *llm.ToolCall   `json:"call,omitempty"`
	Result  *llm.ToolResult `json:"result,omitempty"`
	Usage   *llm.Usage      `json:"usage,omitempty"`
	Error   string          `json:"error,omitempty"`
	Attempt int             `json:"attempt,omitempty"`
}

// Config is what a turn asks of the model.
type Config struct {
	Model, System, Effort, SessionID string
	MaxTokens                        int
	// Temperature and TopP set the sampling; nil leaves the model's own.
	Temperature, TopP *float64
	// MaxSteps bounds the model requests of a turn; 50 when zero.
	MaxSteps int
	// MaxRetries bounds the retries of one request; 3 when zero.
	MaxRetries int
	// Steer, when set, is asked after each round of tool calls for
	// messages the user sent meanwhile; they join the conversation before
	// the next request.
	Steer func() []llm.Part
}

// Agent runs turns.
type Agent struct {
	// Concurrent names the tools whose calls next to each other in a reply
	// run at the same time.
	Concurrent func(name string) bool
	Provider   llm.Provider
	Tools      Tools
	Sink       Sink
	Clock      Clock // real time when nil
}

// ErrStepLimit ends a turn that ran out of steps.
var ErrStepLimit = errors.New("agent: step limit reached")

// Interrupted is the result given to tool calls a cancelled turn did not
// finish, so the conversation stays well formed.
const Interrupted = "Interrupted by the user before this call finished."

// Run takes the conversation so far and the user's message, and runs the
// turn to its end. It returns the turn's usage. When ctx ends, the turn
// stops; what it wrote so far stays in the conversation, through Message
// events.
func (a *Agent) Run(ctx context.Context, cfg Config, history []llm.Message, input llm.Message) (llm.Usage, error) {
	msgs := append(append([]llm.Message(nil), history...), input)
	a.emit(Event{Type: Message, Message: &input})
	var total llm.Usage
	steps := cfg.MaxSteps
	if steps <= 0 {
		steps = 50
	}
	for range steps {
		reply, err := a.request(ctx, cfg, msgs)
		total = total.Add(reply.Usage)
		if len(reply.Message.Parts) > 0 {
			// Keep what streamed before an interruption.
			if err != nil {
				reply.Message.Parts = withoutCalls(reply.Message.Parts)
			}
			if len(reply.Message.Parts) > 0 {
				msgs = append(msgs, reply.Message)
				a.emit(Event{Type: Message, Message: &reply.Message})
			}
		}
		if err != nil {
			return total, err
		}
		calls := reply.Message.ToolCalls()
		if len(calls) == 0 {
			return total, nil
		}
		results := a.runTools(ctx, calls)
		if cfg.Steer != nil && ctx.Err() == nil {
			results.Parts = append(results.Parts, cfg.Steer()...)
		}
		msgs = append(msgs, results)
		a.emit(Event{Type: Message, Message: &results})
		if err := ctx.Err(); err != nil {
			return total, err
		}
	}
	return total, ErrStepLimit
}

// request streams one reply, retrying what can be retried.
func (a *Agent) request(ctx context.Context, cfg Config, msgs []llm.Message) (llm.Reply, error) {
	req := llm.Request{
		Model: cfg.Model, System: cfg.System, Messages: msgs, Tools: a.Tools.Specs(),
		MaxTokens: cfg.MaxTokens, Effort: cfg.Effort, SessionID: cfg.SessionID,
		Temperature: cfg.Temperature, TopP: cfg.TopP,
	}
	retries := cfg.MaxRetries
	if retries <= 0 {
		retries = 3
	}
	for attempt := 0; ; attempt++ {
		reply, err := llm.Collect(a.Provider.Stream(ctx, req), func(e llm.Event) {
			switch e.Type {
			case llm.TextDelta:
				a.emit(Event{Type: TextDelta, Delta: e.Delta})
			case llm.ThinkingDelta:
				a.emit(Event{Type: ThinkingDelta, Delta: e.Delta})
			}
		})
		if err == nil {
			u := reply.Usage
			a.emit(Event{Type: Usage, Usage: &u})
			return reply, nil
		}
		if ctx.Err() != nil {
			return reply, ctx.Err()
		}
		if !llm.Retryable(err) || attempt >= retries {
			return llm.Reply{}, err
		}
		a.emit(Event{Type: Retry, Error: err.Error(), Attempt: attempt + 1})
		if err := a.sleep(ctx, time.Second<<attempt); err != nil {
			return llm.Reply{}, err
		}
	}
}

// runTools runs calls one by one, but runs of concurrent ones together.
// Once ctx ends, the rest get Interrupted.
func (a *Agent) runTools(ctx context.Context, calls []llm.ToolCall) llm.Message {
	results := make([]llm.ToolResult, len(calls))
	for i := 0; i < len(calls); {
		j := i + 1
		if a.Concurrent != nil && a.Concurrent(calls[i].Name) {
			for j < len(calls) && a.Concurrent(calls[j].Name) {
				j++
			}
		}
		var wg sync.WaitGroup
		for k := i; k < j; k++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				results[k] = a.runTool(ctx, calls[k])
			}()
		}
		wg.Wait()
		i = j
	}
	msg := llm.Message{Role: llm.User}
	for i := range results {
		msg.Parts = append(msg.Parts, llm.Part{ToolResult: &results[i]})
	}
	return msg
}

func (a *Agent) runTool(ctx context.Context, call llm.ToolCall) llm.ToolResult {
	if ctx.Err() != nil {
		return llm.ToolResult{CallID: call.ID, Name: call.Name, Content: Interrupted, IsError: true}
	}
	a.emit(Event{Type: ToolStart, Call: &call})
	res, err := a.Tools.Run(ctx, call)
	if err != nil {
		res = llm.ToolResult{Content: Interrupted, IsError: true}
	}
	res.CallID, res.Name = call.ID, call.Name
	a.emit(Event{Type: ToolEnd, Call: &call, Result: &res})
	return res
}

func (a *Agent) emit(e Event) {
	if a.Sink != nil {
		a.Sink.Emit(e)
	}
}

func (a *Agent) sleep(ctx context.Context, d time.Duration) error {
	if a.Clock != nil {
		return a.Clock.Sleep(ctx, d)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// withoutCalls drops tool calls from a reply cut short: their results will
// never come.
func withoutCalls(parts []llm.Part) []llm.Part {
	var out []llm.Part
	for _, p := range parts {
		if p.ToolCall == nil {
			out = append(out, p)
		}
	}
	return out
}

// Describe says what ended a turn, for the user.
func Describe(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return "Stopped."
	case errors.Is(err, ErrStepLimit):
		return "Stopped after too many steps."
	}
	return fmt.Sprint(err)
}
