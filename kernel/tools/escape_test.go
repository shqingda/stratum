package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/shqingda/stratum/kernel/perm"
)

func escapeDirs(t *testing.T) (Env, string) {
	t.Helper()
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root, outside := filepath.Join(parent, "workspace"), filepath.Join(parent, "outside")
	for _, dir := range []string{root, outside} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	return Env{Root: root}, outside
}

func escapeArgs(t *testing.T, path string) json.RawMessage {
	t.Helper()
	args, err := json.Marshal(map[string]string{"path": path, "content": "after", "old_text": "before", "new_text": "after"})
	if err != nil {
		t.Fatal(err)
	}
	return args
}

func escapeLink(t *testing.T, target, path string) {
	t.Helper()
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

func escapeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestFileToolsEscapeAccess(t *testing.T) {
	for _, tool := range []*Tool{WriteFile, EditFile} {
		for _, route := range []string{"file-link", "directory-link", "link-chain", "parent", "absolute"} {
			t.Run(tool.Name+"/"+route, func(t *testing.T) {
				tool := Set{tool}.Get(tool.Name)
				env, outside := escapeDirs(t)
				target := filepath.Join(outside, "file")
				escapeFile(t, target, "before")
				var path string
				switch route {
				case "file-link":
					escapeLink(t, target, filepath.Join(env.Root, "link"))
					path = "link"
				case "directory-link":
					escapeLink(t, outside, filepath.Join(env.Root, "link"))
					path = "link/file"
				case "link-chain":
					escapeLink(t, outside, filepath.Join(env.Root, "first"))
					escapeLink(t, "first", filepath.Join(env.Root, "second"))
					path = "second/file"
				case "parent":
					path = "../outside/file"
				case "absolute":
					path = target
				}
				args := escapeArgs(t, path)
				req, err := tool.Access(env, args)
				if err != nil {
					t.Fatal(err)
				}
				if len(req.Paths) != 1 || req.Paths[0].Inside || req.Paths[0].Abs != target || req.Kind != perm.Write {
					t.Fatalf("external access misclassified: %+v", req)
				}
				for _, mode := range []perm.Mode{perm.Ask, perm.Auto} {
					if got := perm.Decide(mode, req, false); got != perm.AskUser {
						t.Fatalf("%s: decision %s, want approval", mode, got)
					}
				}
				// An approved external write remains a supported operation.
				if _, err := tool.Run(context.Background(), env, args); err != nil {
					t.Fatal(err)
				}
				if got, err := os.ReadFile(target); err != nil || string(got) != "after" {
					t.Fatalf("approved external write: %q, %v", got, err)
				}
			})
		}
	}
}

func TestFileToolsDanglingSymlinkAccess(t *testing.T) {
	for _, tool := range []*Tool{WriteFile, EditFile} {
		for _, route := range []string{"file-link", "directory-link", "link-chain"} {
			t.Run(tool.Name+"/"+route, func(t *testing.T) {
				tool := Set{tool}.Get(tool.Name)
				env, outside := escapeDirs(t)
				target := filepath.Join(outside, "missing")
				escapeLink(t, target, filepath.Join(env.Root, "first"))
				path := "first"
				if route != "file-link" {
					target = filepath.Join(target, "file")
					path += "/file"
				}
				if route == "link-chain" {
					escapeLink(t, "first", filepath.Join(env.Root, "second"))
					path = "second/file"
				}
				req, err := tool.Access(env, escapeArgs(t, path))
				if err != nil {
					t.Fatal(err)
				}
				if len(req.Paths) != 1 || req.Paths[0].Inside || req.Paths[0].Abs != target {
					t.Fatalf("dangling external link misclassified: %+v", req)
				}
				for _, mode := range []perm.Mode{perm.Ask, perm.Auto} {
					if got := perm.Decide(mode, req, false); got != perm.AskUser {
						t.Fatalf("%s: dangling link needs approval, got %s", mode, got)
					}
				}
			})
		}
	}
}

func TestFileToolsEscapeTOCTOU(t *testing.T) {
	for _, tool := range []*Tool{WriteFile, EditFile} {
		for _, route := range []string{"file-link", "directory-link"} {
			t.Run(tool.Name+"/"+route, func(t *testing.T) {
				tool := Set{tool}.Get(tool.Name)
				env, outside := escapeDirs(t)
				inside := filepath.Join(env.Root, "inside")
				if err := os.Mkdir(inside, 0700); err != nil {
					t.Fatal(err)
				}
				internalFile, externalFile := filepath.Join(inside, "file"), filepath.Join(outside, "file")
				escapeFile(t, internalFile, "before")
				escapeFile(t, externalFile, "before")
				initial, changed, path := internalFile, externalFile, "link"
				if route == "directory-link" {
					initial, changed, path = inside, outside, "link/file"
				}
				link := filepath.Join(env.Root, "link")
				escapeLink(t, initial, link)
				args := escapeArgs(t, path)
				req, err := tool.Access(env, args)
				if err != nil {
					t.Fatal(err)
				}
				if len(req.Paths) != 1 || !req.Paths[0].Inside || perm.Decide(perm.Auto, req, false) != perm.Allow {
					t.Fatalf("initial internal access was not allowed: %+v", req)
				}
				if err := os.Remove(link); err != nil {
					t.Fatal(err)
				}
				escapeLink(t, changed, link)
				_, runErr := tool.Run(context.Background(), env, args)
				if got, err := os.ReadFile(externalFile); err != nil || string(got) != "before" {
					t.Fatalf("link swap escaped the approved workspace: outside = %q, read error = %v, run error = %v", got, err, runErr)
				}
				if runErr == nil {
					if got, err := os.ReadFile(internalFile); err != nil || string(got) != "after" {
						t.Fatalf("successful write did not use approved internal target: %q, %v", got, err)
					}
				}
			})
		}
	}
}

func TestFileToolsSensitiveAccess(t *testing.T) {
	env, _ := escapeDirs(t)
	for _, tool := range []*Tool{ReadFile, WriteFile, EditFile} {
		for _, path := range []string{".env", ".env.local", ".ssh/id_ed25519", ".git/config", ".aws/config", ".gnupg/config", ".stratum/config", ".talos/config", ".npmrc", ".netrc", "secrets.json", "credential.json", "api_token.txt", "password.txt", "server.pem", "server.key", "identity.p12"} {
			t.Run(tool.Name+"/"+path, func(t *testing.T) {
				tool := Set{tool}.Get(tool.Name)
				req, err := tool.Access(env, escapeArgs(t, path))
				if err != nil {
					t.Fatal(err)
				}
				if len(req.Paths) != 1 || !req.Paths[0].Inside {
					t.Fatalf("sensitive workspace path misclassified: %+v", req)
				}
				for _, mode := range []perm.Mode{perm.Ask, perm.Auto, perm.Full} {
					want := perm.AskUser
					if mode == perm.Full {
						want = perm.Allow
					}
					if got := perm.Decide(mode, req, false); got != want {
						t.Errorf("%s: got %s, want %s", mode, got, want)
					}
				}
			})
		}
	}
}
