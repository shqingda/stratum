package session

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shqingda/stratum/kernel/store"
	"github.com/shqingda/stratum/proto"
)

type worktreeMeta struct {
	Repo   string `json:"repo"`
	Path   string `json:"path"`
	Branch string `json:"branch"`
}

func worktreeKey(sessionID string) string { return "worktree:" + sessionID }

func (m *Manager) worktreeDir() string {
	if m.WorktreeDir != "" {
		return m.WorktreeDir
	}
	return filepath.Join(os.TempDir(), "stratum-worktrees")
}

func (m *Manager) worktreeMetaOf(ctx context.Context, sessionID string) (worktreeMeta, bool) {
	b, err := m.Store.Setting(ctx, worktreeKey(sessionID))
	if err != nil || len(b) == 0 {
		return worktreeMeta{}, false
	}
	var meta worktreeMeta
	if json.Unmarshal(b, &meta) != nil || meta.Path == "" || meta.Repo == "" {
		return worktreeMeta{}, false
	}
	return meta, true
}

func (m *Manager) sessionProto(ctx context.Context, x store.Session) proto.Session {
	s := toSession(x)
	if meta, ok := m.worktreeMetaOf(ctx, x.ID); ok {
		s.Worktree = true
		s.Repo = meta.Repo
	}
	return s
}

func gitExec(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir, "--no-optional-locks"}, args...)...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (m *Manager) hasStarted(ctx context.Context, sessionID string) bool {
	evs, err := m.Store.Events(ctx, sessionID, 0, 1, proto.EventTurnStart)
	return err == nil && len(evs) > 0
}

func (m *Manager) isRunning(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.runs[sessionID]
	return ok
}

func (m *Manager) enableWorktree(ctx context.Context, x *store.Session) error {
	if _, ok := m.worktreeMetaOf(ctx, x.ID); ok {
		return nil
	}
	started := m.hasStarted(ctx, x.ID)
	if started && m.isRunning(x.ID) {
		return proto.Errorf("bad_request", "cannot hand off to worktree while a turn is running")
	}
	repoTop, err := gitExec(ctx, x.Cwd, "rev-parse", "--show-toplevel")
	if err != nil || repoTop == "" {
		return proto.Errorf("bad_request", "not in a git repository")
	}
	if _, err := gitExec(ctx, repoTop, "rev-parse", "--verify", "-q", "HEAD"); err != nil {
		return proto.Errorf("bad_request", "repository has no commits yet")
	}
	idPrefix := x.ID
	if len(idPrefix) > 12 {
		idPrefix = idPrefix[:12]
	}
	brSuffix := x.ID
	if len(brSuffix) > 8 {
		brSuffix = brSuffix[:8]
	}
	wtBase := filepath.Join(m.worktreeDir(), idPrefix)
	wtPath := filepath.Join(wtBase, filepath.Base(repoTop))
	branch := "stratum/" + brSuffix
	if err := os.MkdirAll(wtBase, 0o755); err != nil {
		return err
	}
	if out, err := gitExec(ctx, repoTop, "worktree", "add", "-b", branch, wtPath, "HEAD"); err != nil {
		_ = os.RemoveAll(wtBase)
		return proto.Errorf("internal", "git worktree add: %s", out)
	}
	copyWorktreeIncludes(repoTop, wtPath)
	if started {
		if err := migrateWorkingTree(ctx, repoTop, wtPath); err != nil {
			_, _ = gitExec(ctx, repoTop, "worktree", "remove", "--force", wtPath)
			_, _ = gitExec(ctx, repoTop, "branch", "-D", branch)
			_ = os.RemoveAll(wtBase)
			return err
		}
	}
	meta := worktreeMeta{Repo: repoTop, Path: wtPath, Branch: branch}
	b, _ := json.Marshal(meta)
	if err := m.Store.SetSetting(ctx, worktreeKey(x.ID), b); err != nil {
		_, _ = gitExec(ctx, repoTop, "worktree", "remove", "--force", wtPath)
		_, _ = gitExec(ctx, repoTop, "branch", "-D", branch)
		_ = os.RemoveAll(wtBase)
		return err
	}
	x.Cwd = wtPath
	return nil
}

func (m *Manager) disableWorktree(ctx context.Context, x *store.Session, restoreCwd bool) error {
	meta, ok := m.worktreeMetaOf(ctx, x.ID)
	if !ok {
		return nil
	}
	if restoreCwd && m.hasStarted(ctx, x.ID) {
		if m.isRunning(x.ID) {
			return proto.Errorf("bad_request", "cannot hand off to local while a turn is running")
		}
		if err := handoffWorktreeToLocal(ctx, meta); err != nil {
			return err
		}
	}
	_, _ = gitExec(ctx, meta.Repo, "worktree", "remove", "--force", meta.Path)
	_ = os.RemoveAll(meta.Path)
	_, _ = gitExec(ctx, meta.Repo, "worktree", "prune")
	if meta.Branch != "" {
		_, _ = gitExec(ctx, meta.Repo, "branch", "-D", meta.Branch)
	}
	_ = os.Remove(filepath.Dir(meta.Path))
	_ = m.Store.DeleteSetting(ctx, worktreeKey(x.ID))
	if restoreCwd {
		x.Cwd = meta.Repo
	}
	return nil
}

