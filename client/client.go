// Package client is the app's state for one kernel connection and one
// workspace, built only from frames the kernel sent. It knows nothing of
// windows: the app calls it on the main thread and sends the commands it
// returns, once each, in order.
//
// The contract:
//   - frames are applied in wire order;
//   - a session's state is rebuilt from its log: session.get gives the
//     session and its queued prompts, events (paged) give the rest;
//   - durable events already applied (seq ≤ cursor) are ignored, and a gap
//     reads the log again from the cursor;
//   - events that arrive while the log is read are buffered (at most 256)
//     and applied after it if newer;
//   - a command left unanswered by a disconnect is unknown, never resent;
//     after reconnecting, receipt.get says what became of it;
//   - the kernel handles commands concurrently and may answer out of order,
//     so a command that depends on another is sent only once that one has
//     been answered.
package client

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/shqingda/talos/llm"
	"github.com/shqingda/talos/locales"
	"github.com/shqingda/talos/proto"
)

// Phase is where the connection stands.
type Phase int

const (
	Starting    Phase = iota // kernel launched, no ready frame yet
	Handshaking              // initialize and the session list in flight
	Syncing                  // a session's log is being read
	Ready                    // in step with the kernel; commands may be sent
	Disconnected
)

func (p Phase) String() string {
	return [...]string{"Starting", "Connecting", "Syncing", "Ready", "Disconnected"}[p]
}

// Role of a transcript entry.
type Role string

const (
	User      Role = "user"
	Assistant Role = "assistant"
	Tool      Role = "tool"
	Notice    Role = "notice"
	Thinking  Role = "thinking" // reasoning the model showed
)

// Entry is one row of the transcript.
type Entry struct {
	Key    int   // stable list key
	Seq    int64 // event of a stored message; 0 for live rows
	Role   Role
	Text   string
	Images []llm.Image // a user message's
	Detail string      // a tool call's arguments, summarized
	// Steps are the tool calls a running task's sub-agent made, as
	// "name\tdetail"; only while it runs.
	Steps []string
	// Tasks are a run_tasks call's tasks, as its arguments give them.
	Tasks  []*GraphTask
	Output string // a tool call's result
	Done   bool
	CallID string
	Failed bool
	// Skipped is a call the user declined, which did not run.
	Skipped bool
	// Note is what a notice is about: "fail", "fallback", "retry",
	// "stop", "limit" (the turn ran out of steps) or "compact".
	Note string
	Args json.RawMessage // a tool call's original arguments
	Turn int             // index in Client.Turns of the turn it is in; -1 before any
	// Typed is a command the user ran themselves (!command), in no turn.
	Typed bool
	// Took is how long a thinking entry streamed, 0 when it was read
	// from the log.
	Took time.Duration
	at   time.Time // when a streaming thinking entry started
}

// Turn is one turn of the current session, as its log tells it.
type Turn struct {
	Seq        int64 // of its turn.start event, as proto.ChangeTurn.TurnSeq
	Start, End time.Time
	Done       bool
	Usage      llm.Usage
}

// Unknown is a mutation whose outcome a disconnect left unknown.
type Unknown struct {
	ID, Method string
	// Outcome is empty until a receipt was read: then the receipt's
	// status, or "not received".
	Outcome string
}

const (
	maxBuffered = 256
	pageSize    = 500
)

type pending struct {
	method   string
	mutation bool
	done     func(json.RawMessage) []proto.Command
	failed   func(*proto.Error) []proto.Command
}

// Client is the state of one workspace on one kernel.
type Client struct {
	Cwd      string
	Phase    Phase
	Err      string
	Instance string

	Sessions []proto.Session // all of them, newest first
	Current  *proto.Session
	// Marks is what the list shows by sessions other than Current: "run",
	// "wait", "unread" or "fail".
	Marks map[string]string

	Entries []*Entry
	Busy    bool
	Queued  []proto.Queued // prompts waiting while the turn runs
	// Returned are prompts that were still waiting when a session's turns
	// stopped, by session, for the composer to take back.
	Returned map[string][]string
	// Draft is what the user was writing to the current session when it
	// was last left, as the kernel kept it.
	Draft string
	// Files are the workspace's files FindFiles last found, for FilesFor.
	Files    []string
	FilesFor string
	// Older is the log having events before the ones shown.
	Older, LoadingOlder bool
	// MCP is how the MCP servers are for the folder MCPFor, nil until
	// read.
	MCP         *proto.MCPList
	MCPFor      string
	Preview     *proto.PreviewSnapshot
	Automations      *proto.AutomationList
	AutomationAlerts []proto.Automation
	Hooks            *proto.HooksList
	Plugins     *proto.PluginList
	// Hits are the messages matching HitsFor, nil before an answer.
	Hits      []proto.SearchHit
	HitsFor   string
	Approvals []proto.Approval // waiting for the user
	// Answering holds approvals whose answer is in flight.
	Answering map[string]bool
	Usage     llm.Usage // of the current session's turns this connection saw end
	Turns     []Turn    // of the current session, oldest first
	// LastInput is the input tokens of the turn's latest request this
	// connection saw: what of the context the session fills.
	LastInput int

	Changes   Changes
	Workspace Workspace

	Accounts []proto.Account
	SignIn   *SignIn
	Settings *proto.Settings // of new sessions, once read
	Models   []proto.Model   // of the account last asked about
	Unknown  []Unknown

	launch    string
	nextCmd   int
	nextKey   int
	pending   map[string]*pending
	cursor    int64         // last durable event applied
	log       []proto.Event // durable events shown, oldest first
	first     int64         // seq of the oldest of them
	trim      bool          // the next read starts mid-log
	searching string        // the query last sent
	buffer    []proto.Event
	overflow  bool
	streaming *Entry
	thinking  *Entry // the thinking entry streaming in
	// streamBuf and thinkBuf gather the deltas of streaming and thinking,
	// whose Text they hand out, so that a long reply is not copied whole
	// for each delta.
	streamBuf, thinkBuf strings.Builder
	thought   *Entry // the streamed thinking entry its message has not yet sent whole
	want      string // session to show on (re)connect
	modelsReq string
	afterSync func() []proto.Command
	decoded   any
}

