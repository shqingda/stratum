package app

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/client"
)

func TestPlanPane(t *testing.T) {
	a := screen1()
	a.rv.tab = "plan"
	plan, _ := json.Marshal(map[string]any{"title": "给 Sub 补表驱动测试", "steps": []map[string]string{
		{"step": "读 sub.go 和现有测试", "status": "done"},
		{"step": "写表驱动用例：正数、负数、零、溢出边界", "status": "done"},
		{"step": "跑测试，修掉失败的用例", "status": "in_progress", "note": "负数用例失败：Sub(-2, 3) = 1，期望 -5"},
		{"step": "更新 README 的 Sub 示例", "status": "pending"},
	}})
	a.C.Entries = append(a.C.Entries, &client.Entry{Key: 30, Role: client.Tool, Text: "update_plan", Detail: "给 Sub 补表驱动测试", CallID: "p", Args: plan, Done: true})
	tt := ui.NewTester(a.View, 1440, 880)
	tt.SetScale(2)
	tt.SetDark(true)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	for _, want := range []string{"4 步 · 已完成 2 步", "跑测试，修掉失败的用例", "负数用例失败：Sub(-2, 3) = 1，期望 -5"} {
		if !tt.HasText(want) {
			t.Errorf("the plan lacks %q", want)
		}
	}
	snapshot(t, tt, "pane-plan")
}

func TestFilesPane(t *testing.T) {
	a := screen1()
	a.C.Files = []string{"src/", "notes.md"}
	a.C.FilesFor = ""
	var opened string
	a.openIn = func(_ string, path string) error { opened = path; return nil }
	tt := ui.NewTester(a.View, 1440, 880)
	tt.Frame()
	if err := tt.Click("文件"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("notes.md") || !tt.HasText("src/") {
		t.Fatalf("files pane missing entries: %q", tt.Texts())
	}
	if err := tt.Click("notes.md"); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(a.C.Current.Cwd, "notes.md"); opened != want {
		t.Fatalf("opened %q, want %q", opened, want)
	}
}
