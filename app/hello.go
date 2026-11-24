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

// An empty session shows what its folder holds, as Craft's start page
// does, with the composer at the foot. A folder holding nothing has no
// overview: the composer itself sits on the canvas, a little above the
// middle, as Spotlight's field does, and goes down to the foot once the
// first message is sent, along a critically damped spring, the way the
// apps of ChatGPT, Claude and Codex take their composer down.

// hero is where the composer of an empty session stands.
type hero struct {
	// session is the session the state below is of; placed tells the
	// composer was put in its place, after the folder was read or the
	// wait for it ran out.
	session string
	placed  bool
	// empty says, by folder, whether one read last held nothing, so a
	// new session in it starts in place.
	empty map[string]bool
	sheet ui.Rect // the sheet the composer floats on, the frame before
	words ui.Rect // where the layout put the composer's words
}

const (
	// heroWait is how long the composer of a new session stays hidden
	// waiting for its folder to be read, before it shows at the foot.
	heroWait = 250 * time.Millisecond
	// heroLine is the share of the sheet over the composer when it sits on
	// the canvas, a little above the middle.
	heroLine = .4
	// heroRefH is how tall the composer is with a line in it, and
	// heroWordsAt how far its words are from its top: the words keep
	// still as lines push the card down and the attached row up.
	heroRefH    = 104
	heroWordsAt = 14
	// overviewMax is how many uncommitted files the overview lists.
	overviewMax = 3
)

// overview tells whether the current folder was read, from this
// session's answer or a last one, and whether it holds nothing.
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

// heroed says whether the composer belongs on the canvas.
func (a *App) heroed() bool {
	known, empty := a.overview()
	return a.helloShown() && known && empty
}

// placeHero moves the composer's dock up onto the canvas while the session
// is empty and its folder holds nothing, and fades a new session's in
// once it is placed. Its placeholder says what / and @ do, so nothing
// else is said under it.
//
// The move is a spring from where the composer is on screen, so a turn
// back halfway (the message taken back) goes back up along the same path;
// a new session starts in place, without a move.
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
		// The folder was read: the composer appears where it belongs.
		a.hero.placed = true
		root.Jump("hero", target)
		root.Jump("hero-fade", 0) // and fades in now, not after the wait
	}
	fade := ui.Animation{Duration: 160 * time.Millisecond, NonMotion: true}
	if !a.hero.placed {
		fade.Delay = heroWait
	}
	op := root.AnimateTo("hero-fade", 1, fade)
	if op > 0 {
		a.hero.placed = true
	}
	t := root.Spring("hero", target, springDefault)
	if t > .001 {
		// The dock keeps its place at the foot in the layout; it is drawn
		// higher, so that the words stay where they are as the card grows
		// down with lines and up with what is attached.
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

// hello is a new session: what its folder holds, in the middle, while the
// composer stays at the foot; nothing when the folder holds nothing and
// the composer is in the middle instead, or while the folder is read.
func (a *App) hello(c *ui.Context) {
	if known, empty := a.overview(); !known || empty {
		return
	}
	p := a.p
	cur := a.C.Current
	w := a.C.Workspace
	ui.Column(c.Key("overview")).Width(480).MaxWidthPercent(100).
		Transition(ui.ElementTransition{Enter: &ui.Motion{Opacity: 0}, Duration: 160 * time.Millisecond}).
		Children(func() {
			// The folder and its branch, and whether anything waits to be
			// committed: Finder's path bar and status line in one.
			ui.Row(c).Height(32).Gap(8).AlignItems(ui.Center).BorderWidth(0, 0, .5, 0).BorderColor(p.Edge).Children(func() {
				where := filepath.Base(cur.Cwd)
				if w.Branch != "" {
					where += " · " + w.Branch
				}
				ui.Text(c, where).FontSize(14).TextColor(p.Fg2).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
				ui.Spacer(c)
				switch {
				case len(w.Files) > 0:
					ui.Text(c, locales.UncommittedCount(len(w.Files))).FontSize(13).TextColor(p.Fg2).SingleLine().FontFeatures("tnum")
				case w.Branch != "":
					ui.Text(c, locales.NoUncommitted()).FontSize(13).TextColor(p.Mute).SingleLine()
				}
			})
			for i, f := range w.Files {
				if i == overviewMax {
					break
				}
				meta := lineCounts(f.Added, f.Removed)
				if a.overviewRow(c, "ov-diff:"+f.Path, "file-pencil", a.relPath(cur.Cwd, f.Path), meta, true).Clicked() {
					a.showUncommitted(f.Path)
				}
			}
			if len(w.Commits) > 0 {
				a.overviewSection(c, locales.RecentCommits())
				for _, cm := range w.Commits {
					a.overviewRow(c, "ov-commit:"+cm.Hash, "git-commit", cm.Subject, ago(a.now(), time.Unix(cm.Time, 0)), false)
				}
			}
			recent := a.recentNotListed()
			if len(recent) > 0 {
				a.overviewSection(c, locales.RecentFiles())
				for _, f := range recent {
					rel := a.relPath(cur.Cwd, f.Path)
					if a.overviewRow(c, "ov-file:"+f.Path, "file", rel, ago(a.now(), time.Unix(f.Time, 0)), true).Clicked() {
						a.rv.open, a.rv.tab = true, "files"
						a.openFilePreview(cur.Cwd, rel)
					}
				}
			}
		})
}

// recentNotListed are the recent files the overview does not list as
// uncommitted already.
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
	ui.Text(c, label).FontSize(12.5).LetterSpacing(.08).TextColor(a.p.Mute).Margin(14, 0, 4, 0).SingleLine()
}

// overviewRow is one line of the overview; one that opens something
// lights up under the pointer and takes the keyboard's focus, as a row of
// a Finder list does.
func (a *App) overviewRow(c *ui.Context, key, icon, text, meta string, opens bool) ui.Element {
	p := a.p
	var row ui.Element
	if opens {
		row = ui.ButtonBase(c.Key(key)).Row().Label(text)
		focusRing(row, p)
		if row.Hovered() {
			row.Background(p.Hover)
		}
	} else {
		row = ui.Row(c.Key(key))
	}
	row.Height(32).Padding(0, 6).Margin(0, -6).Gap(8).Radius(6).AlignItems(ui.Center)
	row.Children(func() {
		sym(c, icon, 15, p.Mute)
		ui.Text(c, text).FontSize(14.5).TextColor(p.Fg).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
		ui.Spacer(c)
		ui.Text(c, meta).FontSize(12.5).LetterSpacing(.08).TextColor(p.Mute).SingleLine().Shrink(0).FontFeatures("tnum")
	})
	return row
}

// showUncommitted opens the review panel on an uncommitted file's diff.
func (a *App) showUncommitted(path string) {
	a.rv.open, a.rv.tab, a.rv.uncommitted, a.rv.branch, a.rv.turn = true, "", true, false, 0
	key := client.DiffKey(0, path)
	a.rv.sel, a.rv.jump = key, key
}

// relPath is path as the folder sees it, or path when it is outside.
func (a *App) relPath(cwd, path string) string {
	if rel, err := filepath.Rel(cwd, path); err == nil && !filepath.IsAbs(rel) && rel != ".." && !hasDotDot(rel) {
		return rel
	}
	return path
}

func hasDotDot(rel string) bool {
	return len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator)
}

// lineCounts says the lines a file gained and lost, leaving out a zero.
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
