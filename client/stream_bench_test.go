package client

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shqingda/stratum/proto"
)

func TestAppendDeltaText(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{`{"text":"hello world"}`, "hello world"},
		{`{"text":"一段流式输出的文字 "}`, "一段流式输出的文字 "},
		{`{"text":""}`, ""},
		{`{"text":"line1\nline2\t\"quoted\"\\"}`, "line1\nline2\t\"quoted\"\\"},
		{`{"text":"a\u003cb"}`, "a<b"},
		{` { "text" : "spaced" } `, "spaced"},
		{`{"text":"x","extra":"y"}`, "x"},
	}
	for _, tc := range cases {
		var sb strings.Builder
		appendDeltaText(&sb, []byte(tc.raw))
		if sb.String() != tc.want {
			t.Errorf("appendDeltaText(%s) = %q, want %q", tc.raw, sb.String(), tc.want)
		}
	}
}

// BenchmarkStreamDeltas is a reply streamed as 10,000 deltas, the cost
// of taking each one in.
func BenchmarkStreamDeltas(b *testing.B) {
	data, _ := json.Marshal(proto.Delta{Text: "一段流式输出的文字 "})
	at := time.Unix(0, 0)
	b.ReportAllocs()
	for b.Loop() {
		c := New("/tmp/x")
		c.apply(proto.Event{Type: proto.EventTurnStart, At: at})
		for range 10000 {
			c.apply(proto.Event{Type: proto.EventTextDelta, Data: data, At: at})
		}
	}
}
