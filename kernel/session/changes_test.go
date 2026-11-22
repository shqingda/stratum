package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shqingda/stratum/kernel/store"
	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/proto"
)

func changesManager(t *testing.T, script *llm.Script) (*Manager, proto.Session) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Error(err)
		}
	})
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := New(st, func(string) (llm.Provider, error) { return script, nil })
	t.Cleanup(m.Close)
	s, err := m.Create(context.Background(), proto.SessionCreate{Cwd: root, Title: "changes test", Account: "script", Model: "script", Mode: "ask"})
	if err != nil {
		t.Fatal(err)
	}
	return m, s
}

func requireChangeError(t *testing.T, err error, code string) {
	t.Helper()
	var pe *proto.Error
	if !errors.As(err, &pe) || pe.Code != code {
		t.Fatalf("got %v, want protocol error %s", err, code)
	}
}

func recordTestChange(t *testing.T, m *Manager, s proto.Session) proto.ChangeQuery {
	t.Helper()
	ctx := context.Background()
	start, err := m.Store.Append(ctx, s.ID, proto.EventTurnStart, proto.TurnStart{Text: "recorded turn"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Cwd, "file")
	if err := os.WriteFile(path, []byte("after"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.changes.Record(ctx, s.ID, start.Seq, path, []byte("before"), []byte("after")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Store.Append(ctx, s.ID, proto.EventTurnEnd, proto.TurnEnd{}); err != nil {
		t.Fatal(err)
	}
	return proto.ChangeQuery{SessionID: s.ID, TurnSeq: start.Seq, Path: path}
}

func TestChangesApplyRejectsActiveTurn(t *testing.T) {
	for _, tt := range []struct {
		name  string
		redo  bool
		other bool
	}{
		{"revert/same-session", false, false},
		{"redo/same-session", true, false},
		{"revert/other-session", false, true},
		{"redo/other-session", true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			script := &llm.Script{Turns: []llm.Turn{{Calls: []llm.ToolCall{llm.Call("write", "write_file", map[string]string{"path": "file", "content": "live turn"})}}}}
			m, s := changesManager(t, script)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			q := recordTestChange(t, m, s)
			want := "after"
			if tt.redo {
				if _, err := m.ChangesApply(ctx, q, false); err != nil {
					t.Fatal(err)
				}
				want = "before"
			}
			activeID := s.ID
			if tt.other {
				other, err := m.Create(ctx, proto.SessionCreate{Cwd: s.Cwd, Title: "other turn", Account: "script", Model: "script", Mode: "ask"})
				if err != nil {
					t.Fatal(err)
				}
				activeID = other.ID
			}
			approvals := make(chan proto.Event, 1)
			unsubscribe := m.Subscribe(func(e proto.Event) {
				if e.SessionID == activeID && e.Type == proto.EventApprovalRequest {
					approvals <- e
				}
			})
			defer unsubscribe()
			if _, err := m.Prompt(ctx, proto.Prompt{SessionID: activeID, Text: "update file"}, ""); err != nil {
				t.Fatal(err)
			}
			select {
			case <-approvals:
			case <-ctx.Done():
				t.Fatal("script did not reach write approval")
			}
			list, err := m.List(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, x := range list {
				if want := map[bool]string{true: "wait"}[x.ID == activeID]; x.State != want {
					t.Fatalf("session %s state %q, want %q", x.ID, x.State, want)
				}
			}
			_, err = m.ChangesApply(ctx, q, tt.redo)
			requireChangeError(t, err, "busy")
			if got, err := os.ReadFile(q.Path); err != nil || string(got) != want {
				t.Fatalf("busy restore changed the file: %q, %v", got, err)
			}
			m.Cancel(activeID)
			m.Close()
			if got, err := os.ReadFile(q.Path); err != nil || string(got) != want {
				t.Fatalf("cancelled unapproved write changed the file: %q, %v", got, err)
			}
			if snap, err := m.Get(ctx, activeID); err != nil || snap.Running || len(snap.Approvals) != 0 {
				t.Fatalf("turn did not finish cleanly: %+v, %v", snap, err)
			}
		})
	}
}

func TestChangesRequestValidation(t *testing.T) {
	m, s := changesManager(t, &llm.Script{})
	ctx := context.Background()
	for _, turnSeq := range []int64{0, -1} {
		q := proto.ChangeQuery{SessionID: s.ID, TurnSeq: turnSeq, Path: filepath.Join(s.Cwd, "file")}
		_, err := m.ChangesDiff(ctx, q)
		requireChangeError(t, err, "bad_request")
		for _, redo := range []bool{false, true} {
			_, err := m.ChangesApply(ctx, q, redo)
			requireChangeError(t, err, "bad_request")
		}
	}
	if _, err := m.ChangesDiff(ctx, proto.ChangeQuery{SessionID: s.ID, TurnSeq: 1}); err == nil {
		t.Fatal("diff accepted an empty path")
	} else {
		requireChangeError(t, err, "bad_request")
	}
	_, err := m.ChangesList(ctx, "missing-session")
	requireChangeError(t, err, "not_found")
	q := proto.ChangeQuery{SessionID: "missing-session", TurnSeq: 1, Path: filepath.Join(s.Cwd, "file")}
	_, err = m.ChangesDiff(ctx, q)
	requireChangeError(t, err, "not_found")
	for _, redo := range []bool{false, true} {
		_, err := m.ChangesApply(ctx, q, redo)
		requireChangeError(t, err, "not_found")
	}
	q.SessionID, q.TurnSeq = s.ID, 999999
	_, err = m.ChangesDiff(ctx, q)
	requireChangeError(t, err, "not_found")
	for _, redo := range []bool{false, true} {
		_, err := m.ChangesApply(ctx, q, redo)
		requireChangeError(t, err, "not_found")
	}
}

func TestChangesDiffReadsOnlyRecordedSnapshots(t *testing.T) {
	m, s := changesManager(t, &llm.Script{})
	ctx := context.Background()
	q := recordTestChange(t, m, s)
	if err := os.Remove(q.Path); err != nil {
		t.Fatal(err)
	}
	rev, err := m.Store.Revision(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	diff, err := m.ChangesDiff(ctx, q)
	if err != nil || diff.Path != q.Path || diff.Before == nil || *diff.Before != "before" || diff.After == nil || *diff.After != "after" {
		t.Fatalf("diff did not use stored snapshots: %+v, %v", diff, err)
	}
	if _, err := os.Stat(q.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("diff changed the missing recorded path: %v", err)
	}
	t.Run("unrecorded-symlink-alias", func(t *testing.T) {
		alias := filepath.Join(t.TempDir(), "alias")
		if err := os.Symlink(s.Cwd, alias); err != nil {
			t.Fatal(err)
		}
		aliasQuery := q
		aliasQuery.Path = filepath.Join(alias, filepath.Base(q.Path))
		aliasDiff, err := m.ChangesDiff(ctx, aliasQuery)
		requireChangeError(t, err, "not_found")
		if aliasDiff.Before != nil || aliasDiff.After != nil {
			t.Fatal("diff resolved an unrecorded disk alias to a recorded snapshot")
		}
	})
	outside := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(outside, []byte("unrecorded private content"), 0600); err != nil {
		t.Fatal(err)
	}
	q.Path = outside
	diff, err = m.ChangesDiff(ctx, q)
	requireChangeError(t, err, "not_found")
	if diff.Before != nil || diff.After != nil {
		t.Fatalf("diff exposed unrecorded file contents: %+v", diff)
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "unrecorded private content" {
		t.Fatalf("diff changed an unrecorded path: %q, %v", got, err)
	}
	if afterRev, err := m.Store.Revision(ctx, s.ID); err != nil || afterRev != rev {
		t.Fatalf("read-only diff changed event revision: before %d, after %d, %v", rev, afterRev, err)
	}
}

func TestChangesRevertHunk(t *testing.T) {
	m, s := changesManager(t, &llm.Script{})
	ctx := context.Background()
	q := recordTestChange(t, m, s)
	events := make(chan proto.Event, 4)
	defer m.Subscribe(func(e proto.Event) {
		if e.Type == proto.EventChangesPatched {
			events <- e
		}
	})()
	if _, err := m.ChangesRevertHunk(ctx, proto.HunkQuery{ChangeQuery: q, OldStart: 1, OldLen: 1, NewStart: 1, NewLen: 2}); err == nil {
		t.Fatal("a hunk past the end was taken")
	}
	q.Path = "file" // relative to the session's folder
	r, err := m.ChangesRevertHunk(ctx, proto.HunkQuery{ChangeQuery: q, OldStart: 1, OldLen: 1, NewStart: 1, NewLen: 1})
	if err != nil || len(r.Paths) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	if got, _ := os.ReadFile(filepath.Join(s.Cwd, "file")); string(got) != "before" {
		t.Fatalf("file holds %q", got)
	}
	select {
	case <-events:
	default:
		t.Fatal("no changes.patched event")
	}
}
