package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shqingda/talos/kernel/boot"
	"github.com/shqingda/talos/llm"
	"github.com/shqingda/talos/llm/auth"
)

// serveRPC serves the kernel over stdio until stdin closes.
func serveRPC(ctx context.Context, creds auth.Store, args []string) error {
	fs := flag.NewFlagSet("rpc", flag.ExitOnError)
	db := fs.String("db", filepath.Join(auth.Dir, "talos.db"), "database `path`")
	script := fs.String("script", "", "replay model turns from a JSON `file` instead of calling providers (for tests)")
	fs.Parse(args)

	o := boot.Options{DB: *db, Creds: creds}
	if *script != "" {
		b, err := os.ReadFile(*script)
		if err != nil {
			return err
		}
		s := &llm.Script{}
		if err := json.Unmarshal(b, &s.Turns); err != nil {
			return fmt.Errorf("script: %w", err)
		}
		o.Script = s
	} else {
		loadCatalog(ctx)
	}
	k, err := boot.Start(ctx, o)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.Server.Serve(ctx, os.Stdin, os.Stdout)
}
