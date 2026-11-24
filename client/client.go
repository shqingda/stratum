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
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/locales"
	"github.com/shqingda/stratum/proto"
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
	MCP              *proto.MCPList
	MCPFor           string
	Preview          *proto.PreviewSnapshot
	Automations      *proto.AutomationList
	AutomationAlerts []proto.Automation
	Hooks            *proto.HooksList
	Plugins          *proto.PluginList
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

	// Ephemeral /btw side question state (read-only, never persisted).
	SideSession  string
	SideQuestion string
	SideAnswer   string
	SideErr      string
	SidePending  bool
	// Suggestion is the post-reply follow-up prompt offered in the composer.
	Suggestion string

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
	thought             *Entry // the streamed thinking entry its message has not yet sent whole
	want                string // session to show on (re)connect
	modelsReq           string
	afterSync           func() []proto.Command
	decoded             any
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

func (c *Client) mutation(method string, params any, done func(json.RawMessage) []proto.Command) ([]proto.Command, error) {
	if !c.CanAct() {
		return nil, ErrNotReady
	}
	return []proto.Command{c.mutate(method, params, done)}, nil
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

func (c *Client) create() proto.Command {
	c.Phase = Syncing
	c.ClearSide()
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
	if c.SideSession != id {
		c.ClearSide()
	}
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
	c.Suggestion = ""
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

// ErrNotReady is returned when the connection cannot take commands now.
var ErrNotReady error = notReady{}

type notReady struct{}

func (notReady) Error() string { return locales.NotConnected() }
