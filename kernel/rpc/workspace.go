package rpc

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shqingda/talos/proto"
)

func git(ctx context.Context, cwd string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", cwd, "--no-optional-locks"}, args...)...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// gitOut is git's output as it is, for records whose leading spaces count.
func gitOut(ctx context.Context, cwd string, args ...string) string {
	out, _ := exec.CommandContext(ctx, "git", append([]string{"-C", cwd, "--no-optional-locks"}, args...)...).Output()
	return string(out)
}

// workspace says which branch a folder is on and what is not committed;
// a folder outside git, or git missing, is a workspace without a branch.
func workspace(ctx context.Context, cwd string) (proto.Workspace, error) {
	if !filepath.IsAbs(cwd) {
		return proto.Workspace{}, proto.Errorf("bad_request", "cwd must be absolute")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var w proto.Workspace
	if b, err := git(ctx, cwd, "symbolic-ref", "--short", "-q", "HEAD"); err == nil {
		w.Branch = b
	} else if b, err := git(ctx, cwd, "rev-parse", "--short", "HEAD"); err == nil {
		w.Branch = b
	} else {
		return w, nil
	}
	top, err := repoTop(ctx, cwd)
	if err != nil {
		return w, nil
	}
	counts := map[string][2]int{}
	stat := gitOut(ctx, cwd, "diff", "HEAD", "--numstat", "--no-renames", "-z")
	for _, rec := range strings.Split(stat, "\x00") {
		f := strings.SplitN(rec, "\t", 3)
		if len(f) < 3 {
			continue
		}
		add, _ := strconv.Atoi(f[0]) // "-" for a binary file
		del, _ := strconv.Atoi(f[1])
		counts[f[2]] = [2]int{add, del}
	}
	status := gitOut(ctx, cwd, "status", "--porcelain=v1", "-z", "--no-renames", "--untracked-files=all")
	for _, rec := range strings.Split(status, "\x00") {
		if len(rec) < 4 {
			continue
		}
		rel := rec[3:]
		f := proto.ChangeFile{Path: filepath.Join(top, rel)}
		if rec[:2] == "??" {
			if b, err := os.ReadFile(f.Path); err == nil && len(b) > 0 {
				f.Added = bytes.Count(b, []byte("\n"))
				if b[len(b)-1] != '\n' {
					f.Added++
				}
			}
		} else {
			f.Added, f.Removed = counts[rel][0], counts[rel][1]
		}
		w.Files = append(w.Files, f)
		w.Added += f.Added
		w.Removed += f.Removed
	}
	w.Dirty = len(w.Files) > 0
	sort.Slice(w.Files, func(i, j int) bool { return w.Files[i].Path < w.Files[j].Path })
	return w, nil
}

// repoTop is the repository's top folder spelled as cwd is, so that paths
// under it match the session's, symbolic links and all.
func repoTop(ctx context.Context, cwd string) (string, error) {
	prefix, err := git(ctx, cwd, "rev-parse", "--show-prefix")
	if err != nil {
		return "", err
	}
	top := filepath.Clean(cwd)
	if prefix != "" {
		for range strings.Split(strings.TrimSuffix(prefix, "/"), "/") {
			top = filepath.Dir(top)
		}
	}
	return top, nil
}

// workspaceDiff reads a file of the folder's repository as HEAD has it and
// as it is on disk; nil for a side that has no such file.
func workspaceDiff(ctx context.Context, q proto.WorkspaceQuery) (proto.ChangeDiff, error) {
	if !filepath.IsAbs(q.Cwd) || !filepath.IsAbs(q.Path) {
		return proto.ChangeDiff{}, proto.Errorf("bad_request", "cwd and path must be absolute")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	top, err := repoTop(ctx, q.Cwd)
	if err != nil {
		return proto.ChangeDiff{}, proto.Errorf("not_found", "not in a git repository")
	}
	rel, err := filepath.Rel(top, filepath.Clean(q.Path))
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return proto.ChangeDiff{}, proto.Errorf("bad_request", "%s is outside the repository", q.Path)
	}
	d := proto.ChangeDiff{Path: q.Path}
	if b, err := exec.CommandContext(ctx, "git", "-C", top, "--no-optional-locks", "show", "HEAD:./"+filepath.ToSlash(rel)).Output(); err == nil {
		s := string(b)
		d.Before = &s
	}
	if b, err := os.ReadFile(q.Path); err == nil {
		s := string(b)
		d.After = &s
	}
	return d, nil
}

// commit stages everything in the folder and commits it.
func commit(ctx context.Context, p proto.Commit) (proto.Workspace, error) {
	if !filepath.IsAbs(p.Cwd) {
		return proto.Workspace{}, proto.Errorf("bad_request", "cwd must be absolute")
	}
	if strings.TrimSpace(p.Message) == "" {
		return proto.Workspace{}, proto.Errorf("bad_request", "empty commit message")
	}
	run, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if out, err := git(run, p.Cwd, "add", "-A"); err != nil {
		return proto.Workspace{}, proto.Errorf("internal", "git add: %s", out)
	}
	if out, err := git(run, p.Cwd, "commit", "-q", "-m", p.Message); err != nil {
		return proto.Workspace{}, proto.Errorf("internal", "git commit: %s", out)
	}
	return workspace(ctx, p.Cwd)
}
