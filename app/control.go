package app

import (
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/locales"
)

// ControlIndicator tells the user that the agent uses the computer, as
// Claude's computer use does: a pill floating at the top of the main
// display while a turn uses it, and esc, anywhere, to stop the turn. The
// pill lets the mouse through and keeps out of screenshots, so the agent
// neither clicks it nor sees it.
type ControlIndicator struct {
	// op keeps the window and the shortcut in the order asked; mu keeps
	// the state, which esc reads on the main thread.
	op   sync.Mutex
	mu   sync.Mutex
	on   bool
	held int
	stop func()
	// The window and the esc shortcut; tests put fakes in their place.
	show, hide func()
	bindEsc    func(fn func()) error
	unbindEsc  func()
}

const pillW, pillH = 340, 40

// ControlIndicator makes the indicator, on the window and the global
// shortcut of the system.
func (a *App) ControlIndicator() *ControlIndicator {
	ci := &ControlIndicator{}
	var win *mygo.Window
	ci.show = func() {
		if win == nil {
			win = mygo.NewWindow(mygo.WindowOptions{
				Title: locales.AgentUsingComputer(), Width: pillW, Height: pillH,
				Panel: true, Frameless: true, Transparent: true, Hidden: true, DisableResize: true,
				AlwaysOnTop: true, IgnoreMouseEvents: true, DisableShadow: false,
				Vibrancy: mygo.VibrancyHUD, CornerRadius: pillH / 2,
				Content: ui.View(a.controlPill),
			})
			win.SetContentProtection(true)
		}
		wa := mygo.Screen.PrimaryDisplay().WorkArea
		win.SetPosition(wa.X+(wa.Width-pillW)/2, wa.Y+10)
		win.ShowInactive() // the app the agent uses keeps the keyboard
	}
	ci.hide = func() {
		if win != nil {
			win.Hide()
		}
	}
	ci.bindEsc = func(fn func()) error { return mygo.GlobalShortcut.Register("Escape", fn) }
	ci.unbindEsc = func() { mygo.GlobalShortcut.Unregister("Escape") }
	return ci
}

// Show shows the pill and takes esc to call stop.
func (ci *ControlIndicator) Show(stop func()) {
	ci.op.Lock()
	defer ci.op.Unlock()
	ci.mu.Lock()
	ci.on, ci.stop = true, stop
	held := ci.held
	ci.mu.Unlock()
	ci.show()
	if held == 0 {
		ci.bindEsc(ci.pressed)
	}
}

// Hide hides the pill and gives esc back.
func (ci *ControlIndicator) Hide() {
	ci.op.Lock()
	defer ci.op.Unlock()
	ci.mu.Lock()
	was, held := ci.on, ci.held
	ci.on, ci.stop = false, nil
	ci.mu.Unlock()
	if !was {
		return
	}
	ci.hide()
	if held == 0 {
		ci.unbindEsc()
	}
}

// Hold gives esc back while the agent itself presses it.
func (ci *ControlIndicator) Hold() (release func()) {
	ci.op.Lock()
	defer ci.op.Unlock()
	ci.mu.Lock()
	ci.held++
	first, on := ci.held == 1, ci.on
	ci.mu.Unlock()
	if first && on {
		ci.unbindEsc()
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			ci.op.Lock()
			defer ci.op.Unlock()
			ci.mu.Lock()
			ci.held--
			last, on := ci.held == 0, ci.on
			ci.mu.Unlock()
			if last && on {
				ci.bindEsc(ci.pressed)
			}
		})
	}
}

// pressed is esc: it stops the turns that use the computer.
func (ci *ControlIndicator) pressed() {
	ci.mu.Lock()
	stop := ci.stop
	ci.mu.Unlock()
	if stop != nil {
		go stop()
	}
}

// controlPill is the pill: a dot, what is going on, and the key that
// stops it.
func (a *App) controlPill(c *ui.Context) {
	dark := c.Theme().Dark
	p := PaletteFor(dark)
	c.Root().Background(ui.Transparent)
	ui.Row(c).Grow(1).Gap(9).AlignItems(ui.Center).Padding(0, 16, 0, 15).Children(func() {
		// A still dot, as the system's own for the camera and recording.
		ui.Box(c).Size(8, 8).Radius(4).Background(p.Live).Shrink(0)
		ui.Text(c, locales.AgentUsingComputer()).FontSize(13).FontWeight(600).TextColor(p.Fg).SingleLine().Ellipsis("…").MinWidth(0).Grow(1)
		ui.Text(c, "esc").FontSize(11).FontWeight(500).TextColor(p.Fg2).Padding(1, 6).Radius(5).
			Border(.5, p.Edge2).SingleLine().Shrink(0)
		ui.Text(c, locales.Stop()).FontSize(12.5).TextColor(p.Fg2).SingleLine().Shrink(0)
	})
}
