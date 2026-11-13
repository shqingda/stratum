package client

import (
	"encoding/json"
	"testing"

	"github.com/shqingda/talos/proto"
)

func TestLogin(t *testing.T) {
	c, _ := connected(t, []proto.Event{})
	cmds, err := c.Login(proto.Login{Provider: "codex", Method: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	login := one(t, cmds, proto.MethodLogin)
	progress, _ := json.Marshal(proto.LoginProgress{Code: "K7QM-4X2P", CodeURL: "https://example.test/device"})
	c.Receive(proto.Frame{Type: proto.FrameEvent, Event: &proto.Event{Type: proto.EventLogin, Data: progress}})
	if c.SignIn == nil || c.SignIn.Code != "K7QM-4X2P" {
		t.Fatalf("progress: %+v", c.SignIn)
	}
	update := one(t, c.Receive(resp(login.ID, proto.Account{ID: "codex", Model: "gpt-6-luna"})), proto.MethodSessionUpdate)
	if u := params[proto.SessionUpdate](t, update); *u.Account != "codex" || *u.Model != "gpt-6-luna" {
		t.Fatalf("update %+v", u)
	}
	if c.SignIn != nil || len(c.Accounts) != 1 {
		t.Fatalf("after signing in: %+v %+v", c.SignIn, c.Accounts)
	}

	cmds, _ = c.Login(proto.Login{Provider: "deepseek", Key: "k"})
	c.Receive(errResp(cmds[0].ID, "bad_request", "no"))
	if c.SignIn == nil || c.SignIn.Err != "no" {
		t.Fatalf("failure: %+v", c.SignIn)
	}
	cmds, _ = c.Login(proto.Login{Provider: "xai", Method: "xai"})
	cancel, _ := c.CancelLogin()
	one(t, cancel, proto.MethodLoginCancel)
	c.Receive(errResp(cmds[0].ID, "cancelled", "sign-in cancelled"))
	if c.SignIn != nil {
		t.Fatalf("after cancelling: %+v", c.SignIn)
	}
}
