package app

import (
	"math"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
)

// ColorStyle mirrors FlClash's four single-seed HCT derivation strategies.
type ColorStyle string

const (
	ColorStyleSoft       ColorStyle = "soft"
	ColorStyleVivid      ColorStyle = "vivid"
	ColorStyleTinted     ColorStyle = "tinted"
	ColorStyleMonochrome ColorStyle = "monochrome"
)

// NormalizeColorStyle canonicalizes persisted color style names.
func NormalizeColorStyle(s string) ColorStyle {
	switch ColorStyle(strings.ToLower(strings.TrimSpace(s))) {
	case ColorStyleVivid:
		return ColorStyleVivid
	case ColorStyleTinted:
		return ColorStyleTinted
	case ColorStyleMonochrome:
		return ColorStyleMonochrome
	default:
		return ColorStyleSoft
	}
}

// MineralSeed defines a curated geological seed preset for Stratum.
type MineralSeed struct {
	ID  string
	Hex string
}

// DefaultSeedHex is Cobalt Basalt, Stratum's primary mineral seed color.
const DefaultSeedHex = "#3B6698"

// MineralSeeds are the built-in geological seed presets.
var MineralSeeds = []MineralSeed{
	{ID: "basalt", Hex: "#3B6698"},
	{ID: "malachite", Hex: "#2E7D63"},
	{ID: "ochre", Hex: "#9E6B2E"},
	{ID: "garnet", Hex: "#9A4453"},
	{ID: "feldspar", Hex: "#65588F"},
	{ID: "obsidian", Hex: "#5C5F66"},
}

// ParseSeedHex parses a "#RRGGBB" or "RRGGBB" hex string into a ui.Color.
func ParseSeedHex(s string) (ui.Color, bool) {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "#"))
	if len(s) != 6 {
		return ui.Color{}, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return ui.Color{}, false
	}
	return ui.RGB(uint8((v>>16)&0xFF), uint8((v>>8)&0xFF), uint8(v&0xFF)), true
}

// FormatHex formats a ui.Color as uppercase "#RRGGBB".
func FormatHex(c ui.Color) string {
	const hexDigits = "0123456789ABCDEF"
	b := [7]byte{
		'#',
		hexDigits[c.R>>4], hexDigits[c.R&0xF],
		hexDigits[c.G>>4], hexDigits[c.G&0xF],
		hexDigits[c.B>>4], hexDigits[c.B&0xF],
	}
	return string(b[:])
}

// Hct represents a color in Google Material Color Utilities' Hue-Chroma-Tone space.
type Hct struct {
	argb   uint32
	Hue    float64
	Chroma float64
	Tone   float64
}

// TonalPalette holds a fixed CAM16 hue and chroma across perceived L* tones [0..100].
type TonalPalette struct {
	Hue    float64
	Chroma float64
}

func (tp TonalPalette) Tone(t float64) ui.Color {
	return colorFromArgb(HctFrom(tp.Hue, tp.Chroma, t).argb)
}

func (tp TonalPalette) Hct(t float64) Hct {
	return HctFrom(tp.Hue, tp.Chroma, t)
}

// HctFromInt converts an ARGB uint32 to HCT.
func HctFromInt(argb uint32) Hct {
	hue, chroma := cam16HueAndChroma(argb)
	return Hct{
		argb:   argb,
		Hue:    hue,
		Chroma: chroma,
		Tone:   lstarFromArgb(argb),
	}
}

// HctFromColor converts a ui.Color to HCT.
func HctFromColor(c ui.Color) Hct {
	return HctFromInt(argbFromColor(c))
}

// HctFrom solves for the closest in-gamut sRGB color matching (hue, chroma, tone).
func HctFrom(hue, chroma, tone float64) Hct {
	return HctFromInt(solveHctToInt(hue, chroma, tone))
}

func (h Hct) Color() ui.Color {
	return colorFromArgb(h.argb)
}

func argbFromColor(c ui.Color) uint32 {
	return uint32(255)<<24 | uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
}

func colorFromArgb(argb uint32) ui.Color {
	return ui.RGB(uint8((argb>>16)&0xFF), uint8((argb>>8)&0xFF), uint8(argb&0xFF))
}

func argbFromRgb(r, g, b int) uint32 {
	return uint32(255)<<24 | uint32(r&255)<<16 | uint32(g&255)<<8 | uint32(b&255)
}

// ContrastRatio computes the WCAG 2.x relative luminance contrast ratio [1..21].
func ContrastRatio(a, b ui.Color) float64 {
	y1 := yFromArgb(argbFromColor(a))
	y2 := yFromArgb(argbFromColor(b))
	if y1 < y2 {
		y1, y2 = y2, y1
	}
	return (y1 + 5.0) / (y2 + 5.0)
}

// --- Color Math & Viewing Conditions (Material Color Utilities) ---

var (
	srgbToXyz = [3][3]float64{
		{0.41233895, 0.35762064, 0.18051042},
		{0.2126, 0.7152, 0.0722},
		{0.01932141, 0.11916382, 0.95034478},
	}
	whitePointD65 = [3]float64{95.047, 100.0, 108.883}
)

func signum(x float64) float64 {
	if x < 0 {
		return -1
	}
	if x > 0 {
		return 1
	}
	return 0
}

