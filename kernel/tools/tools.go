// Package tools holds the tools a model calls. Each tool says what a call
// touches, for the permission decision, and runs it. Adding a tool does
// not change the agent.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/shqingda/talos/kernel/perm"
	"github.com/shqingda/talos/kernel/workspace"
	"github.com/shqingda/talos/llm"
)

// Env is where calls run.
type Env struct {
	Root      string // the workspace
	Sandboxed bool   // commands run under the sandbox
	filePath  string // canonical target bound by a file tool's Access call
}

// Tool is one tool.
type Tool struct {
	Name        string
	Description string
	Schema      json.RawMessage
	// Access describes a call for the permission decision. It fails when
	// the arguments are malformed.
	Access func(env Env, args json.RawMessage) (perm.Request, error)
	// Run runs an approved call and returns what the model reads.
	Run     func(ctx context.Context, env Env, args json.RawMessage) (string, error)
	newCall func() *Tool
}

// Spec is the tool as a model sees it.
func (t *Tool) Spec() llm.Tool {
	return llm.Tool{Name: t.Name, Description: t.Description, Schema: t.Schema}
}

// Set is the tools of a session.
type Set []*Tool

// Default is every built-in tool.
func Default() Set {
	return Set{ReadFile, WriteFile, EditFile, ListDir, Search, Shell, GitStatus, GitDiff}
}

// Get finds a tool by name. File tools return an independent one-call instance.
func (s Set) Get(name string) *Tool {
	i := slices.IndexFunc(s, func(t *Tool) bool { return t.Name == name })
	if i < 0 {
		return nil
	}
	if s[i].newCall != nil {
		return s[i].newCall()
	}
	return s[i]
}

// Specs lists the tools for a model request.
func (s Set) Specs() []llm.Tool {
	out := make([]llm.Tool, len(s))
	for i, t := range s {
		out[i] = t.Spec()
	}
	return out
}

// decode parses arguments into v, naming the tool in the error.
func decode(name string, args json.RawMessage, v any) error {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	if err := json.Unmarshal(args, v); err != nil {
		return fmt.Errorf("%s: bad arguments: %v", name, err)
	}
	return nil
}

// pathAccess resolves path and describes a read or write of it.
func pathAccess(name string, kind perm.Kind, env Env, path string) (perm.Request, error) {
	if strings.TrimSpace(path) == "" {
		return perm.Request{}, fmt.Errorf("%s: path is required", name)
	}
	p, err := workspace.Resolve(env.Root, path)
	if err != nil {
		return perm.Request{}, err
	}
	return perm.Request{Tool: name, Kind: kind, Paths: []workspace.Path{p}}, nil
}

// clip keeps output within limit bytes, keeping its head and tail, which
// is where compilers and test runners say what went wrong.
func clip(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	head, tail := s[:limit/2], s[len(s)-limit/2:]
	for !utf8.ValidString(head) {
		head = head[:len(head)-1]
	}
	for !utf8.ValidString(tail) {
		tail = tail[1:]
	}
	return fmt.Sprintf("%s\n\n[… %d bytes cut …]\n\n%s", head, len(s)-len(head)-len(tail), tail)
}

func schema(s string) json.RawMessage { return json.RawMessage(s) }
