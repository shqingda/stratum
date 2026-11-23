package computer

import (
	"context"
	"slices"
	"strings"
	"sync"
)

// Tier is how much the agent may do in an app, as in Claude's computer
// use: browsers only show, terminals and editors take only clicks, and
// other apps take everything.
type Tier string

const (
	// TierRead shows the app in screenshots; it takes no input. The
	// browser tool is for web pages.
	TierRead Tier = "read"
	// TierClick takes left clicks and scrolling, but no typing, keys,
	// right clicks or drags. The shell tool is for commands.
	TierClick Tier = "click"
	// TierFull takes everything.
	TierFull Tier = "full"
)

var tierRank = map[Tier]int{TierRead: 1, TierClick: 2, TierFull: 3}

// Allows reports whether an app at tier t takes what needs tier need.
func (t Tier) Allows(need Tier) bool { return tierRank[t] >= tierRank[need] }

// TierOf is the tier an app is granted at.
func TierOf(a App) Tier {
	id, name := strings.ToLower(a.ID), strings.ToLower(a.Name)
	if browserIDs[id] || browserNames[name] {
		return TierRead
	}
	if codeIDs[id] || codeNames[name] || strings.HasPrefix(id, "com.jetbrains.") {
		return TierClick
	}
	return TierFull
}

func set(names ...string) map[string]bool {
	m := map[string]bool{}
	for _, n := range names {
		m[strings.ToLower(n)] = true
	}
	return m
}

var (
	browserIDs = set("com.apple.Safari", "com.apple.SafariTechnologyPreview",
		"com.google.Chrome", "com.google.Chrome.beta", "com.google.Chrome.dev", "com.google.Chrome.canary",
		"org.chromium.Chromium", "org.mozilla.firefox", "org.mozilla.firefoxdeveloperedition", "org.mozilla.nightly",
		"com.microsoft.edgemac", "com.microsoft.edgemac.Beta", "com.microsoft.edgemac.Dev", "com.microsoft.edgemac.Canary",
		"company.thebrowser.Browser", "company.thebrowser.dia", "com.brave.Browser", "com.brave.Browser.beta",
		"com.brave.Browser.nightly", "com.operasoftware.Opera", "com.operasoftware.OperaGX", "com.vivaldi.Vivaldi",
		"com.kagi.kagimacOS", "app.zen-browser.zen", "com.duckduckgo.macos.browser", "ai.perplexity.comet",
		"com.openai.atlas", "org.torproject.torbrowser", "net.waterfox.waterfox", "io.gitlab.librewolf-community",
		"com.sigmaos.sigmaos.macos", "com.pushplaylabs.sidekick")
	browserNames = set("Safari", "Safari Technology Preview", "Google Chrome", "Chromium", "Firefox", "Microsoft Edge",
		"Arc", "Dia", "Brave Browser", "Opera", "Vivaldi", "Orion", "Zen", "Zen Browser", "DuckDuckGo", "Comet",
		"ChatGPT Atlas", "Tor Browser", "Waterfox", "LibreWolf", "SigmaOS", "Sidekick")
	codeIDs = set("com.apple.Terminal", "com.googlecode.iterm2", "dev.warp.Warp-Stable", "com.mitchellh.ghostty",
		"org.alacritty", "net.kovidgoyal.kitty", "com.github.wez.wezterm", "co.zeit.hyper", "org.tabby",
		"com.microsoft.VSCode", "com.microsoft.VSCodeInsiders", "com.vscodium", "com.todesktop.230313mzl4w4u92",
		"com.exafunction.windsurf", "dev.zed.Zed", "dev.zed.Zed-Preview", "com.apple.dt.Xcode", "com.panic.Nova",
		"com.barebones.bbedit", "com.sublimetext.4", "com.sublimetext.3", "com.macromates.TextMate",
		"com.google.android.studio", "org.eclipse.platform.ide", "org.gnu.Emacs", "org.vim.MacVim",
		"com.neovide.neovide", "com.apple.ScriptEditor2", "com.apple.Automator")
	codeNames = set("Terminal", "iTerm", "iTerm2", "Warp", "Ghostty", "Alacritty", "kitty", "WezTerm", "Hyper", "Tabby",
		"Visual Studio Code", "Visual Studio Code - Insiders", "Code", "VSCodium", "Cursor", "Windsurf", "Zed",
		"Zed Preview", "Xcode", "Nova", "BBEdit", "Sublime Text", "TextMate", "Android Studio", "Eclipse", "Emacs",
		"MacVim", "Neovide", "Kiro", "Trae", "Antigravity", "Script Editor", "Automator")
)

