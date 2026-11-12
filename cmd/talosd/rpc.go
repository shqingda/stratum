package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shqingda/talos/kernel/rpc"
	"github.com/shqingda/talos/kernel/session"
	"github.com/shqingda/talos/kernel/store"
	"github.com/shqingda/talos/llm"
	"github.com/shqingda/talos/llm/auth"
	"github.com/shqingda/talos/proto"
)

// serveRPC serves the kernel over stdio until stdin closes.
func serveRPC(ctx context.Context, creds auth.Store, args []string) error {
	fs := flag.NewFlagSet("rpc", flag.ExitOnError)
	db := fs.String("db", filepath.Join(auth.Dir, "talos.db"), "database `path`")
	script := fs.String("script", "", "replay model turns from a JSON `file` instead of calling providers (for tests)")
	fs.Parse(args)

	st, err := store.Open(*db)
	if err != nil {
		return err
	}
	defer st.Close()

	open := func(account string) (llm.Provider, error) { return auth.Open(creds, account) }
	accounts := func() []string { names, _ := creds.List(); return names }
	if *script != "" {
		b, err := os.ReadFile(*script)
		if err != nil {
			return err
		}
		s := &llm.Script{}
		if err := json.Unmarshal(b, &s.Turns); err != nil {
			return fmt.Errorf("script: %w", err)
		}
		open = func(string) (llm.Provider, error) { return s, nil }
		accounts = func() []string { return []string{"script"} }
	} else {
		loadCatalog(ctx)
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
		return err
	}
	defer m.Close()

	srv := &rpc.Server{
		Sessions: m,
		Accounts: func(context.Context) ([]proto.Account, error) {
			var out []proto.Account
			for _, id := range accounts() {
				a := proto.Account{ID: id}
				if p, ok := auth.Lookup(id); ok {
					a.Provider, a.Name = p.ID, p.Name
				}
				if c, err := creds.Get(id); err == nil {
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
	return srv.Serve(ctx, os.Stdin, os.Stdout)
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
