// Package session puts the kernel together: it keeps sessions in the
// store, runs their turns with the agent, decides tool permissions, asks
// the user through approval events, and publishes what happens.
package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/shqingda/talos/kernel/agent"
	"github.com/shqingda/talos/kernel/perm"
	"github.com/shqingda/talos/kernel/sandbox"
	"github.com/shqingda/talos/kernel/skills"
	"github.com/shqingda/talos/kernel/store"
	"github.com/shqingda/talos/kernel/tools"
	"github.com/shqingda/talos/llm"
	"github.com/shqingda/talos/proto"
)

// Manager runs the sessions of one kernel.
type Manager struct {
	Store *store.Store
	// Open connects to an account's provider.
	Open func(account string) (llm.Provider, error)
	// Defaults fills an account and model a new session leaves empty.
	Defaults func() (account, model string)
	Tools    tools.Set
	Skills   skills.Loader
	// Sandboxed tells whether commands run in the sandbox.
	Sandboxed bool
	Clock     agent.Clock // real time when nil

	mu    sync.Mutex
	runs  map[string]*run
	subs  map[int]func(proto.Event)
	nextS int
	wg    sync.WaitGroup
}

// New makes a manager with the default tools.
func New(st *store.Store, open func(string) (llm.Provider, error)) *Manager {
	return &Manager{Store: st, Open: open, Tools: tools.Default(), Sandboxed: sandbox.Available()}
}

// run is a turn in progress.
type run struct {
	cancel    context.CancelFunc
	queued    []string
	approvals map[string]*pending
}

type pending struct {
	approval proto.Approval
	answer   chan proto.ApprovalAnswer
}

// Subscribe receives every event of every session until the returned
// function is called. fn must not block.
func (m *Manager) Subscribe(fn func(proto.Event)) (cancel func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.subs == nil {
		m.subs = map[int]func(proto.Event){}
	}
	id := m.nextS
	m.nextS++
	m.subs[id] = fn
	return func() {
		m.mu.Lock()
		delete(m.subs, id)
		m.mu.Unlock()
	}
}

func (m *Manager) publish(e proto.Event) {
	m.mu.Lock()
	subs := make([]func(proto.Event), 0, len(m.subs))
	for _, fn := range m.subs {
		subs = append(subs, fn)
	}
	m.mu.Unlock()
	for _, fn := range subs {
		fn(e)
	}
}

// record appends a durable event and publishes it.
func (m *Manager) record(ctx context.Context, sessionID, typ string, data any) error {
	ev, err := m.Store.Append(context.WithoutCancel(ctx), sessionID, typ, data)
	if err != nil {
		return err
	}
	m.publish(toProto(ev))
	return nil
}

// stream publishes an event that is not kept.
func (m *Manager) stream(sessionID, typ string, data any) {
	b, _ := json.Marshal(data)
	m.publish(proto.Event{SessionID: sessionID, Type: typ, Data: b, At: time.Now()})
}

func toProto(e store.Event) proto.Event {
	return proto.Event{SessionID: e.SessionID, Seq: e.Seq, Type: e.Type, Data: e.Data, At: e.At}
}

func toSession(x store.Session) proto.Session {
	return proto.Session{ID: x.ID, Title: x.Title, Cwd: x.Cwd, Account: x.Account, Model: x.Model,
		Effort: x.Effort, Mode: x.Mode, Created: x.Created, Updated: x.Updated}
}

