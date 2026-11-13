package client

import (
	"encoding/json"
	"slices"

	"github.com/shqingda/talos/proto"
)

// ReadSettings reads what new sessions start with into Settings.
func (c *Client) ReadSettings() ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	return []proto.Command{c.read(proto.MethodSettings, nil, c.keepSettings)}, nil
}

// SaveSettings changes them; Settings shows the change at once.
func (c *Client) SaveSettings(s proto.Settings) ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	c.Settings = &s
	return []proto.Command{c.mutate(proto.MethodSetSettings, s, c.keepSettings)}, nil
}

func (c *Client) keepSettings(r json.RawMessage) []proto.Command {
	var s proto.Settings
	if json.Unmarshal(r, &s) == nil {
		c.Settings = &s
	}
	return nil
}

// Logout forgets an account.
func (c *Client) Logout(id string) ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	return []proto.Command{c.mutate(proto.MethodLogout, proto.AccountRef{Account: id}, func(json.RawMessage) []proto.Command {
		c.Accounts = slices.DeleteFunc(c.Accounts, func(a proto.Account) bool { return a.ID == id })
		if c.Settings != nil && c.Settings.Account == id {
			c.Settings.Account, c.Settings.Model = "", ""
		}
		return nil
	})}, nil
}
