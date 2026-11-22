package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/shqingda/stratum/kernel/store"
	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/proto"
)

// Rewind returns a session to a prompt, the way Claude Code's /rewind does:
// both files and the conversation, only the conversation, only files, or
// summarizing from that prompt on. The prompt itself leaves the log when
// the conversation is dropped, and is kept as the draft so it can be edited
// and sent again. Shell commands are not file changes, so what they did stays.
func (m *Manager) Rewind(ctx context.Context, q proto.Rewind) (proto.RewindResult, error) {
	out := proto.RewindResult{TurnSeq: q.TurnSeq, Action: q.Action, Paths: []string{}, Conflicts: []string{}}
	if q.TurnSeq <= 0 || (q.Action != "both" && q.Action != "conversation" && q.Action != "code" && q.Action != "summarize" && q.Action != "summarize_before") {
		return out, proto.Errorf("bad_request", "rewind needs a prompt and an action")
	}
	if !m.lockIdle(ctx) {
		return out, proto.Errorf("busy", "wait for running turns before rewinding")
	}
	unlocked := false
	unlock := func() {
		if !unlocked {
			unlocked = true
			m.mu.Unlock()
		}
	}
	defer unlock()
	x, err := m.session(ctx, q.SessionID)
	if err != nil {
		return out, err
	}
	evs, err := m.Store.Events(ctx, q.SessionID, q.TurnSeq-1, 1)
	if err != nil {
		return out, err
	}
	if len(evs) == 0 || evs[0].Seq != q.TurnSeq || evs[0].Type != proto.EventTurnStart {
		return out, proto.Errorf("not_found", "no prompt at that point")
	}
	if q.Action == "summarize" {
		unlock()
		return m.rewindSummarize(ctx, x, q.TurnSeq)
	}
	if q.Action == "summarize_before" {
		unlock()
		return m.rewindSummarizeBefore(ctx, x, q.TurnSeq)
	}
	var start proto.TurnStart
	if err := json.Unmarshal(evs[0].Data, &start); err != nil {
		return out, err
	}
	var restored []proto.ChangeResult
	if q.Action != "conversation" {
		restored, err = m.rewindFiles(ctx, q.SessionID, q.TurnSeq, &out)
		if err != nil {
			return out, err
		}
	}
	drop := q.Action != "code" && len(out.Conflicts) == 0
	if drop {
		if err := m.Store.Truncate(ctx, q.SessionID, q.TurnSeq); err != nil {
			return out, err
		}
		if err := m.SetDraft(ctx, proto.DraftSet{SessionID: q.SessionID, Text: start.Text}); err != nil {
			return out, err
		}
		out.Prompt = start.Text
	}
	var published []store.Event
	for _, r := range restored {
		ev, err := m.Store.Append(context.WithoutCancel(ctx), q.SessionID, proto.EventChangesReverted, r)
		if err != nil {
			unlock()
			for _, e := range published {
				m.publish(toProto(e))
			}
			return out, err
		}
		published = append(published, ev)
	}
	unlock()
	for _, e := range published {
		m.publish(toProto(e))
	}
	return out, nil
}

// rewindFiles puts back file changes from the newest turn at or after seq
// back to that turn. It stops at the first file that no longer matches.
func (m *Manager) rewindFiles(ctx context.Context, sessionID string, seq int64, out *proto.RewindResult) ([]proto.ChangeResult, error) {
	turns, err := m.changes.List(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	var restored []proto.ChangeResult
	for i := len(turns) - 1; i >= 0; i-- {
		if turns[i].TurnSeq < seq {
			continue
		}
		result, err := m.changes.Revert(ctx, sessionID, turns[i].TurnSeq, "")
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return restored, err
		}
		out.Paths = append(out.Paths, result.Paths...)
		out.Conflicts = append(out.Conflicts, result.Conflicts...)
		if len(result.Paths) > 0 {
			restored = append(restored, proto.ChangeResult{TurnSeq: turns[i].TurnSeq, Paths: result.Paths, Conflicts: result.Conflicts})
		}
		if len(result.Conflicts) > 0 {
			break
		}
	}
	return restored, nil
}