// Create makes a session.
func (m *Manager) Create(ctx context.Context, p proto.SessionCreate) (proto.Session, error) {
	cwd, err := filepath.Abs(p.Cwd)
	if err != nil || p.Cwd == "" {
		return proto.Session{}, proto.Errorf("bad_request", "cwd is required")
	}
	if fi, err := os.Stat(cwd); err != nil || !fi.IsDir() {
		return proto.Session{}, proto.Errorf("bad_request", "%s is not a directory", cwd)
	}
	mode := cmpOr(p.Mode, string(perm.Ask))
	if !perm.Mode(mode).Valid() {
		return proto.Session{}, proto.Errorf("bad_request", "unknown permission mode %q", mode)
	}
	account, model := p.Account, p.Model
	if (account == "" || model == "") && m.Defaults != nil {
		a, mo := m.Defaults()
		if account == "" {
			account, model = a, cmpOr(model, mo)
		}
	}
	x := store.Session{ID: newID(), Title: p.Title, Cwd: cwd, Account: account, Model: model, Effort: p.Effort, Mode: mode}
	if err := m.Store.CreateSession(ctx, x); err != nil {
		return proto.Session{}, err
	}
	x, err = m.Store.GetSession(ctx, x.ID)
	if err != nil {
		return proto.Session{}, err
	}
	s := toSession(x)
	return s, m.record(ctx, s.ID, proto.EventSession, s)
}

// List lists sessions, most recent first.
func (m *Manager) List(ctx context.Context) ([]proto.Session, error) {
	xs, err := m.Store.Sessions(ctx)
	out := make([]proto.Session, len(xs))
	for i, x := range xs {
		out[i] = toSession(x)
	}
	return out, err
}

// Get returns a session's snapshot.
func (m *Manager) Get(ctx context.Context, id string) (proto.Snapshot, error) {
	x, err := m.session(ctx, id)
	if err != nil {
		return proto.Snapshot{}, err
	}
	rev, err := m.Store.Revision(ctx, id)
	if err != nil {
		return proto.Snapshot{}, err
	}
	snap := proto.Snapshot{Session: toSession(x), Revision: rev}
	m.mu.Lock()
	if r := m.runs[id]; r != nil {
		snap.Running = true
		snap.Queued = append(snap.Queued, r.queued...)
		for _, p := range r.approvals {
			snap.Approvals = append(snap.Approvals, p.approval)
		}
	}
	m.mu.Unlock()
	return snap, nil
}

func (m *Manager) session(ctx context.Context, id string) (store.Session, error) {
	x, err := m.Store.GetSession(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return x, proto.Errorf("not_found", "no session %s", id)
	}
	return x, err
}

// Update changes a session's title or settings. A new mode applies to the
// next tool call, even within a running turn.
func (m *Manager) Update(ctx context.Context, u proto.SessionUpdate) (proto.Session, error) {
	x, err := m.session(ctx, u.SessionID)
	if err != nil {
		return proto.Session{}, err
	}
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	set(&x.Title, u.Title)
	set(&x.Account, u.Account)
	set(&x.Model, u.Model)
	set(&x.Effort, u.Effort)
	set(&x.Mode, u.Mode)
	if !perm.Mode(x.Mode).Valid() {
		return proto.Session{}, proto.Errorf("bad_request", "unknown permission mode %q", x.Mode)
	}
	if err := m.Store.UpdateSession(ctx, x); err != nil {
		return proto.Session{}, err
	}
	x, _ = m.Store.GetSession(ctx, x.ID)
	s := toSession(x)
	return s, m.record(ctx, s.ID, proto.EventSession, s)
}

// Delete removes a session, stopping its turn.
func (m *Manager) Delete(ctx context.Context, id string) error {
	m.Cancel(id)
	return m.Store.DeleteSession(ctx, id)
}

// Cancel stops a session's turn, if one runs.
func (m *Manager) Cancel(id string) {
	m.mu.Lock()
	if r := m.runs[id]; r != nil {
		r.cancel()
	}
	m.mu.Unlock()
}

