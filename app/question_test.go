package app

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/proto"
)

func TestQuestion(t *testing.T) {
	a := working()
	a.C.Queued = nil
	a.C.Entries = a.C.Entries[:len(a.C.Entries)-1]
	args, _ := json.Marshal(map[string]any{"question": "README 的示例也一起改吗？", "options": []string{"一起改", "只修代码"}})
	ask := func(id string) {
		a.C.Approvals = []proto.Approval{{ID: id, Tool: "ask_user", Kind: "question", Args: args,
			Question: "README 的示例也一起改吗？", Options: []string{"一起改", "只修代码"}}}
	}
	ask("q1")
	a.out = make(chan []proto.Command, 16)
	answered := func() proto.ApprovalAnswer {
		t.Helper()
		for {
			select {
			case cmds := <-a.out:
				for _, cmd := range cmds {
					if cmd.Method == proto.MethodApprovalRespond {
						var ans proto.ApprovalAnswer
						json.Unmarshal(cmd.Params, &ans)
						return ans
					}
				}
			default:
				t.Fatal("no answer sent")
			}
		}
	}
	tt := ui.NewTester(a.View, 1200, 780)
	tt.Frame()
	if !tt.HasText("README 的示例也一起改吗？") || tt.HasText("允许") || !tt.HasText("提问") {
		t.Fatalf("no question card: %q", tt.Texts())
	}
	snapshot(t, tt, "question")
	if err := tt.Click("只修代码"); err != nil {
		t.Fatal(err)
	}
	if ans := answered(); ans.ApprovalID != "q1" || !ans.Allow || ans.Text != "只修代码" {
		t.Fatalf("option answer %+v", ans)
	}

	ask("q2")
	tt.Frame()
	a.draft = "先别动 README"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	if ans := answered(); ans.ApprovalID != "q2" || ans.Text != "先别动 README" || a.draft != "" {
		t.Fatalf("typed answer %+v, draft %q", ans, a.draft)
	}

	ask("q3")
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	if ans := answered(); ans.ApprovalID != "q3" || ans.Allow {
		t.Fatalf("skip %+v", ans)
	}
}
