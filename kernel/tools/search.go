package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/shqingda/stratum/kernel/perm"
	"github.com/shqingda/stratum/kernel/workspace"
)

type searchArgs struct {
	Pattern    string
	Path       string
	Glob       string
	IgnoreCase bool `json:"ignore_case"`
	Files      bool // list matching files only
}

// Search finds text in files with ripgrep, which skips what .gitignore
// ignores.
var Search = &Tool{
	Name:        "search",
	Description: "Search file contents for a regular expression (ripgrep syntax). Results are file:line:text. Narrow with path and glob (e.g. \"*.go\"); set files to list only matching files.",
	Schema: schema(`{"type":"object","properties":{
		"pattern":{"type":"string"},
		"path":{"type":"string","description":"directory or file; default the workspace"},
		"glob":{"type":"string"},
		"ignore_case":{"type":"boolean"},
		"files":{"type":"boolean"}},
		"required":["pattern"]}`),
	Access: func(env Env, args json.RawMessage) (perm.Request, error) {
		var a searchArgs
		if err := decode("search", args, &a); err != nil {
			return perm.Request{}, err
		}
		if a.Pattern == "" {
			return perm.Request{}, errors.New("search: pattern is required")
		}
		return pathAccess("search", perm.Read, env, cmpOr(a.Path, "."))
	},
	Run: func(ctx context.Context, env Env, args json.RawMessage) (string, error) {
		var a searchArgs
		decode("search", args, &a)
		p, err := workspace.Resolve(env.Root, cmpOr(a.Path, "."))
		if err != nil {
			return "", err
		}
		// Inside the workspace, a relative path makes ripgrep print short
		// relative names.
		path := p.Abs
		if p.Inside {
			path = cmpOr(p.Rel, ".")
		}
		rg, err := exec.LookPath("rg")
		if err != nil {
			return "", errors.New("search needs ripgrep (brew install ripgrep); use shell with grep meanwhile")
		}
		argv := []string{"--line-number", "--no-heading", "--color=never", "--max-columns=300", "--max-count=200"}
		if a.IgnoreCase {
			argv = append(argv, "--ignore-case")
		}
		if a.Files {
			argv = append(argv, "--files-with-matches")
		}
		if a.Glob != "" {
			argv = append(argv, "--glob", a.Glob)
		}
		argv = append(argv, "--", a.Pattern)
		if path != "." {
			argv = append(argv, path) // without one, ripgrep prints names without ./
		}
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, rg, argv...)
		cmd.Dir, _ = filepath.EvalSymlinks(env.Root)
		out, err := cmd.Output()
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return "No matches.", nil
		}
		if err != nil {
			msg := err.Error()
			if errors.As(err, &exit) {
				msg = strings.TrimSpace(string(exit.Stderr))
			}
			return "", errors.New("search: " + msg)
		}
		return clip(string(out), maxOutput), nil
	},
}
