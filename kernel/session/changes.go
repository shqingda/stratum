package session

import (
	"context"
	"errors"

	"github.com/shqingda/talos/kernel/changes"
	"github.com/shqingda/talos/kernel/store"
	"github.com/shqingda/talos/proto"
)

func (m *Manager) ChangesList(ctx context.Context, id string) ([]proto.ChangeTurn, error) {
	if _, err := m.session(ctx, id); err != nil {
		return nil, err
	}
	turns, err := m.changes.List(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]proto.ChangeTurn, 0, len(turns))
	for _, turn := range turns {
		t := proto.ChangeTurn{TurnSeq: turn.TurnSeq, Files: []proto.ChangeFile{}}
		for _, f := range turn.Files {
			t.Files = append(t.Files, proto.ChangeFile{Path: f.Path, Added: f.Added, Removed: f.Removed})
		}
		out = append(out, t)
	}
	return out, nil
}

func (m *Manager) ChangesDiff(ctx context.Context, q proto.ChangeQuery) (proto.ChangeDiff, error) {
	if q.TurnSeq <= 0 || q.Path == "" {
		return proto.ChangeDiff{}, proto.Errorf("bad_request", "turnSeq and path are required")
	}
	if _, err := m.session(ctx, q.SessionID); err != nil {
		return proto.ChangeDiff{}, err
	}
	d, err := m.changes.Diff(ctx, q.SessionID, q.TurnSeq, q.Path)
	return proto.ChangeDiff{Path: d.Path, Before: d.Before, After: d.After}, changeError(err)
}

// ChangesApply excludes live tool writes and other restores on this manager.
func (m *Manager) ChangesApply(ctx context.Context, q proto.ChangeQuery, redo bool) (proto.ChangeResult, error) {
	if q.TurnSeq <= 0 {
		return proto.ChangeResult{}, proto.Errorf("bad_request", "turnSeq must be positive")
	}
	m.mu.Lock()
	if len(m.runs) != 0 {
		m.mu.Unlock()
		return proto.ChangeResult{}, proto.Errorf("busy", "wait for running turns before restoring files")
	}
	if _, err := m.session(ctx, q.SessionID); err != nil {
		m.mu.Unlock()
		return proto.ChangeResult{}, err
	}
	var result changes.Result
	var err error
	event := proto.EventChangesReverted
	if redo {
		result, err = m.changes.Redo(ctx, q.SessionID, q.TurnSeq)
		event = proto.EventChangesRedone
	} else {
		result, err = m.changes.Revert(ctx, q.SessionID, q.TurnSeq)
	}
	out := proto.ChangeResult{TurnSeq: q.TurnSeq, Paths: result.Paths, Conflicts: result.Conflicts}
	if err != nil && len(out.Paths) == 0 && len(out.Conflicts) == 0 {
		m.mu.Unlock()
		return out, changeError(err)
	}
	applyErr := changeError(err)
	ev, err := m.Store.Append(context.WithoutCancel(ctx), q.SessionID, event, out)
	m.mu.Unlock()
	if err == nil {
		m.publish(toProto(ev))
	}
	return out, errors.Join(applyErr, err)
}

func changeError(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return proto.Errorf("not_found", "no recorded change")
	}
	return err
}
