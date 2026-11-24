package app

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/shqingda/stratum/client"
	"github.com/shqingda/stratum/locales"
	"github.com/shqingda/stratum/proto"
)

const columnWidth = 660

// tools is how a tool call shows: its symbol and verb.
// toolInfo is how a tool shows in the transcript.
type toolInfo struct{ symbol, verb string }

// toolFor looks up a built-in tool by name.
func toolFor(name string) (toolInfo, bool) {
	t, ok := tools[name]
	return toolInfo{t.symbol, func() string {
		if ok {
			return t.verb()
		}
		return ""
	}()}, ok
}

var tools = map[string]struct {
	symbol string
	verb   func() string
}{
	"read_file":   {"file-text", locales.ToolRead},
	"write_file":  {"file-pencil", locales.ToolWrite},
	"edit_file":   {"pencil", locales.ToolEdit},
	"list_dir":    {"folder", locales.ToolList},
	"search":      {"search", locales.ToolSearch},
	"shell":       {"terminal-2", locales.ToolRun},
	"git_status":  {"git-branch", func() string { return "git status" }},
	"git_diff":    {"plus-minus", func() string { return "git diff" }},
	"skill":       {"sparkles", locales.ToolSkill},
	"web_fetch":   {"world", locales.ToolWebFetch},
	"web_search":  {"world", locales.ToolWebSearch},
	"memory":      {"brain", locales.ToolMemory},
	"diagnostics": {"alert-circle", locales.ToolDiagnostics},
	"lsp":         {"file-search", locales.ToolLsp},
	"preview":     {"world", locales.ToolPreview},
	"browser":     {"world", locales.Browser},
	"computer":    {"device-laptop", locales.ComputerUse},
	"update_plan": {"list-check", locales.ToolPlan},
	"ask_user":    {"message", locales.ToolAsk},
	"task":        {"subtask", locales.ToolTask},
	"run_tasks":   {"hierarchy-3", locales.ToolTasks},
}

var kinds = map[string]func() string{"read": locales.KindRead, "write": locales.KindWrite, "exec": locales.KindExec, "mcp": locales.KindMcp, "net": locales.KindNet, "computer": locales.ComputerUse}

// reasons put the kernel's reasons for asking in the app's words.
var reasons = map[string]func() string{
	"It reaches outside the workspace.":                                           locales.ReasonOutsideWorkspace,
	"It runs without the sandbox: with network and access outside the workspace.": locales.ReasonNoSandbox,
	"Commands cannot be sandboxed here.":                                          locales.ReasonCantSandbox,
	"It runs a command.":                                                          locales.ReasonRunsCommand,
	"It changes files.":                                                           locales.ReasonChangesFiles,
	"It calls a tool of an MCP server, outside the sandbox.":                      locales.ReasonMcp,
	"It touches files that may hold secrets.":                                     locales.ReasonSecrets,
	"It reaches a site on the web.":                                               locales.ReasonNet,
	"It sees your screen and uses your mouse and keyboard.":                       locales.ReasonComputer,
	"It asks to see and use these apps until Stratum quits.":                      locales.ReasonComputerApps,
}

// modeName names a permission mode.
func modeName(mode string) string {
	switch mode {
	case "ask":
		return locales.ModeAsk()
	case "auto":
		return locales.ModeAuto()
	case "full":
		return locales.ModeFull()
	}
	return mode
}

func (a *App) bar(c *ui.Context) {
	p := a.p
	// Past the sidebar the title starts at 20; with the sidebar in, past
	// its 74-point toggle pill; in between, as far as the sidebar is.
	shut := toggleLeft(c) + 74 + 12
	left := float32(math.Round(float64(22*a.sideOpen + shut*(1-a.sideOpen))))
	ui.Row(c).Height(sheetBar).Shrink(0).Padding(0, 10, 0, left).Gap(10).AlignItems(ui.Center).DragWindow().Children(func() {
		cur := a.C.Current
		if cur == nil {
			ui.Text(c, phaseText(a.C.Phase)).FontSize(14.5).FontWeight(600).TextColor(p.Mute)
			return
		}
		if !a.helloShown() {
			if a.renaming == cur.ID && a.renameIn == "bar" {
				a.renameInput(c, *cur).FontSize(14.5).FontWeight(600).MaxWidth(360)
			} else {
				// Clicked, the title can be changed where it is.
				t := ui.ButtonBase(c.Key("bar-title")).Row().MinWidth(0).Shrink(1).Padding(3, 7).Margin(0, 0, 0, -7).Radius(6).
					Label(locales.Rename()).Tooltip(locales.Rename())
				press(t)
				focusRing(t, p)
				if t.Hovered() {
					t.Background(p.Hover)
				}
				if t.Clicked() {
					a.rename(*cur, "bar")
				}
				t.Children(func() {
					ui.Text(c, title(*cur)).FontSize(14.5).FontWeight(600).TextColor(p.Fg).SingleLine().Ellipsis("…").MinWidth(0)
				})
			}
		}
		a.workspaceCapsule(c, cur)
		ui.Spacer(c)
		a.reviewToggle(c)
	})
}

func (a *App) transcript(c *ui.Context) {
	p := a.p
	if len(a.C.Entries) == 0 && len(a.C.Approvals) == 0 {
		a.listShown = nil
		if a.C.Current != nil {
			delete(a.lists, a.C.Current.ID)
		}
		a.empty(c)
		return
	}
	items := a.items()
	if a.C.Older {
		items = append([]item{{older: true}}, items...)
	}
	a.seek(items)
	a.findIn(items)
	hit := a.hit()
	st := a.transcriptList()
	st.Key = func(i int) any {
		if i < len(items) {
			return items[i].key()
		}
		return "running"
	}
	st.FollowEnd = true
	st.FollowAnimation = nil
	a.md.Theme = mdTheme(p, c.Theme())
	a.md.CodeBlock = a.codeBlock
	n := len(items)
	if a.thinking() {
		n++
	}
	list := ui.List(c, st, n, func(i int) {
		row := ui.Row(c).Justify(ui.Center).Padding(0, 28)
		row.Children(func() {
			col := ui.Column(c).Grow(1).MaxWidth(columnWidth)
			if i == hit {
				// The row ⌘F found, marked without moving what is in it.
				col.Margin(-4, -10, -4, -10).Padding(4, 10, 4, 10).Radius(10).Background(p.Sel)
			}
			col.Children(func() {
				if i >= len(items) {
					a.turn(c, true, func() { a.running(c) })
					return
				}
				it := items[i]
				switch {
				case it.older:
					a.olderRow(c)
				case it.ap != nil:
					a.waitTurn(c, func() { a.ask(c, *it.ap) })
				case it.fold != nil:
					a.turn(c, it.live, func() { a.fold(c, it.fold) })
				case it.work != nil:
					a.turn(c, false, func() { a.work(c, it.work, it.done) })
				case it.sum:
					a.turn(c, false, func() { a.summary(c, it.done) })
				default:
					a.entry(c, it.e, it.live)
				}
			})
		})
	}).Grow(1).Padding(18, 0, a.dockH+28, 0)
	// Rows fade out under the bar, and above the composer floating at the
	// foot, rather than being cut at an edge.
	if h := list.Bounds().H; h > 0 {
		black := ui.RGBA(0, 0, 0, 1)
		foot := max(0, h-a.dockH)
		list.MaskLinearGradient(ui.LinearGradient{Angle: 180, Stops: []ui.GradientStop{
			{Color: ui.Transparent, At: 0}, {Color: black, At: 28 / h},
			{Color: black, At: max(28, foot-56) / h}, {Color: ui.Transparent, At: foot / h}}})
	}
	// A list not shown in the last frame (a session just opened) says it
	// is not at its end, though it starts there: the pill would flash.
	shown := a.listShown == st
	a.listShown = st
	if len(items) > 0 && shown && !st.AtEnd() {
		a.scrollBottomPill(c)
	}
}

