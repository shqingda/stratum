package changes

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shqingda/talos/kernel/store"
)

func setup(t *testing.T) (*store.Store, *Log, string, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(t.TempDir(), "changes.db")
	s, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.CreateSession(context.Background(), store.Session{ID: "s", Cwd: root}); err != nil {
		t.Fatal(err)
	}
	return s, New(s), root, db
}

func put(t *testing.T, path string, data []byte) {
	t.Helper()
	if data == nil {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
	} else if err := os.WriteFile(path, data, 0o640); err != nil {
		t.Fatal(err)
	}
}

func record(t *testing.T, l *Log, turn int64, path string, before, after []byte) {
	t.Helper()
	put(t, path, after)
	if err := l.Record(context.Background(), "s", turn, path, before, after); err != nil {
		t.Fatal(err)
	}
}

func checkFile(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if want == nil {
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s should be absent: %v", path, err)
		}
	} else if err != nil || string(got) != string(want) {
		t.Fatalf("%s = %q, want %q: %v", path, got, want, err)
	}
}

func TestCreateModifyDeleteRevertRedo(t *testing.T) {
	_, l, root, _ := setup(t)
	cases := []struct {
		name          string
		before, after []byte
	}{
		{"create", nil, []byte("created\n")}, {"create-empty", nil, []byte{}},
		{"modify", []byte("before\n"), []byte("after\n")}, {"delete", []byte("deleted\n"), nil},
		{"empty-before", []byte{}, []byte("filled\n")}, {"empty-after", []byte("filled\n"), []byte{}},
	}
	for _, tc := range cases {
		path := filepath.Join(root, tc.name)
		put(t, path, tc.before)
		record(t, l, 7, path, tc.before, tc.after)
		d, err := l.Diff(context.Background(), "s", 7, tc.name)
		if err != nil || !reflect.DeepEqual(d.Before, text(tc.before)) || !reflect.DeepEqual(d.After, text(tc.after)) {
			t.Fatalf("diff %s: %+v %v", tc.name, d, err)
		}
	}
	for _, revert := range []bool{true, false} {
		var r Result
		var err error
		if revert {
			r, err = l.Revert(context.Background(), "s", 7, "")
		} else {
			r, err = l.Redo(context.Background(), "s", 7, "")
		}
		if err != nil || len(r.Paths) != len(cases) || len(r.Conflicts) != 0 {
			t.Fatalf("restore: %+v %v", r, err)
		}
		for _, tc := range cases {
			want := tc.after
			if revert {
				want = tc.before
			}
			checkFile(t, filepath.Join(root, tc.name), want)
		}
		if info, err := os.Stat(filepath.Join(root, "modify")); err != nil || info.Mode().Perm() != 0o640 {
			t.Fatalf("mode changed: %v %v", info, err)
		}
		if revert {
			r, err = l.Revert(context.Background(), "s", 7, "")
		} else {
			r, err = l.Redo(context.Background(), "s", 7, "")
		}
		if err != nil || len(r.Paths) != 0 || len(r.Conflicts) != 0 {
			t.Fatalf("repeat restore not idempotent: %+v %v", r, err)
		}
	}
}

