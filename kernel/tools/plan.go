package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shqingda/talos/kernel/perm"
)

// PlanStep is a step of the plan the model keeps; the app reads the last
// update_plan call's arguments to show it.
type PlanStep struct {
	Step   string `json:"step"`
	Status string `json:"status"` // pending, in_progress, done
	Note   string `json:"note,omitempty"`
}

// UpdatePlan replaces the plan of the task at hand. It touches nothing.
var UpdatePlan = &Tool{
	Name: "update_plan",
	Description: "Keep a plan for a task of several steps: send the whole list each time a step starts or finishes, " +
		"one step in_progress at a time. Skip it for small tasks. note says briefly what blocks or changed a step.",
	Schema: schema(`{"type":"object","properties":{
		"title":{"type":"string"},
		"steps":{"type":"array","items":{"type":"object","properties":{
			"step":{"type":"string"},
			"status":{"type":"string","enum":["pending","in_progress","done"]},
			"note":{"type":"string"}},"required":["step","status"]}}},
		"required":["steps"]}`),
	Access: func(env Env, args json.RawMessage) (perm.Request, error) {
		return perm.Request{Tool: "update_plan", Kind: perm.Read}, nil
	},
	Run: func(ctx context.Context, env Env, args json.RawMessage) (string, error) {
		var a struct {
			Steps []PlanStep
		}
		if err := decode("update_plan", args, &a); err != nil {
			return "", err
		}
		if len(a.Steps) == 0 {
			return "", fmt.Errorf("update_plan: steps is required")
		}
		done := 0
		for _, s := range a.Steps {
			switch s.Status {
			case "done":
				done++
			case "pending", "in_progress":
			default:
				return "", fmt.Errorf("update_plan: bad status %q", s.Status)
			}
			if strings.TrimSpace(s.Step) == "" {
				return "", fmt.Errorf("update_plan: a step is empty")
			}
		}
		return fmt.Sprintf("Plan updated: %d of %d steps done.", done, len(a.Steps)), nil
	},
}
