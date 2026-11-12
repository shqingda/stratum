// Package proto is Talos's protocol between a client (the app, a test)
// and the kernel: JSON lines. A client sends commands; the kernel answers
// each with a response and streams events.
//
// Every session keeps a log of durable events numbered by Seq, which is
// also the session's revision. Streaming events (text as it is written)
// have Seq 0 and are not kept. A client that reconnects reads a snapshot,
// then the log after the revision it has.
package proto

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/shqingda/talos/llm"
)

// Version is the protocol version; initialize reports it.
const Version = 1

// MaxLine is the longest line either side sends.
const MaxLine = 16 << 20

// Command is a request from the client. ID names it, so that its receipt
// can be looked up when the response was lost.
type Command struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Frame types.
const (
	FrameReady    = "ready"
	FrameResponse = "response"
	FrameEvent    = "event"
)

// Frame is a line from the kernel.
type Frame struct {
	Type string `json:"type"`
	// ready
	Version  int    `json:"version,omitempty"`
	Instance string `json:"instance,omitempty"` // changes when the kernel restarts
	// response
	ID     string          `json:"id,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
	// event
	Event *Event `json:"event,omitempty"`
}

// Error is a failed command.
type Error struct {
	Code    string `json:"code"` // bad_request, not_found, busy, internal
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Errorf makes an Error.
func Errorf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Methods.
const (
	MethodInitialize      = "initialize"       // → Hello
	MethodSessionCreate   = "session.create"   // SessionCreate → Session
	MethodSessionList     = "session.list"     // → []Session
	MethodSessionGet      = "session.get"      // SessionRef → Snapshot
	MethodSessionUpdate   = "session.update"   // SessionUpdate → Session
	MethodSessionDelete   = "session.delete"   // SessionRef → {}
	MethodPrompt          = "prompt"           // Prompt → PromptResult
	MethodCancel          = "cancel"           // SessionRef → {}
	MethodEvents          = "events"           // EventsQuery → EventPage
	MethodApprovalRespond = "approval.respond" // ApprovalAnswer → {}
	MethodAccounts        = "accounts.list"    // → []Account
	MethodModels          = "models.list"      // ModelsQuery → []Model
	MethodReceipt         = "receipt.get"      // ReceiptRef → Receipt
)

// Hello answers initialize.
type Hello struct {
	Version  int    `json:"version"`
	Instance string `json:"instance"`
}

// Session is a conversation in a workspace.
type Session struct {
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	Cwd     string    `json:"cwd"`
	Account string    `json:"account"`
	Model   string    `json:"model"`
	Effort  string    `json:"effort,omitempty"`
	Mode    string    `json:"mode"` // ask, auto, full
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
}

// SessionCreate makes a session. Empty fields take defaults.
type SessionCreate struct {
	Cwd     string `json:"cwd"`
	Title   string `json:"title,omitempty"`
	Account string `json:"account,omitempty"`
	Model   string `json:"model,omitempty"`
	Effort  string `json:"effort,omitempty"`
	Mode    string `json:"mode,omitempty"`
}

// SessionRef names a session.
type SessionRef struct {
	SessionID string `json:"sessionId"`
}

// SessionUpdate changes the given fields of a session.
type SessionUpdate struct {
	SessionID string  `json:"sessionId"`
	Title     *string `json:"title,omitempty"`
	Account   *string `json:"account,omitempty"`
	Model     *string `json:"model,omitempty"`
	Effort    *string `json:"effort,omitempty"`
	Mode      *string `json:"mode,omitempty"`
}

// Snapshot is a session's state at a revision.
type Snapshot struct {
	Session   Session    `json:"session"`
	Revision  int64      `json:"revision"`
	Running   bool       `json:"running"`
	Queued    []string   `json:"queued,omitempty"`    // prompts waiting for the turn to take them
	Approvals []Approval `json:"approvals,omitempty"` // waiting for the user
}

// Prompt sends the user's message. While a turn runs, it joins that turn
// after its current tool calls.
type Prompt struct {
	SessionID string `json:"sessionId"`
	Text      string `json:"text"`
}

// PromptResult says what became of a prompt.
type PromptResult struct {
	Queued bool `json:"queued"` // joined the running turn
}

// EventsQuery reads a session's log after a revision.
type EventsQuery struct {
	SessionID string `json:"sessionId"`
	After     int64  `json:"after"`
	Limit     int    `json:"limit,omitempty"` // 500 when zero
	// --- 任务4 ---
	Before int64 `json:"before,omitempty"`
}

// EventPage is part of a log.
type EventPage struct {
	Events []Event `json:"events"`
	More   bool    `json:"more"`
}

// Event is something that happened in a session.
type Event struct {
	SessionID string          `json:"sessionId"`
	Seq       int64           `json:"seq,omitempty"` // 0 for streaming events
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data,omitempty"`
	At        time.Time       `json:"at"`
}

