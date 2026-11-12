package app

import (
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/client"
	"github.com/shqingda/talos/proto"
)

var now = time.Date(2026, 11, 12, 15, 0, 0, 0, time.Local)

// working is a session at work: a prompt, steps, prose and an approval.
func working() *App {
	c := client.New("/Users/me/Projects/calc")
	c.Phase = client.Ready
	c.Sessions = []proto.Session{
		{ID: "1", Title: "修复 Add 测试", Cwd: "/Users/me/Projects/calc", Model: "gpt-6-luna", Effort: "medium", Mode: "ask", Updated: now.Add(-2 * time.Minute)},
		{ID: "2", Title: "沙箱 profile 收紧到只读 .git", Cwd: "/Users/me/Projects/talos", Updated: now.Add(-40 * time.Minute)},
		{ID: "3", Title: "client 按日志重建", Cwd: "/Users/me/Projects/talos", Updated: now.Add(-3 * time.Hour)},
		{ID: "4", Title: "协议定稿：回执与快照", Cwd: "/Users/me/Projects/talos", Updated: now.Add(-26 * time.Hour)},
		{ID: "5", Title: "首页文案改写", Cwd: "/Users/me/Projects/site", Updated: now.Add(-80 * time.Hour)},
	}
	c.Current = &c.Sessions[0]
	c.Accounts = []proto.Account{{ID: "chatgpt", Name: "ChatGPT Pro", Email: "还剩 74% · 4 时后重置", Subscription: true}}
	c.Busy = true
	c.Entries = []*client.Entry{
		{Key: 1, Role: client.User, Text: "Add 的测试挂了，帮我修一下；顺便看看 README 里的示例对不对", Done: true},
		{Key: 2, Role: client.Tool, Text: "read_file", Detail: "add.go", CallID: "a", Done: true, Output: "package calc\n\nfunc Add(a, b int) int { return a - b }"},
		{Key: 3, Role: client.Tool, Text: "read_file", Detail: "add_test.go", CallID: "b", Done: true},
		{Key: 4, Role: client.Tool, Text: "search", Detail: "Add(", CallID: "c", Done: true},
		{Key: 5, Role: client.Assistant, Text: "`Add` 把 `a + b` 写成了 `a - b`，测试期望 `Add(2, 3) == 5`。README 里的示例输出也跟着写错了。", Done: true},
		{Key: 6, Role: client.Tool, Text: "edit_file", Detail: "add.go", CallID: "d", Done: true},
		{Key: 7, Role: client.Tool, Text: "shell", Detail: "go test ./... -run TestAdd -v", CallID: "e"},
	}
	args, _ := json.Marshal(map[string]string{"command": "go test ./... -run TestAdd -v"})
	c.Approvals = []proto.Approval{{ID: "x", Tool: "shell", Kind: "exec", Command: "go test ./... -run TestAdd -v", Args: args, Reason: "沙箱内：无网络，只写工作区"}}
	c.Queued = []string{"测试过了就顺手把 Sub 也检查一遍"}
	a := New(c)
	a.now = func() time.Time { return now }
	return a
}

func TestViews(t *testing.T) {
	for _, tc := range []struct {
		name string
		app  *App
		dark bool
	}{
		{"working-light", working(), false},
		{"working-dark", working(), true},
		{"empty-light", func() *App {
			a := working()
			a.C.Entries, a.C.Approvals, a.C.Queued, a.C.Busy = nil, nil, nil, false
			return a
		}(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tt := ui.NewTester(tc.app.View, 1200, 780)
			tt.SetDark(tc.dark)
			tt.SetTitleBar(ui.TitleBar{Left: 78, Height: 52})
			tt.Frame()
			for _, want := range []string{"新会话", "定时任务", "本机"} {
				if !tt.HasText(want) {
					t.Errorf("no %q in %q", want, tt.Texts())
				}
			}
			snapshot(t, tt, tc.name)
		})
	}
}

// snapshot writes what the tester shows to $TALOS_SNAPSHOTS/name.png, to
// look at.
func snapshot(t *testing.T, tt *ui.Tester, name string) {
	dir := os.Getenv("TALOS_SNAPSHOTS")
	if dir == "" {
		return
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	png.Encode(f, tt.Image())
}
