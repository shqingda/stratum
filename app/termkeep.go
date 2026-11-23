package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
)

// keptTab is a shell's tab kept across a quit, as Terminal.app restores
// its windows: the folder it was in and what it showed, which the shell
// started in its place shows above its first prompt.
type keptTab struct {
	Dir     string `json:"dir"`
	History []byte `json:"history,omitempty"`
}

// keptHistory is the most of a tab's scrollback kept, in bytes.
const keptHistory = 512 << 10

// KeepTerminals keeps the shells' tabs in file across a quit: it reads
// those kept last time, which a session's terminal opens again the first
// time it shows, and SaveTerminals writes them.
func (a *App) KeepTerminals(file string) {
	a.term.file = file
	if b, err := os.ReadFile(file); err == nil {
		_ = json.Unmarshal(b, &a.term.kept)
	}
}

// reopenTerminals opens the tabs kept for the session, and reports
// whether there were any.
func (a *App) reopenTerminals(sid string) bool {
	tabs := a.term.kept[sid]
	if len(tabs) == 0 {
		return false
	}
	delete(a.term.kept, sid)
	for _, k := range tabs {
		if _, err := os.Stat(k.Dir); err != nil {
			k.Dir = a.C.Current.Cwd
		}
		a.addTerminalAt(k.Dir, k.History)
	}
	return len(a.termSet(sid).tabs) > 0
}

// SaveTerminals writes the tabs of the shells that run, and those kept
// for sessions whose terminal did not show, to the file, as the app quits.
func (a *App) SaveTerminals() error {
	if a.term.file == "" {
		return nil
	}
	kept := map[string][]keptTab{}
	for sid, tabs := range a.term.kept {
		kept[sid] = tabs
	}
	for sid, set := range a.term.sets {
		var tabs []keptTab
		for _, tab := range set.tabs {
			if tab.exited {
				continue
			}
			dir := tab.t.Dir()
			if dir == "" {
				dir = tab.cwd
			}
			tabs = append(tabs, keptTab{Dir: dir, History: lastLines(tab.t.History(), keptHistory)})
		}
		if len(tabs) == 0 {
			delete(kept, sid)
		} else {
			kept[sid] = tabs
		}
	}
	if len(kept) == 0 {
		if err := os.Remove(a.term.file); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	b, err := json.Marshal(kept)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.term.file), 0o700); err != nil {
		return err
	}
	tmp := a.term.file + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, a.term.file)
}

// lastLines is the end of b, at most n bytes, from the start of a line.
func lastLines(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	b = b[len(b)-n:]
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		b = b[i+1:]
	}
	return b
}