// Event types. Durable ones are kept in the log.
const (
	EventSession          = "session.updated"   // durable; Session
	EventTurnStart        = "turn.start"        // durable; TurnStart
	EventTurnEnd          = "turn.end"          // durable; TurnEnd
	EventMessage          = "message"           // durable; llm.Message
	EventApprovalRequest  = "approval.request"  // durable; Approval
	EventApprovalResolved = "approval.resolved" // durable; ApprovalAnswer
	EventTextDelta        = "text.delta"        // Delta
	EventThinkingDelta    = "thinking.delta"    // Delta
	EventToolStart        = "tool.start"        // ToolEvent
	EventToolEnd          = "tool.end"          // ToolEvent
	EventRetry            = "retry"             // Retry
	EventUsage            = "usage"             // llm.Usage of one request
)

// TurnStart begins a turn.
type TurnStart struct {
	Text string `json:"text"`
}

// TurnEnd ends one. Error is empty for a turn that finished.
type TurnEnd struct {
	Usage   llm.Usage `json:"usage"`
	Stopped bool      `json:"stopped,omitempty"` // the user cancelled
	Error   string    `json:"error,omitempty"`
}

// Delta is streamed text.
type Delta struct {
	Text string `json:"text"`
}

// ToolEvent brackets a tool call.
type ToolEvent struct {
	Call   llm.ToolCall    `json:"call"`
	Result *llm.ToolResult `json:"result,omitempty"`
}

// Retry says a request failed and is tried again.
type Retry struct {
	Attempt int    `json:"attempt"`
	Error   string `json:"error"`
}

// Approval asks the user to allow a tool call.
type Approval struct {
	ID      string          `json:"id"`
	Tool    string          `json:"tool"`
	Args    json.RawMessage `json:"args"`
	Kind    string          `json:"kind"` // read, write, exec
	Paths   []string        `json:"paths,omitempty"`
	Command string          `json:"command,omitempty"`
	Reason  string          `json:"reason"`
}

// ApprovalAnswer is the user's answer. Always allows the tool for the rest
// of the session.
type ApprovalAnswer struct {
	SessionID  string `json:"sessionId"`
	ApprovalID string `json:"approvalId"`
	Allow      bool   `json:"allow"`
	Always     bool   `json:"always,omitempty"`
}

// Account is a signed-in provider account.
type Account struct {
	ID           string `json:"id"` // provider or provider/label
	Provider     string `json:"provider"`
	Name         string `json:"name"`
	Email        string `json:"email,omitempty"`
	Subscription bool   `json:"subscription"`
}

// ModelsQuery lists an account's models.
type ModelsQuery struct {
	Account string `json:"account"`
}

// Model is a model to pick.
type Model struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Context   int     `json:"context"`
	Reasoning bool    `json:"reasoning"`
	Default   bool    `json:"default,omitempty"`
	Input     float64 `json:"input"`  // dollars per million tokens
	Output    float64 `json:"output"` // dollars per million tokens
}

// ReceiptRef names a command.
type ReceiptRef struct {
	ID string `json:"id"`
}

// Receipt says what became of a command.
type Receipt struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Status string `json:"status"` // accepted, completed, failed, unknown
	Error  string `json:"error,omitempty"`
}

// --- 任务4 ---

const MethodSearch = "search"

type SearchQuery struct {
	Query     string `json:"query,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

type SearchHit struct {
	SessionID string    `json:"sessionId,omitempty"`
	Seq       int64     `json:"seq,omitempty"`
	Role      string    `json:"role,omitempty"`
	Snippet   string    `json:"snippet,omitempty"`
	At        time.Time `json:"at,omitempty"`
}
