// Package proto is Stratum's protocol between a client (the app, a test)
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

	"github.com/shqingda/stratum/llm"
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
	// Decoded holds an optional pre-decoded payload (e.g. *EventPage)
	// populated off the UI thread.
	Decoded any `json:"-"`
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
	MethodInitialize       = "initialize"        // → Hello
	MethodSessionCreate    = "session.create"    // SessionCreate → Session
	MethodSessionList      = "session.list"      // → []Session
	MethodSessionGet       = "session.get"       // SessionRef → Snapshot
	MethodSessionUpdate    = "session.update"    // SessionUpdate → Session
	MethodSessionDelete    = "session.delete"    // SessionRef → {}
	MethodSessionFlag      = "session.flag"      // SessionFlag → Session
	MethodSessionFork      = "session.fork"      // SessionRef → Session
	MethodSessionRetitle   = "session.retitle"   // SessionRef → Session
	MethodPrompt           = "prompt"            // Prompt → PromptResult
	MethodSideAsk          = "side.ask"          // SideAsk → SideAskResult
	MethodCancel           = "cancel"            // SessionRef → {}
	MethodEvents           = "events"            // EventsQuery → EventPage
	MethodApprovalRespond  = "approval.respond"  // ApprovalAnswer → {}
	MethodAccounts         = "accounts.list"     // → []Account
	MethodModels           = "models.list"       // ModelsQuery → []Model
	MethodReceipt          = "receipt.get"       // ReceiptRef → Receipt
	MethodPreviewOpen      = "preview.open"      // PreviewRequest → PreviewSnapshot
	MethodPreviewState     = "preview.state"     // {} → PreviewSnapshot
	MethodAutomationList   = "automation.list"   // {} → AutomationList
	MethodAutomationUpsert = "automation.upsert" // Automation → Automation
	MethodAutomationDelete = "automation.delete" // AutomationID → {}
	MethodAutomationRun    = "automation.run"    // AutomationID → Automation
	MethodHooksList        = "hooks.list"        // HooksQuery → HooksList
	MethodHooksSave        = "hooks.save"        // HooksSave → HooksList
	MethodPluginList       = "plugin.list"       // HooksQuery → PluginList
	MethodPluginInstall    = "plugin.install"    // PluginInstall → PluginList
	MethodPluginToggle     = "plugin.toggle"     // PluginToggle → PluginList
	MethodPluginRemove     = "plugin.remove"     // PluginRemove → PluginList
)

// Hello answers initialize.
type Hello struct {
	Version  int    `json:"version"`
	Instance string `json:"instance"`
}

// Session is a conversation in a workspace.
type Session struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Cwd     string `json:"cwd"`
	Account string `json:"account"`
	Model   string `json:"model"`
	Effort  string `json:"effort,omitempty"`
	Mode    string `json:"mode"` // ask, auto, full
	// Agent is the primary agent that leads the turns: build when empty,
	// plan, or one of the folder's or the user's own.
	Agent    string    `json:"agent,omitempty"`
	ParentID string    `json:"parentId,omitempty"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
	// State is set only in the list: "run" while a turn runs, "wait"
	// while it waits for an approval.
	State string `json:"state,omitempty"`
	// Pinned and Archived place it in the sidebar; Mark is "unread" or
	// "fail" for a turn that ended unseen.
	Pinned   bool   `json:"pinned,omitempty"`
	Archived bool   `json:"archived,omitempty"`
	Mark     string `json:"mark,omitempty"`
	Worktree bool   `json:"worktree,omitempty"`
	Repo     string `json:"repo,omitempty"`
}

// SessionFlag changes how the sidebar shows a session; nil leaves a
// field as it is.
type SessionFlag struct {
	SessionID string  `json:"sessionId"`
	Pinned    *bool   `json:"pinned,omitempty"`
	Archived  *bool   `json:"archived,omitempty"`
	Mark      *string `json:"mark,omitempty"`
}

// SessionCreate makes a session. Empty fields take defaults.
type SessionCreate struct {
	Cwd      string `json:"cwd"`
	Title    string `json:"title,omitempty"`
	Account  string `json:"account,omitempty"`
	Model    string `json:"model,omitempty"`
	Effort   string `json:"effort,omitempty"`
	Mode     string `json:"mode,omitempty"`
	Worktree bool   `json:"worktree,omitempty"`
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
	Agent     *string `json:"agent,omitempty"`
	Worktree  *bool   `json:"worktree,omitempty"`
}

// Snapshot is a session's state at a revision.
type Snapshot struct {
	Session   Session    `json:"session"`
	Revision  int64      `json:"revision"`
	Running   bool       `json:"running"`
	Queued    []Queued   `json:"queued,omitempty"`    // prompts waiting for the turn to take them
	Approvals []Approval `json:"approvals,omitempty"` // waiting for the user
	Draft     string     `json:"draft,omitempty"`     // what the user was writing to it
}

// MethodShell runs a command the user typed (!command) in the session's
// folder, sandboxed as its mode says: ShellRun → {}. It holds the session
// as a turn does; prompts sent meanwhile wait for it. What it printed goes
// into the conversation for the model to read.
const MethodShell = "shell.run"

// ShellRun is a command line of the user's.
type ShellRun struct {
	SessionID string `json:"sessionId"`
	Command   string `json:"command"`
}

const (
	EventShellStart = "shell.start" // ShellRun, as it starts
	EventShell      = "shell"       // durable; Shell, once it ended
)

// Shell is a command of the user's and what it printed.
type Shell struct {
	Command string `json:"command"`
	Output  string `json:"output"`
	Stopped bool   `json:"stopped,omitempty"`
}

// MethodFilesFind finds the files of a workspace a query names, for the
// composer's @: FilesQuery → []string, paths relative to Cwd, best first.
const MethodFilesFind = "files.find"

type FilesQuery struct {
	Cwd   string `json:"cwd"`
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"` // 20 when zero
}

