package session

import (
	"context"
	"errors"
	"strings"

	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/proto"
)

const sideAskSystemPrompt = "You are answering a quick side question (/btw) about this session. " +
	"Be concise and direct. You have no tools and cannot edit files or run commands."

// SideAsk answers a one-off read-only question against the session's
// conversation snapshot without interrupting an active turn or persisting
// anything to the session store.
func (m *Manager) SideAsk(ctx context.Context, p proto.SideAsk) (proto.SideAskResult, error) {
	q := strings.TrimSpace(p.Text)
	if q == "" {
		return proto.SideAskResult{}, errors.New("side question is empty")
	}
	x, err := m.Store.GetSession(ctx, p.SessionID)
	if err != nil {
		return proto.SideAskResult{}, err
	}
	if x.Account == "" || x.Model == "" {
		return proto.SideAskResult{}, errors.New("the session has no model; pick one")
	}
	provider, err := m.Open(x.Account)
	if err != nil {
		return proto.SideAskResult{}, err
	}
	history, err := m.history(ctx, x.ID)
	if err != nil {
		return proto.SideAskResult{}, err
	}
	msgs := append(history, llm.Text(llm.User, q))
	req := llm.Request{
		Model:     x.Model,
		System:    sideAskSystemPrompt,
		Messages:  msgs,
		Effort:    x.Effort,
		SessionID: x.ID,
	}
	reply, err := llm.Collect(provider.Stream(ctx, req), nil)
	if err != nil {
		return proto.SideAskResult{}, err
	}
	return proto.SideAskResult{Answer: strings.TrimSpace(reply.Message.PlainText())}, nil
}
