// Package changes records file-tool snapshots and restores turns, files
// and hunks.
// Shell writes are not observed and can therefore cause restore conflicts.
package changes

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/shqingda/talos/kernel/store"
)

type Log struct {
	store *store.Store
	mu    sync.Mutex
}

func New(s *store.Store) *Log { return &Log{store: s} }

type File struct {
	Path           string
	Added, Removed int
}

type Turn struct {
	TurnSeq int64
	Files   []File
}

type Diff struct {
	Path          string
	Before, After *string
}

type Result struct {
	Paths, Conflicts []string
}

func (l *Log) Record(ctx context.Context, sessionID string, turnSeq int64, path string, before, after []byte) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if turnSeq <= 0 || path == "" {
		return errors.New("changes: positive turn sequence and path required")
	}
	x, err := l.store.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(x.Cwd, path)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return err
	}
	// The tool supplied its resolved target; never follow a replacement symlink.
	root, err := openParent(path)
	if err != nil {
		return err
	}
	defer root.Close()
	current, mode, err := readFile(root, filepath.Base(path))
	if err != nil {
		return err
	}
	if !same(current, after) {
		return errors.New("changes: file changed before snapshot was recorded")
	}
	// Deleted files have no remaining mode to inspect; restore them privately.
	return l.store.RecordFileChange(ctx, sessionID, turnSeq, path, before, after, uint32(mode))
}

func (l *Log) List(ctx context.Context, sessionID string) ([]Turn, error) {
	rows, err := l.store.FileChanges(ctx, sessionID, 0)
	if err != nil {
		return nil, err
	}
	turns := map[int64][]store.FileChange{}
	for _, row := range rows {
		turns[row.TurnSeq] = append(turns[row.TurnSeq], row)
	}
	out := []Turn{}
	for seq, rows := range turns {
		turn := Turn{TurnSeq: seq, Files: []File{}}
		for _, group := range groupFiles(rows) {
			added, removed := lineCounts(group[0].Before, group[len(group)-1].After)
			turn.Files = append(turn.Files, File{Path: group[0].Path, Added: added, Removed: removed})
		}
		sort.Slice(turn.Files, func(i, j int) bool { return turn.Files[i].Path < turn.Files[j].Path })
		out = append(out, turn)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TurnSeq < out[j].TurnSeq })
	return out, nil
}

func (l *Log) Diff(ctx context.Context, sessionID string, turnSeq int64, path string) (Diff, error) {
	if turnSeq <= 0 || path == "" {
		return Diff{}, errors.New("changes: positive turn sequence and path required")
	}
	x, err := l.store.GetSession(ctx, sessionID)
	if err != nil {
		return Diff{}, err
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(x.Cwd, path)
	}
	path = filepath.Clean(path)
	rows, err := l.store.FileChanges(ctx, sessionID, turnSeq)
	if err != nil {
		return Diff{}, err
	}
	for _, group := range groupFiles(rows) {
		if group[0].Path == path {
			return Diff{Path: group[0].Path, Before: text(group[0].Before), After: text(group[len(group)-1].After)}, nil
		}
	}
	return Diff{}, store.ErrNotFound
}

// Revert takes back what a turn changed: every file, or only path, an
// absolute path as List gives it.
func (l *Log) Revert(ctx context.Context, sessionID string, turnSeq int64, path string) (Result, error) {
	return l.apply(ctx, sessionID, turnSeq, path, true)
}

func (l *Log) Redo(ctx context.Context, sessionID string, turnSeq int64, path string) (Result, error) {
	return l.apply(ctx, sessionID, turnSeq, path, false)
}

