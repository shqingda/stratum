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

func TestSettings(t *testing.T) {
	creds := &auth.Memory{}
	creds.Set("deepseek", auth.Credential{Kind: auth.KindAPIKey, Key: "k"})
	creds.Set("xai", auth.Credential{Kind: auth.KindAPIKey, Key: "k"})
	k, err := Start(context.Background(), Options{DB: ":memory:", Creds: creds})
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	ctx := context.Background()
	m := k.Sessions
	if _, err := m.SetSettings(ctx, proto.Settings{Mode: "nope"}); err == nil {
		t.Fatal("an unknown mode was taken")
	}
	if _, err := m.SetSettings(ctx, proto.Settings{Account: "xai", Model: "grok-x", Effort: "low", Mode: "auto"}); err != nil {
		t.Fatal(err)
	}
	s, err := m.Create(ctx, proto.SessionCreate{Cwd: t.TempDir()})
	if err != nil || s.Account != "xai" || s.Model != "grok-x" || s.Effort != "low" || s.Mode != "auto" {
		t.Fatalf("new session %+v, %v", s, err)
	}
	if err := k.Server.Logout(ctx, "xai"); err != nil {
		t.Fatal(err)
	}
	if err := k.Server.Logout(ctx, "xai"); err == nil {
		t.Fatal("signed out twice")
	}
	if s, _ := m.Create(ctx, proto.SessionCreate{Cwd: t.TempDir()}); s.Account != "deepseek" {
		t.Fatalf("after signing out of the default: %+v", s)
	}
}

func TestDemo(t *testing.T) {
	k, err := Start(context.Background(), Options{DB: ":memory:", Creds: &auth.Memory{}, Demo: true})
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	ctx := context.Background()
	if list, _ := k.Server.Accounts(ctx); len(list) != 1 || list[0].ID != "demo" || list[0].Model != "demo" {
		t.Fatalf("accounts: %+v", list)
	}
	if models, _ := k.Server.Models(ctx, "demo"); len(models) != 1 || !models[0].Default {
		t.Fatalf("models: %+v", models)
	}
	if account, model := k.Sessions.Defaults(); account != "demo" || model != "demo" {
		t.Fatalf("defaults %q %q", account, model)
	}
}