// New returns a client for the workspace at cwd that has not connected
// yet. Command ids start with a random prefix, so that they stay unique
// across launches, as receipts require.
func New(cwd string) *Client {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return newClient(cwd, hex.EncodeToString(b))
}

func newClient(cwd, launch string) *Client {
	c := &Client{Cwd: cwd, launch: launch, pending: map[string]*pending{}, Answering: map[string]bool{}, Marks: map[string]string{}}
	c.resetChanges()
	return c
}

// CanAct reports whether commands that change things may be sent now.
func (c *Client) CanAct() bool { return c.Phase == Ready && c.Current != nil }

func (c *Client) send(method string, params any, p *pending) proto.Command {
	c.nextCmd++
	id := c.launch + "-" + strconv.Itoa(c.nextCmd)
	p.method = method
	c.pending[id] = p
	cmd := proto.Command{ID: id, Method: method}
	if params != nil {
		cmd.Params, _ = json.Marshal(params)
	}
	return cmd
}

func (c *Client) read(method string, params any, done func(json.RawMessage) []proto.Command) proto.Command {
	return c.send(method, params, &pending{done: done})
}

func (c *Client) mutate(method string, params any, done func(json.RawMessage) []proto.Command) proto.Command {
	return c.send(method, params, &pending{mutation: true, done: done})
}

func (c *Client) add(e *Entry) *Entry {
	c.nextKey++
	e.Key = c.nextKey
	e.Turn = len(c.Turns) - 1
	c.Entries = append(c.Entries, e)
	return e
}

func (c *Client) notice(note, format string, args ...any) {
	c.add(&Entry{Role: Notice, Note: note, Text: fmt.Sprintf(format, args...), Done: true})
}

// Receive applies one frame in wire order and returns the commands to send next.
func (c *Client) Receive(f proto.Frame) []proto.Command {
	switch f.Type {
	case proto.FrameReady:
		return c.ready(f)
	case proto.FrameResponse:
		return c.response(f)
	case proto.FrameEvent:
		if f.Event == nil {
			return nil
		}
		switch f.Event.Type {
		case proto.EventLogin:
			c.loginProgress(*f.Event)
			return nil
		case proto.EventAccounts:
			var accs []proto.Account
			if json.Unmarshal(f.Event.Data, &accs) == nil {
				c.Accounts = accs
			}
			return nil
		case proto.EventAutomation:
			var it proto.Automation
			if json.Unmarshal(f.Event.Data, &it) == nil && it.ID != "" {
				c.AutomationAlerts = append(c.AutomationAlerts, it)
				if c.Automations != nil {
					for i := range c.Automations.Items {
						if c.Automations.Items[i].ID == it.ID {
							c.Automations.Items[i] = it
							break
						}
					}
				}
			}
			return nil
		case proto.EventMCP:
			if c.MCP != nil && c.Phase == Ready {
				return []proto.Command{c.readMCP(c.MCPFor)}
			}
			return nil
		}
		if f.Event.Type == proto.EventSession {
			c.sessionUpdated(*f.Event) // any session's: the list shows them all
		}
		if f.Event.Type == proto.EventQueue {
			c.returned(*f.Event) // any session's, kept until it is opened
		}
		if c.Current == nil || f.Event.SessionID != c.Current.ID {
			c.mark(*f.Event)
			return nil
		}
		switch c.Phase {
		case Syncing:
			if len(c.buffer) >= maxBuffered {
				c.buffer, c.overflow = nil, true
			} else if !c.overflow {
				c.buffer = append(c.buffer, *f.Event)
			}
		case Ready:
			return c.apply(*f.Event)
		}
	}
	return nil
}

func (c *Client) ready(f proto.Frame) []proto.Command {
	if f.Version != proto.Version {
		c.fail(locales.ProtocolMismatch(f.Version, proto.Version))
		return nil
	}
	c.Phase, c.Err, c.Instance = Handshaking, "", f.Instance
	return []proto.Command{c.read(proto.MethodInitialize, nil, func(json.RawMessage) []proto.Command {
		cmds := []proto.Command{c.listAccounts(), c.read(proto.MethodSettings, nil, c.keepSettings), c.listSessions(true, true)}
		return append(cmds, c.checkUnknown()...)
	})}
}

// checkUnknown asks for the receipt of each command a disconnect left unknown.
func (c *Client) checkUnknown() []proto.Command {
	var cmds []proto.Command
	for i := range c.Unknown {
		if c.Unknown[i].Outcome != "" {
			continue
		}
		id := c.Unknown[i].ID
		cmds = append(cmds, c.read(proto.MethodReceipt, proto.ReceiptRef{ID: id}, func(r json.RawMessage) []proto.Command {
			var rc proto.Receipt
			_ = json.Unmarshal(r, &rc)
			for j := range c.Unknown {
				if c.Unknown[j].ID != id {
					continue
				}
				switch {
				case rc.Status == "" || rc.Status == "unknown":
					c.Unknown[j].Outcome = "not received"
				case rc.Error != "":
					c.Unknown[j].Outcome = rc.Status + ": " + rc.Error
				default:
					c.Unknown[j].Outcome = rc.Status
				}
			}
			return nil
		}))
	}
	return cmds
}

// PreDecodeFrame decodes heavy response payloads (such as EventPage) on a
// background goroutine so Receive does not unmarshal them on the UI thread.
func PreDecodeFrame(f *proto.Frame) {
	if f == nil || f.Type != proto.FrameResponse || f.Error != nil || len(f.Result) == 0 {
		return
	}
	res := bytes.TrimSpace(f.Result)
	if !bytes.HasPrefix(res, []byte(`{"events":`)) {
		return
	}
	var page proto.EventPage
	if json.Unmarshal(res, &page) == nil {
		f.Decoded = &page
	}
}