// MethodDraftSet keeps what the user is writing to a session, so that it
// is there when they come back to it, after a crash too: DraftSet → {}.
const MethodDraftSet = "draft.set"

// MethodRewind returns a session to a prompt the user sent.
// Rewind → RewindResult. Action "both" restores files and drops the
// conversation from that prompt on, "conversation" drops only the
// conversation, "code" restores only files, "summarize" summarizes
// the conversation from that prompt on while keeping earlier turns,
// and "summarize_before" summarizes earlier turns up to that prompt. A
// shell command's effects are not files, so they stay.
const MethodRewind = "session.rewind"

// Rewind names the turn.start of the prompt to return to.
type Rewind struct {
	SessionID string `json:"sessionId"`
	TurnSeq   int64  `json:"turnSeq"`
	Action    string `json:"action"` // both, conversation, code, summarize, summarize_before
}

// RewindResult is what a rewind did. Prompt is set when the conversation
// was dropped, for the composer to offer again. Conflicts are files that
// no longer match the record; the conversation is then left in place.
type RewindResult struct {
	TurnSeq   int64    `json:"turnSeq"`
	Action    string   `json:"action"`
	Prompt    string   `json:"prompt,omitempty"`
	Paths     []string `json:"paths,omitempty"`
	Conflicts []string `json:"conflicts,omitempty"`
}

// DraftSet is a session's draft; empty forgets it.
type DraftSet struct {
	SessionID string `json:"sessionId"`
	Text      string `json:"text"`
}

// Prompt sends the user's message. While a turn runs, it waits in the
// queue and starts the next turn once this one ends; one that steers
// joins the running turn instead, after its current tool calls.
type Prompt struct {
	SessionID string      `json:"sessionId"`
	Text      string      `json:"text"`
	Images    []llm.Image `json:"images,omitempty"`
	Steer     bool        `json:"steer,omitempty"`
	// Compact summarizes the history now and runs no agent.
	Compact bool `json:"compact,omitempty"`
}

// PromptResult says what became of a prompt.
type PromptResult struct {
	Queued bool   `json:"queued"`       // waits in the running turn's queue
	ID     string `json:"id,omitempty"` // its Queued.ID
}

// SideAsk asks a quick read-only side question (/btw) against a session
// without interrupting its main turn or saving to its transcript.
type SideAsk struct {
	SessionID string `json:"sessionId"`
	Text      string `json:"text"`
}

type SideAskResult struct {
	Answer string `json:"answer"`
}

// Queued is a prompt waiting while a turn runs.
type Queued struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Images int    `json:"images,omitempty"`
	Steer  bool   `json:"steer,omitempty"` // joins the running turn
}

