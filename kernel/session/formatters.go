package session

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shqingda/stratum/kernel/skills"
	"github.com/shqingda/stratum/proto"
)

type formatterRule struct {
	Command    []string `json:"command"`
	Extensions []string `json:"extensions"`
	Disabled   bool     `json:"disabled"`
}

// formatters lists what formats a file after the agent writes it: the
// rules of the folder's formatters.json, then gofmt for .go files unless a
// rule takes them.
func formatters(cwd string) []proto.FormatterInfo {
	var out []proto.FormatterInfo
	goTaken := false
	if b, err := os.ReadFile(filepath.Join(skills.ProjectDir(cwd), "formatters.json")); err == nil {
		var rules map[string]formatterRule
		if json.Unmarshal(b, &rules) == nil {
			names := make([]string, 0, len(rules))
			for name := range rules {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				r := rules[name]
				out = append(out, proto.FormatterInfo{Name: name, Command: r.Command, Extensions: r.Extensions, Disabled: r.Disabled})
				for _, e := range r.Extensions {
					goTaken = goTaken || strings.EqualFold(e, ".go") && !r.Disabled && len(r.Command) > 0
				}
			}
		}
	}
	if !goTaken {
		out = append(out, proto.FormatterInfo{Name: "gofmt", Command: []string{"gofmt", "-w"}, Extensions: []string{".go"}, BuiltIn: true})
	}
	return out
}

func (m *Manager) runFormatter(ctx context.Context, cwd, rawArgs string) {
	if cwd == "" {
		return
	}
	var a struct {
		Path string `json:"path"`
	}
	if json.Unmarshal([]byte(rawArgs), &a) != nil || strings.TrimSpace(a.Path) == "" {
		return
	}
	target := a.Path
	if !filepath.IsAbs(target) {
		target = filepath.Join(cwd, target)
	}
	ext := strings.ToLower(filepath.Ext(target))
	if ext == "" {
		return
	}

	// Check .stratum/formatters.json if present; otherwise use built-in gofmt for .go.
	var cmdArgs []string
	cfgPath := filepath.Join(skills.ProjectDir(cwd), "formatters.json")
	if b, err := os.ReadFile(cfgPath); err == nil {
		var rules map[string]formatterRule
		if json.Unmarshal(b, &rules) == nil {
			for _, r := range rules {
				if r.Disabled || len(r.Command) == 0 {
					continue
				}
				for _, e := range r.Extensions {
					if strings.EqualFold(e, ext) {
						cmdArgs = append([]string(nil), r.Command...)
						break
					}
				}
				if len(cmdArgs) > 0 {
					break
				}
			}
		}
	}
	if len(cmdArgs) == 0 && ext == ".go" {
		if _, err := exec.LookPath("gofmt"); err == nil {
			cmdArgs = []string{"gofmt", "-w", "$FILE"}
		}
	}
	if len(cmdArgs) == 0 {
		return
	}

	replaced := false
	for i := range cmdArgs {
		if cmdArgs[i] == "$FILE" {
			cmdArgs[i] = target
			replaced = true
		}
	}
	if !replaced {
		cmdArgs = append(cmdArgs, target)
	}

	fctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(fctx, cmdArgs[0], cmdArgs[1:]...)
	cmd.Dir = cwd
	_ = cmd.Run()
}