// Grant is an app the user let a session use, and how much.
type Grant struct {
	App
	Tier Tier `json:"tier"`
}

// use is a session's computer use: the apps granted and the display seen.
type use struct {
	apps    map[string]Grant
	display int // the display's ID; 0 is the main display
}

var (
	usesMu sync.Mutex
	uses   = map[string]*use{}
)

// GrantApps lets a session use apps, each at its tier, until Stratum
// quits.
func GrantApps(session string, apps []App) []Grant {
	usesMu.Lock()
	defer usesMu.Unlock()
	u := uses[session]
	if u == nil {
		u = &use{apps: map[string]Grant{}}
		uses[session] = u
	}
	var out []Grant
	for _, a := range apps {
		if a.Self {
			continue
		}
		g := Grant{App: a, Tier: TierOf(a)}
		u.apps[a.key()] = g
		out = append(out, g)
	}
	return out
}

// Granted lists the apps a session may use, by name.
func Granted(session string) []Grant {
	usesMu.Lock()
	defer usesMu.Unlock()
	var out []Grant
	if u := uses[session]; u != nil {
		for _, g := range u.apps {
			out = append(out, g)
		}
	}
	slices.SortFunc(out, func(a, b Grant) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// GrantFor is the grant of an app in a session, if any.
func GrantFor(session string, a App) (Grant, bool) {
	usesMu.Lock()
	defer usesMu.Unlock()
	if u := uses[session]; u != nil {
		g, ok := u.apps[a.key()]
		return g, ok
	}
	return Grant{}, false
}

// Forget drops what a session was granted, and its use of the computer.
func Forget(session string) {
	usesMu.Lock()
	delete(uses, session)
	usesMu.Unlock()
	indMu.Lock()
	t := using[session]
	indMu.Unlock()
	if t != nil {
		close(t.gone)
		endUse(session, t)
	}
}

// ShownDisplay is the ID of the display a session sees, 0 for the main.
func ShownDisplay(session string) int {
	usesMu.Lock()
	defer usesMu.Unlock()
	if u := uses[session]; u != nil {
		return u.display
	}
	return 0
}

// ShowDisplay makes a session see another display.
func ShowDisplay(session string, id int) {
	usesMu.Lock()
	defer usesMu.Unlock()
	if u := uses[session]; u != nil {
		u.display = id
	}
}

// Indicator tells the user that the agent uses the computer, and lets
// them stop it with esc, as Claude's computer use does.
type Indicator interface {
	// Show shows it; stop ends the turns that use the computer.
	Show(stop func())
	Hide()
	// Hold lets esc through while the agent itself presses it.
	Hold() (release func())
}

var (
	indMu     sync.Mutex
	indicator Indicator
	using     = map[string]*turn{}
)

// turn is a session's turn that uses the computer.
type turn struct {
	ctx  context.Context
	stop func()
	gone chan struct{} // closed when the session is forgotten
}

// SetIndicator sets how the user sees that the agent uses the computer.
func SetIndicator(i Indicator) {
	indMu.Lock()
	indicator = i
	indMu.Unlock()
}

// Using marks a session's turn as using the computer until ctx ends: the
// indicator shows meanwhile, and esc calls stop.
func Using(ctx context.Context, session string, stop func()) {
	indMu.Lock()
	if t, ok := using[session]; ok && t.ctx.Err() == nil {
		indMu.Unlock()
		return
	}
	first := len(using) == 0
	t := &turn{ctx: ctx, stop: stop, gone: make(chan struct{})}
	using[session] = t
	i := indicator
	indMu.Unlock()
	if first && i != nil {
		i.Show(stopAll)
	}
	go func() {
		select {
		case <-ctx.Done():
		case <-t.gone:
		}
		endUse(session, t)
	}()
}

// endUse ends a turn's use of the computer, hiding the indicator after
// the last.
func endUse(session string, t *turn) {
	indMu.Lock()
	last := false
	if using[session] == t {
		delete(using, session)
		last = len(using) == 0
	}
	i := indicator
	indMu.Unlock()
	if last && i != nil {
		i.Hide()
	}
}

// stopAll stops every turn that uses the computer: the user pressed esc.
func stopAll() {
	indMu.Lock()
	var fns []func()
	for _, t := range using {
		if t.stop != nil {
			fns = append(fns, t.stop)
		}
	}
	indMu.Unlock()
	for _, f := range fns {
		f()
	}
}

// HoldEsc lets esc through while the agent itself presses it.
func HoldEsc() (release func()) {
	indMu.Lock()
	i, on := indicator, len(using) > 0
	indMu.Unlock()
	if i == nil || !on {
		return func() {}
	}
	return i.Hold()
}
