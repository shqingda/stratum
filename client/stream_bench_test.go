package client

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shqingda/talos/proto"
)

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
