package app

import (
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

// Palette is the Mono palette of design/mockups.html: ink and paper for
// everything, blue only for what is alive.
type Palette struct {
	Bg, Side, Pane, Card    ui.Color // Bg is the window, under the sheets
	Sheet                   ui.Color // the paper the transcript is on
	Fg, Fg2, Mute, Faint    ui.Color
	Edge, Edge2, Sel, Hover ui.Color
	Chip, Bubble            ui.Color // Bubble holds the user's messages
	Acc, OnAcc              ui.Color // primary buttons
	Live, LiveBg            ui.Color // running, waiting on the user
	Plus, Minus             ui.Color // lines added and removed
}

var (
	ink        = ui.Hex("#12131A")
	pureWhite  = ui.Hex("#F4F4F5")
	neutralAsh = ui.Hex("#A1A1AA") // dark lines and fills are neutral gray, zero blue cast

	light = &Palette{
		Bg: ui.Hex("#ECEEF2"), Side: ui.RGBA(232, 234, 239, .86), Pane: ui.Hex("#FBFBFC"), Card: ui.Hex("#FFFFFF"), Sheet: ui.Hex("#FBFBFC"),
		Fg: ink, Fg2: ui.Hex("#5B6272"), Mute: ui.Hex("#8A91A0"), Faint: ui.Hex("#B7BCC6"),
		Edge: ink.Alpha(.10), Edge2: ink.Alpha(.16), Sel: ink.Alpha(.055), Hover: ink.Alpha(.035), Chip: ink.Alpha(.05), Bubble: ink.Alpha(.06),
		Acc: ink, OnAcc: ui.Hex("#F4F5F7"),
		Live: ui.Hex("#2B5FA6"), LiveBg: ui.Hex("#2B5FA6").Alpha(.08),
		Plus: ui.Hex("#3B7356"), Minus: ui.Hex("#A8474B"),
	}
	dark = &Palette{
		Bg: ui.Hex("#09090A"), Side: ui.RGBA(18, 18, 19, .90), Pane: ui.Hex("#111112"), Card: ui.Hex("#18181A"), Sheet: ui.Hex("#0D0D0E"),
		Fg: pureWhite, Fg2: ui.Hex("#A1A1AA"), Mute: ui.Hex("#71717A"), Faint: ui.Hex("#3F3F46"),
		Edge: neutralAsh.Alpha(.11), Edge2: neutralAsh.Alpha(.18), Sel: neutralAsh.Alpha(.10), Hover: neutralAsh.Alpha(.055), Chip: neutralAsh.Alpha(.08), Bubble: neutralAsh.Alpha(.12),
		Acc: pureWhite, OnAcc: ui.Hex("#09090A"),
		Live: ui.Hex("#E4E4E7"), LiveBg: ui.Hex("#E4E4E7").Alpha(.10),
		Plus: ui.Hex("#7DB89A"), Minus: ui.Hex("#DD878B"),
	}
)

// PaletteFor returns the palette of an appearance.
func PaletteFor(isDark bool) *Palette {
	if isDark {
		return dark
	}
	return light
}

// Theme maps a palette onto mygo's widgets, so stock controls match.
func Theme(p *Palette, isDark bool) *ui.Theme {
	t := ui.LightTheme()
	if isDark {
		t = ui.DarkTheme()
	}
	t.Background = p.Bg
	t.Surface = p.Card
	t.SurfaceHover = p.Card.Mix(p.Fg, .04)
	t.SurfacePressed = p.Card.Mix(p.Fg, .08)
	t.Border = p.Edge2
	t.Text, t.TextMuted = p.Fg, p.Fg2
	accent := p.Live
	if isDark {
		accent = ui.Hex("#3A75C4")
	}
	t.Accent, t.AccentText = accent, ui.Hex("#FFFFFF")
	t.AccentHover = accent.Mix(p.Bg, .12)
	t.AccentPressed = accent.Mix(p.Bg, .22)
	t.Danger, t.Success = p.Minus, p.Plus
	t.Selection = p.Live.Alpha(.22)
	t.Focus = p.Live.Alpha(.5)
	if isDark {
		t.Inverse, t.InverseText = p.Card.Mix(p.Fg, .08), p.Fg
	} else {
		t.Inverse, t.InverseText = ink, p.OnAcc
	}
	t.Radius = 7
	t.FontSize = 14
	return t
}

// raise lifts a sheet or a card off what is under it: a hairline edge and
// a soft shadow, deeper in the dark.
func raise(e ui.Element, p *Palette) ui.Element {
	if p == dark {
		return e.Shadow(0, 0, 0, .5, neutralAsh.Alpha(.12)).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, .3)).Shadow(0, 8, 24, -10, ui.RGBA(0, 0, 0, .5))
	}
	return e.Shadow(0, 0, 0, .5, ink.Alpha(.10)).Shadow(0, 1, 2, 0, ink.Alpha(.05)).Shadow(0, 6, 20, -8, ink.Alpha(.10))
}

// lift lifts what floats over the transcript, the composer and what
// waits on the user, a layer higher than raise.
func lift(e ui.Element, p *Palette) ui.Element {
	if p == dark {
		return e.Shadow(0, 0, 0, .5, neutralAsh.Alpha(.16)).Shadow(0, 2, 6, 0, ui.RGBA(0, 0, 0, .3)).Shadow(0, 20, 44, -16, ui.RGBA(0, 0, 0, .6))
	}
	return e.Shadow(0, 0, 0, .5, ink.Alpha(.12)).Shadow(0, 2, 6, 0, ink.Alpha(.06)).Shadow(0, 18, 40, -16, ink.Alpha(.22))
}

