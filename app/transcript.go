package app

import (
	"fmt"
	"hash/fnv"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/shqingda/stratum/client"
	"github.com/shqingda/stratum/locales"
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
	ui.Row(c).Height(sheetBar).Shrink(0).Padding(0, 12, 0, 14).Gap(10).AlignItems(ui.Center).
		DragWindow().Children(func() {
		if a.side.noSide {
			if exp := a.iconButtonSized(c, "side-expand", "layout-sidebar-left-expand", locales.ShowSidebar(), 18); exp.Clicked() {
				a.toggleSide()
			}
		}
		cur := a.C.Current
		if cur == nil {
			ui.Text(c, phaseText(a.C.Phase)).FontSize(15).FontWeight(600).TextColor(p.OnSurfaceVariant)
			return
		}
		if !a.helloShown() {
			if a.renaming == cur.ID && a.renameIn == "bar" {
				a.renameInput(c, *cur).FontSize(15).FontWeight(600).MaxWidth(360)
			} else {
				t := ui.ButtonBase(c.Key("bar-title")).Row().MinWidth(0).Shrink(1).Padding(4, 10).Margin(0, 0, 0, -6).Radius(10).
					Label(locales.Rename()).Tooltip(locales.Rename())
				m3Focus(t, p)
				m3State(t, p, ui.Transparent, false)
				if t.Clicked() {
					a.rename(*cur, "bar")
				}
				t.Children(func() {
					ui.Text(c, title(*cur)).FontSize(15).FontWeight(600).TextColor(p.OnSurface).SingleLine().Ellipsis("…").MinWidth(0)
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
	ui.Box(c.Key("transcript-wrap")).Grow(1).MinHeight(0).Children(func() {
		ui.List(c, st, n, func(i int) {
			row := ui.Row(c).Justify(ui.Center).Padding(0, 28)
			row.Children(func() {
				col := ui.Column(c).Grow(1).MaxWidth(columnWidth)
				if i == hit {
					col.Margin(-4, -10, -4, -10).Padding(4, 10, 4, 10).Radius(12).Background(p.PrimaryContainer.Alpha(.45))
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
		}).Fill().Padding(18, 0, 18, 0)
		shown := a.listShown == st
		a.listShown = st
		if len(items) > 0 && shown && !st.AtEnd() {
			a.scrollBottomPill(c)
		}
	})
}

func (a *App) scrollBottomPill(c *ui.Context) {
	p := a.p
	ui.Row(c).Absolute().Left(0).Right(0).Bottom(14).Justify(ui.Center).PassThrough().ZIndex(15).Children(func() {
		b := ui.ButtonBase(c.Key("scroll-bottom")).Row().Height(34).Padding(0, 14).Gap(6).Radius(17).
			AlignItems(ui.Center).Label(locales.ScrollToBottom()).Tooltip(locales.ScrollToBottom()).
			Transition(ui.ElementTransition{
				Enter:    &ui.Motion{Y: 10, Scale: .92, Opacity: 0},
				Exit:     &ui.Motion{Y: 8, Scale: .94, Opacity: 0},
				Duration: 180 * time.Millisecond,
			})
		m3Focus(b, p)
		m3State(b, p, p.SurfaceContainerHigh, false)
		b.Shadow(0, 2, 8, 0, ui.RGBA(0, 0, 0, .08))
		b.Children(func() {
			if a.C.Busy {
				ui.Box(c).Size(7, 7).Radius(3.5).Background(p.Primary)
			}
			ui.Text(c, locales.ScrollToBottom()).FontSize(12.5).FontWeight(600).TextColor(p.OnSurface).SingleLine()
			sym(c, "chevron-down", 14, p.OnSurfaceVariant)
		})
		if b.Clicked() {
			a.transcriptList().ScrollToEnd()
		}
	})
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
		rail := ui.Box(c).Width(2.5).Radius(1.25).Shrink(0).Clip()
		if live {
			if still || len(a.C.Approvals) > 0 || a.reduceMotion() {
				rail.Background(p.Primary.Alpha(.5))
			} else {
				rail.Background(p.PrimaryContainer)
				pulse := rail.Loop("pulse", 2200*time.Millisecond, ui.EaseInOut)
				rail.Children(func() {
					ui.Box(c).Absolute().Left(0).TopPercent(pulse*140 - 40).Width(2.5).HeightPercent(40).
						LinearGradient(ui.LinearGradient{Angle: 180, Stops: []ui.GradientStop{{Color: p.Primary.Alpha(0), At: 0}, {Color: p.Primary, At: .5}, {Color: p.Primary.Alpha(0), At: 1}}})
				})
			}
		}
		ui.Column(c).Grow(1).MinWidth(0).Padding(0, 0, 0, 14).Children(content)
	})
}

func (a *App) running(c *ui.Context) {
	p := a.p
	ui.Row(c).Height(30).Gap(8).AlignItems(ui.Center).Children(func() {
		if a.reduceMotion() {
			ui.Box(c).Size(8, 8).Radius(4).Background(p.Primary)
		} else {
			spinner(c, p, 14)
		}
		ui.Text(c, locales.ThinkingNow()).FontSize(13.5).FontWeight(500).TextColor(p.OnSurfaceVariant)
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
				bubbleBg := p.SurfaceContainerLow
				if p.Dark {
					bubbleBg = p.SurfaceContainerHigh
				}
				ui.Column(c).MaxWidthPercent(80).Padding(11, 16).Radius(18, 6, 18, 18).Background(bubbleBg).Children(func() {
					a.userMessageText(c, e.Text)
				})
			}
			ts := ""
			if e.Turn >= 0 && e.Turn < len(a.C.Turns) {
				ts = a.msgTime(a.C.Turns[e.Turn].Start)
			}
			if ts != "" || e.Text != "" || a.rewindable(e) {
				actVisible := msg.Hovered() || msg.FocusWithin()
				ui.Row(c).Gap(2).AlignItems(ui.Center).Children(func() {
					if ts != "" {
						tbox := ui.Box(c).Padding(0, 4).Children(func() {
							ui.Text(c, ts).FontSize(12).TextColor(p.OnSurfaceVariant).SingleLine()
						})
						target := float32(0)
						if actVisible {
							target = 1
						}
						tbox.Opacity(tbox.AnimateTo("ts-shown", target, ui.Animation{Duration: 120 * time.Millisecond}))
					}
					if e.Text != "" {
						a.copyButton(c, e.Text, actVisible)
					}
					if a.rewindable(e) {
						if e.Text != "" {
							a.editResendButton(c, e, actVisible)
						}
						a.rewindButton(c, e, actVisible)
					}
				})
			}
		})
	default:
		if e.Typed {
			ui.Column(c).Margin(2, 0, 10, 0).Children(func() { a.card(c, e) })
			return
		}
		a.turn(c, live, func() { a.body(c, e) })
	}
}

func (a *App) userMessageText(c *ui.Context, text string) {
	p := a.p
	toks := refTokens(text, nil)
	if len(toks) == 0 {
		ui.Text(c, text).FontSize(14.5).TextColor(p.OnSurface).LineHeight(1.55).Selectable()
		return
	}
	blue := p.Primary
	var spans []ui.Span
	pos := 0
	for _, t := range toks {
		if t.start > pos {
			spans = append(spans, ui.Span{Text: text[pos:t.start], Size: 14.5, Color: p.OnSurface})
		}
		spans = append(spans,
			ui.Span{Text: text[t.start : t.start+1], Size: 14.5, Color: blue.Alpha(.75)},
			ui.Span{Text: text[t.start+1 : t.end], Size: 14.5, Color: blue, Weight: 600},
		)
		pos = t.end
	}
	if pos < len(text) {
		spans = append(spans, ui.Span{Text: text[pos:], Size: 14.5, Color: p.OnSurface})
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
				// Action bar below finished assistant reply: last reply keeps quiet actions;
				// older replies and timestamps reveal on hover.
				p := a.p
				ts := a.msgTime(a.turnTimestamp(e.Turn))
				canRetry := a.canRetryTurn(e.Turn)
				isLast := a.isLastAssistant(e)
				act := ui.Row(c).Margin(2, 0, 0, -6).Gap(2).AlignItems(ui.Center)
				actHover := msg.Hovered() || msg.FocusWithin()
				if !isLast {
					target := float32(0)
					if actHover {
						target = 1
					}
					act.Opacity(act.AnimateTo("reply-act", target, ui.Animation{Duration: 120 * time.Millisecond}))
				}
				act.Children(func() {
					a.copyButton(c, e.Text, true)
					if canRetry {
						a.retryButton(c, e.Turn)
					}
					if ts != "" {
						tbox := ui.Box(c).Padding(0, 6).Children(func() {
							ui.Text(c, ts).FontSize(12).TextColor(p.OnSurfaceVariant).SingleLine()
						})
						target := float32(0)
						if actHover {
							target = 1
						}
						tbox.Opacity(tbox.AnimateTo("reply-ts", target, ui.Animation{Duration: 120 * time.Millisecond}))
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
	icon, col := noteIcons[e.Note], p.OnSurfaceVariant
	if icon == "" {
		icon = "info-circle"
	}
	if e.Note == "fail" {
		col = p.Error
	}
	key := "note:" + strconv.Itoa(e.Key)
	open := a.opened[key]
	ui.Row(c).Gap(8).AlignItems(ui.Center).Margin(2, 0, 6, 0).Children(func() {
		row := ui.ButtonBase(c.Key(key)).Row().MinHeight(28).Padding(0, 6).Margin(0, -6).Gap(8).Radius(8).AlignItems(ui.Center).MinWidth(0).Label(e.Text).Disabled(e.Output == "")
		if e.Output != "" {
			m3Focus(row, p)
			m3State(row, p, ui.Transparent, false)
		}
		if row.Clicked() {
			a.transcriptList().HoldPlace()
			a.opened[key] = !open
		}
		row.Children(func() {
			ui.Box(c).Width(16).Center().Children(func() { sym(c, icon, 14, col) })
			ui.Text(c, e.Text).FontSize(13.5).TextColor(p.OnSurfaceVariant).MinWidth(0)
			if e.Output != "" {
				disclosureChevron(c, key, open, 10, p.OnSurfaceVariant)
			}
		})
		if e.Note == "limit" && a.last(e) && !a.C.Busy && a.textButton(c, "go-on", locales.ContinueTurn()).Clicked() {
			a.do(a.C.Prompt(locales.ContinueTurn()))
		}
		if e.Note == "fail" && a.last(e) && !a.C.Busy && a.canRetryTurn(e.Turn) && a.textButton(c, "retry-failed", locales.Retry()).Clicked() {
			a.retryTurn(e.Turn)
		}
	})
	if open {
		ui.Text(c, e.Output).FontSize(13.5).LineHeight(1.55).TextColor(p.OnSurfaceVariant).Padding(10, 12).Radius(12).Background(p.SurfaceContainerLow).
			Margin(0, 0, 8, 24).Selectable().
			Transition(ui.ElementTransition{Enter: &ui.Motion{Opacity: 0}, Duration: 120 * time.Millisecond})
	}
}

// last reports whether e is the session's last entry.
func (a *App) last(e *client.Entry) bool {
	n := len(a.C.Entries)
	return n > 0 && a.C.Entries[n-1] == e
}

// isLastAssistant reports whether e is the last assistant reply in the session.
func (a *App) isLastAssistant(e *client.Entry) bool {
	for i := len(a.C.Entries) - 1; i >= 0; i-- {
		if a.C.Entries[i].Role == client.Assistant {
			return a.C.Entries[i] == e
		}
	}
	return false
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
	row := ui.ButtonBase(c.Key("step:"+e.CallID)).Row().Height(28).Padding(0, 8).Margin(0, -8).Gap(8).Radius(8).AlignItems(ui.Center).Label(t.verb + " " + e.Detail)
	if canOpen {
		m3Focus(row, p)
		m3State(row, p, ui.Transparent, false)
	}
	if row.Clicked() && canOpen {
		a.transcriptList().HoldPlace()
		a.opened[e.CallID] = !open
	}
	row.Children(func() {
		ui.Box(c).Width(16).Center().Children(func() { sym(c, t.symbol, 14, p.OnSurfaceVariant) })
		ui.Text(c, t.verb).FontSize(13).TextColor(p.OnSurfaceVariant).SingleLine().Shrink(0)
		if e.Detail != "" {
			d := ui.Text(c, e.Detail).FontSize(13).TextColor(p.OnSurface).SingleLine().Ellipsis("…").MinWidth(0)
			if e.Text != "task" && e.Text != "run_tasks" {
				d.Font("monospace")
			}
		}
		ui.Spacer(c)
		switch {
		case !e.Done:
			ui.Box(c).Size(3, 12).Radius(1.5).Background(p.Primary)
		case e.Skipped:
			ui.Text(c, locales.Skipped()).FontSize(12).TextColor(p.OnSurfaceVariant).SingleLine().Shrink(0)
		case e.Failed:
			sym(c, "alert-circle", 13, p.Error)
		case e.Output != "" && (row.Hovered() || row.FocusVisible() || open):
			disclosureChevron(c, "step:"+e.CallID, open, 10, p.OnSurfaceVariant)
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
		o := ui.Text(c, out).FontSize(13).TextColor(p.OnSurfaceVariant).Padding(10, 12).Radius(12).Background(p.SurfaceContainerLow).
			Margin(2, 0, 8, 24).Selectable().PreserveSpaces()
		if e.Text != "task" && e.Text != "run_tasks" {
			o.Font("monospace")
		}
	}
}

func (a *App) olderRow(c *ui.Context) {
	if !a.C.LoadingOlder {
		a.do(a.C.LoadOlder())
	}
	ui.Text(c, locales.LoadingEarlier()).FontSize(13).TextColor(a.p.OnSurfaceVariant).Center().Padding(4, 0, 16, 0)
}

// seek scrolls to the message a search chose, loading older pages until
// it is there.
func (a *App) seek(items []item) {
	cur := a.C.Current
	if a.search.jump == 0 || cur == nil || cur.ID != a.search.jumpIn || a.C.Phase != client.Ready {
		return
	}
	has := func(es []*client.Entry) bool {
		return slices.ContainsFunc(es, func(e *client.Entry) bool { return e.Seq == a.search.jump })
	}
	for i, it := range items {
		if it.e != nil && it.e.Seq == a.search.jump || has(it.work) || has(it.fold) {
			a.transcriptList().ScrollTo(i, ui.Center)
			a.search.jump = 0
			return
		}
	}
	if a.C.Older {
		if !a.C.LoadingOlder {
			a.do(a.C.LoadOlder())
		}
		return
	}
	a.search.jump = 0
}

// taskSteps are the last few steps of a running sub-agent, under its task.
func (a *App) taskSteps(c *ui.Context, steps []string) {
	p := a.p
	ui.Column(c).Margin(0, 0, 4, 24).Padding(0, 0, 0, 10).BorderWidth(0, 0, 0, 1).BorderColor(p.OutlineVariant).Children(func() {
		if n := len(steps) - 3; n > 0 {
			ui.Text(c, locales.EarlierSteps(n)).FontSize(12.5).TextColor(p.OnSurfaceVariant).Height(20)
			steps = steps[n:]
		}
		for i, s := range steps {
			name, detail, _ := strings.Cut(s, "\t")
			verb := name
			if t, ok := toolFor(name); ok {
				verb = t.verb
			}
			ui.Row(c.Key(i)).Height(20).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, verb).FontSize(12.5).TextColor(p.OnSurfaceVariant).Shrink(0)
				ui.Text(c, detail).Font("monospace").FontSize(12.5).TextColor(p.OnSurface).SingleLine().Ellipsis("…").MinWidth(0)
			})
		}
	})
}

func (a *App) taskGraph(c *ui.Context, tasks []*client.GraphTask) {
	p := a.p
	ui.Column(c).Margin(0, 0, 4, 24).Padding(0, 0, 0, 10).BorderWidth(0, 0, 0, 1).BorderColor(p.OutlineVariant).Children(func() {
		for i, t := range tasks {
			ui.Column(c.Key(i)).Children(func() {
				ui.Row(c).Height(22).Gap(7).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(12, 14).Shrink(0).Center().Children(func() {
						switch t.State {
						case "run":
							ui.Box(c).Size(3, 12).Radius(1.5).Background(p.Primary)
						case "done":
							sym(c, "check", 12, p.Plus)
						case "fail":
							sym(c, "alert-circle", 12, p.Error)
						default:
							ui.Box(c).Size(6, 6).Radius(3).Border(1, p.Outline)
						}
					})
					color := p.OnSurface
					if t.State == "" || t.State == "skip" {
						color = p.OnSurfaceVariant
					}
					ui.Text(c, t.Description).FontSize(13).TextColor(color).SingleLine().Ellipsis("…").MinWidth(0)
					switch {
					case t.State == "skip":
						ui.Text(c, locales.Skipped()).FontSize(12.5).TextColor(p.OnSurfaceVariant).Shrink(0)
					case t.State == "" && len(t.After) > 0:
						ui.Text(c, locales.WaitingFor(strings.Join(t.After, locales.ListSeparator()))).FontSize(12.5).TextColor(p.OnSurfaceVariant).SingleLine().Shrink(0)
					}
				})
				if t.State == "run" && len(t.Steps) > 0 {
					name, detail, _ := strings.Cut(t.Steps[len(t.Steps)-1], "\t")
					if v, ok := toolFor(name); ok {
						name = v.verb
					}
					ui.Row(c).Height(18).Gap(6).Padding(0, 0, 0, 19).AlignItems(ui.Center).Children(func() {
						ui.Text(c, name).FontSize(12.5).TextColor(p.OnSurfaceVariant).Shrink(0)
						ui.Text(c, detail).Font("monospace").FontSize(12.5).TextColor(p.OnSurface).SingleLine().Ellipsis("…").MinWidth(0)
					})
				}
			})
		}
	})
}

func mcpTool(name string) (server, tool string, ok bool) {
	rest, ok := strings.CutPrefix(name, "mcp__")
	if !ok {
		return "", "", false
	}
	return strings.Cut(rest, "__")
}

func (a *App) copyButton(c *ui.Context, text string, shown bool) {
	p := a.p
	h := fnv.New64a()
	h.Write([]byte(text))
	key := h.Sum64()
	done := c.Now().Sub(copied[key]) < 2*time.Second
	b := ui.ButtonBase(c.Key(key)).Size(28, 28).Radius(8).Center().Label(locales.Copy()).Tooltip(locales.Copy())
	m3Focus(b, p)
	target := float32(0)
	if shown || done || b.Focused() {
		target = 1
	}
	b.Opacity(b.AnimateTo("shown", target, ui.Animation{Duration: 120 * time.Millisecond}))
	m3State(b, p, ui.Transparent, false)
	b.Children(func() {
		ui.Box(c).Absolute().Fill().Center().
			Transition(ui.ElementTransition{Enter: &ui.Motion{Scale: .7, Opacity: 0}, Exit: &ui.Motion{Scale: .7, Opacity: 0}}).
			Key(done).Children(func() {
			if done {
				sym(c, "check", 14, p.Plus)
			} else {
				sym(c, "copy", 14, p.OnSurfaceVariant)
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
	b := ui.ButtonBase(c.Key(fmt.Sprintf("retry:%d", turn))).Size(28, 28).Radius(8).Center().
		Label(locales.Retry()).Tooltip(locales.Retry()).Disabled(a.C.Busy)
	m3Focus(b, p)
	m3State(b, p, ui.Transparent, false)
	b.Children(func() { sym(c, "refresh", 14, p.OnSurfaceVariant) })
	if b.Clicked() {
		a.retryTurn(turn)
	}
}
