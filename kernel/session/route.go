package session

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shqingda/stratum/kernel/store"
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
const retitlePrompt = "Write a concise title for the conversation below: at most eight words, in the user's language, no quotes or trailing period. Reply with the title only.\n\n"
const suggestPrompt = "Suggest the next short follow-up prompt the user is most likely to send next in this coding session: at most 12 words, in the user's language, no quotes, no bullet or prefix. Reply with the prompt text only, or NONE if nothing obvious.\n\n"

func (m *Manager) lightOrSession(ctx context.Context, x store.Session, allowScript bool) (llm.Provider, string, string, bool) {
	set, _ := m.Settings(ctx)
	if t, ok := m.role(set.Light); ok {
		return t.provider, t.model, set.Light.Effort, true
	}
	if x.Account == "" || x.Model == "" || m.Open == nil {
		return nil, "", "", false
	}
	p, err := m.Open(x.Account)
	if err != nil {
		return nil, "", "", false
	}
	if !allowScript {
		if _, isScript := p.(*llm.Script); isScript {
			return nil, "", "", false
		}
	}
	return p, x.Model, "", true
}

func cleanTitle(raw string) string {
	s := strings.TrimSpace(raw)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.Trim(strings.TrimSpace(s), "\"'“”「」。.")
}

// retitle has the light model (or the session's own model) name a new session,
// in place of its first words.
func (m *Manager) retitle(x store.Session, first, text string) {
	p, model, effort, ok := m.lightOrSession(context.Background(), x, false)
	if !ok {
		return
	}
	sessionID := x.ID
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		reply, err := llm.Collect(p.Stream(ctx, llm.Request{Model: model, Effort: effort, MaxTokens: 64,
			Messages: []llm.Message{llm.Text(llm.User, titlePrompt+text)}}), nil)
		title := cleanTitle(reply.Message.PlainText())
		if err != nil || title == "" {
			return
		}
		cur, err := m.Store.GetSession(ctx, sessionID)
		if err != nil || cur.Title != first {
			return // renamed meanwhile
		}
		cur.Title = titleOf(title)
		if m.Store.UpdateSession(ctx, cur) == nil {
			cur, _ = m.Store.GetSession(ctx, sessionID)
			m.record(ctx, sessionID, proto.EventSession, m.sessionProto(ctx, cur))
		}
	}()
}

// Retitle summarizes a session's conversation into a fresh title.
func (m *Manager) Retitle(ctx context.Context, sessionID string) (proto.Session, error) {
	x, err := m.Store.GetSession(ctx, sessionID)
	if err != nil {
		return proto.Session{}, err
	}
	p, model, effort, ok := m.lightOrSession(ctx, x, true)
	if !ok {
		return proto.Session{}, errors.New("no model available to summarize title")
	}
	msgs, err := m.history(ctx, sessionID)
	if err != nil {
		return proto.Session{}, err
	}
	snippet := conversationSnippet(msgs, 3, 3, 2400)
	if snippet == "" {
		return proto.Session{}, errors.New("no conversation to summarize")
	}
	reply, err := llm.Collect(p.Stream(ctx, llm.Request{
		Model:     model,
		Effort:    effort,
		MaxTokens: 64,
		Messages:  []llm.Message{llm.Text(llm.User, retitlePrompt+snippet)},
	}), nil)
	if err != nil {
		return proto.Session{}, err
	}
	title := cleanTitle(reply.Message.PlainText())
	if title == "" {
		return proto.Session{}, errors.New("empty title returned")
	}
	x.Title = titleOf(title)
	if err := m.Store.UpdateSession(ctx, x); err != nil {
		return proto.Session{}, err
	}
	x, _ = m.Store.GetSession(ctx, sessionID)
	s := m.sessionProto(ctx, x)
	_ = m.record(ctx, sessionID, proto.EventSession, s)
	return s, nil
}

func (m *Manager) suggestNext(sessionID string) {
	if testing.Testing() && !m.SuggestInTests {
		return
	}
	set, _ := m.Settings(context.Background())
	if set.NoSuggestions {
		return
	}
	x, err := m.Store.GetSession(context.Background(), sessionID)
	if err != nil {
		return
	}
	p, model, effort, ok := m.lightOrSession(context.Background(), x, m.SuggestInTests)
	if !ok {
		return
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		msgs, err := m.history(ctx, sessionID)
		if err != nil {
			return
		}
		snippet := conversationSnippet(msgs, 0, 4, 1600)
		if snippet == "" {
			return
		}
		reply, err := llm.Collect(p.Stream(ctx, llm.Request{
			Model:     model,
			Effort:    effort,
			MaxTokens: 64,
			Messages:  []llm.Message{llm.Text(llm.User, suggestPrompt+snippet)},
		}), nil)
		if err != nil {
			return
		}
		sug := cleanTitle(reply.Message.PlainText())
		if sug == "" || strings.EqualFold(sug, "NONE") || len([]rune(sug)) > 120 {
			return
		}
		m.mu.Lock()
		active := m.runs[sessionID] != nil
		m.mu.Unlock()
		if active {
			return
		}
		b, err := json.Marshal(proto.Suggestion{Text: sug})
		if err != nil {
			return
		}
		m.publish(proto.Event{
			SessionID: sessionID,
			Type:      proto.EventSuggestion,
			At:        time.Now().UTC(),
			Data:      b,
		})
	}()
}

func conversationSnippet(msgs []llm.Message, head, tail, maxChars int) string {
	var lines []string
	for _, msg := range msgs {
		if msg.Role != llm.User && msg.Role != llm.Assistant {
			continue
		}
		text := strings.TrimSpace(msg.PlainText())
		if text == "" {
			continue
		}
		role := "User"
		if msg.Role == llm.Assistant {
			role = "Assistant"
		}
		lines = append(lines, role+": "+text)
	}
	if len(lines) == 0 {
		return ""
	}
	var picked []string
	if head > 0 && tail > 0 && len(lines) > head+tail {
		picked = append(picked, lines[:head]...)
		picked = append(picked, "...")
		picked = append(picked, lines[len(lines)-tail:]...)
	} else if head == 0 && tail > 0 && len(lines) > tail {
		picked = lines[len(lines)-tail:]
	} else {
		picked = lines
	}
	out := strings.Join(picked, "\n\n")
	if r := []rune(out); len(r) > maxChars {
		out = string(r[:maxChars])
	}
	return out
}
