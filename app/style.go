package app

import (
	"hash/fnv"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/shqingda/talos/locales"
)

// mdThemes are the Markdown looks of the two palettes, made once.
var mdThemes = map[*Palette]*markdown.Theme{}

// mdTheme is prose as the mockups set it: 13.5 on 1.55, code in ink with
// keywords bold rather than in colors, as Mono keeps color for what is
// alive.
func mdTheme(p *Palette, t *ui.Theme) *markdown.Theme {
	if th, ok := mdThemes[p]; ok {
		return th
	}
	code := editor.DefaultTheme(t)
	code.Tokens = [len(code.Tokens)]editor.TokenStyle{}
	set := func(k editor.TokenKind, s editor.TokenStyle) { code.Tokens[k] = s }
	set(editor.TokenKeyword, editor.TokenStyle{Color: p.Fg, Bold: true})
	set(editor.TokenString, editor.TokenStyle{Color: p.Fg2})
	set(editor.TokenComment, editor.TokenStyle{Color: p.Mute, Italic: true})
	set(editor.TokenNumber, editor.TokenStyle{Color: p.Fg2})
	set(editor.TokenConstant, editor.TokenStyle{Color: p.Fg2})
	set(editor.TokenPunctuation, editor.TokenStyle{Color: p.Fg2})
	set(editor.TokenOperator, editor.TokenStyle{Color: p.Fg2})
	set(editor.TokenHeading, editor.TokenStyle{Color: p.Fg, Bold: true})
	set(editor.TokenInserted, editor.TokenStyle{Color: p.Plus})
	set(editor.TokenDeleted, editor.TokenStyle{Color: p.Minus})
	set(editor.TokenInvalid, editor.TokenStyle{Color: p.Minus})
	th := &markdown.Theme{
		FontSize: 15, LineHeight: 1.6, CodeLineHeight: 1.6,
		Text: p.Fg, Muted: p.Mute, Link: p.Fg, Strong: p.Fg, LinkWeight: 500,
		Code: p.Fg, CodeBackground: p.Chip, CodeSize: 12.5 / 13.5,
		CodeBlockBackground: p.Chip, CodeBlockBorder: ui.Transparent, CodeBlockRadius: 10, CodeBlockPadding: 11 / 13.5,
		CodeColors: code, QuoteBar: p.Faint, QuoteText: p.Fg2, Rule: p.Edge,
		TableBorder: p.Edge, TableHeader: p.Chip, Checkbox: p.Fg,
	}
	mdThemes[p] = th
	return th
}

// copied holds when each code block, by its code, was last copied.
var copied = map[uint64]time.Time{}

// codeBlock is a fenced block as the mockups draw it: a 30-point header
// with the language and a copy icon, the code under it.
func (a *App) codeBlock(c *ui.Context, b *markdown.CodeBlockView) {
	p := a.p
	copyButton := func() ui.Element {
		h := fnv.New64a()
		h.Write([]byte(b.Code()))
		key := h.Sum64()
		done := c.Now().Sub(copied[key]) < 2*time.Second
		btn := ui.ButtonBase(c).Size(24, 24).Radius(6).Center().Label(locales.CopyCode()).Tooltip(locales.Copy()).Disabled(b.Open)
		if btn.Hovered() {
			btn.Background(p.Hover)
		}
		btn.Children(func() {
			if done {
				sym(c, "check", 12, p.Fg2)
			} else {
				sym(c, "copy", 12, p.Mute)
			}
		})
		if btn.Clicked() {
			c.WriteClipboard(b.Code())
			copied[key] = c.Now()
			c.After(2 * time.Second)
		}
		return btn
	}
	ui.Column(c).Radius(10).Background(p.Chip).Margin(10, 0, 12, 0).Clip().Children(func() {
		if b.Language == "" {
			// No language to name: the copy button sits over the code's
			// top right rather than on a header line of its own.
			b.Body(c)
			copyButton().Absolute().Top(6).Right(8)
			return
		}
		ui.Row(c).Height(30).Padding(0, 8, 0, 12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, b.Language).FontSize(12.5).TextColor(p.Mute)
			ui.Spacer(c)
			copyButton()
		})
		b.Body(c)
	})
}