// press shrinks a control a little while it is held, using a critically
// damped spring so the feedback is interruptible on rapid taps.
func press(e ui.Element) ui.Element {
	target := float32(1)
	if e.Pressed() {
		target = .965
	}
	s := e.Spring("press", target, ui.SpringOptions{Stiffness: 650, Damping: 51})
	return e.Scale(s, s)
}

// focusRing gives keyboard-navigated controls a restrained Apple focus ring
// without showing a box on pointer clicks.
func focusRing(e ui.Element, p *Palette) ui.Element {
	return e.FocusRing(true).FocusRingColor(p.Live.Alpha(.45)).FocusRingOffset(1)
}

// disclosureChevron draws a chevron that smoothly rotates 0°↔90° with a
// critically damped spring instead of snapping between two glyphs.
func disclosureChevron(c *ui.Context, key string, open bool, size float32, col ui.Color) {
	target := float32(0)
	if open {
		target = 90
	}
	wrap := ui.Box(c.Key("chev:"+key)).Size(size+2, size+2).Center().Shrink(0)
	deg := wrap.Spring("deg", target, ui.SpringOptions{Stiffness: 480, Damping: 36})
	wrap.Rotate(deg).Children(func() {
		sym(c, "chevron-right", size, col)
	})
}

// emptyState draws a unified, calm empty placeholder across panes and sheets.
func emptyState(c *ui.Context, p *Palette, icon, title, subtitle string, action func()) {
	ui.Column(c).Grow(1).Center().Padding(28, 20).Gap(8).Children(func() {
		if icon != "" {
			sym(c, icon, 24, p.Mute)
		}
		if title != "" {
			ui.Text(c, title).FontSize(13.5).FontWeight(500).TextColor(p.Fg2).TextAlign(ui.Center).MaxWidth(280)
		}
		if subtitle != "" {
			ui.Text(c, subtitle).FontSize(12.5).LineHeight(1.45).TextColor(p.Mute).TextAlign(ui.Center).MaxWidth(280)
		}
		if action != nil {
			ui.Box(c).Margin(6, 0, 0, 0).Children(action)
		}
	})
}

// tintedGlass returns macOS Tinted Liquid Glass: balanced frosted tint and
// contrast over Clear so sidebar text stays sharp while preserving glass depth.
// glass puts g on e, or the solid card where macOS asks for less
// transparency (Accessibility › Display › Reduce transparency), as its own
// bars turn solid; with more contrast, its edge is darker too.
func (a *App) glass(e ui.Element, g glass.Glass) ui.Element {
	switch {
	case a.sys.ReduceTransparency || a.sys.HighContrast:
		e.Background(a.p.Card)
		if a.sys.HighContrast {
			e.Border(1, a.p.Fg2)
		}
	default:
		e.Material(g)
	}
	return e
}

func tintedGlass(p *Palette) glass.Glass {
	tint := ui.RGBA(244, 245, 248, .28)
	if p == dark {
		tint = ui.RGBA(20, 20, 22, .36)
	}
	return glass.Glass{Tint: tint, Native: true}
}

// glassButton returns an interactive Tinted Liquid Glass material that samples
// the app surface beneath it (Native: false) like Safari's luminous toolbar pill.
func glassButton(p *Palette) glass.Glass {
	tint := ui.RGBA(255, 255, 255, .52)
	if p == dark {
		tint = ui.RGBA(48, 48, 52, .48)
	}
	return glass.Glass{Tint: tint, Interactive: true}
}

func specular(p *Palette) ui.Color {
	if p == dark {
		return ui.RGBA(255, 255, 255, .13)
	}
	return ui.RGBA(255, 255, 255, .72)
}

// rim catches light along the top edge of lifted glass or paper surfaces.
func rim(e ui.Element, p *Palette) ui.Element {
	return e.InsetShadow(0, .5, 0, 0, specular(p))
}

// folderColor picks a calm, deterministic Apple accent color for a project name.
func folderColor(name string, isDark bool) ui.Color {
	lightColors := []ui.Color{
		ui.Hex("#2B5FA6"), ui.Hex("#3B7356"), ui.Hex("#5856D6"),
		ui.Hex("#C67D1A"), ui.Hex("#1D7D8C"), ui.Hex("#A8474B"),
	}
	darkColors := []ui.Color{
		ui.Hex("#5AA9FF"), ui.Hex("#5CC892"), ui.Hex("#8E8DF0"),
		ui.Hex("#F0AD4E"), ui.Hex("#4FC3D4"), ui.Hex("#E07A7E"),
	}
	var h uint32
	for i := 0; i < len(name); i++ {
		h = h*31 + uint32(name[i])
	}
	if isDark {
		return darkColors[int(h)%len(darkColors)]
	}
	return lightColors[int(h)%len(lightColors)]
}

// folderDot draws a soft colored halo dot representing a project folder.
func folderDot(c *ui.Context, p *Palette, name string) {
	col := folderColor(name, p == dark)
	ui.Box(c).Size(15, 15).Radius(7.5).Background(col.Alpha(.15)).Center().Shrink(0).Children(func() {
		ui.Box(c).Size(6.5, 6.5).Radius(3.25).Background(col)
	})
}
