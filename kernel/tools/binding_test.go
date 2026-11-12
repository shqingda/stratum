package tools

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/shqingda/talos/kernel/perm"
)

func bindingContents(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("%s: got %q, error %v; want %q", path, got, err, want)
	}
}

func TestFileToolInvocationGuardrails(t *testing.T) {
	for _, template := range []*Tool{ReadFile, WriteFile, EditFile, ListDir} {
		for _, misuse := range []string{"template", "no-access", "args-content", "args-path", "root", "sandbox", "failed-access", "repeated-access", "canceled", "repeated-run"} {
			t.Run(template.Name+"/"+misuse, func(t *testing.T) {
				env, outside := escapeDirs(t)
				data := filepath.Join(env.Root, "data")
				evil, external := filepath.Join(env.Root, "evil"), filepath.Join(outside, "data")
				for _, path := range []*string{&data, &evil, &external} {
					if template == ListDir {
						if err := os.Mkdir(*path, 0700); err != nil {
							t.Fatal(err)
						}
						*path = filepath.Join(*path, "file")
					}
					escapeFile(t, *path, "before")
				}
				args := escapeArgs(t, "data")
				tool := Set{template}.Get(template.Name)
				if misuse == "template" {
					tool = template
				}
				if misuse != "no-access" {
					if _, err := tool.Access(env, args); err != nil {
						t.Fatal(err)
					}
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				runEnv := env
				want := "before"
				switch misuse {
				case "args-content":
					// Mutate the original backing array: Access must own its snapshot.
					copy(args, bytes.ReplaceAll(args, []byte("after"), []byte("rogue")))
				case "args-path":
					copy(args, bytes.ReplaceAll(args, []byte("data"), []byte("evil")))
				case "root":
					runEnv.Root = outside
				case "sandbox":
					runEnv.Sandboxed = !env.Sandboxed
				case "failed-access":
					if _, err := tool.Access(env, []byte(`{"path":`)); err == nil {
						t.Fatal("invalid Access succeeded")
					}
				case "repeated-access":
					if _, err := tool.Access(env, args); err == nil {
						t.Fatal("repeated Access succeeded")
					}
				case "canceled":
					cancel()
				case "repeated-run":
					if _, err := tool.Run(ctx, env, args); err != nil {
						t.Fatal(err)
					}
					if template == WriteFile || template == EditFile {
						want = "after"
					}
				}
				if out, err := tool.Run(ctx, runEnv, args); err == nil {
					t.Fatalf("%s executed: %q", misuse, out)
				}
				bindingContents(t, data, want)
				bindingContents(t, evil, "before")
				bindingContents(t, external, "before")
			})
		}
	}
}

func TestFileToolInvocationsAreIsolated(t *testing.T) {
	for _, template := range []*Tool{WriteFile, EditFile} {
		t.Run(template.Name, func(t *testing.T) {
			env, _ := escapeDirs(t)
			first, second := filepath.Join(env.Root, "first"), filepath.Join(env.Root, "second")
			escapeFile(t, first, "before")
			escapeFile(t, second, "before")
			tools := []*Tool{Set{template}.Get(template.Name), Set{template}.Get(template.Name)}
			args := [][]byte{escapeArgs(t, "first"), escapeArgs(t, "second")}
			args[1] = bytes.ReplaceAll(args[1], []byte("after"), []byte("other"))
			var ready sync.WaitGroup
			ready.Add(2)
			errs := make(chan error, 2)
			for i, tool := range tools {
				go func() {
					_, err := tool.Access(env, args[i])
					ready.Done()
					ready.Wait()
					if err == nil {
						_, err = tool.Run(context.Background(), env, args[i])
					}
					errs <- err
				}()
			}
			for range tools {
				if err := <-errs; err != nil {
					t.Error(err)
				}
			}
			bindingContents(t, first, "after")
			bindingContents(t, second, "other")
		})
	}
}

func TestWriteFileRejectsMalformedContent(t *testing.T) {
	env, _ := escapeDirs(t)
	path := filepath.Join(env.Root, "file")
	escapeFile(t, path, "before")
	tool := Set{WriteFile}.Get(WriteFile.Name)
	args := []byte(`{"path":"file","content":42}`)
	if _, err := tool.Access(env, args); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Run(context.Background(), env, args); err == nil {
		t.Fatal("invalid content silently replaced the file with empty text")
	}
	bindingContents(t, path, "before")
}

func TestFileToolsCanonicalTargetSwap(t *testing.T) {
	for _, template := range []*Tool{WriteFile, EditFile} {
		for _, component := range []string{"parent", "leaf"} {
			for _, destination := range []string{"outside", "sensitive"} {
				t.Run(template.Name+"/"+component+"/"+destination, func(t *testing.T) {
					env, outside := escapeDirs(t)
					parent := filepath.Join(env.Root, "data")
					if err := os.Mkdir(parent, 0700); err != nil {
						t.Fatal(err)
					}
					targetDir := outside
					if destination == "sensitive" {
						targetDir = filepath.Join(env.Root, ".git")
						if err := os.Mkdir(targetDir, 0700); err != nil {
							t.Fatal(err)
						}
					}
					approved := filepath.Join(parent, "file")
					unapproved := filepath.Join(targetDir, "file")
					escapeFile(t, approved, "before")
					escapeFile(t, unapproved, "before")
					tool := Set{template}.Get(template.Name)
					args := escapeArgs(t, "data/file")
					req, err := tool.Access(env, args)
					if err != nil || perm.Decide(perm.Auto, req, false) != perm.Allow {
						t.Fatalf("initial ordinary file should be allowed: %+v, %v", req, err)
					}
					link, target := approved, unapproved
					if component == "parent" {
						moved := filepath.Join(env.Root, "old-data")
						if err := os.Rename(parent, moved); err != nil {
							t.Fatal(err)
						}
						approved = filepath.Join(moved, "file")
						link, target = parent, targetDir
					} else if err := os.Remove(link); err != nil {
						t.Fatal(err)
					}
					// Relative links also exercise redirects within the same workspace.
					rel, err := filepath.Rel(filepath.Dir(link), target)
					if err != nil {
						t.Fatal(err)
					}
					escapeLink(t, rel, link)
					_, runErr := tool.Run(context.Background(), env, args)
					bindingContents(t, unapproved, "before")
					if runErr == nil {
						bindingContents(t, approved, "after")
					}
				})
			}
		}
	}
}

func TestFileToolsKeepApprovedExternalTarget(t *testing.T) {
	for _, template := range []*Tool{WriteFile, EditFile} {
		t.Run(template.Name, func(t *testing.T) {
			env, outside := escapeDirs(t)
			approved, other := filepath.Join(outside, "approved"), filepath.Join(outside, "other")
			escapeFile(t, approved, "before")
			escapeFile(t, other, "before")
			link := filepath.Join(env.Root, "link")
			escapeLink(t, approved, link)
			tool := Set{template}.Get(template.Name)
			args := escapeArgs(t, "link")
			req, err := tool.Access(env, args)
			if err != nil || len(req.Paths) != 1 || req.Paths[0].Abs != approved || perm.Decide(perm.Auto, req, false) != perm.AskUser {
				t.Fatalf("external target should need approval: %+v, %v", req, err)
			}
			if err := os.Remove(link); err != nil {
				t.Fatal(err)
			}
			escapeLink(t, other, link)
			if _, err := tool.Run(context.Background(), env, args); err != nil {
				t.Fatal(err)
			}
			bindingContents(t, approved, "after")
			bindingContents(t, other, "before")
		})
	}
}

func TestReadToolsDoNotFollowSensitiveSwap(t *testing.T) {
	for _, template := range []*Tool{ReadFile, ListDir} {
		t.Run(template.Name, func(t *testing.T) {
			env, _ := escapeDirs(t)
			ordinary, sensitive := filepath.Join(env.Root, "data"), filepath.Join(env.Root, ".git")
			for _, dir := range []string{ordinary, sensitive} {
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			escapeFile(t, filepath.Join(ordinary, "file"), "allowed-content")
			escapeFile(t, filepath.Join(ordinary, "allowed-entry"), "")
			escapeFile(t, filepath.Join(sensitive, "file"), "private-content")
			escapeFile(t, filepath.Join(sensitive, "private-entry"), "")
			path, want := "data/file", "allowed-content"
			if template == ListDir {
				path, want = "data", "allowed-entry"
			}
			args := escapeArgs(t, path)
			tool := Set{template}.Get(template.Name)
			req, err := tool.Access(env, args)
			if err != nil || perm.Decide(perm.Auto, req, false) != perm.Allow {
				t.Fatalf("ordinary read should be allowed: %+v, %v", req, err)
			}
			if err := os.Rename(ordinary, filepath.Join(env.Root, "old-data")); err != nil {
				t.Fatal(err)
			}
			escapeLink(t, ".git", ordinary)
			out, err := tool.Run(context.Background(), env, args)
			if strings.Contains(out, "private-") || (err == nil && !strings.Contains(out, want)) {
				t.Fatalf("read escaped approved target: output %q, error %v", out, err)
			}
		})
	}
}

func TestWriteFileCreatesApprovedMissingParents(t *testing.T) {
	for _, route := range []string{"inside", "outside", "dangling-link"} {
		t.Run(route, func(t *testing.T) {
			env, outside := escapeDirs(t)
			path := "new/deep/file"
			target := filepath.Join(env.Root, path)
			if route != "inside" {
				target = filepath.Join(outside, path)
				path = target
			}
			if route == "dangling-link" {
				escapeLink(t, filepath.Dir(target), filepath.Join(env.Root, "link"))
				path = "link/file"
			}
			tool := Set{WriteFile}.Get(WriteFile.Name)
			args := escapeArgs(t, path)
			req, err := tool.Access(env, args)
			if err != nil || len(req.Paths) != 1 || req.Paths[0].Abs != target || req.Paths[0].Inside != (route == "inside") {
				t.Fatalf("wrong new-file access: %+v, %v", req, err)
			}
			want := perm.AskUser
			if route == "inside" {
				want = perm.Allow
			}
			if decision := perm.Decide(perm.Auto, req, false); decision != want {
				t.Fatalf("decision %s, want %s", decision, want)
			}
			if _, err := os.Stat(filepath.Dir(target)); !os.IsNotExist(err) {
				t.Fatalf("Access created unapproved parents: %v", err)
			}
			if _, err := tool.Run(context.Background(), env, args); err != nil {
				t.Fatal(err)
			}
			bindingContents(t, target, "after")
		})
	}
}