func (l *Log) apply(ctx context.Context, sessionID string, turnSeq int64, path string, revert bool) (Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	result := Result{Paths: []string{}, Conflicts: []string{}}
	if turnSeq <= 0 {
		return result, errors.New("changes: positive turn sequence required")
	}
	rows, err := l.store.FileChanges(ctx, sessionID, turnSeq)
	if err != nil {
		return result, err
	}
	if len(rows) == 0 {
		return result, store.ErrNotFound
	}
	groups := groupFiles(rows)
	if path != "" {
		groups = slices.DeleteFunc(groups, func(g []store.FileChange) bool { return g[0].Path != path })
		if len(groups) == 0 {
			return result, store.ErrNotFound
		}
	}
	if revert {
		sort.Slice(groups, func(i, j int) bool { return groups[i][len(groups[i])-1].ID > groups[j][len(groups[j])-1].ID })
	}
	var failures []error
	for _, group := range groups {
		first, last := group[0], group[len(group)-1]
		consistent := true
		for i, row := range group {
			if row.Reverted != first.Reverted || i > 0 && !same(group[i-1].After, row.Before) {
				consistent = false
			}
		}
		if !consistent {
			result.Conflicts = append(result.Conflicts, first.Path)
			continue
		}
		before, after := first.Before, last.After
		if revert {
			before, after = after, before
		}
		root, err := openParent(first.Path)
		if err != nil {
			result.Conflicts = append(result.Conflicts, first.Path)
			continue
		}
		name := filepath.Base(first.Path)
		current, mode, err := readFile(root, name)
		if err == nil && first.Reverted == revert && same(current, after) {
			root.Close()
			continue
		}
		if err != nil || !same(current, before) {
			root.Close()
			result.Conflicts = append(result.Conflicts, first.Path)
			continue
		}
		if current == nil {
			mode = os.FileMode(last.Mode)
		}
		if err = replace(root, name, before, after, mode); err != nil {
			root.Close()
			if errors.Is(err, errConflict) {
				result.Conflicts = append(result.Conflicts, first.Path)
			} else {
				failures = append(failures, fmt.Errorf("%s: %w", first.Path, err))
			}
			continue
		}
		ids := make([]int64, len(group))
		for i, row := range group {
			ids[i] = row.ID
		}
		if err := l.store.SetFileChangesReverted(context.WithoutCancel(ctx), ids, revert); err != nil {
			undoErr := replace(root, name, after, before, mode)
			root.Close()
			if undoErr != nil {
				result.Paths = append(result.Paths, first.Path)
			}
			failures = append(failures, fmt.Errorf("%s: save restore state: %w", first.Path, errors.Join(err, undoErr)))
			continue
		}
		root.Close()
		result.Paths = append(result.Paths, first.Path)
	}
	return result, errors.Join(failures...)
}

// Hunk is a run of lines of a turn's diff, as "@@ -l,s +l,s @@" gives it.
type Hunk struct{ OldStart, OldLen, NewStart, NewLen int }

// ErrBadHunk means the hunk is not within the turn's diff of the file.
var ErrBadHunk = errors.New("changes: hunk out of range")

// RevertHunk puts back the old lines of one hunk of what a turn did to
// path. It is kept as a further change of the turn, so the turn still
// reverts and redoes whole.
func (l *Log) RevertHunk(ctx context.Context, sessionID string, turnSeq int64, path string, h Hunk) (Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	result := Result{Paths: []string{}, Conflicts: []string{}}
	rows, err := l.store.FileChanges(ctx, sessionID, turnSeq)
	if err != nil {
		return result, err
	}
	var group []store.FileChange
	for _, g := range groupFiles(rows) {
		if g[0].Path == path {
			group = g
		}
	}
	if group == nil {
		return result, store.ErrNotFound
	}
	first, last := group[0], group[len(group)-1]
	for i, row := range group {
		if row.Reverted || i > 0 && !same(group[i-1].After, row.Before) {
			result.Conflicts = append(result.Conflicts, path)
			return result, nil
		}
	}
	old, cur := splitLines(first.Before), splitLines(last.After)
	os0, ns0 := max(h.OldStart-1, 0), max(h.NewStart-1, 0)
	if h.OldLen < 0 || h.NewLen < 0 || os0+h.OldLen > len(old) || ns0+h.NewLen > len(cur) {
		return result, ErrBadHunk
	}
	next := []byte{}
	for _, part := range [][][]byte{cur[:ns0], old[os0 : os0+h.OldLen], cur[ns0+h.NewLen:]} {
		for _, line := range part {
			next = append(next, line...)
		}
	}
	if len(next) == 0 && first.Before == nil {
		next = nil // a new file with all of it taken back
	}
	root, err := openParent(path)
	if err != nil {
		result.Conflicts = append(result.Conflicts, path)
		return result, nil
	}
	defer root.Close()
	name := filepath.Base(path)
	current, mode, err := readFile(root, name)
	if err != nil || !same(current, last.After) {
		result.Conflicts = append(result.Conflicts, path)
		return result, nil
	}
	if current == nil {
		mode = os.FileMode(last.Mode)
	}
	if err := replace(root, name, last.After, next, mode); err != nil {
		if errors.Is(err, errConflict) {
			result.Conflicts = append(result.Conflicts, path)
			return result, nil
		}
		return result, err
	}
	if err := l.store.RecordFileChange(context.WithoutCancel(ctx), sessionID, turnSeq, path, last.After, next, uint32(mode)); err != nil {
		return result, errors.Join(err, replace(root, name, next, last.After, mode))
	}
	result.Paths = append(result.Paths, path)
	return result, nil
}

