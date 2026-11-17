package client

import (
	"encoding/json"

	"github.com/shqingda/talos/locales"
	"github.com/shqingda/talos/proto"
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

// Commit commits everything in the current session's folder.
func (c *Client) Commit(message string) ([]proto.Command, error) {
	if !c.CanAct() {
		return nil, ErrNotReady
	}
	cmd := c.read(proto.MethodCommit, proto.Commit{Cwd: c.Current.Cwd, Message: message}, nil)
	c.keepWorkspace(cmd.ID)
	c.Workspace.Committing = true
	failed := c.pending[cmd.ID].failed
	c.pending[cmd.ID].failed = func(e *proto.Error) []proto.Command {
		c.Workspace.BranchStale = true // the answer carries no workspace
		c.notice("fail", "%s", locales.CommitFailed(e.Message))
		return failed(e)
	}
	return []proto.Command{cmd}, nil
}

// Push pushes the current branch to origin.
func (c *Client) Push() ([]proto.Command, error) {
	if !c.CanAct() {
		return nil, ErrNotReady
	}
	cmd := c.read(proto.MethodPush, proto.WorkspaceQuery{Cwd: c.Current.Cwd}, nil)
	c.keepWorkspace(cmd.ID)
	c.Workspace.Pushing = true
	failed := c.pending[cmd.ID].failed
	c.pending[cmd.ID].failed = func(e *proto.Error) []proto.Command {
		c.Workspace.BranchStale = true
		c.notice("fail", "%s", locales.PushFailed(e.Message))
		return failed(e)
	}
	return []proto.Command{cmd}, nil
}

// CreatePR pushes HEAD and opens a pull request.
func (c *Client) CreatePR(title, body string) ([]proto.Command, error) {
	if !c.CanAct() {
		return nil, ErrNotReady
	}
	cmd := c.read(proto.MethodCreatePR, proto.CreatePR{Cwd: c.Current.Cwd, Title: title, Body: body}, nil)
	c.keepWorkspace(cmd.ID)
	c.Workspace.Pushing = true
	failed := c.pending[cmd.ID].failed
	c.pending[cmd.ID].failed = func(e *proto.Error) []proto.Command {
		c.Workspace.BranchStale = true
		c.notice("fail", "%s", locales.PrFailed(e.Message))
		return failed(e)
	}
	return []proto.Command{cmd}, nil
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