// MethodQueueEdit changes a waiting prompt: QueueEdit → {}.
const MethodQueueEdit = "queue.edit"

// QueueEdit removes a waiting prompt ("remove") or sends it into the
// running turn after its current tool calls ("steer").
type QueueEdit struct {
	SessionID string `json:"sessionId"`
	ID        string `json:"id"`
	Action    string `json:"action"`
}

// EventQueue streams a session's queue each time it changes. When the
// turns stop, by the user or by an error, the prompts still waiting are
// Returned to the user to send again.
const EventQueue = "queue"

// Queue is the data of EventQueue.
type Queue struct {
	Items    []Queued `json:"items,omitempty"`
	Returned []Queued `json:"returned,omitempty"`
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
	EventSuggestion       = "turn.suggestion"   // live; Suggestion
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

// Suggestion is a short follow-up prompt offered after a turn ends.
type Suggestion struct {
	Text string `json:"text"`
}

// TurnEnd ends one. Error is empty for a turn that finished.
type TurnEnd struct {
	Usage   llm.Usage `json:"usage"`
	Stopped bool      `json:"stopped,omitempty"` // the user cancelled
	Limit   bool      `json:"limit,omitempty"`   // it ran out of steps
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
	// Question, for Kind "question", is what the model asks the user,
	// with Options to pick from; the answer's Text replies.
	Question string   `json:"question,omitempty"`
	Options  []string `json:"options,omitempty"`
	// Apps, for Kind "computer", are the apps the agent asks to use.
	Apps []AppAccess `json:"apps,omitempty"`
	// Rule is what allowing it for the session, or always, lets run
	// unasked from then on.
	Rule *Rule `json:"rule,omitempty"`
}

// AppAccess is an app computer use asks for, and how much it may do
// there: "read" shows it, "click" clicks and scrolls, "full" does all.
type AppAccess struct {
	Name string `json:"name"`
	Tier string `json:"tier"`
}

// Rule lets calls run unasked: those of a tool, and of a command tool
// the command lines that start with Prefix's words. A rule for commands
// outside the sandbox allows only those.
type Rule struct {
	Tool         string `json:"tool"`
	Prefix       string `json:"prefix,omitempty"`
	Unrestricted bool   `json:"unrestricted,omitempty"`
}

// ApprovalAnswer is the user's answer. Always keeps the approval's rule
// for the rest of the session, Forever in the settings.
type ApprovalAnswer struct {
	SessionID  string `json:"sessionId"`
	ApprovalID string `json:"approvalId"`
	Allow      bool   `json:"allow"`
	Always     bool   `json:"always,omitempty"`
	Forever    bool   `json:"forever,omitempty"`
	// Text replies to a question, or says why a call was declined.
	Text string `json:"text,omitempty"`
	// Command is the command line the user changed it to before it ran.
	Command string `json:"command,omitempty"`
}

// Account is a signed-in provider account.
type Account struct {
	ID           string `json:"id"` // provider or provider/label
	Provider     string `json:"provider"`
	Name         string `json:"name"`
	Email        string `json:"email,omitempty"`
	Subscription bool   `json:"subscription"`
	// Model is the provider's default model.
	Model string        `json:"model,omitempty"`
	Quota *AccountQuota `json:"quota,omitempty"`
}

// AccountQuota holds remaining plan quota and reset timing for a subscription account.
type AccountQuota struct {
	RemainingPct int       `json:"remainingPct"` // 0..100, -1 if unknown
	ResetAt      time.Time `json:"resetAt,omitempty"`
	Window       string    `json:"window,omitempty"` // e.g. "5h", "7d", "monthly"
	Plan         string    `json:"plan,omitempty"`
	UpdatedAt    time.Time `json:"updatedAt,omitempty"`
}

// ModelsQuery lists an account's models.
type ModelsQuery struct {
	Account string `json:"account"`
}

// Model is a model to pick.
type Model struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Context    int     `json:"context"`
	Reasoning  bool    `json:"reasoning"`
	Images     bool    `json:"images,omitempty"` // takes images
	Default    bool    `json:"default,omitempty"`
	Input      float64 `json:"input"`                // dollars per million tokens
	Output     float64 `json:"output"`               // dollars per million tokens
	CacheRead  float64 `json:"cacheRead,omitempty"`  // dollars per million tokens
	CacheWrite float64 `json:"cacheWrite,omitempty"` // dollars per million tokens
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

// --- 任务3 ---

const (
	MethodChangesList    = "changes.list"
	MethodChangesDiff    = "changes.diff"
	MethodChangesRevert  = "changes.revert"
	MethodChangesRedo    = "changes.redo"
	EventChangesReverted = "changes.reverted"
	EventChangesRedone   = "changes.redone"
)

// ChangeQuery selects a turn and, for diff, one recorded absolute path.
type ChangeQuery struct {
	SessionID string `json:"sessionId,omitempty"`
	TurnSeq   int64  `json:"turnSeq,omitempty"`
	Path      string `json:"path,omitempty"`
}

type ChangeFile struct {
	Path    string `json:"path,omitempty"`
	Added   int    `json:"added,omitempty"`
	Removed int    `json:"removed,omitempty"`
}

type ChangeTurn struct {
	TurnSeq int64        `json:"turnSeq,omitempty"`
	Files   []ChangeFile `json:"files,omitempty"`
}

// Nil text denotes an absent file; a pointer to an empty string is an empty file.
type ChangeDiff struct {
	Path   string  `json:"path,omitempty"`
	Before *string `json:"before,omitempty"`
	After  *string `json:"after,omitempty"`
}

// ChangeResult is a restore: of the whole turn, or of Path alone.
type ChangeResult struct {
	TurnSeq   int64    `json:"turnSeq,omitempty"`
	Path      string   `json:"path,omitempty"`
	Paths     []string `json:"paths,omitempty"`
	Conflicts []string `json:"conflicts,omitempty"`
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

// MethodWorkspace reads what a session's folder is in git; MethodCommit
// commits everything in it; MethodPush pushes HEAD to origin;
// MethodCreatePR pushes and creates a GitHub pull request.
const (
	MethodWorkspace = "workspace.info"     // WorkspaceQuery → Workspace
	MethodCommit    = "workspace.commit"   // Commit → Workspace
	MethodPush      = "workspace.push"     // WorkspaceQuery → Workspace
	MethodCreatePR  = "workspace.pr"       // CreatePR → Workspace
	MethodCheckout  = "workspace.checkout" // Checkout → Workspace
	// MethodWorkspaceDiff reads a file as HEAD has it and as it is now:
	// WorkspaceQuery with Path → ChangeDiff.
	MethodWorkspaceDiff = "workspace.diff"
)

type Checkout struct {
	Cwd    string `json:"cwd"`
	Branch string `json:"branch"`
}

type CreatePR struct {
	Cwd   string `json:"cwd"`
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
}

type PullRequest struct {
	Number int    `json:"number"`
	Title  string `json:"title,omitempty"`
	URL    string `json:"url"`
	State  string `json:"state,omitempty"`  // open, merged, closed
	Checks string `json:"checks,omitempty"` // pass, fail, pending
}

type WorkspaceQuery struct {
	Cwd  string `json:"cwd"`
	Path string `json:"path,omitempty"`
	// Base reads the file as the branch's base has it, not HEAD.
	Base bool `json:"base,omitempty"`
}

// Workspace is a folder as git sees it. Branch is empty outside a
// repository, and a short commit for a detached head.
// Added and Removed count the lines not yet committed, untracked files'
// too; Dirty is set for any change.
type Workspace struct {
	Branch  string `json:"branch,omitempty"`
	Dirty   bool   `json:"dirty,omitempty"`
	Added   int    `json:"added,omitempty"`
	Removed int    `json:"removed,omitempty"`
	// Files are the files not committed, by absolute path.
	Files []ChangeFile `json:"files,omitempty"`
	// Base names what the branch came from, "main" say, and BaseFiles
	// are the files changed since, committed or not.
	Base      string       `json:"base,omitempty"`
	BaseFiles []ChangeFile `json:"baseFiles,omitempty"`
	Branches  []string     `json:"branches,omitempty"`
	Ahead     int          `json:"ahead,omitempty"`
	PR        *PullRequest `json:"pr,omitempty"`
	// Commits are HEAD's latest commits, newest first, and Recent the
	// files of the folder changed on disk last, newest first: what an
	// empty session shows of the folder. Recent is read outside git too.
	Commits []CommitSummary `json:"commits,omitempty"`
	Recent  []RecentFile    `json:"recent,omitempty"`
}

// CommitSummary is one commit as a list of them shows it.
type CommitSummary struct {
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
	Time    int64  `json:"time"` // Unix seconds, the committer's
}

// RecentFile is a file and when it was last changed on disk.
type RecentFile struct {
	Path string `json:"path"` // absolute
	Time int64  `json:"time"` // Unix seconds
}

type Commit struct {
	Cwd     string `json:"cwd"`
	Message string `json:"message"`
}

// --- 登录 ---

const (
	MethodLogin       = "auth.login"  // Login → Account, once signed in
	MethodLoginCancel = "auth.cancel" // → {}; ends the sign-in under way
	// EventLogin is a sign-in's progress, of no session; not kept.
	EventLogin = "auth.progress" // LoginProgress
	// EventAccounts pushes refreshed subscription quota data to clients.
	EventAccounts = "accounts.updated" // []Account
	// EventAutomation notifies clients when a scheduled automation finishes or fails.
	EventAutomation = "automation.done" // Automation
)

// Login signs in to a provider with one of its Methods, or with Key when
// Method is empty. Device asks for a code to enter on another device.
type Login struct {
	Provider string `json:"provider"`
	Method   string `json:"method,omitempty"`
	Key      string `json:"key,omitempty"`
	Device   bool   `json:"device,omitempty"`
}

// LoginProgress is what the user has to do: a page opened in the browser,
// or a code to enter at CodeURL.
type LoginProgress struct {
	URL     string `json:"url,omitempty"`
	Code    string `json:"code,omitempty"`
	CodeURL string `json:"codeUrl,omitempty"`
}

// --- 设置 ---

const (
	MethodSettings    = "settings.get" // → Settings
	MethodSetSettings = "settings.set" // Settings → Settings
	MethodLogout      = "auth.logout"  // AccountRef → {}
)

// Settings are what a new session starts with; empty fields take the
// kernel's defaults.
type Settings struct {
	Account string `json:"account,omitempty"`
	Model   string `json:"model,omitempty"`
	Effort  string `json:"effort,omitempty"`
	Mode    string `json:"mode,omitempty"`
	// Sub runs sub-agents, Light writes titles and summaries; nil means
	// the session's own model (and, for titles, its first words).
	Sub   *ModelRef `json:"sub,omitempty"`
	Light *ModelRef `json:"light,omitempty"`
	// Fallback moves a turn to the next account, at its default model,
	// when the one in use hits a rate limit or runs out of quota.
	Fallback bool `json:"fallback,omitempty"`
	// Theme is "light" or "dark", or "" to follow the system.
	Theme string `json:"theme,omitempty"`
	// SeedColor is the hex seed color (e.g. "#3B6698") for HCT theme generation.
	SeedColor string `json:"seedColor,omitempty"`
	// ColorStyle selects the HCT derivation strategy: "soft" (default),
	// "vivid", "tinted", or "monochrome".
	ColorStyle string `json:"colorStyle,omitempty"`
	// PureBlack uses true #000000 surface in dark mode when enabled.
	PureBlack bool `json:"pureBlack,omitempty"`
	// Quiet lists what the app does not tell the user about while in the
	// background: "wait", "done", "fail" notifications, or the "badge".
	Quiet []string `json:"quiet,omitempty"`
	// Detail is how much of a finished turn the transcript shows: "" its
	// steps folded into a line, "thinking" its steps and reasoning, "all"
	// every step on its own row.
	Detail string `json:"detail,omitempty"`
	// EnterSteers makes Return, while a turn runs, send into the turn
	// right away, and ⌘Return queue; by default it is the other way.
	EnterSteers bool `json:"enterSteers,omitempty"`
	// PreventSleep keeps the machine awake while a session is actively
	// running (not while waiting for approval), as in Codex and Zed.
	PreventSleep bool `json:"preventSleep,omitempty"`
	// NoSuggestions disables post-reply ghost prompt suggestions.
	NoSuggestions bool `json:"noSuggestions,omitempty"`
	// ComputerUse offers the agent the computer tool: the screen, the
	// mouse and the keyboard, each call asked for but in full access.
	ComputerUse bool `json:"computerUse,omitempty"`
	// NotifyWhen is when notifications show: "" while the app is in the
	// background, "always", or "never".
	NotifyWhen string `json:"notifyWhen,omitempty"`
	// TextSize zooms the window: "" for its size, "small" or "large".
	TextSize string `json:"textSize,omitempty"`
	// ReduceMotion turns off spring and ambient animations for accessibility.
	ReduceMotion bool `json:"reduceMotion,omitempty"`
	// CollapsedProjects lists workspace cwds collapsed in the sidebar.
	CollapsedProjects []string `json:"collapsedProjects,omitempty"`
	// SidebarWidth and InspectorWidth are the widths, in points, the user
	// dragged the sidebar and the review panel to; 0 is the default.
	SidebarWidth   float32 `json:"sidebarWidth,omitempty"`
	InspectorWidth float32 `json:"inspectorWidth,omitempty"`
	// BrowserTabs are the pages the panel's browser showed in its tabs,
	// and BrowserTab the one in front, which it opens again, as Safari
	// reopens its windows.
	BrowserTabs []string `json:"browserTabs,omitempty"`
	BrowserTab  int      `json:"browserTab,omitempty"`
	// BrowserDownloads is the folder the browser saves downloads to, or
	// "" for Downloads; BrowserAskDownload asks where to save each one, as
	// Safari's "Ask for each download" does.
	BrowserDownloads   string `json:"browserDownloads,omitempty"`
	BrowserAskDownload bool   `json:"browserAskDownload,omitempty"`
	// Rules are the calls allowed unasked in every session.
	Rules []Rule `json:"rules,omitempty"`
	// FavoriteModels and RecentModels keep model keys ("account/id") for
	// pinning and quick selection in the model picker.
	FavoriteModels []string `json:"favoriteModels,omitempty"`
	RecentModels   []string `json:"recentModels,omitempty"`
}

type ModelRef struct {
	Account string `json:"account"`
	Model   string `json:"model"`
	Effort  string `json:"effort,omitempty"`
}

// EventFallback records a turn moving to a backup account.
const EventFallback = "fallback"

type Fallback struct {
	From  string `json:"from"` // account/model
	To    string `json:"to"`
	Error string `json:"error"`
}

type AccountRef struct {
	Account string `json:"account"`
}

// --- 按块撤销 ---

const (
	// MethodChangesRevertHunk puts back the old lines of one hunk of what
	// a turn did to a file: HunkQuery → ChangeResult.
	MethodChangesRevertHunk = "changes.revertHunk"
	EventChangesPatched     = "changes.patched" // durable; ChangeResult
)

// HunkQuery names a hunk as "@@ -OldStart,OldLen +NewStart,NewLen @@" of
// the turn's diff of Path gives it, lines counted from 1.
type HunkQuery struct {
	ChangeQuery
	OldStart int `json:"oldStart"`
	OldLen   int `json:"oldLen"`
	NewStart int `json:"newStart"`
	NewLen   int `json:"newLen"`
}

// --- 上下文压缩 ---

// EventCompaction replaces the conversation before it with a summary in
// what the model is sent; durable; Compaction.
const EventCompaction = "compaction"

type Compaction struct {
	Summary string `json:"summary"`
	Asked   bool   `json:"asked,omitempty"`   // the user asked for it, the context was not full
	Partial bool   `json:"partial,omitempty"` // summarizes only from a rewind point on; earlier turns stay
	UpToSeq int64  `json:"upToSeq,omitempty"` // summarizes messages with seq < UpToSeq; later turns stay
}

// EventTaskStep is a sub-agent starting a tool call, streamed for the
// task call that runs it; it is not recorded.
const EventTaskStep = "task.step"

type TaskStep struct {
	CallID string       `json:"callId"`
	Task   string       `json:"task,omitempty"` // a run_tasks task's id
	Call   llm.ToolCall `json:"call"`
}

// EventTaskState is a run_tasks task changing state, streamed.
const EventTaskState = "task.state"

type TaskState struct {
	CallID string `json:"callId"`
	Task   string `json:"task"`
	State  string `json:"state"` // run, done, fail, skip
}

// MCP servers: MethodMCPList says how each configured one is for a
// folder; MethodMCPTrust lets a folder's .mcp.json server run, or stops
// it. EventMCP, sent to every client, says a server connected or failed.
const (
	MethodMCPList     = "mcp.list"     // MCPQuery → MCPList
	MethodMCPTrust    = "mcp.trust"    // MCPTrust → MCPList
	MethodMCPSave     = "mcp.save"     // MCPSave → MCPList
	MethodMCPRemove   = "mcp.remove"   // MCPRemove → MCPList
	MethodSkillSave   = "skill.save"   // SkillSave → MCPList
	MethodSkillToggle = "skill.toggle" // SkillToggle → MCPList
	MethodSkillRemove = "skill.remove" // SkillRemove → MCPList
	EventMCP          = "mcp.changed"
)

type MCPSave struct {
	Cwd     string   `json:"cwd"`
	Name    string   `json:"name"`
	Project bool     `json:"project,omitempty"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	URL     string   `json:"url,omitempty"`
}

type MCPRemove struct {
	Cwd     string `json:"cwd"`
	Name    string `json:"name"`
	Project bool   `json:"project,omitempty"`
}

type SkillSave struct {
	Cwd         string `json:"cwd"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Body        string `json:"body,omitempty"`
	Project     bool   `json:"project,omitempty"`
	Source      string `json:"source,omitempty"`
}

type SkillToggle struct {
	Cwd     string `json:"cwd"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

type SkillRemove struct {
	Cwd     string `json:"cwd"`
	Name    string `json:"name"`
	Project bool   `json:"project,omitempty"`
}

type MCPQuery struct {
	Cwd string `json:"cwd"`
}

type MCPTrust struct {
	Cwd   string `json:"cwd"`
	Name  string `json:"name"`
	Trust bool   `json:"trust"`
}

type MCPList struct {
	Servers []MCPServer `json:"servers"`
	Skills  []SkillInfo `json:"skills,omitempty"` // the folder's and the user's skills
	// Formatters format a file after the agent writes it: the folder's
	// .stratum/formatters.json rules, then the built-in gofmt.
	Formatters []FormatterInfo `json:"formatters,omitempty"`
	// Agents are the built-in, plugin, user and folder agents: primary ones
	// lead a session, subagents take tasks and @name in a message.
	Agents []AgentInfo `json:"agents,omitempty"`
	// Commands are prompts kept as files: .stratum/commands/*.md, .talos/commands/*.md
	// and .claude/commands/*.md in the folder, then ~/.stratum/commands and
	// ~/.talos/commands.
	Commands []CommandInfo `json:"commands,omitempty"`
	// Resources are what connected servers offer for @server:uri.
	Resources []MCPResource `json:"resources,omitempty"`
	// Prompts are what connected servers offer for /server:name.
	Prompts []MCPPrompt `json:"prompts,omitempty"`
	Error   string      `json:"error,omitempty"` // a config file that did not read
}

// MCPResource is one resource a connected server listed.
type MCPResource struct {
	Server      string `json:"server"`
	URI         string `json:"uri"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

// MCPPrompt is one prompt a connected server listed, for /server:name.
type MCPPrompt struct {
	Server      string   `json:"server"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Arguments   []string `json:"arguments,omitempty"`
}

// CommandInfo is a saved prompt the / menu offers; $ARGUMENTS in Body is
// where what the user types after it goes.
// FormatterInfo is one formatter rule.
type FormatterInfo struct {
	Name       string   `json:"name"`
	Command    []string `json:"command"`
	Extensions []string `json:"extensions"`
	Disabled   bool     `json:"disabled,omitempty"`
	BuiltIn    bool     `json:"builtIn,omitempty"`
}

// AgentInfo describes an agent for the composer and settings.
type AgentInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Mode        string `json:"mode"` // primary, subagent or all
	Model       string `json:"model,omitempty"`
	Hidden      bool   `json:"hidden,omitempty"`
	BuiltIn     bool   `json:"builtIn,omitempty"`
	Source      string `json:"source,omitempty"` // the file that defines it
}

type CommandInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Body        string `json:"body"`
}

// SkillInfo names a skill the / menu and settings offer.
type SkillInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Dir         string `json:"dir,omitempty"`
	Body        string `json:"body,omitempty"`
	Project     bool   `json:"project,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
}

type MCPServer struct {
	Name    string   `json:"name"`
	Project bool     `json:"project,omitempty"` // from the folder's .mcp.json
	Target  string   `json:"target"`            // the command or URL
	State   string   `json:"state"`             // untrusted, connecting, ok, fail
	Error   string   `json:"error,omitempty"`
	Tools   []string `json:"tools,omitempty"`
	Logs    []string `json:"logs,omitempty"`
}

// PreviewRequest opens or refreshes a URL in the built-in browser preview.
type PreviewRequest struct {
	URL string `json:"url"`
}

// PreviewConsoleEntry is one console message or uncaught exception.
type PreviewConsoleEntry struct {
	Level  string `json:"level"`
	Text   string `json:"text"`
	Source string `json:"source,omitempty"`
	Line   int    `json:"line,omitempty"`
}

// PreviewNetworkEntry is one failed HTTP request or >=400 response.
type PreviewNetworkEntry struct {
	URL    string `json:"url"`
	Status int    `json:"status,omitempty"`
	Method string `json:"method,omitempty"`
	Error  string `json:"error,omitempty"`
}

// BrowserTab is one tab of the panel's browser.
type BrowserTab struct {
	ID     int    `json:"id"`
	URL    string `json:"url"`
	Title  string `json:"title,omitempty"`
	Active bool   `json:"active,omitempty"`
}

// PreviewSnapshot is the state of the built-in browser preview.
type PreviewSnapshot struct {
	URL        string                `json:"url"`
	Title      string                `json:"title,omitempty"`
	Status     int                   `json:"status,omitempty"`
	Screenshot []byte                `json:"screenshot,omitempty"`
	Console    []PreviewConsoleEntry `json:"console,omitempty"`
	Network    []PreviewNetworkEntry `json:"network,omitempty"`
	Error      string                `json:"error,omitempty"`
	UpdatedAt  time.Time             `json:"updatedAt,omitempty"`
}

// Automation is a background scheduled task (cron or interval).
type Automation struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Prompt        string    `json:"prompt"`
	Cwd           string    `json:"cwd"`
	Schedule      string    `json:"schedule"` // cron expression, @hourly, @daily, or @every 30m
	Mode          string    `json:"mode,omitempty"`
	Enabled       bool      `json:"enabled"`
	LastRunAt     time.Time `json:"lastRunAt,omitempty"`
	NextRunAt     time.Time `json:"nextRunAt,omitempty"`
	LastSessionID string    `json:"lastSessionId,omitempty"`
	LastStatus    string    `json:"lastStatus,omitempty"` // running, ok, error
	LastError     string    `json:"lastError,omitempty"`
}

type AutomationList struct {
	Items []Automation `json:"items"`
}

type AutomationID struct {
	ID string `json:"id"`
}

// HookEntry is one hook rule shown and edited in the Hooks UI.
type HookEntry struct {
	Event    string `json:"event"` // PreToolUse, PostToolUse, Stop
	Matcher  string `json:"matcher,omitempty"`
	Command  string `json:"command"`
	Project  bool   `json:"project,omitempty"`
	Source   string `json:"source,omitempty"`
	Editable bool   `json:"editable,omitempty"`
}

type HooksQuery struct {
	Cwd string `json:"cwd"`
}

type HooksList struct {
	Hooks []HookEntry `json:"hooks"`
	Error string      `json:"error,omitempty"`
}

type HooksSave struct {
	Cwd     string      `json:"cwd"`
	Project bool        `json:"project"`
	Hooks   []HookEntry `json:"hooks"`
}

// PluginInfo is an installed plugin package bundling skills, hooks, commands, or MCP servers.
type PluginInfo struct {
	Name        string   `json:"name"`
	Version     string   `json:"version,omitempty"`
	Description string   `json:"description,omitempty"`
	Author      string   `json:"author,omitempty"`
	Dir         string   `json:"dir,omitempty"`
	Project     bool     `json:"project,omitempty"`
	Enabled     bool     `json:"enabled"`
	Skills      int      `json:"skills,omitempty"`
	Hooks       int      `json:"hooks,omitempty"`
	MCPServers  []string `json:"mcpServers,omitempty"`
}

type PluginList struct {
	Plugins []PluginInfo `json:"plugins"`
	Error   string       `json:"error,omitempty"`
}

type PluginInstall struct {
	Cwd     string `json:"cwd"`
	Source  string `json:"source"`
	Project bool   `json:"project,omitempty"`
}

type PluginToggle struct {
	Cwd     string `json:"cwd"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

type PluginRemove struct {
	Cwd  string `json:"cwd"`
	Name string `json:"name"`
}
