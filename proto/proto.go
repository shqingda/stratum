// Package proto holds the Go side of Hikaro's JSONL RPC v2 wire format,
// as packages/protocol/src/contracts.ts defines it. It decodes frames and
// encodes commands; it keeps no state and never retries anything.
package proto

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Version is the protocol version this client speaks.
const Version = 2

// MaxFrameBytes bounds one line of the wire, as the host does.
const MaxFrameBytes = 1 << 20

// Command is one request line: {id, scope?, method, params}.
type Command struct {
	ID     string         `json:"id"`
	Scope  string         `json:"scope,omitempty"`
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

// Encode returns the command as one line, with its trailing newline.
func (c Command) Encode() ([]byte, error) {
	if c.Params == nil {
		c.Params = map[string]any{}
	}
	b, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	if len(b)+1 > MaxFrameBytes {
		return nil, fmt.Errorf("command %s is %d bytes, over the %d-byte frame limit", c.Method, len(b)+1, MaxFrameBytes)
	}
	return append(b, '\n'), nil
}

// Frame types the host sends.
const (
	TypeReady       = "ready"
	TypeResponse    = "response"
	TypeEvent       = "event"
	TypeApproval    = "approval"
	TypeResolved    = "approval.resolved"
	TypeInvalidated = "session.invalidated"
)

// Frame is any line the host sends. Fields that a type does not use are
// left zero; Result keeps a response's result undecoded.
type Frame struct {
	Type           string          `json:"type"`
	ID             string          `json:"id,omitempty"`
	Scope          string          `json:"scope,omitempty"`
	Revision       uint64          `json:"revision,omitempty"`
	HostInstanceID string          `json:"hostInstanceId,omitempty"`
	ProtocolVer    int             `json:"protocolVersion,omitempty"`
	Result         json.RawMessage `json:"result,omitempty"`
	Error          string          `json:"error,omitempty"`
	Event          *Event          `json:"event,omitempty"`
	Request        *Permission     `json:"request,omitempty"`
	Allow          *bool           `json:"allow,omitempty"`
}

// Event is an agent event as the host publishes it.
type Event struct {
	SessionID string         `json:"sessionId"`
	Seq       uint64         `json:"seq"`
	Type      string         `json:"type"`
	Data      map[string]any `json:"data"`
	Timestamp float64        `json:"timestamp"`
}

// Permission is a pending approval request.
type Permission struct {
	ID                 string         `json:"id"`
	SessionID          string         `json:"sessionId"`
	AuthoritySessionID string         `json:"authoritySessionId,omitempty"`
	Tool               string         `json:"tool"`
	Reason             string         `json:"reason"`
	Arguments          map[string]any `json:"arguments"`
	Scope              string         `json:"scope,omitempty"`
}

// Session is a session as the host projects it.
type Session struct {
	ID             string          `json:"id"`
	Title          string          `json:"title"`
	Cwd            string          `json:"cwd"`
	CreatedAt      float64         `json:"createdAt"`
	ParentID       string          `json:"parentId,omitempty"`
	ModelSelection *ModelSelection `json:"modelSelection,omitempty"`
}

// ModelSelection names a provider's model.
type ModelSelection struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// Snapshot is session.current's result: the scope's selected session and
// its execution state as of Revision.
type Snapshot struct {
	SessionRole     *string      `json:"sessionRole"`
	Session         *Session     `json:"session"`
	HostInstanceID  string       `json:"hostInstanceId"`
	RunID           *string      `json:"runId"`
	RunStatus       string       `json:"runStatus"`
	Busy            bool         `json:"busy"`
	Stream          string       `json:"stream"`
	StreamTruncated bool         `json:"streamTruncated"`
	Cursor          uint64       `json:"cursor"`
	Revision        uint64       `json:"revision"`
	Approvals       []Permission `json:"approvals"`
}

// Workspace is workspace.current's result.
type Workspace struct {
	Cwd            string `json:"cwd"`
	PermissionMode string `json:"permissionMode"`
}

// Model is one entry of models.list.
type Model struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	Name     string `json:"name"`
}

// ModelPage is models.list's result.
type ModelPage struct {
	Items   []Model `json:"items"`
	HasMore bool    `json:"hasMore"`
}

// StoredMessage is one item of messages.read.
type StoredMessage struct {
	Seq     uint64 `json:"seq"`
	Message struct {
		Role string `json:"role"`
		Text string `json:"text"`
	} `json:"message"`
}

// MessagePage is messages.read's result.
type MessagePage struct {
	Items    []StoredMessage `json:"items"`
	HasOlder bool            `json:"hasOlder"`
	HasNewer bool            `json:"hasNewer"`
}

// Receipt is a durable record of how the host handled a command.
type Receipt struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Status string `json:"status"` // accepted, completed, failed, unknown
	Error  string `json:"error,omitempty"`
}

// PermissionModes are the workspace approval modes, as the TUI labels them.
var PermissionModes = []string{"ask", "auto", "full"}

// ErrUnknownType is returned for a frame whose type this client does not know.
var ErrUnknownType = errors.New("unknown frame type")

// Decode parses one line. It rejects malformed frames and those missing the
// fields their type requires, so that state is only ever built from valid ones.
func Decode(line []byte) (Frame, error) {
	var f Frame
	if len(line) > MaxFrameBytes {
		return f, fmt.Errorf("frame of %d bytes is over the limit", len(line))
	}
	if err := json.Unmarshal(line, &f); err != nil {
		return f, fmt.Errorf("malformed frame: %w", err)
	}
	switch f.Type {
	case TypeReady:
	case TypeResponse:
		if f.ID == "" {
			return f, errors.New("response without id")
		}
	case TypeEvent:
		if f.Scope == "" || f.Event == nil || f.Event.SessionID == "" || f.Event.Type == "" {
			return f, errors.New("event frame missing scope or event")
		}
	case TypeApproval:
		if f.Scope == "" || f.Request == nil || f.Request.ID == "" {
			return f, errors.New("approval frame missing scope or request")
		}
	case TypeResolved:
		if f.Scope == "" || f.ID == "" || f.Allow == nil {
			return f, errors.New("resolved frame missing scope, id or allow")
		}
	case TypeInvalidated:
		if f.Scope == "" {
			return f, errors.New("invalidated frame missing scope")
		}
	default:
		return f, fmt.Errorf("%w %q", ErrUnknownType, f.Type)
	}
	return f, nil
}

// String reads a string field of an event's data.
func (e *Event) String(key string) string {
	s, _ := e.Data[key].(string)
	return s
}
