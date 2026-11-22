package client

import (
	"encoding/json"

	"github.com/shqingda/stratum/proto"
)

// SignIn is a sign-in under way, and what the user has to do for it.
type SignIn struct {
	Provider, Method string
	Device           bool
	proto.LoginProgress
	Err string
}

// Login signs in. A browser sign-in reports its progress in SignIn; once
// it is done the account joins Accounts, and a session without one takes
// it.
func (c *Client) Login(q proto.Login) ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	s := &SignIn{Provider: q.Provider, Method: q.Method, Device: q.Device}
	c.SignIn = s
	p := &pending{}
	p.done = func(r json.RawMessage) []proto.Command {
		var a proto.Account
		if json.Unmarshal(r, &a) != nil || a.ID == "" {
			return nil
		}
		if c.SignIn == s {
			c.SignIn = nil
		}
		i := len(c.Accounts)
		for j := range c.Accounts {
			if c.Accounts[j].ID == a.ID {
				i = j
			}
		}
		if i == len(c.Accounts) {
			c.Accounts = append(c.Accounts, a)
		} else {
			c.Accounts[i] = a
		}
		if c.Current != nil && c.Current.Account == "" {
			cmds, _ := c.Update(proto.SessionUpdate{Account: &a.ID, Model: &a.Model})
			return cmds
		}
		return nil
	}
	p.failed = func(e *proto.Error) []proto.Command {
		if c.SignIn == s && e.Code != "cancelled" {
			s.Err = e.Message
		}
		return nil
	}
	return []proto.Command{c.send(proto.MethodLogin, q, p)}, nil
}

// CancelLogin gives up the sign-in under way.
func (c *Client) CancelLogin() ([]proto.Command, error) {
	if c.Phase != Ready {
		return nil, ErrNotReady
	}
	c.SignIn = nil
	return []proto.Command{c.read(proto.MethodLoginCancel, nil, nil)}, nil
}

func (c *Client) loginProgress(e proto.Event) {
	if c.SignIn != nil {
		c.SignIn.LoginProgress = proto.LoginProgress{}
		_ = json.Unmarshal(e.Data, &c.SignIn.LoginProgress)
	}
}
