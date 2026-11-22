package perm

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shqingda/stratum/kernel/workspace"
)

func TestResolveAndDecide(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.go"), nil, 0o644)
	os.Symlink(outside, filepath.Join(root, "escape"))

	res := func(p string) workspace.Path {
		r, err := workspace.Resolve(root, p)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if p := res("a.go"); !p.Inside || p.Rel != "a.go" {
		t.Errorf("%+v", p)
	}
	if p := res("new/dir/file.txt"); !p.Inside {
		t.Errorf("a new file is inside: %+v", p)
	}
	for _, p := range []string{"escape/x", "../x", "/etc/passwd", "escape/../../x"} {
		if res(p).Inside {
			t.Errorf("%s resolved inside", p)
		}
	}
	if !workspace.Sensitive(res(".env").Abs) || !workspace.Sensitive(res(".git/config").Abs) || workspace.Sensitive(res("main.go").Abs) {
		t.Error("sensitive paths")
	}

	cases := []struct {
		mode Mode
		req  Request
		want Decision
	}{
		{Ask, Request{Kind: Read, Paths: []workspace.Path{res("a.go")}}, Allow},
		{Ask, Request{Kind: Write, Paths: []workspace.Path{res("a.go")}}, AskUser},
		{Auto, Request{Kind: Write, Paths: []workspace.Path{res("a.go")}}, Allow},
		{Auto, Request{Kind: Write, Paths: []workspace.Path{res(".env")}}, AskUser},
		{Auto, Request{Kind: Read, Paths: []workspace.Path{res("escape/x")}}, AskUser},
		{Full, Request{Kind: Write, Paths: []workspace.Path{res("/etc/hosts")}}, Allow},
		{Ask, Request{Kind: Exec, Command: "git status"}, Allow},
		{Ask, Request{Kind: Exec, Command: "ls -la src"}, Allow},
		{Ask, Request{Kind: Exec, Command: "ls; rm -rf ."}, AskUser},
		{Ask, Request{Kind: Exec, Command: "go test ./..."}, AskUser},
		{Auto, Request{Kind: Exec, Command: "go test ./..."}, Allow},
		{Auto, Request{Kind: Exec, Command: "curl x", Unrestricted: true}, AskUser},
	}
	for _, c := range cases {
		if got := Decide(c.mode, c.req, true); got != c.want {
			t.Errorf("%s %+v: %s, want %s", c.mode, c.req, got, c.want)
		}
	}
	if Decide(Auto, Request{Kind: Exec, Command: "go test"}, false) != AskUser {
		t.Error("ran unsandboxed without asking")
	}
}
