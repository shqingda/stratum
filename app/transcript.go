package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/shqingda/talos/client"
	"github.com/shqingda/talos/proto"
)

const columnWidth = 640

// tools is how a tool call shows: its symbol and verb.
var tools = map[string]struct{ symbol, verb string }{
	"read_file":  {"doc.text", "读取"},
	"write_file": {"square.and.pencil", "写入"},
	"edit_file":  {"pencil", "编辑"},
	"list_dir":   {"folder", "列出"},
	"search":     {"magnifyingglass", "搜索"},
	"shell":      {"terminal", "运行"},
	"git_status": {"arrow.triangle.branch", "git status"},
	"git_diff":   {"plusminus", "git diff"},
	"skill":      {"sparkles", "技能"},
}

var kinds = map[string]string{"read": "读取文件", "write": "修改文件", "exec": "运行命令"}

var modes = map[string]string{"ask": "询问", "auto": "自动", "full": "完全"}

func (a *App) bar(c *ui.Context) {
	p := a.p
	ui.Row(c).Height(barHeight).Shrink(0).Padding(0, 14, 0, 20).Gap(10).AlignItems(ui.Center).DragWindow().Children(func() {
		cur := a.C.Current
		if cur == nil {
			ui.Text(c, phaseText(a.C.Phase)).FontSize(13.5).FontWeight(600).TextColor(p.Mute)
			return
		}
		ui.Text(c, title(*cur)).FontSize(13.5).FontWeight(600).TextColor(p.Fg).SingleLine().Ellipsis("…").MinWidth(0)
		ui.Row(c).Gap(5).AlignItems(ui.Center).Shrink(0).Children(func() {
			sym(c, "folder", 12, p.Mute)
			ui.Text(c, filepath.Base(cur.Cwd)).FontSize(12.5).TextColor(p.Mute).SingleLine()
		})
		ui.Spacer(c)
		a.reviewToggle(c)
	})
}

func (a *App) transcript(c *ui.Context) {
	p := a.p
	if len(a.C.Entries) == 0 && len(a.C.Approvals) == 0 {
		a.empty(c)
		return
	}
	a.list.Key = func(i int) any {
		switch {
		case i < len(a.C.Entries):
			return a.C.Entries[i].Key
		case i < len(a.C.Entries)+len(a.C.Approvals):
			return "ask:" + a.C.Approvals[i-len(a.C.Entries)].ID
		}
		return "running"
	}
	a.list.FollowEnd = true
	a.list.FollowAnimation = &ui.Smooth
	a.md.Theme = mdTheme(p, c.Theme())
	a.md.CodeBlock = a.codeBlock
	live := a.liveFrom()
	list := ui.List(c, &a.list, a.rows(), func(i int) {
		ui.Row(c).Justify(ui.Center).Padding(0, 28).Children(func() {
			ui.Column(c).Grow(1).MaxWidth(columnWidth).Children(func() {
				switch {
				case i < len(a.C.Entries):
					a.entry(c, a.C.Entries[i], live >= 0 && i >= live)
				case i < len(a.C.Entries)+len(a.C.Approvals):
					a.turn(c, true, func() { a.ask(c, a.C.Approvals[i-len(a.C.Entries)]) })
				default:
					a.turn(c, true, func() { a.running(c) })
				}
			})
		})
	}).Grow(1).Padding(18, 0, 24, 0)
	// Rows fade out under the bar rather than being cut at its edge.
	if h := list.Bounds().H; h > 0 {
		black := ui.RGBA(0, 0, 0, 1)
		list.MaskLinearGradient(ui.LinearGradient{Angle: 180, Stops: []ui.GradientStop{{Color: ui.Transparent, At: 0}, {Color: black, At: 28 / h}, {Color: black, At: 1}}})
	}
}

// rows are the entries, the approvals after them, and a row saying the
// turn is at work while nothing else shows it.
func (a *App) rows() int {
	n := len(a.C.Entries) + len(a.C.Approvals)
	if a.thinking() {
		n++
	}
	return n
}

// thinking is a running turn whose last row shows nothing moving: no
// text streaming in, no tool running, no approval waiting.
func (a *App) thinking() bool {
	if !a.C.Busy || len(a.C.Approvals) > 0 {
		return false
	}
	if n := len(a.C.Entries); n > 0 {
		e := a.C.Entries[n-1]
		if !e.Done && (e.Role == client.Assistant || e.Role == client.Tool) {
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
		dot := ui.Box(c).Size(7, 7).Radius(3.5).Background(p.Live)
		dot.Opacity(.35 + .65*(1-dot.Loop("beat", 1600*time.Millisecond, ui.Bounce(ui.EaseInOut))))
		ui.Text(c, "思考中").FontSize(12.5).TextColor(p.Fg2)
	})
}

