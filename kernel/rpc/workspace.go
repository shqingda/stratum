package rpc

import (
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/shqingda/talos/proto"
)

func git(ctx context.Context, cwd string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", cwd, "--no-optional-locks"}, args...)...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
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
	status, _ := git(ctx, cwd, "status", "--porcelain")
	w.Dirty = status != ""
	stat, _ := git(ctx, cwd, "diff", "HEAD", "--numstat")
	for _, line := range strings.Split(stat, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		add, _ := strconv.Atoi(f[0]) // "-" for a binary file
		del, _ := strconv.Atoi(f[1])
		w.Added += add
		w.Removed += del
	}
	return w, nil
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
