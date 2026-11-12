package client

import (
	"encoding/json"

	"github.com/shqingda/talos/proto"
)

// Workspace is what the current session's folder is in git, read on
// request: the app asks while BranchStale is set.
type Workspace struct {
	proto.Workspace
	BranchStale bool
	Committing  bool
	req         string
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
		c.notice("提交失败：%s", e.Message)
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
			c.Workspace.Workspace, c.Workspace.Committing = w, false
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
