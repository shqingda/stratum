//go:build darwin

package app

import (
	"os"
	"os/exec"
	"strconv"
	"sync"
	"testing"
)

type sleepGuard struct {
	mu  sync.Mutex
	cmd *exec.Cmd
}

func (g *sleepGuard) Set(on bool) {
	if testing.Testing() {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if on {
		if g.cmd != nil {
			return
		}
		cmd := exec.Command("/usr/bin/caffeinate", "-i", "-w", strconv.Itoa(os.Getpid()))
		if err := cmd.Start(); err != nil {
			return
		}
		g.cmd = cmd
		go func(c *exec.Cmd) {
			_ = c.Wait()
			g.mu.Lock()
			if g.cmd == c {
				g.cmd = nil
			}
			g.mu.Unlock()
		}(cmd)
		return
	}
	if g.cmd != nil {
		if g.cmd.Process != nil {
			_ = g.cmd.Process.Kill()
		}
		g.cmd = nil
	}
}
