// Package agents loads built-in and custom Markdown/JSON agent definitions
// following the OpenCode agents specification (primary agents & subagents).
package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/shqingda/stratum/kernel/tools"
)

// Agent is a primary assistant or specialized subagent.
type Agent struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Mode        string            `json:"mode"` // primary, subagent, all
	Model       string            `json:"model,omitempty"`
	Temperature *float64          `json:"temperature,omitempty"`
	MaxSteps    int               `json:"maxSteps,omitempty"`
	Prompt      string            `json:"prompt,omitempty"`
	Tools       map[string]bool   `json:"tools,omitempty"`
	Permissions map[string]string `json:"permissions,omitempty"`
	Hidden      bool              `json:"hidden,omitempty"`
	Disable     bool              `json:"disable,omitempty"`
	BuiltIn     bool              `json:"builtIn,omitempty"`
	Source      string            `json:"source,omitempty"`
}

// BuiltIn returns the default OpenCode-style primary and subagent roles.
func BuiltIn() []Agent {
	return []Agent{
		{
			Name:        "build",
			Description: "Default primary agent with full tool access for building and editing.",
			Mode:        "primary",
			BuiltIn:     true,
		},
		{
			Name:        "plan",
			Description: "Read-only primary agent for architecture analysis and planning.",
			Mode:        "primary",
			BuiltIn:     true,
			Tools:       map[string]bool{"write_file": false, "edit_file": false, "shell": false},
			Prompt:      "You are in Plan mode. Inspect the codebase, reason about architecture, and produce a clear plan without modifying files or running shell commands.",
		},
		{
			Name:        "general",
			Description: "General-purpose subagent for multi-step research and isolated implementation tasks.",
			Mode:        "subagent",
			BuiltIn:     true,
		},
		{
			Name:        "explore",
			Description: "Fast read-only subagent for searching codebases and answering questions.",
			Mode:        "subagent",
			BuiltIn:     true,
			MaxSteps:    20,
			Tools:       map[string]bool{"write_file": false, "edit_file": false, "shell": false},
			Prompt:      "You are an Explore subagent. Search and read files quickly to answer the question accurately. Do not modify files.",
		},
	}
}

// Load discovers built-in, global, and project agents. Later definitions
// override earlier ones of the same name.
func Load(globalDir, cwd string) []Agent {
	byName := make(map[string]Agent)
	var order []string
	put := func(a Agent) {
		if a.Name == "" {
			return
		}
		if _, exists := byName[a.Name]; !exists {
			order = append(order, a.Name)
		}
		byName[a.Name] = a
	}

	for _, a := range BuiltIn() {
		put(a)
	}

	var dirs []string
	if globalDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dirs = append(dirs,
				filepath.Join(home, ".config", "opencode", "agents"),
				filepath.Join(home, ".agents", "agents"),
				filepath.Join(home, ".claude", "agents"),
				filepath.Join(home, ".stratum", "agents"),
				filepath.Join(home, ".talos", "agents"),
			)
		}
	} else {
		dirs = append(dirs, filepath.Join(globalDir, "agents"))
	}
	if cwd != "" {
		dirs = append(dirs,
			filepath.Join(cwd, ".opencode", "agents"),
			filepath.Join(cwd, ".agents", "agents"),
			filepath.Join(cwd, ".claude", "agents"),
			filepath.Join(cwd, ".stratum", "agents"),
			filepath.Join(cwd, ".talos", "agents"),
		)
	}

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
				continue
			}
			p := filepath.Join(dir, e.Name())
			b, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			name := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
			a := parseMarkdownAgent(name, string(b), p)
			put(a)
		}
	}

	if cwd != "" {
		for _, cfg := range []string{filepath.Join(cwd, "opencode.json"), filepath.Join(cwd, "opencode.jsonc")} {
			if b, err := os.ReadFile(cfg); err == nil {
				for _, a := range parseJSONAgents(b, cfg) {
					put(a)
				}
			}
		}
	}

	out := make([]Agent, 0, len(order))
	for _, name := range order {
		a := byName[name]
		if a.Disable {
			continue
		}
		if a.Mode == "" {
			a.Mode = "all"
		}
		out = append(out, a)
	}
	return out
}

// Find returns the named agent if present and enabled.
func Find(globalDir, cwd, name string) (Agent, bool) {
	for _, a := range Load(globalDir, cwd) {
		if a.Name == name {
			return a, true
		}
	}
	return Agent{}, false
}

