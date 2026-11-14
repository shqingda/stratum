package app

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/client"
	"github.com/shqingda/talos/proto"
)

func TestMain(m *testing.M) {
	// The tests read Chinese, whatever language this machine is in.
	i18n.SetLocaleFunc(chinese)
	local := time.Local
	time.Local = testZone
	code := m.Run()
	time.Local = local
	os.Exit(code)
}

func stateApp() *App {
	a := working()
	a.C.Entries, a.C.Approvals, a.C.Queued, a.C.Busy = nil, nil, nil, false
	a.out = make(chan []proto.Command, 8)
	return a
}

func stateTester(a *App, dark bool, w, h int) *ui.Tester {
	tt := ui.NewTester(a.View, w, h)
	tt.SetDark(dark)
	tt.SetTitleBar(ui.TitleBar{Left: 78, Height: 52})
	tt.Frame()
	tt.Advance(2 * time.Second) // Finish text reveal without using the wall clock.
	tt.Frame()
	return tt
}

func stateTool() *client.Entry {
	return &client.Entry{Key: 1, Role: client.Tool, Text: "shell", Detail: "go test ./...", CallID: "tool-1", Done: true, Output: "ok calc 0.003s"}
}

func stateApproval(id string) proto.Approval {
	return proto.Approval{ID: id, Tool: "shell", Kind: "exec", Command: "go test ./... -run " + id, Reason: "沙箱内：无网络，只写工作区"}
}

// Text input values and placeholders are exposed through accessibility, not Texts.
func stateHasText(tt *ui.Tester, want string) bool {
	if tt.HasText(want) {
		return true
	}
	for _, field := range tt.AccessAll() {
		if field.Role != ui.RoleTextField {
			continue
		}
		if strings.Contains(field.Value, want) || (field.Value == "" && strings.Contains(field.Placeholder, want)) {
			return true
		}
	}
	return false
}

