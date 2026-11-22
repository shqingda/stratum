package app

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/client"
	"github.com/shqingda/stratum/proto"
)

func TestFileUndo(t *testing.T) {
	a := screen1()
	c := a.C
	c.Busy, c.Approvals, c.Queued = false, nil, nil
	c.Turns = []client.Turn{{Seq: 3, Start: now.Add(-time.Minute)}}
	c.Changes.Turns = []proto.ChangeTurn{{TurnSeq: 3, Files: []proto.ChangeFile{
		{Path: "/Users/me/Projects/calc/add.go", Added: 1, Removed: 1},
		{Path: "/Users/me/Projects/calc/README.md", Added: 1, Removed: 1},
	}}}
	a.out = make(chan []proto.Command, 8)
	tt := ui.NewTester(a.View, 1440, 880)
	tt.Frame()
	if err := tt.Click("撤销 add.go"); err != nil {
		t.Fatal(err)
	}
	var sent *proto.ChangeQuery
	for len(a.out) > 0 {
		for _, cmd := range <-a.out {
			if cmd.Method == proto.MethodChangesRevert {
				sent = &proto.ChangeQuery{}
				json.Unmarshal(cmd.Params, sent)
			}
		}
	}
	if sent == nil || sent.TurnSeq != 3 || sent.Path != "/Users/me/Projects/calc/add.go" {
		t.Fatalf("revert sent: %+v", sent)
	}
	c.Changes.RevertedFiles[client.DiffKey(3, "/Users/me/Projects/calc/add.go")] = true // as the log would say
	tt.Frame()
	if err := tt.Click("重做 add.go"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("撤销 README.md"); err != nil {
		t.Fatal("the other file's undo is gone: ", err)
	}
}
