package app

import (
	"embed"
	"sync"

	"github.com/egoist/mygo/plugins/sfsymbols"
	"github.com/egoist/mygo/ui"
)

// The icons are the system's SF Symbols, by sfNames, with Lucide's
// outline set (ISC) where the system has none: strokes of 1.75 on a 24 grid,
// in the color of the text (with brand/app marks kept separate).
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
	sf, ok := sfNames[name]
	if !ok {
		return ui.Icon(c, icon(name)).Size(size, size).TextColor(col).Shrink(0)
	}
	// The system's own symbol, drawn by AppKit, so the app's marks are
	// those of Finder and Mail and sit with SF Pro at its weight; the
	// Lucide drawing stands in where the system has no such symbol.
	return sfsymbols.Symbol(c, sf).Size(size).Color(col).Weight(sfsymbols.Medium).Fallback(icon(name)).
		Element.Shrink(0).AccessHidden(true)
}

// sfNames are the SF Symbols that stand for the app's Lucide names. The
// brand marks, Stratum's own, and the spinner keep their drawings.
var sfNames = map[string]string{
	"a-large-small":                "textformat.size",
	"accessibility":                "accessibility",
	"adjustments-horizontal":       "slider.horizontal.3",
	"adjustments":                  "slider.vertical.3",
	"alert-circle":                 "exclamationmark.circle",
	"archive":                      "archivebox",
	"arrow-back-up":                "arrow.uturn.backward",
	"arrow-forward-up":             "arrow.uturn.forward",
	"arrow-right":                  "arrow.right",
	"arrow-up":                     "arrow.up",
	"arrows-exchange":              "arrow.left.arrow.right",
	"bell":                         "bell",
	"brain":                        "brain",
	"bulb":                         "lightbulb",
	"check":                        "checkmark",
	"chevron-down":                 "chevron.down",
	"chevron-left":                 "chevron.left",
	"chevron-right":                "chevron.right",
	"chevron-up":                   "chevron.up",
	"clock-play":                   "play.circle",
	"clock":                        "clock",
	"copy":                         "doc.on.doc",
	"corner-down-right":            "arrow.turn.down.right",
	"cpu":                          "cpu",
	"device-laptop":                "laptopcomputer",
	"dots":                         "ellipsis",
	"edit":                         "square.and.pencil",
	"external-link":                "arrow.up.forward.app",
	"eye":                          "eye",
	"file-code":                    "chevron.left.forwardslash.chevron.right",
	"file-pencil":                  "pencil.line",
	"file-search":                  "doc.text.magnifyingglass",
	"file-text":                    "doc.text",
	"file":                         "doc",
	"files":                        "square.stack",
	"fold-down":                    "arrow.up.and.line.horizontal.and.arrow.down",
	"fold":                         "arrow.down.and.line.horizontal.and.arrow.up",
	"folder":                       "folder",
	"git-branch":                   "arrow.triangle.branch",
	"git-commit":                   "smallcircle.circle",
	"git-compare":                  "arrow.triangle.pull",
	"hierarchy-3":                  "point.3.connected.trianglepath.dotted",
	"history":                      "clock.arrow.circlepath",
	"info-circle":                  "info.circle",
	"key":                          "key",
	"keyboard":                     "keyboard",
	"layout-sidebar-left-collapse": "sidebar.left",
	"layout-sidebar-left-expand":   "sidebar.left",
	"layout-sidebar-right":         "sidebar.right",
	"layout-sidebar":               "sidebar.left",
	"list-check":                   "checklist",
	"list-details":                 "list.bullet.rectangle",
	"lock-open":                    "lock.open",
	"lock":                         "lock",
	"maximize-2":                   "arrow.up.left.and.arrow.down.right",
	"message-plus":                 "plus.bubble",
	"message":                      "bubble.left",
	"minimize-2":                   "arrow.down.right.and.arrow.up.left",
	"palette":                      "paintpalette",
	"pencil":                       "pencil",
	"pin":                          "pin",
	"player-pause":                 "pause.fill",
	"player-stop":                  "stop.fill",
	"plug-connected-x":             "bolt.slash",
	"plug-connected":               "powerplug",
	"plug":                         "powerplug",
	"plus-minus":                   "plusminus",
	"plus":                         "plus",
	"puzzle":                       "puzzlepiece.extension",
	"refresh":                      "arrow.clockwise",
	"route":                        "arrow.triangle.turn.up.right.diamond",
	"search":                       "magnifyingglass",
	"selector":                     "chevron.up.chevron.down",
	"send":                         "paperplane",
	"settings":                     "gearshape",
	"shield-half":                  "shield.lefthalf.filled",
	"shield-lock":                  "lock.shield",
	"shield-off":                   "shield.slash",
	"sparkles":                     "sparkles",
	"subtask":                      "list.bullet.indent",
	"terminal-2":                   "terminal",
	"tool":                         "wrench.and.screwdriver",
	"trash":                        "trash",
	"user-circle":                  "person.crop.circle",
	"world":                        "globe",
	"x":                            "xmark",
}

// brandMark renders the monochrome cutout Stratum emblem without a background
// plate, tinted to col so it adapts to light, dark, and vibrancy surfaces.
func brandMark(c *ui.Context, size float32, col ui.Color) ui.Element {
	return sym(c, "stratum", size, col)
}