func (c *Client) response(f proto.Frame) []proto.Command {
	p, ok := c.pending[f.ID]
	if !ok {
		return nil
	}
	delete(c.pending, f.ID)
	if f.Error != nil {
		if p.failed != nil {
			return p.failed(f.Error)
		}
		if c.Phase == Handshaking || c.Phase == Syncing {
			c.fail(f.Error.Message)
			return nil
		}
		c.notice("fail", "%s", locales.RequestFailed(p.method, f.Error.Message))
		return nil
	}
	if p.done != nil {
		c.decoded = f.Decoded
		defer func() { c.decoded = nil }()
		return p.done(f.Result)
	}
	return nil
}

func (c *Client) listAccounts() proto.Command {
	return c.read(proto.MethodAccounts, nil, func(r json.RawMessage) []proto.Command {
		_ = json.Unmarshal(r, &c.Accounts)
		return nil
	})
}

// listSessions reads the session list. With bind it then shows a session:
// the one shown before a reconnect, else the workspace's newest, else a new
// one.
func (c *Client) listSessions(bind, autoCreate bool) proto.Command {
	return c.read(proto.MethodSessionList, nil, func(r json.RawMessage) []proto.Command {
		var list []proto.Session
		if err := json.Unmarshal(r, &list); err == nil {
			c.Sessions = list
			clear(c.Marks)
			for _, s := range list {
				switch {
				case s.State != "":
					c.Marks[s.ID] = s.State
				case s.Mark != "":
					c.Marks[s.ID] = s.Mark
				}
			}
		}
		if !bind {
			return nil
		}
		if c.want != "" {
			return []proto.Command{c.bind(c.want)}
		}
		for _, s := range c.Sessions {
			if s.Cwd == c.Cwd {
				return []proto.Command{c.bind(s.ID)}
			}
		}
		if autoCreate {
			return []proto.Command{c.create()}
		}
		if len(c.Sessions) > 0 {
			return []proto.Command{c.bind(c.Sessions[0].ID)}
		}
		c.Phase = Ready
		return nil
	})
}

func (c *Client) create() proto.Command {
	c.Phase = Syncing
	c.resetTranscript()
	p := &pending{mutation: true}
	p.done = func(r json.RawMessage) []proto.Command {
		var s proto.Session
		if err := json.Unmarshal(r, &s); err != nil || s.ID == "" {
			c.fail(locales.CreateSessionEmpty())
			return nil
		}
		c.upsertSession(s)
		return []proto.Command{c.bind(s.ID)}
	}
	return c.send(proto.MethodSessionCreate, proto.SessionCreate{Cwd: c.Cwd}, p)
}

// bind shows a session: it reads the session, then its whole log.
func (c *Client) bind(id string) proto.Command {
	if c.Current != nil && c.Current.ID != id {
		switch {
		case len(c.Approvals) > 0:
			c.Marks[c.Current.ID] = "wait"
		case c.Busy:
			c.Marks[c.Current.ID] = "run"
		}
	}
	delete(c.Marks, id)
	c.Phase = Syncing
	c.resetTranscript()
	c.want = id
	p := &pending{}
	p.done = func(r json.RawMessage) []proto.Command {
		if c.want != id {
			return nil // another session was asked for meanwhile
		}
		var s proto.Snapshot
		if err := json.Unmarshal(r, &s); err != nil || s.Session.ID == "" {
			c.fail(locales.ReadSessionEmpty())
			return nil
		}
		c.Current = &s.Session
		c.upsertSession(s.Session)
		c.Queued = s.Queued
		c.Draft = s.Draft
		if s.Revision > tailSize {
			c.cursor, c.trim = s.Revision-tailSize, true
		}
		return []proto.Command{c.catchUp()}
	}
	p.failed = func(e *proto.Error) []proto.Command {
		if e.Code != "not_found" {
			c.fail(e.Message)
			return nil
		}
		// Deleted meanwhile: show another rather than nothing.
		c.want = ""
		c.Sessions = slices.DeleteFunc(c.Sessions, func(s proto.Session) bool { return s.ID == id })
		return []proto.Command{c.listSessions(true, false)}
	}
	return c.send(proto.MethodSessionGet, proto.SessionRef{SessionID: id}, p)
}

func (c *Client) resetTranscript() {
	c.Current, c.Entries, c.streaming, c.Turns = nil, nil, nil, nil
	c.thinking, c.thought, c.LastInput = nil, nil, 0
	c.Busy, c.Queued, c.Approvals, c.Usage = false, nil, nil, llm.Usage{}
	clear(c.Answering)
	c.buffer, c.overflow = nil, false
	c.cursor = 0
	c.log, c.first, c.Older, c.LoadingOlder, c.trim = nil, 0, false, false, false
	c.resetChanges()
	c.Workspace = Workspace{BranchStale: true}
	c.afterSync = nil
}