// Prompt starts a turn with the user's text, or queues the text into the
// running turn. receipt names the command, to record what became of it.
func (m *Manager) Prompt(ctx context.Context, p proto.Prompt, receipt string) (proto.PromptResult, error) {
	if strings.TrimSpace(p.Text) == "" {
		return proto.PromptResult{}, proto.Errorf("bad_request", "text is empty")
	}
	x, err := m.session(ctx, p.SessionID)
	if err != nil {
		return proto.PromptResult{}, err
	}
	m.mu.Lock()
	if r := m.runs[x.ID]; r != nil {
		r.queued = append(r.queued, p.Text)
		m.mu.Unlock()
		return proto.PromptResult{Queued: true}, nil
	}
	if m.runs == nil {
		m.runs = map[string]*run{}
	}
	runCtx, cancel := context.WithCancel(context.Background())
	r := &run{cancel: cancel, approvals: map[string]*pending{}}
	m.runs[x.ID] = r
	m.wg.Add(1)
	m.mu.Unlock()

	if receipt != "" {
		m.Store.PutReceipt(ctx, store.Receipt{ID: receipt, SessionID: x.ID, Method: proto.MethodPrompt, Status: "accepted"})
	}
	go func() {
		defer m.wg.Done()
		err := m.turn(runCtx, x, r, p.Text)
		cancel()
		m.mu.Lock()
		delete(m.runs, x.ID)
		m.mu.Unlock()
		if receipt != "" {
			rc := store.Receipt{ID: receipt, SessionID: x.ID, Method: proto.MethodPrompt, Status: "completed"}
			if err != nil && !errors.Is(err, context.Canceled) {
				rc.Status, rc.Error = "failed", err.Error()
			}
			m.Store.PutReceipt(context.Background(), rc)
		}
	}()
	return proto.PromptResult{}, nil
}

// turn runs one turn to its end and records it.
func (m *Manager) turn(ctx context.Context, x store.Session, r *run, text string) error {
	if err := m.record(ctx, x.ID, proto.EventTurnStart, proto.TurnStart{Text: text}); err != nil {
		return err
	}
	if x.Title == "" {
		x.Title = titleOf(text)
		m.Store.UpdateSession(context.WithoutCancel(ctx), x)
		x, _ = m.Store.GetSession(context.WithoutCancel(ctx), x.ID)
		m.record(ctx, x.ID, proto.EventSession, toSession(x))
	}
	usage, err := m.runAgent(ctx, x, r, text)
	end := proto.TurnEnd{Usage: usage}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			end.Stopped = true
		} else {
			end.Error = agent.Describe(err)
		}
	}
	m.record(ctx, x.ID, proto.EventTurnEnd, end)
	return err
}

func (m *Manager) runAgent(ctx context.Context, x store.Session, r *run, text string) (llm.Usage, error) {
	if x.Account == "" || x.Model == "" {
		return llm.Usage{}, errors.New("the session has no model; pick one")
	}
	provider, err := m.Open(x.Account)
	if err != nil {
		return llm.Usage{}, err
	}
	history, err := m.Store.Messages(ctx, x.ID)
	if err != nil {
		return llm.Usage{}, err
	}
	catalog := m.Skills.Load(x.Cwd)
	turnTools := append(append(tools.Set(nil), m.Tools...), tools.Skill(catalog))
	ag := &agent.Agent{
		Provider: provider,
		Tools:    &runner{m: m, sessionID: x.ID, run: r, tools: turnTools, env: tools.Env{Root: x.Cwd, Sandboxed: m.Sandboxed}},
		Sink:     sink{m: m, ctx: ctx, sessionID: x.ID},
		Clock:    m.Clock,
	}
	cfg := agent.Config{
		Model: x.Model, Effort: x.Effort, SessionID: x.ID, System: systemPrompt(x, m.Sandboxed) + catalog.Prompt,
		Steer: func() []string {
			m.mu.Lock()
			defer m.mu.Unlock()
			q := r.queued
			r.queued = nil
			return q
		},
	}
	return ag.Run(ctx, cfg, history, llm.Text(llm.User, text))
}

// sink records the agent's messages and streams the rest.
type sink struct {
	m         *Manager
	ctx       context.Context
	sessionID string
}

