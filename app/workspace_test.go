package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
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
