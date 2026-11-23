package app

import (
	"fmt"
	"image"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/client"
	"github.com/shqingda/stratum/locales"
)

// longSession is a conversation taller than the window, ending in an
// answer with a table and code, as the demo's.
func longSession() *App {
	a := working()
	a.C.Busy, a.C.Approvals, a.C.Queued = false, nil, nil
	var es []*client.Entry
	for i := 0; i < 20; i++ {
		es = append(es,
			&client.Entry{Key: 2*i + 1, Role: client.User, Text: fmt.Sprintf("问题 %d：%s", i, strings.Repeat("这一段话要换几行才放得下。", i%4+1)), Done: true},
			&client.Entry{Key: 2*i + 2, Role: client.Assistant, Text: fmt.Sprintf("回答 %d：%s", i, strings.Repeat("回答里的文字在列宽里折几行。", i%6+2)), Done: true})
	}
	es = append(es,
		&client.Entry{Key: 98, Role: client.User, Text: "讲讲这个仓库", Done: true},
		&client.Entry{Key: 99, Role: client.Assistant, Done: true, Text: "| 文件 | 内容 |\n|---|---|\n| add.go | Add 函数 |\n| go.mod | 模块 calc |\n\n### Add\n\n```go\nfunc Add(a, b int) int {\n    return a - b\n}\n```\n\n应该是 `a + b`。\n\n> 表驱动测试加一组输入只要加一行。"})
	a.C.Entries = es
	return a
}

// darkRows sums how dark each line of the window's middle is.
func darkRows(img *image.RGBA, w float32) []float64 {
	sc := float32(img.Bounds().Dx()) / w
	x0, x1 := int(300*sc), int((w-50)*sc)
	y0, y1 := int(80*sc), int(480*sc)
	out := make([]float64, y1-y0)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			out[y-y0] += 255 - float64(img.RGBAAt(x, y).G)
		}
	}
	return out
}

// vshift is how far b is a moved up or down, in pixels.
func vshift(a, b []float64) int {
	const r = 200
	best, least := 0, math.Inf(1)
	for d := -r; d <= r; d++ {
		e := 0.0
		for i := r; i < len(a)-r; i++ {
			v := a[i] - b[i+d]
			e += v * v
		}
		if e < least {
			best, least = d, e
		}
	}
	return best
}

// The transcript, at its end, holds still as the sidebar slides in and
// out: the frames are those of the window, one each refresh, with none
// run in between to settle. A sheet rebuilt at the slide's end measured
// the composer as nothing for a frame, and the list's end jumped.
func TestTranscriptHoldsStillAsTheSidebarSlides(t *testing.T) {
	a := longSession()
	tt := ui.NewTester(a.View, 1200, 772)
	tt.SetScale(2)
	tt.SetVibrancy(true)
	tt.Advance(time.Second)
	prev := darkRows(tt.Image(), 1200)
	for _, way := range []string{"in", "out"} {
		a.toggleSide()
		for i := 0; i < 90; i++ {
			tt.Advance(8 * time.Millisecond)
			cur := darkRows(tt.Image(), 1200)
			if d := vshift(prev, cur); d != 0 {
				t.Fatalf("sliding %s, at %.3f open the transcript moved %d px", way, a.sideOpen, d)
			}
			prev = cur
		}
	}
}

// Opening a session shows its end at once: no jump as the composer
// comes, and no pill to the end for a frame.
func TestOpeningASessionHoldsStill(t *testing.T) {
	a := longSession()
	es := a.C.Entries
	a.C.Current, a.C.Entries = &a.C.Sessions[1], nil
	var ctx *ui.Context
	tt := ui.NewTester(func(c *ui.Context) { ctx = c; a.View(c) }, 1200, 772)
	tt.SetScale(2)
	tt.Advance(time.Second)
	a.C.Current, a.C.Phase = &a.C.Sessions[0], client.Syncing
	ctx.Invalidate()
	tt.Advance(40 * time.Millisecond)
	a.C.Entries, a.C.Phase = es, client.Ready
	ctx.Invalidate()
	var prev []float64
	for i := 0; i < 30; i++ {
		tt.Advance(8 * time.Millisecond)
		if tt.HasText(locales.ScrollToBottom()) {
			t.Fatalf("frame %d shows the pill to the end", i)
		}
		cur := darkRows(tt.Image(), 1200)
		if prev != nil {
			if d := vshift(prev, cur); d != 0 {
				t.Fatalf("frame %d moved the transcript %d px", i, d)
			}
		}
		prev = cur
	}
}
