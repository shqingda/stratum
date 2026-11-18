package app

import (
	"time"

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
	ink  = ui.Hex("#12131A")
	mist = ui.Hex("#E8EAEF")
	haze = ui.Hex("#A9B1C1") // dark lines and fills are this, faint

	light = &Palette{
		Bg: ui.Hex("#ECEEF2"), Side: ui.RGBA(232, 234, 239, .86), Pane: ui.Hex("#FBFBFC"), Card: ui.Hex("#FFFFFF"), Sheet: ui.Hex("#FBFBFC"),
		Fg: ink, Fg2: ui.Hex("#5B6272"), Mute: ui.Hex("#8A91A0"), Faint: ui.Hex("#B7BCC6"),
		Edge: ink.Alpha(.10), Edge2: ink.Alpha(.16), Sel: ink.Alpha(.055), Hover: ink.Alpha(.035), Chip: ink.Alpha(.05), Bubble: ink.Alpha(.06),
		Acc: ink, OnAcc: ui.Hex("#F4F5F7"),
		Live: ui.Hex("#2B5FA6"), LiveBg: ui.Hex("#2B5FA6").Alpha(.08),
		Plus: ui.Hex("#3B7356"), Minus: ui.Hex("#A8474B"),
	}
	dark = &Palette{
		Bg: ui.Hex("#0E0F14"), Side: ui.RGBA(22, 24, 32, .9), Pane: ui.Hex("#15161E"), Card: ui.Hex("#1C1E28"), Sheet: ui.Hex("#15161E"),
		Fg: mist, Fg2: ui.Hex("#A9B1C1"), Mute: ui.Hex("#6E7587"), Faint: ui.Hex("#454A58"),
		Edge: haze.Alpha(.10), Edge2: haze.Alpha(.16), Sel: haze.Alpha(.09), Hover: haze.Alpha(.05), Chip: haze.Alpha(.08), Bubble: haze.Alpha(.12),
		Acc: mist, OnAcc: ui.Hex("#12131A"),
		Live: ui.Hex("#7AA8EA"), LiveBg: ui.Hex("#7AA8EA").Alpha(.10),
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
		return e.Shadow(0, 0, 0, .5, haze.Alpha(.12)).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, .3)).Shadow(0, 8, 24, -10, ui.RGBA(0, 0, 0, .5))
	}
	return e.Shadow(0, 0, 0, .5, ink.Alpha(.10)).Shadow(0, 1, 2, 0, ink.Alpha(.05)).Shadow(0, 6, 20, -8, ink.Alpha(.10))
}

// lift lifts what floats over the transcript, the composer and what
// waits on the user, a layer higher than raise.
func lift(e ui.Element, p *Palette) ui.Element {
	if p == dark {
		return e.Shadow(0, 0, 0, .5, haze.Alpha(.16)).Shadow(0, 2, 6, 0, ui.RGBA(0, 0, 0, .3)).Shadow(0, 20, 44, -16, ui.RGBA(0, 0, 0, .6))
	}
	return e.Shadow(0, 0, 0, .5, ink.Alpha(.12)).Shadow(0, 2, 6, 0, ink.Alpha(.06)).Shadow(0, 18, 40, -16, ink.Alpha(.22))
}

// press shrinks a control a little while it is held, as a key gives.
func press(e ui.Element) ui.Element {
	s := float32(1)
	if e.Pressed() {
		s = .97
	}
	return e.AnimatePose(ui.Pose{Scale: s}, ui.Animation{Duration: 100 * time.Millisecond})
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
