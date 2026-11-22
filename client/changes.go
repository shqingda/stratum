package client

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/shqingda/stratum/locales"
	"github.com/shqingda/stratum/proto"
)

// Changes is what the turns of the current session changed in files, read
// on request: the review panel asks while it shows and Stale is set.
type Changes struct {
	Turns []proto.ChangeTurn // oldest first
	// Reverted holds the turns whose changes were taken back and not
	// redone since, as the session's log tells.
	Reverted map[int64]bool
	// RevertedFiles holds files taken back alone, by DiffKey.
	RevertedFiles map[string]bool
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

// FileReverted tells whether a file's changes in a turn are taken back.
func (ch *Changes) FileReverted(turn int64, path string) bool {
	return ch.Reverted[turn] || ch.RevertedFiles[DiffKey(turn, path)]
}

func (c *Client) resetChanges() {
	c.Changes = Changes{Reverted: map[int64]bool{}, RevertedFiles: map[string]bool{}, Diffs: map[string]proto.ChangeDiff{}, Stale: true}
}

// changesEvent follows the log: a turn's end or a restore makes the list
// stale.
func (c *Client) changesEvent(e proto.Event) {
	switch e.Type {
	case proto.EventTurnEnd:
		c.Changes.Stale = true
	case proto.EventChangesPatched:
		c.Changes.Stale = true
		clear(c.Changes.Diffs)
	case proto.EventChangesReverted, proto.EventChangesRedone:
		var r proto.ChangeResult
		if json.Unmarshal(e.Data, &r) == nil {
			reverted := e.Type == proto.EventChangesReverted
			switch {
			case r.Path != "" && reverted:
				c.Changes.RevertedFiles[DiffKey(r.TurnSeq, r.Path)] = true
			case r.Path != "":
				delete(c.Changes.RevertedFiles, DiffKey(r.TurnSeq, r.Path))
				if c.Changes.Reverted[r.TurnSeq] {
					// The rest of the turn stays taken back.
					delete(c.Changes.Reverted, r.TurnSeq)
					for _, t := range c.Changes.Turns {
						if t.TurnSeq != r.TurnSeq {
							continue
						}
						for _, f := range t.Files {
							if f.Path != r.Path {
								c.Changes.RevertedFiles[DiffKey(r.TurnSeq, f.Path)] = true
							}
						}
					}
				}
			case reverted:
				c.Changes.Reverted[r.TurnSeq] = true
			default:
				delete(c.Changes.Reverted, r.TurnSeq)
				prefix := DiffKey(r.TurnSeq, "")
				for k := range c.Changes.RevertedFiles {
					if strings.HasPrefix(k, prefix) {
						delete(c.Changes.RevertedFiles, k)
					}
				}
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

// Revert takes back the files a turn changed, or only path (absolute);
// redo puts the changes back. The session's log brings the outcome.
func (c *Client) Revert(turn int64, path string, redo bool) ([]proto.Command, error) {
	method := proto.MethodChangesRevert
	if redo {
		method = proto.MethodChangesRedo
	}
	cmds, err := c.mutation(method, proto.ChangeQuery{SessionID: sessionID(c.Current), TurnSeq: turn, Path: path}, func(r json.RawMessage) []proto.Command {
		c.Workspace.BranchStale = true
		var res proto.ChangeResult
		if json.Unmarshal(r, &res) == nil && len(res.Conflicts) > 0 {
			c.notice("", "%s", locales.FilesChangedSince(len(res.Conflicts), strings.Join(res.Conflicts, locales.ListSeparator())))
		}
		return nil
	})
	if err == nil {
		c.pending[cmds[0].ID].failed = func(e *proto.Error) []proto.Command {
			c.notice("fail", "%s", locales.RestoreFailed(e.Message))
			return nil
		}
	}
	return cmds, err
}

// RevertHunk puts back the old lines of one hunk of a turn's change to
// path (absolute). The session's log brings the outcome.
func (c *Client) RevertHunk(turn int64, path string, oldStart, oldLen, newStart, newLen int) ([]proto.Command, error) {
	q := proto.HunkQuery{ChangeQuery: proto.ChangeQuery{SessionID: sessionID(c.Current), TurnSeq: turn, Path: path},
		OldStart: oldStart, OldLen: oldLen, NewStart: newStart, NewLen: newLen}
	cmds, err := c.mutation(proto.MethodChangesRevertHunk, q, func(r json.RawMessage) []proto.Command {
		c.Workspace.BranchStale = true
		var res proto.ChangeResult
		if json.Unmarshal(r, &res) == nil && len(res.Conflicts) > 0 {
			c.notice("", "%s", locales.FileChangedSince())
		}
		return nil
	})
	if err == nil {
		c.pending[cmds[0].ID].failed = func(e *proto.Error) []proto.Command {
			c.notice("fail", "%s", locales.UndoHunkFailed(e.Message))
			return nil
		}
	}
	return cmds, err
}
