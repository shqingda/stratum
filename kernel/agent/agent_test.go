package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/shqingda/stratum/llm"
)

type fakeTools struct {
	mu    sync.Mutex
	calls []string
	block chan struct{} // when set, Run waits on it or ctx
}

func (f *fakeTools) Specs() []llm.Tool { return []llm.Tool{{Name: "read"}} }

func (f *fakeTools) Run(ctx context.Context, call llm.ToolCall) (llm.ToolResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, call.ID)
	f.mu.Unlock()
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return llm.ToolResult{}, ctx.Err()
		}
	}
	return llm.ToolResult{Content: "ok:" + call.ID}, nil
}

type recorder struct {
	mu     sync.Mutex
	events []Event
}

func (r *recorder) Emit(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recorder) messages() []llm.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []llm.Message
	for _, e := range r.events {
		if e.Type == Message {
			out = append(out, *e.Message)
		}
	}
	return out
}

type instant struct{ slept []time.Duration }

func (c *instant) Sleep(ctx context.Context, d time.Duration) error {
	c.slept = append(c.slept, d)
	return ctx.Err()
}

func TestToolTurn(t *testing.T) {
	s := &llm.Script{Turns: []llm.Turn{
		{Text: "Reading.", Calls: []llm.ToolCall{llm.Call("a", "read", nil), llm.Call("b", "read", nil)}},
		{Text: "Done."},
	}}
	rec, tools := &recorder{}, &fakeTools{}
	ag := &Agent{Provider: s, Tools: tools, Sink: rec}
	u, err := ag.Run(context.Background(), Config{Model: "m"}, []llm.Message{llm.Text(llm.User, "earlier")}, llm.Text(llm.User, "go"))
	if err != nil {
		t.Fatal(err)
	}
	msgs := rec.messages()
	if len(msgs) != 4 || msgs[0].PlainText() != "go" || msgs[3].PlainText() != "Done." {
		t.Fatalf("%+v", msgs)
	}
	if r := msgs[2].Parts; len(r) != 2 || r[1].ToolResult.Content != "ok:b" || r[1].ToolResult.CallID != "b" {
		t.Errorf("results %+v", r)
	}
	if len(s.Requests) != 2 || len(s.Requests[0].Messages) != 2 || len(s.Requests[1].Messages) != 4 || len(s.Requests[0].Tools) != 1 {
		t.Errorf("requests %+v", s.Requests)
	}
	if u.Output == 0 {
		t.Error("no usage")
	}
}

func TestRetry(t *testing.T) {
	s := &llm.Script{Turns: []llm.Turn{
		{Err: &llm.Error{Status: 529, Retryable: true}},
		{Err: &llm.Error{Status: 529, Retryable: true}},
		{Text: "fine"},
	}}
	clock, rec := &instant{}, &recorder{}
	ag := &Agent{Provider: s, Tools: &fakeTools{}, Sink: rec, Clock: clock}
	if _, err := ag.Run(context.Background(), Config{}, nil, llm.Text(llm.User, "hi")); err != nil {
		t.Fatal(err)
	}
	if len(clock.slept) != 2 || clock.slept[1] != 2*time.Second {
		t.Errorf("slept %v", clock.slept)
	}

	// A request that is not retryable fails the turn at once.
	s = &llm.Script{Turns: []llm.Turn{{Err: &llm.Error{Status: 400, Message: "bad"}}}}
	ag = &Agent{Provider: s, Tools: &fakeTools{}, Clock: clock}
	if _, err := ag.Run(context.Background(), Config{}, nil, llm.Text(llm.User, "hi")); err == nil {
		t.Fatal("no error")
	}
}

func TestStepLimit(t *testing.T) {
	s := &llm.Script{Loop: true, Turns: []llm.Turn{{Calls: []llm.ToolCall{llm.Call("a", "read", nil)}}}}
	ag := &Agent{Provider: s, Tools: &fakeTools{}}
	if _, err := ag.Run(context.Background(), Config{MaxSteps: 3}, nil, llm.Text(llm.User, "hi")); !errors.Is(err, ErrStepLimit) {
		t.Fatal(err)
	}
	if len(s.Requests) != 3 {
		t.Error(len(s.Requests))
	}
}