// catchUp reads the current session's log after the cursor, page by page,
// then applies what arrived meanwhile.
func (c *Client) catchUp() proto.Command {
	c.Phase = Syncing
	session := c.Current.ID
	q := proto.EventsQuery{SessionID: session, After: c.cursor, Limit: pageSize}
	return c.read(proto.MethodEvents, q, func(r json.RawMessage) []proto.Command {
		if c.Current == nil || c.Current.ID != session {
			return nil
		}
		var page proto.EventPage
		if dp, ok := c.decoded.(*proto.EventPage); ok && dp != nil {
			page = *dp
		} else if err := json.Unmarshal(r, &page); err != nil {
			c.fail(locales.ReadEventsFailed(err.Error()))
			return nil
		}
		events := page.Events
		if c.trim && len(events) > 0 {
			c.trim, c.Older = false, true
			c.cursor = events[0].Seq - 1
			events = events[firstTurn(events):]
			c.first = events[0].Seq
		}
		for _, e := range events {
			c.event(e)
		}
		if page.More {
			return []proto.Command{c.catchUp()}
		}
		buffered, overflow := c.buffer, c.overflow
		c.buffer, c.overflow = nil, false
		c.Phase = Ready
		if overflow {
			return []proto.Command{c.catchUp()}
		}
		// Streaming events before a durable one the log already gave
		// belong to it: skip them.
		start := 0
		for i, e := range buffered {
			if e.Seq != 0 && e.Seq <= c.cursor {
				start = i + 1
			}
		}
		var cmds []proto.Command
		for _, e := range buffered[start:] {
			cmds = append(cmds, c.apply(e)...)
			if c.Phase != Ready {
				break // a gap asked for the log again
			}
		}
		if c.Phase == Ready && c.afterSync != nil {
			fn := c.afterSync
			c.afterSync = nil
			cmds = append(cmds, fn()...)
		}
		return cmds
	})
}

// mark follows a session that is not shown.
func (c *Client) mark(e proto.Event) {
	if e.SessionID == c.want {
		return // being opened
	}
	switch e.Type {
	case proto.EventTurnStart, proto.EventApprovalResolved:
		c.Marks[e.SessionID] = "run"
	case proto.EventApprovalRequest:
		c.Marks[e.SessionID] = "wait"
	case proto.EventTurnEnd:
		var end proto.TurnEnd
		json.Unmarshal(e.Data, &end)
		c.Marks[e.SessionID] = "unread"
		if end.Error != "" {
			c.Marks[e.SessionID] = "fail"
		}
	}
}

func (c *Client) upsertSession(s proto.Session) {
	for i := range c.Sessions {
		if c.Sessions[i].ID == s.ID {
			c.Sessions[i] = s
			return
		}
	}
	c.Sessions = append([]proto.Session{s}, c.Sessions...)
}

func (c *Client) sessionUpdated(e proto.Event) {
	var s proto.Session
	if json.Unmarshal(e.Data, &s) != nil || s.ID == "" {
		return
	}
	c.upsertSession(s)
	if c.Current != nil && c.Current.ID == s.ID {
		if c.Current.Cwd != s.Cwd {
			c.Workspace.BranchStale = true
		}
		*c.Current = s
	}
	if m := c.Marks[s.ID]; m != "run" && m != "wait" {
		if s.Mark != "" {
			c.Marks[s.ID] = s.Mark
		} else {
			delete(c.Marks, s.ID)
		}
	}
}

// Flag pins, archives or marks a session.
func (c *Client) Flag(f proto.SessionFlag) ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	return []proto.Command{c.mutate(proto.MethodSessionFlag, f, func(r json.RawMessage) []proto.Command {
		c.sessionUpdated(proto.Event{Data: r})
		return nil
	})}, nil
}

// Rename gives a session a title.
func (c *Client) Rename(id, title string) ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	return []proto.Command{c.mutate(proto.MethodSessionUpdate, proto.SessionUpdate{SessionID: id, Title: &title}, func(r json.RawMessage) []proto.Command {
		c.sessionUpdated(proto.Event{Data: r})
		return nil
	})}, nil
}

// Seen clears the mark of a session whose ended turn the user has now
// seen.
func (c *Client) Seen(id string) []proto.Command {
	marked := c.Marks[id] == "unread" || c.Marks[id] == "fail"
	for i := range c.Sessions {
		if c.Sessions[i].ID == id && c.Sessions[i].Mark != "" {
			c.Sessions[i].Mark, marked = "", true
		}
	}
	if !marked || c.Phase != Ready {
		return nil
	}
	if c.Current != nil && c.Current.ID == id {
		c.Current.Mark = ""
	}
	if m := c.Marks[id]; m == "unread" || m == "fail" {
		delete(c.Marks, id)
	}
	none := ""
	return []proto.Command{c.mutate(proto.MethodSessionFlag, proto.SessionFlag{SessionID: id, Mark: &none}, nil)}
}

// apply applies one live event of the current session while in step.
func (c *Client) apply(e proto.Event) []proto.Command {
	if e.Seq != 0 {
		if e.Seq <= c.cursor {
			return nil // already applied
		}
		if e.Seq != c.cursor+1 {
			return []proto.Command{c.catchUp()}
		}
	}
	return c.event(e)
}

