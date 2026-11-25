package app

import (
	"hash/fnv"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/shqingda/stratum/locales"
)

// mdThemes caches Markdown themes keyed by a compact palette signature.
var mdThemes = map[string]*markdown.Theme{}

func paletteSignature(p *Palette) string {
	return FormatHex(p.Surface) + "|" + FormatHex(p.Primary) + "|" + FormatHex(p.SurfaceContainerLow) + "|" + FormatHex(p.OnSurface)
}

// mdTheme builds an M3 tonal Markdown theme with semantic syntax highlighting
// harmonized with the active HCT palette.
func mdTheme(p *Palette, t *ui.Theme) *markdown.Theme {
	sig := paletteSignature(p)
	if th, ok := mdThemes[sig]; ok {
		return th
	}
	code := editor.DefaultTheme(t)
	code.Tokens = [len(code.Tokens)]editor.TokenStyle{}
	set := func(k editor.TokenKind, s editor.TokenStyle) { code.Tokens[k] = s }
	kwTone, strTone, fnTone := 42.0, 38.0, 44.0
	if p.Dark {
		kwTone, strTone, fnTone = 78.0, 76.0, 80.0
	}
	seedH := HctFromColor(p.Seed).Hue
	chroma := 46.0
	if p.Style == ColorStyleMonochrome {
		chroma = 0
	}
	kwColor := HctFrom(seedH, chroma, kwTone).Color()
	strColor := HctFrom(sanitizeDegrees(seedH+110), chroma*0.85, strTone).Color()
	numColor := HctFrom(sanitizeDegrees(seedH+210), chroma*0.85, fnTone).Color()
	set(editor.TokenKeyword, editor.TokenStyle{Color: kwColor, Bold: true})
	set(editor.TokenString, editor.TokenStyle{Color: strColor})
	set(editor.TokenComment, editor.TokenStyle{Color: p.OnSurfaceVariant, Italic: true})
	set(editor.TokenNumber, editor.TokenStyle{Color: numColor})
	set(editor.TokenConstant, editor.TokenStyle{Color: numColor})
	set(editor.TokenPunctuation, editor.TokenStyle{Color: p.OnSurfaceVariant})
	set(editor.TokenOperator, editor.TokenStyle{Color: p.Primary})
	set(editor.TokenHeading, editor.TokenStyle{Color: p.Primary, Bold: true})
	set(editor.TokenInserted, editor.TokenStyle{Color: p.Plus})
	set(editor.TokenDeleted, editor.TokenStyle{Color: p.Minus})
	set(editor.TokenInvalid, editor.TokenStyle{Color: p.Error})
	th := &markdown.Theme{
		FontSize: 14.5, LineHeight: 1.6, CodeLineHeight: 1.55,
		Text: p.OnSurface, Muted: p.OnSurfaceVariant, Link: p.Primary, Strong: p.OnSurface, LinkWeight: 600,
		Code: p.OnSurface, CodeBackground: p.SurfaceContainerHigh, CodeSize: 13.0 / 14.5,
		CodeBlockBackground: p.SurfaceContainerLow, CodeBlockBorder: p.OutlineVariant, CodeBlockRadius: 12, CodeBlockPadding: 12.0 / 14.5,
		CodeColors: code, QuoteBar: p.Primary, QuoteText: p.OnSurfaceVariant, Rule: p.OutlineVariant,
		TableBorder: p.OutlineVariant, TableHeader: p.SurfaceContainerHigh, Checkbox: p.Primary,
	}
	if len(mdThemes) > 32 {
		clear(mdThemes)
	}
	mdThemes[sig] = th
	return th
}

// copied holds when each code block, by its code, was last copied.
var copied = map[uint64]time.Time{}

// codeBlock renders a fenced code block inside an M3 SurfaceContainerLow container
// with an integrated header bar and accessible copy button.
func (a *App) codeBlock(c *ui.Context, b *markdown.CodeBlockView) {
	p := a.p
	copyButton := func() ui.Element {
		h := fnv.New64a()
		h.Write([]byte(b.Code()))
		key := h.Sum64()
		done := c.Now().Sub(copied[key]) < 2*time.Second
		btn := ui.ButtonBase(c).Size(28, 28).Radius(8).Center().Label(locales.CopyCode()).Tooltip(locales.Copy()).Disabled(b.Open)
		m3Focus(btn, p)
		m3State(btn, p, ui.Transparent, done)
		btn.Children(func() {
			ui.Box(c).Absolute().Fill().Center().
				Transition(ui.ElementTransition{Enter: &ui.Motion{Scale: .7, Opacity: 0}, Exit: &ui.Motion{Scale: .7, Opacity: 0}, Duration: 150 * time.Millisecond}).
				Key(done).Children(func() {
				if done {
					sym(c, "check", 14, p.Plus)
				} else {
					sym(c, "copy", 14, p.OnSurfaceVariant)
				}
			})
		})
		if btn.Clicked() {
			c.WriteClipboard(b.Code())
			copied[key] = c.Now()
			c.After(2 * time.Second)
		}
		return btn
	}
	ui.Column(c).Radius(16).Background(p.SurfaceContainerLow).Margin(10, 0, 12, 0).Clip().Children(func() {
		if b.Language == "" {
			b.Body(c)
			copyButton().Absolute().Top(6).Right(8)
			return
		}
		ui.Row(c).Height(34).Padding(0, 8, 0, 14).AlignItems(ui.Center).Background(p.SurfaceContainerHigh).Children(func() {
			ui.Text(c, b.Language).FontSize(12).FontWeight(500).TextColor(p.OnSurfaceVariant)
			ui.Spacer(c)
			copyButton()
		})
		b.Body(c)
	})
}
