package session

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type formatterRule struct {
	Command    []string `json:"command"`
	Extensions []string `json:"extensions"`
	Disabled   bool     `json:"disabled"`
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

	// Check .talos/formatters.json if present; otherwise use built-in gofmt for .go.
	var cmdArgs []string
	cfgPath := filepath.Join(cwd, ".talos", "formatters.json")
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
