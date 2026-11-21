package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/locales"
)

// remark is a comment on a line of the diff, kept until the batch goes
// to the agent.
type remark struct {
	path string
	line int  // from 1, in the file as it is, or as it was when old
	old  bool // the line was removed
	code string
	text string
}

func (r remark) at() string { return fmt.Sprintf("%s\x00%d:%v", r.path, r.line, r.old) }

// lineRemark is the remark a diff line would take.
func lineRemark(path string, d *fileDiff, e edit) remark {
	if e.op == '-' {
		return remark{path: path, line: e.a + 1, old: true, code: d.old[e.a]}
	}
	return remark{path: path, line: e.b + 1, code: d.new[e.b]}
}

// remarksAt shows the remarks on a line, and the field one is written in.
func (a *App) remarksAt(c *ui.Context, at remark) {
	p := a.p
	for i, r := range a.rv.remarks {
		if r.at() != at.at() {
			continue
		}
		ui.Row(c.Key(fmt.Sprintf("remark:%d", i))).Margin(4, 12, 4, 92).Padding(6, 6, 6, 10).Gap(8).Radius(8).
			Background(p.Card).Border(.5, p.Edge2).AlignItems(ui.Start).Children(func() {
			sym(c, "message", 14, p.Mute)
			ui.Text(c, r.text).FontSize(13).TextColor(p.Fg).Grow(1).MinWidth(0).Selectable()
			if a.iconPill(c, fmt.Sprintf("unremark:%d", i), "x", locales.RemoveRemark()).Size(24, 24).Clicked() {
				a.rv.remarks = append(a.rv.remarks[:i:i], a.rv.remarks[i+1:]...)
			}
		})
	}
	if a.rv.writing != at.at() {
		return
	}
	ui.Row(c.Key("remark-new")).Margin(4, 12, 4, 92).Padding(4, 4, 4, 10).Gap(6).Radius(8).
		Background(p.Card).Border(1, p.Acc).AlignItems(ui.Center).Children(func() {
		ui.TextInputBase(c, &a.rv.remarkDraft).Bind(&a.rv.remarkInput).Label(locales.AddRemark()).
			Placeholder(locales.RemarkPlaceholder()).PlaceholderColor(p.Mute).FontSize(13).TextColor(p.Fg).
			Grow(1).MinWidth(0).SingleLine().OnTextKey(func(k ui.TextKey) bool {
			a.rv.remarkDraft = k.Text()
			if k.Key == ui.KeyEnter && k.Mods == 0 {
				a.addRemark(at)
				return true
			}
			return false
		})
		if a.barButton(c, "remark-cancel", "x", locales.Cancel(), func() {}).Height(24).Clicked() {
			a.rv.writing = ""
		}
		if a.barButton(c, "remark-add", "plus", locales.AddIt(), func() {}).Height(24).Disabled(strings.TrimSpace(a.rv.remarkDraft) == "").Clicked() {
			a.addRemark(at)
		}
	})
}

// writeRemark opens the field for a remark on a line.
func (a *App) writeRemark(c *ui.Context, at remark) {
	a.rv.writing, a.rv.remarkDraft = at.at(), ""
	a.rv.remarkInput.Focus(c)
}

func (a *App) addRemark(at remark) {
	text := strings.TrimSpace(a.rv.remarkDraft)
	if text == "" {
		return
	}
	at.text = text
	a.rv.remarks = append(a.rv.remarks, at)
	a.rv.writing, a.rv.remarkDraft = "", ""
}

// remarksBar holds the batch of remarks over the panel's foot.
func (a *App) remarksBar(c *ui.Context) {
	if len(a.rv.remarks) == 0 {
		return
	}
	p := a.p
	ui.Row(c).Height(40).Shrink(0).Padding(0, 10, 0, 14).Gap(8).AlignItems(ui.Center).BorderWidth(.5, 0, 0, 0).BorderColor(p.Edge).Children(func() {
		sym(c, "message", 14, p.Mute)
		ui.Text(c, locales.RemarkCount(fmt.Sprint(len(a.rv.remarks)))).FontSize(13).TextColor(p.Fg2)
		ui.Spacer(c)
		if a.barButton(c, "remarks-clear", "trash", locales.ClearRemarks(), func() {}).Height(26).Clicked() {
			a.rv.remarks, a.rv.writing = nil, ""
		}
		if a.barButton(c, "remarks-send", "send", locales.SendRemarks(), func() {}).Height(26).Clicked() {
			a.sendRemarks()
		}
	})
}

// sendRemarks sends the remarks as one prompt, each with its place and
// the line it is on.
func (a *App) sendRemarks() {
	var b strings.Builder
	b.WriteString(locales.RemarksIntro())
	for _, r := range a.rv.remarks {
		dir, base := a.splitPath(r.path)
		where := fmt.Sprintf("%s%s:%d", dir, base, r.line)
		if r.old {
			where += " (" + locales.RemovedLine() + ")"
		}
		fmt.Fprintf(&b, "\n\n`%s`\n> %s\n%s", where, strings.TrimSpace(r.code), r.text)
	}
	cmds, err := a.C.Prompt(b.String())
	if a.do(cmds, err); err == nil {
		a.rv.remarks, a.rv.writing = nil, ""
	}
}
