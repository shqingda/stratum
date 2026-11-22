package session

import (
	"context"
	"errors"
	"iter"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/proto"
)

// target is a model of an account to send requests to.
type target struct {
	account, model string
	provider       llm.Provider
}

// role opens what settings give a role; ok is false when they give
// nothing or it does not open, and the session's own model does the role.
func (m *Manager) role(ref *proto.ModelRef) (t target, ok bool) {
	if ref == nil || ref.Account == "" || ref.Model == "" {
		return target{}, false
	}
	p, err := m.Open(ref.Account)
	if err != nil {
		return target{}, false
	}
	return target{ref.Account, ref.Model, p}, true
}

// backups are the other accounts with their default models, in the order
// they are listed, for when the main one runs out.
func (m *Manager) backups(main string) []target {
	if m.Accounts == nil {
		return nil
	}
	var out []target
	for _, id := range m.Accounts() {
		if id == main {
			continue
		}
		provider, _, _ := strings.Cut(id, "/")
		model := llm.DefaultModel(provider)
		p, err := m.Open(id)
		if err != nil || model == "" {
			continue
		}
		out = append(out, target{id, model, p})
	}
	return out
}

// exhausted is an error that another account may not have: a rate limit
// or a used-up quota.
func exhausted(err error) bool {
	var e *llm.Error
	if !errors.As(err, &e) {
		return false
	}
	if e.Status == 429 || e.Status == 402 {
		return true
	}
	msg := strings.ToLower(e.Message)
	return slices.ContainsFunc([]string{"quota", "usage limit", "rate limit", "rate_limit", "insufficient", "credit"}, func(s string) bool {
		return strings.Contains(msg, s)
	})
}

// fallback sends to the first target, and moves on to the next when one
// is exhausted before it said anything; it stays there for the turn.
type fallback struct {
	mu      sync.Mutex
	targets []target
	at      int
	// moved is told of each move.
	moved func(from, to target, err error)
}

func (f *fallback) Stream(ctx context.Context, req llm.Request) iter.Seq2[llm.Event, error] {
	return func(yield func(llm.Event, error) bool) {
		for {
			f.mu.Lock()
			i := f.at
			t := f.targets[i]
			f.mu.Unlock()
			req.Model = t.model
			said := false
			var failed error
			for e, err := range t.provider.Stream(ctx, req) {
				if err != nil && !said && i+1 < len(f.targets) && exhausted(err) && ctx.Err() == nil {
					failed = err
					break
				}
				said = said || err == nil
				if !yield(e, err) || err != nil {
					return
				}
			}
			if failed == nil {
				return
			}
			f.mu.Lock()
			if f.at == i {
				f.at++
				if f.moved != nil {
					f.moved(t, f.targets[f.at], failed)
				}
			}
			f.mu.Unlock()
		}
	}
}

const titlePrompt = "Write a title for a conversation that starts with the message below: at most eight words, in the message's language, no quotes or full stop. Reply with the title only.\n\n"

// retitle has the light model name a new session, in place of its first
// words, when settings give one.
func (m *Manager) retitle(sessionID, first, text string) {
	set, _ := m.Settings(context.Background())
	if set.Light == nil || set.Light.Account == "" || set.Light.Model == "" {
		return
	}
	p, err := m.Open(set.Light.Account)
	if err != nil {
		return
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		reply, err := llm.Collect(p.Stream(ctx, llm.Request{Model: set.Light.Model, Effort: set.Light.Effort, MaxTokens: 64,
			Messages: []llm.Message{llm.Text(llm.User, titlePrompt+text)}}), nil)
		title := strings.Trim(strings.TrimSpace(reply.Message.PlainText()), "\"'“”「」。.")
		if err != nil || title == "" {
			return
		}
		x, err := m.Store.GetSession(ctx, sessionID)
		if err != nil || x.Title != first {
			return // renamed meanwhile
		}
		x.Title = titleOf(title)
		if m.Store.UpdateSession(ctx, x) == nil {
			x, _ = m.Store.GetSession(ctx, sessionID)
			m.record(ctx, sessionID, proto.EventSession, m.sessionProto(ctx, x))
		}
	}()
}
