package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"sync"
	"time"
)

// Demo answers every request offline by echoing the last user text. It
// lets the app run without an account.
type Demo struct {
	// Delay is the pause between chunks; zero streams at once.
	Delay time.Duration
}

func (d Demo) Stream(ctx context.Context, req Request) iter.Seq2[Event, error] {
	var last string
	for i := len(req.Messages) - 1; i >= 0 && last == ""; i-- {
		if req.Messages[i].Role == User {
			last = req.Messages[i].PlainText()
		}
	}
	reply := "Talos is in offline demo mode. Sign in to a model provider to get real answers.\n\nYou wrote: " + last
	return (&Script{Turns: []Turn{{Text: reply}}, Delay: d.Delay, Loop: true}).Stream(ctx, req)
}

// Turn is one scripted reply.
type Turn struct {
	Thinking string
	Text     string
	Calls    []ToolCall
	Err      error // returned instead of the reply
}

// Call makes a scripted tool call with arguments marshaled from args.
func Call(id, name string, args any) ToolCall {
	b, err := json.Marshal(args)
	if err != nil {
		panic(err)
	}
	return ToolCall{ID: id, Name: name, Args: b}
}

// Script replays turns in order, one per request, to test what drives a
// model: tool dispatch, interruption, retries. After the last turn it
// fails, unless Loop repeats the last one. A Script value is safe to share
// between goroutines; use it by pointer.
type Script struct {
	Turns []Turn
	Delay time.Duration
	Loop  bool
	// Requests records every request, for assertions.
	Requests []Request

	mu sync.Mutex
	n  int
}

func (s *Script) next(req Request) (Turn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Requests = append(s.Requests, req)
	if s.n >= len(s.Turns) {
		if !s.Loop || len(s.Turns) == 0 {
			return Turn{}, fmt.Errorf("script: no turn %d", s.n+1)
		}
		return s.Turns[len(s.Turns)-1], nil
	}
	t := s.Turns[s.n]
	s.n++
	return t, nil
}

func (s *Script) Stream(ctx context.Context, req Request) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		t, err := s.next(req)
		if err == nil {
			err = t.Err
		}
		if err != nil {
			yield(Event{}, err)
			return
		}
		pause := func() bool {
			if s.Delay == 0 {
				return ctx.Err() == nil
			}
			select {
			case <-ctx.Done():
				return false
			case <-time.After(s.Delay):
				return true
			}
		}
		chunks := func(text string, typ EventType) bool {
			r := []rune(text)
			for i := 0; i < len(r); i += 12 {
				if !pause() {
					yield(Event{}, ctx.Err())
					return false
				}
				if !yield(Event{Type: typ, Delta: string(r[i:min(i+12, len(r))])}, nil) {
					return false
				}
			}
			return true
		}
		if t.Thinking != "" {
			if !chunks(t.Thinking, ThinkingDelta) || !yield(Event{Type: ThinkingDone, Thinking: &Thinking{Text: t.Thinking}}, nil) {
				return
			}
		}
		if !chunks(t.Text, TextDelta) {
			return
		}
		for i := range t.Calls {
			c := t.Calls[i]
			if !yield(Event{Type: ToolCallDone, ToolCall: &c}, nil) {
				return
			}
		}
		out := (len(t.Text) + len(t.Thinking)) / 4
		if !yield(Event{Type: UsageUpdate, Usage: Usage{Input: estimate(req), Output: out}}, nil) {
			return
		}
		stop := StopEnd
		if len(t.Calls) > 0 {
			stop = StopToolUse
		}
		yield(Event{Type: Done, Stop: stop}, nil)
	}
}

// estimate guesses a request's input tokens at four bytes each.
func estimate(req Request) int {
	n := len(req.System)
	for _, m := range req.Messages {
		for _, p := range m.Parts {
			n += len(p.Text)
			if p.ToolCall != nil {
				n += len(p.ToolCall.Args)
			}
			if p.ToolResult != nil {
				n += len(p.ToolResult.Content)
			}
		}
	}
	return n / 4
}
