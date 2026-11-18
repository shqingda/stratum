package client

import (
	"encoding/json"

	"github.com/shqingda/talos/proto"
)

// OpenPreview navigates the built-in preview browser to url and refreshes Preview.
func (c *Client) OpenPreview(url string) ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	return []proto.Command{c.mutate(proto.MethodPreviewOpen, proto.PreviewRequest{URL: url}, func(r json.RawMessage) []proto.Command {
		var snap proto.PreviewSnapshot
		if json.Unmarshal(r, &snap) == nil {
			c.Preview = &snap
		}
		return nil
	})}, nil
}

// ReadPreview fetches the latest built-in preview state.
func (c *Client) ReadPreview() ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	return []proto.Command{c.read(proto.MethodPreviewState, struct{}{}, func(r json.RawMessage) []proto.Command {
		var snap proto.PreviewSnapshot
		if json.Unmarshal(r, &snap) == nil && snap.URL != "" {
			c.Preview = &snap
		}
		return nil
	})}, nil
}

// ReadAutomations fetches all background/scheduled automations.
func (c *Client) ReadAutomations() ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	return []proto.Command{c.read(proto.MethodAutomationList, struct{}{}, func(r json.RawMessage) []proto.Command {
		var list proto.AutomationList
		if json.Unmarshal(r, &list) == nil {
			c.Automations = &list
		}
		return nil
	})}, nil
}

// UpsertAutomation creates or updates an automation, then refreshes the list.
func (c *Client) UpsertAutomation(a proto.Automation) ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	return []proto.Command{c.mutate(proto.MethodAutomationUpsert, a, func(json.RawMessage) []proto.Command {
		cmds, _ := c.ReadAutomations()
		return cmds
	})}, nil
}

// DeleteAutomation deletes an automation by ID and refreshes the list.
func (c *Client) DeleteAutomation(id string) ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	return []proto.Command{c.mutate(proto.MethodAutomationDelete, proto.AutomationID{ID: id}, func(json.RawMessage) []proto.Command {
		cmds, _ := c.ReadAutomations()
		return cmds
	})}, nil
}

// RunAutomation triggers an automation immediately and refreshes the list + sessions.
func (c *Client) RunAutomation(id string) ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	return []proto.Command{c.mutate(proto.MethodAutomationRun, proto.AutomationID{ID: id}, func(json.RawMessage) []proto.Command {
		cmds, _ := c.ReadAutomations()
		return cmds
	})}, nil
}

// ReadHooks fetches configured hooks for the current workspace.
func (c *Client) ReadHooks() ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	cwd := c.mcpCwd()
	return []proto.Command{c.read(proto.MethodHooksList, proto.HooksQuery{Cwd: cwd}, func(r json.RawMessage) []proto.Command {
		var hl proto.HooksList
		if json.Unmarshal(r, &hl) == nil {
			c.Hooks = &hl
		}
		return nil
	})}, nil
}

// SaveHooks saves editable hooks for project or global scope.
func (c *Client) SaveHooks(project bool, hooks []proto.HookEntry) ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	cwd := c.mcpCwd()
	return []proto.Command{c.mutate(proto.MethodHooksSave, proto.HooksSave{Cwd: cwd, Project: project, Hooks: hooks}, func(r json.RawMessage) []proto.Command {
		var hl proto.HooksList
		if json.Unmarshal(r, &hl) == nil {
			c.Hooks = &hl
		}
		return nil
	})}, nil
}

// ReadPlugins fetches installed plugins for the current workspace.
func (c *Client) ReadPlugins() ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	cwd := c.mcpCwd()
	return []proto.Command{c.read(proto.MethodPluginList, proto.HooksQuery{Cwd: cwd}, func(r json.RawMessage) []proto.Command {
		var pl proto.PluginList
		if json.Unmarshal(r, &pl) == nil {
			c.Plugins = &pl
		}
		return nil
	})}, nil
}

// InstallPlugin installs a plugin from source.
func (c *Client) InstallPlugin(source string, project bool) ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	cwd := c.mcpCwd()
	return []proto.Command{c.mutate(proto.MethodPluginInstall, proto.PluginInstall{Cwd: cwd, Source: source, Project: project}, func(r json.RawMessage) []proto.Command {
		var pl proto.PluginList
		if json.Unmarshal(r, &pl) == nil {
			c.Plugins = &pl
		}
		return nil
	})}, nil
}

// TogglePlugin enables or disables a plugin.
func (c *Client) TogglePlugin(name string, enabled bool) ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	cwd := c.mcpCwd()
	return []proto.Command{c.mutate(proto.MethodPluginToggle, proto.PluginToggle{Cwd: cwd, Name: name, Enabled: enabled}, func(r json.RawMessage) []proto.Command {
		var pl proto.PluginList
		if json.Unmarshal(r, &pl) == nil {
			c.Plugins = &pl
		}
		return nil
	})}, nil
}

// RemovePlugin uninstalls a plugin by name.
func (c *Client) RemovePlugin(name string) ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	cwd := c.mcpCwd()
	return []proto.Command{c.mutate(proto.MethodPluginRemove, proto.PluginRemove{Cwd: cwd, Name: name}, func(r json.RawMessage) []proto.Command {
		var pl proto.PluginList
		if json.Unmarshal(r, &pl) == nil {
			c.Plugins = &pl
		}
		return nil
	})}, nil
}
