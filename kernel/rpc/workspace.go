package rpc

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/shqingda/talos/proto"
)

// workspace says which branch a folder is on; a folder outside git, or
// git missing, is a workspace without one.
func workspace(ctx context.Context, cwd string) (proto.Workspace, error) {
	if !filepath.IsAbs(cwd) {
		return proto.Workspace{}, proto.Errorf("bad_request", "cwd must be absolute")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	git := func(args ...string) string {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", cwd, "--no-optional-locks"}, args...)...)
		out, err := cmd.Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	branch := git("symbolic-ref", "--short", "-q", "HEAD")
	if branch == "" {
		branch = git("rev-parse", "--short", "HEAD")
	}
	return proto.Workspace{Branch: branch}, nil
}
