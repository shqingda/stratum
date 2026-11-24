package client

import (
	"encoding/json"
	"strings"

	"github.com/shqingda/stratum/locales"
	"github.com/shqingda/stratum/proto"
)

// Workspace is what the current session's folder is in git, read on
// request: the app asks while BranchStale is set.
type Workspace struct {
	proto.Workspace
	BranchStale bool
	Committing  bool
	Pushing     bool
	// Diffs are the uncommitted files asked for, HEAD against disk, by
	// path; a new reading of the workspace drops them.
	Diffs map[string]proto.ChangeDiff
	// BaseDiffs are the files of the branch asked for, its base against
	// disk.
	BaseDiffs map[string]proto.ChangeDiff
	Read      int // readings so far, to tell a new one
	req       string
}

// ReadWorkspace reads the current session's folder again; only the
// latest request's answer is kept, and a failure leaves no branch.
func (c *Client) ReadWorkspace() ([]proto.Command, error) {
	if !c.CanAct() {
		return nil, ErrNotReady
	}
	cmd := c.read(proto.MethodWorkspace, proto.WorkspaceQuery{Cwd: c.Current.Cwd}, nil)
	c.keepWorkspace(cmd.ID)
	return []proto.Command{cmd}, nil
}

func (c *Client) workspaceAction(method string, params any, failMsg func(string) string) ([]proto.Command, error) {
	if !c.CanAct() {
		return nil, ErrNotReady
	}
	cmd := c.read(method, params, nil)
	c.keepWorkspace(cmd.ID)
	failed := c.pending[cmd.ID].failed
	c.pending[cmd.ID].failed = func(e *proto.Error) []proto.Command {
		c.Workspace.BranchStale = true
		c.notice("fail", "%s", failMsg(e.Message))
		return failed(e)
	}
	return []proto.Command{cmd}, nil
}

// Commit commits everything in the current session's folder.
func (c *Client) Commit(message string) ([]proto.Command, error) {
	cmds, err := c.workspaceAction(proto.MethodCommit, proto.Commit{Cwd: c.Current.Cwd, Message: message}, locales.CommitFailed)
	if err == nil {
		c.Workspace.Committing = true
	}
	return cmds, err
}

// Push pushes the current branch to origin.
func (c *Client) Push() ([]proto.Command, error) {
	cmds, err := c.workspaceAction(proto.MethodPush, proto.WorkspaceQuery{Cwd: c.Current.Cwd}, locales.PushFailed)
	if err == nil {
		c.Workspace.Pushing = true
	}
	return cmds, err
}

// CreatePR pushes HEAD and opens a pull request.
func (c *Client) CreatePR(title, body string) ([]proto.Command, error) {
	cmds, err := c.workspaceAction(proto.MethodCreatePR, proto.CreatePR{Cwd: c.Current.Cwd, Title: title, Body: body}, locales.PrFailed)
	if err == nil {
		c.Workspace.Pushing = true
	}
	return cmds, err
}

// Checkout switches the current folder's checked-out branch.
func (c *Client) Checkout(branch string) ([]proto.Command, error) {
	return c.workspaceAction(proto.MethodCheckout, proto.Checkout{Cwd: c.Current.Cwd, Branch: branch}, locales.SwitchBranchFailed)
}

// keepWorkspace takes the answer to request id, unless a later request
// or another session has come since.
func (c *Client) keepWorkspace(id string) {
	session := c.Current.ID
	keep := func(w proto.Workspace) {
		if c.Workspace.req == id && c.Current != nil && c.Current.ID == session {
			c.Workspace.Workspace, c.Workspace.Committing, c.Workspace.Pushing = w, false, false
			c.Workspace.Diffs = map[string]proto.ChangeDiff{}
			c.Workspace.BaseDiffs = map[string]proto.ChangeDiff{}
			c.Workspace.Read++
		}
	}
	p := c.pending[id]
	p.done = func(r json.RawMessage) []proto.Command {
		var w proto.Workspace
		_ = json.Unmarshal(r, &w)
		keep(w)
		return nil
	}
	p.failed = func(*proto.Error) []proto.Command {
		keep(proto.Workspace{})
		return nil
	}
	c.Workspace.req, c.Workspace.BranchStale = id, false
}

// DiffUncommitted reads an uncommitted file as HEAD has it and as it is
// now into Workspace.Diffs.
func (c *Client) DiffUncommitted(path string) ([]proto.Command, error) {
	if !c.CanAct() {
		return nil, ErrNotReady
	}
	session, req := c.Current.ID, c.Workspace.req
	return []proto.Command{c.read(proto.MethodWorkspaceDiff, proto.WorkspaceQuery{Cwd: c.Current.Cwd, Path: path}, func(r json.RawMessage) []proto.Command {
		var d proto.ChangeDiff
		if c.Current != nil && c.Current.ID == session && c.Workspace.req == req && c.Workspace.Diffs != nil && json.Unmarshal(r, &d) == nil {
			c.Workspace.Diffs[path] = d
		}
		return nil
	})}, nil
}

// DiffBase reads a file of the branch as its base has it and as it is
// now into Workspace.BaseDiffs.
func (c *Client) DiffBase(path string) ([]proto.Command, error) {
	if !c.CanAct() {
		return nil, ErrNotReady
	}
	session, req := c.Current.ID, c.Workspace.req
	return []proto.Command{c.read(proto.MethodWorkspaceDiff, proto.WorkspaceQuery{Cwd: c.Current.Cwd, Path: path, Base: true}, func(r json.RawMessage) []proto.Command {
		var d proto.ChangeDiff
		if c.Current != nil && c.Current.ID == session && c.Workspace.req == req && c.Workspace.BaseDiffs != nil && json.Unmarshal(r, &d) == nil {
			c.Workspace.BaseDiffs[path] = d
		}
		return nil
	})}, nil
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
