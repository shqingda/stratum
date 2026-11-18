package session

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/shqingda/talos/llm"
	"github.com/shqingda/talos/proto"
)

const (
	maxTasks    = 8
	maxParallel = 4
)

type graphTask struct {
	ID, Description, Prompt, Agent string
	After                          []string
}

// parseGraph reads run_tasks' arguments and checks they make a graph:
// ids unique, afters known, no cycle.
func parseGraph(args json.RawMessage) ([]graphTask, error) {
	var a struct{ Tasks []graphTask }
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("run_tasks: %v", err)
	}
	if len(a.Tasks) == 0 || len(a.Tasks) > maxTasks {
		return nil, fmt.Errorf("run_tasks: give 1 to %d tasks", maxTasks)
	}
	ids := map[string]bool{}
	for _, t := range a.Tasks {
		if strings.TrimSpace(t.ID) == "" || strings.TrimSpace(t.Prompt) == "" {
			return nil, fmt.Errorf("run_tasks: every task needs an id and a prompt")
		}
		if ids[t.ID] {
			return nil, fmt.Errorf("run_tasks: id %q is used twice", t.ID)
		}
		ids[t.ID] = true
	}
	for _, t := range a.Tasks {
		for _, d := range t.After {
			if !ids[d] {
				return nil, fmt.Errorf("run_tasks: %q is after %q, which is not a task", t.ID, d)
			}
		}
	}
	// Kahn: a cycle leaves tasks that never become ready.
	left := slices.Clone(a.Tasks)
	done := map[string]bool{}
	for len(left) > 0 {
		n := len(left)
		left = slices.DeleteFunc(left, func(t graphTask) bool {
			if !all(t.After, done) {
				return false
			}
			done[t.ID] = true
			return true
		})
		if len(left) == n {
			return nil, fmt.Errorf("run_tasks: the tasks wait on each other in a cycle")
		}
	}
	return a.Tasks, nil
}

func all(ids []string, set map[string]bool) bool {
	for _, id := range ids {
		if !set[id] {
			return false
		}
	}
	return true
}

// graph runs a run_tasks call: each task once its after-tasks are done,
// at most maxParallel at a time.
func (r *runner) graph(ctx context.Context, call llm.ToolCall) (llm.ToolResult, error) {
	tasks, err := parseGraph(call.Args)
	if err != nil {
		return llm.ToolResult{Content: err.Error(), IsError: true}, nil
	}
	state := map[string]string{}
	reports := map[string]string{}
	set := func(id, s string) {
		state[id] = s
		r.m.stream(r.sessionID, proto.EventTaskState, proto.TaskState{CallID: call.ID, Task: id, State: s})
	}
	type result struct {
		id, report string
		err        error
	}
	finished := make(chan result)
	running := 0
	for {
		for _, t := range tasks {
			if state[t.ID] != "" || running == maxParallel || ctx.Err() != nil {
				continue
			}
			if slices.ContainsFunc(t.After, func(d string) bool { return state[d] == "fail" || state[d] == "skip" }) {
				set(t.ID, "skip")
				continue
			}
			if !slices.ContainsFunc(t.After, func(d string) bool { return state[d] != "done" }) {
				prompt := t.Prompt
				for _, d := range t.After {
					prompt += "\n\nReport of task " + d + ", which this one comes after:\n" + reports[d]
				}
				set(t.ID, "run")
				running++
				go func(id, prompt string) {
					report, err := r.task(ctx, call.ID, id, prompt, t.Agent)
					finished <- result{id, report, err}
				}(t.ID, prompt)
			}
		}
		if running == 0 {
			break
		}
		res := <-finished
		running--
		reports[res.id] = res.report
		switch {
		case ctx.Err() != nil:
		case res.err != nil:
			reports[res.id] = "failed: " + res.err.Error()
			set(res.id, "fail")
		default:
			set(res.id, "done")
		}
	}
	if err := ctx.Err(); err != nil {
		return llm.ToolResult{}, err
	}
	var b strings.Builder
	for _, t := range tasks {
		fmt.Fprintf(&b, "## %s (%s)\n", t.ID, state[t.ID])
		switch state[t.ID] {
		case "skip":
			b.WriteString("Skipped: a task it comes after did not finish.\n\n")
		default:
			b.WriteString(reports[t.ID] + "\n\n")
		}
	}
	return llm.ToolResult{Content: strings.TrimSpace(b.String())}, nil
}