// event applies one event of the current session, from the log or live.
func (c *Client) event(e proto.Event) []proto.Command {
	if e.Seq != 0 {
		c.cursor = e.Seq
		c.log = append(c.log, e)
	}
	c.changesEvent(e)
	switch e.Type {
	case proto.EventTurnStart:
		c.Busy = true
		c.Turns = append(c.Turns, Turn{Seq: e.Seq, Start: e.At})
	case proto.EventThinkingDelta:
		var d proto.Delta
		_ = json.Unmarshal(e.Data, &d)
		if c.thinking == nil {
			c.endStream()
			c.thinking = c.add(&Entry{Role: Thinking, at: e.At})
			c.thought = c.thinking
			c.thinkBuf.Reset()
		}
		c.thinkBuf.WriteString(d.Text)
		c.thinking.Text = c.thinkBuf.String()
		c.thinking.Took = e.At.Sub(c.thinking.at)
	case proto.EventUsage:
		var u llm.Usage
		if json.Unmarshal(e.Data, &u) == nil {
			c.LastInput = u.Input
		}
	case proto.EventTextDelta:
		var d proto.Delta
		_ = json.Unmarshal(e.Data, &d)
		c.endThinking()
		if c.streaming == nil {
			c.streaming = c.add(&Entry{Role: Assistant})
			c.streamBuf.Reset()
		}
		c.streamBuf.WriteString(d.Text)
		c.streaming.Text = c.streamBuf.String()
	case proto.EventMessage:
		var m llm.Message
		if json.Unmarshal(e.Data, &m) == nil {
			c.message(e.Seq, m)
		}
	case proto.EventToolStart, proto.EventToolEnd:
		var t proto.ToolEvent
		if json.Unmarshal(e.Data, &t) == nil {
			c.endStream()
			en := c.tool(t.Call)
			if t.Result != nil {
				c.toolResult(*t.Result)
			} else {
				en.Done = false
			}
		}
	case proto.EventTaskStep:
		var st proto.TaskStep
		if json.Unmarshal(e.Data, &st) == nil {
			if i := slices.IndexFunc(c.Entries, func(x *Entry) bool { return x.CallID == st.CallID }); i >= 0 {
				var args map[string]any
				json.Unmarshal(st.Call.Args, &args)
				step := st.Call.Name + "\t" + summarize(args)
				if t := c.Entries[i].task(st.Task); t != nil {
					t.Steps = append(t.Steps, step)
				} else {
					c.Entries[i].Steps = append(c.Entries[i].Steps, step)
				}
			}
		}
	case proto.EventTaskState:
		var st proto.TaskState
		if json.Unmarshal(e.Data, &st) == nil {
			if i := slices.IndexFunc(c.Entries, func(x *Entry) bool { return x.CallID == st.CallID }); i >= 0 {
				if t := c.Entries[i].task(st.Task); t != nil {
					t.State = st.State
				}
			}
		}
	case proto.EventApprovalRequest:
		var a proto.Approval
		if json.Unmarshal(e.Data, &a) == nil && !slices.ContainsFunc(c.Approvals, func(x proto.Approval) bool { return x.ID == a.ID }) {
			c.Approvals = append(c.Approvals, a)
		}
	case proto.EventApprovalResolved:
		var a proto.ApprovalAnswer
		if json.Unmarshal(e.Data, &a) == nil {
			if a.Allow && a.Command != "" {
				c.changed(a)
			}
			c.Approvals = slices.DeleteFunc(c.Approvals, func(x proto.Approval) bool { return x.ID == a.ApprovalID })
			delete(c.Answering, a.ApprovalID)
		}
	case proto.EventCompaction:
		var cp proto.Compaction
		if json.Unmarshal(e.Data, &cp) == nil {
			c.endStream()
			text := locales.Compacted()
			if cp.UpToSeq > 0 {
				text = locales.CompactedUpTo()
			} else if cp.Partial {
				text = locales.CompactedFromHere()
			} else if cp.Asked {
				text = locales.CompactedAsked()
			}
			c.add(&Entry{Role: Notice, Note: "compact", Text: text, Output: cp.Summary, CallID: "compaction:" + strconv.FormatInt(e.Seq, 10), Done: true})
		}
	case proto.EventFallback:
		var f proto.Fallback
		if json.Unmarshal(e.Data, &f) == nil {
			c.endStream()
			c.notice("fallback", "%s", locales.FellBack(c.modelName(f.From), c.modelName(f.To)))
		}
	case proto.EventShellStart:
		var sh proto.ShellRun
		if json.Unmarshal(e.Data, &sh) == nil {
			c.endStream()
			c.Busy = true
			c.typed(sh.Command)
		}
	case proto.EventShell:
		var sh proto.Shell
		if json.Unmarshal(e.Data, &sh) == nil {
			c.Busy = false
			en := c.typedRunning(sh.Command)
			if en == nil {
				en = c.typed(sh.Command)
			}
			en.Seq, en.Output, en.Done, en.Failed = e.Seq, sh.Output, true, sh.Stopped
		}
	case proto.EventQueue:
		var q proto.Queue
		if json.Unmarshal(e.Data, &q) == nil {
			c.Queued = q.Items
		}
	case proto.EventRetry:
		var r proto.Retry
		_ = json.Unmarshal(e.Data, &r)
		c.notice("retry", "%s", locales.Retrying(r.Attempt, r.Error))
	case proto.EventTurnEnd:
		var t proto.TurnEnd
		_ = json.Unmarshal(e.Data, &t)
		c.endStream()
		c.Busy, c.Approvals = false, nil // the queue says itself what became of it
		c.thought = nil
		c.Workspace.BranchStale = true // the turn may have switched it
		clear(c.Answering)
		c.Usage = c.Usage.Add(t.Usage)
		if n := len(c.Turns); n > 0 && !c.Turns[n-1].Done {
			c.Turns[n-1].End, c.Turns[n-1].Done, c.Turns[n-1].Usage = e.At, true, t.Usage
		}
		switch {
		case t.Stopped:
			c.notice("stop", "%s", locales.Stopped())
		case t.Limit:
			c.notice("limit", "%s", locales.StepLimit())
		case t.Error != "":
			c.notice("fail", "%s", locales.TurnFailed(t.Error))
		}
	}
	return nil
}

// mentionAttach is the block expandMentions appends for the model.
// The bubble keeps the user's words; the block stays in the log.
var mentionAttach = regexp.MustCompile(`(?s)\n\n<(?:session|mcp-resource|mcp-prompt)\b[^>]*>.*?</(?:session|mcp-resource|mcp-prompt)>`)

func visiblePrompt(text string) string {
	return strings.TrimRight(mentionAttach.ReplaceAllString(text, ""), "\n")
}

