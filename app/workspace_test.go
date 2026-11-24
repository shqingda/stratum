package app

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/locales"
	"github.com/shqingda/stratum/proto"
)

func TestOpenAndCommit(t *testing.T) {
	a := working()
	a.C.Busy, a.C.Approvals = false, nil
	a.C.Workspace.Branch = "main"
	a.hasApp = func(name string) bool { return name == "Visual Studio Code" || name == "Terminal" }
	var opened []string
	a.openIn = func(app, dir string) error {
		opened = append(opened, app, dir)
		return nil
	}
	tt := ui.NewTester(a.View, 1200, 800)
	tt.Frame()
	if tt.HasText("提交改动") {
		t.Fatal("a clean folder offers a commit")
	}
	if err := tt.Click("calc · main"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("用 VS Code 打开"); err != nil {
		t.Fatal(err)
	}
	if len(opened) != 2 || opened[0] != "Visual Studio Code" || opened[1] != "/Users/me/Projects/calc" {
		t.Fatalf("%q", opened)
	}

	a.C.Workspace.Dirty, a.C.Workspace.Added, a.C.Workspace.Removed = true, 9, 3
	tt.Frame()
	if !tt.HasText("+9") || !tt.HasText("−3") {
		t.Fatal("the inspect button does not count the lines")
	}
	if err := tt.Click(locales.ShowChanges()); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	if !a.rv.open {
		t.Fatal("the inspect button did not open the panel")
	}
	if err := tt.Click("提交说明"); err != nil {
		t.Fatal(err)
	}
	tt.Type("修好 Add")
	if err := tt.Click("提交改动"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.C.Workspace.Committing || a.commitMsg != "" {
		t.Fatalf("%+v %q", a.C.Workspace, a.commitMsg)
	}
}

func TestWorkspaceCapsuleInstalledOpenersAndNoDetail(t *testing.T) {
	a := working()
	a.C.Busy, a.C.Approvals = false, nil
	a.C.Workspace.Branch = "main"
	a.C.Workspace.Branches = []string{"main", "feat"}
	// Only Terminal and Cursor are installed; VS Code is NOT installed.
	a.hasApp = func(name string) bool { return name == "Terminal" || name == "Cursor" }
	tt := ui.NewTester(a.View, 1200, 800)
	tt.Frame()
	if err := tt.Click("calc · main"); err != nil {
		t.Fatal(err)
	}
	menu := tt.Menu()
	has := func(sub string) bool {
		for _, m := range menu {
			if m == sub {
				return true
			}
		}
		return false
	}
	if !has("用 Cursor 打开") || !has("用 终端 打开") || !has("用 访达 打开") {
		t.Fatalf("expected Cursor, Terminal, Finder in menu, got %v", menu)
	}
	if has("用 VS Code 打开") {
		t.Fatalf("uninstalled VS Code should not be in menu: %v", menu)
	}
	for _, m := range menu {
		if m == "详情：纯文本" || m == "详情：含思考" || m == "详情：全部" {
			t.Fatalf("Detail item should not be in workspace capsule menu: %v", menu)
		}
	}
}

func TestPRAndCIStatus(t *testing.T) {
	a := working()
	a.C.Busy, a.C.Approvals = false, nil
	a.rv.open = true
	a.out = make(chan []proto.Command, 16)
	var openedURL string
	a.openURL = func(u string) error { openedURL = u; return nil }
	a.C.Workspace.Branch = "fix-add"
	a.C.Workspace.Base = "main"
	a.C.Workspace.Ahead = 2
	tt := ui.NewTester(a.View, 1200, 800)
	tt.Frame()
	if !tt.HasText("创建 PR") {
		t.Fatalf("missing Create PR button in %q", tt.Texts())
	}
	if err := tt.Click("创建 PR"); err != nil {
		t.Fatal(err)
	}
	cmd := sent(a, proto.MethodCreatePR)
	var prReq proto.CreatePR
	if cmd == nil || json.Unmarshal(cmd.Params, &prReq) != nil || prReq.Title != "修复 Add 测试" {
		t.Fatalf("create PR cmd = %+v", prReq)
	}

	a.C.Workspace.Pushing = false
	a.C.Workspace.Ahead = 0
	a.C.Workspace.PR = &proto.PullRequest{Number: 17, Title: "Fix Add", URL: "https://github.com/org/repo/pull/17", State: "open", Checks: "pass"}
	tt.Frame()
	if !tt.HasText("PR #17") {
		t.Fatalf("missing PR #17 in %q", tt.Texts())
	}
	if err := tt.Click("PR #17"); err != nil {
		t.Fatal(err)
	}
	if openedURL != "https://github.com/org/repo/pull/17" {
		t.Fatalf("openedURL = %q", openedURL)
	}
}
