package app

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/locales"
)

func TestTerminalThatFailsToStartSaysWhy(t *testing.T) {
	a := screen1()
	a.newTerm = func(terminal.Options) (*terminal.Terminal, error) { return nil, errors.New("no libghostty-vt") }
	tt := ui.NewTester(a.View, 1440, 880)
	tt.Frame()
	tt.Key(ui.Ctrl, ui.KeyBackquote)
	tt.Frame()
	if !a.rv.open || a.rv.tab != "term" {
		t.Fatalf("⌃` left the panel open=%v on %q", a.rv.open, a.rv.tab)
	}
	for _, want := range []string{"终端没有启动", "no libghostty-vt", "重试"} {
		if !tt.HasText(want) {
			t.Errorf("the terminal tab lacks %q: %q", want, tt.Texts())
		}
	}
	tt.Key(ui.Ctrl, ui.KeyBackquote)
	tt.Frame()
	if a.rv.open {
		t.Fatal("⌃` again kept the terminal")
	}
}

func TestTerminalStartsOnlyWhenOpened(t *testing.T) {
	a := screen1()
	started := 0
	a.newTerm = func(terminal.Options) (*terminal.Terminal, error) { started++; return nil, errors.New("fake") }
	a.rv.open, a.rv.tab = true, "plan"
	tt := ui.NewTester(a.View, 1440, 880)
	tt.Frame()
	if err := tt.Click("终端"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if started != 1 || a.rv.tab != "term" {
		t.Fatalf("the 终端 tab started %d shells, shows %q", started, a.rv.tab)
	}
	a.C.Current = &a.C.Sessions[1]
	tt.Frame()
	tt.Frame()
	if started != 1 {
		t.Fatalf("passing through a session started %d shells", started)
	}
	if !tt.HasText("没有打开的终端") || !tt.HasText("新建终端") {
		t.Errorf("a session without a shell lacks its empty state: %q", tt.Texts())
	}
}

// termLibrary is whether libghostty-vt is on this machine already, as the
// tests that run a shell need: they never download it themselves, unless
// STRATUM_TERMINAL_TESTS asks.
func termLibrary() bool {
	if os.Getenv("MYGO_GHOSTTY_VT") != "" || os.Getenv("STRATUM_TERMINAL_TESTS") != "" {
		return true
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return false
	}
	m, _ := filepath.Glob(filepath.Join(dir, "mygo", "natives", "*", "libghostty-vt.*"))
	return len(m) > 0
}

// termApp is an app whose terminals run /bin/sh in a folder of the test's,
// with a marker file in it, and a pump that runs what the shells send the
// UI thread and draws.
func termApp(t *testing.T) (a *App, tt *ui.Tester, pump func(), dir string) {
	t.Helper()
	if !termLibrary() {
		t.Skip("libghostty-vt is not in this machine's cache (terminal.LibraryPath); STRATUM_TERMINAL_TESTS=1 downloads it")
	}
	a = screen1()
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marker-stratum.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	a.C.Current.Cwd = dir
	a.C.Busy = false
	var mu sync.Mutex
	var queue []func()
	a.update = func(f func()) { mu.Lock(); queue = append(queue, f); mu.Unlock() }
	a.newTerm = func(o terminal.Options) (*terminal.Terminal, error) {
		o.Command = []string{"/bin/sh"}
		o.Env = append(o.Env, "PS1=$ ", "ENV=")
		return terminal.New(o)
	}
	tt = ui.NewTester(a.View, 1440, 880)
	pump = func() {
		mu.Lock()
		q := queue
		queue = nil
		mu.Unlock()
		for _, f := range q {
			f()
		}
		tt.Frame()
	}
	t.Cleanup(func() {
		for _, set := range a.term.sets {
			for _, tab := range set.tabs {
				tab.t.Close()
			}
		}
	})
	tt.Frame()
	return
}

func waitFor(t *testing.T, pump func(), what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		pump()
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestTerminalRunsAShellInTheSessionFolder(t *testing.T) {
	a, tt, pump, _ := termApp(t)
	tt.Key(ui.Ctrl, ui.KeyBackquote)
	waitFor(t, pump, "a shell", func() bool { return a.activeTerm() != nil && tt.Focused("Terminal") })
	term := a.activeTerm().t
	waitFor(t, pump, "the prompt", func() bool { return strings.Contains(term.Text(), "$") })
	tt.Type("ls")
	tt.Key(0, ui.KeyEnter)
	waitFor(t, pump, "ls in the session's folder", func() bool { return strings.Contains(term.Text(), "marker-stratum.txt") })
	tt.Advance(time.Second)
	pump()
	snapshot(t, tt, "pane-term")
	session := a.C.Current.ID

	// The keys a shell reads reach it, not the window's shortcuts.
	tt.Type("cat -v")
	tt.Key(0, ui.KeyEnter)
	waitFor(t, pump, "cat", func() bool { return strings.Count(term.Text(), "cat -v") > 0 })
	time.Sleep(50 * time.Millisecond)
	tt.Key(0, ui.KeyEscape)
	tt.Key(ui.Shift, ui.KeyTab)
	tt.Key(ui.Alt, ui.KeyDown)
	tt.Type("z")
	tt.Key(0, ui.KeyEnter)
	waitFor(t, pump, "esc and ⇧⇥ echoed", func() bool { return strings.Contains(term.Text(), "^[^[[Z") })
	if a.C.Current.ID != session {
		t.Errorf("⌥↓ in the terminal moved to session %s", a.C.Current.ID)
	}
	if !a.rv.open {
		t.Error("esc in the terminal closed the panel")
	}
	tt.Key(ui.Ctrl, ui.KeyC)
}

func TestTerminalTabs(t *testing.T) {
	a, tt, pump, _ := termApp(t)
	a.showTerminal()
	waitFor(t, pump, "a shell", func() bool { return a.activeTerm() != nil && tt.Focused("Terminal") })
	tt.Key(ui.Cmd, ui.KeyT)
	pump()
	set := a.term.sets[a.C.Current.ID]
	if len(set.tabs) != 2 || set.active != 1 {
		t.Fatalf("⌘T made %d tabs, showing %d", len(set.tabs), set.active)
	}
	second := set.tabs[1].t
	waitFor(t, pump, "the second shell to have the keyboard", func() bool { return tt.Focused("Terminal") })
	name, ok1 := tt.Find(termTitle(set.tabs[0]))
	x, ok2 := tt.Find(locales.CloseTerminal())
	if !ok1 || !ok2 || x.X < name.X+name.W-0.5 || x.Y > name.Y+name.H || x.Y+x.H < name.Y {
		t.Errorf("the close button %v is not on the right of the tab %v", x, name)
	}
	waitFor(t, pump, "the second prompt", func() bool { return strings.Contains(second.Text(), "$") })
	tt.Type("exit")
	tt.Key(0, ui.KeyEnter)
	waitFor(t, pump, "the tab to close after exit", func() bool { return len(set.tabs) == 1 })
	if set.active != 0 {
		t.Errorf("after exit the pane shows tab %d", set.active)
	}
	waitFor(t, pump, "the first shell to have the keyboard", func() bool { return tt.Focused("Terminal") })
}

func TestTerminalAsksBeforeEndingAJob(t *testing.T) {
	a, tt, pump, _ := termApp(t)
	a.showTerminal()
	waitFor(t, pump, "a shell", func() bool { return a.activeTerm() != nil && tt.Focused("Terminal") })
	tab := a.activeTerm()
	waitFor(t, pump, "the prompt", func() bool { return strings.Contains(tab.t.Text(), "$") })

	// At the prompt ⌘W closes the tab at once, as Terminal.app does.
	tt.Key(ui.Cmd, ui.KeyT)
	waitFor(t, pump, "a second shell", func() bool { return len(a.term.sets[a.C.Current.ID].tabs) == 2 && tt.Focused("Terminal") })
	waitFor(t, pump, "the second prompt", func() bool { return strings.Contains(a.activeTerm().t.Text(), "$") })
	tt.Key(ui.Cmd, ui.KeyW)
	waitFor(t, pump, "the idle tab to close", func() bool { return len(a.term.sets[a.C.Current.ID].tabs) == 1 })
	if a.term.closing != "" {
		t.Fatal("⌘W asked about an idle shell")
	}

	waitFor(t, pump, "the first shell to have the keyboard", func() bool { return tt.Focused("Terminal") })
	tt.Type("sleep 30")
	tt.Key(0, ui.KeyEnter)
	waitFor(t, pump, "sleep in front", func() bool { _, busy := foregroundJob(a.liveTerminals(), tab.id); return busy })
	tt.Key(ui.Cmd, ui.KeyW)
	waitFor(t, pump, "the terminate dialog", func() bool { return a.term.closing == tab.id })
	if a.term.job != "sleep" {
		t.Errorf("the dialog names %q, not sleep", a.term.job)
	}
	if !tt.HasText("关闭这个终端会终止正在运行的进程：sleep。") {
		t.Errorf("the dialog lacks its words: %q", tt.Texts())
	}
	if err := tt.Click("终止"); err != nil {
		t.Fatal(err)
	}
	pump()
	if n := len(a.term.sets[a.C.Current.ID].tabs); n != 0 {
		t.Fatalf("%d tabs after Terminate", n)
	}
	if !tt.HasText("没有打开的终端") {
		t.Errorf("no empty state after the last tab: %q", tt.Texts())
	}
}

// TestZshReportsItsFolder runs a real zsh with startup files of the
// user's own in ZDOTDIR: they load as ever, and the shell reports its
// folder, which names its tab.
func TestZshReportsItsFolder(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("no zsh")
	}
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	user, deep, tmp := filepath.Join(home, "zdot"), filepath.Join(home, "a", "b", "c 100%"), t.TempDir()
	for _, d := range []string{user, deep} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// The user's own title, as Oh My Zsh sets it, does not name the tab.
	for name, body := range map[string]string{
		".zshenv": "export FROM_ZSHENV=yes\n",
		".zshrc":  "PS1='$ '\nprecmd() { print -Pn '\\e]2;%n@%m:%~\\a' }\n",
	} {
		if err := os.WriteFile(filepath.Join(user, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("SHELL", zsh)
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", user)
	t.Setenv("TMPDIR", tmp)
	t.Setenv("TERM_PROGRAM", "")
	defer func(start func(terminal.Options) (*terminal.Terminal, error)) { startTerminal = start }(startTerminal)
	startTerminal = terminal.New
	exited := make(chan struct{})
	term, err := startShell(terminal.Options{Dir: deep, ShellIntegration: true, OnExit: func(int) { close(exited) }})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	tab := &termTab{t: term, cwd: deep}
	wait := func(what string, ok func() bool) {
		t.Helper()
		for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
			if ok() {
				return
			}
		}
		t.Fatalf("timed out waiting for %s: tab %q, title %q, screen:\n%s", what, termTitle(tab), term.Title(), term.Text())
	}
	wait("the folder", func() bool { return term.Dir() == deep && termTitle(tab) == "…/a/b/c 100%" })
	term.Send([]byte("cd ~/a\r"))
	wait("the folder after cd", func() bool { return term.Dir() == filepath.Join(home, "a") && termTitle(tab) == "~/a" })
	if !strings.Contains(term.Title(), ":~/a") {
		t.Errorf("the user's title hook did not run: %q", term.Title())
	}
	term.Send([]byte("echo \"zdotdir=$ZDOTDIR env=$FROM_ZSHENV leak=${STRATUM_ZDOTDIR-none}\"\r"))
	wait("the user's startup files", func() bool {
		return strings.Contains(term.Text(), "zdotdir="+user+" env=yes leak=none")
	})
	term.Send([]byte("exit\r"))
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("zsh did not exit")
	}
	if left, _ := filepath.Glob(filepath.Join(tmp, "stratum-zsh-*")); len(left) > 0 {
		t.Errorf("the shell left %v", left)
	}
}

func TestShortDirAsGhosttyTitlesTabs(t *testing.T) {
	for dir, want := range map[string]string{
		"/Users/me":                      "~",
		"/Users/me/Projects/stratum":     "~/Projects/stratum",
		"/Users/me/a/b/c":                "…/a/b/c",
		"/Users/me/a/b/c/d":              "…/b/c/d",
		"/Users/meg/x":                   "/Users/meg/x",
		"/":                              "/",
		"/opt/homebrew":                  "/opt/homebrew",
		"/Volumes/Disk/work/site/public": "…/work/site/public",
	} {
		if got := shortDir(dir, "/Users/me"); got != want {
			t.Errorf("shortDir(%q) = %q, want %q", dir, got, want)
		}
	}
}
