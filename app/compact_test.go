package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/client"
)

func TestCompactionNote(t *testing.T) {
	a := working()
	a.C.Entries = append([]*client.Entry{{Key: 900, Role: client.Notice, Text: "上下文快满了，较早的对话已写成摘要",
		Output: "修好了 Add 的溢出，测试已通过。", CallID: "compaction:4", Done: true}}, a.C.Entries...)
	tt := ui.NewTester(a.View, 1200, 780)
	tt.Frame()
	if tt.HasText("修好了 Add 的溢出") {
		t.Fatal("summary shown before opening")
	}
	if err := tt.Click("上下文快满了，较早的对话已写成摘要"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("修好了 Add 的溢出") {
		t.Fatal("summary not shown")
	}
	snapshot(t, tt, "compaction")
}
