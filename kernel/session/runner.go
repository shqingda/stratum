package session

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/shqingda/stratum/kernel/perm"
	"github.com/shqingda/stratum/kernel/tools"
	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/proto"
)

// runner runs a turn's tool calls: it decides permission, asks the user
// when needed, and runs what is allowed.
type runner struct {
	m         *Manager
	sessionID string
	run       *run
	env       tools.Env
	tools     tools.Set
	// task runs a sub-agent for a task call; nil in a sub-agent.
	task func(ctx context.Context, callID, taskID, prompt, agentName string) (string, error)
}

func (r *runner) Specs() []llm.Tool { return r.tools.Specs() }

func (r *runner) Run(ctx context.Context, call llm.ToolCall) (llm.ToolResult, error) {
	fail := func(format string, args ...any) (llm.ToolResult, error) {
		return llm.ToolResult{Content: fmt.Sprintf(format, args...), IsError: true}, nil
	}
	tool := r.tools.Get(call.Name)
	if tool == nil {
		return fail("There is no tool %q.", call.Name)
	}
	switch {
	case tool == tools.AskUser:
		return r.question(ctx, call)
	case tool == tools.RunTasks && r.task != nil:
		return r.graph(ctx, call)
	case tool == tools.Task && r.task != nil:
		var t struct{ Prompt, Agent string }
		if json.Unmarshal(call.Args, &t) != nil || strings.TrimSpace(t.Prompt) == "" {
			return fail("task: prompt is required.")
		}
		report, err := r.task(ctx, call.ID, "", t.Prompt, strings.TrimSpace(t.Agent))
		if ctx.Err() != nil {
			return llm.ToolResult{}, ctx.Err()
		}
		if err != nil {
			return fail("The sub-agent failed: %v", err)
		}
		return llm.ToolResult{Content: report}, nil
	}
	req, err := tool.Access(r.env, call.Args)
	if err != nil {
		return fail("%v", err)
	}
	// The mode is read per call: the user may change it mid-turn.
	x, err := r.m.Store.GetSession(context.WithoutCancel(ctx), r.sessionID)
	if err != nil {
		return llm.ToolResult{}, err
	}
	decision := perm.Decide(perm.Mode(x.Mode), req, r.env.Sandboxed)
	if decision == perm.AskUser && r.allowed(ctx, req) {
		decision = perm.Allow
	}
	var changed string
	switch decision {
	case perm.Deny:
		return fail("Stratum does not allow this call.")
	case perm.AskUser:
		ans, err := r.ask(ctx, call, req)
		if err != nil {
			return llm.ToolResult{}, err
		}
		if !ans.Allow {
			why := "Ask them how to proceed, or take another approach."
			if t := strings.TrimSpace(ans.Text); t != "" {
				why = "They said: " + t
			}
			res, err := fail("The user declined this call. %s", why)
			res.Skipped = true
			return res, err
		}
		if cmd := strings.TrimSpace(ans.Command); cmd != "" && cmd != strings.TrimSpace(req.Command) && req.Kind == perm.Exec {
			var args map[string]any
			if json.Unmarshal(call.Args, &args) == nil {
				args["command"] = cmd
				call.Args, _ = json.Marshal(args)
				changed = cmd
			}
		}
	}
	if blocked, reason := r.m.runPreToolHooks(ctx, r.env.Root, r.sessionID, call.Name, call.Args); blocked {
		return fail("Blocked by PreToolUse hook: %s", reason)
	}
	out, err := tool.Run(ctx, r.env, call.Args)
	if changed != "" {
		out = fmt.Sprintf("The user changed the command before it ran. It ran:\n%s\n\n%s", changed, out)
	}
	if ctx.Err() != nil {
		return llm.ToolResult{}, ctx.Err()
	}
	if err != nil {
		return fail("%v", err)
	}
	if call.Name == "write_file" || call.Name == "edit_file" {
		r.m.runFormatter(ctx, r.env.Root, string(call.Args))
	}
	if note := r.m.runPostToolHooks(ctx, r.env.Root, r.sessionID, call.Name, call.Args, out); note != "" {
		out += "\n\n[PostToolUse hook]\n" + note
	}
	return llm.ToolResult{Content: out}, nil
}