// scrollBottomPill floats a Liquid Glass pill above the composer when the
// user scrolls up through a long conversation.
func (a *App) scrollBottomPill(c *ui.Context) {
	p := a.p
	ui.Row(c).Absolute().Left(0).Right(0).Bottom(a.dockH + 16).Justify(ui.Center).PassThrough().ZIndex(15).Children(func() {
		b := ui.ButtonBase(c.Key("scroll-bottom")).Row().Height(32).Padding(0, 12).Gap(6).Radius(16).
			AlignItems(ui.Center).Border(.5, p.Edge2).
			Label(locales.ScrollToBottom()).Tooltip(locales.ScrollToBottom()).
			Transition(ui.ElementTransition{
				Enter:    &ui.Motion{Y: 10, Scale: .92, Opacity: 0},
				Exit:     &ui.Motion{Y: 8, Scale: .94, Opacity: 0},
				Duration: 180 * time.Millisecond,
			})
		a.glass(b, glassButton(p))
		press(b)
		focusRing(b, p)
		rim(b, p)
		lift(b, p)
		b.Children(func() {
			if a.C.Busy {
				ui.Box(c).Size(6, 6).Radius(3).Background(p.Live)
			}
			ui.Text(c, locales.ScrollToBottom()).FontSize(12.5).FontWeight(500).TextColor(p.Fg).SingleLine()
			sym(c, "chevron-down", 13, p.Fg2)
		})
		if b.Clicked() {
			a.transcriptList().ScrollToEnd()
		}
	})
}

// item is a row of the transcript: an entry, a fold of steps that only
// looked around, or an approval.
type item struct {
	e    *client.Entry
	fold []*client.Entry
	ap   *proto.Approval
	live bool
	// A finished turn shows its work folded, then its last words, then
	// what it changed: work holds the steps, done the turn's index.
	work    []*client.Entry
	done    int
	turnSeq int64 // of the turn done is, for a key that survives older turns loading
	sum     bool
	older   bool // the row that loads what came before
}

func (it item) key() any {
	switch {
	case it.older:
		return "older"
	case it.ap != nil:
		return "ask:" + it.ap.ID
	case it.fold != nil:
		return "fold:" + it.fold[0].CallID
	case it.work != nil:
		return fmt.Sprintf("work:%d", it.work[0].Key)
	case it.sum:
		return fmt.Sprintf("sum:%d", it.turnSeq)
	}
	return it.e.Key
}

// browsing are the tools that only look around: runs of them fold into
// one line.
var browsing = map[string]bool{"read_file": true, "list_dir": true, "search": true, "git_status": true, "git_diff": true}

// items are the rows the transcript shows: a finished turn's steps fold
// into one line, runs of two or more finished browsing steps fold, and a
// step waiting on an approval shows only as the approval.
func (a *App) items() []item {
	es := a.C.Entries
	live := a.liveFrom()
	asked := map[*client.Entry]bool{}
	for _, ap := range a.C.Approvals {
		for i := len(es) - 1; i >= 0; i-- {
			e := es[i]
			if e.Role == client.Tool && !e.Done && e.Text == ap.Tool && !asked[e] &&
				(len(e.Args) == 0 || compact(e.Args) == compact(ap.Args)) {
				asked[e] = true
				break
			}
		}
	}
	var out []item
	for i := 0; i < len(es); {
		e := es[i]
		if t := e.Turn; e.Role == client.User && t >= 0 && t < len(a.C.Turns) && a.C.Turns[t].Done {
			j := i + 1
			for j < len(es) && es[j].Turn == t && es[j].Role != client.User {
				j++
			}
			out = append(out, item{e: e})
			out = append(out, a.finished(es[i+1:j], t)...)
			i = j
			continue
		}
		var run []item
		run, i = a.row(es, i, live, asked)
		out = append(out, run...)
	}
	for i := range a.C.Approvals {
		out = append(out, item{ap: &a.C.Approvals[i]})
	}
	return out
}

// row is the row that starts at es[i], a fold of browsing steps or an
// entry, and the index after it.
func (a *App) row(es []*client.Entry, i, live int, asked map[*client.Entry]bool) ([]item, int) {
	if asked[es[i]] {
		return nil, i + 1
	}
	j := i
	for j < len(es) && browse(es[j]) {
		j++
	}
	if j-i >= 2 && a.settings().Detail != "all" {
		return []item{{fold: es[i:j], live: live >= 0 && i >= live}}, j
	}
	return []item{{e: es[i], live: live >= 0 && i >= live}}, i + 1
}

func browse(e *client.Entry) bool {
	return e.Role == client.Tool && e.Done && !e.Failed && browsing[e.Text]
}

// finished are the rows of a finished turn after its prompt: the steps
// folded, the last thing it said, the notes after it, and its summary.
func (a *App) finished(body []*client.Entry, t int) []item {
	tail := len(body)
	for tail > 0 && body[tail-1].Role == client.Notice {
		tail--
	}
	last := -1
	if tail > 0 && body[tail-1].Role == client.Assistant {
		last = tail - 1
	}
	work := body[:tail]
	if last >= 0 {
		work = body[:last]
	}
	var out []item
	if a.settings().Detail == "" && slices.ContainsFunc(work, func(e *client.Entry) bool { return e.Role == client.Tool }) {
		out = append(out, item{work: work, done: t})
	} else {
		for i := 0; i < len(work); {
			var run []item
			run, i = a.row(work, i, -1, nil)
			out = append(out, run...)
		}
	}
	if last >= 0 {
		out = append(out, item{e: body[last]})
	}
	for _, e := range body[tail:] {
		out = append(out, item{e: e})
	}
	return append(out, item{sum: true, done: t, turnSeq: a.C.Turns[t].Seq})
}

