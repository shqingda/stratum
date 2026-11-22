package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/shqingda/stratum/proto"
)

const hookTimeout = 30 * time.Second

type hookSpec struct {
	matcher string
	command string
}

type hookDoc struct {
	Hooks       map[string][]hookMatcher `json:"hooks"`
	PreToolUse  []hookMatcher            `json:"PreToolUse"`
	PostToolUse []hookMatcher            `json:"PostToolUse"`
	Stop        []hookMatcher            `json:"Stop"`
}

type hookMatcher struct {
	Matcher string        `json:"matcher"`
	Command string        `json:"command"`
	Hooks   []hookCommand `json:"hooks"`
}

type hookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

type hookInput struct {
	SessionID     string          `json:"session_id"`
	Cwd           string          `json:"cwd"`
	HookEventName string          `json:"hook_event_name"`
	ToolName      string          `json:"tool_name,omitempty"`
	ToolInput     json.RawMessage `json:"tool_input,omitempty"`
	ToolResponse  string          `json:"tool_response,omitempty"`
}

func (m *Manager) hookFiles(cwd string) []string {
	var files []string
	if cwd != "" {
		files = append(files,
			filepath.Join(cwd, ".talos", "hooks.json"),
			filepath.Join(cwd, ".claude", "settings.json"),
		)
	}
	global := m.Skills.GlobalDir
	if global == "" {
		if home, err := os.UserHomeDir(); err == nil {
			global = filepath.Join(home, ".talos")
		}
	}
	if global != "" {
		files = append(files, filepath.Join(global, "hooks.json"))
	}
	files = append(files, m.pluginHookFiles(cwd)...)
	return files
}

func (m *Manager) loadHooks(cwd, event string) []hookSpec {
	var out []hookSpec
	for _, path := range m.hookFiles(cwd) {
		b, err := os.ReadFile(path)
		if err != nil || len(b) == 0 {
			continue
		}
		var doc hookDoc
		if json.Unmarshal(b, &doc) != nil {
			continue
		}
		var groups []hookMatcher
		switch event {
		case "PreToolUse":
			groups = append(groups, doc.PreToolUse...)
		case "PostToolUse":
			groups = append(groups, doc.PostToolUse...)
		case "Stop":
			groups = append(groups, doc.Stop...)
		}
		if doc.Hooks != nil {
			groups = append(groups, doc.Hooks[event]...)
		}
		for _, g := range groups {
			if cmd := strings.TrimSpace(g.Command); cmd != "" {
				out = append(out, hookSpec{matcher: g.Matcher, command: cmd})
			}
			for _, h := range g.Hooks {
				if (h.Type == "" || h.Type == "command") && strings.TrimSpace(h.Command) != "" {
					out = append(out, hookSpec{matcher: g.Matcher, command: strings.TrimSpace(h.Command)})
				}
			}
		}
	}
	return out
}

func hookMatches(matcher, tool string) bool {
	matcher = strings.TrimSpace(matcher)
	if matcher == "" || matcher == "*" {
		return true
	}
	re, err := regexp.Compile("^(" + matcher + ")$")
	if err != nil {
		return matcher == tool
	}
	return re.MatchString(tool)
}

func runHookCommand(ctx context.Context, cwd, sessionID, command string, payload hookInput) (code int, stdout, stderr string) {
	runCtx, cancel := context.WithTimeout(ctx, hookTimeout)
	defer cancel()
	in, _ := json.Marshal(payload)
	cmd := exec.CommandContext(runCtx, "sh", "-c", command)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "TALOS_SESSION_ID="+sessionID, "CLAUDE_PROJECT_DIR="+cwd)
	cmd.Stdin = bytes.NewReader(in)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			code = 1
		}
	}
	return code, strings.TrimSpace(outBuf.String()), strings.TrimSpace(errBuf.String())
}

// runPreToolHooks runs PreToolUse hooks matching toolName. A hook exiting
// with code 2 blocks the call and returns its reason.
func (m *Manager) runPreToolHooks(ctx context.Context, cwd, sessionID, toolName string, args json.RawMessage) (blocked bool, reason string) {
	specs := m.loadHooks(cwd, "PreToolUse")
	if len(specs) == 0 {
		return false, ""
	}
	payload := hookInput{
		SessionID:     sessionID,
		Cwd:           cwd,
		HookEventName: "PreToolUse",
		ToolName:      toolName,
		ToolInput:     args,
	}
	for _, s := range specs {
		if !hookMatches(s.matcher, toolName) {
			continue
		}
		code, stdout, stderr := runHookCommand(ctx, cwd, sessionID, s.command, payload)
		if code == 2 {
			msg := stderr
			if msg == "" {
				msg = stdout
			}
			if msg == "" {
				msg = "blocked by hook"
			}
			return true, msg
		}
	}
	return false, ""
}

// runPostToolHooks runs PostToolUse hooks after a tool succeeds and
// returns any feedback they emitted.
func (m *Manager) runPostToolHooks(ctx context.Context, cwd, sessionID, toolName string, args json.RawMessage, output string) string {
	specs := m.loadHooks(cwd, "PostToolUse")
	if len(specs) == 0 {
		return ""
	}
	payload := hookInput{
		SessionID:     sessionID,
		Cwd:           cwd,
		HookEventName: "PostToolUse",
		ToolName:      toolName,
		ToolInput:     args,
		ToolResponse:  output,
	}
	var notes []string
	for _, s := range specs {
		if !hookMatches(s.matcher, toolName) {
			continue
		}
		code, stdout, stderr := runHookCommand(ctx, cwd, sessionID, s.command, payload)
		msg := stdout
		if code == 2 && stderr != "" {
			msg = stderr
		}
		if msg != "" {
			notes = append(notes, msg)
		}
	}
	return strings.Join(notes, "\n")
}

