package client

import (
	"encoding/json"
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/shqingda/talos/llm"
	"github.com/shqingda/talos/proto"
)

// tailSize is how many events of a session's log opening it reads; the
// rest come a page at a time with LoadOlder.
const tailSize = 400

// firstTurn is where a page read from the middle of a log starts: at its
// first turn, so that no turn shows without its beginning.
func firstTurn(events []proto.Event) int {
	return max(0, slices.IndexFunc(events, func(e proto.Event) bool { return e.Type == proto.EventTurnStart }))
}

// LoadOlder reads the page of the log before what is shown.
func (c *Client) LoadOlder() ([]proto.Command, error) {
	if !c.Older || c.LoadingOlder || c.Current == nil || c.Phase != Ready {
		return nil, nil
	}
	c.LoadingOlder = true
	session := c.Current.ID
	q := proto.EventsQuery{SessionID: session, Before: c.first, Limit: tailSize}
	return []proto.Command{c.read(proto.MethodEvents, q, func(r json.RawMessage) []proto.Command {
		c.LoadingOlder = false
		var page proto.EventPage
		if c.Current == nil || c.Current.ID != session || json.Unmarshal(r, &page) != nil {
			return nil
		}
		events := page.Events
		if page.More {
			events = events[firstTurn(events):]
		}
		c.Older = page.More
		if len(events) == 0 {
			return nil
		}
		c.first = events[0].Seq
		c.rebuild(append(events, c.log...))
		return nil
	})}, nil
}

// rebuild makes the transcript again from log, as from the start. The
// entries shown before keep their keys, so that the list keeps its place.
func (c *Client) rebuild(log []proto.Event) {
	keys := map[string]int{}
	for id, e := range identities(c.Entries) {
		keys[id] = e.Key
	}
	queued := c.Queued
	c.Entries, c.streaming, c.Turns, c.thinking, c.thought = nil, nil, nil, nil, nil
	c.Busy, c.Approvals, c.Usage, c.LastInput = false, nil, llm.Usage{}, 0
	c.log = nil
	for _, e := range log {
		c.event(e)
	}
	c.Queued = queued
	for id, e := range identities(c.Entries) {
		if k, ok := keys[id]; ok {
			e.Key = k
		}
	}
}

// identities names entries the same way however often the log is read.
func identities(entries []*Entry) map[string]*Entry {
	out := map[string]*Entry{}
	for _, e := range entries {
		id := "call:" + e.CallID
		if e.CallID == "" {
			id = fmt.Sprint(e.Seq, e.Role, e.Text)
		}
		for n := 1; out[id] != nil; n++ {
			id = fmt.Sprint(id, "#", n)
		}
		out[id] = e
	}
	return out
}

// Search finds messages across sessions; only the latest query's answer
// is kept, in Hits.
func (c *Client) Search(query string) []proto.Command {
	if query == c.searching {
		return nil // asked already
	}
	c.searching = query
	if utf8.RuneCountInString(query) < 2 {
		c.Hits, c.HitsFor = nil, query
		return nil
	}
	return []proto.Command{c.read(proto.MethodSearch, proto.SearchQuery{Query: query, Limit: 20}, func(r json.RawMessage) []proto.Command {
		if c.searching != query {
			return nil
		}
		var hits []proto.SearchHit
		json.Unmarshal(r, &hits)
		c.Hits, c.HitsFor = hits, query
		if c.Hits == nil {
			c.Hits = []proto.SearchHit{}
		}
		return nil
	})}
}
