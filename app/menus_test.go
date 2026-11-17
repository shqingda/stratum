package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/client"
	"github.com/shqingda/talos/proto"
)

func task8App() *App {
	a := working()
	a.C.Approvals, a.C.Queued, a.C.Busy = nil, nil, false
	a.C.Current.Account = "chatgpt"
	for i, e := range a.C.Entries {
		e.Key = -i - 1
	}
	a.out = make(chan []proto.Command, 20)
	a.now = func() time.Time { return time.Date(2026, 11, 12, 15, 0, 0, 0, time.FixedZone("T8", 8*3600)) }
	for i := range a.C.Sessions {
		a.C.Sessions[i].Updated = a.now().Add(a.C.Sessions[i].Updated.Sub(now))
	}
	return a
}

func task8Tester(a *App, dark bool) *ui.Tester {
	tt := ui.NewTester(a.View, 1200, 780)
	tt.SetDark(dark)
	tt.SetTitleBar(ui.TitleBar{Left: 78, Height: 52})
	tt.Frame()
	return tt
}

// next is the next batch of commands the app sent, leaving out the ⌘K
// palette's searches of messages and files.
func next(a *App) ([]proto.Command, bool) {
	for {
		select {
		case cmds := <-a.out:
			if len(cmds) == 1 && (cmds[0].Method == proto.MethodSearch || cmds[0].Method == proto.MethodFilesFind) {
				continue
			}
			return cmds, true
		default:
			return nil, false
		}
	}
}

func task8Command(t *testing.T, a *App, method string, params any) proto.Command {
	t.Helper()
	cmds, ok := next(a)
	switch {
	case ok:
		if len(cmds) != 1 || cmds[0].Method != method {
			t.Fatalf("commands = %+v, want one %s", cmds, method)
		}
		if params != nil {
			if err := json.Unmarshal(cmds[0].Params, params); err != nil {
				t.Fatal(err)
			}
		}
		return cmds[0]
	default:
		t.Fatalf("no %s command", method)
		return proto.Command{}
	}
}

func task8NoCommand(t *testing.T, a *App) {
	t.Helper()
	if cmds, ok := next(a); ok {
		t.Fatalf("unexpected commands: %+v", cmds)
	}
}

func task8Models(a *App, id string, models []proto.Model) {
	b, _ := json.Marshal(models)
	a.receive(proto.Frame{Type: proto.FrameResponse, ID: id, Result: b})
}

func task8Click(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
}

func TestTask8Menus(t *testing.T) {
	for _, dark := range []bool{false, true} {
		appearance := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(appearance, func(t *testing.T) {
			a := task8App()
			tt := task8Tester(a, dark)
			task8Click(t, tt, "gpt-6-luna")
			var query proto.ModelsQuery
			cmd := task8Command(t, a, proto.MethodModels, &query)
			if query.Account != "chatgpt" || !tt.HasText("加载中…") {
				t.Fatalf("query %+v, texts %q", query, tt.Texts())
			}
			snapshot(t, tt, "t8-model-loading-"+appearance)
			task8Models(a, cmd.ID, []proto.Model{
				{ID: "gpt-6-luna", Name: "GPT 6 Luna", Context: 200_000, Reasoning: true},
				{ID: "gpt-6-sol", Name: "GPT 6 Sol", Context: 1_000_000, Reasoning: true, Default: true},
			})
			tt.Frame()
			for _, want := range []string{"GPT 6 Luna", "GPT 6 Sol", "200K", "1M", "✓ 默认模型"} {
				if !tt.HasText(want) {
					t.Errorf("missing %q in %q", want, tt.Texts())
				}
			}
			if selected, ok := tt.AccessByRole(ui.RoleMenuItemCheckBox, "GPT 6 Luna"); !ok || !selected.Checked {
				t.Errorf("current model is not checked: %+v", selected)
			}
			snapshot(t, tt, "t8-model-"+appearance)
			if err := tt.ChooseMenuItem("GPT 6 Sol"); err != nil {
				t.Fatal(err)
			}
			var update proto.SessionUpdate
			task8Command(t, a, proto.MethodSessionUpdate, &update)
			if update.SessionID != "1" || update.Model == nil || *update.Model != "gpt-6-sol" {
				t.Fatalf("model update = %+v", update)
			}
			task8Click(t, tt, "中")
			if got := tt.Menu(); fmt.Sprint(got) != "[默认 最低 低 中 高 极高]" {
				t.Fatalf("effort menu = %q", got)
			}
			snapshot(t, tt, "t8-effort-"+appearance)
			if err := tt.ChooseMenuItem("极高"); err != nil {
				t.Fatal(err)
			}
			update = proto.SessionUpdate{}
			task8Command(t, a, proto.MethodSessionUpdate, &update)
			if update.Effort == nil || *update.Effort != "xhigh" {
				t.Fatalf("effort update = %+v", update)
			}
			task8Click(t, tt, "询问")
			for _, text := range []string{"写文件和运行命令前问你", "沙箱内自动允许", "不再询问，慎用"} {
				if !tt.HasText(text) {
					t.Errorf("missing mode explanation %q", text)
				}
			}
			snapshot(t, tt, "t8-mode-"+appearance)
			if err := tt.ChooseMenuItem("自动"); err != nil {
				t.Fatal(err)
			}
			update = proto.SessionUpdate{}
			task8Command(t, a, proto.MethodSessionUpdate, &update)
			if update.Mode == nil || *update.Mode != "auto" {
				t.Fatalf("mode update = %+v", update)
			}
			task8Click(t, tt, "gpt-6-luna")
			cmd = task8Command(t, a, proto.MethodModels, nil)
			task8Models(a, cmd.ID, nil)
			tt.Frame()
			if !tt.HasText("这个账号没有可用模型") {
				t.Fatal("empty models not shown")
			}
			snapshot(t, tt, "t8-model-empty-"+appearance)
		})
	}
}

