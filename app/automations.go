package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/locales"
	"github.com/shqingda/talos/proto"
)

type automationsState struct {
	formOpen bool
	editID   string
	name     string
	schedule string
	prompt   string
	cwd      string
}

func (a *App) openAutomations() {
	a.automationsOpen = true
	a.settingsOpen = false
	a.quiet(a.C.ReadAutomations())
}

func (a *App) automationsView(c *ui.Context, vibrant bool) {
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

	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		side := ui.Column(c).Width(sidebarWidth).Shrink(0)
		if vibrant {
			tint := ui.RGBA(244, 245, 248, .38)
			if p == dark {
				tint = ui.RGBA(18, 18, 20, .48)
			}
			side.Background(tint).BorderWidth(0, .5, 0, 0).BorderColor(p.Edge)
		} else {
			side.Background(p.Side).BorderWidth(0, .5, 0, 0).BorderColor(p.Edge)
		}
		side.Children(func() {
			ui.Box(c).Height(barHeight).Shrink(0).DragWindow()
			ui.Row(c).Padding(4, 20, 14, 20).Gap(10).AlignItems(ui.Center).Children(func() {
				sym(c, "clock-play", 20, p.Fg)
				ui.Text(c, locales.Automations()).FontSize(16).FontWeight(600).TextColor(p.Fg)
			})
			ui.Column(c).Padding(0, 12, 12, 12).Gap(8).Children(func() {
				if a.button(c, "auto-new-side", locales.NewAutomation(), "plus", true).Clicked() {
					st.formOpen = !st.formOpen
					st.editID = ""
					st.name, st.prompt = "", ""
					st.schedule = "@daily"
				}
			})
			ui.Spacer(c)
			ui.Column(c).Padding(8, 12, 14, 12).Children(func() {
				if a.navItem(c, "auto-back", "arrow-back-up", locales.Close(), "").Clicked() {
					a.automationsOpen = false
				}
			})
		})

		ui.Column(c).Grow(1).Background(p.Sheet).Children(func() {
			ui.Row(c).Height(barHeight).Shrink(0).Padding(0, 24).AlignItems(ui.Center).DragWindow().Children(func() {
				ui.Text(c, locales.Automations()).FontSize(15).FontWeight(600).TextColor(p.Fg)
				ui.Spacer(c)
				if a.button(c, "auto-new-top", locales.NewAutomation(), "plus", true).Clicked() {
					st.formOpen = !st.formOpen
					st.editID = ""
				}
				if a.iconButton(c, "auto-close", "x", locales.Close()).Margin(0, 0, 0, 8).Clicked() {
					a.automationsOpen = false
				}
			})

			ui.Scroll(c).Grow(1).Padding(8, 28, 28, 28).Children(func() {
				ui.Column(c).MaxWidth(680).Gap(14).Children(func() {
					if st.formOpen {
						a.automationForm(c)
					}
					list := a.C.Automations
					switch {
					case list == nil:
						ui.Text(c, locales.Reading()).FontSize(13).TextColor(p.Mute)
					case len(list.Items) == 0:
						group(c, p, func() {
							ui.Column(c).Padding(20).Gap(10).Center().Children(func() {
								sym(c, "clock-play", 26, p.Mute)
								ui.Text(c, locales.AutomationsEmpty()).FontSize(13).TextColor(p.Fg2)
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
									ui.Box(c.Key(fmt.Sprint("auto-sep:", i))).Height(.5).Shrink(0).Background(p.Edge)
								}
								a.automationRow(c, it)
							}
						})
					}
				})
			})
		})
	})
}

func (a *App) automationForm(c *ui.Context) {
	p := a.p
	st := &a.automationsUI
	group(c, p, func() {
		ui.Column(c).Padding(14).Gap(10).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.TextInputBase(c.Key("auto-name"), &st.name).
					Label(locales.AutomationName()).Placeholder(locales.AutomationName()).PlaceholderColor(p.Mute).
					FontSize(13).TextColor(p.Fg).SingleLine().Height(32).Padding(0, 10).Radius(8).Background(p.Chip).Grow(1).MinWidth(0)
				ui.TextInputBase(c.Key("auto-sched"), &st.schedule).
					Label(locales.AutomationSchedule()).Placeholder("@daily").PlaceholderColor(p.Mute).
					Font("monospace").FontSize(12.5).TextColor(p.Fg).SingleLine().Height(32).Padding(0, 10).Radius(8).Background(p.Chip).Width(170)
			})
			ui.TextInputBase(c.Key("auto-cwd"), &st.cwd).
				Label(locales.ChooseFolder()).Placeholder(locales.ChooseFolder()).PlaceholderColor(p.Mute).
				Font("monospace").FontSize(12).TextColor(p.Fg2).SingleLine().Height(30).Padding(0, 10).Radius(8).Background(p.Chip)
			ui.TextInputBase(c.Key("auto-prompt"), &st.prompt).
				Label(locales.AutomationPrompt()).Placeholder(locales.AutomationPrompt()).PlaceholderColor(p.Mute).
				FontSize(13).TextColor(p.Fg).Height(68).Padding(8, 10).Radius(8).Background(p.Chip)
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
	ui.Row(c.Key("auto:"+it.ID)).Padding(12, 14).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, it.Name).FontSize(13.5).FontWeight(600).TextColor(p.Fg).SingleLine().Ellipsis("…")
				ui.Text(c, it.Schedule).Font("monospace").FontSize(11.5).TextColor(p.Fg2).
					Padding(2, 6).Radius(5).Background(p.Chip)
				if it.LastStatus == "ok" {
					ui.Text(c, "OK").FontSize(11).FontWeight(600).TextColor(p.Plus)
				} else if it.LastStatus == "error" {
					ui.Text(c, "Error").FontSize(11).FontWeight(600).TextColor(p.Minus)
				}
			})
			ui.Text(c, it.Prompt).FontSize(12.5).TextColor(p.Fg2).SingleLine().Ellipsis("…")
			if !it.NextRunAt.IsZero() && it.Enabled {
				ui.Text(c, fmt.Sprintf("%s · %s", home(it.Cwd), it.NextRunAt.Local().Format("01-02 15:04"))).FontSize(11.5).TextColor(p.Mute).SingleLine()
			} else {
				ui.Text(c, home(it.Cwd)).FontSize(11.5).TextColor(p.Mute).SingleLine()
			}
		})
		if it.LastSessionID != "" {
			if a.iconButton(c, "auto-open:"+it.ID, "arrow-right", locales.Open()).Clicked() {
				a.automationsOpen = false
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
			a.do(a.C.DeleteAutomation(it.ID))
		}
	})
}
