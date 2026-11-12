package app

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

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
	})
}

// row is one item of the transcript list: an entry, or an approval after
// them.
func (a *App) rows() int { return len(a.C.Entries) + len(a.C.Approvals) }

func (a *App) transcript(c *ui.Context) {
	p := a.p
	if len(a.C.Entries) == 0 && len(a.C.Approvals) == 0 {
		a.empty(c)
		return
	}
	a.list.Key = func(i int) any {
		if i < len(a.C.Entries) {
			return a.C.Entries[i].Key
		}
		return "ask:" + a.C.Approvals[i-len(a.C.Entries)].ID
	}
	a.list.FollowEnd = true
	a.list.FollowAnimation = &ui.Smooth
	a.md.Theme = &markdown.Theme{
		FontSize: 13.5, LineHeight: 1.55, Text: p.Fg, Muted: p.Mute, Link: p.Fg, Strong: p.Fg,
		Code: p.Fg, CodeBackground: p.Chip,
	}
	ui.List(c, &a.list, a.rows(), func(i int) {
		ui.Row(c).Justify(ui.Center).Padding(0, 28).Children(func() {
			ui.Column(c).Grow(1).MaxWidth(columnWidth).Children(func() {
				if i < len(a.C.Entries) {
					a.entry(c, a.C.Entries[i], i)
				} else {
					a.ask(c, a.C.Approvals[i-len(a.C.Entries)])
				}
			})
		})
	}).Grow(1).Padding(18, 0, 24, 0)
}

func (a *App) empty(c *ui.Context) {
	p := a.p
	ui.Column(c).Grow(1).Center().Gap(14).Children(func() {
		ui.Image(c, mark).Size(56, 56)
		msg := "要做点什么？"
		switch {
		case a.C.Phase != 3: // client.Ready
			msg = "正在连接内核…"
		case a.C.Current == nil:
			msg = "新建一个会话开始"
		}
		ui.Text(c, msg).FontSize(15).FontWeight(500).TextColor(p.Fg2)
	})
}

func (a *App) entry(c *ui.Context, e *client.Entry, i int) {
	p := a.p
	switch e.Role {
	case client.User:
		ui.Row(c).Margin(4, 0, 18, 0).AlignItems(ui.Stretch).Children(func() {
			ui.Box(c).Width(2).Margin(3, 0, 3, 0).Background(p.Faint).Shrink(0)
			ui.Text(c, e.Text).FontWeight(500).TextColor(p.Fg).LineHeight(1.55).Padding(0, 0, 0, 12).Selectable().Grow(1).MinWidth(0)
		})
	case client.Assistant:
		ui.Column(c).Margin(4, 0, 10, 0).Children(func() {
			if e.Done {
				markdown.View(c, e.Text, &a.md)
			} else {
				markdown.Stream(c, e.Text, &a.md)
			}
		})
	case client.Tool:
		a.step(c, e)
	case client.Notice:
		ui.Text(c, e.Text).FontSize(12).TextColor(p.Mute).Margin(4, 0, 10, 0).Selectable()
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
		ui.Box(c).Width(16).Center().Children(func() { sym(c, t.symbol, 13, p.Mute) })
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
		})
		what := ap.Command
		if what == "" && len(ap.Paths) > 0 {
			what = strings.Join(ap.Paths, "\n")
		}
		if what == "" {
			what = compact(ap.Args)
		}
		ui.Text(c, what).Font("monospace").FontSize(12.5).TextColor(p.Fg).Padding(9, 11).Radius(8).Background(p.Chip).
			Margin(10, 0, 8, 0).Selectable()
		if ap.Reason != "" {
			ui.Text(c, ap.Reason).FontSize(12).TextColor(p.Mute)
		}
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
