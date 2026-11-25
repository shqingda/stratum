package app

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// Palette holds the full Material You (Material Design 3) tonal role tokens
// generated from a single seed color via the Go HCT engine.
type Palette struct {
	Dark      bool
	Style     ColorStyle
	Seed      ui.Color
	PureBlack bool

	// Tonal surface hierarchy
	Surface                 ui.Color
	SurfaceDim              ui.Color
	SurfaceBright           ui.Color
	SurfaceContainerLowest  ui.Color
	SurfaceContainerLow     ui.Color
	SurfaceContainer        ui.Color
	SurfaceContainerHigh    ui.Color
	SurfaceContainerHighest ui.Color

	// Accent & semantic roles
	Primary              ui.Color
	OnPrimary            ui.Color
	PrimaryContainer     ui.Color
	OnPrimaryContainer   ui.Color
	Secondary            ui.Color
	OnSecondary          ui.Color
	SecondaryContainer   ui.Color
	OnSecondaryContainer ui.Color
	Tertiary             ui.Color
	OnTertiary           ui.Color
	TertiaryContainer    ui.Color
	OnTertiaryContainer  ui.Color
	Error                ui.Color
	OnError              ui.Color
	ErrorContainer       ui.Color
	OnErrorContainer     ui.Color

	// Content & structural boundaries
	OnSurface        ui.Color
	OnSurfaceVariant ui.Color
	Outline          ui.Color
	OutlineVariant   ui.Color
	InverseSurface   ui.Color
	InverseOnSurface ui.Color
	InversePrimary   ui.Color
	Scrim            ui.Color

	// Interactive state overlays & semantic status/diff roles
	Hover            ui.Color
	Sel              ui.Color
	Plus             ui.Color
	PlusContainer    ui.Color
	Minus            ui.Color
	MinusContainer   ui.Color
	Warning          ui.Color
	WarningContainer ui.Color
}

var (
	defaultSeed, _ = ParseSeedHex(DefaultSeedHex)
	light          = SeededPalette(defaultSeed, false, ColorStyleSoft, false)
	dark           = SeededPalette(defaultSeed, true, ColorStyleSoft, false)
)

// PaletteFor returns the default Material You palette for an appearance.
func PaletteFor(isDark bool) *Palette {
	if isDark {
		return dark
	}
	return light
}

// PaletteLerp smoothly interpolates between light and dark default palettes in Oklab space.
func PaletteLerp(t float32) *Palette {
	return PaletteLerpBetween(light, dark, t)
}

// PaletteLerpBetween interpolates between a (0) and b (1) using Oklab.
func PaletteLerpBetween(a, b *Palette, t float32) *Palette {
	if t <= 0.001 {
		return a
	}
	if t >= 0.999 {
		return b
	}
	mix := func(x, y ui.Color) ui.Color { return x.MixOklab(y, t) }
	darkMode := t >= 0.5
	style := a.Style
	pureBlack := a.PureBlack
	if darkMode {
		style = b.Style
		pureBlack = b.PureBlack
	}
	return &Palette{
		Dark:                    darkMode,
		Style:                   style,
		Seed:                    mix(a.Seed, b.Seed),
		PureBlack:               pureBlack,
		Surface:                 mix(a.Surface, b.Surface),
		SurfaceDim:              mix(a.SurfaceDim, b.SurfaceDim),
		SurfaceBright:           mix(a.SurfaceBright, b.SurfaceBright),
		SurfaceContainerLowest:  mix(a.SurfaceContainerLowest, b.SurfaceContainerLowest),
		SurfaceContainerLow:     mix(a.SurfaceContainerLow, b.SurfaceContainerLow),
		SurfaceContainer:        mix(a.SurfaceContainer, b.SurfaceContainer),
		SurfaceContainerHigh:    mix(a.SurfaceContainerHigh, b.SurfaceContainerHigh),
		SurfaceContainerHighest: mix(a.SurfaceContainerHighest, b.SurfaceContainerHighest),
		Primary:                 mix(a.Primary, b.Primary),
		OnPrimary:               mix(a.OnPrimary, b.OnPrimary),
		PrimaryContainer:        mix(a.PrimaryContainer, b.PrimaryContainer),
		OnPrimaryContainer:      mix(a.OnPrimaryContainer, b.OnPrimaryContainer),
		Secondary:               mix(a.Secondary, b.Secondary),
		OnSecondary:             mix(a.OnSecondary, b.OnSecondary),
		SecondaryContainer:      mix(a.SecondaryContainer, b.SecondaryContainer),
		OnSecondaryContainer:    mix(a.OnSecondaryContainer, b.OnSecondaryContainer),
		Tertiary:                mix(a.Tertiary, b.Tertiary),
		OnTertiary:              mix(a.OnTertiary, b.OnTertiary),
		TertiaryContainer:       mix(a.TertiaryContainer, b.TertiaryContainer),
		OnTertiaryContainer:     mix(a.OnTertiaryContainer, b.OnTertiaryContainer),
		Error:                   mix(a.Error, b.Error),
		OnError:                 mix(a.OnError, b.OnError),
		ErrorContainer:          mix(a.ErrorContainer, b.ErrorContainer),
		OnErrorContainer:        mix(a.OnErrorContainer, b.OnErrorContainer),
		OnSurface:               mix(a.OnSurface, b.OnSurface),
		OnSurfaceVariant:        mix(a.OnSurfaceVariant, b.OnSurfaceVariant),
		Outline:                 mix(a.Outline, b.Outline),
		OutlineVariant:          mix(a.OutlineVariant, b.OutlineVariant),
		InverseSurface:          mix(a.InverseSurface, b.InverseSurface),
		InverseOnSurface:        mix(a.InverseOnSurface, b.InverseOnSurface),
		InversePrimary:          mix(a.InversePrimary, b.InversePrimary),
		Scrim:                   mix(a.Scrim, b.Scrim),
		Hover:                   mix(a.Hover, b.Hover),
		Sel:                     mix(a.Sel, b.Sel),
		Plus:                    mix(a.Plus, b.Plus),
		PlusContainer:           mix(a.PlusContainer, b.PlusContainer),
		Minus:                   mix(a.Minus, b.Minus),
		MinusContainer:          mix(a.MinusContainer, b.MinusContainer),
		Warning:                 mix(a.Warning, b.Warning),
		WarningContainer:        mix(a.WarningContainer, b.WarningContainer),
	}
}

