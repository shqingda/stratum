package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/locales"
	"github.com/shqingda/stratum/proto"
)

type automationsState struct {
	formOpen     bool
	editID       string
	name         string
	schedule     string
	prompt       string
	cwd          string
	deletingID   string
	deletingName string
}

func (a *App) openAutomations() {
	a.automationsOpen = true
	a.settingsOpen = false
	a.navPage = "automations"
	a.quiet(a.C.ReadAutomations())
}

func (a *App) automationsView(c *ui.Context) {
	p := a.p
	st := &a.automationsUI
	if st.cwd == "" {
		st.cwd = a.C.Cwd
		if a.C.Current != nil && a.C.Current.Cwd != "" {
			st.cwd = a.C.Current.Cwd
		}
	}
	if st.schedule == "" {
		st.schedule = "@daily"
	}

	ui.Column(c).Fill().Background(p.Surface).Clip().Children(func() {
		ui.Row(c).Height(barHeight).Shrink(0).Padding(0, 20).Gap(10).AlignItems(ui.Center).
			DragWindow().Children(func() {
			sym(c, "clock-play", 18, p.Primary)
			ui.Text(c, locales.Automations()).FontSize(17).FontWeight(600).LetterSpacing(-.2).TextColor(p.OnSurface).SingleLine()
			ui.Spacer(c)
			if a.button(c, "auto-new-top", locales.NewAutomation(), "plus", true).Clicked() {
				st.formOpen = !st.formOpen
				st.editID = ""
				st.name, st.prompt = "", ""
				st.schedule = "@daily"
			}
			done := ui.ButtonBase(c.Key("auto-close")).Row().Shrink(0).Size(32, 32).Margin(0, 0, 0, 4).Radius(16).Center().
				Label(locales.Close()).Tooltip(locales.Close()).KeyShortcuts(0, ui.KeyEscape)
			m3Focus(done, p)
			m3State(done, p, ui.Color{}, false)
			done.Children(func() { sym(c, "x", 16, p.OnSurfaceVariant) })
			if done.Clicked() {
				a.automationsOpen = false
				a.navPage = "sessions"
			}
		})
		ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).Width(680).MaxWidthPercent(100).AlignSelf(ui.Center).Padding(20, 28, 52, 28).Gap(14).Children(func() {
				ui.Text(c, locales.AutomationsLede()).FontSize(13.5).LineHeight(1.5).TextColor(p.OnSurfaceVariant).Margin(0, 0, 4, 0)
				if st.formOpen {
					a.automationForm(c)
				}
				list := a.C.Automations
				switch {
				case list == nil:
					ui.Text(c, locales.Reading()).FontSize(13).TextColor(p.OnSurfaceVariant)
				case len(list.Items) == 0:
					group(c, p, func() {
						emptyState(c, p, "clock-play", locales.Automations(), locales.AutomationsEmpty(), func() {
							if a.button(c, "auto-empty-add", locales.NewAutomation(), "plus", true).Clicked() {
								st.formOpen = true
								st.editID = ""
							}
						})
					})
				default:
					group(c, p, func() {
						for i, it := range list.Items {
							if i > 0 {
								ui.Box(c.Key(fmt.Sprint("auto-sep:", i))).Height(1).Shrink(0).Background(p.OutlineVariant)
							}
							a.automationRow(c, it)
						}
					})
				}
			})
		})
	})
	a.automationDeleteDialog(c)
}

func (a *App) automationForm(c *ui.Context) {
	p := a.p
	st := &a.automationsUI
	group(c, p, func() {
		ui.Column(c).Padding(16).Gap(10).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.TextInputBase(c.Key("auto-name"), &st.name).
					Label(locales.AutomationName()).Placeholder(locales.AutomationName()).PlaceholderColor(p.OnSurfaceVariant).
					FontSize(13).TextColor(p.OnSurface).SingleLine().Height(34).Padding(0, 12).Radius(10).Background(p.SurfaceContainerHighest).Grow(1).MinWidth(0)
				ui.TextInputBase(c.Key("auto-sched"), &st.schedule).
					Label(locales.AutomationSchedule()).Placeholder("@daily").PlaceholderColor(p.OnSurfaceVariant).
					Font("monospace").FontSize(12.5).TextColor(p.OnSurface).SingleLine().Height(34).Padding(0, 12).Radius(10).Background(p.SurfaceContainerHighest).Width(176)
			})
			ui.TextInputBase(c.Key("auto-cwd"), &st.cwd).
				Label(locales.ChooseFolder()).Placeholder(locales.ChooseFolder()).PlaceholderColor(p.OnSurfaceVariant).
				Font("monospace").FontSize(12).TextColor(p.OnSurfaceVariant).SingleLine().Height(32).Padding(0, 12).Radius(10).Background(p.SurfaceContainerHighest)
			ui.TextInputBase(c.Key("auto-prompt"), &st.prompt).
				Label(locales.AutomationPrompt()).Placeholder(locales.AutomationPrompt()).PlaceholderColor(p.OnSurfaceVariant).
				FontSize(13).TextColor(p.OnSurface).Height(72).Padding(10, 12).Radius(10).Background(p.SurfaceContainerHighest)
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				for _, preset := range []string{"@hourly", "@daily", "0 9 * * 1-5"} {
					if a.pushButton(c, "auto-pre:"+preset, preset).Clicked() {
						st.schedule = preset
					}
				}
				ui.Spacer(c)
				if a.button(c, "auto-cancel", locales.Cancel(), "", false).Clicked() {
					st.formOpen = false
				}
				canSave := strings.TrimSpace(st.name) != "" && strings.TrimSpace(st.prompt) != ""
				if a.button(c, "auto-save", locales.Save(), "", true).Disabled(!canSave).Clicked() {
					a.do(a.C.UpsertAutomation(proto.Automation{
						ID:       st.editID,
						Name:     st.name,
						Schedule: st.schedule,
						Prompt:   st.prompt,
						Cwd:      st.cwd,
						Mode:     "auto",
						Enabled:  true,
					}))
					st.formOpen = false
					st.editID, st.name, st.prompt = "", "", ""
				}
			})
		})
	})
}