func (m *Manager) rewindSummarize(ctx context.Context, x store.Session, turnSeq int64) (proto.RewindResult, error) {
	out := proto.RewindResult{TurnSeq: turnSeq, Action: "summarize", Paths: []string{}, Conflicts: []string{}}
	events, err := m.Store.Events(ctx, x.ID, turnSeq-1, 0, store.MessageEvent, proto.EventCompaction, proto.EventShell)
	if err != nil {
		return out, err
	}
	seg, err := eventsToMessages(events)
	if err != nil {
		return out, err
	}
	if len(seg) == 0 {
		return out, proto.Errorf("not_found", "no messages from that prompt to summarize")
	}
	if x.Account == "" || x.Model == "" {
		return out, proto.Errorf("bad_request", "the session has no model; pick one")
	}
	provider, err := m.Open(x.Account)
	if err != nil {
		return out, err
	}
	model, effort := x.Model, x.Effort
	if set, err := m.Settings(ctx); err == nil {
		if light, ok := m.role(set.Light); ok {
			provider, model, effort = light.provider, light.model, set.Light.Effort
		}
	}
	req := llm.Request{
		Model:     model,
		System:    systemPrompt(x, m.Sandboxed),
		Messages:  append(seg, llm.Text(llm.User, compactPrompt)),
		Effort:    effort,
		SessionID: x.ID,
	}
	reply, err := llm.Collect(provider.Stream(ctx, req), nil)
	if err != nil {
		return out, err
	}
	summary := strings.TrimSpace(reply.Message.PlainText())
	if summary == "" {
		return out, errEmptySummary
	}

	if !m.lockIdle(ctx) {
		return out, proto.Errorf("busy", "wait for running turns before rewinding")
	}
	evs, err := m.Store.Events(ctx, x.ID, turnSeq-1, 1)
	if err != nil {
		m.mu.Unlock()
		return out, err
	}
	if len(evs) == 0 || evs[0].Seq != turnSeq || evs[0].Type != proto.EventTurnStart {
		m.mu.Unlock()
		return out, proto.Errorf("not_found", "no prompt at that point")
	}
	if err := m.Store.Truncate(ctx, x.ID, turnSeq); err != nil {
		m.mu.Unlock()
		return out, err
	}
	ev, err := m.Store.Append(context.WithoutCancel(ctx), x.ID, proto.EventCompaction, proto.Compaction{Summary: summary, Asked: true, Partial: true})
	m.mu.Unlock()
	if err != nil {
		return out, err
	}
	m.publish(toProto(ev))
	return out, nil
}

func (m *Manager) rewindSummarizeBefore(ctx context.Context, x store.Session, turnSeq int64) (proto.RewindResult, error) {
	out := proto.RewindResult{TurnSeq: turnSeq, Action: "summarize_before", Paths: []string{}, Conflicts: []string{}}
	events, err := m.Store.Events(ctx, x.ID, 0, 0, store.MessageEvent, proto.EventCompaction, proto.EventShell)
	if err != nil {
		return out, err
	}
	var earlier []store.Event
	for _, e := range events {
		if e.Seq < turnSeq {
			earlier = append(earlier, e)
		}
	}
	seg, err := eventsToMessages(earlier)
	if err != nil {
		return out, err
	}
	if len(seg) == 0 {
		return out, proto.Errorf("not_found", "no earlier messages before that prompt to summarize")
	}
	if x.Account == "" || x.Model == "" {
		return out, proto.Errorf("bad_request", "the session has no model; pick one")
	}
	provider, err := m.Open(x.Account)
	if err != nil {
		return out, err
	}
	model, effort := x.Model, x.Effort
	if set, err := m.Settings(ctx); err == nil {
		if light, ok := m.role(set.Light); ok {
			provider, model, effort = light.provider, light.model, set.Light.Effort
		}
	}
	req := llm.Request{
		Model:     model,
		System:    systemPrompt(x, m.Sandboxed),
		Messages:  append(seg, llm.Text(llm.User, compactPrompt)),
		Effort:    effort,
		SessionID: x.ID,
	}
	reply, err := llm.Collect(provider.Stream(ctx, req), nil)
	if err != nil {
		return out, err
	}
	summary := strings.TrimSpace(reply.Message.PlainText())
	if summary == "" {
		return out, errEmptySummary
	}

	if !m.lockIdle(ctx) {
		return out, proto.Errorf("busy", "wait for running turns before rewinding")
	}
	evs, err := m.Store.Events(ctx, x.ID, turnSeq-1, 1)
	if err != nil {
		m.mu.Unlock()
		return out, err
	}
	if len(evs) == 0 || evs[0].Seq != turnSeq || evs[0].Type != proto.EventTurnStart {
		m.mu.Unlock()
		return out, proto.Errorf("not_found", "no prompt at that point")
	}
	ev, err := m.Store.Append(context.WithoutCancel(ctx), x.ID, proto.EventCompaction, proto.Compaction{Summary: summary, Asked: true, UpToSeq: turnSeq})
	m.mu.Unlock()
	if err != nil {
		return out, err
	}
	m.publish(toProto(ev))
	return out, nil
}
