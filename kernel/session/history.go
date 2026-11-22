package session

import (
	"context"

	"github.com/shqingda/stratum/proto"
)

func (m *Manager) EventsBefore(ctx context.Context, q proto.EventsQuery) (proto.EventPage, error) {
	if q.Before <= 0 || q.After != 0 {
		return proto.EventPage{}, proto.Errorf("bad_request", "before must be positive and cannot be combined with after")
	}
	limit := historyLimit(q.Limit)
	events, err := m.Store.EventsBefore(ctx, q.SessionID, q.Before, limit+1)
	if err != nil {
		return proto.EventPage{}, err
	}
	page := proto.EventPage{Events: []proto.Event{}}
	if len(events) > limit {
		events = events[1:]
		page.More = true
	}
	for _, event := range events {
		page.Events = append(page.Events, toProto(event))
	}
	return page, nil
}

func (m *Manager) Search(ctx context.Context, q proto.SearchQuery) ([]proto.SearchHit, error) {
	hits, err := m.Store.Search(ctx, q.Query, q.SessionID, historyLimit(q.Limit))
	if err != nil {
		return nil, err
	}
	out := make([]proto.SearchHit, 0, len(hits))
	for _, hit := range hits {
		out = append(out, proto.SearchHit{SessionID: hit.SessionID, Seq: hit.Seq, Role: hit.Role, Snippet: hit.Snippet, At: hit.At})
	}
	return out, nil
}

func historyLimit(limit int) int {
	if limit <= 0 || limit > 2000 {
		return 500
	}
	return limit
}