// Theme maps an M3 Palette onto mygo's stock widgets.
func Theme(p *Palette, isDark bool) *ui.Theme {
	t := ui.LightTheme()
	if isDark {
		t = ui.DarkTheme()
	}
	t.Background = p.SurfaceContainer
	t.Surface = p.Surface
	t.SurfaceHover = p.Surface.MixOklab(p.OnSurface, .06)
	t.SurfacePressed = p.Surface.MixOklab(p.OnSurface, .10)
	t.Border = p.OutlineVariant
	t.Text, t.TextMuted = p.OnSurface, p.OnSurfaceVariant
	t.Accent, t.AccentText = p.Primary, p.OnPrimary
	t.AccentHover = p.Primary.MixOklab(p.OnPrimary, .10)
	t.AccentPressed = p.Primary.MixOklab(p.OnPrimary, .18)
	t.Danger, t.Success = p.Error, p.Plus
	t.Selection = p.Primary.Alpha(.22)
	t.Focus = p.Primary.Alpha(.65)
	t.Inverse, t.InverseText = p.InverseSurface, p.InverseOnSurface
	t.Radius = 12
	t.FontSize = 14
	return t
}

// elevation2 gives transient overlays (menus, popovers) a single soft M3 lift
// without wireframe borders.
func elevation2(e ui.Element, p *Palette) ui.Element {
	if p.Dark {
		return e.Shadow(0, 6, 20, -2, ui.RGBA(0, 0, 0, .42))
	}
	return e.Shadow(0, 4, 16, -2, ui.RGBA(0, 0, 0, .08))
}

// elevation3 gives top-layer modal dialogs and SearchView clean tonal lift
// without borders.
func elevation3(e ui.Element, p *Palette) ui.Element {
	if p.Dark {
		return e.Shadow(0, 14, 36, -4, ui.RGBA(0, 0, 0, .55))
	}
	return e.Shadow(0, 12, 32, -4, ui.RGBA(0, 0, 0, .14))
}

// disclosureChevron smoothly rotates 0°↔90° with Material 3 standard easing.
func disclosureChevron(c *ui.Context, key string, open bool, size float32, col ui.Color) {
	target := float32(0)
	if open {
		target = 90
	}
	wrap := ui.Box(c.Key("chev:"+key)).Size(size+2, size+2).Center().Shrink(0)
	deg := wrap.AnimateTo("deg", target, ui.Animation{Duration: 200 * time.Millisecond, Ease: easeStandard})
	wrap.Rotate(deg).Children(func() {
		sym(c, "chevron-right", size, col)
	})
}

// emptyState draws a calm M3 tonal placeholder across panes and destination stages.
func emptyState(c *ui.Context, p *Palette, icon, title, subtitle string, action func()) {
	ui.Column(c).Grow(1).Center().Padding(28, 24).Gap(10).Children(func() {
		if icon != "" {
			ui.Box(c).Size(44, 44).Radius(14).Background(p.SurfaceContainerHigh).Center().Children(func() {
				sym(c, icon, 22, p.OnSurfaceVariant)
			})
		}
		if title != "" {
			ui.Text(c, title).FontSize(14.5).FontWeight(600).TextColor(p.OnSurface).TextAlign(ui.Center).MaxWidth(300)
		}
		if subtitle != "" {
			ui.Text(c, subtitle).FontSize(13).LineHeight(1.5).TextColor(p.OnSurfaceVariant).TextAlign(ui.Center).MaxWidth(300)
		}
		if action != nil {
			ui.Box(c).Margin(6, 0, 0, 0).Children(action)
		}
	})
}

// projectBadgeColor derives a harmonized HCT accent color for a project folder
// harmonized with the active seed color hue.
func projectBadgeColor(name string, p *Palette) ui.Color {
	seedH := HctFromColor(p.Seed).Hue
	var h uint32
	for i := 0; i < len(name); i++ {
		h = h*31 + uint32(name[i])
	}
	hue := sanitizeDegrees(seedH + float64(h%6)*48.0)
	chroma := 42.0
	if p.Style == ColorStyleMonochrome {
		chroma = 0
	}
	tone := 42.0
	if p.Dark {
		tone = 74.0
	}
	return HctFrom(hue, chroma, tone).Color()
}

// projectSwatch draws a tonal rounded square project badge (replaces Apple halo dot).
func projectSwatch(c *ui.Context, p *Palette, name string) {
	col := projectBadgeColor(name, p)
	ui.Box(c).Size(10, 10).Radius(3).Background(col).Shrink(0)
}
