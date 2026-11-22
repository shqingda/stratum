// Package agents loads built-in and custom Markdown/JSON agent definitions
// following the OpenCode agents specification (primary agents & subagents).
package agents

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/shqingda/stratum/kernel/perm"
	"github.com/shqingda/stratum/kernel/tools"
	"github.com/shqingda/stratum/kernel/workspace"
)

// Agent is a primary assistant or specialized subagent.
type Agent struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Mode        string          `json:"mode"` // primary, subagent, all
	Model       string          `json:"model,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	TopP        *float64        `json:"topP,omitempty"`
	MaxSteps    int             `json:"maxSteps,omitempty"`
	Prompt      string          `json:"prompt,omitempty"`
	Tools       map[string]bool `json:"tools,omitempty"`
	// Permissions maps a tool, or an OpenCode name for one (bash, edit,
	// webfetch), to allow, ask or deny.
	Permissions map[string]string `json:"permissions,omitempty"`
	// Bash maps a command to allow, ask or deny. A rule matches the command
	// itself and the command with more words ("git push" matches "git push
	// origin"); * matches any text. The longest rule that matches wins.
	Bash    map[string]string `json:"bash,omitempty"`
	Hidden  bool              `json:"hidden,omitempty"`
	Disable bool              `json:"disable,omitempty"`
	BuiltIn bool              `json:"builtIn,omitempty"`
	Source  string            `json:"source,omitempty"`
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

// Load discovers built-in, plugin (extra), global, and project agents.
// Later definitions override earlier ones of the same name.
func Load(globalDir, cwd string, extra ...string) []Agent {
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

	dirs := append([]string(nil), extra...)
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
		for _, cfg := range []string{filepath.Join(cwd, "opencode.json"), filepath.Join(cwd, "opencode.jsonc"), filepath.Join(cwd, ".opencode", "opencode.json")} {
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
func Find(globalDir, cwd, name string, extra ...string) (Agent, bool) {
	return Named(Load(globalDir, cwd, extra...), name)
}

// Named returns the agent called name in list.
func Named(list []Agent, name string) (Agent, bool) {
	for _, a := range list {
		if a.Name == name {
			return a, true
		}
	}
	return Agent{}, false
}

// Primary reports whether a can lead a session, as build and plan do.
func (a Agent) Primary() bool { return !a.Hidden && (a.Mode == "primary" || a.Mode == "all") }

// Sub reports whether a can take a task from another agent.
func (a Agent) Sub() bool { return !a.Hidden && (a.Mode == "subagent" || a.Mode == "all") }

// Decide applies the agent's permission to base, what the session's mode
// decided for req. deny refuses the call; ask asks even when the mode
// allows it; allow runs unasked what the mode would ask about, but never a
// path outside the workspace, a secret, or a command outside the sandbox.
func (a Agent) Decide(req perm.Request, base perm.Decision, sandboxed bool) perm.Decision {
	switch a.action(req) {
	case "deny":
		return perm.Deny
	case "ask":
		if base == perm.Allow {
			return perm.AskUser
		}
	case "allow":
		if base == perm.AskUser && liftable(req, sandboxed) {
			return perm.Allow
		}
	}
	return base
}

func liftable(req perm.Request, sandboxed bool) bool {
	for _, p := range req.Paths {
		if !p.Inside || workspace.Sensitive(p.Abs) {
			return false
		}
	}
	return req.Kind != perm.Exec || sandboxed && !req.Unrestricted
}

// action is what the agent says about req: allow, ask, deny or "".
func (a Agent) action(req perm.Request) string {
	if req.Kind == perm.Exec && strings.TrimSpace(req.Command) != "" && len(a.Bash) > 0 {
		if act := bashAction(a.Bash, req.Command); act != "" {
			return act
		}
	}
	for _, name := range aliases(req.Tool) {
		if v, ok := a.Permissions[name]; ok {
			return action(v)
		}
	}
	return ""
}

func action(v string) string {
	switch v = strings.ToLower(strings.TrimSpace(v)); v {
	case "false":
		return "deny"
	case "true":
		return "allow"
	case "allow", "ask", "deny":
		return v
	}
	return ""
}

// aliases are the names a permission may give a tool: its own, then
// OpenCode's.
func aliases(tool string) []string {
	switch tool {
	case "shell":
		return []string{"shell", "bash"}
	case "write_file", "edit_file":
		return []string{tool, "edit", "write", "file_edit"}
	case "web_fetch":
		return []string{tool, "webfetch"}
	case "web_search":
		return []string{tool, "websearch"}
	}
	return []string{tool}
}

var (
	commandSep = regexp.MustCompile(`\s*(?:&&|\|\||;|\||\n)\s*`)
	// expansion marks a command whose words are not all in sight: allow
	// rules do not cover it.
	expansion = regexp.MustCompile("`|\\$\\(|[<>]")
)

