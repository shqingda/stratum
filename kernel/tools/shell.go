package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/shqingda/stratum/kernel/perm"
	"github.com/shqingda/stratum/kernel/sandbox"
)

const (
	maxOutput      = 30 << 10
	defaultTimeout = 2 * time.Minute
	maxTimeout     = 10 * time.Minute
)

type shellArgs struct {
	Command      string
	Timeout      int  // seconds
	Unrestricted bool // outside the sandbox; always asks
}

// Shell runs a command line.
var Shell = &Tool{
	Name: "shell",
	Description: "Run a command line in zsh in the workspace. " + sandbox.Describe() +
		" Set unrestricted only when the command needs the network or files outside; the user is asked.",
	Schema: schema(`{"type":"object","properties":{
		"command":{"type":"string"},
		"timeout":{"type":"integer","description":"seconds; default 120, at most 600"},
		"unrestricted":{"type":"boolean"}},
		"required":["command"]}`),
	Access: func(env Env, args json.RawMessage) (perm.Request, error) {
		var a shellArgs
		if err := decode("shell", args, &a); err != nil {
			return perm.Request{}, err
		}
		if strings.TrimSpace(a.Command) == "" {
			return perm.Request{}, errors.New("shell: command is required")
		}
		return perm.Request{Tool: "shell", Kind: perm.Exec, Command: a.Command, Unrestricted: a.Unrestricted}, nil
	},
	Run: func(ctx context.Context, env Env, args json.RawMessage) (string, error) {
		var a shellArgs
		decode("shell", args, &a)
		timeout := defaultTimeout
		if a.Timeout > 0 {
			timeout = min(time.Duration(a.Timeout)*time.Second, maxTimeout)
		}
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		var cmd *exec.Cmd
		if env.Sandboxed && !a.Unrestricted {
			c, tmp, err := sandbox.Command(ctx, env.Root, a.Command)
			if err != nil {
				return "", err
			}
			defer os.RemoveAll(tmp)
			cmd = c
		} else {
			cmd = sandbox.Unsandboxed(ctx, env.Root, a.Command)
		}
		return runCommand(ctx, cmd, timeout)
	},
}

// runCommand runs cmd and reports its output and exit status. A failing
// command is a result the model reads, not an error of the tool.
func runCommand(ctx context.Context, cmd *exec.Cmd, timeout time.Duration) (string, error) {
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	cmd.Stdin = nil
	cmd.WaitDelay = 2 * time.Second // let killed pipes close
	err := cmd.Run()
	text := clip(out.String(), maxOutput)
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		return text + fmt.Sprintf("\n[killed after %s]", timeout), nil
	case ctx.Err() != nil:
		return "", ctx.Err()
	case err != nil:
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return strings.TrimRight(text, "\n") + fmt.Sprintf("\n[exit status %d]", exit.ExitCode()), nil
		}
		return "", err
	}
	if text == "" {
		return "(no output)", nil
	}
	return text, nil
}
