package session

import (
	"context"
	"strings"
	"time"
	"unicode"

	"github.com/shqingda/talos/kernel/store"
	"github.com/shqingda/talos/llm"
)

// mentionRunes is how much of a session or MCP resource is attached.
const mentionRunes = 4000

// expandMentions attaches what @ words name, after the user's text.
// @session:<id> is that session's title and dialogue, the latest part
// when it is long. @<server>:<uri> is an MCP resource, the form Claude
// Code uses. A file @path is left for the model to read. The same word
// twice is attached once. The attachment is a snapshot from this send.
func (m *Manager) expandMentions(ctx context.Context, current store.Session, text string) string {
	words := atWords(text)
	if len(words) == 0 {
		return text
	}
	var blocks []string
	seen := map[string]bool{}
	for _, word := range words {
		if seen[word] {
			continue
		}
		seen[word] = true
		if block, ok := m.mentionBlock(ctx, current, word); ok {
			blocks = append(blocks, block)
		}
	}
	if len(blocks) == 0 {
		return text
	}
	return text + "\n\n" + strings.Join(blocks, "\n\n")
}

// atWords are the @ words: an @ that starts a word, through the next space.
func atWords(text string) []string {
	r := []rune(text)
	var out []string
	for i := 0; i < len(r); i++ {
		if r[i] != '@' || i > 0 && !unicode.IsSpace(r[i-1]) {
			continue
		}
		j := i + 1
		for j < len(r) && !unicode.IsSpace(r[j]) {
			j++
		}
		if j > i+1 {
			out = append(out, string(r[i+1:j]))
		}
		i = j
	}
	return out
}

func (m *Manager) mentionBlock(ctx context.Context, current store.Session, word string) (string, bool) {
	if id, ok := strings.CutPrefix(word, "session:"); ok && id != "" && id != current.ID {
		if x, err := m.Store.GetSession(ctx, id); err == nil {
			return m.sessionBlock(ctx, x), true
		}
	}
	server, uri, ok := strings.Cut(word, ":")
	if !ok || server == "" || uri == "" || m.MCP == nil || !m.MCP.HasServer(current.Cwd, server) {
		return "", false
	}
	readCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	body, err := m.MCP.ReadResource(readCtx, current.Cwd, server, uri)
	if err != nil {
		body = err.Error()
	}
	return resourceBlock(server, uri, body), true
}

func (m *Manager) sessionBlock(ctx context.Context, x store.Session) string {
	body := ""
	if msgs, err := m.history(ctx, x.ID); err != nil {
		body = err.Error()
	} else {
		body = tailRunes(dialogue(msgs), mentionRunes)
	}
	body = hideClosers(body)
	title := x.Title
	if title == "" {
		title = x.ID
	}
	return "<session id=\"" + escapeAttr(x.ID) + "\" title=\"" + escapeAttr(title) + "\">\n" + body + "\n</session>"
}

func resourceBlock(server, uri, body string) string {
	body = hideClosers(tailRunes(body, mentionRunes))
	return "<mcp-resource server=\"" + escapeAttr(server) + "\" uri=\"" + escapeAttr(uri) + "\">\n" + body + "\n</mcp-resource>"
}

func hideClosers(body string) string {
	body = strings.ReplaceAll(body, "</session>", "</ session>")
	return strings.ReplaceAll(body, "</mcp-resource>", "</ mcp-resource>")
}

func dialogue(msgs []llm.Message) string {
	var b strings.Builder
	for _, msg := range msgs {
		t := strings.TrimSpace(msg.PlainText())
		if t == "" {
			continue
		}
		role := "user"
		if msg.Role == llm.Assistant {
			role = "assistant"
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(role)
		b.WriteString(": ")
		b.WriteString(t)
	}
	return b.String()
}

// tailRunes keeps the end of s, which is the recent part of a dialogue.
func tailRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	cut := string(r[len(r)-n:])
	if i := strings.IndexByte(cut, '\n'); i >= 0 && i < len(cut)-1 {
		cut = cut[i+1:]
	}
	return "…\n" + cut
}

func escapeAttr(s string) string {
	return strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;", ">", "&gt;", "\n", " ").Replace(s)
}
