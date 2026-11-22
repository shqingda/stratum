package rpc

import (
	"context"
	"path"
	"sync"
	"time"

	ws "github.com/shqingda/stratum/kernel/workspace"
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

// withDirs adds the folders the files are in, each once and ending in /,
// so @ can name a folder too.
func withDirs(files []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(files)+len(files)/4)
	for _, f := range files {
		for d := path.Dir(f); d != "." && d != "/" && !seen[d]; d = path.Dir(d) {
			seen[d] = true
			out = append(out, d+"/")
		}
	}
	return append(out, files...)
}
