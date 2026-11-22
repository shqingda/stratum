package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/locales"
	"github.com/shqingda/stratum/proto"
)

// builtInAgents stand for the kernel's list until the folder's arrives.
var builtInAgents = []proto.AgentInfo{
	{Name: "build", Mode: "primary", BuiltIn: true},
	{Name: "plan", Mode: "primary", BuiltIn: true},
}

// agentInfos are the agents of the current folder: built in, from
// plugins, the user's and the folder's own.
func (a *App) agentInfos() []proto.AgentInfo {
	if l := a.C.MCP; l != nil && a.C.Current != nil && a.C.MCPFor == a.C.Current.Cwd && len(l.Agents) > 0 {
		return l.Agents
	}
	return builtInAgents
}

// primaryAgents are the agents that can lead a session, in the kernel's
// order: build and plan first.
func primaryAgents(list []proto.AgentInfo) []proto.AgentInfo {
	var out []proto.AgentInfo
	for _, ag := range list {
		if !ag.Hidden && (ag.Mode == "primary" || ag.Mode == "all") {
			out = append(out, ag)
		}
	}
	return out
}

// subAgents are the agents @name hands work to.
func subAgents(list []proto.AgentInfo) []proto.AgentInfo {
	var out []proto.AgentInfo
	for _, ag := range list {
		if !ag.Hidden && (ag.Mode == "subagent" || ag.Mode == "all") {
			out = append(out, ag)
		}
	}
	return out
}

func agentLabel(name string) string {
	switch name {
	case "", "build":
		return locales.AgentBuild()
	case "plan":
		return locales.AgentPlan()
	case "general":
		return locales.AgentGeneral()
	case "explore":
		return locales.AgentExplore()
	}
	return name
}

func agentNote(ag proto.AgentInfo) string {
	if ag.BuiltIn {
		switch ag.Name {
		case "build":
			return locales.AgentBuildNote()
		case "plan":
			return locales.AgentPlanNote()
		case "general":
			return locales.AgentGeneralNote()
		case "explore":
			return locales.AgentExploreNote()
		}
	}
	return ag.Description
}

// currentAgent is the primary agent of the open session.
func (a *App) currentAgent() string {
	if a.C.Current == nil || a.C.Current.Agent == "" {
		return "build"
	}
	return a.C.Current.Agent
}

// agentPill picks the primary agent that leads the session, as OpenCode's
// build and plan; Tab in the composer moves to the next.
// The folder's own agents are read when the menu opens or Tab is pressed,
// as the / and @ lists read theirs.
func (a *App) agentPill(c *ui.Context) {
	cur := a.currentAgent()
	b := a.pill(c, "agent", "", agentLabel(cur), true)
	b.Disabled(!a.C.CanAct()).PopoverMenuButton(func(m *ui.Menu) {
		a.ensureMCP()
		for _, ag := range primaryAgents(a.agentInfos()) {
			item := m.Item(agentLabel(ag.Name)).ID("agent:" + ag.Name).Checked(cur == ag.Name)
			if note := agentNote(ag); note != "" {
				item.Subtitle(note)
			}
			if item.Chosen() {
				a.setAgent(ag.Name)
			}
		}
		m.Separator()
		if m.Item(locales.NextAgent()).ID("agent:next").Shortcut(0, ui.KeyTab).Chosen() {
			a.cycleAgent(1)
		}
	}, a.menuOptions(&a.agentMenu))
}

func (a *App) setAgent(name string) {
	if a.C.Current == nil || name == a.currentAgent() {
		return
	}
	a.do(a.C.Update(proto.SessionUpdate{Agent: &name}))
}

// cycleAgent moves the session to the next primary agent, or the one
// before for step -1, and says which it is.
func (a *App) cycleAgent(step int) {
	a.ensureMCP()
	list := primaryAgents(a.agentInfos())
	if a.C.Current == nil || len(list) < 2 {
		return
	}
	at := 0
	for i, ag := range list {
		if ag.Name == a.currentAgent() {
			at = i
		}
	}
	next := list[(at+step+len(list))%len(list)].Name
	a.setAgent(next)
	a.flash("cpu", agentLabel(next))
}

// agentsSection lists the folder's agents in settings, primary ones and
// subagents apart, each with where it is defined.
func (a *App) agentsSection(c *ui.Context) {
	p := a.p
	a.ensureMCP()
	list := a.agentInfos()
	for i, part := range []struct {
		label string
		items []proto.AgentInfo
	}{{locales.PrimaryAgents(), primaryAgents(list)}, {locales.Subagents(), subAgents(list)}} {
		if len(part.items) == 0 {
			continue
		}
		section(c, p, part.label, i == 0)
		group(c, p, func() {
			r := rows{p: p}
			for _, ag := range part.items {
				r.row(c, "agent:"+part.label+":"+ag.Name, agentLabel(ag.Name), agentNote(ag), func() {
					ui.Text(c, agentSource(ag)).FontSize(12).TextColor(p.Mute).SingleLine().Ellipsis("…").MaxWidth(180)
				})
			}
		})
	}
	note(c, p, locales.AgentsNote())
}

// agentSource says where an agent comes from: built in, or its file.
func agentSource(ag proto.AgentInfo) string {
	switch {
	case ag.BuiltIn && ag.Source == "":
		return locales.BuiltIn()
	case ag.Source == "":
		return ""
	}
	return home(ag.Source)
}

// formattersSection lists what formats a file after the agent writes it.
func (a *App) formattersSection(c *ui.Context) {
	p := a.p
	a.ensureMCP()
	var list []proto.FormatterInfo
	if l := a.C.MCP; l != nil && a.C.Current != nil && a.C.MCPFor == a.C.Current.Cwd {
		list = l.Formatters
	}
	section(c, p, locales.Formatters(), true)
	group(c, p, func() {
		r := rows{p: p}
		for _, f := range list {
			sub := strings.Join(f.Command, " ")
			label := f.Name
			if f.BuiltIn {
				label += " · " + locales.BuiltIn()
			}
			r.row(c, "formatter:"+f.Name, label, sub, func() {
				what := strings.Join(f.Extensions, " ")
				if f.Disabled {
					what = locales.SettingOff()
				}
				ui.Text(c, what).FontSize(12).TextColor(p.Mute).SingleLine()
			})
		}
	})
	note(c, p, locales.FormattersNote())
}
