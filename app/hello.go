package app

import (
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/shqingda/stratum/client"
	"github.com/shqingda/stratum/locales"
	"github.com/shqingda/stratum/proto"
)

type hero struct {
	session string
	placed  bool
	empty   map[string]bool
	sheet   ui.Rect
	words   ui.Rect
}

const (
	heroWait    = 250 * time.Millisecond
	heroLine    = .4
	heroRefH    = 112
	heroWordsAt = 16
	overviewMax = 3
)

func (a *App) overview() (known, empty bool) {
	cur := a.C.Current
	if cur == nil {
		return false, false
	}
	w := a.C.Workspace
	if w.Read > 0 {
		empty = len(w.Files) == 0 && len(w.Commits) == 0 && len(w.Recent) == 0
		if a.hero.empty == nil {
			a.hero.empty = map[string]bool{}
		}
		a.hero.empty[cur.Cwd] = empty
		return true, empty
	}
	empty, known = a.hero.empty[cur.Cwd]
	return known, empty
}

func (a *App) heroed() bool {
	known, empty := a.overview()
	return a.helloShown() && known && empty
}

func (a *App) placeHero(c *ui.Context, dock ui.Element) {
	root := c.Root()
	target := float32(0)
	if a.heroed() {
		target = 1
	}
	known, _ := a.overview()
	if id := a.C.Current.ID; a.hero.session != id {
		a.hero.session = id
		a.hero.placed = !a.helloShown() || known
		root.Jump("hero", target)
		fade := float32(0)
		if a.hero.placed {
			fade = 1
		}
		root.Jump("hero-fade", fade)
	} else if !a.hero.placed && known {
		a.hero.placed = true
		root.Jump("hero", target)
		root.Jump("hero-fade", 0)
	}
	fade := ui.Animation{Duration: 180 * time.Millisecond, Ease: easeEmphasizedDecelerate, NonMotion: true}
	if !a.hero.placed {
		fade.Delay = heroWait
	}
	op := root.AnimateTo("hero-fade", 1, fade)
	if op > 0 {
		a.hero.placed = true
	}
	t := root.AnimateTo("hero", target, animEnter)
	if t > .001 {
		words, sheet := a.hero.words, a.hero.sheet
		if sheet.H > 0 && words.H > 0 {
			top := sheet.Y + (sheet.H-heroRefH)*heroLine + heroWordsAt
			if dy := words.Y - top; dy > 0 {
				dock.Translate(0, -dy*t)
			}
		}
	}
	if op < .999 {
		dock.Opacity(op)
	}
}

