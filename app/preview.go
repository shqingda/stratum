package app

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/locales"
)

type previewState struct {
	url       string
	lastBytes int
	img       ui.ImageSource
	polled    bool
}

func (a *App) previewPane(c *ui.Context) {
	p := a.p
	st := &a.previewUI
	if !st.polled {
		st.polled = true
		a.do(a.C.ReadPreview())
	}
	if st.url == "" {
		if a.C.Preview != nil && a.C.Preview.URL != "" {
			st.url = a.C.Preview.URL
		} else {
			st.url = locales.PreviewUrlPlaceholder()
		}
	}

	// Address bar + actions
	ui.Row(c).Height(44).Shrink(0).Padding(0, 12).Gap(8).AlignItems(ui.Center).BorderWidth(0, 0, .5, 0).BorderColor(p.Edge).Children(func() {
		inp := ui.TextInputBase(c.Key("preview-url"), &st.url).
			Placeholder(locales.PreviewUrlPlaceholder()).PlaceholderColor(p.Mute).
			Font("monospace").FontSize(12.5).TextColor(p.Fg).SingleLine().
			Height(30).Padding(0, 10).Radius(8).Background(p.Chip).Grow(1).MinWidth(0)
		if inp.Submitted() {
			a.do(a.C.OpenPreview(st.url))
		}
		goBtn := a.iconButton(c, "preview-go", "refresh", locales.Preview())
		if goBtn.Clicked() {
			a.do(a.C.OpenPreview(st.url))
		}
		extBtn := a.iconButton(c, "preview-ext", "world", locales.OpenInBrowser())
		if extBtn.Clicked() && a.openURL != nil {
			_ = a.openURL(st.url)
		}
	})

	snap := a.C.Preview
	if snap == nil || snap.URL == "" {
		ui.Column(c).Grow(1).Padding(24).Gap(10).Center().Children(func() {
			sym(c, "world", 28, p.Mute)
			ui.Text(c, locales.PreviewEmpty()).FontSize(13).TextColor(p.Fg2)
			if a.button(c, "preview-open-default", locales.Preview(), "", true).Clicked() {
				a.do(a.C.OpenPreview(st.url))
			}
		})
		return
	}

	if len(snap.Screenshot) > 0 && len(snap.Screenshot) != st.lastBytes {
		if src, err := ui.DecodeImage(bytes.Clone(snap.Screenshot)); err == nil {
			st.img = src
			st.lastBytes = len(snap.Screenshot)
		}
	}

	ui.Scroll(c).Grow(1).Padding(12).Children(func() {
		ui.Column(c).Gap(12).Children(func() {
			// Status header
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				title := snap.Title
				if title == "" {
					title = snap.URL
				}
				ui.Text(c, title).FontSize(13.5).FontWeight(600).TextColor(p.Fg).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
				if snap.Status > 0 {
					col := p.Plus
					if snap.Status >= 400 {
						col = p.Minus
					}
					ui.Text(c, fmt.Sprintf("HTTP %d", snap.Status)).Font("monospace").FontSize(11.5).TextColor(col)
				}
			})
			if snap.Error != "" {
				ui.Text(c, snap.Error).FontSize(12).TextColor(p.Minus).Selectable()
			}
			if st.img != nil {
				w, h := ui.ImageSize(st.img)
				dispW := float32(reviewWidth - 28)
				dispH := dispW * 0.62
				if w > 0 && h > 0 {
					dispH = dispW * float32(h) / float32(w)
				}
				ui.Image(c.Key("preview-shot"), st.img).Size(dispW, dispH).Fit(ui.Contain).Radius(10).Border(.5, p.Edge)
			}

			// Console & Network section
			ui.Text(c, locales.PreviewConsole()).FontSize(12.5).FontWeight(600).TextColor(p.Fg).Margin(4, 0, 0, 0)
			if len(snap.Console) == 0 && len(snap.Network) == 0 {
				ui.Text(c, locales.PreviewNoErrors()).FontSize(12.5).TextColor(p.Mute)
			} else {
				group(c, p, func() {
					ui.Column(c).Padding(10).Gap(6).Children(func() {
						for i, ce := range snap.Console {
							col := p.Live
							if ce.Level == "error" {
								col = p.Minus
							}
							line := fmt.Sprintf("[%s] %s", strings.ToUpper(ce.Level), ce.Text)
							if ce.Source != "" {
								line += fmt.Sprintf(" (%s:%d)", ce.Source, ce.Line)
							}
							ui.Text(c.Key(fmt.Sprint("pc:", i)), line).Font("monospace").FontSize(12).TextColor(col).Selectable()
						}
						for i, ne := range snap.Network {
							line := fmt.Sprintf("[HTTP %d] %s %s", ne.Status, ne.URL, ne.Error)
							ui.Text(c.Key(fmt.Sprint("pn:", i)), line).Font("monospace").FontSize(12).TextColor(p.Minus).Selectable()
						}
					})
				})
			}
		})
	})
}
