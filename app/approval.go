package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/locales"
	"github.com/shqingda/stratum/proto"
)

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
			var parts []string
			if ap.Reason != "" {
				r := ap.Reason
				if say, ok := reasons[ap.Reason]; ok {
					r = say()
				}
				parts = append(parts, r)
			}
			if ap.Rule != nil && !editing {
				parts = append(parts, locales.RuleScope(ruleText(*ap.Rule)))
			}
			if len(parts) > 0 {
				why("shield-lock", strings.Join(parts, locales.ClauseSeparator()))
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

// waiting is the card of what waits on the user, cleanly bordered over the transcript.
func (a *App) waiting(c *ui.Context) ui.Element {
	p := a.p
	card := ui.Column(c).Margin(6, 0, 18, 0).Padding(14, 14, 14, 16).Radius(14).Background(p.Card).
		Border(.5, p.Edge2).
		Transition(ui.ElementTransition{Enter: &ui.Motion{Opacity: 0}, Duration: 140 * time.Millisecond})
	return raise(card, p)
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

// home shortens a path in the home directory to start with ~.
func home(path string) string {
	if h, err := os.UserHomeDir(); err == nil && (path == h || strings.HasPrefix(path, h+"/")) {
		return "~" + path[len(h):]
	}
	return path
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
