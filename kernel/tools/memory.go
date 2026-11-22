package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shqingda/stratum/kernel/perm"
)

// Memory reads and writes cross-session MEMORY.md files (project or global).
var Memory = &Tool{
	Name:        "memory",
	Description: "Read or update persistent cross-session memory stored in MEMORY.md (project .talos/MEMORY.md or global ~/.talos/MEMORY.md). Use it to remember user preferences, project conventions, and decisions across sessions.",
	Schema: schema(`{"type":"object","properties":{
		"action":{"type":"string","enum":["read","append","replace"],"description":"Whether to read, append to, or replace memory."},
		"scope":{"type":"string","enum":["project","global"],"description":"Memory scope (defaults to project)."},
		"content":{"type":"string","description":"Markdown content to append or replace with."}},
		"required":["action"]}`),
	Access: func(env Env, args json.RawMessage) (perm.Request, error) {
		var a struct {
			Action, Scope, Content string
		}
		if err := decode("memory", args, &a); err != nil {
			return perm.Request{}, err
		}
		p, err := memoryTarget(env, a.Scope)
		if err != nil {
			return perm.Request{}, err
		}
		switch a.Action {
		case "read":
			return pathAccess("memory", perm.Read, env, p)
		case "append", "replace":
			if strings.TrimSpace(a.Content) == "" {
				return perm.Request{}, fmt.Errorf("memory: content is required")
			}
			return pathAccess("memory", perm.Write, env, p)
		default:
			return perm.Request{}, fmt.Errorf("memory: unknown action %q", a.Action)
		}
	},
	Run: func(ctx context.Context, env Env, args json.RawMessage) (string, error) {
		var a struct {
			Action, Scope, Content string
		}
		if err := decode("memory", args, &a); err != nil {
			return "", err
		}
		p, err := memoryTarget(env, a.Scope)
		if err != nil {
			return "", err
		}
		switch a.Action {
		case "read":
			b, err := os.ReadFile(p)
			if errors.Is(err, os.ErrNotExist) {
				return "(empty)", nil
			}
			if err != nil {
				return "", err
			}
			return clip(string(b), 64<<10), nil
		case "append", "replace":
			if strings.TrimSpace(a.Content) == "" {
				return "", fmt.Errorf("memory: content is required")
			}
			var before []byte
			if b, err := os.ReadFile(p); err == nil {
				before = b
			} else if !errors.Is(err, os.ErrNotExist) {
				return "", err
			}
			var next string
			if a.Action == "append" && len(before) > 0 {
				cur := strings.TrimRight(string(before), "\n")
				if cur != "" {
					next = cur + "\n\n" + strings.TrimSpace(a.Content) + "\n"
				} else {
					next = strings.TrimSpace(a.Content) + "\n"
				}
			} else {
				next = strings.TrimSpace(a.Content) + "\n"
			}
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return "", err
			}
			after := []byte(next)
			if err := os.WriteFile(p, after, 0o644); err != nil {
				return "", err
			}
			if env.RecordChange != nil && a.Scope != "global" {
				_ = env.RecordChange(p, before, after)
			}
			return fmt.Sprintf("Saved memory to %s.", p), nil
		default:
			return "", fmt.Errorf("memory: unknown action %q", a.Action)
		}
	},
}

func memoryTarget(env Env, scope string) (string, error) {
	switch scope {
	case "", "project":
		return filepath.Join(env.Root, ".talos", "MEMORY.md"), nil
	case "global":
		dir := os.Getenv("TALOS_MEMORY_DIR")
		if dir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			dir = filepath.Join(home, ".talos")
		}
		return filepath.Join(dir, "MEMORY.md"), nil
	default:
		return "", fmt.Errorf("memory: unknown scope %q", scope)
	}
}