func TestStateMatrix(t *testing.T) {
	type state struct {
		name          string
		setup         func(*App)
		want          []string
		width, height int
		scroll        string
	}
	states := []state{
		{name: "sidebar-empty", setup: func(a *App) { a.C.Sessions, a.C.Current = nil, nil }, want: []string{"新建一个会话开始"}},
		{name: "sidebar-many", setup: func(a *App) {
			a.C.Sessions = nil
			for i := range 60 {
				a.C.Sessions = append(a.C.Sessions, proto.Session{ID: fmt.Sprint(i), Title: fmt.Sprintf("会话 %02d：检查工作区", i+1), Cwd: fmt.Sprintf("/workspace/project-%d", i%8+1), Model: "gpt-6-luna", Effort: "medium", Mode: "ask", Updated: now.Add(-time.Duration(i) * time.Minute)})
			}
			a.C.Current = &a.C.Sessions[0]
		}, want: []string{"project-1", "会话 01：检查工作区"}, scroll: "sidebar"},
		{name: "sidebar-long-title", setup: func(a *App) { a.C.Sessions[0].Title = string([]rune(strings.Repeat("修复 Talos 边界 ", 8))[:80]) }, want: []string{"修复 Talos 边界"}},
		{name: "palette-hit", setup: func(a *App) { a.pal, a.query = true, "CLIENT" }, want: []string{"按日志重建", "talos · 3 时"}},
		{name: "palette-miss", setup: func(a *App) { a.pal, a.query = true, "完全不存在的会话" }, want: []string{"没有匹配的会话、操作或消息"}},
		{name: "sidebar-waiting", setup: func(a *App) { a.C.Approvals = []proto.Approval{stateApproval("waiting")} }, want: []string{"需要你", "审批"}},
		{name: "sidebar-relative-times", setup: func(a *App) {
			a.C.Sessions = nil
			for i, d := range []time.Duration{0, 2 * time.Minute, 3 * time.Hour, 26 * time.Hour, 80 * time.Hour, 40 * 24 * time.Hour, 400 * 24 * time.Hour} {
				a.C.Sessions = append(a.C.Sessions, proto.Session{ID: fmt.Sprint(i), Title: fmt.Sprintf("相对时间 %d", i), Cwd: "/workspace/calc", Model: "gpt-6-luna", Effort: "medium", Mode: "ask", Updated: now.Add(-d)})
			}
			a.C.Current = &a.C.Sessions[0]
		}, want: []string{"刚刚", "2 分", "3 时", "昨天", "周一", "10月", "2025 年"}},
		{name: "transcript-user-only", setup: func(a *App) {
			a.C.Entries = []*client.Entry{{Key: 1, Role: client.User, Text: "请检查 README 示例和 Add 测试。", Done: true}}
		}, want: []string{"请检查 README"}},
		{name: "transcript-streaming", setup: func(a *App) {
			a.C.Busy = true
			a.C.Entries = []*client.Entry{{Key: 1, Role: client.Assistant, Text: "正在检查 **Add** 的边界情况，接下来会", Done: false}}
		}, want: []string{"边界情况"}},
		{name: "transcript-markdown", setup: func(a *App) {
			a.C.Entries = []*client.Entry{{Key: 1, Role: client.Assistant, Text: stateMarkdown, Done: true}}
		}, want: []string{"测试报告", "Add", "README"}, scroll: "transcript"},
		{name: "transcript-30-tools", setup: func(a *App) {
			for i := range 30 {
				a.C.Entries = append(a.C.Entries, &client.Entry{Key: i + 1, Role: client.Tool, Text: "read_file", Detail: fmt.Sprintf("src/file-%02d.go", i+1), CallID: fmt.Sprint(i), Done: true})
			}
			a.opened["fold:0"] = true // the run folds; this state shows it open
		}, want: []string{"读取"}, scroll: "transcript"},
		{name: "transcript-failed-tool", setup: func(a *App) {
			e := stateTool()
			e.Failed, e.Output = true, "--- FAIL: TestAdd\nexit status 1"
			a.C.Entries = []*client.Entry{e}
			a.opened[e.CallID] = true
		}, want: []string{"FAIL: TestAdd", "exit status 1"}},
		{name: "transcript-unfinished-tool", setup: func(a *App) {
			e := stateTool()
			e.Done, e.Output = false, ""
			a.C.Entries = []*client.Entry{e}
			a.C.Busy = true
		}, want: []string{"运行", "go test ./..."}},
		{name: "transcript-output-45-lines", setup: func(a *App) {
			e := stateTool()
			e.Text, e.Detail = "read_file", "notes.txt"
			var lines []string
			for i := range 45 {
				lines = append(lines, fmt.Sprintf("line %02d: test output", i+1))
			}
			e.Output = strings.Join(lines, "\n")
			a.C.Entries = []*client.Entry{e}
			a.opened[e.CallID] = true
		}, want: []string{"还有 5 行"}, scroll: "transcript"},
		{name: "transcript-notice", setup: func(a *App) {
			a.C.Entries = []*client.Entry{{Key: 1, Role: client.Notice, Text: "已停止。未完成的工具调用保留在会话中。", Done: true}}
		}, want: []string{"已停止。"}},
		{name: "transcript-three-approvals", setup: func(a *App) {
			for _, id := range []string{"TestAdd", "TestSub", "TestMul"} {
				a.C.Approvals = append(a.C.Approvals, stateApproval(id))
			}
		}, want: []string{"TestAdd", "TestSub", "TestMul"}, scroll: "transcript"},
		{name: "transcript-answering", setup: func(a *App) {
			a.C.Approvals = []proto.Approval{stateApproval("TestAdd")}
			a.C.Answering["TestAdd"] = true
		}, want: []string{"需要你允许"}},
		{name: "composer-empty", want: []string{"让 Talos 做点什么"}},
		{name: "composer-multiline", setup: func(a *App) {
			var lines []string
			for i := range 12 {
				lines = append(lines, fmt.Sprintf("第 %02d 行：请检查输入框的多行滚动和高度。", i+1))
			}
			a.draft = strings.Join(lines, "\n")
		}, want: []string{"第 01 行"}},
		{name: "composer-three-queued", setup: func(a *App) { a.C.Queued = []string{"检查 Add", "检查 Sub", "更新 README"} }, want: []string{"检查 Add", "检查 Sub", "更新 README"}},
		{name: "composer-busy", setup: func(a *App) { a.C.Busy = true }, want: []string{"运行中：回车排队", "停止"}},
		{name: "composer-error", setup: func(a *App) { a.err = "发送失败：内核连接已断开，请稍后重试。" }, want: []string{"发送失败"}},
	}
	for _, phase := range []client.Phase{client.Starting, client.Handshaking, client.Syncing, client.Disconnected, client.Ready} {
		states = append(states, state{name: "connection-" + strings.ToLower(phase.String()), setup: func(a *App) {
			a.C.Phase, a.C.Current = phase, nil
			if phase == client.Disconnected {
				a.C.Err = "连接已断开：测试错误"
			}
		}, want: []string{map[client.Phase]string{
			client.Starting: "正在启动…", client.Handshaking: "正在连接…", client.Syncing: "正在同步…",
			client.Disconnected: "连接已断开：测试错误", client.Ready: "新建一个会话开始",
		}[phase]}})
	}
	for _, size := range [][2]int{{760, 480}, {1200, 780}, {1800, 1100}} {
		states = append(states, state{name: fmt.Sprintf("window-%dx%d", size[0], size[1]), setup: func(a *App) { a.C = working().C }, width: size[0], height: size[1], want: []string{"需要你允许", "修复 Add 测试"}})
	}
	for _, tc := range states {
		for _, dark := range []bool{false, true} {
			name := tc.name + map[bool]string{false: "-light", true: "-dark"}[dark]
			t.Run(name, func(t *testing.T) {
				a := stateApp()
				if tc.setup != nil {
					tc.setup(a)
				}
				w, h := tc.width, tc.height
				if w == 0 {
					w, h = 1200, 780
				}
				tt := stateTester(a, dark, w, h)
				for _, want := range tc.want {
					if !stateHasText(tt, want) {
						t.Errorf("missing %q in %q", want, tt.Texts())
					}
				}
				if tc.name == "palette-hit" && tt.HasText("site · ") {
					t.Error("nonmatching session is shown")
				}
				if tc.name == "transcript-output-45-lines" && (!tt.HasText("line 40:") || tt.HasText("line 41:")) {
					t.Error("tool output was not truncated at exactly 40 lines")
				}
				snapshot(t, tt, name)
				if tc.name == "composer-multiline" {
					tt.Scroll(600, 550, 0, -5000)
					snapshot(t, tt, name+"-top")
				}
				if tc.scroll == "sidebar" {
					if r, ok := tt.Find("会话 56：检查工作区"); ok && r.H > 0 {
						t.Fatalf("last workspace unexpectedly visible before scrolling: %v", r)
					}
					tt.Scroll(120, 400, 0, 5000)
					if r, ok := tt.Find("会话 56：检查工作区"); !ok || r.H <= 0 {
						t.Fatalf("last workspace cannot be reached: %v %v", r, ok)
					}
					snapshot(t, tt, name+"-scrolled")
				} else if tc.scroll == "transcript" {
					tt.Scroll(800, 300, 0, -5000)
					if tc.name == "transcript-30-tools" {
						if r, ok := tt.Find("读取 src/file-01.go"); !ok || r.H <= 0 {
							t.Fatalf("first tool is unreachable: %v %v", r, ok)
						}
					}
					snapshot(t, tt, name+"-top")
					tt.Scroll(800, 300, 0, 5000)
					if tc.name == "transcript-30-tools" {
						if r, ok := tt.Find("读取 src/file-30.go"); !ok || r.H <= 0 {
							t.Fatalf("last tool is unreachable: %v %v", r, ok)
						}
					}
					snapshot(t, tt, name+"-bottom")
				}
			})
		}
	}
}

