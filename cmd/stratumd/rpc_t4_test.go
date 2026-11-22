package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/proto"
)

func TestRPCSearchAndHistory(t *testing.T) {
	k := start(t, filepath.Join(t.TempDir(), "talos.db"), []llm.Turn{
		{Text: "The orange nebula is ready."},
		{Text: "藍色星球有深藍海洋。"},
		{Text: "The violet comet is visible."},
		{Text: "金色森林住著許多鳥類。"},
	})
	prompts := [][]string{
		{"The amberquartz lantern belongs to this conversation.", "這裡討論藍色星球與海洋。"},
		{"The cobaltmeadow notebook belongs to the second conversation.", "我們正在討論金色森林與鳥類。"},
	}
	sessions := make([]proto.Session, len(prompts))
	history := map[string][]proto.Event{}
	for i, turns := range prompts {
		k.call(proto.MethodSessionCreate, proto.SessionCreate{Cwd: t.TempDir()}, &sessions[i])
		for _, text := range turns {
			k.call(proto.MethodPrompt, proto.Prompt{SessionID: sessions[i].ID, Text: text}, nil)
			awaitHistoryTurn(t, k, sessions[i].ID)
		}
		var page proto.EventPage
		k.call(proto.MethodEvents, proto.EventsQuery{SessionID: sessions[i].ID, Limit: 1000}, &page)
		if page.More || len(page.Events) < 7 {
			t.Fatalf("incomplete history: %+v", page)
		}
		for j, event := range page.Events {
			if event.SessionID != sessions[i].ID || event.Seq != int64(j+1) {
				t.Fatalf("unexpected sequence in history: %+v", event)
			}
		}
		history[sessions[i].ID] = page.Events
	}

	for _, tc := range []struct {
		query, sessionID string
		want             map[string]int
	}{
		{"amberquartz", "", map[string]int{sessions[0].ID: 1}},
		{"cobaltmeadow", "", map[string]int{sessions[1].ID: 1}},
		{"nebula", "", map[string]int{sessions[0].ID: 1}},
		{"藍色星球", "", map[string]int{sessions[0].ID: 2}},
		{"金色森林", "", map[string]int{sessions[1].ID: 2}},
		{"conversation", "", map[string]int{sessions[0].ID: 1, sessions[1].ID: 1}},
		{"conversation", sessions[0].ID, map[string]int{sessions[0].ID: 1}},
		{"amberquartz", sessions[1].ID, map[string]int{}},
		{"amberquartz:", "", map[string]int{}},
	} {
		t.Run("search/"+tc.query+"/"+tc.sessionID, func(t *testing.T) {
			var hits []proto.SearchHit
			k.call(proto.MethodSearch, proto.SearchQuery{Query: tc.query, SessionID: tc.sessionID}, &hits)
			got := map[string]int{}
			seen := map[string]bool{}
			for _, hit := range hits {
				got[hit.SessionID]++
				key := fmt.Sprintf("%s/%d", hit.SessionID, hit.Seq)
				if seen[key] {
					t.Errorf("duplicate search hit %s", key)
				}
				seen[key] = true
				events := history[hit.SessionID]
				if hit.Seq <= 0 || hit.Seq > int64(len(events)) || hit.At.IsZero() || !strings.Contains(hit.Snippet, tc.query) {
					t.Fatalf("incomplete or wrong search hit: %+v", hit)
				}
				event := events[hit.Seq-1]
				var message llm.Message
				if err := json.Unmarshal(event.Data, &message); err != nil {
					t.Fatal(err)
				}
				if event.Type != proto.EventMessage || hit.Role != string(message.Role) || !hit.At.Equal(event.At) || !strings.Contains(message.PlainText(), tc.query) {
					t.Errorf("hit does not identify its source message: %+v, event %+v", hit, event)
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("search sessions/counts = %v, want %v", got, tc.want)
			}
		})
	}
	var limited []proto.SearchHit
	k.call(proto.MethodSearch, proto.SearchQuery{Query: "conversation", Limit: 1}, &limited)
	if len(limited) != 1 || history[limited[0].SessionID] == nil {
		t.Fatalf("search limit not applied: %+v", limited)
	}

	for _, session := range sessions {
		t.Run("history/"+session.ID, func(t *testing.T) {
			all := history[session.ID]
			before := all[len(all)-1].Seq + 1
			var reconstructed []proto.Event
			for end := len(all); end > 0; {
				var page proto.EventPage
				k.call(proto.MethodEvents, proto.EventsQuery{SessionID: session.ID, Before: before, Limit: 3}, &page)
				first := max(0, end-3)
				if !reflect.DeepEqual(page.Events, all[first:end]) || page.More != (first > 0) {
					t.Fatalf("before %d: got %+v, want seq %d..%d, more %v", before, page, all[first].Seq, all[end-1].Seq, first > 0)
				}
				reconstructed = append(page.Events, reconstructed...)
				before, end = page.Events[0].Seq, first
			}
			if before != 1 || !reflect.DeepEqual(reconstructed, all) {
				t.Fatal("backwards history has missing, duplicated or reordered events")
			}
			for _, tc := range []struct {
				query proto.EventsQuery
				want  []proto.Event
				more  bool
			}{
				{proto.EventsQuery{SessionID: session.ID, Before: 1, Limit: 3}, nil, false},
				{proto.EventsQuery{SessionID: session.ID, Before: all[len(all)-1].Seq + 1, Limit: len(all)}, all, false},
				{proto.EventsQuery{SessionID: session.ID, After: 1, Limit: 3}, all[1:4], true},
				{proto.EventsQuery{SessionID: session.ID, After: all[len(all)-1].Seq, Limit: 3}, nil, false},
			} {
				var page proto.EventPage
				k.call(proto.MethodEvents, tc.query, &page)
				if page.More != tc.more || len(page.Events) != len(tc.want) || len(tc.want) > 0 && !reflect.DeepEqual(page.Events, tc.want) {
					t.Errorf("events %+v: got %+v, want %d events, more %v", tc.query, page, len(tc.want), tc.more)
				}
			}
			got := historyRPCError(t, k, proto.MethodEvents, proto.EventsQuery{SessionID: session.ID, After: 1, Before: 2, Limit: 3})
			if got == nil || got.Code != "bad_request" {
				t.Fatalf("both cursors: got %v, want bad_request", got)
			}
		})
	}
}

func awaitHistoryTurn(t *testing.T, k *kernel, sessionID string) {
	t.Helper()
	end := k.await(proto.EventTurnEnd, nil)
	var result proto.TurnEnd
	if err := json.Unmarshal(end.Data, &result); err != nil {
		t.Fatal(err)
	}
	if end.SessionID != sessionID || result.Error != "" || result.Stopped {
		t.Fatalf("unexpected turn end: %+v, %+v", end, result)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var snap proto.Snapshot
		k.call(proto.MethodSessionGet, proto.SessionRef{SessionID: sessionID}, &snap)
		if !snap.Running {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("session is still running after turn.end")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func historyRPCError(t *testing.T, k *kernel, method string, params any) *proto.Error {
	t.Helper()
	k.mu.Lock()
	k.n++
	id := fmt.Sprintf("c%d", k.n)
	ch := make(chan proto.Frame, 1)
	k.waiting[id] = ch
	k.mu.Unlock()
	defer func() {
		k.mu.Lock()
		delete(k.waiting, id)
		k.mu.Unlock()
	}()
	p, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	line, err := json.Marshal(proto.Command{ID: id, Method: method, Params: p})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.in.Write(append(line, '\n')); err != nil {
		t.Fatal(err)
	}
	select {
	case frame := <-ch:
		return frame.Error
	case <-time.After(30 * time.Second):
		t.Fatalf("%s: no response", method)
		return nil
	}
}
