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
