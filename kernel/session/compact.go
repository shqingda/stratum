package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/shqingda/stratum/kernel/store"
	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/proto"
)

var errEmptySummary = errors.New("the summary came back empty")

const compactPrompt = "The conversation is about to outgrow the context window. Write a summary that lets you carry on " +
	"the work without it: the user's goals and decisions, what was done and which files changed, what was found, " +
	"what is still open, and anything the user asked to keep in mind. Be specific and brief; call no tools."

// history is what the model is sent of a session: its messages, from the
// last full compaction's summary on (partial compactions keep what came before).
func (m *Manager) history(ctx context.Context, sessionID string) ([]llm.Message, error) {
	events, err := m.Store.Events(ctx, sessionID, 0, 0, store.MessageEvent, proto.EventCompaction, proto.EventShell)
	if err != nil {
		return nil, err
	}
	return eventsToMessages(events)
}

type seqMsg struct {
	seq int64
	msg llm.Message
}

func eventsToMessages(events []store.Event) ([]llm.Message, error) {
	var out []seqMsg
	for _, e := range events {
		if e.Type == proto.EventShell {
			// A command of the user's is something they tell the model.
			var sh proto.Shell
			if json.Unmarshal(e.Data, &sh) == nil {
				out = append(out, seqMsg{seq: e.Seq, msg: llm.Text(llm.User, shellNote(sh))})
			}
			continue
		}
		if e.Type == proto.EventCompaction {
			var c proto.Compaction
			if json.Unmarshal(e.Data, &c) == nil {
				sum := summarized(c.Summary)
				switch {
				case c.UpToSeq > 0:
					var kept []seqMsg
					for _, sm := range out {
						if sm.seq >= c.UpToSeq {
							kept = append(kept, sm)
						}
					}
					var next []seqMsg
					for _, m := range sum {
						next = append(next, seqMsg{seq: c.UpToSeq - 1, msg: m})
					}
					out = append(next, kept...)
				case c.Partial:
					for _, m := range sum {
						out = append(out, seqMsg{seq: e.Seq, msg: m})
					}
				default:
					out = out[:0]
					for _, m := range sum {
						out = append(out, seqMsg{seq: e.Seq, msg: m})
					}
				}
			}
			continue
		}
		var msg llm.Message
		if err := json.Unmarshal(e.Data, &msg); err != nil {
			return nil, err
		}
		out = append(out, seqMsg{seq: e.Seq, msg: msg})
	}
	msgs := make([]llm.Message, len(out))
	for i, sm := range out {
		msgs[i] = sm.msg
	}
	return msgs, nil
}

func summarized(summary string) []llm.Message {
	return []llm.Message{
		llm.Text(llm.User, "Summary of the conversation so far, written by you:\n\n"+summary),
		llm.Text(llm.Assistant, "Understood; I will continue from there."),
	}
}

// compactAt is how many tokens of history start a compaction: three
// quarters of the model's context, 0 when it is not known.
func (m *Manager) compactAt(account, model string) int {
	if m.CompactAt > 0 {
		return m.CompactAt
	}
	provider, _, _ := strings.Cut(account, "/")
	if x, ok := llm.FindModel(provider, model); ok {
		return x.Context * 3 / 4
	}
	return 0
}

// seesImages reports whether a model takes images, as the catalog says;
// a model it doesn't know, as the demo's, is taken to.
func seesImages(account, model string) bool {
	provider, _, _ := strings.Cut(account, "/")
	x, ok := llm.FindModel(provider, model)
	return !ok || x.Images
}

// tokens guesses the tokens of messages from their size: about three
// bytes each, between English and CJK text.
func tokens(msgs []llm.Message) int {
	b, _ := json.Marshal(msgs)
	return len(b) / 3
}

// compact has the model summarize history and records the summary; the
// conversation the model is sent then starts from it.
func (m *Manager) compact(ctx context.Context, provider llm.Provider, req llm.Request, sessionID string, asked bool) ([]llm.Message, llm.Usage, error) {
	req.Messages = append(req.Messages, llm.Text(llm.User, compactPrompt))
	reply, err := llm.Collect(provider.Stream(ctx, req), nil)
	if err != nil {
		return nil, reply.Usage, err
	}
	summary := strings.TrimSpace(reply.Message.PlainText())
	if summary == "" {
		return nil, reply.Usage, errEmptySummary
	}
	if err := m.record(ctx, sessionID, proto.EventCompaction, proto.Compaction{Summary: summary, Asked: asked}); err != nil {
		return nil, reply.Usage, err
	}
	return summarized(summary), reply.Usage, nil
}

// shellNote is how the model reads a command the user ran.
func shellNote(sh proto.Shell) string {
	note := "I ran this command myself in the workspace:\n\n$ " + sh.Command + "\n" + sh.Output
	if sh.Stopped {
		note += "\n[stopped before it finished]"
	}
	return note
}
