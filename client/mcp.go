package client

import (
	"encoding/json"

	"github.com/shqingda/talos/proto"
)

// ReadMCP reads how the MCP servers are for the current session's folder.
func (c *Client) ReadMCP() ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	return []proto.Command{c.readMCP(c.Cwd)}, nil
}

func (c *Client) readMCP(cwd string) proto.Command {
	if c.Current != nil {
		cwd = c.Current.Cwd
	}
	return c.read(proto.MethodMCPList, proto.MCPQuery{Cwd: cwd}, func(r json.RawMessage) []proto.Command {
		c.keepMCP(cwd, r)
		return nil
	})
}

func (c *Client) keepMCP(cwd string, r json.RawMessage) {
	var l proto.MCPList
	if json.Unmarshal(r, &l) == nil {
		c.MCP, c.MCPFor = &l, cwd
	}
}

// TrustMCP lets a server of the folder's .mcp.json run, or stops it.
func (c *Client) TrustMCP(name string, trust bool) ([]proto.Command, error) {
	if c.Phase != Ready || c.MCP == nil {
		return nil, ErrNotReady
	}
	cwd := c.MCPFor
	return []proto.Command{c.mutate(proto.MethodMCPTrust, proto.MCPTrust{Cwd: cwd, Name: name, Trust: trust}, func(r json.RawMessage) []proto.Command {
		c.keepMCP(cwd, r)
		return nil
	})}, nil
}

func (c *Client) mcpMutate(method string, payload any, cwd string) ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	return []proto.Command{c.mutate(method, payload, func(r json.RawMessage) []proto.Command {
		c.keepMCP(cwd, r)
		return nil
	})}, nil
}

// SaveMCP adds or updates an MCP server.
func (c *Client) SaveMCP(s proto.MCPSave) ([]proto.Command, error) {
	if s.Cwd == "" {
		s.Cwd = c.mcpCwd()
	}
	return c.mcpMutate(proto.MethodMCPSave, s, s.Cwd)
}

// RemoveMCP removes an MCP server.
func (c *Client) RemoveMCP(name string, project bool) ([]proto.Command, error) {
	cwd := c.mcpCwd()
	return c.mcpMutate(proto.MethodMCPRemove, proto.MCPRemove{Cwd: cwd, Name: name, Project: project}, cwd)
}

// SaveSkill creates a skill or installs skills from Source.
func (c *Client) SaveSkill(s proto.SkillSave) ([]proto.Command, error) {
	if s.Cwd == "" {
		s.Cwd = c.mcpCwd()
	}
	return c.mcpMutate(proto.MethodSkillSave, s, s.Cwd)
}

// ToggleSkill enables or disables a skill by name.
func (c *Client) ToggleSkill(name string, enabled bool) ([]proto.Command, error) {
	cwd := c.mcpCwd()
	return c.mcpMutate(proto.MethodSkillToggle, proto.SkillToggle{Cwd: cwd, Name: name, Enabled: enabled}, cwd)
}

// RemoveSkill removes an installed skill directory.
func (c *Client) RemoveSkill(name string, project bool) ([]proto.Command, error) {
	cwd := c.mcpCwd()
	return c.mcpMutate(proto.MethodSkillRemove, proto.SkillRemove{Cwd: cwd, Name: name, Project: project}, cwd)
}

func (c *Client) mcpCwd() string {
	if c.MCPFor != "" {
		return c.MCPFor
	}
	if c.Current != nil {
		return c.Current.Cwd
	}
	return c.Cwd
}