// copyWorktreeIncludes copies files matched by .worktreeinclude in repoTop
// into wtPath, following Codex / Claude Code convention.
func copyWorktreeIncludes(repoTop, wtPath string) {
	b, err := os.ReadFile(filepath.Join(repoTop, ".worktreeinclude"))
	if err != nil {
		return
	}
	for _, raw := range strings.Split(string(b), "\n") {
		pat := strings.TrimSpace(raw)
		if pat == "" || strings.HasPrefix(pat, "#") {
			continue
		}
		pat = strings.TrimPrefix(pat, "/")
		matches, err := filepath.Glob(filepath.Join(repoTop, pat))
		if err != nil || len(matches) == 0 {
			p := filepath.Join(repoTop, pat)
			if _, statErr := os.Lstat(p); statErr == nil {
				matches = []string{p}
			}
		}
		for _, src := range matches {
			rel, err := filepath.Rel(repoTop, src)
			if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
			_ = copyIncludePath(src, filepath.Join(wtPath, rel))
		}
	}
}

func copyIncludePath(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	if info.IsDir() {
		return filepath.WalkDir(src, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil || d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			rel, err := filepath.Rel(src, p)
			if err != nil {
				return nil
			}
			target := filepath.Join(dst, rel)
			if d.IsDir() {
				return os.MkdirAll(target, 0o755)
			}
			fi, err := d.Info()
			if err != nil {
				return nil
			}
			return copySingleFile(p, target, fi.Mode())
		})
	}
	return copySingleFile(src, dst, info.Mode())
}

func copySingleFile(src, dst string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// migrateWorkingTree copies uncommitted tracked diffs and untracked non-ignored
// files from srcDir into dstDir so an active session can hand off cleanly.
func migrateWorkingTree(ctx context.Context, srcDir, dstDir string) error {
	diffCmd := exec.CommandContext(ctx, "git", "-C", srcDir, "--no-optional-locks", "diff", "--binary", "HEAD")
	diffOut, err := diffCmd.Output()
	if err == nil && len(bytes.TrimSpace(diffOut)) > 0 {
		apply := exec.CommandContext(ctx, "git", "-C", dstDir, "--no-optional-locks", "apply", "--whitespace=nowarn", "-")
		apply.Stdin = bytes.NewReader(diffOut)
		if out, err := apply.CombinedOutput(); err != nil {
			return proto.Errorf("conflict", "handoff diff apply failed: %s", strings.TrimSpace(string(out)))
		}
	}
	untracked, err := exec.CommandContext(ctx, "git", "-C", srcDir, "--no-optional-locks", "ls-files", "--others", "--exclude-standard", "-z").Output()
	if err == nil && len(untracked) > 0 {
		for _, rec := range strings.Split(string(untracked), "\x00") {
			if rec == "" {
				continue
			}
			src := filepath.Join(srcDir, rec)
			dst := filepath.Join(dstDir, rec)
			if err := copyIncludePath(src, dst); err != nil {
				return err
			}
		}
	}
	return nil
}

func handoffWorktreeToLocal(ctx context.Context, meta worktreeMeta) error {
	if status, err := gitExec(ctx, meta.Repo, "status", "--porcelain"); err == nil && status != "" {
		// Only block if tracked/untracked files conflict with worktree changes;
		// check if worktree actually has commits or uncommitted changes first.
		wtStatus, _ := gitExec(ctx, meta.Path, "status", "--porcelain")
		ahead, _ := gitExec(ctx, meta.Repo, "rev-list", "--count", "HEAD.."+meta.Branch)
		if wtStatus != "" || (ahead != "" && ahead != "0") {
			return proto.Errorf("conflict", "local repository has uncommitted changes; commit or stash them before handing off from worktree")
		}
	}
	if ahead, err := gitExec(ctx, meta.Repo, "rev-list", "--count", "HEAD.."+meta.Branch); err == nil && ahead != "" && ahead != "0" {
		if out, err := gitExec(ctx, meta.Repo, "merge", "--ff-only", meta.Branch); err != nil {
			return proto.Errorf("conflict", "handoff merge failed: %s", out)
		}
	}
	return migrateWorkingTree(ctx, meta.Path, meta.Repo)
}
