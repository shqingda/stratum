package client

import (
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/locales"
	"github.com/shqingda/stratum/proto"
)

// apply applies one live event of the current session while in step.
func (c *Client) apply(e proto.Event) []proto.Command {
	if e.Seq != 0 {
		if e.Seq <= c.cursor {
			return nil // already applied
		}
		if e.Seq != c.cursor+1 {
			return []proto.Command{c.catchUp()}
		}
	}
	return c.event(e)
}

// event applies one event of the current session, from the log or live.
func (c *Client) event(e proto.Event) []proto.Command {
	if e.Seq != 0 {
		c.cursor = e.Seq
		c.log = append(c.log, e)
	}
	c.changesEvent(e)
	switch e.Type {
	case proto.EventTurnStart:
		c.Busy = true
		c.Suggestion = ""
		c.Turns = append(c.Turns, Turn{Seq: e.Seq, Start: e.At})
	case proto.EventThinkingDelta:
		if c.thinking == nil {
			c.endStream()
			c.thinking = c.add(&Entry{Role: Thinking, at: e.At})
			c.thought = c.thinking
			c.thinkBuf.Reset()
		}
		appendDeltaText(&c.thinkBuf, e.Data)
		c.thinking.Text = c.thinkBuf.String()
		c.thinking.Took = e.At.Sub(c.thinking.at)
	case proto.EventUsage:
		var u llm.Usage
		if json.Unmarshal(e.Data, &u) == nil {
			c.LastInput = u.Input
		}
	case proto.EventTextDelta:
		c.endThinking()
		if c.streaming == nil {
			c.streaming = c.add(&Entry{Role: Assistant})
			c.streamBuf.Reset()
		}
		appendDeltaText(&c.streamBuf, e.Data)
		c.streaming.Text = c.streamBuf.String()
	case proto.EventMessage:
		var m llm.Message
		if json.Unmarshal(e.Data, &m) == nil {
			c.message(e.Seq, m)
		}
	case proto.EventToolStart, proto.EventToolEnd:
		var t proto.ToolEvent
		if json.Unmarshal(e.Data, &t) == nil {
			c.endStream()
			en := c.tool(t.Call)
			if t.Result != nil {
				c.toolResult(*t.Result)
			} else {
				en.Done = false
			}
		}
	case proto.EventTaskStep:
		var st proto.TaskStep
		if json.Unmarshal(e.Data, &st) == nil {
			if i := slices.IndexFunc(c.Entries, func(x *Entry) bool { return x.CallID == st.CallID }); i >= 0 {
				var args map[string]any
				json.Unmarshal(st.Call.Args, &args)
				step := st.Call.Name + "\t" + summarize(args)
				if t := c.Entries[i].task(st.Task); t != nil {
					t.Steps = append(t.Steps, step)
				} else {
					c.Entries[i].Steps = append(c.Entries[i].Steps, step)
				}
			}
		}
	case proto.EventTaskState:
		var st proto.TaskState
		if json.Unmarshal(e.Data, &st) == nil {
			if i := slices.IndexFunc(c.Entries, func(x *Entry) bool { return x.CallID == st.CallID }); i >= 0 {
				if t := c.Entries[i].task(st.Task); t != nil {
					t.State = st.State
				}
			}
		}
	case proto.EventApprovalRequest:
		var a proto.Approval
		if json.Unmarshal(e.Data, &a) == nil && !slices.ContainsFunc(c.Approvals, func(x proto.Approval) bool { return x.ID == a.ID }) {
			c.Approvals = append(c.Approvals, a)
		}
	case proto.EventApprovalResolved:
		var a proto.ApprovalAnswer
		if json.Unmarshal(e.Data, &a) == nil {
			if a.Allow && a.Command != "" {
				c.changed(a)
			}
			c.Approvals = slices.DeleteFunc(c.Approvals, func(x proto.Approval) bool { return x.ID == a.ApprovalID })
			delete(c.Answering, a.ApprovalID)
		}
	case proto.EventCompaction:
		var cp proto.Compaction
		if json.Unmarshal(e.Data, &cp) == nil {
			c.endStream()
			text := locales.Compacted()
			if cp.UpToSeq > 0 {
				text = locales.CompactedUpTo()
			} else if cp.Partial {
				text = locales.CompactedFromHere()
			} else if cp.Asked {
				text = locales.CompactedAsked()
			}
			c.add(&Entry{Role: Notice, Note: "compact", Text: text, Output: cp.Summary, CallID: "compaction:" + strconv.FormatInt(e.Seq, 10), Done: true})
		}
	case proto.EventFallback:
		var f proto.Fallback
		if json.Unmarshal(e.Data, &f) == nil {
			c.endStream()
			c.notice("fallback", "%s", locales.FellBack(c.modelName(f.From), c.modelName(f.To)))
		}
	case proto.EventShellStart:
		var sh proto.ShellRun
		if json.Unmarshal(e.Data, &sh) == nil {
			c.endStream()
			c.Busy = true
			c.typed(sh.Command)
		}
	case proto.EventShell:
		var sh proto.Shell
		if json.Unmarshal(e.Data, &sh) == nil {
			c.Busy = false
			en := c.typedRunning(sh.Command)
			if en == nil {
				en = c.typed(sh.Command)
			}
			en.Seq, en.Output, en.Done, en.Failed = e.Seq, sh.Output, true, sh.Stopped
		}
	case proto.EventQueue:
		var q proto.Queue
		if json.Unmarshal(e.Data, &q) == nil {
			c.Queued = q.Items
		}
	case proto.EventRetry:
		var r proto.Retry
		_ = json.Unmarshal(e.Data, &r)
		c.notice("retry", "%s", locales.Retrying(r.Attempt, r.Error))
	case proto.EventTurnEnd:
		var t proto.TurnEnd
		_ = json.Unmarshal(e.Data, &t)
		c.endStream()
		c.Busy, c.Approvals = false, nil // the queue says itself what became of it
		c.thought = nil
		c.Workspace.BranchStale = true // the turn may have switched it
		clear(c.Answering)
		c.Usage = c.Usage.Add(t.Usage)
		if n := len(c.Turns); n > 0 && !c.Turns[n-1].Done {
			c.Turns[n-1].End, c.Turns[n-1].Done, c.Turns[n-1].Usage = e.At, true, t.Usage
		}
		switch {
		case t.Stopped:
			c.notice("stop", "%s", locales.Stopped())
		case t.Limit:
			c.notice("limit", "%s", locales.StepLimit())
		case t.Error != "":
			c.notice("fail", "%s", locales.TurnFailed(t.Error))
		}
	case proto.EventSuggestion:
		var s proto.Suggestion
		if !c.Busy && json.Unmarshal(e.Data, &s) == nil {
			c.Suggestion = strings.TrimSpace(s.Text)
		}
	}
	return nil
}

