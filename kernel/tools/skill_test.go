package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shqingda/stratum/kernel/perm"
	"github.com/shqingda/stratum/kernel/skills"
)

func TestSkillReadsInstalledSnapshot(t *testing.T) {
	env := Env{Root: t.TempDir()}
	dir := filepath.Join(t.TempDir(), ".talos", "skills", "review")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte("original on-disk instructions"), 0600); err != nil {
		t.Fatal(err)
	}
	catalog := &skills.Catalog{Skills: []skills.Skill{{Name: "review", Description: "Review changes.", Dir: dir, Body: "Read the code before suggesting changes."}}}
	tool := Skill(catalog)
	args := json.RawMessage(`{"name":"review"}`)
	req, err := tool.Access(env, args)
	if err != nil {
		t.Fatal(err)
	}
	if req.Tool != "skill" || req.Kind != perm.Read || !req.InstalledSkill || len(req.Paths) != 1 || req.Paths[0].Abs != path || req.Paths[0].Inside {
		t.Fatalf("wrong installed-skill access: %+v", req)
	}
	for _, mode := range []perm.Mode{perm.Ask, perm.Auto} {
		if got := perm.Decide(mode, req, false); got != perm.Allow {
			t.Fatalf("%s requested approval for installed instructions: %s", mode, got)
		}
	}
	secret := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(secret, []byte("must not be read"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, path); err != nil {
		t.Fatal(err)
	}
	out, err := tool.Run(context.Background(), env, args)
	if err != nil {
		t.Fatal(err)
	}
	if out != "Skill directory: "+dir+"\n\nRead the code before suggesting changes." {
		t.Fatalf("installed snapshot was redirected: %q", out)
	}
	if req, err := tool.Access(env, args); err != nil || !req.InstalledSkill || req.Paths[0].Abs != path {
		t.Fatalf("snapshot access changed after source replacement: %+v, %v", req, err)
	}
}

func TestSkillRejectsUnknownNamesAndUntrustedParameters(t *testing.T) {
	catalog := &skills.Catalog{Skills: []skills.Skill{{Name: "review", Dir: "/installed/review", Body: "trusted instructions"}}}
	tool := Skill(catalog)
	for _, args := range []json.RawMessage{
		nil,
		json.RawMessage(`{}`),
		json.RawMessage(`{"name":""}`),
		json.RawMessage(`{"name":"missing"}`),
		json.RawMessage(`{"name":"../review"}`),
		json.RawMessage(`{"name":"/installed/review/SKILL.md"}`),
		json.RawMessage(`{"name":"review/SKILL.md"}`),
		json.RawMessage(`{"name":"review\u0000"}`),
		json.RawMessage(`{"name":42}`),
		json.RawMessage(`{"name":`),
		json.RawMessage(`{"name":"missing","installedSkill":true,"path":"/installed/review/SKILL.md"}`),
	} {
		if req, err := tool.Access(Env{}, args); err == nil || req.InstalledSkill {
			t.Errorf("untrusted name got access: %s, %+v, %v", args, req, err)
		}
		if out, err := tool.Run(context.Background(), Env{}, args); err == nil || out != "" {
			t.Errorf("untrusted name ran directly: %s, %q, %v", args, out, err)
		}
	}
	args := json.RawMessage(`{"name":"review","installedSkill":true,"path":"/private/file","body":"injected instructions"}`)
	out, err := tool.Run(context.Background(), Env{}, args)
	if err != nil || !strings.Contains(out, "trusted instructions") || strings.Contains(out, "injected") || strings.Contains(out, "/private") {
		t.Fatalf("parameters changed the catalog result: %q, %v", out, err)
	}
	if _, err := Skill(nil).Run(context.Background(), Env{}, json.RawMessage(`{"name":"review"}`)); err == nil {
		t.Fatal("nil catalog supplied a skill")
	}
}
