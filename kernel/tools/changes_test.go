package tools

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecordFileChanges(t *testing.T) {
	for _, tt := range []struct {
		name   string
		tool   *Tool
		before []byte
		after  string
		args   map[string]any
	}{
		{"create", WriteFile, nil, "after", map[string]any{"content": "after"}},
		{"create-empty", WriteFile, nil, "", map[string]any{"content": ""}},
		{"replace-empty", WriteFile, []byte{}, "after", map[string]any{"content": "after"}},
		{"truncate", WriteFile, []byte("before"), "", map[string]any{"content": ""}},
		{"replace", WriteFile, []byte("before"), "after", map[string]any{"content": "after"}},
		{"edit", EditFile, []byte("before before"), "after after", map[string]any{"old_text": "before", "new_text": "after", "replace_all": true}},
		{"edit-empty", EditFile, []byte("before"), "", map[string]any{"old_text": "before", "new_text": ""}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "file")
			if tt.before != nil {
				if err := os.WriteFile(path, tt.before, 0600); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			env := Env{Root: root, RecordChange: func(gotPath string, before, after []byte) error {
				calls++
				if gotPath != path || !bytes.Equal(before, tt.before) || (before == nil) != (tt.before == nil) {
					t.Errorf("wrong before snapshot: path %s, bytes %q, nil %v", gotPath, before, before == nil)
				}
				if after == nil || string(after) != tt.after {
					t.Errorf("wrong after snapshot: %q, nil %v", after, after == nil)
				}
				onDisk, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(onDisk, after) {
					t.Errorf("callback ran before the write: %q, %v", onDisk, err)
				}
				return nil
			}}
			tt.args["path"] = "file"
			if _, err := call(t, env, tt.tool, tt.args); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Errorf("record calls = %d, want 1", calls)
			}
			if tt.before != nil {
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatal("existing file permissions were not preserved")
				}
			}
		})
	}
}

func TestRecordChangeFailureReportsWrittenFile(t *testing.T) {
	for _, tool := range []*Tool{WriteFile, EditFile} {
		t.Run(tool.Name, func(t *testing.T) {
			env := Env{Root: t.TempDir()}
			path := filepath.Join(env.Root, "file")
			if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
				t.Fatal(err)
			}
			recordErr := errors.New("record unavailable")
			env.RecordChange = func(string, []byte, []byte) error { return recordErr }
			_, err := call(t, env, tool, map[string]any{"path": "file", "content": "after", "old_text": "before", "new_text": "after"})
			if !errors.Is(err, recordErr) || !strings.Contains(err.Error(), "was changed") || !strings.Contains(err.Error(), "recording change failed") {
				t.Fatalf("record failure did not explain completed write: %v", err)
			}
			if got, err := os.ReadFile(path); err != nil || string(got) != "after" {
				t.Fatalf("write was not retained: %q, %v", got, err)
			}
		})
	}
}

func TestRecordChangeReadFailurePreventsWrite(t *testing.T) {
	env := Env{Root: t.TempDir(), RecordChange: func(string, []byte, []byte) error {
		t.Fatal("record called after failed read")
		return nil
	}}
	path := filepath.Join(env.Root, "directory")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	_, err := call(t, env, WriteFile, map[string]any{"path": "directory", "content": "after"})
	if err == nil || !strings.Contains(err.Error(), "before writing") {
		t.Fatalf("write did not stop when before could not be read: %v", err)
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Fatal("target changed after failed before read")
	}
}

func TestRecordChangeFailedEditIsNotRecorded(t *testing.T) {
	env := Env{Root: t.TempDir(), RecordChange: func(string, []byte, []byte) error {
		t.Fatal("record called after failed edit")
		return nil
	}}
	path := filepath.Join(env.Root, "file")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := call(t, env, EditFile, map[string]any{"path": "file", "old_text": "absent", "new_text": "after"}); err == nil {
		t.Fatal("edit unexpectedly succeeded")
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "before" {
		t.Fatalf("failed edit changed file: %q, %v", got, err)
	}
}
