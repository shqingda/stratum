package perm

import (
	"encoding/json"
	"testing"

	"github.com/shqingda/talos/kernel/workspace"
)

func TestInstalledSkillPermissionIsNarrow(t *testing.T) {
	outside := workspace.Path{Abs: "/home/example/.talos/skills/review/SKILL.md"}
	inside := workspace.Path{Abs: "/workspace/.talos/skills/review/SKILL.md", Inside: true, Rel: ".talos/skills/review/SKILL.md"}
	for _, tt := range []struct {
		name string
		req  Request
		want Decision
	}{
		{"global-snapshot", Request{Tool: "skill", Kind: Read, Paths: []workspace.Path{outside}, InstalledSkill: true}, Allow},
		{"workspace-snapshot", Request{Tool: "skill", Kind: Read, Paths: []workspace.Path{inside}, InstalledSkill: true}, Allow},
		{"untrusted-skill", Request{Tool: "skill", Kind: Read, Paths: []workspace.Path{outside}}, AskUser},
		{"ordinary-read", Request{Tool: "read_file", Kind: Read, Paths: []workspace.Path{outside}, InstalledSkill: true}, AskUser},
		{"sensitive-read", Request{Tool: "read_file", Kind: Read, Paths: []workspace.Path{inside}, InstalledSkill: true}, AskUser},
		{"write-not-exempt", Request{Tool: "skill", Kind: Write, Paths: []workspace.Path{outside}, InstalledSkill: true}, AskUser},
		{"exec-not-exempt", Request{Tool: "skill", Kind: Exec, Paths: []workspace.Path{outside}, InstalledSkill: true}, AskUser},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, mode := range []Mode{Ask, Auto} {
				if got := Decide(mode, tt.req, true); got != tt.want {
					t.Errorf("%s: got %s, want %s", mode, got, tt.want)
				}
			}
			if got := Decide(Full, tt.req, true); got != Allow {
				t.Errorf("full access changed: %s", got)
			}
		})
	}
	var injected Request
	if err := json.Unmarshal([]byte(`{"Tool":"skill","Kind":"read","InstalledSkill":true}`), &injected); err != nil {
		t.Fatal(err)
	}
	if injected.InstalledSkill {
		t.Fatal("serialized parameters granted the installed-skill marker")
	}
}
