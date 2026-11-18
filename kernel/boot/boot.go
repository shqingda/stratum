// Package boot assembles a kernel: the store, the session manager and the
// RPC server over them. talosd serves it on stdio; the desktop app serves
// it on in-process pipes, so both run the same kernel the same way.
package boot

import (
	"context"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/shqingda/talos/kernel/rpc"
	"github.com/shqingda/talos/kernel/session"
	"github.com/shqingda/talos/kernel/store"
	"github.com/shqingda/talos/llm"
	"github.com/shqingda/talos/llm/auth"
	"github.com/shqingda/talos/proto"
)

// Options say what the kernel runs on.
type Options struct {
	DB    string     // database path; ":memory:" for tests
	Creds auth.Store // signed-in accounts
	// Script, when set, answers every account in place of real providers
	// (tests and demos).
	Script *llm.Script
	// Demo, when set, offers one account, "demo", that acts out scenes
	// (llm.Play) in place of every real provider.
	Demo bool
	// OpenURL shows a sign-in page; the default browser when nil.
	OpenURL func(u string) error
}

// Kernel is an assembled kernel.
type Kernel struct {
	Server     *rpc.Server
	Sessions   *session.Manager
	store      *store.Store
	autoCancel context.CancelFunc
}

// LoadCatalog reads the model catalog (models.dev), cached in Talos's data
// directory for a day, so that model lists and defaults are current.
func LoadCatalog(ctx context.Context) {
	llm.LoadCatalog(ctx, filepath.Join(auth.Dir, "models", "models.dev.json"), 24*time.Hour, auth.CatalogNames())
}

// Start opens the store, recovers turns cut short and builds the server.
func Start(ctx context.Context, o Options) (*Kernel, error) {
	st, err := store.Open(o.DB)
	if err != nil {
		return nil, err
	}
	open := func(account string) (llm.Provider, error) { return auth.Open(o.Creds, account) }
	accounts := func() []string { names, _ := o.Creds.List(); return names }
	if o.Script != nil {
		open = func(string) (llm.Provider, error) { return o.Script, nil }
		accounts = func() []string { return []string{"script"} }
	}
	if o.Demo {
		open = func(string) (llm.Provider, error) { return llm.Play{Delay: 25 * time.Millisecond}, nil }
		accounts = func() []string { return []string{"demo"} }
	}

	m := session.New(st, open)
	m.UseMCP(filepath.Join(auth.Dir, "mcp.json"))
	m.WorktreeDir = filepath.Join(auth.Dir, "worktrees")
	m.Accounts = accounts
	m.Defaults = func() (string, string) {
		names := accounts()
		if len(names) == 0 {
			return "", ""
		}
		set, _ := m.Settings(context.Background())
		if slices.Contains(names, set.Account) {
			provider, _, _ := strings.Cut(set.Account, "/")
			return set.Account, cmpOr(set.Model, llm.DefaultModel(provider))
		}
		provider, _, _ := strings.Cut(names[0], "/")
		if o.Demo {
			return "demo", "demo"
		}
		return names[0], cmpOr(llm.DefaultModel(provider), "script")
	}
	if err := m.Recover(ctx); err != nil {
		st.Close()
		return nil, err
	}

	browse := o.OpenURL
	if browse == nil {
		browse = openURL
	}
	account := func(id string) proto.Account {
		a := proto.Account{ID: id}
		if o.Demo {
			return proto.Account{ID: id, Provider: "demo", Name: "Demo", Model: "demo"}
		}
		if p, ok := auth.Lookup(id); ok {
			a.Provider, a.Name, a.Model = p.ID, p.Name, llm.DefaultModel(p.ID)
		}
		if c, err := o.Creds.Get(id); err == nil {
			a.Email, a.Subscription = c.Email, auth.Subscription(c)
		}
		return a
	}
	srv := &rpc.Server{
		Sessions: m,
		Accounts: func(context.Context) ([]proto.Account, error) {
			out := []proto.Account{}
			for _, id := range accounts() {
				out = append(out, account(id))
			}
			return out, nil
		},
		Login: func(ctx context.Context, q proto.Login, progress func(proto.LoginProgress)) (proto.Account, error) {
			c, err := login(ctx, q, progress, browse)
			if err != nil {
				return proto.Account{}, err
			}
			if err := o.Creds.Set(q.Provider, c); err != nil {
				return proto.Account{}, err
			}
			return account(q.Provider), nil
		},
		Logout: func(_ context.Context, id string) error {
			if !slices.Contains(accounts(), id) {
				return proto.Errorf("not_found", "no account %q", id)
			}
			return o.Creds.Delete(id)
		},
		Models: func(_ context.Context, account string) ([]proto.Model, error) {
			if o.Demo {
				return []proto.Model{{ID: "demo", Name: "Demo", Context: 200000, Reasoning: true, Images: true, Default: true}}, nil
			}
			provider, _, _ := strings.Cut(account, "/")
			var out []proto.Model
			for _, x := range llm.Models(provider) {
				out = append(out, proto.Model{ID: x.ID, Name: x.Name, Context: x.Context, Reasoning: x.Reasoning, Images: x.Images,
					Default: x.ID == llm.DefaultModel(provider), Input: x.Price.Input, Output: x.Price.Output,
					CacheRead: x.Price.CacheRead, CacheWrite: x.Price.CacheWrite})
			}
			return out, nil
		},
	}
	autoCtx, autoCancel := context.WithCancel(context.Background())
	m.StartAutomations(autoCtx)
	return &Kernel{Server: srv, Sessions: m, store: st, autoCancel: autoCancel}, nil
}

// Close stops running turns and closes the store.
func (k *Kernel) Close() error {
	if k.autoCancel != nil {
		k.autoCancel()
	}
	k.Sessions.Close()
	return k.store.Close()
}

// login signs in to a provider the way talosd login does, without a
// second account label or a custom endpoint.
func login(ctx context.Context, q proto.Login, progress func(proto.LoginProgress), open func(string) error) (auth.Credential, error) {
	p, ok := auth.Lookup(q.Provider)
	if !ok || p.ID != q.Provider || p.ID == "custom" {
		return auth.Credential{}, proto.Errorf("bad_request", "unknown provider %q", q.Provider)
	}
	if q.Method == "" {
		key := strings.TrimSpace(q.Key)
		if key == "" || p.KeyURL == "" {
			return auth.Credential{}, proto.Errorf("bad_request", "%s needs a key", p.Name)
		}
		return auth.Credential{Kind: auth.KindAPIKey, Key: key}, nil
	}
	m := auth.Methods[q.Method]
	if m == nil || !slices.Contains(p.Logins, q.Method) {
		return auth.Credential{}, proto.Errorf("bad_request", "%s does not offer sign-in %q", p.Name, q.Method)
	}
	in := auth.Interaction{
		OpenURL: func(u string) error {
			progress(proto.LoginProgress{URL: u})
			return open(u)
		},
		DeviceCode: func(code, uri string) {
			progress(proto.LoginProgress{Code: code, CodeURL: uri})
			open(uri)
		},
	}
	if codex, ok := m.Flow.(*auth.Codex); ok && q.Device {
		return codex.LoginDevice(ctx, in)
	}
	return m.Flow.Login(ctx, in)
}

func openURL(u string) error { return exec.Command("open", u).Start() }

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
