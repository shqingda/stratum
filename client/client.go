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
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/shqingda/talos/llm"
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
	Detail string // a tool call's arguments, summarized
	Output string // a tool call's result
	Done   bool
	CallID string
	Failed bool
	Args   json.RawMessage // a tool call's original arguments
	Turn   int             // index in Client.Turns of the turn it is in; -1 before any
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

	Entries   []*Entry
	Busy      bool
	Queued    []string         // prompts waiting for the running turn
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
	Models   []proto.Model // of the account last asked about
	Unknown  []Unknown

	launch    string
	nextCmd   int
	nextKey   int
	pending   map[string]*pending
	cursor    int64 // last durable event applied
	buffer    []proto.Event
	overflow  bool
	streaming *Entry
	thinking  *Entry // the thinking entry streaming in
	thought   *Entry // the streamed thinking entry its message has not yet sent whole
	want      string // session to show on (re)connect
	modelsReq string
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

func (c *Client) notice(format string, args ...any) {
	c.add(&Entry{Role: Notice, Text: fmt.Sprintf(format, args...), Done: true})
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
		if f.Event.Type == proto.EventLogin {
			c.loginProgress(*f.Event)
			return nil
		}
		if f.Event.Type == proto.EventSession {
			c.sessionUpdated(*f.Event) // any session's: the list shows them all
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
		c.fail(fmt.Sprintf("内核协议版本为 %d，应用协议版本为 %d。", f.Version, proto.Version))
		return nil
	}
	c.Phase, c.Err, c.Instance = Handshaking, "", f.Instance
	return []proto.Command{c.read(proto.MethodInitialize, nil, func(json.RawMessage) []proto.Command {
		cmds := []proto.Command{c.listAccounts(), c.listSessions(true)}
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
		c.notice("%s 失败：%s", p.method, f.Error.Message)
		return nil
	}
	if p.done != nil {
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
func (c *Client) listSessions(bind bool) proto.Command {
	return c.read(proto.MethodSessionList, nil, func(r json.RawMessage) []proto.Command {
		var list []proto.Session
		if err := json.Unmarshal(r, &list); err == nil {
			c.Sessions = list
			clear(c.Marks)
			for _, s := range list {
				if s.State != "" {
					c.Marks[s.ID] = s.State
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
		return []proto.Command{c.create()}
	})
}

func (c *Client) create() proto.Command {
	c.Phase = Syncing
	c.resetTranscript()
	p := &pending{mutation: true}
	p.done = func(r json.RawMessage) []proto.Command {
		var s proto.Session
		if err := json.Unmarshal(r, &s); err != nil || s.ID == "" {
			c.fail("创建会话未返回会话信息")
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
			c.fail("读取会话未返回会话信息")
			return nil
		}
		c.Current = &s.Session
		c.upsertSession(s.Session)
		c.Queued = s.Queued
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
		return []proto.Command{c.listSessions(true)}
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
	c.resetChanges()
	c.Workspace = Workspace{BranchStale: true}
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
		if err := json.Unmarshal(r, &page); err != nil {
			c.fail("读取事件失败：" + err.Error())
			return nil
		}
		for _, e := range page.Events {
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
		*c.Current = s
	}
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
		}
		c.thinking.Text += d.Text
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
		}
		c.streaming.Text += d.Text
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
	case proto.EventApprovalRequest:
		var a proto.Approval
		if json.Unmarshal(e.Data, &a) == nil && !slices.ContainsFunc(c.Approvals, func(x proto.Approval) bool { return x.ID == a.ID }) {
			c.Approvals = append(c.Approvals, a)
		}
	case proto.EventApprovalResolved:
		var a proto.ApprovalAnswer
		if json.Unmarshal(e.Data, &a) == nil {
			c.Approvals = slices.DeleteFunc(c.Approvals, func(x proto.Approval) bool { return x.ID == a.ApprovalID })
			delete(c.Answering, a.ApprovalID)
		}
	case proto.EventRetry:
		var r proto.Retry
		_ = json.Unmarshal(e.Data, &r)
		c.notice("正在重试（第 %d 次）：%s", r.Attempt, r.Error)
	case proto.EventTurnEnd:
		var t proto.TurnEnd
		_ = json.Unmarshal(e.Data, &t)
		c.endStream()
		c.Busy, c.Queued, c.Approvals = false, nil, nil
		c.thought = nil
		c.Workspace.BranchStale = true // the turn may have switched it
		clear(c.Answering)
		c.Usage = c.Usage.Add(t.Usage)
		if n := len(c.Turns); n > 0 && !c.Turns[n-1].Done {
			c.Turns[n-1].End, c.Turns[n-1].Done, c.Turns[n-1].Usage = e.At, true, t.Usage
		}
		switch {
		case t.Stopped:
			c.notice("已停止。")
		case t.Error != "":
			c.notice("本轮执行失败：%s", t.Error)
		}
	}
	return nil
}

// message adds a stored message: the user's prompt, the assistant's text
// and tool calls, or tool results (which travel as a user message).
func (c *Client) message(seq int64, m llm.Message) {
	var text strings.Builder
	for _, p := range m.Parts {
		text.WriteString(p.Text)
	}
	switch m.Role {
	case llm.User:
		if text.Len() > 0 {
			c.endStream()
			c.add(&Entry{Seq: seq, Role: User, Text: text.String(), Done: true})
			if i := slices.Index(c.Queued, text.String()); i >= 0 {
				c.Queued = slices.Delete(c.Queued, i, i+1) // the turn took it
			}
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
	return c.add(&Entry{Role: Tool, Text: call.Name, Detail: summarize(args), CallID: call.ID, Args: call.Args})
}

func (c *Client) toolResult(r llm.ToolResult) {
	for i := len(c.Entries) - 1; i >= 0; i-- {
		if en := c.Entries[i]; en.Role == Tool && en.CallID == r.CallID {
			en.Done, en.Failed, en.Output = true, r.IsError, r.Content
			return
		}
	}
}

// summarize shows a tool call's most telling argument.
func summarize(args map[string]any) string {
	for _, k := range []string{"path", "command", "pattern", "query", "url", "title"} {
		if s, ok := args[k].(string); ok && s != "" {
			if len(s) > 120 {
				s = s[:117] + "…"
			}
			return s
		}
	}
	return ""
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
		c.Err = "内核已退出。"
	}
}

// --- commands the user starts ---

// ErrNotReady is returned when the connection cannot take commands now.
var ErrNotReady = fmt.Errorf("未连接")

func (c *Client) mutation(method string, params any, done func(json.RawMessage) []proto.Command) ([]proto.Command, error) {
	if !c.CanAct() {
		return nil, ErrNotReady
	}
	return []proto.Command{c.mutate(method, params, done)}, nil
}

// Prompt sends text to the current session. While a turn runs, the text
// waits in Queued until the turn takes it.
func (c *Client) Prompt(text string) ([]proto.Command, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("没有可发送的内容")
	}
	session := c.Current
	return c.mutation(proto.MethodPrompt, proto.Prompt{SessionID: sessionID(session), Text: text}, func(r json.RawMessage) []proto.Command {
		var res proto.PromptResult
		if json.Unmarshal(r, &res) == nil && res.Queued && c.Current == session {
			c.Queued = append(c.Queued, text)
		}
		return nil
	})
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
		return nil, fmt.Errorf("当前没有运行中的任务")
	}
	return c.mutation(proto.MethodCancel, proto.SessionRef{SessionID: sessionID(c.Current)}, nil)
}

// Answer allows or denies an approval request; always allows the tool for
// the rest of the session.
func (c *Client) Answer(id string, allow, always bool) ([]proto.Command, error) {
	if c.Answering[id] {
		return nil, fmt.Errorf("已回答此审批")
	}
	cmds, err := c.mutation(proto.MethodApprovalRespond, proto.ApprovalAnswer{SessionID: sessionID(c.Current), ApprovalID: id, Allow: allow, Always: always}, nil)
	if err == nil {
		c.Answering[id] = true
		c.pending[cmds[0].ID].failed = func(e *proto.Error) []proto.Command {
			delete(c.Answering, id)
			c.notice("审批失败：%s", e.Message)
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

// Delete deletes a session; if it is shown, another is.
func (c *Client) Delete(id string) ([]proto.Command, error) {
	return c.mutation(proto.MethodSessionDelete, proto.SessionRef{SessionID: id}, func(json.RawMessage) []proto.Command {
		c.Sessions = slices.DeleteFunc(c.Sessions, func(s proto.Session) bool { return s.ID == id })
		if c.Current != nil && c.Current.ID == id {
			c.want = ""
			c.Phase = Syncing
			c.resetTranscript()
			return []proto.Command{c.listSessions(true)}
		}
		return nil
	})
}

// Update changes the current session: title, account, model, effort or
// approval mode. The kernel's session.updated event brings the result.
func (c *Client) Update(u proto.SessionUpdate) ([]proto.Command, error) {
	if u.Mode != nil && !slices.Contains([]string{"ask", "auto", "full"}, *u.Mode) {
		return nil, fmt.Errorf("未知权限模式 %q", *u.Mode)
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