func TestConflictRetryAndRestart(t *testing.T) {
	s, l, root, db := setup(t)
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	for _, path := range []string{a, b} {
		record(t, l, 9, path, []byte("old"), []byte("new"))
	}
	put(t, a, []byte("manual"))
	r, err := l.Revert(context.Background(), "s", 9, "")
	if err != nil || !reflect.DeepEqual(r.Paths, []string{b}) || !reflect.DeepEqual(r.Conflicts, []string{a}) {
		t.Fatalf("partial revert: %+v %v", r, err)
	}
	checkFile(t, a, []byte("manual"))
	checkFile(t, b, []byte("old"))
	r, err = l.Redo(context.Background(), "s", 9, "")
	if err != nil || !reflect.DeepEqual(r.Paths, []string{b}) || !reflect.DeepEqual(r.Conflicts, []string{a}) {
		t.Fatalf("redo must recheck files skipped by revert: %+v %v", r, err)
	}
	r, err = l.Revert(context.Background(), "s", 9, "")
	if err != nil || !reflect.DeepEqual(r.Paths, []string{b}) || !reflect.DeepEqual(r.Conflicts, []string{a}) {
		t.Fatalf("restore partially reverted state: %+v %v", r, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	l = New(s)
	put(t, a, []byte("new"))
	r, err = l.Revert(context.Background(), "s", 9, "")
	if err != nil || !reflect.DeepEqual(r.Paths, []string{a}) || len(r.Conflicts) != 0 {
		t.Fatalf("retry: %+v %v", r, err)
	}
	put(t, b, []byte("manual redo"))
	r, err = l.Redo(context.Background(), "s", 9, "")
	if err != nil || !reflect.DeepEqual(r.Paths, []string{a}) || !reflect.DeepEqual(r.Conflicts, []string{b}) {
		t.Fatalf("partial redo: %+v %v", r, err)
	}
	checkFile(t, a, []byte("new"))
	checkFile(t, b, []byte("manual redo"))
	put(t, b, []byte("old"))
	r, err = l.Redo(context.Background(), "s", 9, "")
	if err != nil || !reflect.DeepEqual(r.Paths, []string{b}) || len(r.Conflicts) != 0 {
		t.Fatalf("redo retry: %+v %v", r, err)
	}
}

func TestRepeatedFileAndBrokenChain(t *testing.T) {
	_, l, root, _ := setup(t)
	path := filepath.Join(root, "file")
	record(t, l, 3, path, []byte("a\n"), []byte("b\n"))
	record(t, l, 3, path, []byte("b\n"), []byte("c\n"))
	turns, err := l.List(context.Background(), "s")
	if err != nil || len(turns) != 1 || len(turns[0].Files) != 1 || turns[0].Files[0].Added != 1 || turns[0].Files[0].Removed != 1 {
		t.Fatalf("net changes: %+v %v", turns, err)
	}
	r, err := l.Revert(context.Background(), "s", 3, "")
	if err != nil || len(r.Paths) != 1 || len(r.Conflicts) != 0 {
		t.Fatalf("multi-write revert: %+v %v", r, err)
	}
	checkFile(t, path, []byte("a\n"))
	r, err = l.Redo(context.Background(), "s", 3, "")
	if err != nil || len(r.Paths) != 1 || len(r.Conflicts) != 0 {
		t.Fatalf("multi-write redo: %+v %v", r, err)
	}
	checkFile(t, path, []byte("c\n"))
	other := filepath.Join(root, "broken")
	record(t, l, 4, other, []byte("original"), []byte("first"))
	record(t, l, 4, other, []byte("manual middle"), []byte("last"))
	r, err = l.Revert(context.Background(), "s", 4, "")
	if err != nil || len(r.Paths) != 0 || !reflect.DeepEqual(r.Conflicts, []string{other}) {
		t.Fatalf("broken chain partially restored: %+v %v", r, err)
	}
	checkFile(t, other, []byte("last"))
}

func TestRestoreRejectsSymlinks(t *testing.T) {
	for _, kind := range []string{"file", "parent"} {
		t.Run(kind, func(t *testing.T) {
			_, l, root, _ := setup(t)
			parent := filepath.Join(root, "dir")
			if err := os.Mkdir(parent, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(parent, "file")
			record(t, l, 1, path, []byte("before"), []byte("after"))
			outside := t.TempDir()
			target := filepath.Join(outside, "file")
			put(t, target, []byte("after"))
			if kind == "file" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Rename(parent, parent+"-old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, parent); err != nil {
					t.Fatal(err)
				}
			}
			r, err := l.Revert(context.Background(), "s", 1, "")
			if err != nil || len(r.Paths) != 0 || !reflect.DeepEqual(r.Conflicts, []string{path}) {
				t.Fatalf("symlink restore: %+v %v", r, err)
			}
			checkFile(t, target, []byte("after"))
		})
	}
}

func TestRecordRejectsChangedTarget(t *testing.T) {
	for _, kind := range []string{"parent-symlink", "file-symlink", "changed-content", "missing-empty-file"} {
		t.Run(kind, func(t *testing.T) {
			s, l, root, _ := setup(t)
			parent := filepath.Join(root, "dir")
			if err := os.Mkdir(parent, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(parent, "file")
			after := []byte("after")
			put(t, path, after)
			outside := t.TempDir()
			target := filepath.Join(outside, "file")
			put(t, target, after)
			switch kind {
			case "parent-symlink":
				if err := os.Rename(parent, parent+"-old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, parent); err != nil {
					t.Fatal(err)
				}
			case "file-symlink":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			case "changed-content":
				put(t, path, []byte("manual"))
			case "missing-empty-file":
				after = []byte{}
				put(t, path, nil)
			}
			if err := l.Record(context.Background(), "s", 1, path, []byte("before"), after); err == nil {
				t.Fatal("record accepted a substituted target")
			}
			rows, err := s.FileChanges(context.Background(), "s", 0)
			if err != nil || len(rows) != 0 {
				t.Fatalf("rejected target left a change: %+v %v", rows, err)
			}
			if _, err := l.Revert(context.Background(), "s", 1, ""); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("rejected target became restorable: %v", err)
			}
			checkFile(t, target, []byte("after"))
		})
	}
}

func TestLineCounts(t *testing.T) {
	for _, tc := range []struct {
		before, after  string
		added, removed int
	}{
		{"", "", 0, 0}, {"", "a\nb\n", 2, 0}, {"a\nb\n", "", 0, 2},
		{"a\nb\nc\n", "a\nx\nc\n", 1, 1}, {"a\n", "a", 1, 1},
		{"a\nb\n", "a\nx\nb\ny\n", 2, 0}, {"a\nb\nc", "a\nc", 0, 1},
	} {
		a, r := lineCounts([]byte(tc.before), []byte(tc.after))
		if a != tc.added || r != tc.removed {
			t.Errorf("%q -> %q = +%d -%d, want +%d -%d", tc.before, tc.after, a, r, tc.added, tc.removed)
		}
	}
	large := strings.Repeat("same\n", 10000)
	if a, r := lineCounts([]byte(large), []byte(large+"new\n")); a != 1 || r != 0 {
		t.Fatalf("large common prefix = +%d -%d", a, r)
	}
}

func TestRevertOneFile(t *testing.T) {
	_, l, root, _ := setup(t)
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	record(t, l, 3, a, []byte("a0"), []byte("a1"))
	record(t, l, 3, b, []byte("b0"), []byte("b1"))
	r, err := l.Revert(context.Background(), "s", 3, a)
	if err != nil || !reflect.DeepEqual(r.Paths, []string{a}) {
		t.Fatalf("%+v %v", r, err)
	}
	checkFile(t, a, []byte("a0"))
	checkFile(t, b, []byte("b1"))
	// The whole turn then takes back only what is left.
	if r, err = l.Revert(context.Background(), "s", 3, ""); err != nil || !reflect.DeepEqual(r.Paths, []string{b}) {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err = l.Redo(context.Background(), "s", 3, filepath.Join(root, "c")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a file the turn did not change: %v", err)
	}
	if _, err = l.Redo(context.Background(), "s", 3, a); err != nil {
		t.Fatal(err)
	}
	checkFile(t, a, []byte("a1"))
	checkFile(t, b, []byte("b0"))
}

func TestRevertHunk(t *testing.T) {
	_, l, root, _ := setup(t)
	ctx := context.Background()
	f := filepath.Join(root, "f")
	before := []byte("1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n")
	after := []byte("1\ntwo\n3\n4\n5\n6\n7\n8\n9\n10\neleven\n12\n13\n")
	record(t, l, 3, f, before, after)
	// The second hunk: "@@ -8,5 +8,6 @@" turns 11 into eleven and adds 13.
	if _, err := l.RevertHunk(ctx, "s", 3, f, Hunk{OldStart: 8, OldLen: 5, NewStart: 8, NewLen: 7}); !errors.Is(err, ErrBadHunk) {
		t.Fatalf("a hunk past the end: %v", err)
	}
	r, err := l.RevertHunk(ctx, "s", 3, f, Hunk{OldStart: 8, OldLen: 5, NewStart: 8, NewLen: 6})
	if err != nil || !reflect.DeepEqual(r.Paths, []string{f}) {
		t.Fatalf("%+v %v", r, err)
	}
	checkFile(t, f, []byte("1\ntwo\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n"))
	turns, _ := l.List(ctx, "s")
	if got := turns[0].Files[0]; got.Added != 1 || got.Removed != 1 {
		t.Fatalf("after the hunk: %+v", got)
	}
	// The turn still reverts and redoes whole.
	if _, err := l.Revert(ctx, "s", 3, ""); err != nil {
		t.Fatal(err)
	}
	checkFile(t, f, before)
	if _, err := l.Redo(ctx, "s", 3, ""); err != nil {
		t.Fatal(err)
	}
	checkFile(t, f, []byte("1\ntwo\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n"))
	// A file changed since is a conflict, left alone.
	os.WriteFile(f, []byte("mine\n"), 0o644)
	if r, err := l.RevertHunk(ctx, "s", 3, f, Hunk{OldStart: 1, OldLen: 5, NewStart: 1, NewLen: 5}); err != nil || len(r.Conflicts) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	checkFile(t, f, []byte("mine\n"))

	// A new file with its only hunk taken back is gone.
	n := filepath.Join(root, "n")
	record(t, l, 4, n, nil, []byte("x\ny\n"))
	if _, err := l.RevertHunk(ctx, "s", 4, n, Hunk{NewStart: 1, NewLen: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(n); !os.IsNotExist(err) {
		t.Fatalf("new file left: %v", err)
	}
}