// fold is a run of browsing steps as one line, "浏览了 4 个文件，搜索 1
// 次", that opens to the steps.
func (a *App) fold(c *ui.Context, steps []*client.Entry) {
	p := a.p
	count := map[string]int{}
	for _, e := range steps {
		count[e.Text]++
	}
	var parts []string
	add := func(n int, say func(int) string) {
		if n > 0 {
			parts = append(parts, say(n))
		}
	}
	add(count["read_file"], locales.BrowsedFiles)
	add(count["list_dir"], locales.ListedDirs)
	add(count["search"], locales.SearchedTimes)
	add(count["git_status"]+count["git_diff"], locales.GitLooks)
	key := "fold:" + steps[0].CallID
	open := a.opened[key]
	if a.inWork && len(count) == 1 && count["read_file"] > 0 {
		// In a finished turn's work, reads are a step naming the files.
		var names []string
		for _, e := range steps {
			names = append(names, e.Detail)
		}
		row := ui.ButtonBase(c.Key(key)).Row().Height(28).Padding(0, 6).Margin(0, -6).Gap(8).Radius(6).AlignItems(ui.Center).Label(locales.Browse() + " " + strings.Join(names, " ")).Expanded(open)
		press(row)
		focusRing(row, p)
		if row.Hovered() {
			row.Background(p.Hover)
		}
		if row.Clicked() {
			a.transcriptList().HoldPlace()
			open = !open
			a.opened[key] = open
		}
		row.Children(func() {
			ui.Box(c).Width(16).Center().Children(func() { sym(c, "file-search", 15, p.Mute) })
			ui.Text(c, locales.Browse()).FontSize(13.5).TextColor(p.Fg2).SingleLine().Shrink(0)
			ui.Text(c, strings.Join(names, " ")).Font("monospace").FontSize(13).TextColor(p.Fg).SingleLine().PreserveSpaces().Ellipsis("…").MinWidth(0)
			ui.Spacer(c)
			if row.Hovered() || open {
				disclosureChevron(c, key, open, 10, p.Mute)
			}
		})
	} else {
		row := ui.ButtonBase(c.Key(key)).Row().Height(28).Padding(0, 6).Margin(0, -6).Gap(7).Radius(6).AlignItems(ui.Center).Label(locales.ExpandSteps()).Expanded(open)
		press(row)
		focusRing(row, p)
		if row.Hovered() {
			row.Background(p.Hover)
		}
		if row.Clicked() {
			a.transcriptList().HoldPlace()
			open = !open
			a.opened[key] = open
		}
		row.Children(func() {
			disclosureChevron(c, key, open, 12, p.Mute)
			ui.Text(c, strings.Join(parts, locales.ClauseSeparator())).FontSize(13.5).TextColor(p.Mute).SingleLine()
			ui.Spacer(c)
		})
	}
	if open {
		ui.Column(c).Margin(2, 0, 8, 0).
			Transition(ui.ElementTransition{Enter: &ui.Motion{Y: -4, Opacity: 0}, Duration: 150 * time.Millisecond}).
			Children(func() {
				for _, e := range steps {
					ui.Column(c.Key(e.Key)).Children(func() { a.card(c, e) })
				}
			})
	}
}

// thinking is a running turn whose last row shows nothing moving: no
// text streaming in, no tool running, no approval waiting.
func (a *App) thinking() bool {
	if !a.C.Busy || len(a.C.Approvals) > 0 {
		return false
	}
	if n := len(a.C.Entries); n > 0 {
		e := a.C.Entries[n-1]
		if !e.Done && (e.Role == client.Assistant || e.Role == client.Tool || e.Role == client.Thinking) {
			return false
		}
	}
	return true
}

// liveFrom is the first entry of the running turn, -1 when none runs.
func (a *App) liveFrom() int {
	if !a.C.Busy {
		return -1
	}
	for i := len(a.C.Entries) - 1; i >= 0; i-- {
		if a.C.Entries[i].Role == client.User {
			return i + 1
		}
	}
	return 0
}

// turn indents what the agent did 14 points, behind a 2-point rail that
// the running turn lights, with a pulse running down it.
func (a *App) turn(c *ui.Context, live bool, content func()) {
	a.turnRail(c, live, false, content)
}

// waitTurn is a turn waiting on the user: its rail is lit and still, as
// what waits does not move while nothing happens, and the window is not
// redrawn for as long as the question stands.
func (a *App) waitTurn(c *ui.Context, content func()) {
	a.turnRail(c, true, true, content)
}

func (a *App) turnRail(c *ui.Context, live, still bool, content func()) {
	p := a.p
	ui.Row(c).AlignItems(ui.Stretch).Children(func() {
		rail := ui.Box(c).Width(2).Shrink(0).Clip()
		if live {
			// While the turn waits on the user nothing of it moves.
			if still || len(a.C.Approvals) > 0 || a.reduceMotion() {
				rail.Background(p.Live.Alpha(.45))
			} else {
				rail.Background(p.LiveBg)
				pulse := rail.Loop("pulse", 2800*time.Millisecond, ui.EaseInOut)
				rail.Children(func() {
					ui.Box(c).Absolute().Left(0).TopPercent(pulse*140 - 40).Width(2).HeightPercent(40).
						LinearGradient(ui.LinearGradient{Angle: 180, Stops: []ui.GradientStop{{Color: p.Live.Alpha(0), At: 0}, {Color: p.Live, At: .5}, {Color: p.Live.Alpha(0), At: 1}}})
				})
			}
		}
		ui.Column(c).Grow(1).MinWidth(0).Padding(0, 0, 0, 12).Children(content)
	})
}

// running says the turn is at work before anything of it shows.
func (a *App) running(c *ui.Context) {
	p := a.p
	ui.Row(c).Height(28).Gap(8).AlignItems(ui.Center).Children(func() {
		if a.reduceMotion() {
			ui.Box(c).Size(7, 7).Radius(3.5).Background(p.Live)
			ui.Text(c, locales.ThinkingNow()).FontSize(13.5).TextColor(p.Fg2)
			return
		}
		spinner(c, p, 12)
		// A faint blue light passes over the words, left to right.
		words := ui.Box(c)
		at := words.Loop("shine", 1800*time.Millisecond, ui.EaseInOut)*1.6 - .3
		black := ui.RGBA(0, 0, 0, 1)
		words.Children(func() {
			ui.Text(c, locales.ThinkingNow()).FontSize(13.5).TextColor(p.Fg2)
			ui.Text(c, locales.ThinkingNow()).FontSize(13.5).TextColor(p.Live).Absolute().Left(0).Top(0).AccessHidden(true).
				MaskLinearGradient(ui.LinearGradient{Angle: 90, Stops: []ui.GradientStop{
					{Color: ui.Transparent, At: at - .25}, {Color: black, At: at}, {Color: ui.Transparent, At: at + .25}}})
		})
	})
}

func (a *App) empty(c *ui.Context) {
	p := a.p
	ui.Column(c).Grow(1).Center().Gap(10).Padding(0, 28, 60, 28).Children(func() {
		switch {
		case a.C.Phase == client.Disconnected:
			subtitle := locales.KernelDisconnectedHint()
			if a.C.Err != "" {
				subtitle = a.C.Err + "\n" + subtitle
			}
			var action func()
			if a.reconnect != nil {
				action = func() {
					if a.pushButton(c, "reconnect-kernel", locales.Reconnect()).Clicked() {
						a.reconnect()
					}
				}
			}
			emptyState(c, p, "plug-connected-x", locales.KernelDisconnected(), subtitle, action)
		case a.C.Phase != client.Ready:
			emptyState(c, p, "clock", phaseText(a.C.Phase)+"…", "", nil)
		case a.C.Current == nil:
			emptyState(c, p, "message-plus", locales.StartNewSession(), "", func() {
				if a.pushButton(c, "empty-new-session", locales.NewSession()).Clicked() {
					a.do(a.C.NewSession())
				}
			})
		default:
			a.hello(c)
		}
	})
}

