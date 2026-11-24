package client

import (
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/locales"
	"github.com/shqingda/stratum/proto"
)

// Flag pins, archives or marks a session.
func (c *Client) Flag(f proto.SessionFlag) ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	return []proto.Command{c.mutate(proto.MethodSessionFlag, f, func(r json.RawMessage) []proto.Command {
		c.sessionUpdated(proto.Event{Data: r})
		return nil
	})}, nil
}

// Rename gives a session a title.
func (c *Client) Rename(id, title string) ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	return []proto.Command{c.mutate(proto.MethodSessionUpdate, proto.SessionUpdate{SessionID: id, Title: &title}, func(r json.RawMessage) []proto.Command {
		c.sessionUpdated(proto.Event{Data: r})
		return nil
	})}, nil
}

// Retitle asks the kernel to summarize the session into a new title.
func (c *Client) Retitle(id string) ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	if id == "" && c.Current != nil {
		id = c.Current.ID
	}
	if id == "" {
		return nil, ErrNotReady
	}
	return c.mutation(proto.MethodSessionRetitle, proto.SessionRef{SessionID: id}, func(r json.RawMessage) []proto.Command {
		c.sessionUpdated(proto.Event{Data: r})
		return nil
	})
}

// Seen clears the mark of a session whose ended turn the user has now
// seen.
func (c *Client) Seen(id string) []proto.Command {
	marked := c.Marks[id] == "unread" || c.Marks[id] == "fail"
	for i := range c.Sessions {
		if c.Sessions[i].ID == id && c.Sessions[i].Mark != "" {
			c.Sessions[i].Mark, marked = "", true
		}
	}
	if !marked || c.Phase != Ready {
		return nil
	}
	if c.Current != nil && c.Current.ID == id {
		c.Current.Mark = ""
	}
	if m := c.Marks[id]; m == "unread" || m == "fail" {
		delete(c.Marks, id)
	}
	none := ""
	return []proto.Command{c.mutate(proto.MethodSessionFlag, proto.SessionFlag{SessionID: id, Mark: &none}, nil)}
}

// Prompt sends text and images to the current session. While a turn
// runs, the prompt waits in Queued and starts the next turn.
func (c *Client) Prompt(text string, images ...llm.Image) ([]proto.Command, error) {
	return c.prompt(false, text, images)
}

// Steer sends text and images into the running turn, after its current
// tool calls; with none running it is a prompt.
func (c *Client) Steer(text string, images ...llm.Image) ([]proto.Command, error) {
	return c.prompt(true, text, images)
}

func (c *Client) prompt(steer bool, text string, images []llm.Image) ([]proto.Command, error) {
	if strings.TrimSpace(text) == "" && len(images) == 0 {
		return nil, errors.New(locales.NothingToSend())
	}
	c.Suggestion = ""
	p := proto.Prompt{SessionID: sessionID(c.Current), Text: text, Images: images, Steer: steer}
	return c.mutation(proto.MethodPrompt, p, nil) // the queue streams what became of it
}

// ClearSide resets the ephemeral /btw side-question state.
func (c *Client) ClearSide() {
	c.SideSession, c.SideQuestion, c.SideAnswer, c.SideErr, c.SidePending = "", "", "", "", false
}

// SideAsk asks a one-off read-only side question (/btw) against the current
// session without interrupting its main turn or saving to its transcript.
func (c *Client) SideAsk(text string) ([]proto.Command, error) {
	q := strings.TrimSpace(text)
	if q == "" {
		return nil, errors.New(locales.NothingToSend())
	}
	sid := sessionID(c.Current)
	if sid == "" {
		return nil, ErrNotReady
	}
	c.SideSession = sid
	c.SideQuestion = q
	c.SideAnswer = ""
	c.SideErr = ""
	c.SidePending = true
	cmds, err := c.mutation(proto.MethodSideAsk, proto.SideAsk{SessionID: sid, Text: q}, func(r json.RawMessage) []proto.Command {
		var res proto.SideAskResult
		if err := json.Unmarshal(r, &res); err != nil {
			if c.SideSession == sid && c.SideQuestion == q {
				c.SidePending = false
				c.SideErr = err.Error()
			}
			return nil
		}
		if c.SideSession == sid && c.SideQuestion == q {
			c.SidePending = false
			c.SideAnswer = res.Answer
		}
		return nil
	})
	if err != nil {
		c.SidePending = false
		c.SideErr = err.Error()
		return nil, err
	}
	c.pending[cmds[0].ID].failed = func(e *proto.Error) []proto.Command {
		if c.SideSession == sid && c.SideQuestion == q {
			c.SidePending = false
			c.SideErr = e.Message
		}
		return nil
	}
	return cmds, nil
}

