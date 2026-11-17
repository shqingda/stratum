package session

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/shqingda/talos/kernel/store"
	"github.com/shqingda/talos/proto"
)

// Rewind returns a session to a prompt, the way Claude Code's /rewind does:
// both files and the conversation, only the conversation, or only files.
// The prompt itself leaves the log when the conversation is dropped, and
// is kept as the draft so it can be edited and sent again. Shell commands
// are not file changes, so what they did stays.
func (m *Manager) Rewind(ctx context.Context, q proto.Rewind) (proto.RewindResult, error) {
	out := proto.RewindResult{TurnSeq: q.TurnSeq, Action: q.Action, Paths: []string{}, Conflicts: []string{}}
	if q.TurnSeq <= 0 || (q.Action != "both" && q.Action != "conversation" && q.Action != "code") {
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
	if _, err := m.session(ctx, q.SessionID); err != nil {
		return out, err
	}
	evs, err := m.Store.Events(ctx, q.SessionID, q.TurnSeq-1, 1)
	if err != nil {
		return out, err
	}
	if len(evs) == 0 || evs[0].Seq != q.TurnSeq || evs[0].Type != proto.EventTurnStart {
		return out, proto.Errorf("not_found", "no prompt at that point")
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
