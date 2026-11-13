package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/shqingda/talos/kernel/perm"
)

// Task hands work to a sub-agent; the session runs it.
var Task = &Tool{
	Name: "task",
	Description: "Hand a self-contained piece of work to a sub-agent that has your tools: a wide search of the codebase, reading many files to answer a question, " +
		"or one part of a change that does not depend on the others. It knows nothing of this conversation, so the prompt must say all it needs. " +
		"You get back only its final report. Several task calls in one reply run at the same time.",
	Schema: schema(`{"type":"object","properties":{
		"description":{"type":"string","description":"A few words on what it does, shown to the user."},
		"prompt":{"type":"string","description":"The whole task."}},
		"required":["description","prompt"]}`),
	Access: func(env Env, args json.RawMessage) (perm.Request, error) {
		return perm.Request{Tool: "task", Kind: perm.Read}, nil
	},
	Run: func(ctx context.Context, env Env, args json.RawMessage) (string, error) {
		return "", fmt.Errorf("task is run by the session")
	},
}

// Without is s less the tools named.
func (s Set) Without(names ...string) Set {
	var out Set
	for _, t := range s {
		if !slicesContains(names, t.Name) {
			out = append(out, t)
		}
	}
	return out
}

func slicesContains(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}
