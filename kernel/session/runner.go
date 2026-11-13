package session

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shqingda/talos/kernel/perm"
	"github.com/shqingda/talos/kernel/tools"
	"github.com/shqingda/talos/llm"
	"github.com/shqingda/talos/proto"
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
	task func(ctx context.Context, call llm.ToolCall, prompt string) (string, error)
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
	case tool == tools.Task && r.task != nil:
		var t struct{ Prompt string }
		if json.Unmarshal(call.Args, &t) != nil || strings.TrimSpace(t.Prompt) == "" {
			return fail("task: prompt is required.")
		}
		report, err := r.task(ctx, call, t.Prompt)
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
	r.m.mu.Lock()
	if decision == perm.AskUser && r.run.always[call.Name] {
		decision = perm.Allow
	}
	r.m.mu.Unlock()
	switch decision {
	case perm.Deny:
		return fail("Talos does not allow this call.")
	case perm.AskUser:
		allowed, err := r.ask(ctx, call, req)
		if err != nil {
			return llm.ToolResult{}, err
		}
		if !allowed {
			return fail("The user declined this call. Ask them how to proceed, or take another approach.")
		}
	}
	out, err := tool.Run(ctx, r.env, call.Args)
	if ctx.Err() != nil {
		return llm.ToolResult{}, ctx.Err()
	}
	if err != nil {
		return fail("%v", err)
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
func (r *runner) ask(ctx context.Context, call llm.ToolCall, req perm.Request) (bool, error) {
	a := proto.Approval{ID: newID(), Tool: call.Name, Args: call.Args, Kind: string(req.Kind), Command: req.Command, Reason: reason(req, r.env)}
	for _, p := range req.Paths {
		a.Paths = append(a.Paths, p.Abs)
	}
	ans, err := r.wait(ctx, a)
	if err != nil {
		return false, err
	}
	if ans.Allow && ans.Always {
		r.m.mu.Lock()
		if r.run.always == nil {
			r.run.always = map[string]bool{}
		}
		r.run.always[call.Name] = true
		r.m.mu.Unlock()
	}
	return ans.Allow, nil
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
	case req.Kind == perm.Write:
		return "It changes files."
	}
	return "It touches files that may hold secrets."
}