func TestTask8AllMenuValues(t *testing.T) {
	for _, tc := range []struct{ pill, label, field, value string }{
		{"中", "默认", "effort", ""}, {"中", "最低", "effort", "minimal"},
		{"中", "低", "effort", "low"}, {"中", "中", "effort", "medium"},
		{"中", "高", "effort", "high"}, {"中", "极高", "effort", "xhigh"},
		{"询问", "询问", "mode", "ask"}, {"询问", "自动", "mode", "auto"}, {"询问", "完全", "mode", "full"},
	} {
		t.Run(tc.field+"/"+tc.label, func(t *testing.T) {
			a := task8App()
			tt := task8Tester(a, false)
			task8Click(t, tt, tc.pill)
			if err := tt.ChooseMenuItem(tc.label); err != nil {
				t.Fatal(err)
			}
			var params map[string]any
			task8Command(t, a, proto.MethodSessionUpdate, &params)
			if len(params) != 2 || params[tc.field] != tc.value || params["sessionId"] != "1" {
				t.Fatalf("params = %+v", params)
			}
		})
	}
}

func TestTask8ModelsFailureAndStaleResponses(t *testing.T) {
	a := task8App()
	tt := task8Tester(a, false)
	task8Click(t, tt, "gpt-6-luna")
	first := task8Command(t, a, proto.MethodModels, nil)
	tt.Key(0, ui.KeyEscape)
	task8NoCommand(t, a)
	a.C.Current.Account = "other"
	tt.Frame()
	task8Click(t, tt, "gpt-6-luna")
	var query proto.ModelsQuery
	second := task8Command(t, a, proto.MethodModels, &query)
	if query.Account != "other" {
		t.Fatalf("account = %q", query.Account)
	}
	task8Models(a, first.ID, []proto.Model{{ID: "stale", Name: "过期模型"}})
	tt.Frame()
	if !tt.HasText("加载中…") || tt.HasText("过期模型") {
		t.Fatal("an old response ended loading or leaked models")
	}
	a.receive(proto.Frame{Type: proto.FrameResponse, ID: second.ID, Error: proto.Errorf("test", "离线")})
	tt.Frame()
	if !tt.HasText("加载失败，请重新打开菜单") || tt.HasText("加载中…") {
		t.Fatal("model request failure is not shown")
	}
	tt.Key(0, ui.KeyEscape)
	task8Click(t, tt, "gpt-6-luna")
	third := task8Command(t, a, proto.MethodModels, nil)
	task8Models(a, third.ID, []proto.Model{{ID: "gpt-6-luna", Name: "Luna", Reasoning: false}})
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	if b, ok := tt.Access("中"); !ok || !b.Disabled || b.Description != "这个模型不支持思考强度" {
		t.Fatalf("reasoning button = %+v, found %v", b, ok)
	}
	task8NoCommand(t, a)
	task8Click(t, tt, "gpt-6-luna")
	task8Command(t, a, proto.MethodModels, nil)
	a.C.Disconnected(nil)
	tt.Frame()
	if len(tt.Menu()) != 0 {
		t.Fatal("menu stayed open after disconnect")
	}
}

func TestTask8PhaseAndContextText(t *testing.T) {
	for phase, want := range map[client.Phase]string{
		client.Starting: "正在启动", client.Handshaking: "正在连接", client.Syncing: "正在同步",
		client.Ready: "已连接", client.Disconnected: "已断开连接", client.Phase(99): "连接状态未知",
	} {
		if got := phaseText(phase); got != want {
			t.Errorf("phase %d = %q, want %q", phase, got, want)
		}
	}
	for n, want := range map[int]string{-1: "", 0: "", 512: "512", 200_000: "200K", 1_500_000: "1.5M"} {
		if got := contextSize(n); got != want {
			t.Errorf("context %d = %q, want %q", n, got, want)
		}
	}
}

