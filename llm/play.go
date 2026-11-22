package llm

import (
	"context"
	"errors"
	"iter"
	"strings"
	"time"
)

// Play is the provider of the demo environment (STRATUM_DEMO=1): it acts out
// a scene picked by words in the user's message, at a model's pace, with
// real tool calls on the demo repository, so every state of the app can be
// seen without an account. The scenes are written for scripts/demo.sh's
// calc repository, whose Add subtracts.
type Play struct {
	// Delay is the pause between chunks; zero streams at once.
	Delay time.Duration
}

// A scene's steps answer the requests of one turn in order: step i is
// the reply after i model replies since the user's message.
type scene struct {
	words []string
	steps []Turn
}

var scenes = []scene{
	// The first scene is also the default: fix the failing test.
	{words: []string{"修", "fix"}, steps: []Turn{
		{Thinking: "先看代码和测试，再跑一遍确认问题。", Calls: []ToolCall{
			Call("f1", "read_file", map[string]any{"path": "add.go"}),
			Call("f2", "read_file", map[string]any{"path": "add_test.go"}),
			Call("f3", "search", map[string]any{"pattern": "func Add"}),
		}},
		{Text: "先跑一下测试。", Calls: []ToolCall{Call("f4", "shell", map[string]any{"command": "go test ./..."})}},
		{Thinking: "Add 返回 a - b，测试期望 a + b。改一行就行。",
			Calls: []ToolCall{Call("f5", "edit_file", map[string]any{"path": "add.go", "old_text": "return a - b", "new_text": "return a + b"})}},
		{Calls: []ToolCall{Call("f6", "shell", map[string]any{"command": "go test ./..."})}},
		{Text: "修好了。`Add` 原来写成了 `a - b`，改成 `a + b` 后测试通过：\n\n```\nok  \tcalc\t0.2s\n```"},
	}},
	{words: []string{"出错", "error"}, steps: []Turn{
		{Thinking: "先看看测试。", Calls: []ToolCall{Call("e1", "shell", map[string]any{"command": "go test ./..."})}},
		{Text: "测试跑完了，接着改", Err: errors.New("demo: the provider closed the stream (HTTP 500)")},
	}},
	{words: []string{"问", "ask", "选"}, steps: []Turn{
		{Thinking: "加法有两种修法，要用户定。",
			Calls: []ToolCall{Call("q1", "ask_user", map[string]any{
				"question": "Add 想支持几个参数？",
				"options":  []string{"两个，保持现在的签名", "任意多个，改成 Add(nums ...int)"},
			})}},
		{Text: "好，按你选的来：{answer}。要我现在动手改吗？"},
	}},
	{words: []string{"依赖", "联网", "deps", "network"}, steps: []Turn{
		{Text: "要拉一个依赖，这需要联网，会先问你。",
			Calls: []ToolCall{Call("n1", "shell", map[string]any{"command": "go get golang.org/x/exp@latest", "unrestricted": true})}},
		{Text: "依赖处理完了。"},
	}},
	{words: []string{"子任务", "并行", "task", "parallel"}, steps: []Turn{
		{Thinking: "两块互不相干，分给两个子任务同时看。", Calls: []ToolCall{
			Call("t1", "task", map[string]any{"description": "找出所有算术函数", "prompt": "List every arithmetic function in this repository with its file."}),
			Call("t2", "task", map[string]any{"description": "检查测试覆盖", "prompt": "Say which functions in this repository have no test."}),
		}},
		{Text: "两个子任务都回来了：\n\n- 算术函数只有 `Add`，在 `add.go`。\n- `Add` 有测试，但测试现在是失败的。\n\n要我修 `Add` 吗？"},
	}},
	{words: []string{"计划", "plan"}, steps: []Turn{
		{Calls: []ToolCall{Call("p1", "update_plan", map[string]any{"title": "修好 calc", "steps": []map[string]string{
			{"step": "读代码和测试", "status": "in_progress"}, {"step": "修 Add", "status": "pending"}, {"step": "跑测试", "status": "pending"},
		}})}},
		{Calls: []ToolCall{Call("p2", "read_file", map[string]any{"path": "add.go"})}},
		{Calls: []ToolCall{Call("p3", "update_plan", map[string]any{"title": "修好 calc", "steps": []map[string]string{
			{"step": "读代码和测试", "status": "done"}, {"step": "修 Add", "status": "in_progress"}, {"step": "跑测试", "status": "pending"},
		}})}},
		{Text: "计划列好了，`Add` 写成了减法，下一步改它。"},
	}},
	{words: []string{"长", "long", "解释", "explain"}, steps: []Turn{
		{Thinking: "用户想要一份完整的说明，分几节写，带代码和表格。", Text: longAnswer},
	}},
	{words: []string{"跑测试", "测试", "test"}, steps: []Turn{
		{Calls: []ToolCall{Call("s1", "shell", map[string]any{"command": "go test ./..."})}},
		{Text: "`TestAdd` 失败：`Add(2, 3)` 得到 `-1`，应该是 `5`。`add.go` 里写成了 `a - b`。"},
	}},
}

