package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/client"
	"github.com/shqingda/stratum/proto"
)

func sent(a *App, method string) *proto.Command {
	for {
		select {
		case cmds := <-a.out:
			for _, cmd := range cmds {
				if cmd.Method == method {
					return &cmd
				}
			}
		default:
			return nil
		}
	}
}

func TestSearchHits(t *testing.T) {
	a := working()
	a.out = make(chan []proto.Command, 16)
	var user *client.Entry
	for _, e := range a.C.Entries {
		if e.Role == client.User {
			user = e
		}
	}
	user.Seq = 42
	a.pal, a.query = true, "README"
	a.C.Hits, a.C.HitsFor = []proto.SearchHit{{SessionID: a.C.Current.ID, Seq: 42, Snippet: "顺便看看 [README] 里的示例对不对"}}, "README"
	tt := ui.NewTester(a.View, 1200, 780)
	tt.Frame()
	if !tt.HasText("顺便看看 README 里的示例对不对") {
		t.Fatal("hit not shown")
	}
	snapshot(t, tt, "palette-messages")
	if err := tt.Click("顺便看看 README 里的示例对不对"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.jumpIn != a.C.Current.ID || a.jump != 0 || a.pal {
		t.Fatalf("jump %d in %q, pal %v", a.jump, a.jumpIn, a.pal)
	}
}

func TestOlderLoads(t *testing.T) {
	a := working()
	a.out = make(chan []proto.Command, 16)
	a.C.Older = true
	tt := ui.NewTester(a.View, 1200, 780)
	tt.Frame()
	cmd := sent(a, proto.MethodEvents)
	if cmd == nil {
		t.Fatal("older events not asked for")
	}
	if !a.C.LoadingOlder || !tt.HasText("正在加载更早的消息") {
		t.Fatal("not loading")
	}
}
