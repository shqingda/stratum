package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/client"
)

func TestTaskRow(t *testing.T) {
	a := working()
	a.C.Approvals = nil
	last := a.C.Entries[len(a.C.Entries)-1]
	a.C.Entries = append(a.C.Entries, &client.Entry{Key: 901, Role: client.Tool, Text: "task", Detail: "找出所有调用 Add 的地方", CallID: "t1", Turn: last.Turn,
		Steps: []string{"list_dir\t.", "search\tAdd(", "read_file\tcalc/add.go", "read_file\tcmd/main.go", "read_file\tREADME.md"}})
	tt := ui.NewTester(a.View, 1200, 780)
	tt.Frame()
	for _, s := range []string{"子任务", "找出所有调用 Add 的地方", "前面还有 2 步", "cmd/main.go"} {
		if !tt.HasText(s) {
			t.Fatalf("%q not shown", s)
		}
	}
	snapshot(t, tt, "task-running")
}

func TestTaskGraphRow(t *testing.T) {
	a := working()
	a.C.Approvals = nil
	last := a.C.Entries[len(a.C.Entries)-1]
	a.C.Entries = append(a.C.Entries, &client.Entry{Key: 902, Role: client.Tool, Text: "run_tasks", Detail: "4 个任务", CallID: "g", Turn: last.Turn,
		Tasks: []*client.GraphTask{
			{ID: "find", Description: "找出调用 Add 的地方", State: "done"},
			{ID: "docs", Description: "核对 README 示例", State: "run", Steps: []string{"read_file\tREADME.md"}},
			{ID: "fix", Description: "改调用方", After: []string{"find"}, State: "run", Steps: []string{"edit_file\tcmd/main.go"}},
			{ID: "test", Description: "跑全部测试", After: []string{"fix", "docs"}},
		}})
	tt := ui.NewTester(a.View, 1200, 780)
	tt.Frame()
	for _, s := range []string{"任务图", "4 个任务", "核对 README 示例", "等 fix、docs", "cmd/main.go"} {
		if !tt.HasText(s) {
			t.Fatalf("%q not shown", s)
		}
	}
	snapshot(t, tt, "task-graph")
}