// Rewind returns the current session to a prompt. Dropping the
// conversation reloads the log and keeps the prompt as the draft.
func (c *Client) Rewind(turn int64, action string) ([]proto.Command, error) {
	id := sessionID(c.Current)
	return c.mutation(proto.MethodRewind, proto.Rewind{SessionID: id, TurnSeq: turn, Action: action}, func(r json.RawMessage) []proto.Command {
		var out proto.RewindResult
		if json.Unmarshal(r, &out) != nil {
			return nil
		}
		if len(out.Conflicts) > 0 {
			c.notice("rewind", "%s", locales.RewindConflict(strings.Join(out.Conflicts, locales.ListSeparator())))
		}
		if out.Action == "code" || len(out.Conflicts) > 0 {
			return nil
		}
		if out.Action != "summarize" && out.Action != "summarize_before" {
			c.Draft = out.Prompt
		}
		return []proto.Command{c.bind(id)}
	})
}

// Retry rewinds the current session to turn (restoring code too when action
// is "both") and immediately re-sends that turn's prompt.
func (c *Client) Retry(turn int64, action string) ([]proto.Command, error) {
	id := sessionID(c.Current)
	return c.mutation(proto.MethodRewind, proto.Rewind{SessionID: id, TurnSeq: turn, Action: action}, func(r json.RawMessage) []proto.Command {
		var out proto.RewindResult
		if json.Unmarshal(r, &out) != nil {
			return nil
		}
		if len(out.Conflicts) > 0 {
			c.notice("rewind", "%s", locales.RewindConflict(strings.Join(out.Conflicts, locales.ListSeparator())))
			return nil
		}
		cmds := []proto.Command{c.bind(id)}
		if prompt := out.Prompt; strings.TrimSpace(prompt) != "" {
			c.afterSync = func() []proto.Command {
				next, _ := c.Prompt(prompt)
				return next
			}
		}
		return cmds
	})
}

// Compact has the kernel summarize the session's history now.
func (c *Client) Compact() ([]proto.Command, error) {
	p := proto.Prompt{SessionID: sessionID(c.Current), Text: "/compact", Compact: true}
	return c.mutation(proto.MethodPrompt, p, nil)
}

// Shell runs a command line of the user's in the current session.
func (c *Client) Shell(command string) ([]proto.Command, error) {
	if strings.TrimSpace(command) == "" {
		return nil, errors.New(locales.NothingToSend())
	}
	return c.mutation(proto.MethodShell, proto.ShellRun{SessionID: sessionID(c.Current), Command: command}, nil)
}

// FindFiles asks for the current session's files that query names; the
// answer lands in Files, unless another query was asked for meanwhile.
func (c *Client) FindFiles(query string) []proto.Command {
	if c.Phase != Ready || c.Current == nil {
		return nil
	}
	c.FilesFor = query
	return []proto.Command{c.read(proto.MethodFilesFind, proto.FilesQuery{Cwd: c.Current.Cwd, Query: query, Limit: 8}, func(r json.RawMessage) []proto.Command {
		if c.FilesFor == query {
			c.Files = nil
			json.Unmarshal(r, &c.Files)
		}
		return nil
	})}
}

// SetDraft keeps what the user is writing to a session.
// It needs no session open: it is sent as the user leaves one.
func (c *Client) SetDraft(sessionID, text string) ([]proto.Command, error) {
	if c.Phase != Syncing && c.Phase != Ready {
		return nil, ErrNotReady
	}
	return []proto.Command{c.mutate(proto.MethodDraftSet, proto.DraftSet{SessionID: sessionID, Text: text}, nil)}, nil
}

// Unqueue takes back a waiting prompt; SendNow makes it join the running
// turn.
func (c *Client) Unqueue(id string) ([]proto.Command, error) { return c.editQueue(id, "remove") }
func (c *Client) SendNow(id string) ([]proto.Command, error) { return c.editQueue(id, "steer") }

