package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/shqingda/stratum/kernel/perm"
	"github.com/shqingda/stratum/kernel/skills"
	"github.com/shqingda/stratum/kernel/workspace"
)

// Skill reads only the installed skill snapshot supplied for this turn.
func Skill(catalog *skills.Catalog) *Tool {
	if catalog == nil {
		catalog = &skills.Catalog{}
	}
	find := func(args json.RawMessage) (skills.Skill, error) {
		var a struct{ Name string }
		if err := decode("skill", args, &a); err != nil {
			return skills.Skill{}, err
		}
		s, ok := catalog.Find(a.Name)
		if !ok {
			return skills.Skill{}, fmt.Errorf("skill: no installed skill %q", a.Name)
		}
		return s, nil
	}
	return &Tool{
		Name:        "skill",
		Description: "Read an installed skill's instructions by its exact name.",
		Schema:      schema(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`),
		Access: func(env Env, args json.RawMessage) (perm.Request, error) {
			s, err := find(args)
			if err != nil {
				return perm.Request{}, err
			}
			path := workspace.Path{Abs: filepath.Join(s.Dir, "SKILL.md")}
			if rel, err := filepath.Rel(env.Root, path.Abs); err == nil && filepath.IsLocal(rel) {
				path.Inside, path.Rel = true, rel
			}
			return perm.Request{Tool: "skill", Kind: perm.Read, Paths: []workspace.Path{path}, InstalledSkill: true}, nil
		},
		Run: func(ctx context.Context, env Env, args json.RawMessage) (string, error) {
			s, err := find(args)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Skill directory: %s\n\n%s", s.Dir, s.Body), nil
		},
	}
}
