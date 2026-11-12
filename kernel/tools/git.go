package tools

import (
	"context"
	"encoding/json"
	"os/exec"
	"time"

	"github.com/shqingda/talos/kernel/perm"
)

func git(ctx context.Context, env Env, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"--no-pager", "-c", "color.ui=never"}, args...)...)
	cmd.Dir = env.Root
	cmd.Env = append(cmd.Environ(), "GIT_OPTIONAL_LOCKS=0")
	return runCommand(ctx, cmd, 30*time.Second)
}

func readWorkspace(name string) func(Env, json.RawMessage) (perm.Request, error) {
	return func(env Env, args json.RawMessage) (perm.Request, error) {
		return pathAccess(name, perm.Read, env, ".")
	}
}

// GitStatus shows the branch and changed files.
var GitStatus = &Tool{
	Name:        "git_status",
	Description: "Show the git branch and the changed and untracked files of the workspace.",
	Schema:      schema(`{"type":"object","properties":{}}`),
	Access:      readWorkspace("git_status"),
	Run: func(ctx context.Context, env Env, args json.RawMessage) (string, error) {
		return git(ctx, env, "status", "--short", "--branch")
	},
}

// GitDiff shows uncommitted changes.
var GitDiff = &Tool{
	Name:        "git_diff",
	Description: "Show the workspace's uncommitted changes as a diff; staged only with staged, one file with path.",
	Schema: schema(`{"type":"object","properties":{
		"staged":{"type":"boolean"},
		"path":{"type":"string"}}}`),
	Access: readWorkspace("git_diff"),
	Run: func(ctx context.Context, env Env, args json.RawMessage) (string, error) {
		var a struct {
			Staged bool
			Path   string
		}
		if err := decode("git_diff", args, &a); err != nil {
			return "", err
		}
		argv := []string{"diff"}
		if a.Staged {
			argv = append(argv, "--staged")
		}
		if a.Path != "" {
			argv = append(argv, "--", a.Path)
		}
		out, err := git(ctx, env, argv...)
		if out == "(no output)" {
			return "No changes.", err
		}
		return out, err
	},
}