// runStopHooks runs Stop hooks when a turn completes.
func (m *Manager) runStopHooks(ctx context.Context, cwd, sessionID string) {
	specs := m.loadHooks(cwd, "Stop")
	if len(specs) == 0 {
		return
	}
	payload := hookInput{
		SessionID:     sessionID,
		Cwd:           cwd,
		HookEventName: "Stop",
	}
	for _, s := range specs {
		runHookCommand(ctx, cwd, sessionID, s.command, payload)
	}
}

// ListHooks returns all configured hooks across project, global, Claude settings,
// and enabled plugins, indicating which entries are directly editable.
func (m *Manager) ListHooks(ctx context.Context, cwd string) (proto.HooksList, error) {
	var out proto.HooksList
	global := m.Skills.GlobalDir
	if global == "" {
		if home, err := os.UserHomeDir(); err == nil {
			global = filepath.Join(home, ".talos")
		}
	}
	type sourceFile struct {
		path     string
		project  bool
		editable bool
	}
	var sources []sourceFile
	if cwd != "" {
		sources = append(sources,
			sourceFile{filepath.Join(cwd, ".talos", "hooks.json"), true, true},
			sourceFile{filepath.Join(cwd, ".claude", "settings.json"), true, false},
		)
	}
	if global != "" {
		sources = append(sources, sourceFile{filepath.Join(global, "hooks.json"), false, true})
	}
	for _, pf := range m.pluginHookFiles(cwd) {
		sources = append(sources, sourceFile{pf, strings.HasPrefix(pf, cwd), false})
	}

	events := []string{"PreToolUse", "PostToolUse", "Stop"}
	for _, src := range sources {
		b, err := os.ReadFile(src.path)
		if err != nil || len(b) == 0 {
			continue
		}
		var doc hookDoc
		if err := json.Unmarshal(b, &doc); err != nil {
			out.Error = src.path + ": " + err.Error()
			continue
		}
		for _, ev := range events {
			var groups []hookMatcher
			switch ev {
			case "PreToolUse":
				groups = append(groups, doc.PreToolUse...)
			case "PostToolUse":
				groups = append(groups, doc.PostToolUse...)
			case "Stop":
				groups = append(groups, doc.Stop...)
			}
			if doc.Hooks != nil {
				groups = append(groups, doc.Hooks[ev]...)
			}
			for _, g := range groups {
				if cmd := strings.TrimSpace(g.Command); cmd != "" {
					out.Hooks = append(out.Hooks, proto.HookEntry{
						Event:    ev,
						Matcher:  g.Matcher,
						Command:  cmd,
						Project:  src.project,
						Source:   src.path,
						Editable: src.editable,
					})
				}
				for _, h := range g.Hooks {
					if (h.Type == "" || h.Type == "command") && strings.TrimSpace(h.Command) != "" {
						out.Hooks = append(out.Hooks, proto.HookEntry{
							Event:    ev,
							Matcher:  g.Matcher,
							Command:  strings.TrimSpace(h.Command),
							Project:  src.project,
							Source:   src.path,
							Editable: src.editable,
						})
					}
				}
			}
		}
	}
	return out, nil
}

// SaveHooks writes the editable hooks for project (.talos/hooks.json) or
// global (~/.talos/hooks.json) scope and returns the updated hook list.
func (m *Manager) SaveHooks(ctx context.Context, s proto.HooksSave) (proto.HooksList, error) {
	var target string
	if s.Project {
		if strings.TrimSpace(s.Cwd) == "" {
			return proto.HooksList{}, proto.Errorf("bad_request", "cwd is required for project hooks")
		}
		target = filepath.Join(s.Cwd, ".talos", "hooks.json")
	} else {
		global := m.Skills.GlobalDir
		if global == "" {
			if home, err := os.UserHomeDir(); err == nil {
				global = filepath.Join(home, ".talos")
			}
		}
		if global == "" {
			return proto.HooksList{}, proto.Errorf("internal", "cannot determine global talos dir")
		}
		target = filepath.Join(global, "hooks.json")
	}

	doc := hookDoc{
		Hooks: make(map[string][]hookMatcher),
	}
	for _, h := range s.Hooks {
		cmd := strings.TrimSpace(h.Command)
		if cmd == "" {
			continue
		}
		ev := h.Event
		if ev != "PreToolUse" && ev != "PostToolUse" && ev != "Stop" {
			return proto.HooksList{}, proto.Errorf("bad_request", "unsupported hook event %q", ev)
		}
		doc.Hooks[ev] = append(doc.Hooks[ev], hookMatcher{
			Matcher: strings.TrimSpace(h.Matcher),
			Hooks:   []hookCommand{{Type: "command", Command: cmd}},
		})
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return proto.HooksList{}, err
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return proto.HooksList{}, err
	}
	if err := os.WriteFile(target, append(b, '\n'), 0o644); err != nil {
		return proto.HooksList{}, err
	}
	return m.ListHooks(ctx, s.Cwd)
}