// message adds a stored message: the user's prompt, the assistant's text
// and tool calls, or tool results (which travel as a user message).
func (c *Client) message(seq int64, m llm.Message) {
	var text strings.Builder
	var images []llm.Image
	for _, p := range m.Parts {
		text.WriteString(p.Text)
		if p.Image != nil {
			images = append(images, *p.Image)
		}
	}
	switch m.Role {
	case llm.User:
		shown := visiblePrompt(text.String())
		if shown != "" || len(images) > 0 {
			c.endStream()
			c.add(&Entry{Seq: seq, Role: User, Text: shown, Images: images, Done: true})
		}
	case llm.Assistant:
		var thought strings.Builder
		for _, p := range m.Parts {
			if p.Thinking != nil {
				thought.WriteString(p.Thinking.Text)
			}
		}
		c.endThinking()
		switch {
		case c.thought != nil:
			if thought.Len() > 0 {
				c.thought.Text = thought.String()
			}
			c.thought = nil
		case thought.Len() > 0:
			c.add(&Entry{Seq: seq, Role: Thinking, Text: thought.String(), Done: true})
		}
		switch {
		case c.streaming != nil:
			c.streaming.Text, c.streaming.Done, c.streaming.Seq = text.String(), true, seq
			c.streaming = nil
		case text.Len() > 0:
			c.add(&Entry{Seq: seq, Role: Assistant, Text: text.String(), Done: true})
		}
	}
	for _, p := range m.Parts {
		switch {
		case p.ToolCall != nil:
			c.tool(*p.ToolCall)
		case p.ToolResult != nil:
			c.toolResult(*p.ToolResult)
		}
	}
}

// tool returns the row of a call, adding it the first time.
func (c *Client) tool(call llm.ToolCall) *Entry {
	for i := len(c.Entries) - 1; i >= 0; i-- {
		if en := c.Entries[i]; en.Role == Tool && en.CallID == call.ID {
			return en
		}
	}
	var args map[string]any
	_ = json.Unmarshal(call.Args, &args)
	en := &Entry{Role: Tool, Text: call.Name, Detail: summarize(args), CallID: call.ID, Args: call.Args, Tasks: graphTasks(call)}
	if len(en.Tasks) > 0 {
		en.Detail = locales.TaskCount(len(en.Tasks))
	}
	return c.add(en)
}

func (c *Client) toolResult(r llm.ToolResult) {
	for i := len(c.Entries) - 1; i >= 0; i-- {
		if en := c.Entries[i]; en.Role == Tool && en.CallID == r.CallID {
			en.Done, en.Failed, en.Skipped, en.Output = true, r.IsError && !r.Skipped, r.Skipped, r.Content
			return
		}
	}
}

// summarize shows a tool call's most telling argument.
func summarize(args map[string]any) string {
	for _, k := range []string{"path", "command", "pattern", "query", "url", "title", "question", "description"} {
		if s, ok := args[k].(string); ok && s != "" {
			if k == "command" {
				s = unwrap(s)
			}
			if len(s) > 120 {
				s = s[:117] + "…"
			}
			return s
		}
	}
	return ""
}

// wrapped is a command handed to a shell of its own, as models write
// them: bash -lc '…', and a cd into the workspace before it.
var wrapped = regexp.MustCompile(`^(?:/bin/)?(?:ba|z)?sh\s+-l?c\s+(?:'([^']*)'|"((?:[^"\\]|\\.)*)")$`)
var cdFirst = regexp.MustCompile(`^cd\s+(?:'[^']*'|"[^"]*"|\S+)\s*&&\s*`)

// unwrap is the command a line runs, without the shell around it.
func unwrap(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if m := wrapped.FindStringSubmatch(cmd); m != nil {
		cmd = m[1] + m[2]
	}
	return cdFirst.ReplaceAllString(strings.TrimSpace(cmd), "")
}

// endThinking ends the streaming thinking entry: it is done when text or
// a tool call comes.
func (c *Client) endThinking() {
	if c.thinking != nil {
		c.thinking.Done = true
		c.thinking = nil
	}
}

func (c *Client) endStream() {
	c.endThinking()
	if c.streaming != nil {
		c.streaming.Done = true
		c.streaming = nil
	}
}

func (c *Client) fail(msg string) {
	c.Phase, c.Err = Disconnected, msg
}

// Disconnected records that the kernel went away. Unanswered mutations
// become unknown; nothing is resent.
func (c *Client) Disconnected(err error) {
	c.endStream()
	c.searching, c.LoadingOlder = "", false // their answers are lost
	for id, p := range c.pending {
		if p.mutation {
			c.Unknown = append(c.Unknown, Unknown{ID: id, Method: p.method})
		}
	}
	clear(c.pending)
	clear(c.Answering)
	c.Busy, c.SignIn = false, nil
	c.buffer, c.overflow = nil, false
	c.Phase = Disconnected
	if err != nil {
		c.Err = err.Error()
	} else {
		c.Err = locales.KernelExited()
	}
}

// --- commands the user starts ---

// ErrNotReady is returned when the connection cannot take commands now.
var ErrNotReady error = notReady{}

type notReady struct{}

func (notReady) Error() string { return locales.NotConnected() }

func (c *Client) mutation(method string, params any, done func(json.RawMessage) []proto.Command) ([]proto.Command, error) {
	if !c.CanAct() {
		return nil, ErrNotReady
	}
	return []proto.Command{c.mutate(method, params, done)}, nil
}

// Prompt sends text and images to the current session. While a turn
// runs, the prompt waits in Queued and starts the next turn.
func (c *Client) Prompt(text string, images ...llm.Image) ([]proto.Command, error) {
	return c.prompt(false, text, images)
}

// Steer sends text and images into the running turn, after its current
// tool calls; with none running it is a prompt.
func (c *Client) Steer(text string, images ...llm.Image) ([]proto.Command, error) {
	return c.prompt(true, text, images)
}

func (c *Client) prompt(steer bool, text string, images []llm.Image) ([]proto.Command, error) {
	if strings.TrimSpace(text) == "" && len(images) == 0 {
		return nil, errors.New(locales.NothingToSend())
	}
	p := proto.Prompt{SessionID: sessionID(c.Current), Text: text, Images: images, Steer: steer}
	return c.mutation(proto.MethodPrompt, p, nil) // the queue streams what became of it
}