// bashAction decides a command line by rules: each command of a chain or
// pipe is matched alone. One denied command denies the line and one asked
// asks; the line is allowed only when every command is, with no expansion
// or redirection.
func bashAction(rules map[string]string, line string) string {
	var acts []string
	for _, cmd := range commandSep.Split(strings.TrimSpace(line), -1) {
		if cmd = strings.TrimSpace(cmd); cmd != "" {
			acts = append(acts, ruleFor(rules, cmd))
		}
	}
	switch {
	case len(acts) == 0:
		return ""
	case containsAct(acts, "deny"):
		return "deny"
	case containsAct(acts, "ask"):
		return "ask"
	}
	for _, act := range acts {
		if act != "allow" {
			return ""
		}
	}
	if expansion.MatchString(line) {
		return ""
	}
	return "allow"
}

func containsAct(acts []string, act string) bool {
	for _, a := range acts {
		if a == act {
			return true
		}
	}
	return false
}

// ruleFor returns the action of the longest rule matching cmd; of rules
// as long, the strictest.
func ruleFor(rules map[string]string, cmd string) string {
	best, bestLen := "", -1
	rank := map[string]int{"allow": 0, "ask": 1, "deny": 2}
	for pattern, v := range rules {
		act := action(v)
		if act == "" || !matchCommand(pattern, cmd) {
			continue
		}
		n := len(strings.ReplaceAll(pattern, "*", ""))
		if n > bestLen || n == bestLen && rank[act] > rank[best] {
			best, bestLen = act, n
		}
	}
	return best
}

func matchCommand(pattern, cmd string) bool {
	pattern = strings.Join(strings.Fields(pattern), " ")
	cmd = strings.Join(strings.Fields(cmd), " ")
	if !strings.Contains(pattern, "*") {
		return cmd == pattern || strings.HasPrefix(cmd, pattern+" ")
	}
	parts := strings.Split(pattern, "*")
	for i, part := range parts {
		parts[i] = regexp.QuoteMeta(part)
	}
	ok, _ := regexp.MatchString("^"+strings.Join(parts, ".*")+"$", cmd)
	return ok
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
		for pattern, v := range a.Tools {
			if ok, _ := path.Match(pattern, name); !v && ok && strings.Contains(pattern, "*") {
				return true
			}
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

	// section is the open map at the top (tools, permission); bash the
	// indent of an open permission.bash map, -1 when none is open.
	section, bash := "", -1
	for _, raw := range strings.Split(fm, "\n") {
		line := strings.TrimRight(raw, " \t")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		k, v, ok := cutKey(trimmed)
		if !ok {
			continue
		}
		if indent == 0 {
			section, bash = "", -1
			if v == "" && (k == "tools" || k == "permissions" || k == "permission") {
				section = k
				continue
			}
			a.set(k, v)
			continue
		}
		switch section {
		case "tools":
			if a.Tools == nil {
				a.Tools = map[string]bool{}
			}
			a.Tools[k] = v == "true"
		case "permissions", "permission":
			if bash >= 0 && indent > bash {
				if a.Bash == nil {
					a.Bash = map[string]string{}
				}
				a.Bash[k] = v
				continue
			}
			bash = -1
			if k == "bash" && v == "" {
				bash = indent
				continue
			}
			if a.Permissions == nil {
				a.Permissions = map[string]string{}
			}
			a.Permissions[k] = v
		}
	}
	return a
}

// set reads one top-level field of the front matter.
func (a *Agent) set(k, v string) {
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
	case "top_p", "topP":
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			a.TopP = &f
		}
	case "max_steps", "maxSteps", "steps":
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			a.MaxSteps = n
		}
	case "prompt":
		if a.Prompt == "" {
			a.Prompt = v
		}
	case "hidden":
		a.Hidden = v == "true"
	case "disable":
		a.Disable = v == "true"
	}
}