// hello renders an expressive Material You × FlClash Bento workspace dashboard.
func (a *App) hello(c *ui.Context) {
	if known, empty := a.overview(); !known || empty {
		return
	}
	p := a.p
	cur := a.C.Current
	w := a.C.Workspace
	recent := a.recentNotListed()
	commitsCard := func() {
		ui.Column(c).FillWidth().Radius(18).Background(p.SurfaceContainerLow).Padding(14, 16).Gap(4).Children(func() {
			a.overviewSection(c, locales.RecentCommits())
			for _, cm := range w.Commits {
				a.overviewRow(c, "ov-commit:"+cm.Hash, "git-commit", cm.Subject, ago(a.now(), time.Unix(cm.Time, 0)), false)
			}
		})
	}
	recentCard := func() {
		ui.Column(c).FillWidth().Radius(18).Background(p.SurfaceContainerLow).Padding(14, 16).Gap(4).Children(func() {
			a.overviewSection(c, locales.RecentFiles())
			for _, f := range recent {
				rel := a.relPath(cur.Cwd, f.Path)
				if a.overviewRow(c, "ov-file:"+f.Path, "file", rel, ago(a.now(), time.Unix(f.Time, 0)), true).Clicked() {
					a.rv.open, a.rv.tab = true, "files"
					a.openFilePreview(cur.Cwd, rel)
				}
			}
		})
	}

	ui.Column(c.Key("overview")).Width(720).MaxWidthPercent(100).Gap(12).
		Transition(ui.ElementTransition{Enter: &ui.Motion{Opacity: 0}, Duration: 180 * time.Millisecond}).
		Children(func() {
			// Unboxed Typographic Workspace Header
			ui.Column(c).Padding(2, 4, 4, 4).Gap(10).Children(func() {
				ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Row(c).Grow(1).MinWidth(0).Gap(10).AlignItems(ui.Center).Children(func() {
						ui.Text(c, filepath.Base(cur.Cwd)).FontSize(20).FontWeight(700).TextColor(p.OnSurface).SingleLine().Ellipsis("…").MinWidth(0)
						if w.Branch != "" {
							ui.Row(c).Gap(4).AlignItems(ui.Center).Shrink(0).Children(func() {
								sym(c, "git-branch", 12, p.OnSurfaceVariant)
								ui.Text(c, w.Branch).FontSize(12.5).FontWeight(500).TextColor(p.OnSurfaceVariant).SingleLine()
							})
						}
					})
					switch {
					case len(w.Files) > 0:
						ui.Row(c).Height(24).Gap(6).AlignItems(ui.Center).Shrink(0).Children(func() {
							ui.Box(c).Size(6, 6).Radius(3).Background(p.Primary)
							ui.Text(c, locales.UncommittedCount(len(w.Files))).FontSize(12).FontWeight(500).TextColor(p.OnSurfaceVariant).SingleLine().FontFeatures("tnum")
						})
					case w.Branch != "":
						ui.Row(c).Height(24).Gap(5).AlignItems(ui.Center).Shrink(0).Children(func() {
							sym(c, "check", 12, p.OnSurfaceVariant)
							ui.Text(c, locales.NoUncommitted()).FontSize(12).FontWeight(500).TextColor(p.OnSurfaceVariant).SingleLine()
						})
					}
				})
			})

			// 2-Column Bento Grid
			switch {
			case len(w.Files) > 0:
				ui.Row(c).Gap(12).AlignItems(ui.Stretch).Children(func() {
					ui.Column(c).Grow(1).Basis(0).MinWidth(0).Radius(18).Background(p.SurfaceContainerLow).Padding(14, 16).Gap(4).Children(func() {
						a.overviewSection(c, locales.Changes())
						for i, f := range w.Files {
							if i == overviewMax {
								break
							}
							meta := lineCounts(f.Added, f.Removed)
							if a.overviewRow(c, "ov-diff:"+f.Path, "file-pencil", a.relPath(cur.Cwd, f.Path), meta, true).Clicked() {
								a.showUncommitted(f.Path)
							}
						}
					})
					if len(w.Commits) > 0 || len(recent) > 0 {
						ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(12).Children(func() {
							if len(w.Commits) > 0 {
								commitsCard()
							}
							if len(recent) > 0 {
								recentCard()
							}
						})
					}
				})
			case len(w.Commits) > 0 && len(recent) > 0:
				ui.Row(c).Gap(12).AlignItems(ui.Stretch).Children(func() {
					ui.Column(c).Grow(1).Basis(0).MinWidth(0).Children(func() { commitsCard() })
					ui.Column(c).Grow(1).Basis(0).MinWidth(0).Children(func() { recentCard() })
				})
			default:
				if len(w.Commits) > 0 {
					commitsCard()
				}
				if len(recent) > 0 {
					recentCard()
				}
			}
		})
}

func (a *App) recentNotListed() []proto.RecentFile {
	w := a.C.Workspace
	listed := map[string]bool{}
	for i, f := range w.Files {
		if i == overviewMax {
			break
		}
		listed[f.Path] = true
	}
	var out []proto.RecentFile
	for _, f := range w.Recent {
		if !listed[f.Path] {
			out = append(out, f)
		}
	}
	return out
}

func (a *App) overviewSection(c *ui.Context, label string) {
	ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(a.p.OnSurfaceVariant).Margin(2, 0, 6, 4).SingleLine()
}

func (a *App) overviewRow(c *ui.Context, key, icon, text, meta string, opens bool) ui.Element {
	p := a.p
	var row ui.Element
	if opens {
		row = ui.ButtonBase(c.Key(key)).Row().Label(text)
		m3Focus(row, p)
		m3State(row, p, ui.Transparent, false)
	} else {
		row = ui.Row(c.Key(key))
	}
	row.Height(32).Padding(0, 8).Gap(8).Radius(10).AlignItems(ui.Center)
	row.Children(func() {
		sym(c, icon, 15, p.OnSurfaceVariant)
		ui.Text(c, text).FontSize(13.5).TextColor(p.OnSurface).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
		ui.Spacer(c)
		ui.Text(c, meta).FontSize(12).TextColor(p.OnSurfaceVariant).SingleLine().Shrink(0).FontFeatures("tnum")
	})
	return row
}

func (a *App) showUncommitted(path string) {
	a.rv.open, a.rv.tab, a.rv.uncommitted, a.rv.branch, a.rv.turn = true, "", true, false, 0
	key := client.DiffKey(0, path)
	a.rv.sel, a.rv.jump = key, key
}

func (a *App) relPath(cwd, path string) string {
	if rel, err := filepath.Rel(cwd, path); err == nil && !filepath.IsAbs(rel) && rel != ".." && !hasDotDot(rel) {
		return rel
	}
	return path
}

func hasDotDot(rel string) bool {
	return len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator)
}

func lineCounts(added, removed int) string {
	var parts []string
	if added > 0 || removed == 0 {
		parts = append(parts, "+"+strconv.Itoa(added))
	}
	if removed > 0 {
		parts = append(parts, "−"+strconv.Itoa(removed))
	}
	return strings.Join(parts, " ")
}
