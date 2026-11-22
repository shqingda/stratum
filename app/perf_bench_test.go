package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/client"
	"github.com/shqingda/stratum/proto"
)

// The frame-cost benchmarks: each builds a large fixture once, then
// measures what one frame of it costs, built and painted, as the window
// does at the display's rate while anything animates.

func benchFrames(b *testing.B, a *App) {
	b.Helper()
	tt := ui.NewTester(a.View, 1440, 880)
	tt.Frame()
	tt.Advance(2 * time.Second)
	tt.Frame()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		tt.Frame()
	}
}

// longText is n lines of Go-ish source, the kth line differing by tag.
func longText(n int, tag string) string {
	var s strings.Builder
	for i := range n {
		if i%10 == 3 {
			fmt.Fprintf(&s, "\treturn compute%s(a, b, %d) // changed\n", tag, i)
		} else {
			fmt.Fprintf(&s, "\tv%d := strings.Repeat(\"x\", %d) + fmt.Sprint(i)\n", i, i%40)
		}
	}
	return s.String()
}

// BenchmarkFrameTranscript is a session of 2000 entries.
func BenchmarkFrameTranscript(b *testing.B) {
	a := working()
	c := a.C
	c.Busy, c.Approvals, c.Queued = false, nil, nil
	c.Entries = nil
	for i := range 2000 {
		k := i + 1
		switch i % 4 {
		case 0:
			c.Entries = append(c.Entries, &client.Entry{Key: k, Role: client.User, Text: fmt.Sprintf("第 %d 个问题：Add 的测试为什么挂了？", i), Done: true})
		case 1:
			c.Entries = append(c.Entries, &client.Entry{Key: k, Role: client.Tool, Text: "shell", Detail: "go test ./...", CallID: fmt.Sprint("s", i), Done: true,
				Output: longText(40, "out")})
		case 2:
			c.Entries = append(c.Entries, &client.Entry{Key: k, Role: client.Tool, Text: "read_file", Detail: "add.go", CallID: fmt.Sprint("r", i), Done: true})
		default:
			c.Entries = append(c.Entries, &client.Entry{Key: k, Role: client.Assistant, Text: "`Add` 把 `a + b` 写成了 `a - b`。\n\n- 第一点\n- 第二点\n\n```go\nfunc Add(a, b int) int { return a + b }\n```\n", Done: true})
		}
	}
	benchFrames(b, a)
}

// BenchmarkFrameReviewDiff is the Review panel on a 2000-line file.
func BenchmarkFrameReviewDiff(b *testing.B) {
	a := screen1()
	c := a.C
	c.Busy, c.Approvals, c.Queued = false, nil, nil
	key := client.DiffKey(3, "big.go")
	c.Changes.Turns = []proto.ChangeTurn{{TurnSeq: 3, Files: []proto.ChangeFile{{Path: "big.go", Added: 200, Removed: 200}}}}
	before, after := longText(2000, "Old"), longText(2000, "New")
	c.Changes.Diffs = map[string]proto.ChangeDiff{key: {Path: "big.go", Before: &before, After: &after}}
	a.rv.open, a.rv.sel = true, key
	benchFrames(b, a)
}

// BenchmarkFrameSidebar is 200 sessions in 10 folders.
func BenchmarkFrameSidebar(b *testing.B) {
	a := working()
	c := a.C
	c.Busy, c.Approvals, c.Queued = false, nil, nil
	first := c.Sessions[0]
	c.Sessions = []proto.Session{first}
	for i := range 199 {
		c.Sessions = append(c.Sessions, proto.Session{ID: fmt.Sprint("s", i), Title: fmt.Sprintf("会话 %d：收紧沙箱", i),
			Cwd: fmt.Sprintf("/Users/me/Projects/p%d", i%10), Updated: now.Add(-time.Duration(i) * time.Hour)})
	}
	c.Current = &c.Sessions[0]
	benchFrames(b, a)
}

// BenchmarkFrameApproval is an edit of 1000 lines waiting for an answer.
func BenchmarkFrameApproval(b *testing.B) {
	a := working()
	c := a.C
	args, _ := json.Marshal(map[string]string{"path": "big.go", "old_text": longText(1000, "Old"), "new_text": longText(1000, "New")})
	c.Approvals = []proto.Approval{{ID: "big", Tool: "edit_file", Kind: "write", Paths: []string{"big.go"}, Args: args}}
	c.Queued = nil
	benchFrames(b, a)
}

// TestApprovalWaitsStill checks that a turn waiting on an approval asks
// for no more frames: the window is not redrawn while nothing happens.
func TestApprovalWaitsStill(t *testing.T) {
	a := working()
	a.C.Queued = nil
	tt := ui.NewTester(a.View, 1440, 880)
	tt.Frame()
	tt.Advance(3 * time.Second)
	tt.Frame()
	if tt.FramePending() {
		t.Fatal("a turn waiting on an approval keeps asking for frames")
	}
}
