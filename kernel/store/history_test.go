package store

import (
	"bytes"
	"context"
	"slices"
	"testing"
)

func TestEventsBefore(t *testing.T) {
	ctx := context.Background()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, id := range []string{"history", "empty", "other"} {
		if err := s.CreateSession(ctx, Session{ID: id, Cwd: "/workspace", Mode: "ask"}); err != nil {
			t.Fatal(err)
		}
	}
	var original []Event
	for i, typ := range []string{"note", "phase", "note", "other", "note", "phase", "note"} {
		e, err := s.Append(ctx, "history", typ, map[string]int{"value": i + 1})
		if err != nil {
			t.Fatal(err)
		}
		original = append(original, e)
		if _, err := s.Append(ctx, "other", typ, "do not include"); err != nil {
			t.Fatal(err)
		}
	}
	for _, tt := range []struct {
		name    string
		session string
		before  int64
		limit   int
		types   []string
		seqs    []int64
	}{
		{"empty-session", "empty", 100, 3, nil, nil},
		{"exact-page", "history", 4, 3, nil, []int64{1, 2, 3}},
		{"before-one", "history", 1, 3, nil, nil},
		{"before-zero", "history", 0, 3, nil, nil},
		{"excludes-boundary", "history", 6, 3, nil, []int64{3, 4, 5}},
		{"nearest-page", "history", 100, 3, nil, []int64{5, 6, 7}},
		{"limit-one", "history", 6, 1, nil, []int64{5}},
		{"zero-limit", "history", 5, 0, nil, []int64{1, 2, 3, 4}},
		{"negative-limit", "history", 5, -2, nil, []int64{1, 2, 3, 4}},
		{"type-filter", "history", 7, 2, []string{"note"}, []int64{3, 5}},
		{"multiple-types", "history", 8, 3, []string{"note", "other"}, []int64{4, 5, 7}},
		{"no-matching-type", "history", 8, 3, []string{"missing"}, nil},
		{"type-is-literal", "history", 8, 3, []string{"note') OR 1=1--"}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			events, err := s.EventsBefore(ctx, tt.session, tt.before, tt.limit, tt.types...)
			if err != nil {
				t.Fatal(err)
			}
			var seqs []int64
			for _, event := range events {
				seqs = append(seqs, event.Seq)
				want := original[event.Seq-1]
				if event.SessionID != tt.session || event.Type != want.Type || !bytes.Equal(event.Data, want.Data) || event.At.UnixMilli() != want.At.UnixMilli() {
					t.Errorf("event fields were changed or crossed sessions: %+v", event)
				}
			}
			if !slices.Equal(seqs, tt.seqs) {
				t.Fatalf("sequences %v, want %v", seqs, tt.seqs)
			}
		})
	}

	t.Run("page-to-start", func(t *testing.T) {
		before := int64(8)
		var collected []int64
		pages := 0
		for {
			events, err := s.EventsBefore(ctx, "history", before, 3)
			if err != nil {
				t.Fatal(err)
			}
			if len(events) == 0 {
				break
			}
			pages++
			if pages > len(original) {
				t.Fatal("pagination did not reach the beginning")
			}
			page := make([]int64, len(events))
			for i, event := range events {
				if event.Seq >= before || i > 0 && event.Seq <= page[i-1] {
					t.Fatalf("page order or exclusive cursor violated: %+v", events)
				}
				page[i] = event.Seq
			}
			collected = append(page, collected...)
			before = events[0].Seq
		}
		if pages != 3 || !slices.Equal(collected, []int64{1, 2, 3, 4, 5, 6, 7}) {
			t.Fatalf("got %d pages and %v", pages, collected)
		}
	})

	t.Run("existing-forward-pagination", func(t *testing.T) {
		for _, tt := range []struct {
			after int64
			limit int
			types []string
			seqs  []int64
		}{
			{2, 2, nil, []int64{3, 4}},
			{2, 2, []string{"note"}, []int64{3, 5}},
			{0, 0, nil, []int64{1, 2, 3, 4, 5, 6, 7}},
			{5, -1, nil, []int64{6, 7}},
			{7, 3, nil, nil},
		} {
			events, err := s.Events(ctx, "history", tt.after, tt.limit, tt.types...)
			if err != nil {
				t.Fatal(err)
			}
			var seqs []int64
			for _, event := range events {
				seqs = append(seqs, event.Seq)
			}
			if !slices.Equal(seqs, tt.seqs) {
				t.Errorf("after %d limit %d types %v: got %v, want %v", tt.after, tt.limit, tt.types, seqs, tt.seqs)
			}
		}
	})
}
