package app

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/proto"
)

func TestOpenAndCommit(t *testing.T) {
	a := working()
	a.C.Busy, a.C.Approvals = false, nil
	var opened []string
	a.openIn = func(app, dir string) error {
		opened = append(opened, app, dir)
		return nil
	}
	tt := ui.NewTester(a.View, 1200, 800)
	tt.Frame()
	if tt.HasText("提交") {
		t.Fatal("a clean folder offers a commit")
	}
	if err := tt.Click("打开"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("VS Code"); err != nil {
		t.Fatal(err)
	}
	if len(opened) != 2 || opened[0] != "Visual Studio Code" || opened[1] != "/Users/me/Projects/calc" {
		t.Fatalf("%q", opened)
	}

	a.C.Workspace.Dirty, a.C.Workspace.Added, a.C.Workspace.Removed = true, 9, 3
	tt.Frame()
	if !tt.HasText("+9") || !tt.HasText("−3") {
		t.Fatal("the commit button does not count the lines")
	}
	if err := tt.Click("提交"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.rv.open {
		t.Fatal("the commit button did not open the panel")
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

func TestPRAndCIStatus(t *testing.T) {
	a := working()
	a.C.Busy, a.C.Approvals = false, nil
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