// mentionAttach is the block expandMentions appends for the model.
// The bubble keeps the user's words; the block stays in the log.
var mentionAttach = regexp.MustCompile(`(?s)\n\n<(?:session|mcp-resource|mcp-prompt)\b[^>]*>.*?</(?:session|mcp-resource|mcp-prompt)>`)

func visiblePrompt(text string) string {
	return strings.TrimRight(mentionAttach.ReplaceAllString(text, ""), "\n")
}

// message adds a stored message: the user's prompt, the assistant's text
// and tool calls, or tool results (which travel as a user message).
func (c *Client) message(seq int64, m llm.Message) {
	var text strings.Builder
	var images []llm.Image
	for _, p := range m.Parts {
		text.WriteString(p.Text)
		if p.Image != nil {
			images = append(images, *p.Image)
		}
	}
	switch m.Role {
	case llm.User:
		shown := visiblePrompt(text.String())
		if shown != "" || len(images) > 0 {
			c.endStream()
			c.add(&Entry{Seq: seq, Role: User, Text: shown, Images: images, Done: true})
		}
	case llm.Assistant:
		var thought strings.Builder
		for _, p := range m.Parts {
			if p.Thinking != nil {
				thought.WriteString(p.Thinking.Text)
			}
		}
		c.endThinking()
		switch {
		case c.thought != nil:
			if thought.Len() > 0 {
				c.thought.Text = thought.String()
			}
			c.thought = nil
		case thought.Len() > 0:
			c.add(&Entry{Seq: seq, Role: Thinking, Text: thought.String(), Done: true})
		}
		switch {
		case c.streaming != nil:
			c.streaming.Text, c.streaming.Done, c.streaming.Seq = text.String(), true, seq
			c.streaming = nil
		case text.Len() > 0:
			c.add(&Entry{Seq: seq, Role: Assistant, Text: text.String(), Done: true})
		}
	}
	for _, p := range m.Parts {
		switch {
		case p.ToolCall != nil:
			c.tool(*p.ToolCall)
		case p.ToolResult != nil:
			c.toolResult(*p.ToolResult)
		}
	}
}