func (c *Client) editQueue(id, action string) ([]proto.Command, error) {
	return c.mutation(proto.MethodQueueEdit, proto.QueueEdit{SessionID: sessionID(c.Current), ID: id, Action: action}, nil)
}

// Cancel stops the current turn.
func (c *Client) Cancel() ([]proto.Command, error) {
	if !c.Busy {
		return nil, errors.New(locales.NothingRunning())
	}
	return c.mutation(proto.MethodCancel, proto.SessionRef{SessionID: sessionID(c.Current)}, nil)
}

// Answer allows or denies an approval request; always allows the tool for
// the rest of the session.
func (c *Client) Answer(id string, allow, always bool) ([]proto.Command, error) {
	return c.answer(proto.ApprovalAnswer{ApprovalID: id, Allow: allow, Always: always})
}

// Respond answers an approval in full: declined with words, kept as a
// rule, or with the command changed.
func (c *Client) Respond(ans proto.ApprovalAnswer) ([]proto.Command, error) { return c.answer(ans) }

// Reply answers the model's question; an empty text skips it.
func (c *Client) Reply(id, text string) ([]proto.Command, error) {
	text = strings.TrimSpace(text)
	return c.answer(proto.ApprovalAnswer{ApprovalID: id, Allow: text != "", Text: text})
}

func (c *Client) answer(ans proto.ApprovalAnswer) ([]proto.Command, error) {
	id := ans.ApprovalID
	if c.Answering[id] {
		return nil, errors.New(locales.AlreadyAnswered())
	}
	ans.SessionID = sessionID(c.Current)
	cmds, err := c.mutation(proto.MethodApprovalRespond, ans, nil)
	if err == nil {
		c.Answering[id] = true
		c.pending[cmds[0].ID].failed = func(e *proto.Error) []proto.Command {
			delete(c.Answering, id)
			c.notice("fail", "%s", locales.ApprovalFailed(e.Message))
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

// NewSessionIn makes dir the workspace, creates a session there and
// shows it.
func (c *Client) NewSessionIn(dir string) ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	c.Cwd = dir
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

// Fork branches off a copy of a session and opens it.
func (c *Client) Fork(id string) ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	if id == "" && c.Current != nil {
		id = c.Current.ID
	}
	if id == "" {
		return nil, ErrNotReady
	}
	return []proto.Command{c.mutate(proto.MethodSessionFork, proto.SessionRef{SessionID: id}, func(r json.RawMessage) []proto.Command {
		var s proto.Session
		if json.Unmarshal(r, &s) != nil || s.ID == "" {
			return nil
		}
		c.upsertSession(s)
		return []proto.Command{c.bind(s.ID)}
	})}, nil
}

// Delete deletes a session; if it is shown, another is.
func (c *Client) Delete(id string) ([]proto.Command, error) {
	return c.mutation(proto.MethodSessionDelete, proto.SessionRef{SessionID: id}, func(json.RawMessage) []proto.Command {
		c.Sessions = slices.DeleteFunc(c.Sessions, func(s proto.Session) bool { return s.ID == id })
		if c.want != "" && c.want != id {
			return nil // another session is on its way
		}
		if c.Current != nil && c.Current.ID == id {
			c.want = ""
			c.Phase = Syncing
			c.resetTranscript()
			return []proto.Command{c.listSessions(true, false)}
		}
		return nil
	})
}

// Update changes the current session: title, account, model, effort or
// approval mode. The kernel's session.updated event brings the result.
func (c *Client) Update(u proto.SessionUpdate) ([]proto.Command, error) {
	if u.Mode != nil && !slices.Contains([]string{"ask", "auto", "full"}, *u.Mode) {
		return nil, errors.New(locales.UnknownMode(strconv.Quote(*u.Mode)))
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

// QueuedLabel is how a queued prompt shows: its text, or what it holds.
func QueuedLabel(q proto.Queued) string { return queuedLabel(q.Text, q.Images) }

// queuedLabel is how a queued prompt shows: its text, or what it holds
// when it has none.
func queuedLabel(text string, images int) string {
	if text == "" && images > 0 {
		return locales.ImagesPlaceholder(images)
	}
	return text
}