func (a *App) entry(c *ui.Context, e *client.Entry, live bool) {
	p := a.p
	switch e.Role {
	case client.User:
		// Right-aligned user message column (Antigravity-style): attached
		// images above the text bubble, and hover actions (copy / rewind)
		// right-aligned directly below the bubble.
		msg := ui.Column(c).Margin(4, 0, 12, 0).AlignItems(ui.End).Gap(4)
		msg.ContextMenu(func(m *ui.Menu) {
			if e.Text != "" {
				if m.Item(locales.Copy()).Chosen() {
					c.WriteClipboard(e.Text)
					a.flash("check", locales.Copy())
				}
			}
			if a.rewindable(e) {
				if e.Text != "" && m.Item(locales.EditAndResend()).Chosen() {
					a.editAndResend(c, a.C.Turns[e.Turn].Seq, e.Text)
				}
				if m.Item(locales.RewindTo()).Chosen() {
					seq := a.C.Turns[e.Turn].Seq
					text := e.Text
					if text == "" {
						text = locales.ImagesPlaceholder(len(e.Images))
					}
					a.openRewindAt(seq, text)
				}
			}
			m.Separator()
			m.EditItems()
		})
		msg.Children(func() {
			if len(e.Images) > 0 {
				a.sentImages(c, e)
			}
			if e.Text != "" {
				ui.Column(c).MaxWidthPercent(80).Padding(9, 15).Radius(18).Background(p.Bubble).Children(func() {
					a.userMessageText(c, e.Text)
				})
			}
			ts := ""
			if e.Turn >= 0 && e.Turn < len(a.C.Turns) {
				ts = a.msgTime(a.C.Turns[e.Turn].Start)
			}
			if ts != "" || e.Text != "" || a.rewindable(e) {
				ui.Row(c).Gap(2).AlignItems(ui.Center).Children(func() {
					if ts != "" {
						tbox := ui.Box(c).Padding(0, 4).Children(func() {
							ui.Text(c, ts).FontSize(12).TextColor(p.Mute).SingleLine()
						})
						target := float32(0)
						if msg.Hovered() {
							target = 1
						}
						tbox.Opacity(tbox.AnimateTo("ts-shown", target, ui.Animation{Duration: 120 * time.Millisecond}))
					}
					if e.Text != "" {
						a.copyButton(c, e.Text, msg.Hovered())
					}
					if a.rewindable(e) {
						if e.Text != "" {
							a.editResendButton(c, e, msg.Hovered())
						}
						a.rewindButton(c, e, msg.Hovered())
					}
				})
			}
		})
	default:
		if e.Typed {
			// A command the user ran is theirs, in no turn: no rail.
			ui.Column(c).Margin(2, 0, 10, 0).Children(func() { a.card(c, e) })
			return
		}
		a.turn(c, live, func() { a.body(c, e) })
	}
}

// userMessageText renders a user's prompt with its /skill and @file words
// in the accent, the / or @ faint, as the composer showed them.
func (a *App) userMessageText(c *ui.Context, text string) {
	p := a.p
	toks := refTokens(text, nil)
	if len(toks) == 0 {
		ui.Text(c, text).FontSize(15).TextColor(p.Fg).LineHeight(1.55).Selectable()
		return
	}
	blue := skillAccent(p)
	var spans []ui.Span
	pos := 0
	for _, t := range toks {
		if t.start > pos {
			spans = append(spans, ui.Span{Text: text[pos:t.start], Size: 15, Color: p.Fg})
		}
		spans = append(spans,
			ui.Span{Text: text[t.start : t.start+1], Size: 15, Color: blue.Alpha(.55)},
			ui.Span{Text: text[t.start+1 : t.end], Size: 15, Color: blue, Weight: 500},
		)
		pos = t.end
	}
	if pos < len(text) {
		spans = append(spans, ui.Span{Text: text[pos:], Size: 15, Color: p.Fg})
	}
	ui.RichText(c, spans...).LineHeight(1.55).Selectable()
}

// body is what the agent did or said, without the rail of its turn.
func (a *App) body(c *ui.Context, e *client.Entry) {
	switch e.Role {
	case client.Assistant:
		msg := ui.Column(c).Margin(4, 0, 6, 0)
		msg.Children(func() {
			if e.Done {
				markdown.View(c, e.Text, &a.md).ContextMenu(func(m *ui.Menu) {
					if m.Item(locales.CopyReply()).Chosen() {
						c.WriteClipboard(plain(e.Text))
					}
					if m.Item(locales.CopyMarkdown()).Chosen() {
						c.WriteClipboard(e.Text)
					}
					m.Separator()
					m.EditItems()
				})
				// Action bar below finished assistant reply: copy, retry, and timestamp.
				p := a.p
				ts := a.msgTime(a.turnTimestamp(e.Turn))
				canRetry := a.canRetryTurn(e.Turn)
				ui.Row(c).Margin(2, 0, 0, -6).Gap(2).AlignItems(ui.Center).Children(func() {
					a.copyButton(c, e.Text, true)
					if canRetry {
						a.retryButton(c, e.Turn)
					}
					if ts != "" {
						ui.Text(c, ts).FontSize(12).TextColor(p.Mute).Padding(0, 6).SingleLine()
					}
				})
			} else {
				markdown.Stream(c, e.Text, &a.md)
			}
		})
	case client.Tool:
		a.card(c, e)
	case client.Notice:
		a.note(c, e)
	case client.Thinking:
		a.thought(c, e)
	}
}

// noteIcons are the symbols of the notices, by what they are about.
var noteIcons = map[string]string{"fail": "alert-circle", "fallback": "arrows-exchange", "retry": "refresh",
	"stop": "player-stop", "limit": "player-pause", "compact": "history"}

// note is a notice as one line among the steps: a turn stopped or
// failed, moved to another account, retried, compacted. A compaction
// opens to the summary the model goes on from; a turn out of steps can
// go on.
func (a *App) note(c *ui.Context, e *client.Entry) {
	p := a.p
	icon, col := noteIcons[e.Note], p.Mute
	if icon == "" {
		icon = "info-circle"
	}
	if e.Note == "fail" {
		col = p.Minus
	}
	key := "note:" + strconv.Itoa(e.Key)
	open := a.opened[key]
	ui.Row(c).Gap(8).AlignItems(ui.Center).Margin(2, 0, 6, 0).Children(func() {
		row := ui.ButtonBase(c.Key(key)).Row().MinHeight(28).Padding(0, 6).Margin(0, -6).Gap(8).Radius(6).AlignItems(ui.Center).MinWidth(0).Label(e.Text).Disabled(e.Output == "")
		if e.Output != "" {
			press(row)
			focusRing(row, p)
			if row.Hovered() {
				row.Background(p.Hover)
			}
		}
		if row.Clicked() {
			a.transcriptList().HoldPlace()
			a.opened[key] = !open
		}
		row.Children(func() {
			ui.Box(c).Width(16).Center().Children(func() { sym(c, icon, 14, col) })
			ui.Text(c, e.Text).FontSize(13.5).TextColor(p.Mute).MinWidth(0)
			if e.Output != "" {
				disclosureChevron(c, key, open, 10, p.Mute)
			}
		})
		if e.Note == "limit" && a.last(e) && !a.C.Busy && a.textButton(c, "go-on", locales.ContinueTurn()).Clicked() {
			a.do(a.C.Prompt(locales.ContinueTurn()))
		}
		// A turn that failed can be run again from where it stands, as
		// Safari offers to reload a page that did not load.
		if e.Note == "fail" && a.last(e) && !a.C.Busy && a.canRetryTurn(e.Turn) && a.textButton(c, "retry-failed", locales.Retry()).Clicked() {
			a.retryTurn(e.Turn)
		}
	})
	if open {
		ui.Text(c, e.Output).FontSize(13.5).LineHeight(1.55).TextColor(p.Fg2).Padding(9, 11).Radius(8).Background(p.Chip).
			Margin(0, 0, 8, 24).Selectable().
			Transition(ui.ElementTransition{Enter: &ui.Motion{Y: -4, Opacity: 0}, Duration: 150 * time.Millisecond})
	}
}

