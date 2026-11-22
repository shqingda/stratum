package session

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/shqingda/stratum/kernel/changes"
	"github.com/shqingda/stratum/kernel/store"
	"github.com/shqingda/stratum/proto"
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
	if !m.lockIdle(ctx) {
		return proto.ChangeResult{}, proto.Errorf("busy", "wait for running turns before restoring files")
	}
	x, err := m.session(ctx, q.SessionID)
	if err != nil {
		m.mu.Unlock()
		return proto.ChangeResult{}, err
	}
	path := q.Path
	if path != "" && !filepath.IsAbs(path) {
		path = filepath.Join(x.Cwd, path)
	}
	if path != "" {
		path = filepath.Clean(path)
	}
	var result changes.Result
	event := proto.EventChangesReverted
	if redo {
		result, err = m.changes.Redo(ctx, q.SessionID, q.TurnSeq, path)
		event = proto.EventChangesRedone
	} else {
		result, err = m.changes.Revert(ctx, q.SessionID, q.TurnSeq, path)
	}
	out := proto.ChangeResult{TurnSeq: q.TurnSeq, Path: path, Paths: result.Paths, Conflicts: result.Conflicts}
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

// ChangesRevertHunk puts back one hunk of a turn's change to a file, while
// no turn runs.
func (m *Manager) ChangesRevertHunk(ctx context.Context, q proto.HunkQuery) (proto.ChangeResult, error) {
	if q.TurnSeq <= 0 || q.Path == "" {
		return proto.ChangeResult{}, proto.Errorf("bad_request", "turnSeq and path are required")
	}
	if !m.lockIdle(ctx) {
		return proto.ChangeResult{}, proto.Errorf("busy", "wait for running turns before restoring files")
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
		return proto.ChangeResult{}, err
	}
	path := q.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(x.Cwd, path)
	}
	path = filepath.Clean(path)
	result, err := m.changes.RevertHunk(ctx, q.SessionID, q.TurnSeq, path, changes.Hunk{OldStart: q.OldStart, OldLen: q.OldLen, NewStart: q.NewStart, NewLen: q.NewLen})
	out := proto.ChangeResult{TurnSeq: q.TurnSeq, Path: path, Paths: result.Paths, Conflicts: result.Conflicts}
	if errors.Is(err, changes.ErrBadHunk) {
		return out, proto.Errorf("bad_request", "the hunk is not in the turn's diff")
	}
	if err != nil || len(out.Paths) == 0 {
		return out, changeError(err)
	}
	ev, err := m.Store.Append(context.WithoutCancel(ctx), q.SessionID, proto.EventChangesPatched, out)
	unlock() // publish takes the lock
	if err == nil {
		m.publish(toProto(ev))
	}
	return out, err
}

func changeError(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return proto.Errorf("not_found", "no recorded change")
	}
	return err
}

// lockIdle takes m.mu once no turn runs, and says whether it did. A turn
// tells the client it is over a moment before it lets go of the session,
// so an undo pressed at once waits that moment out rather than failing.
func (m *Manager) lockIdle(ctx context.Context) bool {
	for wait := 0; ; wait++ {
		m.mu.Lock()
		if len(m.runs) == 0 {
			return true
		}
		m.mu.Unlock()
		if wait == 50 {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(20 * time.Millisecond):
		}
	}
}