func TestTask8ModelMenuClosesOnSessionSwitch(t *testing.T) {
	a := task8App()
	tt := task8Tester(a, false)
	task8Click(t, tt, "gpt-6-luna")
	first := task8Command(t, a, proto.MethodModels, nil)
	a.C.Current = &proto.Session{ID: "other", Account: "other-account", Model: "other-model", Mode: "ask"}
	tt.Frame()
	if len(tt.Menu()) != 0 {
		t.Fatal("model menu stayed open for a different session")
	}
	task8Click(t, tt, "other-model")
	var query proto.ModelsQuery
	second := task8Command(t, a, proto.MethodModels, &query)
	if query.Account != "other-account" {
		t.Fatalf("new account = %q", query.Account)
	}
	task8Models(a, first.ID, []proto.Model{{ID: "old", Name: "旧账号模型"}})
	tt.Frame()
	if a.modelsPending != second.ID || tt.HasText("旧账号模型") {
		t.Fatal("late response crossed session/account boundary")
	}
	task8Models(a, second.ID, []proto.Model{{ID: "other-model", Name: "新账号模型"}})
	tt.Frame()
	if !tt.HasText("新账号模型") || a.modelsPending != "" {
		t.Fatal("new session's model response not shown")
	}
}

func TestTask8ModelFailureDoesNotDisconnectNewSession(t *testing.T) {
	a := task8App()
	tt := task8Tester(a, false)
	task8Click(t, tt, "gpt-6-luna")
	models := task8Command(t, a, proto.MethodModels, nil)
	tt.Key(0, ui.KeyEscape)
	tt.Key(ui.Cmd, ui.KeyN)
	task8Command(t, a, proto.MethodSessionCreate, nil)
	if a.C.Phase != client.Syncing {
		t.Fatalf("new session phase = %v, want Syncing", a.C.Phase)
	}
	a.receive(proto.Frame{Type: proto.FrameResponse, ID: models.ID, Error: proto.Errorf("test", "旧模型列表请求失败")})
	if a.C.Phase != client.Syncing {
		t.Fatalf("unrelated models response changed new session phase to %v: %s", a.C.Phase, a.C.Err)
	}
}

func TestTask8ModelStates(t *testing.T) {
	for _, dark := range []bool{false, true} {
		appearance := map[bool]string{false: "light", true: "dark"}[dark]
		a := task8App()
		a.C.Models = []proto.Model{{ID: "gpt-6-luna", Name: "Luna", Reasoning: false}}
		tt := task8Tester(a, dark)
		if b, ok := tt.Access("中"); !ok || !b.Disabled || b.Description != "这个模型不支持思考强度" {
			t.Fatalf("known unsupported model: %+v, found %v", b, ok)
		}
		b, _ := tt.Find("中")
		tt.Move(b.X+b.W/2, b.Y+b.H/2)
		tt.Advance(time.Second)
		if !tt.HasText("这个模型不支持思考强度") {
			t.Fatal("disabled pill has no visible tooltip")
		}
		snapshot(t, tt, "t8-effort-disabled-"+appearance)
		task8Click(t, tt, "gpt-6-luna")
		cmd := task8Command(t, a, proto.MethodModels, nil)
		if b, _ := tt.Access("中"); !b.Disabled {
			t.Fatal("refreshing models enabled an unsupported reasoning setting")
		}
		a.receive(proto.Frame{Type: proto.FrameResponse, ID: cmd.ID, Error: proto.Errorf("test", "账号暂时离线")})
		tt.Frame()
		snapshot(t, tt, "t8-model-error-"+appearance)
		tt.Key(0, ui.KeyEscape)
		a.modelsAccount, a.modelsError = "chatgpt", ""
		a.C.Current.Account = "another"
		tt.Frame()
		if b, _ := tt.Access("中"); b.Disabled {
			t.Fatal("another account inherited old model capabilities")
		}
	}
}

func TestTask8PhaseViews(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, phase := range []client.Phase{client.Starting, client.Handshaking, client.Syncing, client.Ready, client.Disconnected} {
			a := task8App()
			a.C.Current, a.C.Entries = nil, nil
			a.C.Phase = phase
			tt := task8Tester(a, dark)
			if !tt.HasText(phaseText(phase)) {
				t.Fatalf("missing phase %q", phaseText(phase))
			}
			if phase != client.Ready && tt.HasText(phase.String()) {
				t.Errorf("phase %d (dark=%v): status still shows English %q", phase, dark, phase.String())
			}
			appearance := map[bool]string{false: "light", true: "dark"}[dark]
			snapshot(t, tt, fmt.Sprintf("t8-phase-%d-%s", phase, appearance))
		}
	}
}