// last reports whether e is the session's last entry.
func (a *App) last(e *client.Entry) bool {
	n := len(a.C.Entries)
	return n > 0 && a.C.Entries[n-1] == e
}

// step is a tool call: one line, its output under it once opened.
func (a *App) step(c *ui.Context, e *client.Entry) {
	p := a.p
	t, ok := toolFor(e.Text)
	if !ok {
		t.symbol, t.verb = "tool", e.Text
		if server, name, ok := mcpTool(e.Text); ok {
			t.symbol, t.verb = "plug", server+" · "+name
		}
	}
	open := a.opened[e.CallID]
	canOpen := e.Output != "" && !e.Skipped
	row := ui.ButtonBase(c.Key("step:"+e.CallID)).Row().Height(28).Padding(0, 6).Margin(0, -6).Gap(8).Radius(6).AlignItems(ui.Center).Label(t.verb + " " + e.Detail)
	if canOpen {
		press(row)
		focusRing(row, p)
		if row.Hovered() {
			row.Background(p.Hover)
		}
	}
	if row.Clicked() && canOpen {
		a.transcriptList().HoldPlace()
		a.opened[e.CallID] = !open
	}
	row.Children(func() {
		ui.Box(c).Width(16).Center().Children(func() { sym(c, t.symbol, 14, p.Mute) })
		ui.Text(c, t.verb).FontSize(13.5).TextColor(p.Fg2).SingleLine().Shrink(0)
		if e.Detail != "" {
			d := ui.Text(c, e.Detail).FontSize(13).TextColor(p.Fg).SingleLine().Ellipsis("…").MinWidth(0)
			if e.Text != "task" && e.Text != "run_tasks" {
				d.Font("monospace")
			}
		}
		ui.Spacer(c)
		switch {
		case !e.Done:
			ui.Box(c).Size(2, 12).Background(p.Live)
		case e.Skipped:
			ui.Text(c, locales.Skipped()).FontSize(12.5).TextColor(p.Mute).SingleLine().Shrink(0)
		case e.Failed:
			ui.Box(c).Size(7, 2).Background(p.Minus)
		case e.Output != "" && (row.Hovered() || open):
			disclosureChevron(c, "step:"+e.CallID, open, 10, p.Mute)
		}
	})
	if !e.Done && len(e.Steps) > 0 {
		a.taskSteps(c, e.Steps)
	}
	if !e.Done && len(e.Tasks) > 0 {
		a.taskGraph(c, e.Tasks)
	}
	if open {
		out := e.Output
		if lines := strings.Split(out, "\n"); len(lines) > 40 {
			out = strings.Join(lines[:40], "\n") + "\n" + locales.MoreLines(len(lines)-40)
		}
		o := ui.Text(c, out).FontSize(13).TextColor(p.Fg2).Padding(9, 11).Radius(8).Background(p.Chip).
			Margin(2, 0, 8, 24).Selectable().PreserveSpaces()
		if e.Text != "task" && e.Text != "run_tasks" {
			o.Font("monospace")
		}
	}
}

// ask is an approval request: what the tool would do, and the three
// answers.
func (a *App) ask(c *ui.Context, ap proto.Approval) {
	if ap.Kind == "question" {
		a.question(c, ap)
		return
	}
	p := a.p
	busy := a.C.Answering[ap.ID]
	a.waiting(c).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			slot(c, p, "wait")
			kind := ap.Tool
			if k, ok := kinds[ap.Kind]; ok {
				kind = k()
			}
			if kind == "" {
				kind = ap.Tool
			}
			ui.Text(c, kind).FontSize(13.5).FontWeight(600).TextColor(p.Fg).SingleLine().Shrink(0)
			ui.Text(c, locales.NeedsApproval()).FontSize(13.5).TextColor(p.Fg2).SingleLine().Ellipsis("…").MinWidth(0)
			ui.Spacer(c)
			if w := a.waited(c, ap.ID); w != "" {
				sym(c, "clock", 12, p.Mute)
				ui.Text(c, w).FontSize(12.5).TextColor(p.Mute).SingleLine().Shrink(0)
			}
		})
		editing := a.editing == ap.ID
		if len(ap.Apps) > 0 {
			a.appsChip(c, ap)
		} else {
			what := ap.Command
			if ap.Kind == "mcp" {
				what += "\n" + compact(ap.Args)
			}
			if what == "" && len(ap.Paths) > 0 {
				var rel []string
				for _, path := range ap.Paths {
					dir, base := a.splitPath(path)
					rel = append(rel, dir+base)
				}
				what = strings.Join(rel, "\n")
			}
			if what == "" {
				what = compact(ap.Args)
			}
			chip := ui.Column(c).Padding(9, 12).Gap(4).Radius(9).Background(p.Chip).Margin(10, 0, 8, 0)
			if editing {
				chip.InsetShadow(0, 0, 0, 1, p.Live)
			}
			chip.Children(func() {
				if ap.Command != "" && ap.Kind != "mcp" && a.C.Current != nil {
					cwd := home(a.C.Current.Cwd)
					ui.Text(c, filepath.Base(a.C.Current.Cwd)).Font("monospace").FontSize(11.5).TextColor(p.Mute).
						SingleLine().TruncateMiddle().Unselectable().Tooltip(cwd)
				}
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					if ap.Command != "" && ap.Kind != "mcp" {
						ui.Text(c, "$").Font("monospace").FontSize(13.5).TextColor(p.Mute).Shrink(0).Unselectable()
					}
					if editing {
						ui.TextInputBase(c, &a.cmdDraft).Bind(&a.cmdInput).Label(locales.EditCommand()).Font("monospace").FontSize(13.5).TextColor(p.Fg).
							Grow(1).MinWidth(0).SingleLine().OnTextKey(func(k ui.TextKey) bool {
							a.cmdDraft = k.Text()
							if k.Key == ui.KeyEnter && k.Mods == 0 {
								a.runEdited(ap)
								return true
							}
							return false
						})
						return
					}
					ui.Text(c, what).Font("monospace").FontSize(13.5).TextColor(p.Fg).Selectable().Grow(1).MinWidth(0)
				})
			})
		}
		a.approvalDiff(c, ap)
		ui.Column(c).Gap(4).Children(func() {
			why := func(symbol, s string) {
				ui.Row(c).Gap(6).AlignItems(ui.Center).MinWidth(0).Children(func() {
					sym(c, symbol, 12, p.Mute)
					ui.Text(c, s).FontSize(12.5).TextColor(p.Mute).MinWidth(0)
				})
			}
			if ap.Reason != "" {
				r := ap.Reason
				if say, ok := reasons[ap.Reason]; ok {
					r = say()
				}
				why("shield-lock", r)
			}
			if cur := a.C.Current; cur != nil {
				why("adjustments-horizontal", locales.CurrentMode(modeName(cur.Mode)))
			}
			if ap.Rule != nil && !editing {
				// What allowing it for the session, or always, lets through.
				why("route", locales.RuleScope(ruleText(*ap.Rule)))
			}
			if len(ap.Apps) > 0 {
				why("keyboard", locales.EscStops())
			}
		})
		if editing {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Margin(12, 0, 0, 0).Children(func() {
				ui.Text(c, locales.EditCommandHint()).FontSize(13).TextColor(p.Mute).Grow(1).MinWidth(0)
				if a.button(c, "edit-cancel:"+ap.ID, locales.Cancel(), "esc", false).Disabled(busy).Clicked() {
					a.editing = ""
				}
				if a.button(c, "edit-run:"+ap.ID, locales.RunIt(), "⏎", true).Disabled(busy || strings.TrimSpace(a.cmdDraft) == "").Clicked() {
					a.runEdited(ap)
				}
			})
			return
		}
		ui.Row(c).Gap(6).Wrap().AlignItems(ui.Center).Margin(10, 0, 0, 0).Children(func() {
			if ap.Kind == "exec" && ap.Command != "" && a.textButton(c, "edit:"+ap.ID, locales.EditCommand()).Disabled(busy).Clicked() {
				a.editing, a.cmdDraft = ap.ID, ap.Command
				a.cmdInput.Focus(c)
			}
			ui.Spacer(c)
			denyHint := a.approvalHint(c, ap.ID, "esc")
			if a.denyWithWords() && a.C.Approvals[0].ID == ap.ID && !busy {
				denyHint = "⏎" // the words in the composer go with it
			}
			if a.button(c, "deny:"+ap.ID, locales.Deny(), denyHint, false).Disabled(busy).Clicked() {
				a.deny(ap.ID)
			}
			if ap.Rule != nil {
				if a.button(c, "always:"+ap.ID, locales.AllowAlways(), a.approvalHint(c, ap.ID, "⌘⏎"), false).Disabled(busy).Clicked() {
					a.do(a.C.Answer(ap.ID, true, true))
				}
			}
			if a.button(c, "allow:"+ap.ID, locales.Allow(), a.approvalHint(c, ap.ID, "⏎"), true).Disabled(busy).Clicked() {
				a.do(a.C.Answer(ap.ID, true, false))
			}
		})
	})
}

