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
	"github.com/shqingda/stratum/kernel/mcp"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/shqingda/stratum/kernel/agent"
	"github.com/shqingda/stratum/kernel/agents"
	"github.com/shqingda/stratum/kernel/changes"
	"github.com/shqingda/stratum/kernel/lsp"
	"github.com/shqingda/stratum/kernel/perm"
	"github.com/shqingda/stratum/kernel/sandbox"
	"github.com/shqingda/stratum/kernel/skills"
	"github.com/shqingda/stratum/kernel/store"
	"github.com/shqingda/stratum/kernel/tools"
	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/proto"
)

// Manager runs the sessions of one kernel.
type Manager struct {
	Store   *store.Store
	changes *changes.Log
	// Open connects to an account's provider.
	Open func(account string) (llm.Provider, error)
	// CompactAt, when set, is how many tokens of history start a
	// compaction, in place of three quarters of the model's context.
	CompactAt int
	// Defaults fills an account and model a new session leaves empty.
	Defaults func() (account, model string)
	// Accounts lists the signed-in accounts, for backups.
	Accounts    func() []string
	Tools       tools.Set
	Skills      skills.Loader
	MCP         *mcp.Hub // nil without MCP
	WorktreeDir string   // directory holding session git worktrees; TempDir when empty
	// Sandboxed tells whether commands run in the sandbox.
	Sandboxed bool
	Clock     agent.Clock // real time when nil

	mu   sync.Mutex
	runs map[string]*run
	// allowed are the rules the user allowed for the rest of a session,
	// by its ID, until Stratum quits.
	allowed map[string][]proto.Rule
	subs    map[int]func(proto.Event)
	nextS   int
	wg      sync.WaitGroup
}

// New makes a manager with the default tools.
func New(st *store.Store, open func(string) (llm.Provider, error)) *Manager {
	m := &Manager{Store: st, changes: changes.New(st), Open: open, Tools: tools.Default(), Sandboxed: sandbox.Available()}
	m.Skills.Disabled = func(name string) bool { return m.skillDisabled(context.Background(), name) }
	m.Skills.ExtraSkillDirs = m.pluginSkillDirs
	return m
}

// run is a turn in progress.
type run struct {
	turnSeq   int64
	cancel    context.CancelFunc
	queued    []waiting // prompts sent while it runs, oldest first
	nextQ     int       // numbers the queued prompts
	approvals map[string]*pending
}

// waiting is a prompt in a run's queue.
type waiting struct {
	id    string
	steer bool
	p     proto.Prompt
}

func (w waiting) item() proto.Queued {
	return proto.Queued{ID: w.id, Text: w.p.Text, Images: len(w.p.Images), Steer: w.steer}
}

func items(ws []waiting) []proto.Queued {
	var out []proto.Queued
	for _, w := range ws {
		out = append(out, w.item())
	}
	return out
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

// Broadcast publishes an event of no session, not kept.
func (m *Manager) Broadcast(typ string, data any) { m.stream("", typ, data) }

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
		Effort: x.Effort, Mode: x.Mode, Agent: x.Agent, ParentID: x.ParentID, Created: x.Created, Updated: x.Updated, Pinned: x.Pinned, Archived: x.Archived, Mark: x.Mark}
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
	set, err := m.Settings(ctx)
	if err != nil {
		return proto.Session{}, err
	}
	mode := cmpOr(p.Mode, cmpOr(set.Mode, string(perm.Ask)))
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
	x := store.Session{ID: newID(), Title: p.Title, Cwd: cwd, Account: account, Model: model, Effort: cmpOr(p.Effort, set.Effort), Mode: mode}
	if p.Worktree {
		if err := m.enableWorktree(ctx, &x); err != nil {
			return proto.Session{}, err
		}
	}
	if err := m.Store.CreateSession(ctx, x); err != nil {
		if p.Worktree {
			_ = m.disableWorktree(ctx, &x, false)
		}
		return proto.Session{}, err
	}
	x, err = m.Store.GetSession(ctx, x.ID)
	if err != nil {
		return proto.Session{}, err
	}
	s := m.sessionProto(ctx, x)
	return s, m.record(ctx, s.ID, proto.EventSession, s)
}

