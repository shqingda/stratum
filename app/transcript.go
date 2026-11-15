package app

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/shqingda/talos/client"
	"github.com/shqingda/talos/locales"
	"github.com/shqingda/talos/proto"
)

const columnWidth = 680

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
	"update_plan": {"list-check", locales.ToolPlan},
	"ask_user":    {"message", locales.ToolAsk},
	"task":        {"subtask", locales.ToolTask},
	"run_tasks":   {"hierarchy-3", locales.ToolTasks},
}

var kinds = map[string]func() string{"read": locales.KindRead, "write": locales.KindWrite, "exec": locales.KindExec, "mcp": locales.KindMcp}

// reasons put the kernel's reasons for asking in the app's words.
var reasons = map[string]func() string{
	"It reaches outside the workspace.":                                           locales.ReasonOutsideWorkspace,
	"It runs without the sandbox: with network and access outside the workspace.": locales.ReasonNoSandbox,
	"Commands cannot be sandboxed here.":                                          locales.ReasonCantSandbox,
	"It runs a command.":                                                          locales.ReasonRunsCommand,
	"It changes files.":                                                           locales.ReasonChangesFiles,
	"It calls a tool of an MCP server, outside the sandbox.":                      locales.ReasonMcp,
	"It touches files that may hold secrets.":                                     locales.ReasonSecrets,
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
	// its toggle; in between, as far as the sidebar is.
	shut := toggleLeft(c) + 28 + 12 - sheetGap
	left := 22*a.sideOpen + shut*(1-a.sideOpen)
	ui.Row(c).Height(sheetBar).Shrink(0).Padding(0, 10, 0, left).Gap(10).AlignItems(ui.Center).DragWindow().Children(func() {
		cur := a.C.Current
		if cur == nil {
			ui.Text(c, phaseText(a.C.Phase)).FontSize(14.5).FontWeight(600).TextColor(p.Mute)
			return
		}
		if a.helloShown() {
			return // the middle of the window says what the session is
		}
		ui.Text(c, title(*cur)).FontSize(14.5).FontWeight(600).TextColor(p.Fg).SingleLine().Ellipsis("…").MinWidth(0)
		ui.Row(c).Gap(5).AlignItems(ui.Center).Shrink(0).Children(func() {
			sym(c, "folder", 14, p.Mute)
			ui.Text(c, filepath.Base(cur.Cwd)).FontSize(13.5).TextColor(p.Mute).SingleLine()
		})
		if b := a.C.Workspace.Branch; b != "" {
			ui.Row(c).Gap(5).AlignItems(ui.Center).Shrink(1).MinWidth(0).Children(func() {
				sym(c, "git-branch", 14, p.Mute)
				ui.Text(c, b).FontSize(13.5).TextColor(p.Mute).SingleLine().Ellipsis("…").MinWidth(0)
			})
		}
		ui.Spacer(c)
		a.detailButton(c)
		a.openButton(c)
		a.commitButton(c)
		a.reviewToggle(c)
	})
}

