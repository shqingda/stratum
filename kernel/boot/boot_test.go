package boot

import (
	"context"
	"testing"

	"github.com/shqingda/talos/llm/auth"
	"github.com/shqingda/talos/proto"
)

func TestLogin(t *testing.T) {
	creds := &auth.Memory{}
	k, err := Start(context.Background(), Options{DB: ":memory:", Creds: creds,
		OpenURL: func(string) error { t.Fatal("opened a browser"); return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	ctx := context.Background()
	if list, _ := k.Server.Accounts(ctx); list == nil || len(list) != 0 {
		t.Fatalf("accounts before signing in: %#v", list)
	}
	nop := func(proto.LoginProgress) {}
	for _, q := range []proto.Login{
		{Provider: "nope", Key: "k"},
		{Provider: "deepseek/work", Key: "k"},
		{Provider: "custom", Key: "k"},
		{Provider: "deepseek", Key: "  "},
		{Provider: "codex", Key: "k"},
		{Provider: "deepseek", Method: "codex"},
	} {
		if _, err := k.Server.Login(ctx, q, nop); err == nil {
			t.Errorf("%+v was taken", q)
		}
	}
	a, err := k.Server.Login(ctx, proto.Login{Provider: "deepseek", Key: " sk-test \n"}, nop)
	if err != nil || a.ID != "deepseek" || a.Name != "DeepSeek" || a.Model == "" {
		t.Fatalf("signed in as %+v, %v", a, err)
	}
	if c, _ := creds.Get("deepseek"); c.Key != "sk-test" {
		t.Fatalf("stored %+v", c)
	}
	if list, _ := k.Server.Accounts(ctx); len(list) != 1 || list[0].ID != "deepseek" {
		t.Fatalf("accounts: %+v", list)
	}
}