// Cancelling while a tool runs ends the turn with every call answered.
func TestCancelDuringTools(t *testing.T) {
	s := &llm.Script{Turns: []llm.Turn{{Calls: []llm.ToolCall{llm.Call("a", "read", nil), llm.Call("b", "read", nil)}}}}
	tools := &fakeTools{block: make(chan struct{})}
	rec := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			tools.mu.Lock()
			n := len(tools.calls)
			tools.mu.Unlock()
			if n > 0 {
				cancel()
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	_, err := (&Agent{Provider: s, Tools: tools, Sink: rec}).Run(ctx, Config{}, nil, llm.Text(llm.User, "hi"))
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	msgs := rec.messages()
	last := msgs[len(msgs)-1]
	if len(last.Parts) != 2 || last.Parts[0].ToolResult.Content != Interrupted || last.Parts[1].ToolResult.Content != Interrupted {
		t.Errorf("%+v", last)
	}
	if len(tools.calls) != 1 {
		t.Errorf("ran %v", tools.calls)
	}
}

// Cancelling mid-reply keeps the text written so far, without its calls.
func TestCancelMidReply(t *testing.T) {
	s := &llm.Script{Delay: 5 * time.Millisecond, Turns: []llm.Turn{{Text: "a long answer that keeps going and going", Calls: []llm.ToolCall{llm.Call("a", "read", nil)}}}}
	rec := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	ag := &Agent{Provider: s, Tools: &fakeTools{}, Sink: &cancelAfterText{rec, cancel}}
	_, err := ag.Run(ctx, Config{}, nil, llm.Text(llm.User, "hi"))
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	msgs := rec.messages()
	if len(msgs) != 2 || msgs[1].PlainText() == "" || len(msgs[1].ToolCalls()) != 0 {
		t.Errorf("%+v", msgs)
	}
}

type cancelAfterText struct {
	*recorder
	cancel func()
}

func (c *cancelAfterText) Emit(e Event) {
	c.recorder.Emit(e)
	if e.Type == TextDelta {
		c.cancel()
	}
}

func TestSteer(t *testing.T) {
	s := &llm.Script{Turns: []llm.Turn{{Calls: []llm.ToolCall{llm.Call("a", "read", nil)}}, {Text: "ok"}}}
	queued := []llm.Part{{Text: "also check tests"}}
	ag := &Agent{Provider: s, Tools: &fakeTools{}}
	ag.Run(context.Background(), Config{Steer: func() []llm.Part { q := queued; queued = nil; return q }}, nil, llm.Text(llm.User, "hi"))
	results := s.Requests[1].Messages[2]
	if len(results.Parts) != 2 || results.Parts[1].Text != "also check tests" {
		t.Errorf("%+v", results)
	}
}

// TestConcurrent runs neighbouring concurrent calls together: each waits
// for the other to start.
func TestConcurrent(t *testing.T) {
	s := &llm.Script{Turns: []llm.Turn{{Calls: []llm.ToolCall{llm.Call("a", "task", nil), llm.Call("b", "task", nil), llm.Call("c", "read", nil)}}, {Text: "ok"}}}
	var wg sync.WaitGroup
	wg.Add(2)
	both := make(chan struct{})
	go func() { wg.Wait(); close(both) }()
	tools := toolFunc(func(ctx context.Context, call llm.ToolCall) (llm.ToolResult, error) {
		if call.Name == "task" {
			wg.Done()
			select {
			case <-both:
			case <-time.After(2 * time.Second):
				return llm.ToolResult{Content: "alone"}, nil
			}
		}
		return llm.ToolResult{Content: call.ID}, nil
	})
	ag := &Agent{Provider: s, Tools: tools, Concurrent: func(name string) bool { return name == "task" }}
	ag.Run(context.Background(), Config{}, nil, llm.Text(llm.User, "hi"))
	results := s.Requests[1].Messages[2].Parts
	if len(results) != 3 || results[0].ToolResult.Content != "a" || results[1].ToolResult.Content != "b" || results[2].ToolResult.Content != "c" {
		t.Fatalf("%+v", results)
	}
}

type toolFunc func(ctx context.Context, call llm.ToolCall) (llm.ToolResult, error)

func (f toolFunc) Specs() []llm.Tool { return nil }
func (f toolFunc) Run(ctx context.Context, call llm.ToolCall) (llm.ToolResult, error) {
	return f(ctx, call)
}