func (s sink) Emit(e agent.Event) {
	switch e.Type {
	case agent.Message:
		s.m.record(s.ctx, s.sessionID, proto.EventMessage, e.Message)
	case agent.TextDelta:
		s.m.stream(s.sessionID, proto.EventTextDelta, proto.Delta{Text: e.Delta})
	case agent.ThinkingDelta:
		s.m.stream(s.sessionID, proto.EventThinkingDelta, proto.Delta{Text: e.Delta})
	case agent.ToolStart:
		s.m.stream(s.sessionID, proto.EventToolStart, proto.ToolEvent{Call: *e.Call})
	case agent.ToolEnd:
		s.m.stream(s.sessionID, proto.EventToolEnd, proto.ToolEvent{Call: *e.Call, Result: e.Result})
	case agent.Retry:
		s.m.stream(s.sessionID, proto.EventRetry, proto.Retry{Attempt: e.Attempt, Error: e.Error})
	case agent.Usage:
		s.m.stream(s.sessionID, proto.EventUsage, e.Usage)
	}
}

// Close stops every turn and waits for them to end.
func (m *Manager) Close() {
	m.mu.Lock()
	for _, r := range m.runs {
		r.cancel()
	}
	m.mu.Unlock()
	m.wg.Wait()
}

// Recover tidies sessions a previous kernel left mid-turn: it ends their
// turns and answers their unanswered tool calls, so their conversations
// are well formed.
func (m *Manager) Recover(ctx context.Context) error {
	xs, err := m.Store.Sessions(ctx)
	if err != nil {
		return err
	}
	for _, x := range xs {
		evs, err := m.Store.Events(ctx, x.ID, 0, 0, proto.EventTurnStart, proto.EventTurnEnd)
		if err != nil {
			return err
		}
		if len(evs) == 0 || evs[len(evs)-1].Type != proto.EventTurnStart {
			continue
		}
		msgs, err := m.Store.Messages(ctx, x.ID)
		if err != nil {
			return err
		}
		if n := len(msgs); n > 0 && msgs[n-1].Role == llm.Assistant {
			if calls := msgs[n-1].ToolCalls(); len(calls) > 0 {
				res := llm.Message{Role: llm.User}
				for _, c := range calls {
					res.Parts = append(res.Parts, llm.Part{ToolResult: &llm.ToolResult{CallID: c.ID, Name: c.Name, Content: agent.Interrupted, IsError: true}})
				}
				m.record(ctx, x.ID, proto.EventMessage, res)
			}
		}
		m.record(ctx, x.ID, proto.EventTurnEnd, proto.TurnEnd{Error: "Talos quit before the turn finished."})
	}
	return nil
}

// Answer delivers the user's answer to an approval.
func (m *Manager) Answer(a proto.ApprovalAnswer) error {
	m.mu.Lock()
	r := m.runs[a.SessionID]
	var p *pending
	if r != nil {
		p = r.approvals[a.ApprovalID]
	}
	m.mu.Unlock()
	if p == nil {
		return proto.Errorf("not_found", "no approval %s waiting", a.ApprovalID)
	}
	select {
	case p.answer <- a:
		return nil
	default:
		return proto.Errorf("busy", "approval %s was already answered", a.ApprovalID)
	}
}

func titleOf(text string) string {
	t := strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(t) > 60 {
		t = string([]rune(t)[:57]) + "…"
	}
	return t
}

func systemPrompt(x store.Session, sandboxed bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are Talos, a coding agent working in %s on %s.\n", x.Cwd, time.Now().Format("2006-01-02"))
	b.WriteString("Use the tools to read, search and change files and to run commands; read a file before editing it. ")
	b.WriteString("Keep changes focused, run the project's tests after changing code, and answer briefly.\n")
	if sandboxed {
		b.WriteString(sandbox.Describe() + "\n")
	}
	return b.String()
}

func newID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