// sub is what a sub-agent does: look around and report.
var sub = []Turn{
	{Calls: []ToolCall{Call("u1", "list_dir", map[string]any{"path": "."}), Call("u2", "search", map[string]any{"pattern": "^func "})}},
	{Text: "只有 `add.go` 里的 `Add(a, b int) int`，`add_test.go` 测了它。"},
}

const longAnswer = "## calc 是怎么组织的\n\n" +
	"这个仓库很小，只有一个包。\n\n" +
	"| 文件 | 内容 |\n|---|---|\n| `add.go` | `Add` 函数 |\n| `add_test.go` | `Add` 的表驱动测试 |\n| `go.mod` | 模块 `calc` |\n\n" +
	"### Add\n\n```go\nfunc Add(a, b int) int {\n\treturn a - b\n}\n```\n\n" +
	"这里有个错误：应该是 `a + b`。测试用三组输入检查它：\n\n" +
	"1. `Add(2, 3)` 应为 `5`\n2. `Add(-1, 1)` 应为 `0`\n3. `Add(0, 0)` 应为 `0`\n\n" +
	"第 2、3 组碰巧能过，第 1 组会失败。\n\n" +
	"### 怎么验证\n\n运行 `go test ./...`。修好后应该看到 `ok calc`。\n\n" +
	"> 表驱动测试加一组新输入只要加一行，比每组写一个函数省事。\n\n" +
	"还有什么想了解的？"

func (p Play) Stream(ctx context.Context, req Request) iter.Seq2[Event, error] {
	if isSummaryRequest(req) {
		t := Turn{Text: "演示模式摘要：已梳理项目结构、测试状态与后续修改计划，可继续基于当前上下文推进。"}
		return (&Script{Turns: []Turn{t}, Delay: p.Delay}).Stream(ctx, req)
	}
	steps, n, answer := sub, 0, ""
	if !strings.Contains(req.System, "You are a sub-agent") {
		steps = pick(req.Messages).steps
	}
	for i := len(req.Messages) - 1; i >= 0; i-- {
		m := req.Messages[i]
		if m.Role == Assistant {
			n++
			continue
		}
		if spoke(m) {
			break
		}
		for _, part := range m.Parts {
			if part.ToolResult != nil && answer == "" {
				answer = part.ToolResult.Content
			}
		}
	}
	t := steps[min(n, len(steps)-1)]
	if n >= len(steps) {
		t = Turn{Text: "这一段演完了。"}
	}
	t.Text = strings.ReplaceAll(t.Text, "{answer}", answer)
	return (&Script{Turns: []Turn{t}, Delay: p.Delay}).Stream(ctx, req)
}

// pick is the scene of the user's last message.
func pick(msgs []Message) scene {
	var said string
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == User && spoke(msgs[i]) {
			said = strings.ToLower(msgs[i].PlainText())
			break
		}
	}
	for _, s := range scenes {
		for _, w := range s.words {
			if strings.Contains(said, w) {
				return s
			}
		}
	}
	return scenes[0]
}

// spoke says whether a user message is the user's words, not tool results.
func spoke(m Message) bool {
	if m.Role != User {
		return false
	}
	for _, p := range m.Parts {
		if p.ToolResult != nil {
			return false
		}
	}
	return true
}

func isSummaryRequest(req Request) bool {
	if len(req.Tools) > 0 {
		return false
	}
	hasSummaryWord := func(s string) bool {
		l := strings.ToLower(s)
		return strings.Contains(l, "summary") || strings.Contains(l, "summarize")
	}
	if hasSummaryWord(req.System) {
		return true
	}
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == User {
			return hasSummaryWord(req.Messages[i].PlainText())
		}
	}
	return false
}
