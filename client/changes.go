package client

import (
	"encoding/json"
	"strconv"

	"github.com/shqingda/talos/proto"
)

// Changes is what the turns of the current session changed in files, read
// on request: the review panel asks while it shows and Stale is set.
type Changes struct {
	Turns []proto.ChangeTurn // oldest first
	// Reverted holds the turns whose changes were taken back and not
	// redone since, as the session's log tells.
	Reverted map[int64]bool
	// Diffs are the before and after of files asked for, by DiffKey.
	Diffs map[string]proto.ChangeDiff
	// Stale is set when a turn ended or files were restored since Turns
	// were read, and when they never were.
	Stale   bool
	Loading bool
	Err     string

	listReq string
}

// DiffKey names one file of one turn in Changes.Diffs.
func DiffKey(turn int64, path string) string { return strconv.FormatInt(turn, 10) + ":" + path }

func (c *Client) resetChanges() {
	c.Changes = Changes{Reverted: map[int64]bool{}, Diffs: map[string]proto.ChangeDiff{}, Stale: true}
}

// changesEvent follows the log: a turn's end or a restore makes the list
// stale.
func (c *Client) changesEvent(e proto.Event) {
	switch e.Type {
	case proto.EventTurnEnd:
		c.Changes.Stale = true
	case proto.EventChangesReverted, proto.EventChangesRedone:
		var r proto.ChangeResult
		if json.Unmarshal(e.Data, &r) == nil {
			if e.Type == proto.EventChangesReverted {
				c.Changes.Reverted[r.TurnSeq] = true
			} else {
				delete(c.Changes.Reverted, r.TurnSeq)
			}
		}
		c.Changes.Stale = true
		clear(c.Changes.Diffs)
	}
}

// ListChanges reads the turns' changes again; only the latest request's
// answer is kept.
func (c *Client) ListChanges() ([]proto.Command, error) {
	if !c.CanAct() {
		return nil, ErrNotReady
	}
	session := c.Current.ID
	var cmd proto.Command
	cmd = c.read(proto.MethodChangesList, proto.SessionRef{SessionID: session}, func(r json.RawMessage) []proto.Command {
		if c.Changes.listReq != cmd.ID || c.Current == nil || c.Current.ID != session {
			return nil
		}
		c.Changes.Loading, c.Changes.Err, c.Changes.Turns = false, "", nil
		_ = json.Unmarshal(r, &c.Changes.Turns)
		return nil
	})
	c.pending[cmd.ID].failed = func(e *proto.Error) []proto.Command {
		if c.Changes.listReq == cmd.ID {
			c.Changes.Loading, c.Changes.Err = false, e.Message
		}
		return nil
	}
	c.Changes.listReq, c.Changes.Loading, c.Changes.Stale = cmd.ID, true, false
	clear(c.Changes.Diffs)
	return []proto.Command{cmd}, nil
}

// DiffChange reads a file's before and after for one turn into Diffs.
func (c *Client) DiffChange(turn int64, path string) ([]proto.Command, error) {
	if !c.CanAct() {
		return nil, ErrNotReady
	}
	session, key := c.Current.ID, DiffKey(turn, path)
	cmd := c.read(proto.MethodChangesDiff, proto.ChangeQuery{SessionID: session, TurnSeq: turn, Path: path}, func(r json.RawMessage) []proto.Command {
		var d proto.ChangeDiff
		if c.Current != nil && c.Current.ID == session && json.Unmarshal(r, &d) == nil {
			c.Changes.Diffs[key] = d
		}
		return nil
	})
	c.pending[cmd.ID].failed = func(e *proto.Error) []proto.Command {
		if c.Current != nil && c.Current.ID == session {
			c.Changes.Err = e.Message
		}
		return nil
	}
	return []proto.Command{cmd}, nil
}

// Revert takes back the files a turn changed; redo puts its changes back.
// The session's log brings the outcome.
func (c *Client) Revert(turn int64, redo bool) ([]proto.Command, error) {
	method := proto.MethodChangesRevert
	if redo {
		method = proto.MethodChangesRedo
	}
	cmds, err := c.mutation(method, proto.ChangeQuery{SessionID: sessionID(c.Current), TurnSeq: turn}, func(r json.RawMessage) []proto.Command {
		var res proto.ChangeResult
		if json.Unmarshal(r, &res) == nil && len(res.Conflicts) > 0 {
			c.notice("%d 个文件在这之后又被改过，没有动它们：%v", len(res.Conflicts), res.Conflicts)
		}
		return nil
	})
	if err == nil {
		c.pending[cmds[0].ID].failed = func(e *proto.Error) []proto.Command {
			c.notice("恢复文件失败：%s", e.Message)
			return nil
		}
	}
	return cmds, err
}