func (a *App) empty(c *ui.Context) {
	p := a.p
	ui.Column(c).Grow(1).Center().Gap(10).Padding(0, 28, 60, 28).Children(func() {
		switch {
		case a.C.Phase == client.Disconnected:
			sym(c, "bolt.horizontal.circle", 28, p.Minus)
			ui.Text(c, "和内核的连接断开了").FontSize(15).FontWeight(600).TextColor(p.Fg).Margin(8, 0, 0, 0)
			if a.C.Err != "" {
				ui.Text(c, a.C.Err).FontSize(12.5).TextColor(p.Fg2).MaxWidth(480).TextAlign(ui.Center).Selectable()
			}
			ui.Text(c, "重新打开 Talos 会重启内核，没写完的轮次会从日志里恢复。").FontSize(12).TextColor(p.Mute)
		case a.C.Phase != client.Ready:
			ui.Image(c, mark).Size(56, 56).Opacity(.5)
			ui.Text(c, phaseText(a.C.Phase)+"…").FontSize(13).TextColor(p.Mute).Margin(12, 0, 0, 0)
		case a.C.Current == nil:
			ui.Image(c, mark).Size(56, 56)
			ui.Text(c, "新建一个会话开始").FontSize(15).FontWeight(500).TextColor(p.Fg2).Margin(12, 0, 0, 0)
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
	ui.Image(c, mark).Size(56, 56)
	ui.Text(c, "在 "+filepath.Base(cur.Cwd)+" 里做点什么").FontSize(22).FontWeight(600).LetterSpacing(-.33).TextColor(p.Fg).Margin(12, 0, 0, 0)
	ui.Text(c, "Talos 会先读代码，改动前按你的权限模式询问。").TextColor(p.Fg2).Margin(0, 0, 16, 0)
	ui.Column(c).Width(540).MaxWidthPercent(100).Children(func() { a.dock(c) })
	var recent []proto.Session
	for _, s := range a.C.Sessions {
		if s.ID != cur.ID && s.Cwd == cur.Cwd && len(recent) < 3 {
			recent = append(recent, s)
		}
	}
	if len(recent) > 0 {
		ui.Column(c).Width(540).MaxWidthPercent(100).Margin(12, 0, 0, 0).Children(func() {
			ui.Text(c, "继续").FontSize(11.5).FontWeight(600).TextColor(p.Mute).Margin(0, 0, 4, 10)
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
		ui.Row(c).Margin(4, 0, 18, 0).AlignItems(ui.Stretch).Children(func() {
			ui.Box(c).Width(2).Margin(3, 0, 3, 0).Background(p.Faint).Shrink(0)
			ui.Text(c, e.Text).FontWeight(500).TextColor(p.Fg).LineHeight(1.55).Padding(0, 0, 0, 12).Selectable().Grow(1).MinWidth(0)
		})
	case client.Assistant:
		a.turn(c, live, func() {
			ui.Column(c).Margin(4, 0, 10, 0).Children(func() {
				if e.Done {
					markdown.View(c, e.Text, &a.md)
				} else {
					markdown.Stream(c, e.Text, &a.md)
				}
			})
		})
	case client.Tool:
		a.turn(c, live, func() { a.card(c, e) })
	case client.Notice:
		a.turn(c, live, func() {
			ui.Text(c, e.Text).FontSize(12).TextColor(p.Mute).Margin(4, 0, 10, 0).Selectable()
		})
	}
}

// step is a tool call: one line, its output under it once opened.
func (a *App) step(c *ui.Context, e *client.Entry) {
	p := a.p
	t, ok := tools[e.Text]
	if !ok {
		t.symbol, t.verb = "wrench.and.screwdriver", e.Text
	}
	open := a.opened[e.CallID]
	row := ui.ButtonBase(c.Key("step:" + e.CallID)).Row().Height(25).Gap(8).AlignItems(ui.Center).Label(t.verb + " " + e.Detail)
	if row.Clicked() && e.Output != "" {
		a.opened[e.CallID] = !open
	}
	row.Children(func() {
		ui.Box(c).Width(16).Center().Children(func() { sym(c, t.symbol, 14, p.Mute) })
		ui.Text(c, t.verb).FontSize(12.5).TextColor(p.Fg2).SingleLine().Shrink(0)
		if e.Detail != "" {
			ui.Text(c, e.Detail).Font("monospace").FontSize(12).TextColor(p.Fg).SingleLine().Ellipsis("…").MinWidth(0)
		}
		ui.Spacer(c)
		switch {
		case !e.Done:
			ui.Box(c).Size(2, 12).Background(p.Live)
		case e.Failed:
			ui.Box(c).Size(7, 2).Background(p.Minus)
		case e.Output != "" && row.Hovered():
			sym(c, map[bool]string{true: "chevron.down", false: "chevron.right"}[open], 10, p.Mute)
		}
	})
	if open {
		out := e.Output
		if lines := strings.Split(out, "\n"); len(lines) > 40 {
			out = strings.Join(lines[:40], "\n") + fmt.Sprintf("\n… 还有 %d 行", len(lines)-40)
		}
		ui.Text(c, out).Font("monospace").FontSize(12).TextColor(p.Fg2).Padding(9, 11).Radius(8).Background(p.Chip).
			Margin(2, 0, 8, 24).Selectable().PreserveSpaces()
	}
}

// ask is an approval request: what the tool would do, and the three
// answers.
func (a *App) ask(c *ui.Context, ap proto.Approval) {
	p := a.p
	busy := a.C.Answering[ap.ID]
	ui.Column(c).Margin(12, 0).Padding(14, 14, 12, 16).Radius(12).Background(p.Card).Border(.5, p.Edge2).
		Shadow(0, 0, 0, 4, p.LiveBg).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			slot(c, p, "wait")
			kind := kinds[ap.Kind]
			if kind == "" {
				kind = ap.Tool
			}
			ui.Text(c, kind).FontSize(12.5).FontWeight(600).TextColor(p.Fg)
			ui.Text(c, "需要你允许").FontSize(12.5).TextColor(p.Fg2)
			ui.Spacer(c)
			if w := a.waited(c, ap.ID); w != "" {
				sym(c, "clock", 11, p.Mute)
				ui.Text(c, w).FontSize(11.5).TextColor(p.Mute)
			}
		})
		what := ap.Command
		if what == "" && len(ap.Paths) > 0 {
			what = strings.Join(ap.Paths, "\n")
		}
		if what == "" {
			what = compact(ap.Args)
		}
		ui.Row(c).Padding(9, 11).Radius(8).Background(p.Chip).Margin(10, 0, 8, 0).Children(func() {
			if ap.Command != "" && a.C.Current != nil {
				ui.Text(c, home(a.C.Current.Cwd)+" $").Font("monospace").FontSize(12.5).TextColor(p.Mute).Shrink(0).Margin(0, 7.5, 0, 0).Unselectable()
			}
			ui.Text(c, what).Font("monospace").FontSize(12.5).TextColor(p.Fg).Selectable().Grow(1).MinWidth(0)
		})
		ui.Row(c).Gap(14).Children(func() {
			why := func(symbol, s string) {
				ui.Row(c).Gap(4).AlignItems(ui.Center).MinWidth(0).Children(func() {
					sym(c, symbol, 11, p.Mute)
					ui.Text(c, s).FontSize(12).TextColor(p.Mute)
				})
			}
			if ap.Reason != "" {
				why("lock.shield", ap.Reason)
			}
			if cur := a.C.Current; cur != nil {
				why("slider.horizontal.3", "当前模式："+modes[cur.Mode])
			}
		})
		ui.Row(c).Gap(8).Justify(ui.End).Margin(12, 0, 0, 0).Children(func() {
			if a.button(c, "deny:"+ap.ID, "拒绝", a.approvalHint(c, ap.ID, "esc"), false).Disabled(busy).Clicked() {
				a.do(a.C.Answer(ap.ID, false, false))
			}
			if a.button(c, "always:"+ap.ID, "本会话都允许", a.approvalHint(c, ap.ID, "⌘⏎"), false).Disabled(busy).Clicked() {
				a.do(a.C.Answer(ap.ID, true, true))
			}
			if a.button(c, "allow:"+ap.ID, "允许", a.approvalHint(c, ap.ID, "⏎"), true).Disabled(busy).Clicked() {
				a.do(a.C.Answer(ap.ID, true, false))
			}
		})
	})
}

// button is a 28-point button with an optional key hint; primary ones are
// ink on paper turned over.
func (a *App) button(c *ui.Context, key, label, kbd string, primary bool) ui.Element {
	p := a.p
	b := ui.ButtonBase(c.Key(key)).Row().Height(28).Padding(0, 10).Gap(6).Radius(7).AlignItems(ui.Center).Label(label)
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
		return fmt.Sprintf("等了 %d 秒", int(d.Seconds()))
	}
	return fmt.Sprintf("等了 %d 分", int(d.Minutes()))
}

// home shortens a path in the home directory to start with ~.
func home(path string) string {
	if h, err := os.UserHomeDir(); err == nil && (path == h || strings.HasPrefix(path, h+"/")) {
		return "~" + path[len(h):]
	}
	return path
}