// Rewind returns the current session to a prompt. Dropping the
// conversation reloads the log and keeps the prompt as the draft.
func (c *Client) Rewind(turn int64, action string) ([]proto.Command, error) {
	id := sessionID(c.Current)
	return c.mutation(proto.MethodRewind, proto.Rewind{SessionID: id, TurnSeq: turn, Action: action}, func(r json.RawMessage) []proto.Command {
		var out proto.RewindResult
		if json.Unmarshal(r, &out) != nil {
			return nil
		}
		if len(out.Conflicts) > 0 {
			c.notice("rewind", "%s", locales.RewindConflict(strings.Join(out.Conflicts, locales.ListSeparator())))
		}
		if out.Action == "code" || len(out.Conflicts) > 0 {
			return nil
		}
		if out.Action != "summarize" && out.Action != "summarize_before" {
			c.Draft = out.Prompt
		}
		return []proto.Command{c.bind(id)}
	})
}

// Retry rewinds the current session to turn (restoring code too when action
// is "both") and immediately re-sends that turn's prompt.
func (c *Client) Retry(turn int64, action string) ([]proto.Command, error) {
	id := sessionID(c.Current)
	return c.mutation(proto.MethodRewind, proto.Rewind{SessionID: id, TurnSeq: turn, Action: action}, func(r json.RawMessage) []proto.Command {
		var out proto.RewindResult
		if json.Unmarshal(r, &out) != nil {
			return nil
		}
		if len(out.Conflicts) > 0 {
			c.notice("rewind", "%s", locales.RewindConflict(strings.Join(out.Conflicts, locales.ListSeparator())))
			return nil
		}
		cmds := []proto.Command{c.bind(id)}
		if prompt := out.Prompt; strings.TrimSpace(prompt) != "" {
			c.afterSync = func() []proto.Command {
				next, _ := c.Prompt(prompt)
				return next
			}
		}
		return cmds
	})
}

// Compact has the kernel summarize the session's history now.
func (c *Client) Compact() ([]proto.Command, error) {
	p := proto.Prompt{SessionID: sessionID(c.Current), Text: "/compact", Compact: true}
	return c.mutation(proto.MethodPrompt, p, nil)
}

// Shell runs a command line of the user's in the current session.
func (c *Client) Shell(command string) ([]proto.Command, error) {
	if strings.TrimSpace(command) == "" {
		return nil, errors.New(locales.NothingToSend())
	}
	return c.mutation(proto.MethodShell, proto.ShellRun{SessionID: sessionID(c.Current), Command: command}, nil)
}

// typed adds a command the user runs as a shell step of no turn.
func (c *Client) typed(command string) *Entry {
	args, _ := json.Marshal(map[string]string{"command": command})
	e := c.add(&Entry{Role: Tool, Text: "shell", Detail: command, Args: args, Typed: true})
	e.Turn = -1
	return e
}

// typedRunning is the command of the user's that runs, if it is command.
func (c *Client) typedRunning(command string) *Entry {
	if n := len(c.Entries); n > 0 {
		if e := c.Entries[n-1]; e.Typed && !e.Done && e.Detail == command {
			return e
		}
	}
	return nil
}

// FindFiles asks for the current session's files that query names; the
// answer lands in Files, unless another query was asked for meanwhile.
func (c *Client) FindFiles(query string) []proto.Command {
	if c.Phase != Ready || c.Current == nil {
		return nil
	}
	c.FilesFor = query
	return []proto.Command{c.read(proto.MethodFilesFind, proto.FilesQuery{Cwd: c.Current.Cwd, Query: query, Limit: 8}, func(r json.RawMessage) []proto.Command {
		if c.FilesFor == query {
			c.Files = nil
			json.Unmarshal(r, &c.Files)
		}
		return nil
	})}
}

// SetDraft keeps what the user is writing to a session.
// It needs no session open: it is sent as the user leaves one.
func (c *Client) SetDraft(sessionID, text string) ([]proto.Command, error) {
	if c.Phase != Syncing && c.Phase != Ready {
		return nil, ErrNotReady
	}
	return []proto.Command{c.mutate(proto.MethodDraftSet, proto.DraftSet{SessionID: sessionID, Text: text}, nil)}, nil
}

// Unqueue takes back a waiting prompt; SendNow makes it join the running
// turn.
func (c *Client) Unqueue(id string) ([]proto.Command, error) { return c.editQueue(id, "remove") }
func (c *Client) SendNow(id string) ([]proto.Command, error) { return c.editQueue(id, "steer") }

func (c *Client) editQueue(id, action string) ([]proto.Command, error) {
	return c.mutation(proto.MethodQueueEdit, proto.QueueEdit{SessionID: sessionID(c.Current), ID: id, Action: action}, nil)
}

// returned keeps the prompts a session's stopped turns gave back.
func (c *Client) returned(e proto.Event) {
	var q proto.Queue
	if json.Unmarshal(e.Data, &q) != nil || len(q.Returned) == 0 {
		return
	}
	if c.Returned == nil {
		c.Returned = map[string][]string{}
	}
	for _, it := range q.Returned {
		if it.Text != "" {
			c.Returned[e.SessionID] = append(c.Returned[e.SessionID], it.Text)
		}
	}
}

func sessionID(s *proto.Session) string {
	if s == nil {
		return ""
	}
	return s.ID
}

// Cancel stops the current turn.
func (c *Client) Cancel() ([]proto.Command, error) {
	if !c.Busy {
		return nil, errors.New(locales.NothingRunning())
	}
	return c.mutation(proto.MethodCancel, proto.SessionRef{SessionID: sessionID(c.Current)}, nil)
}

// Answer allows or denies an approval request; always allows the tool for
// the rest of the session.
func (c *Client) Answer(id string, allow, always bool) ([]proto.Command, error) {
	return c.answer(proto.ApprovalAnswer{ApprovalID: id, Allow: allow, Always: always})
}