func splitLines(b []byte) [][]byte {
	if len(b) == 0 {
		return nil
	}
	lines := bytes.SplitAfter(b, []byte("\n"))
	if len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func groupFiles(rows []store.FileChange) [][]store.FileChange {
	indexes := map[string]int{}
	groups := [][]store.FileChange{}
	for _, row := range rows {
		i, ok := indexes[row.Path]
		if !ok {
			i = len(groups)
			indexes[row.Path] = i
			groups = append(groups, nil)
		}
		groups[i] = append(groups[i], row)
	}
	return groups
}

func text(b []byte) *string {
	if b == nil {
		return nil
	}
	s := string(b)
	return &s
}

func same(a, b []byte) bool {
	return (a == nil) == (b == nil) && sha256.Sum256(a) == sha256.Sum256(b)
}

var errConflict = errors.New("changes: file changed during restore")

// Open each parent through a pinned directory and refuse substituted symlinks.
func openParent(path string) (*os.Root, error) {
	root, err := os.OpenRoot(string(filepath.Separator))
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Dir(path), string(filepath.Separator)), string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		next, err := root.OpenRoot(part)
		if err != nil {
			root.Close()
			return nil, err
		}
		info, err := root.Lstat(part)
		opened, statErr := next.Stat(".")
		root.Close()
		if err != nil || statErr != nil || !info.IsDir() || !os.SameFile(info, opened) {
			next.Close()
			return nil, errConflict
		}
		root = next
	}
	return root, nil
}

func readFile(root *os.Root, name string) ([]byte, os.FileMode, error) {
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0o600, nil
	}
	if err != nil {
		return nil, 0, err
	}
	if !info.Mode().IsRegular() {
		return nil, 0, errConflict
	}
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	if !os.SameFile(info, opened) {
		return nil, 0, errConflict
	}
	data, err := io.ReadAll(f)
	if data == nil && err == nil {
		data = []byte{}
	}
	return data, info.Mode().Perm(), err
}

func replace(root *os.Root, name string, before, after []byte, mode os.FileMode) error {
	tmp := ".talos-restore-" + rand.Text()
	if after != nil {
		f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		defer root.Remove(tmp)
		_, err = f.Write(after)
		if err == nil {
			err = f.Chmod(mode.Perm())
		}
		if err == nil {
			err = f.Sync()
		}
		err = errors.Join(err, f.Close())
		if err != nil {
			return err
		}
	}
	current, _, err := readFile(root, name)
	if err != nil || !same(current, before) {
		return errConflict
	}
	if after == nil {
		if current == nil {
			return nil
		}
		return root.Remove(name)
	}
	if before == nil {
		if err := root.Link(tmp, name); errors.Is(err, os.ErrExist) {
			return errConflict
		} else {
			return err
		}
	}
	// shortcut: external writers can race the final hash check; add shared file locks if simultaneous edits must be serialized.
	return root.Rename(tmp, name)
}

// lineCounts uses a single LCS row, keeping memory linear in file length.
func lineCounts(before, after []byte) (added, removed int) {
	lines := func(b []byte) []string {
		if len(b) == 0 {
			return nil
		}
		s := strings.SplitAfter(string(b), "\n")
		if s[len(s)-1] == "" {
			s = s[:len(s)-1]
		}
		return s
	}
	a, b := lines(before), lines(after)
	for len(a) > 0 && len(b) > 0 && a[0] == b[0] {
		a, b = a[1:], b[1:]
	}
	for len(a) > 0 && len(b) > 0 && a[len(a)-1] == b[len(b)-1] {
		a, b = a[:len(a)-1], b[:len(b)-1]
	}
	beforeLines, afterLines := len(a), len(b)
	if len(a) < len(b) {
		a, b = b, a
	}
	row := make([]int, len(b)+1)
	for _, left := range a {
		diag := 0
		for j, right := range b {
			old := row[j+1]
			if left == right {
				row[j+1] = diag + 1
			} else {
				row[j+1] = max(row[j], row[j+1])
			}
			diag = old
		}
	}
	return afterLines - row[len(b)], beforeLines - row[len(b)]
}