const stateMarkdown = "# 测试报告\n\n检查 `Add(2, 3)` 和 [README](https://example.com/readme) 的示例。\n\n## 检查清单\n\n- 处理正数和负数\n- 保留零值\n- 检查边界\n\n| 输入 | 预期 | 状态 |\n| --- | --- | --- |\n| 2 + 3 | 5 | 通过 |\n| -2 + 3 | 1 | 通过 |\n\n```go\npackage calc\n\nfunc Add(a, b int) int {\n    return a + b\n}\n```\n\n" +
	"### 详细说明\n\n" +
	"1. 测试不会连接真实模型。\n2. 这个报告检查长文本的换行、列表缩进和代码块。\n3. 下面保留完整的验证记录。\n\n" +
	"```text\n=== RUN TestAdd\n--- PASS: TestAdd\n=== RUN TestSub\n--- PASS: TestSub\n=== RUN TestMul\n--- PASS: TestMul\n=== RUN TestDiv\n--- PASS: TestDiv\nok calc 0.012s\n```\n\n**验证完成**：所有示例均已检查。"

func TestAgo(t *testing.T) {
	date := func(y int, m time.Month, d, h, min int) time.Time { return time.Date(y, m, d, h, min, 0, 0, testZone) }
	for _, tc := range []struct {
		name      string
		now, then time.Time
		want      string
	}{
		{"zero", now, time.Time{}, ""},
		{"just-now", now, now.Add(-59 * time.Second), "刚刚"},
		{"one-minute", now, now.Add(-time.Minute), "1 分"},
		{"before-hour", now, now.Add(-time.Hour + time.Second), "59 分"},
		{"one-hour", now, now.Add(-time.Hour), "1 时"},
		{"same-day", now, now.Add(-3 * time.Hour), "3 时"},
		{"midnight", date(2026, 11, 13, 1, 0), date(2026, 11, 12, 23, 0), "昨天"},
		{"midnight-under-hour", date(2026, 11, 13, 0, 10), date(2026, 11, 12, 23, 40), "30 分"},
		{"month-boundary", date(2026, 12, 1, 1, 0), date(2026, 11, 30, 23, 0), "昨天"},
		{"new-year-yesterday", date(2027, 1, 1, 1, 0), date(2026, 12, 31, 23, 0), "昨天"},
		{"weekday", now, now.Add(-72 * time.Hour), "周一"},
		{"before-six-days", now, now.Add(-6*24*time.Hour + time.Second), "周五"},
		{"six-days", now, now.Add(-6 * 24 * time.Hour), "11月"},
		{"month", now, date(2026, 10, 1, 12, 0), "10月"},
		{"year", now, date(2025, 12, 31, 12, 0), "2025 年"},
		{"future", now, now.Add(time.Minute), "刚刚"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ago(tc.now, tc.then); got != tc.want {
				t.Errorf("ago(%s, %s)=%q, want %q", tc.now, tc.then, got, tc.want)
			}
		})
	}
}

