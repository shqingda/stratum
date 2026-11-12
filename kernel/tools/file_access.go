package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/shqingda/talos/kernel/perm"
)

// fileTool keeps the exported descriptor usable for specs and access inspection.
func fileTool(t *Tool) *Tool {
	run := t.Run
	t.Run = func(ctx context.Context, env Env, args json.RawMessage) (string, error) {
		return "", errors.New("file tool execution requires a fresh Set.Get call and Access check")
	}
	t.newCall = func() *Tool {
		call := *t
		var mu sync.Mutex
		const (
			fresh = iota
			checked
			finished
		)
		state := fresh
		var approved Env
		var snapshot json.RawMessage
		call.Access = func(env Env, args json.RawMessage) (perm.Request, error) {
			mu.Lock()
			defer mu.Unlock()
			if state != fresh {
				state = finished
				return perm.Request{}, errors.New("file tool instance already checked; get a new instance")
			}
			state = finished
			snapshot = bytes.Clone(args)
			req, err := t.Access(env, snapshot)
			if err != nil {
				return perm.Request{}, err
			}
			if len(req.Paths) != 1 || !filepath.IsAbs(req.Paths[0].Abs) {
				return perm.Request{}, errors.New("file access must resolve one absolute target")
			}
			approved = env
			approved.filePath = req.Paths[0].Abs
			state = checked
			return req, nil
		}
		call.Run = func(ctx context.Context, env Env, args json.RawMessage) (string, error) {
			mu.Lock()
			if state != checked {
				state = finished
				mu.Unlock()
				return "", errors.New("file tool instance has no unused Access check")
			}
			state = finished
			if env.Root != approved.Root || env.Sandboxed != approved.Sandboxed || !bytes.Equal(args, snapshot) {
				mu.Unlock()
				return "", errors.New("file tool arguments or environment changed after Access")
			}
			bound, args := approved, snapshot
			mu.Unlock()
			if err := ctx.Err(); err != nil {
				return "", err
			}
			return run(ctx, bound, args)
		}
		return &call
	}
	return t
}

// openFileParent opens the approved target without following replacement links.
func openFileParent(env Env, create bool) (*os.Root, string, error) {
	root, err := openDirectory(filepath.Dir(env.filePath), create)
	return root, filepath.Base(env.filePath), err
}

func openDirectory(path string, create bool) (*os.Root, error) {
	root, err := os.OpenRoot(string(filepath.Separator))
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		info, err := root.Lstat(part)
		if errors.Is(err, os.ErrNotExist) && create {
			if err = root.Mkdir(part, 0o755); err == nil || errors.Is(err, os.ErrExist) {
				info, err = root.Lstat(part)
			}
		}
		if err != nil {
			root.Close()
			return nil, err
		}
		if !info.IsDir() {
			root.Close()
			return nil, fmt.Errorf("file target parent %s is not a regular directory", part)
		}
		next, err := root.OpenRoot(part)
		if err != nil {
			root.Close()
			return nil, err
		}
		opened, err := next.Stat(".")
		root.Close()
		if err != nil || !os.SameFile(info, opened) {
			next.Close()
			return nil, errors.New("file target directory changed while opening")
		}
		root = next
	}
	return root, nil
}

func readFile(root *os.Root, name string) ([]byte, error) {
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", name)
	}
	return io.ReadAll(f)
}
