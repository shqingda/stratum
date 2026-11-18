package app

import (
	"embed"
	"sync"

	"github.com/egoist/mygo/ui"
)

// The icons are Tabler's outline set (MIT), as the mockups draw them:
// strokes of 2 on a 24 grid, in the color of the text.
//
//go:embed icons/*.svg
var iconFiles embed.FS

var (
	iconMu sync.Mutex
	icons  = map[string]*ui.SVG{}
)

// icon is the Tabler icon of that name, parsed once; a missing name
// panics, as it is a mistake in the program.
func icon(name string) *ui.SVG {
	iconMu.Lock()
	defer iconMu.Unlock()
	if s, ok := icons[name]; ok {
		return s
	}
	data, err := iconFiles.ReadFile("icons/" + name + ".svg")
	if err != nil {
		panic("app: no icon " + name)
	}
	s := ui.MustParseSVG(data)
	icons[name] = s
	return s
}

// sym shows an icon size points square, as the mockups' icon font does at
// that font size.
func sym(c *ui.Context, name string, size float32, col ui.Color) ui.Element {
	return ui.Icon(c, icon(name)).Size(size, size).TextColor(col).Shrink(0)
}

// brandMark renders the monochrome cutout Talos emblem without a background
// plate, tinted to col so it adapts to light, dark, and vibrancy surfaces.
func brandMark(c *ui.Context, size float32, col ui.Color) ui.Element {
	return sym(c, "talos", size, col)
}

// heroMark renders the specular metallic Talos emblem for the Welcome canvas
// and empty states, using light or dark brushed metal gradients.
func heroMark(c *ui.Context, size float32, isDark bool) ui.Element {
	name := "talos-hero-light"
	if isDark {
		name = "talos-hero-dark"
	}
	return ui.Icon(c, icon(name)).Size(size, size).Shrink(0)
}
