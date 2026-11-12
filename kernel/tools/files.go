package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/shqingda/talos/kernel/perm"
)

const maxRead = 256 << 10 // bytes read_file returns

// ReadFile reads a text file, with line numbers, a window at a time.
var ReadFile = &Tool{
	Name:        "read_file",
	Description: "Read a text file with line numbers. Paths are relative to the workspace. Use offset and limit (lines) for large files.",
	Schema: schema(`{"type":"object","properties":{
		"path":{"type":"string"},
		"offset":{"type":"integer","description":"first line, from 1"},
		"limit":{"type":"integer","description":"lines to read; default 2000"}},
		"required":["path"]}`),
	Access: func(env Env, args json.RawMessage) (perm.Request, error) {
		var a struct{ Path string }
		if err := decode("read_file", args, &a); err != nil {
			return perm.Request{}, err
		}
		return pathAccess("read_file", perm.Read, env, a.Path)
	},
	Run: func(ctx context.Context, env Env, args json.RawMessage) (string, error) {
		var a struct {
			Path          string
			Offset, Limit int
		}
		decode("read_file", args, &a)
		path, err := resolve(env, a.Path)
		if err != nil {
			return "", err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		if bytes.IndexByte(b[:min(len(b), 8000)], 0) >= 0 {
			return "", fmt.Errorf("%s is a binary file", a.Path)
		}
		if len(b) == 0 {
			return "(empty file)", nil
		}
		lines := strings.SplitAfter(string(b), "\n")
		if lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		first := max(a.Offset, 1)
		limit := a.Limit
		if limit <= 0 {
			limit = 2000
		}
		if first > len(lines) {
			return "", fmt.Errorf("%s has %d lines", a.Path, len(lines))
		}
		var out strings.Builder
		last := min(first-1+limit, len(lines))
		for i := first - 1; i < last; i++ {
			if out.Len() > maxRead {
				last = i
				break
			}
			fmt.Fprintf(&out, "%6d\t%s", i+1, strings.TrimSuffix(lines[i], "\n"))
			out.WriteByte('\n')
		}
		if last < len(lines) {
			fmt.Fprintf(&out, "[lines %d-%d of %d; read on with offset %d]\n", first, last, len(lines), last+1)
		}
		return out.String(), nil
	},
}

// WriteFile creates or replaces a file.
var WriteFile = &Tool{
	Name:        "write_file",
	Description: "Create a file, or replace one's whole content. Prefer edit_file to change part of a file.",
	Schema: schema(`{"type":"object","properties":{
		"path":{"type":"string"},
		"content":{"type":"string"}},
		"required":["path","content"]}`),
	Access: func(env Env, args json.RawMessage) (perm.Request, error) {
		var a struct{ Path string }
		if err := decode("write_file", args, &a); err != nil {
			return perm.Request{}, err
		}
		return pathAccess("write_file", perm.Write, env, a.Path)
	},
	Run: func(ctx context.Context, env Env, args json.RawMessage) (string, error) {
		var a struct{ Path, Content string }
		decode("write_file", args, &a)
		path, err := resolve(env, a.Path)
		if err != nil {
			return "", err
		}
		_, statErr := os.Stat(path)
		var before []byte
		if env.RecordChange != nil {
			before, err = os.ReadFile(path)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return "", fmt.Errorf("read %s before writing: %w", a.Path, err)
			}
			if err == nil && before == nil {
				before = []byte{}
			}
		}
		after := []byte(a.Content)
		if err := writeAtomic(path, after); err != nil {
			return "", err
		}
		if env.RecordChange != nil {
			if err := env.RecordChange(path, before, after); err != nil {
				return "", fmt.Errorf("file %s was changed, but recording change failed: %w", a.Path, err)
			}
		}
		verb := "Wrote"
		if errors.Is(statErr, fs.ErrNotExist) {
			verb = "Created"
		}
		return fmt.Sprintf("%s %s (%d lines)", verb, a.Path, strings.Count(a.Content, "\n")+1), nil
	},
}

// EditFile replaces text in a file.
var EditFile = &Tool{
	Name:        "edit_file",
	Description: "Replace old_text with new_text in a file. old_text must match exactly, whitespace included, and occur once unless replace_all is set. Read the file first.",
	Schema: schema(`{"type":"object","properties":{
		"path":{"type":"string"},
		"old_text":{"type":"string"},
		"new_text":{"type":"string"},
		"replace_all":{"type":"boolean"}},
		"required":["path","old_text","new_text"]}`),
	Access: func(env Env, args json.RawMessage) (perm.Request, error) {
		var a struct{ Path string }
		if err := decode("edit_file", args, &a); err != nil {
			return perm.Request{}, err
		}
		return pathAccess("edit_file", perm.Write, env, a.Path)
	},
	Run: func(ctx context.Context, env Env, args json.RawMessage) (string, error) {
		var a struct {
			Path       string
			OldText    string `json:"old_text"`
			NewText    string `json:"new_text"`
			ReplaceAll bool   `json:"replace_all"`
		}
		decode("edit_file", args, &a)
		if a.OldText == "" {
			return "", errors.New("old_text is empty; use write_file to create a file")
		}
		if a.OldText == a.NewText {
			return "", errors.New("old_text and new_text are the same")
		}
		path, err := resolve(env, a.Path)
		if err != nil {
			return "", err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		s := string(b)
		n := strings.Count(s, a.OldText)
		switch {
		case n == 0:
			return "", fmt.Errorf("old_text is not in %s; read the file and copy the text exactly", a.Path)
		case n > 1 && !a.ReplaceAll:
			return "", fmt.Errorf("old_text occurs %d times in %s; add context to make it unique, or set replace_all", n, a.Path)
		}
		if !a.ReplaceAll {
			n = 1
		}
		s = strings.Replace(s, a.OldText, a.NewText, n)
		after := []byte(s)
		if err := writeAtomic(path, after); err != nil {
			return "", err
		}
		if env.RecordChange != nil {
			if err := env.RecordChange(path, b, after); err != nil {
				return "", fmt.Errorf("file %s was changed, but recording change failed: %w", a.Path, err)
			}
		}
		return fmt.Sprintf("Edited %s (%d replacement%s)", a.Path, n, plural(n)), nil
	},
}

// ListDir lists a directory.
var ListDir = &Tool{
	Name:        "list_dir",
	Description: "List a directory's entries; directories end in /. The path is relative to the workspace; \".\" is its root.",
	Schema:      schema(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
	Access: func(env Env, args json.RawMessage) (perm.Request, error) {
		var a struct{ Path string }
		if err := decode("list_dir", args, &a); err != nil {
			return perm.Request{}, err
		}
		return pathAccess("list_dir", perm.Read, env, cmpOr(a.Path, "."))
	},
	Run: func(ctx context.Context, env Env, args json.RawMessage) (string, error) {
		var a struct{ Path string }
		decode("list_dir", args, &a)
		path, err := resolve(env, cmpOr(a.Path, "."))
		if err != nil {
			return "", err
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return "", err
		}
		var out strings.Builder
		for i, e := range entries {
			if i == 1000 {
				fmt.Fprintf(&out, "[%d more]\n", len(entries)-i)
				break
			}
			out.WriteString(e.Name())
			if e.IsDir() {
				out.WriteByte('/')
			}
			out.WriteByte('\n')
		}
		if len(entries) == 0 {
			return "(empty directory)", nil
		}
		return out.String(), nil
	},
}

// writeAtomic replaces a file through a temporary one, keeping its mode.
func writeAtomic(path string, data []byte) error {
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".talos-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
