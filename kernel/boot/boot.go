// Package boot assembles a kernel: the store, the session manager and the
// RPC server over them. talosd serves it on stdio; the desktop app serves
// it on in-process pipes, so both run the same kernel the same way.
package boot

import (
	"context"
	"path/filepath"
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
}

// Kernel is an assembled kernel.
type Kernel struct {
	Server   *rpc.Server
	Sessions *session.Manager
	store    *store.Store
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

	m := session.New(st, open)
	m.Defaults = func() (string, string) {
		names := accounts()
		if len(names) == 0 {
			return "", ""
		}
		provider, _, _ := strings.Cut(names[0], "/")
		return names[0], cmpOr(llm.DefaultModel(provider), "script")
	}
	if err := m.Recover(ctx); err != nil {
		st.Close()
		return nil, err
	}

	srv := &rpc.Server{
		Sessions: m,
		Accounts: func(context.Context) ([]proto.Account, error) {
			var out []proto.Account
			for _, id := range accounts() {
				a := proto.Account{ID: id}
				if p, ok := auth.Lookup(id); ok {
					a.Provider, a.Name = p.ID, p.Name
				}
				if c, err := o.Creds.Get(id); err == nil {
					a.Email, a.Subscription = c.Email, auth.Subscription(c)
				}
				out = append(out, a)
			}
			return out, nil
		},
		Models: func(_ context.Context, account string) ([]proto.Model, error) {
			provider, _, _ := strings.Cut(account, "/")
			var out []proto.Model
			for _, x := range llm.Models(provider) {
				out = append(out, proto.Model{ID: x.ID, Name: x.Name, Context: x.Context, Reasoning: x.Reasoning,
					Default: x.ID == llm.DefaultModel(provider), Input: x.Price.Input, Output: x.Price.Output})
			}
			return out, nil
		},
	}
	return &Kernel{Server: srv, Sessions: m, store: st}, nil
}

// Close stops running turns and closes the store.
func (k *Kernel) Close() error {
	k.Sessions.Close()
	return k.store.Close()
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