// Settings reads what new sessions start with.
func (m *Manager) Settings(ctx context.Context) (proto.Settings, error) {
	var s proto.Settings
	b, err := m.Store.Setting(ctx, "defaults")
	if err == nil && b != nil {
		err = json.Unmarshal(b, &s)
	}
	return s, err
}

// SetSettings changes what new sessions start with.
func (m *Manager) SetSettings(ctx context.Context, s proto.Settings) (proto.Settings, error) {
	if s.Mode != "" && !perm.Mode(s.Mode).Valid() {
		return proto.Settings{}, proto.Errorf("bad_request", "unknown permission mode %q", s.Mode)
	}
	if s.Account == "" {
		s.Model = ""
	}
	b, _ := json.Marshal(s)
	return s, m.Store.SetSetting(ctx, "defaults", b)
}

// List lists sessions, most recent first.
func (m *Manager) List(ctx context.Context) ([]proto.Session, error) {
	xs, err := m.Store.Sessions(ctx)
	out := make([]proto.Session, len(xs))
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, x := range xs {
		out[i] = m.sessionProto(ctx, x)
		if r := m.runs[x.ID]; r != nil {
			out[i].State = "run"
			if len(r.approvals) > 0 {
				out[i].State = "wait"
			}
		}
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
	snap := proto.Snapshot{Session: m.sessionProto(ctx, x), Revision: rev}
	m.mu.Lock()
	if r := m.runs[id]; r != nil {
		snap.Running = true
		for _, q := range r.queued {
			snap.Queued = append(snap.Queued, q.item())
		}
		for _, p := range r.approvals {
			snap.Approvals = append(snap.Approvals, p.approval)
		}
	}
	m.mu.Unlock()
	if d, err := m.Store.Setting(ctx, store.DraftKey(id)); err == nil {
		snap.Draft = string(d)
	}
	return snap, nil
}

// SetDraft keeps what the user is writing to a session.
func (m *Manager) SetDraft(ctx context.Context, d proto.DraftSet) error {
	if _, err := m.session(ctx, d.SessionID); err != nil {
		return err
	}
	if d.Text == "" {
		return m.Store.DeleteSetting(ctx, store.DraftKey(d.SessionID))
	}
	return m.Store.SetSetting(ctx, store.DraftKey(d.SessionID), []byte(d.Text))
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
	if u.Agent != nil {
		name := strings.TrimSpace(*u.Agent)
		if name == "build" {
			name = ""
		}
		if a, ok := agents.Named(m.agents(x.Cwd), cmpOr(name, "build")); !ok || !a.Primary() {
			return proto.Session{}, proto.Errorf("bad_request", "%q is not a primary agent", name)
		}
		x.Agent = name
	}
	if u.Worktree != nil {
		if *u.Worktree {
			if err := m.enableWorktree(ctx, &x); err != nil {
				return proto.Session{}, err
			}
		} else {
			if err := m.disableWorktree(ctx, &x, true); err != nil {
				return proto.Session{}, err
			}
		}
	}
	if err := m.Store.UpdateSession(ctx, x); err != nil {
		return proto.Session{}, err
	}
	x, _ = m.Store.GetSession(ctx, x.ID)
	s := m.sessionProto(ctx, x)
	return s, m.record(ctx, s.ID, proto.EventSession, s)
}

// Flag pins, archives or marks a session, as the sidebar shows it.
func (m *Manager) Flag(ctx context.Context, f proto.SessionFlag) (proto.Session, error) {
	x, err := m.session(ctx, f.SessionID)
	if err != nil {
		return proto.Session{}, err
	}
	if f.Pinned != nil {
		x.Pinned = *f.Pinned
	}
	if f.Archived != nil {
		x.Archived = *f.Archived
	}
	if f.Mark != nil {
		if *f.Mark != "" && *f.Mark != "unread" && *f.Mark != "fail" {
			return proto.Session{}, proto.Errorf("bad_request", "unknown mark %q", *f.Mark)
		}
		x.Mark = *f.Mark
	}
	if err := m.Store.SetFlags(ctx, x); err != nil {
		return proto.Session{}, err
	}
	s := m.sessionProto(ctx, x)
	return s, m.record(ctx, s.ID, proto.EventSession, s)
}

// Fork creates a new session branched off an existing session, copying its
// conversation history and settings.
func (m *Manager) Fork(ctx context.Context, ref proto.SessionRef) (proto.Session, error) {
	src, err := m.session(ctx, ref.SessionID)
	if err != nil {
		return proto.Session{}, err
	}
	cwd := src.Cwd
	if meta, ok := m.worktreeMetaOf(ctx, src.ID); ok {
		cwd = meta.Repo
	}
	title := src.Title
	if title != "" {
		title += " (fork)"
	}
	dst := store.Session{
		ID:       newID(),
		Title:    title,
		Cwd:      cwd,
		Account:  src.Account,
		Model:    src.Model,
		Effort:   src.Effort,
		Mode:     src.Mode,
		ParentID: src.ID,
	}
	if err := m.Store.CreateSession(ctx, dst); err != nil {
		return proto.Session{}, err
	}
	evs, err := m.Store.Events(ctx, src.ID, 0, 0)
	if err != nil {
		return proto.Session{}, err
	}
	for _, e := range evs {
		switch e.Type {
		case proto.EventSession, proto.EventApprovalRequest, proto.EventApprovalResolved:
			continue
		}
		if _, err := m.Store.Append(context.WithoutCancel(ctx), dst.ID, e.Type, json.RawMessage(e.Data)); err != nil {
			return proto.Session{}, err
		}
	}
	dst, err = m.Store.GetSession(ctx, dst.ID)
	if err != nil {
		return proto.Session{}, err
	}
	s := m.sessionProto(ctx, dst)
	return s, m.record(ctx, s.ID, proto.EventSession, s)
}

// Delete removes a session, stopping its turn and cleaning up any worktree.
func (m *Manager) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	if r := m.runs[id]; r != nil {
		r.cancel()
	}
	m.mu.Unlock()
	if x, err := m.Store.GetSession(ctx, id); err == nil {
		_ = m.disableWorktree(ctx, &x, false)
	}
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

// next takes the prompt the next turn starts with: the steering ones
// left over, joined, else the oldest waiting. m.mu is held.
func (r *run) next() (proto.Prompt, bool) {
	var p proto.Prompt
	rest := r.queued[:0:0]
	for _, w := range r.queued {
		if !w.steer {
			rest = append(rest, w)
			continue
		}
		if p.SessionID == "" {
			p = w.p
			continue
		}
		p.Text += "\n\n" + w.p.Text
		p.Images = append(p.Images, w.p.Images...)
	}
	if p.SessionID != "" {
		r.queued = rest
		return p, true
	}
	if len(r.queued) == 0 {
		return p, false
	}
	p = r.queued[0].p
	r.queued = r.queued[1:]
	return p, true
}

// EditQueue removes a waiting prompt, or makes it join the running turn.
func (m *Manager) EditQueue(e proto.QueueEdit) error {
	m.mu.Lock()
	r := m.runs[e.SessionID]
	if r == nil {
		m.mu.Unlock()
		return proto.Errorf("not_found", "no turn runs")
	}
	i := slices.IndexFunc(r.queued, func(w waiting) bool { return w.id == e.ID })
	if i < 0 {
		m.mu.Unlock()
		return proto.Errorf("not_found", "no such prompt waits")
	}
	switch e.Action {
	case "remove":
		r.queued = slices.Delete(r.queued, i, i+1)
	case "steer":
		r.queued[i].steer = true
	default:
		m.mu.Unlock()
		return proto.Errorf("bad_request", "unknown action %q", e.Action)
	}
	q := items(r.queued)
	m.mu.Unlock()
	m.stream(e.SessionID, proto.EventQueue, proto.Queue{Items: q})
	return nil
}

// Prompt starts a turn with the user's text, or queues the text while a
// turn runs. receipt names the command, to record what became of it.
func (m *Manager) Prompt(ctx context.Context, p proto.Prompt, receipt string) (proto.PromptResult, error) {
	if strings.TrimSpace(p.Text) == "" && len(p.Images) == 0 {
		return proto.PromptResult{}, proto.Errorf("bad_request", "text is empty")
	}
	x, err := m.session(ctx, p.SessionID)
	if err != nil {
		return proto.PromptResult{}, err
	}
	m.mu.Lock()
	if r := m.runs[x.ID]; r != nil {
		r.nextQ++
		w := waiting{id: fmt.Sprintf("q%d", r.nextQ), steer: p.Steer, p: p}
		r.queued = append(r.queued, w)
		q := items(r.queued)
		m.mu.Unlock()
		m.stream(x.ID, proto.EventQueue, proto.Queue{Items: q})
		return proto.PromptResult{Queued: true, ID: w.id}, nil
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
	go m.drive(runCtx, x, r, func(ctx context.Context) error {
		err := m.turn(ctx, x, r, p)
		if receipt != "" {
			rc := store.Receipt{ID: receipt, SessionID: x.ID, Method: proto.MethodPrompt, Status: "completed"}
			if err != nil && !errors.Is(err, context.Canceled) {
				rc.Status, rc.Error = "failed", err.Error()
			}
			m.Store.PutReceipt(context.Background(), rc)
		}
		return err
	})
	return proto.PromptResult{}, nil
}

// Shell runs a command the user typed.
func (m *Manager) Shell(ctx context.Context, sh proto.ShellRun) error {
	if strings.TrimSpace(sh.Command) == "" {
		return proto.Errorf("bad_request", "command is empty")
	}
	x, err := m.session(ctx, sh.SessionID)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if m.runs[x.ID] != nil {
		m.mu.Unlock()
		return proto.Errorf("busy", "a turn is running")
	}
	if m.runs == nil {
		m.runs = map[string]*run{}
	}
	runCtx, cancel := context.WithCancel(context.Background())
	r := &run{cancel: cancel, approvals: map[string]*pending{}}
	m.runs[x.ID] = r
	m.wg.Add(1)
	m.mu.Unlock()
	m.stream(x.ID, proto.EventShellStart, sh)
	go m.drive(runCtx, x, r, func(ctx context.Context) error {
		// The user typed it, so it asks nothing; outside full access it
		// stays in the sandbox all the same.
		args, _ := json.Marshal(map[string]any{"command": sh.Command, "unrestricted": x.Mode == "full"})
		out, err := tools.Shell.Run(ctx, tools.Env{Root: x.Cwd, Sandboxed: m.Sandboxed}, args)
		rec := proto.Shell{Command: sh.Command, Output: out}
		switch {
		case ctx.Err() != nil:
			rec.Stopped, err = true, ctx.Err()
		case err != nil:
			rec.Output = err.Error()
		}
		m.record(ctx, x.ID, proto.EventShell, rec)
		return err
	})
	return nil
}

// drive runs first, a turn or a command of the user's, then the turns
// the prompts waiting meanwhile start, until none waits or one ends badly;
// then the session is free again.
func (m *Manager) drive(ctx context.Context, x store.Session, r *run, first func(context.Context) error) {
	defer m.wg.Done()
	err := first(ctx)
	// What ended well takes the next prompt waiting: those that meant to
	// join it and came too late first, all together, else the oldest one.
	for err == nil {
		m.mu.Lock()
		next, ok := r.next()
		q := items(r.queued)
		m.mu.Unlock()
		if !ok {
			break
		}
		m.stream(x.ID, proto.EventQueue, proto.Queue{Items: q})
		if x2, e := m.Store.GetSession(context.WithoutCancel(ctx), x.ID); e == nil {
			x = x2 // its model or mode may have changed meanwhile
		}
		err = m.turn(ctx, x, r, next)
	}
	r.cancel()
	m.mu.Lock()
	delete(m.runs, x.ID)
	back := items(r.queued)
	r.queued = nil
	m.mu.Unlock()
	// Stopped or failed, the turns go no further: what still waits goes
	// back to the user rather than running unasked.
	if len(back) > 0 {
		m.stream(x.ID, proto.EventQueue, proto.Queue{Returned: back})
	}
}

// turn runs one turn to its end and records it.
func (m *Manager) turn(ctx context.Context, x store.Session, r *run, p proto.Prompt) error {
	text := p.Text
	start, err := m.Store.Append(context.WithoutCancel(ctx), x.ID, proto.EventTurnStart, proto.TurnStart{Text: text})
	if err != nil {
		return err
	}
	r.turnSeq = start.Seq
	m.publish(toProto(start))
	if x.Title == "" {
		x.Title = titleOf(text)
		m.Store.UpdateSession(context.WithoutCancel(ctx), x)
		x, _ = m.Store.GetSession(context.WithoutCancel(ctx), x.ID)
		m.record(ctx, x.ID, proto.EventSession, m.sessionProto(ctx, x))
		m.retitle(x.ID, x.Title, text)
	}
	usage, err := m.runAgent(ctx, x, r, p)
	end := proto.TurnEnd{Usage: usage}
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			end.Stopped = true
		case errors.Is(err, agent.ErrStepLimit):
			end.Limit = true
		default:
			end.Error = agent.Describe(err)
		}
	}
	m.record(ctx, x.ID, proto.EventTurnEnd, end)
	// It is unseen until the user looks: the app clears the mark then.
	if !end.Stopped {
		mark := "unread"
		if end.Error != "" {
			mark = "fail"
		}
		m.Flag(context.WithoutCancel(ctx), proto.SessionFlag{SessionID: x.ID, Mark: &mark})
	}
	return err
}