func clampDouble(lo, hi, v float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampInt(lo, hi, v int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func lerpDouble(a, b, t float64) float64 {
	return (1-t)*a + t*b
}

func sanitizeDegrees(d float64) float64 {
	d = math.Mod(d, 360.0)
	if d < 0 {
		d += 360.0
	}
	return d
}

func linearized(rgbComp uint8) float64 {
	norm := float64(rgbComp) / 255.0
	if norm <= 0.040449936 {
		return norm / 12.92 * 100.0
	}
	return math.Pow((norm+0.055)/1.055, 2.4) * 100.0
}

func delinearized(rgbComp float64) int {
	norm := rgbComp / 100.0
	var out float64
	if norm <= 0.0031308 {
		out = norm * 12.92
	} else {
		out = 1.055*math.Pow(norm, 1.0/2.4) - 0.055
	}
	return clampInt(0, 255, int(math.Round(out*255.0)))
}

func argbFromLinrgb(lin [3]float64) uint32 {
	return argbFromRgb(delinearized(lin[0]), delinearized(lin[1]), delinearized(lin[2]))
}

func labF(t float64) float64 {
	const e = 216.0 / 24389.0
	const kappa = 24389.0 / 27.0
	if t > e {
		return math.Cbrt(t)
	}
	return (kappa*t + 16.0) / 116.0
}

func labInvF(ft float64) float64 {
	const e = 216.0 / 24389.0
	const kappa = 24389.0 / 27.0
	ft3 := ft * ft * ft
	if ft3 > e {
		return ft3
	}
	return (116.0*ft - 16.0) / kappa
}

func yFromLstar(lstar float64) float64 {
	return 100.0 * labInvF((lstar+16.0)/116.0)
}

func lstarFromY(y float64) float64 {
	return labF(y/100.0)*116.0 - 16.0
}

func xyzFromArgb(argb uint32) [3]float64 {
	r := linearized(uint8((argb >> 16) & 0xFF))
	g := linearized(uint8((argb >> 8) & 0xFF))
	b := linearized(uint8(argb & 0xFF))
	return mulMat3(r, g, b, srgbToXyz)
}

func yFromArgb(argb uint32) float64 {
	r := linearized(uint8((argb >> 16) & 0xFF))
	g := linearized(uint8((argb >> 8) & 0xFF))
	b := linearized(uint8(argb & 0xFF))
	return srgbToXyz[1][0]*r + srgbToXyz[1][1]*g + srgbToXyz[1][2]*b
}

func lstarFromArgb(argb uint32) float64 {
	return lstarFromY(yFromArgb(argb))
}

func argbFromLstar(lstar float64) uint32 {
	c := delinearized(yFromLstar(lstar))
	return argbFromRgb(c, c, c)
}

func labFromArgb(argb uint32) (l, a, b float64) {
	xyz := xyzFromArgb(argb)
	fx := labF(xyz[0] / whitePointD65[0])
	fy := labF(xyz[1] / whitePointD65[1])
	fz := labF(xyz[2] / whitePointD65[2])
	return 116.0*fy - 16.0, 500.0 * (fx - fy), 200.0 * (fy - fz)
}

func mulMat3(a, b, c float64, m [3][3]float64) [3]float64 {
	return [3]float64{
		m[0][0]*a + m[0][1]*b + m[0][2]*c,
		m[1][0]*a + m[1][1]*b + m[1][2]*c,
		m[2][0]*a + m[2][1]*b + m[2][2]*c,
	}
}

type viewingConditions struct {
	n, aw, nbb, ncb, c, nc, fl float64
	rgbD                       [3]float64
	z                          float64
}

var defaultViewingConditions = func() viewingConditions {
	adaptingLuminance := 200.0 / math.Pi * yFromLstar(50.0) / 100.0
	const backgroundLstar = 50.0
	const surround = 2.0
	xyz := whitePointD65
	rW := xyz[0]*0.401288 + xyz[1]*0.650173 + xyz[2]*-0.051461
	gW := xyz[0]*-0.250268 + xyz[1]*1.204414 + xyz[2]*0.045854
	bW := xyz[0]*-0.002079 + xyz[1]*0.048952 + xyz[2]*0.953127
	f := 0.8 + (surround / 10.0)
	c := lerpDouble(0.59, 0.69, (f-0.9)*10.0)
	d := clampDouble(0.0, 1.0, f*(1.0-(1.0/3.6)*math.Exp((-adaptingLuminance-42.0)/92.0)))
	nc := f
	rgbD := [3]float64{
		d*(100.0/rW) + 1.0 - d,
		d*(100.0/gW) + 1.0 - d,
		d*(100.0/bW) + 1.0 - d,
	}
	k := 1.0 / (5.0*adaptingLuminance + 1.0)
	k4 := k * k * k * k
	k4F := 1.0 - k4
	fl := (k4 * adaptingLuminance) + (0.1 * k4F * k4F * math.Cbrt(5.0*adaptingLuminance))
	n := yFromLstar(backgroundLstar) / whitePointD65[1]
	z := 1.48 + math.Sqrt(n)
	nbb := 0.725 / math.Pow(n, 0.2)
	ncb := nbb
	rAF := math.Pow(fl*rgbD[0]*rW/100.0, 0.42)
	gAF := math.Pow(fl*rgbD[1]*gW/100.0, 0.42)
	bAF := math.Pow(fl*rgbD[2]*bW/100.0, 0.42)
	rA := (400.0 * rAF) / (rAF + 27.13)
	gA := (400.0 * gAF) / (gAF + 27.13)
	bA := (400.0 * bAF) / (bAF + 27.13)
	aw := (40.0*rA + 20.0*gA + bA) / 20.0 * nbb
	return viewingConditions{
		n: n, aw: aw, nbb: nbb, ncb: ncb, c: c, nc: nc, fl: fl, rgbD: rgbD, z: z,
	}
}()

func cam16HueAndChroma(argb uint32) (hue, chroma float64) {
	vc := defaultViewingConditions
	xyz := xyzFromArgb(argb)
	x, y, z := xyz[0], xyz[1], xyz[2]

	rC := 0.401288*x + 0.650173*y - 0.051461*z
	gC := -0.250268*x + 1.204414*y + 0.045854*z
	bC := -0.002079*x + 0.048952*y + 0.953127*z

	rD := vc.rgbD[0] * rC
	gD := vc.rgbD[1] * gC
	bD := vc.rgbD[2] * bC

	rAF := math.Pow(vc.fl*math.Abs(rD)/100.0, 0.42)
	gAF := math.Pow(vc.fl*math.Abs(gD)/100.0, 0.42)
	bAF := math.Pow(vc.fl*math.Abs(bD)/100.0, 0.42)

	rA := signum(rD) * 400.0 * rAF / (rAF + 27.13)
	gA := signum(gD) * 400.0 * gAF / (gAF + 27.13)
	bA := signum(bD) * 400.0 * bAF / (bAF + 27.13)

	a := (11.0*rA - 12.0*gA + bA) / 11.0
	b := (rA + gA - 2.0*bA) / 9.0
	u := (20.0*rA + 20.0*gA + 21.0*bA) / 20.0
	p2 := (40.0*rA + 20.0*gA + bA) / 20.0

	hue = sanitizeDegrees(math.Atan2(b, a) * 180.0 / math.Pi)
	ac := p2 * vc.nbb
	j := 100.0 * math.Pow(ac/vc.aw, vc.c*vc.z)

	huePrime := hue
	if huePrime < 20.14 {
		huePrime += 360.0
	}
	eHue := 0.25 * (math.Cos(huePrime*math.Pi/180.0+2.0) + 3.8)
	p1 := 50000.0 / 13.0 * eHue * vc.nc * vc.ncb
	t := p1 * math.Hypot(a, b) / (u + 0.305)
	alpha := math.Pow(t, 0.9) * math.Pow(1.64-math.Pow(0.29, vc.n), 0.73)
	chroma = alpha * math.Sqrt(j/100.0)
	return hue, chroma
}

// --- HctSolver ---

var (
	scaledDiscountFromLinrgb = [3][3]float64{
		{0.001200833568784504, 0.002389694492170889, 0.0002795742885861124},
		{0.0005891086651375999, 0.0029785502573438758, 0.0003270666104008398},
		{0.00010146692491640572, 0.0005364214359186694, 0.0032979401770712076},
	}
	linrgbFromScaledDiscount = [3][3]float64{
		{1373.2198709594231, -1100.4251190754821, -7.278681089101213},
		{-271.815969077903, 559.6580465940733, -32.46047482791194},
		{1.9622899599665666, -57.173814538844006, 308.7233197812385},
	}
	yFromLinrgb = [3]float64{0.2126, 0.7152, 0.0722}

	criticalPlanes = func() [255]float64 {
		var planes [255]float64
		for i := range planes {
			planes[i] = linearized(uint8(i)) + (linearized(uint8(i+1))-linearized(uint8(i)))*0.5
		}
		// Match exact 8-bit boundary luminance transitions from MCU criticalPlanes.
		for i := range planes {
			norm := (float64(i) + 0.5) / 255.0
			if norm <= 0.040449936 {
				planes[i] = norm / 12.92 * 100.0
			} else {
				planes[i] = math.Pow((norm+0.055)/1.055, 2.4) * 100.0
			}
		}
		return planes
	}()
)

func sanitizeRadians(angle float64) float64 {
	return math.Mod(angle+math.Pi*8, math.Pi*2)
}

func trueDelinearized(rgbComp float64) float64 {
	norm := rgbComp / 100.0
	var out float64
	if norm <= 0.0031308 {
		out = norm * 12.92
	} else {
		out = 1.055*math.Pow(norm, 1.0/2.4) - 0.055
	}
	return out * 255.0
}

func chromaticAdaptation(comp float64) float64 {
	af := math.Pow(math.Abs(comp), 0.42)
	return signum(comp) * 400.0 * af / (af + 27.13)
}

func inverseChromaticAdaptation(adapted float64) float64 {
	absA := math.Abs(adapted)
	base := math.Max(0, 27.13*absA/(400.0-absA))
	return signum(adapted) * math.Pow(base, 1.0/0.42)
}

func hueOfLinrgb(lin [3]float64) float64 {
	sd := mulMat3(lin[0], lin[1], lin[2], scaledDiscountFromLinrgb)
	rA := chromaticAdaptation(sd[0])
	gA := chromaticAdaptation(sd[1])
	bA := chromaticAdaptation(sd[2])
	a := (11.0*rA - 12.0*gA + bA) / 11.0
	b := (rA + gA - 2.0*bA) / 9.0
	return math.Atan2(b, a)
}

func areInCyclicOrder(a, b, c float64) bool {
	return sanitizeRadians(b-a) < sanitizeRadians(c-a)
}

func nthVertex(y float64, n int) [3]float64 {
	kR, kG, kB := yFromLinrgb[0], yFromLinrgb[1], yFromLinrgb[2]
	coordA := 0.0
	if n%4 > 1 {
		coordA = 100.0
	}
	coordB := 0.0
	if n%2 != 0 {
		coordB = 100.0
	}
	switch {
	case n < 4:
		r := (y - coordA*kG - coordB*kB) / kR
		if 0 <= r && r <= 100 {
			return [3]float64{r, coordA, coordB}
		}
	case n < 8:
		g := (y - coordB*kR - coordA*kB) / kG
		if 0 <= g && g <= 100 {
			return [3]float64{coordB, g, coordA}
		}
	default:
		b := (y - coordA*kR - coordB*kG) / kB
		if 0 <= b && b <= 100 {
			return [3]float64{coordA, coordB, b}
		}
	}
	return [3]float64{-1, -1, -1}
}

func bisectToSegment(y, targetHue float64) (left, right [3]float64) {
	left = [3]float64{-1, -1, -1}
	right = left
	var leftHue, rightHue float64
	initialized := false
	uncut := true
	for n := 0; n < 12; n++ {
		mid := nthVertex(y, n)
		if mid[0] < 0 {
			continue
		}
		midHue := hueOfLinrgb(mid)
		if !initialized {
			left, right = mid, mid
			leftHue, rightHue = midHue, midHue
			initialized = true
			continue
		}
		if uncut || areInCyclicOrder(leftHue, midHue, rightHue) {
			uncut = false
			if areInCyclicOrder(leftHue, targetHue, midHue) {
				right, rightHue = mid, midHue
			} else {
				left, leftHue = mid, midHue
			}
		}
	}
	return left, right
}

func bisectToLimit(y, targetHue float64) [3]float64 {
	left, right := bisectToSegment(y, targetHue)
	leftHue := hueOfLinrgb(left)
	for axis := 0; axis < 3; axis++ {
		if left[axis] == right[axis] {
			continue
		}
		var lPlane, rPlane int
		if left[axis] < right[axis] {
			lPlane = int(math.Floor(trueDelinearized(left[axis]) - 0.5))
			rPlane = int(math.Ceil(trueDelinearized(right[axis]) - 0.5))
		} else {
			lPlane = int(math.Ceil(trueDelinearized(left[axis]) - 0.5))
			rPlane = int(math.Floor(trueDelinearized(right[axis]) - 0.5))
		}
		for i := 0; i < 8; i++ {
			if absInt(rPlane-lPlane) <= 1 {
				break
			}
			mPlane := int(math.Floor(float64(lPlane+rPlane) / 2.0))
			if mPlane < 0 || mPlane >= len(criticalPlanes) {
				break
			}
			coord := criticalPlanes[mPlane]
			t := (coord - left[axis]) / (right[axis] - left[axis])
			mid := [3]float64{
				left[0] + (right[0]-left[0])*t,
				left[1] + (right[1]-left[1])*t,
				left[2] + (right[2]-left[2])*t,
			}
			midHue := hueOfLinrgb(mid)
			if areInCyclicOrder(leftHue, targetHue, midHue) {
				right, rPlane = mid, mPlane
			} else {
				left, leftHue, lPlane = mid, midHue, mPlane
			}
		}
	}
	return [3]float64{(left[0] + right[0]) / 2, (left[1] + right[1]) / 2, (left[2] + right[2]) / 2}
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func findResultByJ(hueRadians, chroma, y float64) uint32 {
	j := math.Sqrt(y) * 11.0
	vc := defaultViewingConditions
	tInnerCoeff := 1.0 / math.Pow(1.64-math.Pow(0.29, vc.n), 0.73)
	eHue := 0.25 * (math.Cos(hueRadians+2.0) + 3.8)
	p1 := eHue * (50000.0 / 13.0) * vc.nc * vc.ncb
	hSin := math.Sin(hueRadians)
	hCos := math.Cos(hueRadians)

	for iter := 0; iter < 5; iter++ {
		jNorm := j / 100.0
		alpha := 0.0
		if chroma != 0 && j != 0 {
			alpha = chroma / math.Sqrt(jNorm)
		}
		t := math.Pow(alpha*tInnerCoeff, 1.0/0.9)
		ac := vc.aw * math.Pow(jNorm, 1.0/vc.c/vc.z)
		p2 := ac / vc.nbb
		gamma := 23.0 * (p2 + 0.305) * t / (23.0*p1 + 11.0*t*hCos + 108.0*t*hSin)
		a := gamma * hCos
		b := gamma * hSin
		rA := (460.0*p2 + 451.0*a + 288.0*b) / 1403.0
		gA := (460.0*p2 - 891.0*a - 261.0*b) / 1403.0
		bA := (460.0*p2 - 220.0*a - 6300.0*b) / 1403.0
		lin := mulMat3(
			inverseChromaticAdaptation(rA),
			inverseChromaticAdaptation(gA),
			inverseChromaticAdaptation(bA),
			linrgbFromScaledDiscount,
		)
		if lin[0] < 0 || lin[1] < 0 || lin[2] < 0 {
			return 0
		}
		fnj := yFromLinrgb[0]*lin[0] + yFromLinrgb[1]*lin[1] + yFromLinrgb[2]*lin[2]
		if fnj <= 0 {
			return 0
		}
		if iter == 4 || math.Abs(fnj-y) < 0.002 {
			if lin[0] > 100.01 || lin[1] > 100.01 || lin[2] > 100.01 {
				return 0
			}
			return argbFromLinrgb(lin)
		}
		j -= (fnj - y) * j / (2.0 * fnj)
	}
	return 0
}

func solveHctToInt(hueDegrees, chroma, lstar float64) uint32 {
	if chroma < 0.0001 || lstar < 0.0001 || lstar > 99.9999 {
		return argbFromLstar(lstar)
	}
	hueRadians := sanitizeDegrees(hueDegrees) / 180.0 * math.Pi
	y := yFromLstar(lstar)
	if exact := findResultByJ(hueRadians, chroma, y); exact != 0 {
		return exact
	}
	return argbFromLinrgb(bisectToLimit(y, hueRadians))
}

// --- DynamicScheme & FlClash ColorStyle Derivation ---

func fixIfDisliked(h Hct) Hct {
	hr := math.Round(h.Hue)
	cr := math.Round(h.Chroma)
	tr := math.Round(h.Tone)
	if hr >= 90 && hr <= 111 && cr > 16 && tr < 65 {
		return HctFrom(h.Hue, h.Chroma, 70)
	}
	return h
}

func rawTemperature(h Hct) float64 {
	_, a, b := labFromArgb(h.argb)
	hue := sanitizeDegrees(math.Atan2(b, a) * 180.0 / math.Pi)
	chroma := math.Hypot(a, b)
	return -0.5 + 0.02*math.Pow(chroma, 1.07)*math.Cos(sanitizeDegrees(hue-50.0)*math.Pi/180.0)
}

func analogueHct(input Hct) Hct {
	var hcts [361]Hct
	var temps [361]float64
	inTemp := rawTemperature(input)
	coldest, warmest := inTemp, inTemp
	for hue := 0; hue <= 360; hue++ {
		c := HctFrom(float64(hue), input.Chroma, input.Tone)
		t := rawTemperature(c)
		hcts[hue] = c
		temps[hue] = t
		if t < coldest {
			coldest = t
		}
		if t > warmest {
			warmest = t
		}
	}
	rng := warmest - coldest
	rel := func(h int) float64 {
		if rng == 0 {
			return 0.5
		}
		return (temps[h] - coldest) / rng
	}
	startHue := int(math.Round(input.Hue)) % 360
	if startHue < 0 {
		startHue += 360
	}
	lastT := rel(startHue)
	total := 0.0
	for i := 0; i < 360; i++ {
		t := rel((startHue + i) % 360)
		total += math.Abs(t - lastT)
		lastT = t
	}
	step := total / 6.0
	lastT = rel(startHue)
	travelled := 0.0
	for add := 1; add <= 360; add++ {
		h := (startHue + add) % 360
		t := rel(h)
		travelled += math.Abs(t - lastT)
		if travelled >= step {
			return hcts[h]
		}
		lastT = t
	}
	return hcts[startHue]
}

func ratioOfTones(toneA, toneB float64) float64 {
	y1 := yFromLstar(clampDouble(0, 100, toneA))
	y2 := yFromLstar(clampDouble(0, 100, toneB))
	if y1 < y2 {
		y1, y2 = y2, y1
	}
	return (y1 + 5.0) / (y2 + 5.0)
}

func lighterTone(tone, ratio float64) float64 {
	if tone < 0 || tone > 100 {
		return -1
	}
	darkY := yFromLstar(tone)
	lightY := ratio*(darkY+5.0) - 5.0
	realC := (lightY + 5.0) / (darkY + 5.0)
	if realC < ratio && math.Abs(realC-ratio) > 0.04 {
		return -1
	}
	ret := lstarFromY(lightY) + 0.4
	if ret < 0 || ret > 100 {
		return -1
	}
	return ret
}

func darkerTone(tone, ratio float64) float64 {
	if tone < 0 || tone > 100 {
		return -1
	}
	lightY := yFromLstar(tone)
	darkY := (lightY+5.0)/ratio - 5.0
	if darkY < 0 {
		return -1
	}
	realC := (lightY + 5.0) / (darkY + 5.0)
	if realC < ratio && math.Abs(realC-ratio) > 0.04 {
		return -1
	}
	ret := lstarFromY(darkY) - 0.4
	if ret < 0 || ret > 100 {
		return -1
	}
	return ret
}

func foregroundTone(bgTone, ratio float64) float64 {
	lt := lighterTone(bgTone, ratio)
	if lt < 0 {
		lt = 100
	}
	dt := darkerTone(bgTone, ratio)
	if dt < 0 {
		dt = 0
	}
	lr := ratioOfTones(lt, bgTone)
	dr := ratioOfTones(dt, bgTone)
	if math.Round(bgTone) < 60 {
		negligible := math.Abs(lr-dr) < 0.1 && lr < ratio && dr < ratio
		if lr >= ratio || lr >= dr || negligible {
			return lt
		}
		return dt
	}
	if dr >= ratio || dr >= lr {
		return dt
	}
	return lt
}

func findDesiredChromaByTone(hue, chroma, tone float64, byDecreasingTone bool) float64 {
	answer := tone
	closest := HctFrom(hue, chroma, tone)
	if closest.Chroma >= chroma {
		return tone
	}
	smallestDelta := math.Abs(closest.Chroma - chroma)
	for delta := 1.0; delta < 50.0; delta++ {
		if math.Round(chroma) == math.Round(closest.Chroma) {
			return answer
		}
		nextTone := tone + delta
		if byDecreasingTone {
			nextTone = tone - delta
		}
		if nextTone < 0 || nextTone > 100 {
			break
		}
		h := HctFrom(hue, chroma, nextTone)
		d := math.Abs(h.Chroma - chroma)
		if d < smallestDelta {
			smallestDelta = d
			answer = nextTone
		}
	}
	return answer
}

type paletteCacheKey struct {
	seed      uint32
	dark      bool
	style     ColorStyle
	pureBlack bool
}

var (
	paletteCache      = map[paletteCacheKey]*Palette{}
	paletteCacheOrder []paletteCacheKey
)

const maxCachedPalettes = 64

// SeededPalette generates (and caches) a full Material 3 Palette from a single seed color,
// supporting FlClash's four ColorStyles and OLED PureBlack mode.
func SeededPalette(seed ui.Color, dark bool, style ColorStyle, pureBlack bool) *Palette {
	if style == "" {
		style = ColorStyleSoft
	}
	key := paletteCacheKey{
		seed:      argbFromColor(seed),
		dark:      dark,
		style:     style,
		pureBlack: pureBlack && dark,
	}
	if p, ok := paletteCache[key]; ok {
		return p
	}
	p := buildSeededPalette(seed, dark, style, pureBlack && dark)
	if len(paletteCacheOrder) >= maxCachedPalettes {
		oldest := paletteCacheOrder[0]
		paletteCacheOrder = paletteCacheOrder[1:]
		delete(paletteCache, oldest)
	}
	paletteCache[key] = p
	paletteCacheOrder = append(paletteCacheOrder, key)
	return p
}

func buildSeededPalette(seed ui.Color, dark bool, style ColorStyle, pureBlack bool) *Palette {
	source := HctFromColor(seed)
	var primaryTP, secondaryTP, tertiaryTP, neutralTP, neutralVariantTP TonalPalette
	switch style {
	case ColorStyleVivid, ColorStyleTinted:
		primaryTP = TonalPalette{Hue: source.Hue, Chroma: source.Chroma}
		secondaryTP = TonalPalette{Hue: source.Hue, Chroma: math.Max(source.Chroma-32.0, source.Chroma*0.5)}
		tertiaryTP = TonalPalette{Hue: fixIfDisliked(analogueHct(source)).Hue, Chroma: fixIfDisliked(analogueHct(source)).Chroma}
		neutralTP = TonalPalette{Hue: source.Hue, Chroma: source.Chroma / 8.0}
		neutralVariantTP = TonalPalette{Hue: source.Hue, Chroma: (source.Chroma / 8.0) + 4.0}
	case ColorStyleMonochrome:
		primaryTP = TonalPalette{Hue: source.Hue, Chroma: 0}
		secondaryTP = TonalPalette{Hue: source.Hue, Chroma: 0}
		tertiaryTP = TonalPalette{Hue: source.Hue, Chroma: 0}
		neutralTP = TonalPalette{Hue: source.Hue, Chroma: 0}
		neutralVariantTP = TonalPalette{Hue: source.Hue, Chroma: 0}
	default: // ColorStyleSoft (TonalSpot)
		primaryTP = TonalPalette{Hue: source.Hue, Chroma: 36.0}
		secondaryTP = TonalPalette{Hue: source.Hue, Chroma: 16.0}
		tertiaryTP = TonalPalette{Hue: sanitizeDegrees(source.Hue + 60.0), Chroma: 24.0}
		neutralTP = TonalPalette{Hue: source.Hue, Chroma: 6.0}
		neutralVariantTP = TonalPalette{Hue: source.Hue, Chroma: 8.0}
	}
	errorTP := TonalPalette{Hue: 25.0, Chroma: 84.0}

	const (
		surfaceChroma          = 2.5
		paperChroma            = 1.5
		tintDivisor            = 6.0
		maxTintChroma          = 8.0
		primaryContainerChroma = 48.0
		darkOnSurfaceTone      = 95.0
	)
	var tintChroma *float64
	if style == ColorStyleTinted {
		v := math.Min(source.Chroma/tintDivisor, maxTintChroma)
		tintChroma = &v
	}

	neutralColor := func(tp TonalPalette, maxChroma, tone float64) ui.Color {
		c := math.Min(tp.Chroma, maxChroma)
		if tintChroma != nil {
			c = *tintChroma
		}
		return HctFrom(tp.Hue, c, tone).Color()
	}

	paperC := paperChroma
	if dark {
		paperC = surfaceChroma
	}

	// Surface ladder: outer canvas (SurfaceContainer) frames borderless tonal islands
	// (Surface stage & SurfaceContainerLow drawers/cards) with clear M3 hierarchy.
	var (
		surfTone, surfDimTone, surfBrightTone float64
		scLowestTone, scLowTone, scTone       float64
		scHighTone, scHighestTone             float64
	)
	if dark {
		surfTone, surfDimTone, surfBrightTone = 12, 8, 22
		scLowestTone, scLowTone, scTone = 9, 16, 9
		scHighTone, scHighestTone = 20, 25
	} else {
		surfTone, surfDimTone, surfBrightTone = 100, 92, 100
		scLowestTone, scLowTone, scTone = 100, 97, 94
		scHighTone, scHighestTone = 92, 89
	}

	surface := neutralColor(neutralTP, paperC, surfTone)
	surfaceDim := neutralColor(neutralTP, surfaceChroma, surfDimTone)
	surfaceBright := neutralColor(neutralTP, paperC, surfBrightTone)
	scLowest := neutralColor(neutralTP, surfaceChroma, scLowestTone)
	scLow := neutralColor(neutralTP, surfaceChroma, scLowTone)
	sc := neutralColor(neutralTP, surfaceChroma, scTone)
	scHigh := neutralColor(neutralTP, surfaceChroma, scHighTone)
	scHighest := neutralColor(neutralTP, surfaceChroma, scHighestTone)

	onSurface := neutralTP.Tone(10)
	onSurfaceVariant := neutralVariantTP.Tone(34)
	outline := neutralVariantTP.Tone(52)
	outlineVariant := neutralColor(neutralVariantTP, 3.0, 90)
	inverseSurface := neutralTP.Tone(20)
	inverseOnSurface := neutralTP.Tone(95)
	if dark {
		onSurface = neutralTP.Tone(darkOnSurfaceTone)
		onSurfaceVariant = neutralVariantTP.Tone(78)
		outline = neutralVariantTP.Tone(58)
		outlineVariant = neutralColor(neutralVariantTP, 3.0, 22)
		inverseSurface = neutralTP.Tone(90)
		inverseOnSurface = neutralTP.Tone(20)
	}

	// Primary & accent
	var primary, onPrimary, primaryContainer, onPrimaryContainer, inversePrimary ui.Color
	switch style {
	case ColorStyleMonochrome:
		if dark {
			primary = primaryTP.Tone(96)
			onPrimary = primaryTP.Tone(10)
			primaryContainer = primaryTP.Tone(28)
			onPrimaryContainer = primaryTP.Tone(94)
			inversePrimary = primaryTP.Tone(35)
		} else {
			primary = primaryTP.Tone(12)
			onPrimary = primaryTP.Tone(96)
			primaryContainer = primaryTP.Tone(88)
			onPrimaryContainer = primaryTP.Tone(12)
			inversePrimary = primaryTP.Tone(80)
		}
	case ColorStyleVivid, ColorStyleTinted:
		var accentTone float64
		if dark {
			accentTone = clampDouble(56, 76, source.Tone+4)
		} else {
			accentTone = clampDouble(35, 52, source.Tone)
		}
		primary = HctFrom(source.Hue, source.Chroma, accentTone).Color()
		onPrimary = primaryTP.Tone(foregroundTone(accentTone, 4.5))
		contTP := TonalPalette{Hue: primaryTP.Hue, Chroma: math.Min(primaryTP.Chroma, primaryContainerChroma)}
		cTone := 90.0
		if dark {
			cTone = 28.0
		}
		primaryContainer = contTP.Tone(cTone)
		onPrimaryContainer = contTP.Tone(foregroundTone(cTone, 4.5))
		if dark {
			inversePrimary = primaryTP.Tone(40)
		} else {
			inversePrimary = primaryTP.Tone(80)
		}
	default: // Soft
		if dark {
			primary = primaryTP.Tone(80)
			onPrimary = primaryTP.Tone(20)
			primaryContainer = primaryTP.Tone(30)
			onPrimaryContainer = primaryTP.Tone(90)
			inversePrimary = primaryTP.Tone(40)
		} else {
			primary = primaryTP.Tone(40)
			onPrimary = primaryTP.Tone(100)
			primaryContainer = primaryTP.Tone(90)
			onPrimaryContainer = primaryTP.Tone(30)
			inversePrimary = primaryTP.Tone(80)
		}
	}

	// Secondary
	var secondary, onSecondary, secondaryContainer, onSecondaryContainer ui.Color
	if style == ColorStyleMonochrome {
		if dark {
			secondary, onSecondary = secondaryTP.Tone(80), secondaryTP.Tone(12)
			secondaryContainer, onSecondaryContainer = secondaryTP.Tone(24), secondaryTP.Tone(90)
		} else {
			secondary, onSecondary = secondaryTP.Tone(36), secondaryTP.Tone(98)
			secondaryContainer, onSecondaryContainer = secondaryTP.Tone(88), secondaryTP.Tone(12)
		}
	} else {
		if dark {
			secondary, onSecondary = secondaryTP.Tone(80), secondaryTP.Tone(20)
			secTone := 28.0
			if style != ColorStyleSoft {
				secTone = findDesiredChromaByTone(secondaryTP.Hue, secondaryTP.Chroma, 28.0, false)
			}
			secondaryContainer = secondaryTP.Tone(secTone)
			onSecondaryContainer = secondaryTP.Tone(foregroundTone(secTone, 4.5))
		} else {
			secondary, onSecondary = secondaryTP.Tone(40), secondaryTP.Tone(100)
			secTone := 90.0
			if style != ColorStyleSoft {
				secTone = findDesiredChromaByTone(secondaryTP.Hue, secondaryTP.Chroma, 90.0, true)
			}
			secondaryContainer = secondaryTP.Tone(secTone)
			onSecondaryContainer = secondaryTP.Tone(foregroundTone(secTone, 4.5))
		}
	}

	// Tertiary
	var tertiary, onTertiary, tertiaryContainer, onTertiaryContainer ui.Color
	if style == ColorStyleMonochrome {
		if dark {
			tertiary, onTertiary = tertiaryTP.Tone(88), tertiaryTP.Tone(10)
			tertiaryContainer, onTertiaryContainer = tertiaryTP.Tone(32), tertiaryTP.Tone(92)
		} else {
			tertiary, onTertiary = tertiaryTP.Tone(28), tertiaryTP.Tone(98)
			tertiaryContainer, onTertiaryContainer = tertiaryTP.Tone(84), tertiaryTP.Tone(10)
		}
	} else if dark {
		tertiary, onTertiary = tertiaryTP.Tone(80), tertiaryTP.Tone(20)
		tTone := 30.0
		if style != ColorStyleSoft {
			tTone = fixIfDisliked(tertiaryTP.Hct(source.Tone)).Tone
			if tTone > 45 {
				tTone = 32
			}
		}
		tertiaryContainer = tertiaryTP.Tone(tTone)
		onTertiaryContainer = tertiaryTP.Tone(foregroundTone(tTone, 4.5))
	} else {
		tertiary, onTertiary = tertiaryTP.Tone(40), tertiaryTP.Tone(100)
		tTone := 90.0
		if style != ColorStyleSoft {
			tTone = fixIfDisliked(tertiaryTP.Hct(source.Tone)).Tone
			if tTone < 80 {
				tTone = 88
			}
		}
		tertiaryContainer = tertiaryTP.Tone(tTone)
		onTertiaryContainer = tertiaryTP.Tone(foregroundTone(tTone, 4.5))
	}

	// Error
	var errCol, onErrCol, errCont, onErrCont ui.Color
	if dark {
		errCol, onErrCol = errorTP.Tone(80), errorTP.Tone(20)
		errCont, onErrCont = errorTP.Tone(30), errorTP.Tone(90)
	} else {
		errCol, onErrCol = errorTP.Tone(40), errorTP.Tone(100)
		errCont, onErrCont = errorTP.Tone(90), errorTP.Tone(30)
	}

	// Ensure strict WCAG contrast invariants across all containers and outlines.
	ensureContrast := func(fg ui.Color, bg ui.Color, minRatio float64) ui.Color {
		if ContrastRatio(fg, bg) >= minRatio {
			return fg
		}
		h := HctFromColor(fg)
		bgH := HctFromColor(bg)
		t := foregroundTone(bgH.Tone, minRatio)
		return HctFrom(h.Hue, h.Chroma, t).Color()
	}

	onPrimary = ensureContrast(onPrimary, primary, 4.5)
	onPrimaryContainer = ensureContrast(onPrimaryContainer, primaryContainer, 4.5)
	onSecondaryContainer = ensureContrast(onSecondaryContainer, secondaryContainer, 4.5)
	onTertiaryContainer = ensureContrast(onTertiaryContainer, tertiaryContainer, 4.5)
	onErrorContainer := ensureContrast(onErrCont, errCont, 4.5)
	onSurface = ensureContrast(onSurface, surface, 7.0)
	onSurfaceVariant = ensureContrast(onSurfaceVariant, surface, 4.5)
	outline = ensureContrast(outline, surface, 3.0)

	// PureBlack remapping (OLED dark mode, preserves rail/card tonal separation)
	if pureBlack && dark {
		surface = ui.RGB(0, 0, 0)
		surfaceDim = ui.RGB(0, 0, 0)
		scLowest = ui.RGB(0, 0, 0)
		sc = ui.RGB(0, 0, 0)
		scLow = neutralColor(neutralTP, surfaceChroma, 8)
		scHigh = neutralColor(neutralTP, surfaceChroma, 13)
		scHighest = neutralColor(neutralTP, surfaceChroma, 18)
		surfaceBright = neutralColor(neutralTP, surfaceChroma, 20)
	}

	// Semantic diff & status palettes
	plusTP := TonalPalette{Hue: 148, Chroma: 38}
	warnTP := TonalPalette{Hue: 68, Chroma: 48}
	var plus, plusCont, warn, warnCont ui.Color
	if dark {
		plus, plusCont = plusTP.Tone(78), plusTP.Tone(24)
		warn, warnCont = warnTP.Tone(80), warnTP.Tone(28)
	} else {
		plus, plusCont = plusTP.Tone(38), plusTP.Tone(92)
		warn, warnCont = warnTP.Tone(42), warnTP.Tone(92)
	}
	minus, minusCont := errCol, errCont

	hoverAlpha := float32(0.05)
	selAlpha := float32(0.08)
	if dark {
		hoverAlpha = 0.06
		selAlpha = 0.11
	}
	scrim := ui.RGBA(0, 0, 0, 0.28)
	if dark {
		scrim = ui.RGBA(0, 0, 0, 0.52)
	}

	return &Palette{
		Dark:      dark,
		Style:     style,
		Seed:      seed,
		PureBlack: pureBlack,

		Surface:                 surface,
		SurfaceDim:              surfaceDim,
		SurfaceBright:           surfaceBright,
		SurfaceContainerLowest:  scLowest,
		SurfaceContainerLow:     scLow,
		SurfaceContainer:        sc,
		SurfaceContainerHigh:    scHigh,
		SurfaceContainerHighest: scHighest,

		Primary:              primary,
		OnPrimary:            onPrimary,
		PrimaryContainer:     primaryContainer,
		OnPrimaryContainer:   onPrimaryContainer,
		Secondary:            secondary,
		OnSecondary:          onSecondary,
		SecondaryContainer:   secondaryContainer,
		OnSecondaryContainer: onSecondaryContainer,
		Tertiary:             tertiary,
		OnTertiary:           onTertiary,
		TertiaryContainer:    tertiaryContainer,
		OnTertiaryContainer:  onTertiaryContainer,
		Error:                errCol,
		OnError:              onErrCol,
		ErrorContainer:       errCont,
		OnErrorContainer:     onErrorContainer,

		OnSurface:        onSurface,
		OnSurfaceVariant: onSurfaceVariant,
		Outline:          outline,
		OutlineVariant:   outlineVariant,
		InverseSurface:   inverseSurface,
		InverseOnSurface: inverseOnSurface,
		InversePrimary:   inversePrimary,
		Scrim:            scrim,

		Hover:            onSurface.Alpha(hoverAlpha),
		Sel:              onSurface.Alpha(selAlpha),
		Plus:             plus,
		PlusContainer:    plusCont,
		Minus:            minus,
		MinusContainer:   minusCont,
		Warning:          warn,
		WarningContainer: warnCont,
	}
}
