package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shqingda/stratum/kernel/perm"
	"github.com/shqingda/stratum/kernel/tools"
)

// TestMain is also the MCP server the tests start: the test binary run
// with STRATUM_TEST_MCP set serves an echo tool on stdio.
func TestMain(m *testing.M) {
	if os.Getenv("STRATUM_TEST_MCP") != "" {
		s := sdk.NewServer(&sdk.Implementation{Name: "echo", Version: "1"}, nil)
		type in struct {
			Text string `json:"text"`
		}
		sdk.AddTool(s, &sdk.Tool{Name: "echo", Description: "Says text back."}, func(ctx context.Context, r *sdk.CallToolRequest, a in) (*sdk.CallToolResult, any, error) {
			if a.Text == "fail" {
				return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: "it failed"}}}, nil, nil
			}
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "echo: " + a.Text}}}, nil, nil
		})
		s.AddResource(&sdk.Resource{URI: "note://today", Name: "today", Description: "A note"}, func(context.Context, *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
			return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{URI: "note://today", Text: "hello resource"}}}, nil
		})
		s.AddPrompt(&sdk.Prompt{
			Name:        "greet",
			Description: "Greet someone",
			Arguments:   []*sdk.PromptArgument{{Name: "who", Required: true}},
		}, func(_ context.Context, r *sdk.GetPromptRequest) (*sdk.GetPromptResult, error) {
			return &sdk.GetPromptResult{Messages: []*sdk.PromptMessage{
				{Role: "user", Content: &sdk.TextContent{Text: "hello " + r.Params.Arguments["who"]}},
			}}, nil
		})
		s.Run(context.Background(), &sdk.StdioTransport{})
		return
	}
	os.Exit(m.Run())
}

func writeConfig(t *testing.T, path string, names ...string) {
	t.Helper()
	servers := map[string]Server{}
	for _, n := range names {
		servers[n] = Server{Command: os.Args[0], Env: map[string]string{"STRATUM_TEST_MCP": "1"}}
	}
	b, _ := json.Marshal(map[string]any{"mcpServers": servers})
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHub(t *testing.T) {
	home, cwd := t.TempDir(), t.TempDir()
	writeConfig(t, filepath.Join(home, "mcp.json"), "echo")
	writeConfig(t, filepath.Join(cwd, ".mcp.json"), "repo", "echo")
	trusted := false
	h := &Hub{UserFile: filepath.Join(home, "mcp.json"), Trusted: func(string, Entry) bool { return trusted }}
	defer h.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	set := h.Tools(ctx, cwd)
	if len(set) != 1 || set[0].Name != "mcp__echo__echo" {
		t.Fatalf("tools %v", names(set))
	}
	tool := set[0]
	if req, _ := tool.Access(tools.Env{}, nil); req.Kind != perm.MCP || perm.Decide(perm.Auto, req, true) != perm.AskUser {
		t.Fatalf("access %+v", req)
	}
	out, err := tool.Run(ctx, tools.Env{}, json.RawMessage(`{"text":"hi"}`))
	if err != nil || out != "echo: hi" {
		t.Fatalf("%q %v", out, err)
	}
	if _, err := tool.Run(ctx, tools.Env{}, json.RawMessage(`{"text":"fail"}`)); err == nil || err.Error() != "it failed" {
		t.Fatalf("error %v", err)
	}

	sts, err := h.Status(cwd)
	if err != nil || len(sts) != 2 || sts[0].State != "ok" || sts[0].Tools[0] != "echo" || sts[1].Name != "repo" || sts[1].State != "untrusted" {
		t.Fatalf("%+v %v", sts, err)
	}
	trusted = true
	if set := h.Tools(ctx, cwd); len(set) != 2 {
		t.Fatalf("trusted tools %v", names(set))
	}
	got := h.Resources(cwd)
	if len(got) != 2 || got[0].Server != "echo" || got[0].URI != "note://today" || got[0].Name != "today" {
		t.Fatalf("resources %+v", got)
	}
	body, err := h.ReadResource(ctx, cwd, "echo", "note://today")
	if err != nil || body != "hello resource" {
		t.Fatalf("read %q %v", body, err)
	}
	prompts := h.Prompts(cwd)
	if len(prompts) != 2 || prompts[0].Server != "echo" || prompts[0].Name != "greet" || len(prompts[0].Arguments) != 1 || prompts[0].Arguments[0] != "who" {
		t.Fatalf("prompts %+v", prompts)
	}
	pr, err := h.GetPrompt(ctx, cwd, "echo", "greet", []string{"Stratum"})
	if err != nil || pr != "hello Stratum" {
		t.Fatalf("get prompt %q %v", pr, err)
	}
}

func TestLoadBadFile(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, "mcp.json"), []byte("{"), 0o644)
	if _, err := Load(filepath.Join(home, "mcp.json"), t.TempDir()); err == nil {
		t.Fatal("no error")
	}
}

func names(s tools.Set) []string {
	var out []string
	for _, t := range s {
		out = append(out, t.Name)
	}
	return out
}
