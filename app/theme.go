package app

import "github.com/egoist/mygo/ui"

// Palette is the Mono palette of design/mockups.html: ink and paper for
// everything, blue only for what is alive.
type Palette struct {
	Bg, Side, Pane, Card    ui.Color
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
		Bg: ui.Hex("#F4F5F7"), Side: ui.RGBA(234, 236, 240, .92), Pane: ui.Hex("#FBFBFC"), Card: ui.Hex("#FFFFFF"),
		Fg: ink, Fg2: ui.Hex("#5B6272"), Mute: ui.Hex("#8A91A0"), Faint: ui.Hex("#B7BCC6"),
		Edge: ink.Alpha(.10), Edge2: ink.Alpha(.16), Sel: ink.Alpha(.055), Hover: ink.Alpha(.035), Chip: ink.Alpha(.05), Bubble: ink.Alpha(.06),
		Acc: ink, OnAcc: ui.Hex("#F4F5F7"),
		Live: ui.Hex("#2B5FA6"), LiveBg: ui.Hex("#2B5FA6").Alpha(.08),
		Plus: ui.Hex("#3B7356"), Minus: ui.Hex("#A8474B"),
	}
	dark = &Palette{
		Bg: ui.Hex("#12131A"), Side: ui.RGBA(23, 25, 34, .94), Pane: ui.Hex("#15161E"), Card: ui.Hex("#1C1E28"),
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
	t.Accent, t.AccentText = p.Acc, p.OnAcc
	t.AccentHover = p.Acc.Mix(p.Bg, .12)
	t.AccentPressed = p.Acc.Mix(p.Bg, .22)
	t.Danger, t.Success = p.Minus, p.Plus
	t.Selection = p.Live.Alpha(.22)
	t.Focus = p.Live.Alpha(.5)
	t.Radius = 7
	t.FontSize = 14
	return t
}
