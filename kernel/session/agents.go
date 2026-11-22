package session

import (
	"strings"

	"github.com/shqingda/stratum/kernel/agents"
	"github.com/shqingda/stratum/kernel/store"
	"github.com/shqingda/stratum/proto"
)

// agents lists the agents for a folder: built in, from enabled plugins,
// the user's and the folder's own, later ones replacing earlier of a name.
func (m *Manager) agents(cwd string) []agents.Agent {
	return agents.Load(m.Skills.GlobalRoot(), cwd, m.pluginDirs(cwd, "agents")...)
}

// primary is the agent that leads x's turns: its own, or build when that
// one is gone.
func (m *Manager) primary(list []agents.Agent, x store.Session) agents.Agent {
	if a, ok := agents.Named(list, cmpOr(x.Agent, "build")); ok && a.Primary() {
		return a
	}
	a, _ := agents.Named(list, "build")
	return a
}

// agentTarget is where an agent's model setting sends its requests. spec
// is a model of the session's account, or provider/model as OpenCode
// writes it, served by the first signed-in account of that provider.
func (m *Manager) agentTarget(account, spec string) (target, bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return target{}, false
	}
	if provider, model, ok := strings.Cut(spec, "/"); ok && m.Accounts != nil {
		for _, id := range m.Accounts() {
			if p, _, _ := strings.Cut(id, "/"); p == provider {
				if t, ok := m.role(&proto.ModelRef{Account: id, Model: model}); ok {
					return t, true
				}
			}
		}
	}
	return m.role(&proto.ModelRef{Account: account, Model: spec})
}