func (a *App) automationRow(c *ui.Context, it proto.Automation) {
	p := a.p
	ui.Row(c.Key("auto:"+it.ID)).Padding(12, 16).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, it.Name).FontSize(13.5).FontWeight(600).TextColor(p.OnSurface).SingleLine().Ellipsis("…")
				ui.Text(c, it.Schedule).Font("monospace").FontSize(11.5).TextColor(p.OnSecondaryContainer).
					Padding(2, 7).Radius(6).Background(p.SecondaryContainer)
				if it.LastStatus == "ok" {
					ui.Text(c, locales.Succeeded()).FontSize(11).FontWeight(600).TextColor(p.Plus)
				} else if it.LastStatus == "error" {
					ui.Text(c, locales.Failed()).FontSize(11).FontWeight(600).TextColor(p.Minus)
				}
			})
			ui.Text(c, it.Prompt).FontSize(12.5).TextColor(p.OnSurfaceVariant).SingleLine().Ellipsis("…")
			if !it.NextRunAt.IsZero() && it.Enabled {
				ui.Text(c, fmt.Sprintf("%s · %s", home(it.Cwd), it.NextRunAt.Local().Format("01-02 15:04"))).FontSize(11.5).TextColor(p.OnSurfaceVariant).SingleLine()
			} else {
				ui.Text(c, home(it.Cwd)).FontSize(11.5).TextColor(p.OnSurfaceVariant).SingleLine()
			}
		})
		if it.LastSessionID != "" {
			if a.iconButton(c, "auto-open:"+it.ID, "arrow-right", locales.Open()).Clicked() {
				a.automationsOpen = false
				a.navPage = "sessions"
				a.do(a.C.Open(it.LastSessionID))
			}
		}
		if a.iconButton(c, "auto-run:"+it.ID, "clock-play", locales.RunNow()).Clicked() {
			a.do(a.C.RunAutomation(it.ID))
		}
		en := it.Enabled
		if ui.Switch(c.Key("auto-tog:"+it.ID), &en).Label(it.Name).Changed() {
			it.Enabled = en
			a.do(a.C.UpsertAutomation(it))
		}
		if a.iconButton(c, "auto-del:"+it.ID, "trash", locales.Delete()).Clicked() {
			a.automationsUI.deletingID = it.ID
			a.automationsUI.deletingName = it.Name
		}
	})
}

func (a *App) automationDeleteDialog(c *ui.Context) {
	st := &a.automationsUI
	open := st.deletingID != ""
	if !open {
		return
	}
	p := a.p
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(p.Scrim)
		panel.Width(400).Padding(24).Radius(28).Background(p.SurfaceContainerHigh).Label(locales.DeleteAutomationTitle(st.deletingName))
		elevation3(panel, p).Children(func() {
			ui.Text(c, locales.DeleteAutomationTitle(st.deletingName)).FontSize(20).FontWeight(500).TextColor(p.OnSurface)
			ui.Text(c, locales.DeleteAutomationBody()).FontSize(13.5).LineHeight(1.5).TextColor(p.OnSurfaceVariant).Margin(12, 0, 20, 0)
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				cancel := a.button(c, "auto-del-cancel", locales.Cancel(), "esc", false)
				cancel.AutoFocus()
				if cancel.Clicked() {
					st.deletingID, st.deletingName = "", ""
				}
				if a.dangerButton(c, "auto-del-ok", locales.Delete(), "").Clicked() {
					a.do(a.C.DeleteAutomation(st.deletingID))
					st.deletingID, st.deletingName = "", ""
				}
			})
		})
	})
	if !open {
		st.deletingID, st.deletingName = "", ""
	}
}