func (m *Manager) runAgent(ctx context.Context, x store.Session, r *run, p proto.Prompt) (llm.Usage, error) {
	if x.Account == "" || x.Model == "" {
		return llm.Usage{}, errors.New("the session has no model; pick one")
	}
	opened, err := m.Open(x.Account)
	if err != nil {
		return llm.Usage{}, err
	}
	set, _ := m.Settings(ctx)
	list := m.agents(x.Cwd)
	lead := m.primary(list, x)
	main := target{x.Account, x.Model, opened}
	if t, ok := m.agentTarget(x.Account, lead.Model); ok {
		main = t
	}
	chain := &fallback{targets: []target{main}, moved: func(from, to target, err error) {
		m.record(context.WithoutCancel(ctx), x.ID, proto.EventFallback, proto.Fallback{
			From: from.account + "/" + from.model, To: to.account + "/" + to.model, Error: err.Error()})
	}}
	if set.Fallback {
		chain.targets = append(chain.targets, m.backups(x.Account)...)
	}
	var provider llm.Provider = chain
	subProvider, subModel, subEffort := provider, x.Model, x.Effort
	if sub, ok := m.role(set.Sub); ok {
		subProvider, subModel, subEffort = sub.provider, sub.model, set.Sub.Effort
	}
	history, err := m.history(ctx, x.ID)
	if err != nil {
		return llm.Usage{}, err
	}
	catalog := m.Skills.Load(x.Cwd)
	turnTools := append(append(tools.Set(nil), m.Tools...), tools.Skill(catalog))
	if m.MCP != nil {
		turnTools = append(turnTools, m.MCP.Tools(ctx, x.Cwd)...)
	}
	turnTools = agents.FilterTools(turnTools, lead)
	system := systemPrompt(x, m.Sandboxed) + catalog.Prompt + agentsPrompt(list)
	if lead.Prompt != "" {
		system += "\n\n" + lead.Prompt
	}
	var used llm.Usage
	if at := m.compactAt(x.Account, x.Model); p.Compact && len(history) > 0 || at > 0 && tokens(history) > at {
		req := llm.Request{Model: x.Model, System: system, Messages: history, Tools: turnTools.Specs(), Effort: x.Effort, SessionID: x.ID}
		compactWith := provider
		if light, ok := m.role(set.Light); ok {
			compactWith, req.Model, req.Effort = light.provider, light.model, set.Light.Effort
		}
		short, u, err := m.compact(ctx, compactWith, req, x.ID, p.Compact)
		used = u
		switch {
		case ctx.Err() != nil:
			return used, ctx.Err()
		case p.Compact:
			return used, err
		case err == nil:
			history = short
		} // a failed summary leaves the history whole
	}
	if p.Compact {
		return used, nil
	}
	env := tools.Env{Root: x.Cwd, Sandboxed: m.Sandboxed, RecordChange: func(path string, before, after []byte) error {
		return m.changes.Record(context.WithoutCancel(ctx), x.ID, r.turnSeq, path, before, after)
	}}
	var subMu sync.Mutex
	task := func(ctx context.Context, callID, taskID, prompt, agentName string) (string, error) {
		s := &subSink{m: m, sessionID: x.ID, callID: callID, task: taskID}
		subTools := turnTools.Without(tools.Task.Name, tools.RunTasks.Name, tools.AskUser.Name)
		subSys := system + subPrompt
		cfg := agent.Config{Model: subModel, Effort: subEffort, SessionID: x.ID, System: subSys}
		via := subProvider
		var policy *agents.Agent
		if agentName != "" {
			custom, ok := agents.Named(list, agentName)
			if !ok || !custom.Sub() {
				return "", fmt.Errorf("there is no subagent %q", agentName)
			}
			policy = &custom
			subTools = agents.FilterTools(subTools, custom)
			if custom.Prompt != "" {
				cfg.System += "\n\n" + custom.Prompt
			}
			cfg.MaxSteps = custom.MaxSteps
			cfg.Temperature, cfg.TopP = custom.Temperature, custom.TopP
			if t, ok := m.agentTarget(x.Account, custom.Model); ok {
				via, cfg.Model = t.provider, t.model
			}
		}
		sa := &agent.Agent{
			Provider: via,
			Tools:    &runner{m: m, sessionID: x.ID, run: r, tools: subTools, env: env, policy: policy},
			Sink:     s,
			Clock:    m.Clock,
		}
		u, err := sa.Run(ctx, cfg, nil, llm.Text(llm.User, prompt))
		subMu.Lock()
		used = used.Add(u)
		subMu.Unlock()
		return s.report, err
	}
	ag := &agent.Agent{
		Provider:   provider,
		Tools:      &runner{m: m, sessionID: x.ID, run: r, tools: turnTools, env: env, task: task, policy: &lead},
		Sink:       sink{m: m, ctx: ctx, sessionID: x.ID},
		Clock:      m.Clock,
		Concurrent: func(name string) bool { return name == tools.Task.Name || name == tools.RunTasks.Name },
	}
	cfg := agent.Config{
		Model: main.model, Effort: x.Effort, SessionID: x.ID, System: system,
		Temperature: lead.Temperature, TopP: lead.TopP, MaxSteps: lead.MaxSteps,
		Steer: func() []llm.Part {
			m.mu.Lock()
			var steered []proto.Prompt
			rest := r.queued[:0:0]
			for _, w := range r.queued {
				if w.steer {
					steered = append(steered, w.p)
				} else {
					rest = append(rest, w)
				}
			}
			changed := len(rest) != len(r.queued)
			r.queued = rest
			q := items(rest)
			m.mu.Unlock()
			if changed {
				m.stream(x.ID, proto.EventQueue, proto.Queue{Items: q})
			}
			var parts []llm.Part
			for _, sp := range steered {
				sp.Text = m.expandMentions(ctx, x, sp.Text)
				parts = append(parts, promptParts(sp)...)
			}
			return parts
		},
	}
	asked := p
	asked.Text = m.expandMentions(ctx, x, p.Text)
	u, err := ag.Run(ctx, cfg, history, llm.Message{Role: llm.User, Parts: promptParts(asked)})
	if err == nil && ctx.Err() == nil {
		m.runStopHooks(ctx, x.Cwd, x.ID)
	}
	subMu.Lock()
	defer subMu.Unlock()
	return used.Add(u), err
}