// A session not yet begun moves to another folder: one there takes its
// place, and the empty one goes; a begun one stays.
func TestMoveTo(t *testing.T) {
	a := task8App()
	old := a.C.Current.ID
	a.moveTo(a.C.Current.Cwd)
	task8NoCommand(t, a)

	a.moveTo("/tmp/other")
	var sc proto.SessionCreate
	task8Command(t, a, proto.MethodSessionCreate, &sc)
	if sc.Cwd != "/tmp/other" {
		t.Fatalf("created in %q", sc.Cwd)
	}
	task8NoCommand(t, a) // the old one had begun

	a = task8App()
	a.C.Entries = nil
	a.moveTo("/tmp/other")
	task8Command(t, a, proto.MethodSessionCreate, nil)
	var ref proto.SessionRef
	task8Command(t, a, proto.MethodSessionDelete, &ref)
	if ref.SessionID != old {
		t.Fatalf("deleted %q, want %q", ref.SessionID, old)
	}
}

// The / menu lists the app's commands and the folder's skills; Enter
// runs the one chosen, and a skill becomes words for the agent.
func TestSlashMenu(t *testing.T) {
	a := task8App()
	a.C.MCP, a.C.MCPFor = &proto.MCPList{Skills: []proto.SkillInfo{{Name: "release", Description: "Cut a release"}}}, a.C.Current.Cwd
	tt := task8Tester(a, false)
	tt.Type("/se")
	tt.Frame()
	if !a.slash.open || a.slashLen != 2 { // search, settings
		t.Fatalf("open %v, %d items", a.slash.open, a.slashLen)
	}
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !a.settingsOpen || a.draft != "" {
		t.Fatalf("settings %v, draft %q", a.settingsOpen, a.draft)
	}
	a.settingsOpen = false
	tt.Frame()
	tt.Type("/rel")
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !strings.Contains(a.draft, "release") {
		t.Fatalf("draft %q", a.draft)
	}
}

// / sets the session's permission mode.
func TestSlashMode(t *testing.T) {
	a := task8App()
	a.C.MCP, a.C.MCPFor = &proto.MCPList{}, a.C.Current.Cwd
	tt := task8Tester(a, false)
	tt.Type("/full")
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	var u proto.SessionUpdate
	task8Command(t, a, proto.MethodSessionUpdate, &u)
	if u.Mode == nil || *u.Mode != "full" || a.draft != "" {
		t.Fatalf("update %+v, draft %q", u, a.draft)
	}
}

// /undo puts back the last turn's changes.
func TestSlashUndo(t *testing.T) {
	a := task8App()
	a.C.MCP, a.C.MCPFor = &proto.MCPList{}, a.C.Current.Cwd
	a.C.Changes.Turns = []proto.ChangeTurn{{TurnSeq: 3, Files: []proto.ChangeFile{{Path: "a.go"}}}}
	tt := task8Tester(a, false)
	tt.Type("/undo")
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	var q proto.ChangeQuery
	task8Command(t, a, proto.MethodChangesRevert, &q)
	if q.TurnSeq != 3 {
		t.Fatalf("%+v", q)
	}
}

// /compact asks the kernel to summarize now.
func TestSlashCompact(t *testing.T) {
	a := task8App()
	a.C.MCP, a.C.MCPFor = &proto.MCPList{}, a.C.Current.Cwd
	a.C.Entries = []*client.Entry{{Key: 1}}
	tt := task8Tester(a, false)
	tt.Type("/compact")
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	var p proto.Prompt
	task8Command(t, a, proto.MethodPrompt, &p)
	if !p.Compact || a.draft != "" {
		t.Fatalf("%+v, draft %q", p, a.draft)
	}
}

// A saved command goes in as /name, and is sent as its prompt.
func TestSlashCommand(t *testing.T) {
	a := task8App()
	a.C.MCP, a.C.MCPFor = &proto.MCPList{Commands: []proto.CommandInfo{{Name: "ship", Body: "Release $ARGUMENTS now"}}}, a.C.Current.Cwd
	tt := task8Tester(a, false)
	tt.Type("/sh")
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.draft != "/ship " {
		t.Fatalf("draft %q", a.draft)
	}
	if got := a.expandCommand("/ship v2"); got != "Release v2 now" {
		t.Fatalf("%q", got)
	}
	if got := a.expandCommand("/other x"); got != "/other x" {
		t.Fatalf("%q", got)
	}
}