// appsChip lists the apps computer use asks for, each with what the
// agent may do there, after what the agent wants them for, as Claude's
// request for access does.
func (a *App) appsChip(c *ui.Context, ap proto.Approval) {
	p := a.p
	var args struct {
		Reason string `json:"reason"`
	}
	json.Unmarshal(ap.Args, &args)
	why := strings.TrimSpace(args.Reason)
	ui.Column(c).Padding(3, 12).Radius(9).Background(p.Chip).Margin(10, 0, 8, 0).Children(func() {
		if why != "" {
			ui.Text(c, why).FontSize(13.5).TextColor(p.Fg).Selectable().Margin(6, 0, 6, 0)
		}
		for i, app := range ap.Apps {
			label, note := tierText(app.Tier)
			row := ui.Row(c.Key("app:"+app.Name)).Gap(10).AlignItems(ui.Center).Padding(7, 0)
			if i > 0 || why != "" {
				row.BorderWidth(.5, 0, 0, 0).BorderColor(p.Edge)
			}
			row.Children(func() {
				ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
					ui.Text(c, app.Name).FontSize(13.5).FontWeight(600).TextColor(p.Fg).SingleLine().Ellipsis("…")
					ui.Text(c, note).FontSize(12).TextColor(p.Mute).SingleLine().Ellipsis("…")
				})
				ui.Text(c, label).FontSize(12.5).FontWeight(500).TextColor(p.Fg2).SingleLine().Shrink(0)
			})
		}
	})
}

// tierText names what computer use may do in an app, and says it.
func tierText(tier string) (label, note string) {
	switch tier {
	case "read":
		return locales.TierRead(), locales.TierReadNote()
	case "click":
		return locales.TierClick(), locales.TierClickNote()
	}
	return locales.TierFull(), locales.TierFullNote()
}

// ruleText says what a rule lets run: "以 go test 开头的命令".
func ruleText(r proto.Rule) string {
	var s string
	if r.Prefix != "" {
		s = locales.RuleCommands(r.Prefix)
	} else {
		verb := r.Tool
		if t, ok := toolFor(r.Tool); ok {
			verb = t.verb
		} else if server, name, ok := mcpTool(r.Tool); ok {
			verb = server + " · " + name
		}
		s = locales.RuleTool(verb)
	}
	if r.Unrestricted {
		s += locales.RuleOutside()
	}
	return s
}

// runEdited runs the command as the user changed it.
func (a *App) runEdited(ap proto.Approval) {
	cmd := strings.TrimSpace(a.cmdDraft)
	if cmd == "" || a.C.Answering[ap.ID] {
		return
	}
	cmds, err := a.C.Respond(proto.ApprovalAnswer{ApprovalID: ap.ID, Allow: true, Command: cmd})
	if a.do(cmds, err); err == nil {
		a.editing, a.cmdDraft = "", ""
	}
}

// denyWithWords reports whether declining the approval waiting takes
// what is written in the composer with it.
func (a *App) denyWithWords() bool {
	return len(a.C.Approvals) > 0 && !a.questionFirst() && strings.TrimSpace(a.draft) != "" && len(a.attached) == 0
}

// deny declines an approval, with the composer's words if any.
func (a *App) deny(id string) {
	ans := proto.ApprovalAnswer{ApprovalID: id}
	words := a.denyWithWords() && len(a.C.Approvals) > 0 && a.C.Approvals[0].ID == id
	if words {
		ans.Text = strings.TrimSpace(a.draft)
	}
	cmds, err := a.C.Respond(ans)
	if a.do(cmds, err); err == nil && words {
		a.draft = ""
	}
}

// waiting is the card of what waits on the user: the top layer of the
// sheet, edged in blue, rising into place as it comes.
func (a *App) waiting(c *ui.Context) ui.Element {
	p := a.p
	card := ui.Column(c).Margin(6, 0, 18, 0).Padding(14, 14, 14, 16).Radius(14).Background(p.Card).
		Transition(ui.ElementTransition{Enter: &ui.Motion{Y: 10, Scale: .98, Opacity: 0}})
	return lift(card, p).InsetShadow(0, 0, 0, 1, p.Live)
}

