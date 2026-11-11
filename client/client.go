// Package model is the desktop app's state for one kernel connection,
// built only from frames the kernel sent. It knows nothing of windows: the
// app calls it on the main thread and sends the commands it returns, once
// each, in order.
//
// It follows the client contract of packages/client-core/README.md:
//   - frames are applied in wire order; duplicate or old revisions are
//     ignored, and a revision gap triggers a fresh snapshot;
//   - scope frames that arrive while a snapshot is outstanding are buffered
//     (at most 128) and applied after it if newer;
//   - a command left unanswered by a disconnect is unknown, never resent;
//     after reconnecting, command.receipt says what became of it;
//   - the kernel handles commands concurrently and may answer out of order,
//     so a command that depends on another is sent only once that one has
//     been answered.
package client

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"

	"talos/proto"
)

// Phase is where the connection stands.
type Phase int

const (
	Starting    Phase = iota // kernel launched, no ready frame yet
	Handshaking              // hello, workspace and session list in flight
	Syncing                  // a session is being bound or re-read
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
)

// Entry is one row of the transcript.
type Entry struct {
	Key    int    // stable list key
	Seq    uint64 // event sequence of a stored message; 0 for live rows
	Role   Role
	Text   string
	Detail string // a tool call's arguments, summarized
	Done   bool
	CallID string
	Failed bool
}

// Unknown is a mutation whose outcome a disconnect left unknown.
type Unknown struct {
	ID, Method string
	// Outcome is empty until a receipt was read: then the receipt's
	// status, or "not received".
	Outcome string
}

const maxBuffered = 128

type pending struct {
	method   string
	mutation bool
	done     func(json.RawMessage) []proto.Command
	failed   func(string) []proto.Command
}

// Client is the state of one scope on one kernel.
type Client struct {
	Scope string
	Phase Phase
	Err   string

	Workspace proto.Workspace
	Sessions  []proto.Session
	Current   *proto.Session

	Entries   []*Entry
	HasOlder  bool
	Busy      bool
	RunID     string
	Approvals []proto.Permission
	// Answering holds approvals whose answer is in flight.
	Answering map[string]bool

	Models     []proto.Model
	ModelsMore bool
	Unknown    []Unknown

	launch    string
	nextCmd   int
	nextKey   int
	pending   map[string]*pending
	revision  uint64
	cursor    uint64
	buffer    []proto.Frame
	overflow  bool
	streaming *Entry
	want      string // session to resume on (re)connect
	modelsReq string
}

// New returns a client for scope that has not connected yet. Command ids
// start with a random prefix, so that they stay unique across launches, as
// durable receipts require.
func New(scope string) *Client {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return newClient(scope, hex.EncodeToString(b))
}

func newClient(scope, launch string) *Client {
	return &Client{Scope: scope, launch: launch, pending: map[string]*pending{}, Answering: map[string]bool{}}
}

// CanAct reports whether commands that change things may be sent now.
func (c *Client) CanAct() bool { return c.Phase == Ready && c.Current != nil }

// CanSend reports whether a prompt may be sent now.
func (c *Client) CanSend() bool { return c.CanAct() && !c.Busy }

func (c *Client) send(method string, params map[string]any, p *pending) proto.Command {
	c.nextCmd++
	id := c.launch + "-" + strconv.Itoa(c.nextCmd)
	p.method = method
	c.pending[id] = p
	return proto.Command{ID: id, Scope: c.Scope, Method: method, Params: params}
}

func (c *Client) read(method string, params map[string]any, done func(json.RawMessage) []proto.Command) proto.Command {
	return c.send(method, params, &pending{done: done})
}

func (c *Client) mutate(method string, params map[string]any, done func(json.RawMessage) []proto.Command) proto.Command {
	return c.send(method, params, &pending{mutation: true, done: done})
}

func (c *Client) add(e *Entry) *Entry {
	c.nextKey++
	e.Key = c.nextKey
	c.Entries = append(c.Entries, e)
	return e
}

