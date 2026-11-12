package client

import (
	"encoding/json"

	"github.com/shqingda/talos/proto"
)

// Workspace is what the current session's folder is in git, read on
// request: the app asks while BranchStale is set.
type Workspace struct {
	Branch      string
	BranchStale bool
	req         string
}

// ReadWorkspace reads the current session's branch again; only the
// latest request's answer is kept, and a failure leaves no branch.
func (c *Client) ReadWorkspace() ([]proto.Command, error) {
	if !c.CanAct() {
		return nil, ErrNotReady
	}
	session := c.Current.ID
	var cmd proto.Command
	keep := func(branch string) {
		if c.Workspace.req == cmd.ID && c.Current != nil && c.Current.ID == session {
			c.Workspace.Branch = branch
		}
	}
	cmd = c.read(proto.MethodWorkspace, proto.WorkspaceQuery{Cwd: c.Current.Cwd}, func(r json.RawMessage) []proto.Command {
		var w proto.Workspace
		_ = json.Unmarshal(r, &w)
		keep(w.Branch)
		return nil
	})
	c.pending[cmd.ID].failed = func(*proto.Error) []proto.Command {
		keep("")
		return nil
	}
	c.Workspace.req, c.Workspace.BranchStale = cmd.ID, false
	return []proto.Command{cmd}, nil
}