// cutKey splits "key: value", where the key may be quoted ("git push":
// ask) and quotes around the value go.
func cutKey(line string) (k, v string, ok bool) {
	if q := line[0]; q == '"' || q == '\'' {
		end := strings.IndexByte(line[1:], q)
		if end < 0 {
			return "", "", false
		}
		k, rest := line[1:end+1], strings.TrimSpace(line[end+2:])
		if !strings.HasPrefix(rest, ":") {
			return "", "", false
		}
		return k, unquote(rest[1:]), true
	}
	k, v, ok = strings.Cut(line, ":")
	return strings.TrimSpace(k), unquote(v), ok
}

func unquote(v string) string { return strings.Trim(strings.TrimSpace(v), `"'`) }

type jsonAgent struct {
	Description string                     `json:"description"`
	Mode        string                     `json:"mode"`
	Model       string                     `json:"model"`
	Temperature *float64                   `json:"temperature"`
	TopP        *float64                   `json:"top_p"`
	TopPCamel   *float64                   `json:"topP"`
	MaxSteps    int                        `json:"max_steps"`
	Steps       int                        `json:"steps"`
	Prompt      string                     `json:"prompt"`
	Tools       map[string]bool            `json:"tools"`
	Permission  map[string]json.RawMessage `json:"permission"`
	Permissions map[string]json.RawMessage `json:"permissions"`
	Hidden      bool                       `json:"hidden"`
	Disable     bool                       `json:"disable"`
}

// parseJSONAgents reads the agents of an opencode.json: under "agent", as
// OpenCode names it, or "agents". Comments and trailing commas (JSONC)
// are allowed.
func parseJSONAgents(raw []byte, source string) []Agent {
	var doc struct {
		Agent  map[string]jsonAgent `json:"agent"`
		Agents map[string]jsonAgent `json:"agents"`
	}
	if json.Unmarshal(stripJSONC(raw), &doc) != nil {
		return nil
	}
	all := map[string]jsonAgent{}
	for k, v := range doc.Agents {
		all[k] = v
	}
	for k, v := range doc.Agent {
		all[k] = v
	}
	names := make([]string, 0, len(all))
	for k := range all {
		names = append(names, k)
	}
	sort.Strings(names)
	var out []Agent
	for _, name := range names {
		d := all[name]
		a := Agent{
			Name:        name,
			Description: d.Description,
			Mode:        d.Mode,
			Model:       d.Model,
			Temperature: d.Temperature,
			TopP:        d.TopP,
			MaxSteps:    d.MaxSteps,
			Prompt:      d.Prompt,
			Tools:       d.Tools,
			Hidden:      d.Hidden,
			Disable:     d.Disable,
			Source:      source,
		}
		if a.TopP == nil {
			a.TopP = d.TopPCamel
		}
		if a.MaxSteps == 0 {
			a.MaxSteps = d.Steps
		}
		if a.Mode == "" {
			a.Mode = "all"
		}
		for _, perms := range []map[string]json.RawMessage{d.Permissions, d.Permission} {
			for k, v := range perms {
				var act string
				var rules map[string]string
				switch {
				case json.Unmarshal(v, &act) == nil:
					if a.Permissions == nil {
						a.Permissions = map[string]string{}
					}
					a.Permissions[k] = act
				case k == "bash" && json.Unmarshal(v, &rules) == nil:
					if a.Bash == nil {
						a.Bash = map[string]string{}
					}
					for p, r := range rules {
						a.Bash[p] = r
					}
				}
			}
		}
		out = append(out, a)
	}
	return out
}

// stripJSONC drops // and /* */ comments and trailing commas outside
// strings.
func stripJSONC(b []byte) []byte {
	var out []byte
	inString, escaped := false, false
	for i := 0; i < len(b); i++ {
		c := b[i]
		if inString {
			out = append(out, c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch {
		case c == '"':
			inString = true
		case c == '/' && i+1 < len(b) && b[i+1] == '/':
			for i < len(b) && b[i] != '\n' {
				i++
			}
			continue
		case c == '/' && i+1 < len(b) && b[i+1] == '*':
			end := strings.Index(string(b[i+2:]), "*/")
			if end < 0 {
				return out
			}
			i += end + 3
			continue
		case c == '}' || c == ']':
			j := len(out) - 1
			for j >= 0 && (out[j] == ' ' || out[j] == '\t' || out[j] == '\n' || out[j] == '\r') {
				j--
			}
			if j >= 0 && out[j] == ',' {
				out = append(out[:j], out[j+1:]...)
			}
		}
		out = append(out, c)
	}
	return out
}