// question is the model asking the user: the question, the options it
// gave, and the composer below for any other answer.
func (a *App) question(c *ui.Context, ap proto.Approval) {
	p := a.p
	busy := a.C.Answering[ap.ID]
	a.waiting(c).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			slot(c, p, "wait")
			ui.Text(c, locales.Question()).FontSize(13.5).FontWeight(600).TextColor(p.Fg)
			ui.Text(c, locales.NeedsAnswer()).FontSize(13.5).TextColor(p.Fg2)
			ui.Spacer(c)
			if w := a.waited(c, ap.ID); w != "" {
				sym(c, "clock", 12, p.Mute)
				ui.Text(c, w).FontSize(12.5).TextColor(p.Mute)
			}
		})
		ui.Text(c, ap.Question).FontSize(14.5).LineHeight(1.55).TextColor(p.Fg).Margin(10, 0, 0, 0).Selectable()
		if len(ap.Options) > 0 {
			ui.Row(c).Gap(8).Wrap().Margin(10, 0, 0, 0).Children(func() {
				for i, o := range ap.Options {
					if a.button(c, fmt.Sprintf("option:%s:%d", ap.ID, i), o, "", false).Disabled(busy).Clicked() {
						a.do(a.C.Reply(ap.ID, o))
					}
				}
			})
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Margin(12, 0, 0, 0).Children(func() {
			ui.Text(c, locales.AnswerBelow()).FontSize(13).TextColor(p.Mute).Grow(1).MinWidth(0)
			if a.button(c, "skip:"+ap.ID, locales.Skip(), a.approvalHint(c, ap.ID, "esc"), false).Disabled(busy).Clicked() {
				a.do(a.C.Reply(ap.ID, ""))
			}
		})
	})
}

// button is a 28-point button with an optional key hint; primary ones are
// ink on paper turned over.
func (a *App) button(c *ui.Context, key, label, kbd string, primary bool) ui.Element {
	p := a.p
	// Answering keeps the keyboard where it was, in the composer for the
	// next message, as the buttons of Mail's and Notes' bars do.
	b := ui.ButtonBase(c.Key(key)).Row().Height(26).Padding(0, 10).Gap(5).Radius(7).AlignItems(ui.Center).Label(label).KeepFocus()
	press(b)
	focusRing(b, p)
	fg, hint := p.Fg, p.Mute
	if primary {
		b.Background(p.Acc)
		fg, hint = p.OnAcc, p.OnAcc.Alpha(.6)
	} else {
		b.Background(p.Card).Border(.5, p.Edge2)
		if b.Hovered() {
			b.Background(p.Card.Mix(p.Fg, .04))
		}
	}
	b.Children(func() {
		ui.Text(c, label).FontSize(12.5).FontWeight(500).TextColor(fg).SingleLine()
		if kbd != "" {
			ui.Text(c, kbd).FontSize(11).TextColor(hint).SingleLine()
		}
	})
	return b
}

func compact(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}

// waited says how long an approval has waited, counting from when this
// window first showed it, once it is more than a few seconds.
func (a *App) waited(c *ui.Context, id string) string {
	if a.seen == nil {
		a.seen = map[string]time.Time{}
	}
	at, ok := a.seen[id]
	if !ok {
		at = a.now()
		a.seen[id] = at
	}
	d := a.now().Sub(at)
	c.After(time.Second)
	switch {
	case d < 5*time.Second:
		return ""
	case d < time.Minute:
		return locales.WaitedSeconds(int(d.Seconds()))
	}
	return locales.WaitedMinutes(int(d.Minutes()))
}

// home shortens a path in the home directory to start with ~.
func home(path string) string {
	if h, err := os.UserHomeDir(); err == nil && (path == h || strings.HasPrefix(path, h+"/")) {
		return "~" + path[len(h):]
	}
	return path
}

// olderRow loads the log before what is shown once it scrolls into view.
func (a *App) olderRow(c *ui.Context) {
	if !a.C.LoadingOlder {
		a.do(a.C.LoadOlder())
	}
	ui.Text(c, locales.LoadingEarlier()).FontSize(13).TextColor(a.p.Mute).Center().Padding(4, 0, 16, 0)
}

// seek scrolls to the message a search chose, loading older pages until
// it is there.
func (a *App) seek(items []item) {
	cur := a.C.Current
	if a.jump == 0 || cur == nil || cur.ID != a.jumpIn || a.C.Phase != client.Ready {
		return
	}
	has := func(es []*client.Entry) bool {
		return slices.ContainsFunc(es, func(e *client.Entry) bool { return e.Seq == a.jump })
	}
	for i, it := range items {
		if it.e != nil && it.e.Seq == a.jump || has(it.work) || has(it.fold) {
			a.transcriptList().ScrollTo(i, ui.Center)
			a.jump = 0
			return
		}
	}
	if a.C.Older {
		if !a.C.LoadingOlder {
			a.do(a.C.LoadOlder())
		}
		return
	}
	a.jump = 0
}

// taskSteps are the last few steps of a running sub-agent, under its task.
func (a *App) taskSteps(c *ui.Context, steps []string) {
	p := a.p
	ui.Column(c).Margin(0, 0, 4, 24).Padding(0, 0, 0, 10).BorderWidth(0, 0, 0, 1).BorderColor(p.Edge2).Children(func() {
		if n := len(steps) - 3; n > 0 {
			ui.Text(c, locales.EarlierSteps(n)).FontSize(12.5).TextColor(p.Mute).Height(20)
			steps = steps[n:]
		}
		for i, s := range steps {
			name, detail, _ := strings.Cut(s, "\t")
			verb := name
			if t, ok := toolFor(name); ok {
				verb = t.verb
			}
			ui.Row(c.Key(i)).Height(20).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, verb).FontSize(12.5).TextColor(p.Mute).Shrink(0)
				ui.Text(c, detail).Font("monospace").FontSize(12.5).TextColor(p.Fg2).SingleLine().Ellipsis("…").MinWidth(0)
			})
		}
	})
}

// taskGraph is a running run_tasks call: each task with its state, what
// it waits on, and the step it is at.
func (a *App) taskGraph(c *ui.Context, tasks []*client.GraphTask) {
	p := a.p
	ui.Column(c).Margin(0, 0, 4, 24).Padding(0, 0, 0, 10).BorderWidth(0, 0, 0, 1).BorderColor(p.Edge2).Children(func() {
		for i, t := range tasks {
			ui.Column(c.Key(i)).Children(func() {
				ui.Row(c).Height(22).Gap(7).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(12, 14).Shrink(0).Center().Children(func() {
						switch t.State {
						case "run":
							ui.Box(c).Size(2, 12).Background(p.Live)
						case "done":
							sym(c, "check", 12, p.Fg2)
						case "fail":
							ui.Box(c).Size(7, 2).Background(p.Minus)
						default:
							ui.Box(c).Size(6, 6).Radius(3).Border(1, p.Mute)
						}
					})
					color := p.Fg
					if t.State == "" || t.State == "skip" {
						color = p.Fg2
					}
					ui.Text(c, t.Description).FontSize(13).TextColor(color).SingleLine().Ellipsis("…").MinWidth(0)
					switch {
					case t.State == "skip":
						ui.Text(c, locales.Skipped()).FontSize(12.5).TextColor(p.Mute).Shrink(0)
					case t.State == "" && len(t.After) > 0:
						ui.Text(c, locales.WaitingFor(strings.Join(t.After, locales.ListSeparator()))).FontSize(12.5).TextColor(p.Mute).SingleLine().Shrink(0)
					}
				})
				if t.State == "run" && len(t.Steps) > 0 {
					name, detail, _ := strings.Cut(t.Steps[len(t.Steps)-1], "\t")
					if v, ok := toolFor(name); ok {
						name = v.verb
					}
					ui.Row(c).Height(18).Gap(6).Padding(0, 0, 0, 19).AlignItems(ui.Center).Children(func() {
						ui.Text(c, name).FontSize(12.5).TextColor(p.Mute).Shrink(0)
						ui.Text(c, detail).Font("monospace").FontSize(12.5).TextColor(p.Fg2).SingleLine().Ellipsis("…").MinWidth(0)
					})
				}
			})
		}
	})
}

