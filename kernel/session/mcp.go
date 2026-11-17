package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/shqingda/talos/kernel/mcp"
	"github.com/shqingda/talos/proto"
)

const trustKey = "mcp.trusted"

func trustID(cwd string, e mcp.Entry) string {
	return cwd + "\x00" + e.Name + "\x00" + e.Fingerprint
}

func (m *Manager) trusted(ctx context.Context) []string {
	var ids []string
	if b, err := m.Store.Setting(ctx, trustKey); err == nil && b != nil {
		json.Unmarshal(b, &ids)
	}
	return ids
}

// UseMCP serves the tools of the servers in userFile and in each folder's
// .mcp.json, the latter once the user trusts them.
func (m *Manager) UseMCP(userFile string) {
	m.MCP = &mcp.Hub{
		UserFile: userFile,
		Trusted: func(cwd string, e mcp.Entry) bool {
			return slices.Contains(m.trusted(context.Background()), trustID(cwd, e))
		},
		Changed: func() { m.Broadcast(proto.EventMCP, struct{}{}) },
	}
}

// MCPList says how each server is for a folder.
func (m *Manager) MCPList(ctx context.Context, q proto.MCPQuery) (proto.MCPList, error) {
	out := proto.MCPList{Servers: []proto.MCPServer{}}
	if q.Cwd != "" {
		for _, sk := range m.Skills.Load(q.Cwd).Skills {
			out.Skills = append(out.Skills, proto.SkillInfo{Name: sk.Name, Description: sk.Description})
		}
	}
	out.Commands = commands(q.Cwd)
	if m.MCP == nil {
		return out, nil
	}
	sts, err := m.MCP.Status(q.Cwd)
	if err != nil {
		out.Error = err.Error()
	}
	for _, st := range sts {
		target := st.Server.URL
		if st.Server.Command != "" {
			target = strings.Join(append([]string{st.Server.Command}, st.Server.Args...), " ")
		}
		out.Servers = append(out.Servers, proto.MCPServer{Name: st.Name, Project: st.Project, Target: target, State: st.State, Error: st.Error, Tools: st.Tools})
	}
	out.Resources = m.MCP.Resources(q.Cwd)
	return out, nil
}

// MCPTrust lets a folder's server run, or stops trusting it.
func (m *Manager) MCPTrust(ctx context.Context, t proto.MCPTrust) (proto.MCPList, error) {
	if m.MCP == nil {
		return proto.MCPList{}, proto.Errorf("bad_request", "MCP is off")
	}
	es, _ := mcp.Load(m.MCP.UserFile, t.Cwd)
	i := slices.IndexFunc(es, func(e mcp.Entry) bool { return e.Name == t.Name && e.Project })
	if i < 0 {
		return proto.MCPList{}, proto.Errorf("not_found", "no server %q in %s/.mcp.json", t.Name, t.Cwd)
	}
	id := trustID(t.Cwd, es[i])
	ids := slices.DeleteFunc(m.trusted(ctx), func(x string) bool { return strings.HasPrefix(x, t.Cwd+"\x00"+t.Name+"\x00") })
	if t.Trust {
		ids = append(ids, id)
	}
	b, _ := json.Marshal(ids)
	if err := m.Store.SetSetting(ctx, trustKey, b); err != nil {
		return proto.MCPList{}, err
	}
	return m.MCPList(ctx, proto.MCPQuery{Cwd: t.Cwd})
}

// commands reads the saved prompts for cwd: the folder's own first, then
// the user's; a name already taken is skipped. The first line is the
// description, unless the file starts with front matter.
func commands(cwd string) []proto.CommandInfo {
	var dirs []string
	if cwd != "" {
		dirs = append(dirs, filepath.Join(cwd, ".talos", "commands"), filepath.Join(cwd, ".claude", "commands"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".talos", "commands"))
	}
	var out []proto.CommandInfo
	seen := map[string]bool{}
	for _, dir := range dirs {
		names, _ := filepath.Glob(filepath.Join(dir, "*.md"))
		for _, path := range names {
			name := strings.TrimSuffix(filepath.Base(path), ".md")
			b, err := os.ReadFile(path)
			if err != nil || seen[name] || len(b) > 64<<10 {
				continue
			}
			seen[name] = true
			body, desc := strings.TrimSpace(string(b)), ""
			if rest, ok := strings.CutPrefix(body, "---\n"); ok {
				if front, after, ok := strings.Cut(rest, "\n---"); ok {
					body = strings.TrimSpace(after)
					for _, line := range strings.Split(front, "\n") {
						if v, ok := strings.CutPrefix(line, "description:"); ok {
							desc = strings.Trim(strings.TrimSpace(v), `"'`)
						}
					}
				}
			}
			if desc == "" {
				desc, _, _ = strings.Cut(body, "\n")
				desc = strings.TrimLeft(desc, "# ")
			}
			out = append(out, proto.CommandInfo{Name: name, Description: desc, Body: body})
		}
	}
	return out
}
