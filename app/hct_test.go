package app

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestHctRoundTrip(t *testing.T) {
	for _, seed := range MineralSeeds {
		c, ok := ParseSeedHex(seed.Hex)
		if !ok {
			t.Fatalf("failed to parse seed %s (%s)", seed.ID, seed.Hex)
		}
		h := HctFromColor(c)
		re := HctFrom(h.Hue, h.Chroma, h.Tone).Color()
		if absInt(int(c.R)-int(re.R)) > 2 || absInt(int(c.G)-int(re.G)) > 2 || absInt(int(c.B)-int(re.B)) > 2 {
			t.Fatalf("seed %s round-trip mismatch: got %v want %v (hct=%+v)", seed.ID, re, c, h)
		}
		if math.IsNaN(h.Hue) || math.IsNaN(h.Chroma) || math.IsNaN(h.Tone) {
			t.Fatalf("NaN in HCT for %s: %+v", seed.ID, h)
		}
	}
}

func TestSeededPaletteContrastMatrix(t *testing.T) {
	styles := []ColorStyle{ColorStyleSoft, ColorStyleVivid, ColorStyleTinted, ColorStyleMonochrome}
	for _, seed := range MineralSeeds {
		c, _ := ParseSeedHex(seed.Hex)
		for _, style := range styles {
			for _, tc := range []struct {
				name      string
				dark      bool
				pureBlack bool
			}{
				{"light", false, false},
				{"dark", true, false},
				{"pureBlack", true, true},
			} {
				p := SeededPalette(c, tc.dark, style, tc.pureBlack)
				if got := ContrastRatio(p.OnSurface, p.Surface); got < 4.5 {
					t.Errorf("%s/%s/%s OnSurface vs Surface contrast=%.2f < 4.5", seed.ID, style, tc.name, got)
				}
				if got := ContrastRatio(p.OnSurface, p.SurfaceContainerLow); got < 4.5 {
					t.Errorf("%s/%s/%s OnSurface vs SurfaceContainerLow contrast=%.2f < 4.5", seed.ID, style, tc.name, got)
				}
				if got := ContrastRatio(p.OnSurfaceVariant, p.Surface); got < 4.5 {
					t.Errorf("%s/%s/%s OnSurfaceVariant vs Surface contrast=%.2f < 4.5", seed.ID, style, tc.name, got)
				}
				if got := ContrastRatio(p.OnPrimary, p.Primary); got < 4.5 {
					t.Errorf("%s/%s/%s OnPrimary vs Primary contrast=%.2f < 4.5", seed.ID, style, tc.name, got)
				}
				if got := ContrastRatio(p.OnPrimaryContainer, p.PrimaryContainer); got < 4.5 {
					t.Errorf("%s/%s/%s OnPrimaryContainer vs PrimaryContainer contrast=%.2f < 4.5", seed.ID, style, tc.name, got)
				}
				if got := ContrastRatio(p.OnSecondaryContainer, p.SecondaryContainer); got < 4.5 {
					t.Errorf("%s/%s/%s OnSecondaryContainer vs SecondaryContainer contrast=%.2f < 4.5", seed.ID, style, tc.name, got)
				}
				if got := ContrastRatio(p.OnErrorContainer, p.ErrorContainer); got < 4.5 {
					t.Errorf("%s/%s/%s OnErrorContainer vs ErrorContainer contrast=%.2f < 4.5", seed.ID, style, tc.name, got)
				}
				if got := ContrastRatio(p.Outline, p.Surface); got < 3.0 {
					t.Errorf("%s/%s/%s Outline vs Surface contrast=%.2f < 3.0", seed.ID, style, tc.name, got)
				}
				if tc.pureBlack && p.Surface != ui.RGB(0, 0, 0) {
					t.Errorf("%s/%s pureBlack Surface = %v, want #000000", seed.ID, style, p.Surface)
				}
			}
		}
	}
}