// FilterTools removes tools disabled by a.Tools or a.Permissions.
func FilterTools(base tools.Set, a Agent) tools.Set {
	if len(a.Tools) == 0 && len(a.Permissions) == 0 {
		return base
	}
	disabled := func(name string) bool {
		if v, ok := a.Tools[name]; ok && !v {
			return true
		}
		if p, ok := a.Permissions[name]; ok && (p == "deny" || p == "false") {
			return true
		}
		// OpenCode category aliases
		switch name {
		case "shell":
			if v, ok := a.Tools["bash"]; ok && !v {
				return true
			}
			if p, ok := a.Permissions["bash"]; ok && (p == "deny" || p == "false") {
				return true
			}
		case "write_file", "edit_file":
			for _, alias := range []string{"file_edit", "edit", "write"} {
				if v, ok := a.Tools[alias]; ok && !v {
					return true
				}
				if p, ok := a.Permissions[alias]; ok && (p == "deny" || p == "false") {
					return true
				}
			}
		}
		return false
	}
	var out tools.Set
	for _, t := range base {
		if !disabled(t.Name) {
			out = append(out, t)
		}
	}
	return out
}

func parseMarkdownAgent(name, text, source string) Agent {
	a := Agent{Name: name, Mode: "all", Source: source}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		a.Prompt = strings.TrimSpace(text)
		return a
	}
	rest := text[4:]
	idx := strings.Index(rest, "\n---\n")
	if idx < 0 {
		a.Prompt = strings.TrimSpace(text)
		return a
	}
	fm := rest[:idx]
	a.Prompt = strings.TrimSpace(rest[idx+5:])

	var section string
	for _, raw := range strings.Split(fm, "\n") {
		line := strings.TrimRight(raw, " \t")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t") {
			k, v, ok := strings.Cut(strings.TrimSpace(line), ":")
			if !ok {
				continue
			}
			k = strings.TrimSpace(k)
			v = strings.Trim(strings.TrimSpace(v), `"'`)
			switch section {
			case "tools":
				if a.Tools == nil {
					a.Tools = map[string]bool{}
				}
				a.Tools[k] = v == "true"
			case "permissions", "permission":
				if a.Permissions == nil {
					a.Permissions = map[string]string{}
				}
				a.Permissions[k] = v
			}
			continue
		}
		section = ""
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if v == "" && (k == "tools" || k == "permissions" || k == "permission") {
			section = k
			continue
		}
		switch k {
		case "description":
			a.Description = v
		case "mode":
			a.Mode = v
		case "model":
			a.Model = v
		case "temperature":
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				a.Temperature = &f
			}
		case "max_steps", "steps":
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				a.MaxSteps = n
			}
		case "hidden":
			a.Hidden = v == "true"
		case "disable":
			a.Disable = v == "true"
		}
	}
	return a
}

func parseJSONAgents(raw []byte, source string) []Agent {
	var doc struct {
		Agents map[string]struct {
			Description string            `json:"description"`
			Mode        string            `json:"mode"`
			Model       string            `json:"model"`
			Temperature *float64          `json:"temperature"`
			MaxSteps    int               `json:"max_steps"`
			Steps       int               `json:"steps"`
			Prompt      string            `json:"prompt"`
			Tools       map[string]bool   `json:"tools"`
			Permissions map[string]string `json:"permissions"`
			Hidden      bool              `json:"hidden"`
			Disable     bool              `json:"disable"`
		} `json:"agents"`
	}
	if json.Unmarshal(raw, &doc) != nil || len(doc.Agents) == 0 {
		return nil
	}
	names := make([]string, 0, len(doc.Agents))
	for k := range doc.Agents {
		names = append(names, k)
	}
	sort.Strings(names)
	var out []Agent
	for _, name := range names {
		d := doc.Agents[name]
		steps := d.MaxSteps
		if steps == 0 {
			steps = d.Steps
		}
		mode := d.Mode
		if mode == "" {
			mode = "all"
		}
		out = append(out, Agent{
			Name:        name,
			Description: d.Description,
			Mode:        mode,
			Model:       d.Model,
			Temperature: d.Temperature,
			MaxSteps:    steps,
			Prompt:      d.Prompt,
			Tools:       d.Tools,
			Permissions: d.Permissions,
			Hidden:      d.Hidden,
			Disable:     d.Disable,
			Source:      source,
		})
	}
	return out
}
