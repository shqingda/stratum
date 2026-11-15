package rpc

import (
	"context"
	"sync"
	"time"

	ws "github.com/shqingda/talos/kernel/workspace"
)

// The composer's @ asks at each key; a workspace's files are listed once
// for a few seconds of them.
var (
	filesMu sync.Mutex
	files   = map[string]listing{}
)

type listing struct {
	at    time.Time
	paths []string
}

const filesFresh = 5 * time.Second

func listed(ctx context.Context, cwd string) []string {
	filesMu.Lock()
	l, ok := files[cwd]
	filesMu.Unlock()
	if ok && time.Since(l.at) < filesFresh {
		return l.paths
	}
	paths := ws.Files(ctx, cwd)
	filesMu.Lock()
	files[cwd] = listing{time.Now(), paths}
	filesMu.Unlock()
	return paths
}