func wantCommand(t *testing.T, a *App, method string, params any) {
	t.Helper()
	select {
	case cmds := <-a.out:
		if len(cmds) != 1 || cmds[0].Method != method || cmds[0].ID == "" {
			t.Fatalf("commands = %+v, want one %s", cmds, method)
		}
		want, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		var gotValue, wantValue any
		if err := json.Unmarshal(cmds[0].Params, &gotValue); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(want, &wantValue); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(gotValue, wantValue) {
			t.Errorf("params=%s, want %s", cmds[0].Params, want)
		}
	default:
		t.Fatalf("no %s command; error=%q", method, a.err)
	}
	select {
	case extra := <-a.out:
		t.Errorf("unexpected extra commands: %+v", extra)
	default:
	}
}

func TestStateInteractions(t *testing.T) {
	t.Run("open-session", func(t *testing.T) {
		a := stateApp()
		tt := stateTester(a, false, 1200, 780)
		if err := tt.Click("client 按日志重建"); err != nil {
			t.Fatal(err)
		}
		wantCommand(t, a, proto.MethodSessionGet, proto.SessionRef{SessionID: "3"})
	})
	t.Run("new-session", func(t *testing.T) {
		a := stateApp()
		tt := stateTester(a, false, 1200, 780)
		if err := tt.Click("新会话"); err != nil {
			t.Fatal(err)
		}
		wantCommand(t, a, proto.MethodSessionCreate, proto.SessionCreate{Cwd: a.C.Cwd})
	})
	t.Run("enter-sends-and-clears", func(t *testing.T) {
		a := stateApp()
		tt := stateTester(a, false, 1200, 780)
		tt.Type("检查 Add 测试")
		tt.Key(0, ui.KeyEnter)
		wantCommand(t, a, proto.MethodPrompt, proto.Prompt{SessionID: "1", Text: "检查 Add 测试"})
		if a.draft != "" {
			t.Errorf("draft=%q", a.draft)
		}
		for _, field := range tt.AccessAll() {
			if field.Role == ui.RoleTextField && field.Multiline && field.Value != "" {
				t.Errorf("composer still displays %q after sending", field.Value)
			}
		}
	})
	t.Run("busy-escape", func(t *testing.T) {
		a := stateApp()
		a.C.Busy = true
		tt := stateTester(a, false, 1200, 780)
		tt.Key(0, ui.KeyEscape)
		wantCommand(t, a, proto.MethodCancel, proto.SessionRef{SessionID: "1"})
	})
	t.Run("toggle-tool", func(t *testing.T) {
		a := stateApp()
		e := stateTool()
		e.Text, e.Detail = "read_file", "notes.txt"
		a.C.Entries = []*client.Entry{e}
		tt := stateTester(a, false, 1200, 780)
		for _, open := range []bool{true, false} {
			if err := tt.Click("读取 notes.txt"); err != nil {
				t.Fatal(err)
			}
			if a.opened[e.CallID] != open || tt.HasText(e.Output) != open {
				t.Fatalf("open=%v, output visible=%v; want %v", a.opened[e.CallID], tt.HasText(e.Output), open)
			}
		}
	})
	for _, tc := range []struct {
		label         string
		allow, always bool
	}{{"拒绝", false, false}, {"本会话都允许", true, true}, {"允许", true, false}} {
		t.Run("approval-"+tc.label, func(t *testing.T) {
			a := stateApp()
			a.C.Approvals = []proto.Approval{stateApproval("approval")}
			tt := stateTester(a, false, 1200, 780)
			if err := tt.Click(tc.label); err != nil {
				t.Fatal(err)
			}
			wantCommand(t, a, proto.MethodApprovalRespond, proto.ApprovalAnswer{SessionID: "1", ApprovalID: "approval", Allow: tc.allow, Always: tc.always})
			if !a.C.Answering["approval"] {
				t.Error("answer not marked in flight")
			}
		})
		t.Run("answering-disables-"+tc.label, func(t *testing.T) {
			a := stateApp()
			a.C.Approvals = []proto.Approval{stateApproval("approval")}
			a.C.Answering["approval"] = true
			tt := stateTester(a, false, 1200, 780)
			access, ok := tt.AccessByRole(ui.RoleButton, tc.label)
			if !ok || !access.Disabled {
				t.Fatalf("button not disabled: %+v, found=%v", access, ok)
			}
			r, ok := tt.Find(tc.label)
			if !ok || r.W <= 0 || r.H <= 0 {
				t.Fatalf("button not visible: %+v", r)
			}
			tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
			select {
			case cmd := <-a.out:
				t.Fatalf("disabled button sent %+v", cmd)
			default:
			}
			if a.err != "" {
				t.Errorf("disabled button handled click: %q", a.err)
			}
		})
	}
}

func chinese() string { return "zh-Hans" }
