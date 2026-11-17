package app

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/proto"
)

func TestMCPPage(t *testing.T) {
	a := working()
	a.out = make(chan []proto.Command, 16)
	a.settingsOpen, a.settingsPage = true, "mcp"
	a.C.MCPFor = "/Users/me/Projects/calc"
	a.C.MCP = &proto.MCPList{Servers: []proto.MCPServer{
		{Name: "github", Target: "github-mcp-server stdio", State: "ok", Tools: []string{"create_issue", "list_prs", "get_file"}},
		{Name: "linear", Target: "https://mcp.linear.app/mcp", State: "fail", Error: "401 Unauthorized"},
		{Name: "calc-db", Project: true, Target: "./scripts/db-mcp", State: "untrusted"},
	}}
	tt := ui.NewTester(a.View, 1200, 780)
	tt.Frame()
	for _, s := range []string{"3 个工具", "连接失败：401 Unauthorized", "./scripts/db-mcp", "项目"} {
		if !tt.HasText(s) {
			t.Fatalf("%q not shown", s)
		}
	}
	snapshot(t, tt, "settings-mcp")
	if err := tt.Click("启用"); err != nil {
		t.Fatal(err)
	}
	cmd := sent(a, proto.MethodMCPTrust)
	var tr proto.MCPTrust
	if cmd == nil || json.Unmarshal(cmd.Params, &tr) != nil || tr.Name != "calc-db" || !tr.Trust || tr.Cwd != a.C.MCPFor {
		t.Fatalf("%+v", tr)
	}
}

func TestMCPApproval(t *testing.T) {
	a := working()
	args, _ := json.Marshal(map[string]any{"title": "Add 返回差而不是和", "repo": "me/calc"})
	a.C.Approvals = []proto.Approval{{ID: "m1", Tool: "mcp__github__create_issue", Kind: "mcp", Command: "github · create_issue", Args: args,
		Reason: "It calls a tool of an MCP server, outside the sandbox."}}
	tt := ui.NewTester(a.View, 1200, 780)
	tt.Frame()
	for _, s := range []string{"调用 MCP 工具", "github · create_issue", "MCP 服务器不在沙箱里运行"} {
		if !tt.HasText(s) {
			t.Fatalf("%q not shown", s)
		}
	}
	if tt.HasText("calc $") {
		t.Fatal("a shell prompt before an MCP call")
	}
	snapshot(t, tt, "mcp-approval")
}

func TestMCPAndSkillsManageUI(t *testing.T) {
	a := working()
	a.out = make(chan []proto.Command, 16)
	a.settingsOpen, a.settingsPage = true, "mcp"
	a.C.MCPFor = "/Users/me/Projects/calc"
	a.C.MCP = &proto.MCPList{
		Skills: []proto.SkillInfo{{Name: "lint", Description: "Run lint", Body: "Use golangci-lint", Project: true}},
	}
	tt := ui.NewTester(a.View, 1200, 780)
	tt.Frame()
	if !tt.HasText("lint") || !tt.HasText("Run lint") {
		t.Fatalf("skill not shown: %q", tt.Texts())
	}
	if err := tt.Click("lint"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Use golangci-lint") {
		t.Fatalf("skill body not expanded: %q", tt.Texts())
	}
	if err := tt.Click("添加服务器"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	a.mcpUI.mcpName = "docs"
	a.mcpUI.mcpTarget = "https://docs.example.com/mcp"
	tt.Frame()
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	cmd := sent(a, proto.MethodMCPSave)
	var save proto.MCPSave
	if cmd == nil || json.Unmarshal(cmd.Params, &save) != nil || save.Name != "docs" || save.URL != "https://docs.example.com/mcp" {
		t.Fatalf("save mcp = %+v", save)
	}
}