const subPrompt = `

You are a sub-agent: another agent gave you the task below and reads only your last reply. Do the task with your tools, then reply with a short report of what you found or did, with the paths and facts it needs. Do not ask questions; decide yourself.`

// subSink streams a sub-agent's steps as progress of its task call, and
// keeps its last words as the report. Its messages are not recorded.
type subSink struct {
	m                       *Manager
	sessionID, callID, task string
	report                  string
}

func (s *subSink) Emit(e agent.Event) {
	switch e.Type {
	case agent.Message:
		if e.Message.Role == llm.Assistant {
			if t := e.Message.PlainText(); t != "" {
				s.report = t
			}
		}
	case agent.ToolStart:
		s.m.stream(s.sessionID, proto.EventTaskStep, proto.TaskStep{CallID: s.callID, Task: s.task, Call: *e.Call})
	}
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
	if m.MCP != nil {
		m.MCP.Close()
	}
	lsp.Default.Close()
}

// Recover tidies sessions a previous kernel left mid-turn: it ends their
// turns and answers their unanswered tool calls, so their conversations
// are well formed.
func (m *Manager) Recover(ctx context.Context) error {
	ids, err := m.Store.OpenTurnSessions(ctx, proto.EventTurnStart, proto.EventTurnEnd)
	if err != nil {
		return err
	}
	for _, id := range ids {
		msgs, err := m.Store.Messages(ctx, id)
		if err != nil {
			return err
		}
		if n := len(msgs); n > 0 && msgs[n-1].Role == llm.Assistant {
			if calls := msgs[n-1].ToolCalls(); len(calls) > 0 {
				res := llm.Message{Role: llm.User}
				for _, c := range calls {
					res.Parts = append(res.Parts, llm.Part{ToolResult: &llm.ToolResult{CallID: c.ID, Name: c.Name, Content: agent.Interrupted, IsError: true}})
				}
				m.record(ctx, id, proto.EventMessage, res)
			}
		}
		m.record(ctx, id, proto.EventTurnEnd, proto.TurnEnd{Error: "Stratum quit before the turn finished."})
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
	fmt.Fprintf(&b, "You are Stratum, a coding agent working in %s on %s.\n", x.Cwd, time.Now().Format("2006-01-02"))
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

// promptParts is what the user sent: the text, then the images.
func promptParts(p proto.Prompt) []llm.Part {
	var parts []llm.Part
	if p.Text != "" {
		parts = append(parts, llm.Part{Text: p.Text})
	}
	for i := range p.Images {
		parts = append(parts, llm.Part{Image: &p.Images[i]})
	}
	return parts
}

func agentsPrompt(list []agents.Agent) string {
	var b strings.Builder
	for _, a := range list {
		if !a.Sub() {
			continue
		}
		if b.Len() == 0 {
			b.WriteString("\nAvailable subagents for task/run_tasks (pass via the `agent` field):\n")
		}
		fmt.Fprintf(&b, "- %s: %s\n", a.Name, a.Description)
	}
	return b.String()
}