// mcpTool splits the name of an MCP server's tool, mcp__server__tool.
func mcpTool(name string) (server, tool string, ok bool) {
	rest, ok := strings.CutPrefix(name, "mcp__")
	if !ok {
		return "", "", false
	}
	return strings.Cut(rest, "__")
}

// copyButton copies a message. It comes up while the pointer is over the
// message, keeping its place so nothing moves, and says it copied.
func (a *App) copyButton(c *ui.Context, text string, shown bool) {
	p := a.p
	h := fnv.New64a()
	h.Write([]byte(text))
	key := h.Sum64()
	done := c.Now().Sub(copied[key]) < 2*time.Second
	b := ui.ButtonBase(c.Key(key)).Size(26, 26).Radius(7).Center().Label(locales.Copy()).Tooltip(locales.Copy())
	press(b)
	focusRing(b, p)
	target := float32(0)
	if shown || done || b.Focused() {
		target = 1
	}
	b.Opacity(b.AnimateTo("shown", target, ui.Animation{Duration: 120 * time.Millisecond}))
	if b.Hovered() {
		b.Background(p.Hover)
	}
	b.Children(func() {
		ui.Box(c).Absolute().Fill().Center().
			Transition(ui.ElementTransition{Enter: &ui.Motion{Scale: .6, Opacity: 0}, Exit: &ui.Motion{Scale: .6, Opacity: 0}}).
			Key(done).Children(func() {
			if done {
				sym(c, "check", 14, p.Plus)
			} else {
				sym(c, "copy", 14, p.Mute)
			}
		})
	})
	if b.Clicked() {
		c.WriteClipboard(text)
		copied[key] = c.Now()
		c.After(2 * time.Second)
	}
}

func (a *App) turnTimestamp(turn int) time.Time {
	if turn < 0 || turn >= len(a.C.Turns) {
		return time.Time{}
	}
	tu := a.C.Turns[turn]
	if !tu.End.IsZero() {
		return tu.End
	}
	return tu.Start
}

// msgTime formats a message timestamp: relative time within 24 hours
// ("just now", "12m ago", "3h ago") and a short date beyond 1 day
// ("Nov 10" / "11月10日").
func (a *App) msgTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	now := time.Now()
	if a.now != nil {
		now = a.now()
	}
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return locales.JustNow()
	case d < time.Hour:
		return locales.MsgAgoMinutes(int(d.Minutes()))
	case d < 24*time.Hour:
		return locales.MsgAgoHours(int(d.Hours()))
	case now.Year() == t.Year():
		if strings.HasPrefix(i18n.Locale(), "zh") {
			return fmt.Sprintf("%d月%d日", t.Month(), t.Day())
		}
		return i18n.Months("en", i18n.Abbreviated, true)[t.Month()-1] + " " + strconv.Itoa(t.Day())
	default:
		if strings.HasPrefix(i18n.Locale(), "zh") {
			return t.Format("2006年1月2日")
		}
		return i18n.Months("en", i18n.Abbreviated, true)[t.Month()-1] + fmt.Sprintf(" %d, %d", t.Day(), t.Year())
	}
}

func (a *App) canRetryTurn(turn int) bool {
	if turn < 0 || turn >= len(a.C.Turns) || !a.C.Turns[turn].Done || a.C.Turns[turn].Seq <= 0 {
		return false
	}
	for _, e := range a.C.Entries {
		if e.Turn == turn && e.Role == client.User && !e.Typed && strings.TrimSpace(e.Text) != "" {
			return true
		}
	}
	return false
}

func (a *App) retryTurn(turn int) {
	if a.C.Busy || !a.canRetryTurn(turn) {
		return
	}
	seq := a.C.Turns[turn].Seq
	action := "conversation"
	for _, t := range a.C.Changes.Turns {
		if t.TurnSeq >= seq && len(t.Files) > 0 && !a.C.Changes.Reverted[t.TurnSeq] {
			action = "both"
			break
		}
	}
	a.do(a.C.Retry(seq, action))
}

func (a *App) retryButton(c *ui.Context, turn int) {
	p := a.p
	b := ui.ButtonBase(c.Key(fmt.Sprintf("retry:%d", turn))).Size(26, 26).Radius(7).Center().
		Label(locales.Retry()).Tooltip(locales.Retry()).Disabled(a.C.Busy)
	press(b)
	focusRing(b, p)
	if b.Hovered() {
		b.Background(p.Hover)
	}
	b.Children(func() { sym(c, "refresh", 14, p.Mute) })
	if b.Clicked() {
		a.retryTurn(turn)
	}
}

// approvalLines is the diff a file change asked about would make, worked
// out once for each approval.
func approvalLines(ap proto.Approval) []diffLine {
	var args struct {
		OldText string `json:"old_text"`
		NewText string `json:"new_text"`
		Content *string
	}
	if json.Unmarshal(ap.Args, &args) != nil {
		return nil
	}
	oldText, newText := args.OldText, args.NewText
	if ap.Tool == "write_file" {
		if args.Content == nil {
			return nil
		}
		oldText, newText = "", *args.Content
	}
	lines, _ := lineDiff(oldText, newText)
	return lines
}

// approvalDiff shows what a file change asked about would change, before
// it runs: the first lines of the edit, or of the new file.
func (a *App) approvalDiff(c *ui.Context, ap proto.Approval) {
	if ap.Tool != "edit_file" && ap.Tool != "write_file" {
		return
	}
	lines, ok := a.apDiffs[ap.ID]
	if !ok {
		lines = approvalLines(ap)
		if a.apDiffs == nil || len(a.apDiffs) > 16 {
			a.apDiffs = map[string][]diffLine{}
		}
		a.apDiffs[ap.ID] = lines
	}
	if len(lines) == 0 {
		return
	}
	p := a.p
	const most = 10
	ui.Column(c).Radius(9).Clip().Border(.5, p.Edge).Margin(0, 0, 9, 0).Children(func() {
		for _, line := range lines[:min(most, len(lines))] {
			bg, signColor := ui.Transparent, p.Mute
			switch line.sign {
			case "+":
				bg, signColor = p.Plus.Alpha(.10), p.Plus
			case "−":
				bg, signColor = p.Minus.Alpha(.10), p.Minus
			}
			ui.Row(c).Height(20).Background(bg).Children(func() {
				ui.Text(c, line.sign).Width(22).Shrink(0).Font("monospace").FontSize(13).FixedLineHeight(20).TextColor(signColor).TextAlign(ui.Center)
				ui.Text(c, strings.TrimSuffix(line.text, "\n")).Grow(1).MinWidth(0).Padding(0, 6, 0, 0).Font("monospace").FontSize(13).FixedLineHeight(20).
					TextColor(p.Fg).PreserveSpaces().SingleLine().Selectable()
			})
		}
		if len(lines) > most {
			ui.Text(c, locales.MoreLines(len(lines)-most)).FontSize(12.5).TextColor(p.Mute).Padding(4, 10)
		}
	})
}
