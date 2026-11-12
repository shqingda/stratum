package tools

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/shqingda/talos/kernel/perm"
)

const maxRead = 256 << 10 // bytes read_file returns

// ReadFile reads a text file, with line numbers, a window at a time.
var ReadFile = fileTool(&Tool{
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
		if err := decode("read_file", args, &a); err != nil {
			return "", err
		}
		root, name, err := openFileParent(env, false)
		if err != nil {
			return "", err
		}
		defer root.Close()
		b, err := readFile(root, name)
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
})

// WriteFile creates or replaces a file.
var WriteFile = fileTool(&Tool{
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
		if err := decode("write_file", args, &a); err != nil {
			return "", err
		}
		root, name, err := openFileParent(env, true)
		if err != nil {
			return "", err
		}
		defer root.Close()
		_, statErr := root.Lstat(name)
		var before []byte
		if env.RecordChange != nil {
			before, err = readFile(root, name)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return "", fmt.Errorf("read %s before writing: %w", a.Path, err)
			}
			if err == nil && before == nil {
				before = []byte{}
			}
		}
		after := []byte(a.Content)
		if err := writeAtomic(root, name, after); err != nil {
			return "", err
		}
		if env.RecordChange != nil {
			if err := env.RecordChange(env.filePath, before, after); err != nil {
				return "", fmt.Errorf("file %s was changed, but recording change failed: %w", a.Path, err)
			}
		}
		verb := "Wrote"
		if errors.Is(statErr, fs.ErrNotExist) {
			verb = "Created"
		}
		return fmt.Sprintf("%s %s (%d lines)", verb, a.Path, strings.Count(a.Content, "\n")+1), nil
	},
})

// EditFile replaces text in a file.
var EditFile = fileTool(&Tool{
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
		if err := decode("edit_file", args, &a); err != nil {
			return "", err
		}
		if a.OldText == "" {
			return "", errors.New("old_text is empty; use write_file to create a file")
		}
		if a.OldText == a.NewText {
			return "", errors.New("old_text and new_text are the same")
		}
		root, name, err := openFileParent(env, false)
		if err != nil {
			return "", err
		}
		defer root.Close()
		b, err := readFile(root, name)
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
		if err := writeAtomic(root, name, after); err != nil {
			return "", err
		}
		if env.RecordChange != nil {
			if err := env.RecordChange(env.filePath, b, after); err != nil {
				return "", fmt.Errorf("file %s was changed, but recording change failed: %w", a.Path, err)
			}
		}
		return fmt.Sprintf("Edited %s (%d replacement%s)", a.Path, n, plural(n)), nil
	},
})

// ListDir lists a directory.
var ListDir = fileTool(&Tool{
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
		if err := decode("list_dir", args, &a); err != nil {
			return "", err
		}
		root, err := openDirectory(env.filePath, false)
		if err != nil {
			return "", err
		}
		defer root.Close()
		entries, err := fs.ReadDir(root.FS(), ".")
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
})

// writeAtomic replaces a file through a temporary one, keeping its mode.
func writeAtomic(root *os.Root, name string, data []byte) error {
	mode := os.FileMode(0o644)
	if fi, err := root.Lstat(name); err == nil {
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", name)
		}
		mode = fi.Mode().Perm()
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	tmp := ".talos-" + rand.Text()
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer root.Remove(tmp)
	_, err = f.Write(data)
	if err == nil {
		err = f.Chmod(mode)
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	return root.Rename(tmp, name)
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