// question puts the model's question to the user and returns the reply.
func (r *runner) question(ctx context.Context, call llm.ToolCall) (llm.ToolResult, error) {
	var q struct {
		Question string
		Options  []string
	}
	if json.Unmarshal(call.Args, &q) != nil || strings.TrimSpace(q.Question) == "" {
		return llm.ToolResult{Content: "ask_user: question is required.", IsError: true}, nil
	}
	ans, err := r.wait(ctx, proto.Approval{ID: newID(), Tool: call.Name, Args: call.Args, Kind: "question", Question: q.Question, Options: q.Options})
	if err != nil {
		return llm.ToolResult{}, err
	}
	if !ans.Allow || strings.TrimSpace(ans.Text) == "" {
		return llm.ToolResult{Content: "The user skipped the question. Decide yourself, or stop and say what you need."}, nil
	}
	return llm.ToolResult{Content: "The user answered: " + ans.Text}, nil
}

// ask records an approval request and waits for the answer or the end of
// the turn.
func (r *runner) ask(ctx context.Context, call llm.ToolCall, req perm.Request) (proto.ApprovalAnswer, error) {
	rule := ruleFor(req)
	a := proto.Approval{ID: newID(), Tool: call.Name, Args: call.Args, Kind: string(req.Kind), Command: req.Command, Reason: reason(req, r.env)}
	if req.Kind != perm.Exec || rule.Prefix != "" {
		a.Rule = &rule
	}
	for _, p := range req.Paths {
		a.Paths = append(a.Paths, p.Abs)
	}
	ans, err := r.wait(ctx, a)
	if err != nil || !ans.Allow || a.Rule == nil || ans.Command != "" && strings.TrimSpace(ans.Command) != strings.TrimSpace(req.Command) {
		return ans, err // a changed command keeps no rule of the one asked about
	}
	switch {
	case ans.Forever:
		set, err := r.m.Settings(context.WithoutCancel(ctx))
		if err == nil && !slices.Contains(set.Rules, rule) {
			set.Rules = append(set.Rules, rule)
			r.m.SetSettings(context.WithoutCancel(ctx), set)
		}
	case ans.Always:
		r.m.mu.Lock()
		if r.m.allowed == nil {
			r.m.allowed = map[string][]proto.Rule{}
		}
		r.m.allowed[r.sessionID] = append(r.m.allowed[r.sessionID], rule)
		r.m.mu.Unlock()
	}
	return ans, nil
}

// allowed reports whether a rule of the session or of the settings lets
// req run unasked.
func (r *runner) allowed(ctx context.Context, req perm.Request) bool {
	r.m.mu.Lock()
	rules := slices.Clone(r.m.allowed[r.sessionID])
	r.m.mu.Unlock()
	if set, err := r.m.Settings(context.WithoutCancel(ctx)); err == nil {
		rules = append(rules, set.Rules...)
	}
	return slices.ContainsFunc(rules, func(x proto.Rule) bool { return allows(x, req) })
}

// wait records an approval request and waits for the answer or the end
// of the turn.
func (r *runner) wait(ctx context.Context, a proto.Approval) (proto.ApprovalAnswer, error) {
	p := &pending{approval: a, answer: make(chan proto.ApprovalAnswer, 1)}
	r.m.mu.Lock()
	r.run.approvals[a.ID] = p
	r.m.mu.Unlock()
	defer func() {
		r.m.mu.Lock()
		delete(r.run.approvals, a.ID)
		r.m.mu.Unlock()
	}()
	if err := r.m.record(ctx, r.sessionID, proto.EventApprovalRequest, a); err != nil {
		return proto.ApprovalAnswer{}, err
	}
	select {
	case ans := <-p.answer:
		r.m.record(ctx, r.sessionID, proto.EventApprovalResolved, ans)
		return ans, nil
	case <-ctx.Done():
		r.m.record(ctx, r.sessionID, proto.EventApprovalResolved, proto.ApprovalAnswer{SessionID: r.sessionID, ApprovalID: a.ID})
		return proto.ApprovalAnswer{}, ctx.Err()
	}
}

// reason says, for the user, why a call needs approval.
func reason(req perm.Request, env tools.Env) string {
	for _, p := range req.Paths {
		if !p.Inside {
			return "It reaches outside the workspace."
		}
	}
	switch {
	case req.Unrestricted:
		return "It runs without the sandbox: with network and access outside the workspace."
	case req.Kind == perm.Exec && !env.Sandboxed:
		return "Commands cannot be sandboxed here."
	case req.Kind == perm.Exec:
		return "It runs a command."
	case req.Kind == perm.MCP:
		return "It calls a tool of an MCP server, outside the sandbox."
	case req.Kind == perm.Write:
		return "It changes files."
	}
	return "It touches files that may hold secrets."
}