// tool returns the row of a call, adding it the first time.
func (c *Client) tool(call llm.ToolCall) *Entry {
	for i := len(c.Entries) - 1; i >= 0; i-- {
		if en := c.Entries[i]; en.Role == Tool && en.CallID == call.ID {
			return en
		}
	}
	var args map[string]any
	_ = json.Unmarshal(call.Args, &args)
	en := &Entry{Role: Tool, Text: call.Name, Detail: summarize(args), CallID: call.ID, Args: call.Args, Tasks: graphTasks(call)}
	if len(en.Tasks) > 0 {
		en.Detail = locales.TaskCount(len(en.Tasks))
	}
	return c.add(en)
}

func (c *Client) toolResult(r llm.ToolResult) {
	for i := len(c.Entries) - 1; i >= 0; i-- {
		if en := c.Entries[i]; en.Role == Tool && en.CallID == r.CallID {
			en.Done, en.Failed, en.Skipped, en.Output = true, r.IsError && !r.Skipped, r.Skipped, r.Content
			return
		}
	}
}

// summarize shows a tool call's most telling argument.
func summarize(args map[string]any) string {
	for _, k := range []string{"path", "command", "pattern", "query", "url", "title", "question", "description"} {
		if s, ok := args[k].(string); ok && s != "" {
			if k == "command" {
				s = unwrap(s)
			}
			if len(s) > 120 {
				s = s[:117] + "…"
			}
			return s
		}
	}
	return ""
}

// wrapped is a command handed to a shell of its own, as models write
// them: bash -lc '…', and a cd into the workspace before it.
var wrapped = regexp.MustCompile(`^(?:/bin/)?(?:ba|z)?sh\s+-l?c\s+(?:'([^']*)'|"((?:[^"\\]|\\.)*)")$`)
var cdFirst = regexp.MustCompile(`^cd\s+(?:'[^']*'|"[^"]*"|\S+)\s*&&\s*`)

// unwrap is the command a line runs, without the shell around it.
func unwrap(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if m := wrapped.FindStringSubmatch(cmd); m != nil {
		cmd = m[1] + m[2]
	}
	return cdFirst.ReplaceAllString(strings.TrimSpace(cmd), "")
}

// endThinking ends the streaming thinking entry: it is done when text or
// a tool call comes.
func (c *Client) endThinking() {
	if c.thinking != nil {
		c.thinking.Done = true
		c.thinking = nil
	}
}

func (c *Client) endStream() {
	c.endThinking()
	if c.streaming != nil {
		c.streaming.Done = true
		c.streaming = nil
	}
}

// typed adds a command the user runs as a shell step of no turn.
func (c *Client) typed(command string) *Entry {
	args, _ := json.Marshal(map[string]string{"command": command})
	e := c.add(&Entry{Role: Tool, Text: "shell", Detail: command, Args: args, Typed: true})
	e.Turn = -1
	return e
}

// typedRunning is the command of the user's that runs, if it is command.
func (c *Client) typedRunning(command string) *Entry {
	if n := len(c.Entries); n > 0 {
		if e := c.Entries[n-1]; e.Typed && !e.Done && e.Detail == command {
			return e
		}
	}
	return nil
}

// changed shows the command the user changed a call to, on its step.
func (c *Client) changed(a proto.ApprovalAnswer) {
	i := slices.IndexFunc(c.Approvals, func(x proto.Approval) bool { return x.ID == a.ApprovalID })
	if i < 0 {
		return
	}
	ap := c.Approvals[i]
	for j := len(c.Entries) - 1; j >= 0; j-- {
		if e := c.Entries[j]; e.Role == Tool && !e.Done && e.Text == ap.Tool && string(e.Args) == string(ap.Args) {
			var args map[string]any
			if json.Unmarshal(e.Args, &args) == nil {
				args["command"] = a.Command
				e.Args, _ = json.Marshal(args)
				e.Detail = summarize(args)
			}
			return
		}
	}
}

// appendDeltaText extracts the "text" field of an encoded proto.Delta without
// calling json.Unmarshal when the payload is a plain unescaped {"text":"..."}.
func appendDeltaText(dst *strings.Builder, data []byte) {
	const prefix = `{"text":"`
	if len(data) >= len(prefix)+2 && string(data[:len(prefix)]) == prefix && data[len(data)-2] == '"' && data[len(data)-1] == '}' {
		payload := data[len(prefix) : len(data)-2]
		plain := true
		for _, b := range payload {
			if b == '\\' || b == '"' || b < 0x20 {
				plain = false
				break
			}
		}
		if plain {
			dst.Write(payload)
			return
		}
	}
	var d proto.Delta
	_ = json.Unmarshal(data, &d)
	dst.WriteString(d.Text)
}
