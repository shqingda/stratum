// Package perm decides whether a tool call may run, must ask the user, or
// is refused. Decide is a pure function of the mode and the request.
package perm

import (
	"regexp"
	"strings"

	"github.com/shqingda/talos/kernel/workspace"
)

// Mode is how much a session may do unasked.
type Mode string

const (
	// Ask lets the model read the workspace; it asks before writing or
	// running anything.
	Ask Mode = "ask"
	// Auto also lets it edit the workspace and run commands in the
	// sandbox; it asks to reach outside, run unsandboxed or touch secrets.
	Auto Mode = "auto"
	// Full lets it do anything.
	Full Mode = "full"
)

// Modes in the order a picker shows them.
var Modes = []Mode{Ask, Auto, Full}

// Valid tells whether m is a known mode.
func (m Mode) Valid() bool { return m == Ask || m == Auto || m == Full }

// Decision is what happens to a call.
type Decision string

const (
	Allow   Decision = "allow"
	AskUser Decision = "ask"
	Deny    Decision = "deny"
)

// Kind is what a call does.
type Kind string

const (
	Read  Kind = "read"
	Write Kind = "write"
	Exec  Kind = "exec"
	// MCP calls a tool of an MCP server, which runs outside the sandbox:
	// only Full mode lets it run unasked.
	MCP Kind = "mcp"
)

// Request describes a call for the decision.
type Request struct {
	Tool string
	Kind Kind
	// Paths are the resolved paths it touches.
	Paths []workspace.Path
	// Command is a shell command line, for Exec.
	Command string
	// Unrestricted asks to run outside the sandbox.
	Unrestricted bool
	// InstalledSkill is set only after the skill tool finds an installed snapshot.
	InstalledSkill bool `json:"-"`
}

// Decide returns what happens to req in mode. sandboxed tells whether
// commands can run in the sandbox here.
func Decide(mode Mode, req Request, sandboxed bool) Decision {
	if mode == Full {
		return Allow
	}
	if req.Kind == Read && req.Tool == "skill" && req.InstalledSkill {
		return Allow
	}
	for _, p := range req.Paths {
		if !p.Inside || workspace.Sensitive(p.Abs) {
			return AskUser
		}
	}
	switch req.Kind {
	case Read:
		return Allow
	case Write:
		if mode == Auto {
			return Allow
		}
		return AskUser
	case Exec:
		if req.Unrestricted || !sandboxed {
			return AskUser
		}
		if mode == Auto || readOnlyCommand(req.Command) {
			return Allow
		}
		return AskUser
	}
	return AskUser
}

var literal = regexp.MustCompile(`^[\w./:=,@%+-]+$`)

// readOnlyCommands may run unasked even in Ask mode: they only look, and
// the sandbox bounds them anyway.
var readOnlyCommands = map[string]bool{
	"pwd": true, "ls": true, "git status": true, "git diff": true, "git log": true, "git branch": true,
}

// readOnlyCommand accepts a short list of commands with literal arguments:
// no expansion, redirection, pipes or chains.
func readOnlyCommand(cmd string) bool {
	words := strings.Fields(cmd)
	if len(words) == 0 {
		return false
	}
	for _, w := range words {
		if !literal.MatchString(w) || workspace.Sensitive(w) {
			return false
		}
	}
	if readOnlyCommands[words[0]] {
		return true
	}
	return len(words) > 1 && readOnlyCommands[words[0]+" "+words[1]]
}
