package app

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// Material Design 3 motion curves and durations.
var (
	easeEmphasizedDecelerate = ui.CubicBezier(0.05, 0.7, 0.1, 1.0)
	easeEmphasizedAccelerate = ui.CubicBezier(0.3, 0.0, 0.8, 0.15)
	easeStandard             = ui.CubicBezier(0.2, 0.0, 0.0, 1.0)

	animStandard = ui.Animation{Duration: 220 * time.Millisecond, Ease: easeStandard}
	animFast     = ui.Animation{Duration: 150 * time.Millisecond, Ease: easeStandard}
	animColor    = ui.Animation{Duration: 200 * time.Millisecond, Ease: easeStandard, Oklab: true, NonMotion: true}
	animEnter    = ui.Animation{Duration: 260 * time.Millisecond, Ease: easeEmphasizedDecelerate}
)

// m3Focus applies an accessible high-contrast Material 3 keyboard focus ring
// (>= 3:1 contrast against surfaces) that appears only on keyboard navigation.
func m3Focus(e ui.Element, p *Palette) ui.Element {
	return e.FocusRing(true).FocusRingColor(p.Primary).FocusRingOffset(2)
}

// m3State applies Material 3 tonal state layers (hover 8%, press 12%, selected
// SecondaryContainer or tonal blend) cleanly without Apple hover-only chrome.
func m3State(e ui.Element, p *Palette, base ui.Color, selected bool) ui.Element {
	bg := base
	if selected {
		bg = p.SecondaryContainer
	}
	switch {
	case e.Pressed():
		bg = bg.MixOklab(p.OnSurface, .12)
	case e.Hovered():
		bg = bg.MixOklab(p.OnSurface, .08)
	case e.FocusVisible():
		bg = bg.MixOklab(p.OnSurface, .10)
	}
	if bg.A > 0 {
		e.Background(bg)
	}
	return e
}