// changed shows the command the user changed a call to, on its step.
func (c *Client) changed(a proto.ApprovalAnswer) {
	i := slices.IndexFunc(c.Approvals, func(x proto.Approval) bool { return x.ID == a.ApprovalID })
	if i < 0 {
		return
	}
	ap := c.Approvals[i]
	for j := len(c.Entries) - 1; j >= 0; j-- {
		if e := c.Entries[j]; e.Role == Tool && !e.Done && e.Text == ap.Tool && string(e.Args) == string(ap.Args) {
			var args map[string]any
			if json.Unmarshal(e.Args, &args) == nil {
				args["command"] = a.Command
				e.Args, _ = json.Marshal(args)
				e.Detail = summarize(args)
			}
			return
		}
	}
}

// Respond answers an approval in full: declined with words, kept as a
// rule, or with the command changed.
func (c *Client) Respond(ans proto.ApprovalAnswer) ([]proto.Command, error) { return c.answer(ans) }

// Reply answers the model's question; an empty text skips it.
func (c *Client) Reply(id, text string) ([]proto.Command, error) {
	text = strings.TrimSpace(text)
	return c.answer(proto.ApprovalAnswer{ApprovalID: id, Allow: text != "", Text: text})
}

func (c *Client) answer(ans proto.ApprovalAnswer) ([]proto.Command, error) {
	id := ans.ApprovalID
	if c.Answering[id] {
		return nil, errors.New(locales.AlreadyAnswered())
	}
	ans.SessionID = sessionID(c.Current)
	cmds, err := c.mutation(proto.MethodApprovalRespond, ans, nil)
	if err == nil {
		c.Answering[id] = true
		c.pending[cmds[0].ID].failed = func(e *proto.Error) []proto.Command {
			delete(c.Answering, id)
			c.notice("fail", "%s", locales.ApprovalFailed(e.Message))
			return nil
		}
	}
	return cmds, err
}

// NewSession creates a session in the workspace and shows it.
func (c *Client) NewSession() ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	return []proto.Command{c.create()}, nil
}

// NewSessionIn makes dir the workspace, creates a session there and
// shows it.
func (c *Client) NewSessionIn(dir string) ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	c.Cwd = dir
	return []proto.Command{c.create()}, nil
}

// Open shows another session.
func (c *Client) Open(id string) ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	if c.Current != nil && c.Current.ID == id {
		return nil, nil
	}
	return []proto.Command{c.bind(id)}, nil
}

// Fork branches off a copy of a session and opens it.
func (c *Client) Fork(id string) ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	if id == "" && c.Current != nil {
		id = c.Current.ID
	}
	if id == "" {
		return nil, ErrNotReady
	}
	return []proto.Command{c.mutate(proto.MethodSessionFork, proto.SessionRef{SessionID: id}, func(r json.RawMessage) []proto.Command {
		var s proto.Session
		if json.Unmarshal(r, &s) != nil || s.ID == "" {
			return nil
		}
		c.upsertSession(s)
		return []proto.Command{c.bind(s.ID)}
	})}, nil
}

// Delete deletes a session; if it is shown, another is.
func (c *Client) Delete(id string) ([]proto.Command, error) {
	return c.mutation(proto.MethodSessionDelete, proto.SessionRef{SessionID: id}, func(json.RawMessage) []proto.Command {
		c.Sessions = slices.DeleteFunc(c.Sessions, func(s proto.Session) bool { return s.ID == id })
		if c.want != "" && c.want != id {
			return nil // another session is on its way
		}
		if c.Current != nil && c.Current.ID == id {
			c.want = ""
			c.Phase = Syncing
			c.resetTranscript()
			return []proto.Command{c.listSessions(true, false)}
		}
		return nil
	})
}

// Update changes the current session: title, account, model, effort or
// approval mode. The kernel's session.updated event brings the result.
func (c *Client) Update(u proto.SessionUpdate) ([]proto.Command, error) {
	if u.Mode != nil && !slices.Contains([]string{"ask", "auto", "full"}, *u.Mode) {
		return nil, errors.New(locales.UnknownMode(strconv.Quote(*u.Mode)))
	}
	u.SessionID = sessionID(c.Current)
	return c.mutation(proto.MethodSessionUpdate, u, func(r json.RawMessage) []proto.Command {
		c.sessionUpdated(proto.Event{Data: r})
		return nil
	})
}

// ListModels reads an account's models; only the latest request's answer
// is kept.
func (c *Client) ListModels(account string) ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	var cmd proto.Command
	cmd = c.read(proto.MethodModels, proto.ModelsQuery{Account: account}, func(r json.RawMessage) []proto.Command {
		if c.modelsReq != cmd.ID {
			return nil
		}
		c.Models = nil
		_ = json.Unmarshal(r, &c.Models)
		return nil
	})
	// The model list is never part of connecting: its failure, stale or
	// not, must not end a handshake or sync that started after it.
	c.pending[cmd.ID].failed = func(e *proto.Error) []proto.Command {
		if c.modelsReq == cmd.ID {
			c.Models = nil // the app shows the error in its menu
		}
		return nil
	}
	c.modelsReq = cmd.ID
	return []proto.Command{cmd}, nil
}

// QueuedLabel is how a queued prompt shows: its text, or what it holds.
func QueuedLabel(q proto.Queued) string { return queuedLabel(q.Text, q.Images) }

// queuedLabel is how a queued prompt shows: its text, or what it holds
// when it has none.
func queuedLabel(text string, images int) string {
	if text == "" && images > 0 {
		return locales.ImagesPlaceholder(images)
	}
	return text
}

// modelName shows "account/model" as the account's name and the model.
func (c *Client) modelName(ref string) string {
	i := strings.LastIndex(ref, "/")
	if i < 0 {
		return ref
	}
	account, model := ref[:i], ref[i+1:]
	for _, a := range c.Accounts {
		if a.ID == account && a.Name != "" {
			account = a.Name
		}
	}
	return account + " · " + model
}