func (a *App) transcript(c *ui.Context) {
	p := a.p
	if len(a.C.Entries) == 0 && len(a.C.Approvals) == 0 {
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
	a.list.Key = func(i int) any {
		if i < len(items) {
			return items[i].key()
		}
		return "running"
	}
	a.list.FollowEnd = true
	a.list.FollowAnimation = &ui.Smooth
	a.md.Theme = mdTheme(p, c.Theme())
	a.md.CodeBlock = a.codeBlock
	n := len(items)
	if a.thinking() {
		n++
	}
	list := ui.List(c, &a.list, n, func(i int) {
		row := ui.Row(c).Justify(ui.Center).Padding(0, 28)
		// What the running turn adds rises into place; rows already there
		// when a session opens just show.
		if i >= len(items) || items[i].live || i == len(items)-1 && items[i].e != nil && items[i].e.Role == client.User {
			row.Transition(ui.ElementTransition{Enter: &ui.Motion{Y: 8, Opacity: 0}, Duration: 220 * time.Millisecond})
		}
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
					a.turn(c, true, func() { a.ask(c, *it.ap) })
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
		row := ui.ButtonBase(c.Key(key)).Row().Height(25).Gap(8).AlignItems(ui.Center).Label(locales.Browse() + " " + strings.Join(names, " ")).Expanded(open)
		if row.Clicked() {
			open = !open
			a.opened[key] = open
		}
		row.Children(func() {
			ui.Box(c).Width(16).Center().Children(func() { sym(c, "file-search", 15, p.Mute) })
			ui.Text(c, locales.Browse()).FontSize(13.5).TextColor(p.Fg2).SingleLine().Shrink(0)
			ui.Text(c, strings.Join(names, " ")).Font("monospace").FontSize(13).TextColor(p.Fg).SingleLine().PreserveSpaces().Ellipsis("…").MinWidth(0)
			ui.Spacer(c)
			if row.Hovered() {
				sym(c, map[bool]string{true: "chevron-down", false: "chevron-right"}[open], 10, p.Mute)
			}
		})
	} else {
		row := ui.ButtonBase(c.Key(key)).Row().Height(26).Gap(7).AlignItems(ui.Center).Label(locales.ExpandSteps()).Expanded(open)
		if row.Clicked() {
			open = !open
			a.opened[key] = open
		}
		row.Children(func() {
			sym(c, map[bool]string{true: "chevron-down", false: "chevron-right"}[open], 13, p.Mute)
			ui.Text(c, strings.Join(parts, locales.ClauseSeparator())).FontSize(13.5).TextColor(p.Mute).SingleLine()
			ui.Spacer(c)
		})
	}
	if open {
		ui.Column(c).Margin(2, 0, 8, 0).Children(func() {
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
	p := a.p
	ui.Row(c).AlignItems(ui.Stretch).Children(func() {
		rail := ui.Box(c).Width(2).Shrink(0).Clip()
		if live {
			rail.Background(p.LiveBg)
			pulse := rail.Loop("pulse", 2800*time.Millisecond, ui.EaseInOut)
			rail.Children(func() {
				ui.Box(c).Absolute().Left(0).TopPercent(pulse*140 - 40).Width(2).HeightPercent(40).
					LinearGradient(ui.LinearGradient{Angle: 180, Stops: []ui.GradientStop{{Color: p.Live.Alpha(0), At: 0}, {Color: p.Live, At: .5}, {Color: p.Live.Alpha(0), At: 1}}})
			})
		}
		ui.Column(c).Grow(1).MinWidth(0).Padding(0, 0, 0, 12).Children(content)
	})
}

// running says the turn is at work before anything of it shows.
func (a *App) running(c *ui.Context) {
	p := a.p
	ui.Row(c).Height(28).Gap(8).AlignItems(ui.Center).Children(func() {
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
			sym(c, "plug-connected-x", 28, p.Minus)
			ui.Text(c, locales.KernelDisconnected()).FontSize(16).FontWeight(600).TextColor(p.Fg).Margin(8, 0, 0, 0)
			if a.C.Err != "" {
				ui.Text(c, a.C.Err).FontSize(13.5).TextColor(p.Fg2).MaxWidth(480).TextAlign(ui.Center).Selectable()
			}
			ui.Text(c, locales.KernelDisconnectedHint()).FontSize(13).TextColor(p.Mute)
		case a.C.Phase != client.Ready:
			ui.Image(c, mark).Size(56, 56).Opacity(.5)
			ui.Text(c, phaseText(a.C.Phase)+"…").FontSize(14).TextColor(p.Mute).Margin(12, 0, 0, 0)
		case a.C.Current == nil:
			ui.Image(c, mark).Size(56, 56)
			ui.Text(c, locales.StartNewSession()).FontSize(16).FontWeight(500).TextColor(p.Fg2).Margin(12, 0, 0, 0)
		default:
			a.hello(c)
		}
	})
}

// hello is a new session: what it works on, the composer in the middle
// and the sessions to go back to.
func (a *App) hello(c *ui.Context) {
	p := a.p
	cur := a.C.Current
	ui.Image(c, mark).Size(44, 44)
	ui.Text(c, locales.HelloTitle(filepath.Base(cur.Cwd))).FontSize(24).FontWeight(600).LetterSpacing(-.33).TextColor(p.Fg).Margin(22, 0, 6, 0)
	ui.Text(c, locales.HelloLead()).TextColor(p.Fg2).Margin(0, 0, 26, 0)
	ui.Column(c).Width(540).MaxWidthPercent(100).Children(func() { a.dock(c) })
	var recent []proto.Session
	for _, s := range a.C.Sessions {
		if s.ID != cur.ID && s.Cwd == cur.Cwd && len(recent) < 3 {
			recent = append(recent, s)
		}
	}
	if len(recent) > 0 {
		ui.Column(c).Width(540).MaxWidthPercent(100).Margin(22, 0, 0, 0).Children(func() {
			ui.Text(c, locales.Recent()).FontSize(12.5).FontWeight(600).TextColor(p.Mute).Margin(0, 0, 4, 10)
			for _, s := range recent {
				a.sessionRow(c, s)
			}
		})
	}
}

func (a *App) entry(c *ui.Context, e *client.Entry, live bool) {
	p := a.p
	switch e.Role {
	case client.User:
		// The user's words sit on the right in a bubble, as in every chat;
		// the agent's work runs down the left.
		// Its copy button comes up beside the bubble, on its left, so it
		// takes no line of its own.
		msg := ui.Row(c).Margin(4, 0, 18, 0).Justify(ui.End).AlignItems(ui.End).Gap(4)
		msg.Children(func() {
			if e.Text != "" {
				a.copyButton(c, e.Text, msg.Hovered())
			}
			ui.Column(c).MaxWidthPercent(80).Padding(9, 14).Radius(18).Background(p.Bubble).Gap(8).Children(func() {
				if e.Text != "" {
					ui.Text(c, e.Text).TextColor(p.Fg).LineHeight(1.55).Selectable()
				}
				if len(e.Images) > 0 {
					a.sentImages(c, e)
				}
			})
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
				// The icon lines up with the text above it.
				ui.Row(c).Margin(0, 0, 0, -6).Children(func() { a.copyButton(c, e.Text, msg.Hovered()) })
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
		row := ui.ButtonBase(c.Key(key)).Row().MinHeight(25).Gap(8).AlignItems(ui.Center).MinWidth(0).Label(e.Text).Disabled(e.Output == "")
		if row.Clicked() {
			a.opened[key] = !open
		}
		row.Children(func() {
			ui.Box(c).Width(16).Center().Children(func() { sym(c, icon, 14, col) })
			ui.Text(c, e.Text).FontSize(13.5).TextColor(p.Mute).MinWidth(0)
			if e.Output != "" {
				sym(c, map[bool]string{true: "chevron-down", false: "chevron-right"}[open], 10, p.Mute)
			}
		})
		if e.Note == "limit" && a.last(e) && !a.C.Busy && a.textButton(c, "go-on", locales.ContinueTurn()).Clicked() {
			a.do(a.C.Prompt(locales.ContinueTurn()))
		}
	})
	if open {
		ui.Text(c, e.Output).FontSize(13.5).LineHeight(1.55).TextColor(p.Fg2).Padding(9, 11).Radius(8).Background(p.Chip).
			Margin(0, 0, 8, 24).Selectable()
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
	row := ui.ButtonBase(c.Key("step:" + e.CallID)).Row().Height(25).Gap(8).AlignItems(ui.Center).Label(t.verb + " " + e.Detail)
	if row.Clicked() && e.Output != "" && !e.Skipped {
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
		case e.Output != "" && row.Hovered():
			sym(c, map[bool]string{true: "chevron-down", false: "chevron-right"}[open], 10, p.Mute)
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
			ui.Text(c, kind).FontSize(13.5).FontWeight(600).TextColor(p.Fg)
			ui.Text(c, locales.NeedsApproval()).FontSize(13.5).TextColor(p.Fg2)
			ui.Spacer(c)
			if w := a.waited(c, ap.ID); w != "" {
				sym(c, "clock", 12, p.Mute)
				ui.Text(c, w).FontSize(12.5).TextColor(p.Mute)
			}
		})
		what := ap.Command
		if ap.Kind == "mcp" {
			what += "\n" + compact(ap.Args)
		}
		if what == "" && len(ap.Paths) > 0 {
			what = strings.Join(ap.Paths, "\n")
		}
		if what == "" {
			what = compact(ap.Args)
		}
		ui.Row(c).Padding(9, 12).Radius(9).Background(p.Chip).Margin(11, 0, 9, 0).Children(func() {
			if ap.Command != "" && ap.Kind != "mcp" && a.C.Current != nil {
				// A long folder gives way to the command, cut short in
				// the middle so both ends still say where it is.
				cwd := home(a.C.Current.Cwd)
				ui.Text(c, cwd).Font("monospace").FontSize(13.5).TextColor(p.Mute).SingleLine().TruncateMiddle().MaxWidth(160).Shrink(0).Unselectable().Tooltip(cwd)
				ui.Text(c, "$").Font("monospace").FontSize(13.5).TextColor(p.Mute).Shrink(0).Margin(0, 8, 0, 6).Unselectable()
			}
			ui.Text(c, what).Font("monospace").FontSize(13.5).TextColor(p.Fg).Selectable().Grow(1).MinWidth(0)
		})
		ui.Row(c).Gap(14).Children(func() {
			why := func(symbol, s string) {
				ui.Row(c).Gap(4).AlignItems(ui.Center).MinWidth(0).Children(func() {
					sym(c, symbol, 12, p.Mute)
					ui.Text(c, s).FontSize(13).TextColor(p.Mute)
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
		})
		ui.Row(c).Gap(8).Justify(ui.End).Margin(12, 0, 0, 0).Children(func() {
			if a.button(c, "deny:"+ap.ID, locales.Deny(), a.approvalHint(c, ap.ID, "esc"), false).Disabled(busy).Clicked() {
				a.do(a.C.Answer(ap.ID, false, false))
			}
			if a.button(c, "always:"+ap.ID, locales.AllowForSession(), a.approvalHint(c, ap.ID, "⌘⏎"), false).Disabled(busy).Clicked() {
				a.do(a.C.Answer(ap.ID, true, true))
			}
			if a.button(c, "allow:"+ap.ID, locales.Allow(), a.approvalHint(c, ap.ID, "⏎"), true).Disabled(busy).Clicked() {
				a.do(a.C.Answer(ap.ID, true, false))
			}
		})
	})
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
	b := ui.ButtonBase(c.Key(key)).Row().Height(30).Padding(0, 12).Gap(8).Radius(8).AlignItems(ui.Center).Label(label)
	press(b)
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
		ui.Text(c, label).FontSize(13.5).FontWeight(500).TextColor(fg).SingleLine()
		if kbd != "" {
			ui.Text(c, kbd).FontSize(12).TextColor(hint).SingleLine()
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
			a.list.ScrollTo(i, ui.Center)
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
	target := float32(0)
	if shown || done || b.Focused() {
		target = 1
	}
	b.Opacity(b.AnimateTo("shown", target, ui.Animation{Duration: 120 * time.Millisecond}))
	if b.Hovered() {
		b.Background(p.Hover)
	}
	b.Children(func() {
		if done {
			sym(c, "check", 14, p.Fg2)
		} else {
			sym(c, "copy", 14, p.Mute)
		}
	})
	if b.Clicked() {
		c.WriteClipboard(text)
		copied[key] = c.Now()
		c.After(2 * time.Second)
	}
}