func (c *Client) notice(format string, args ...any) {
	c.add(&Entry{Role: Notice, Text: fmt.Sprintf(format, args...), Done: true})
}

// Receive applies one frame in wire order and returns the commands to send next.
func (c *Client) Receive(f proto.Frame) []proto.Command {
	switch f.Type {
	case proto.TypeReady:
		return c.ready(f)
	case proto.TypeResponse:
		return c.response(f)
	}
	if f.Scope != c.Scope {
		return nil
	}
	if c.Phase == Syncing {
		if len(c.buffer) >= maxBuffered {
			c.buffer, c.overflow = nil, true
		} else if !c.overflow {
			c.buffer = append(c.buffer, f)
		}
		return nil
	}
	if c.Phase != Ready {
		return nil
	}
	return c.apply(f)
}

func (c *Client) ready(f proto.Frame) []proto.Command {
	if f.ProtocolVer != 0 && f.ProtocolVer != proto.Version {
		c.fail(fmt.Sprintf("The kernel speaks protocol %d; this app speaks %d.", f.ProtocolVer, proto.Version))
		return nil
	}
	c.Phase, c.Err = Handshaking, ""
	return []proto.Command{c.read("hello", map[string]any{"protocolVersions": []int{proto.Version}}, func(json.RawMessage) []proto.Command {
		cmds := []proto.Command{
			c.read("workspace.current", nil, func(r json.RawMessage) []proto.Command {
				_ = json.Unmarshal(r, &c.Workspace)
				return nil
			}),
			c.listSessions(true),
		}
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
		cmds = append(cmds, c.read("command.receipt", map[string]any{"commandId": id}, func(r json.RawMessage) []proto.Command {
			var res struct{ Receipt *proto.Receipt }
			_ = json.Unmarshal(r, &res)
			for j := range c.Unknown {
				if c.Unknown[j].ID != id {
					continue
				}
				switch {
				case res.Receipt == nil:
					c.Unknown[j].Outcome = "not received"
				case res.Receipt.Error != "":
					c.Unknown[j].Outcome = res.Receipt.Status + ": " + res.Receipt.Error
				default:
					c.Unknown[j].Outcome = res.Receipt.Status
				}
			}
			return nil
		}))
	}
	return cmds
}

func (c *Client) response(f proto.Frame) []proto.Command {
	p, ok := c.pending[f.ID]
	if !ok {
		return nil
	}
	delete(c.pending, f.ID)
	if f.Error != "" {
		if p.failed != nil {
			return p.failed(f.Error)
		}
		if c.Phase == Handshaking {
			c.fail(f.Error)
			return nil
		}
		c.notice("%s failed: %s", p.method, f.Error)
		return nil
	}
	if p.done != nil {
		return p.done(f.Result)
	}
	return nil
}

// listSessions reads the session list. With bind it then binds the session
// to show: the one shown before a reconnect, else the newest, else a new one.
func (c *Client) listSessions(bind bool) proto.Command {
	return c.read("session.list", map[string]any{"limit": 100}, func(r json.RawMessage) []proto.Command {
		var list []proto.Session
		if err := json.Unmarshal(r, &list); err == nil {
			c.Sessions = list
		}
		if !bind {
			return nil
		}
		if c.want != "" {
			return []proto.Command{c.bind("session.resume", c.want)}
		}
		if len(c.Sessions) > 0 {
			return []proto.Command{c.bind("session.resume", c.Sessions[0].ID)}
		}
		return []proto.Command{c.bind("session.create", "")}
	})
}

// bind selects a session for the scope, then reads its snapshot and history.
func (c *Client) bind(method, id string) proto.Command {
	c.Phase = Syncing
	c.resetTranscript()
	var params map[string]any
	if id != "" {
		params = map[string]any{"sessionId": id}
	}
	p := &pending{mutation: method == "session.create"}
	p.done = func(r json.RawMessage) []proto.Command {
		var s proto.Session
		if err := json.Unmarshal(r, &s); err != nil || s.ID == "" {
			c.fail(method + " returned no session")
			return nil
		}
		c.Current, c.want = &s, s.ID
		c.upsertSession(s)
		return []proto.Command{c.snapshot(true)}
	}
	p.failed = func(msg string) []proto.Command {
		if method == "session.resume" {
			// Deleted meanwhile: start a new one rather than show nothing.
			c.want = ""
			return []proto.Command{c.bind("session.create", "")}
		}
		c.fail(msg)
		return nil
	}
	return c.send(method, params, p)
}

func (c *Client) resetTranscript() {
	c.Entries, c.streaming, c.HasOlder = nil, nil, false
	c.Busy, c.RunID, c.Approvals = false, "", nil
	clear(c.Answering)
	c.buffer, c.overflow = nil, false
	c.cursor = 0
}

// snapshot reads session.current. With reload it rebuilds the transcript
// from the snapshot and stored history; without, it only refreshes the
// session and execution state (after session.invalidated).
func (c *Client) snapshot(reload bool) proto.Command {
	if reload {
		c.Phase = Syncing
		c.resetTranscript()
	}
	return c.read("session.current", nil, func(r json.RawMessage) []proto.Command {
		var s proto.Snapshot
		if err := json.Unmarshal(r, &s); err != nil {
			c.fail("session.current: " + err.Error())
			return nil
		}
		if s.Session == nil {
			// Unbound, as after its deletion: show another.
			c.Current, c.want = nil, ""
			c.Phase = Syncing
			c.resetTranscript()
			return []proto.Command{c.listSessions(true)}
		}
		c.Current = s.Session
		c.upsertSession(*s.Session)
		c.Busy = s.Busy
		c.RunID = ""
		if s.RunID != nil && s.RunStatus == "running" {
			c.RunID = *s.RunID
		}
		c.Approvals = s.Approvals
		var cmds []proto.Command
		if reload {
			c.revision, c.cursor = s.Revision, s.Cursor
			if s.Busy && s.Stream != "" {
				c.streaming = c.add(&Entry{Role: Assistant, Text: s.Stream})
			}
			cmds = append(cmds, c.history(s.Cursor+1))
		} else {
			c.revision = max(c.revision, s.Revision)
		}
		buffered, overflow := c.buffer, c.overflow
		c.buffer, c.overflow = nil, false
		c.Phase = Ready
		if overflow {
			return append(cmds, c.snapshot(true))
		}
		for _, f := range buffered {
			cmds = append(cmds, c.apply(f)...)
			if c.Phase != Ready {
				break // a gap in the buffer asked for another snapshot
			}
		}
		return cmds
	})
}

// history reads the stored messages before seq and puts them ahead of the
// transcript's live rows.
func (c *Client) history(before uint64) proto.Command {
	session := c.Current.ID
	return c.read("messages.read", map[string]any{"sessionId": session, "before": before, "limit": 50}, func(r json.RawMessage) []proto.Command {
		if c.Current == nil || c.Current.ID != session {
			return nil
		}
		var page proto.MessagePage
		if err := json.Unmarshal(r, &page); err != nil {
			return nil
		}
		c.HasOlder = page.HasOlder
		var rows []*Entry
		for _, m := range page.Items {
			role := Role(m.Message.Role)
			if (role != User && role != Assistant) || m.Message.Text == "" || c.hasSeq(m.Seq) {
				continue
			}
			c.nextKey++
			rows = append(rows, &Entry{Key: c.nextKey, Seq: m.Seq, Role: role, Text: m.Message.Text, Done: true})
		}
		c.Entries = append(rows, c.Entries...)
		return nil
	})
}

func (c *Client) hasSeq(seq uint64) bool {
	return slices.ContainsFunc(c.Entries, func(e *Entry) bool { return e.Seq == seq })
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

// apply applies one scope frame while in step.
func (c *Client) apply(f proto.Frame) []proto.Command {
	if f.Revision != 0 {
		if f.Revision <= c.revision {
			return nil
		}
		if f.Revision != c.revision+1 {
			return []proto.Command{c.snapshot(true)}
		}
		c.revision = f.Revision
	}
	switch f.Type {
	case proto.TypeEvent:
		return c.event(f.Event)
	case proto.TypeApproval:
		if !slices.ContainsFunc(c.Approvals, func(a proto.Permission) bool { return a.ID == f.Request.ID }) {
			c.Approvals = append(c.Approvals, *f.Request)
		}
	case proto.TypeResolved:
		c.Approvals = slices.DeleteFunc(c.Approvals, func(a proto.Permission) bool { return a.ID == f.ID })
		delete(c.Answering, f.ID)
	case proto.TypeInvalidated:
		return []proto.Command{c.snapshot(false)}
	}
	return nil
}

func (c *Client) event(e *proto.Event) []proto.Command {
	if c.Current == nil || e.SessionID != c.Current.ID {
		return nil
	}
	if e.Seq != 0 {
		if e.Seq <= c.cursor {
			return nil // already in the snapshot or history
		}
		c.cursor = e.Seq
	}
	switch e.Type {
	case "run.start":
		c.Busy = true
		c.RunID = e.String("runId")
	case "text.delta":
		if c.streaming == nil {
			c.streaming = c.add(&Entry{Role: Assistant})
		}
		c.streaming.Text += e.String("text")
	case "message":
		m, _ := e.Data["message"].(map[string]any)
		role, _ := m["role"].(string)
		text, _ := m["text"].(string)
		switch Role(role) {
		case User:
			c.add(&Entry{Seq: e.Seq, Role: User, Text: text, Done: true})
		case Assistant:
			if c.streaming != nil {
				c.streaming.Text, c.streaming.Done, c.streaming.Seq = text, true, e.Seq
				c.streaming = nil
			} else if text != "" {
				c.add(&Entry{Seq: e.Seq, Role: Assistant, Text: text, Done: true})
			}
		}
	case "tool.start":
		c.endStream()
		args, _ := e.Data["arguments"].(map[string]any)
		c.add(&Entry{Role: Tool, Text: e.String("name"), Detail: summarize(args), CallID: e.String("callId")})
	case "tool.end":
		id := e.String("callId")
		for i := len(c.Entries) - 1; i >= 0; i-- {
			if en := c.Entries[i]; en.Role == Tool && en.CallID == id {
				en.Done = true
				if r, ok := e.Data["result"].(map[string]any); ok {
					en.Failed, _ = r["isError"].(bool)
				}
				break
			}
		}
	case "run.end":
		c.endStream()
		c.Busy, c.RunID = false, ""
		if st := e.String("status"); st == "failed" || st == "cancelled" {
			msg := "Run " + st
			if reason := e.String("error"); reason != "" {
				msg += ": " + reason
			}
			c.notice("%s", msg)
		}
		// The first prompt names a session.
		return []proto.Command{c.listSessions(false)}
	}
	return nil
}

// summarize shows a tool call's most telling argument.
func summarize(args map[string]any) string {
	for _, k := range []string{"path", "command", "pattern", "query", "url"} {
		if s, ok := args[k].(string); ok && s != "" {
			if len(s) > 120 {
				s = s[:117] + "…"
			}
			return s
		}
	}
	return ""
}

func (c *Client) endStream() {
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
	for id, p := range c.pending {
		if p.mutation {
			c.Unknown = append(c.Unknown, Unknown{ID: id, Method: p.method})
		}
	}
	clear(c.pending)
	clear(c.Answering)
	c.Busy, c.RunID = false, ""
	c.buffer, c.overflow = nil, false
	c.Phase = Disconnected
	if err != nil {
		c.Err = err.Error()
	} else {
		c.Err = "The kernel exited."
	}
}

// --- commands the user starts ---

// ErrNotReady is returned when the connection cannot take commands now.
var ErrNotReady = fmt.Errorf("not connected")

func (c *Client) mutation(method string, params map[string]any, done func(json.RawMessage) []proto.Command) ([]proto.Command, error) {
	if !c.CanAct() {
		return nil, ErrNotReady
	}
	return []proto.Command{c.mutate(method, params, done)}, nil
}

// Prompt sends text to the current session.
func (c *Client) Prompt(text string) ([]proto.Command, error) {
	if c.Busy {
		return nil, fmt.Errorf("a run is in progress")
	}
	return c.mutation("prompt", map[string]any{"text": text}, nil)
}

// Cancel stops the current run.
func (c *Client) Cancel() ([]proto.Command, error) {
	if c.RunID == "" {
		return nil, fmt.Errorf("nothing is running")
	}
	return c.mutation("run.cancel", map[string]any{"sessionId": c.Current.ID, "runId": c.RunID}, nil)
}

// Answer allows or denies an approval request.
func (c *Client) Answer(id string, allow bool) ([]proto.Command, error) {
	if c.Answering[id] {
		return nil, fmt.Errorf("already answered")
	}
	cmds, err := c.mutation("approval.respond", map[string]any{"id": id, "allow": allow}, nil)
	if err == nil {
		c.Answering[id] = true
		c.pending[cmds[0].ID].failed = func(msg string) []proto.Command {
			delete(c.Answering, id)
			c.notice("Approval failed: %s", msg)
			return nil
		}
	}
	return cmds, err
}

// NewSession creates a session and shows it.
func (c *Client) NewSession() ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	return []proto.Command{c.bind("session.create", "")}, nil
}

// Open shows another session.
func (c *Client) Open(id string) ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	if c.Current != nil && c.Current.ID == id {
		return nil, nil
	}
	return []proto.Command{c.bind("session.resume", id)}, nil
}

// Delete deletes a session; the kernel then unbinds it if it is shown.
func (c *Client) Delete(id string) ([]proto.Command, error) {
	return c.mutation("session.delete", map[string]any{"sessionId": id}, func(json.RawMessage) []proto.Command {
		c.Sessions = slices.DeleteFunc(c.Sessions, func(s proto.Session) bool { return s.ID == id })
		return nil
	})
}

// Rename renames a session.
func (c *Client) Rename(id, title string) ([]proto.Command, error) {
	return c.mutation("session.rename", map[string]any{"sessionId": id, "title": title}, func(r json.RawMessage) []proto.Command {
		var s proto.Session
		if json.Unmarshal(r, &s) == nil && s.ID != "" {
			c.upsertSession(s)
		}
		return nil
	})
}

// SetPermissionMode sets the workspace's approval mode.
func (c *Client) SetPermissionMode(mode string) ([]proto.Command, error) {
	if !slices.Contains(proto.PermissionModes, mode) {
		return nil, fmt.Errorf("unknown permission mode %q", mode)
	}
	return c.mutation("permissions.set", map[string]any{"mode": mode}, func(r json.RawMessage) []proto.Command {
		var res struct{ Mode string }
		if json.Unmarshal(r, &res) == nil && res.Mode != "" {
			c.Workspace.PermissionMode = res.Mode
		}
		return nil
	})
}

// SetModel selects the current session's model.
func (c *Client) SetModel(provider, model string) ([]proto.Command, error) {
	return c.mutation("model.set", map[string]any{"sessionId": c.Current.ID, "provider": provider, "model": model}, func(r json.RawMessage) []proto.Command {
		var s proto.Session
		if json.Unmarshal(r, &s) == nil && s.ID != "" {
			if c.Current != nil && c.Current.ID == s.ID {
				c.Current = &s
			}
			c.upsertSession(s)
		}
		return nil
	})
}

// SearchModels lists models matching query; only the latest search's
// answer is kept.
func (c *Client) SearchModels(query string) ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	var cmd proto.Command
	cmd = c.read("models.list", map[string]any{"query": query, "limit": 50}, func(r json.RawMessage) []proto.Command {
		if c.modelsReq != cmd.ID {
			return nil
		}
		var page proto.ModelPage
		if json.Unmarshal(r, &page) == nil {
			c.Models, c.ModelsMore = page.Items, page.HasMore
		}
		return nil
	})
	c.modelsReq = cmd.ID
	return []proto.Command{cmd}, nil
}
