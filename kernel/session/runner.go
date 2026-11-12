package session

import (
	"context"
	"fmt"
	"sync"

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

	mu     sync.Mutex
	always map[string]bool // tools the user allowed for the session
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
	r.mu.Lock()
	if decision == perm.AskUser && r.always[call.Name] {
		decision = perm.Allow
	}
	r.mu.Unlock()
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

// ask records an approval request and waits for the answer or the end of
// the turn.
func (r *runner) ask(ctx context.Context, call llm.ToolCall, req perm.Request) (bool, error) {
	a := proto.Approval{ID: newID(), Tool: call.Name, Args: call.Args, Kind: string(req.Kind), Command: req.Command, Reason: reason(req, r.env)}
	for _, p := range req.Paths {
		a.Paths = append(a.Paths, p.Abs)
	}
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
		return false, err
	}
	select {
	case ans := <-p.answer:
		r.m.record(ctx, r.sessionID, proto.EventApprovalResolved, ans)
		if ans.Allow && ans.Always {
			r.mu.Lock()
			if r.always == nil {
				r.always = map[string]bool{}
			}
			r.always[call.Name] = true
			r.mu.Unlock()
		}
		return ans.Allow, nil
	case <-ctx.Done():
		r.m.record(ctx, r.sessionID, proto.EventApprovalResolved, proto.ApprovalAnswer{SessionID: r.sessionID, ApprovalID: a.ID})
		return false, ctx.Err()
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
