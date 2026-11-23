package computer

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"math"
)

// RedactScreenshot masks pixels covered by unauthorized layer-0 application
// windows in jpg. Windows must be ordered front-to-back in global display
// points; an authorized window in front keeps its pixels over an unauthorized
// window behind it.
func RedactScreenshot(jpg []byte, d Display, scale float64, windows []Window, allowed func(App) bool) ([]byte, error) {
	if len(windows) == 0 {
		return jpg, nil
	}
	src, err := jpeg.Decode(bytes.NewReader(jpg))
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return jpg, nil
	}

	// mask values: 0 = untouched, 1 = allowed/visible, 2 = unauthorized/redacted.
	mask := make([]uint8, w*h)
	hasRedacted := false
	for _, win := range windows {
		if win.W <= 0 || win.H <= 0 {
			continue
		}
		x0 := max(0, int(math.Floor((win.X-d.X)*scale)))
		y0 := max(0, int(math.Floor((win.Y-d.Y)*scale)))
		x1 := min(w, int(math.Ceil((win.X+win.W-d.X)*scale)))
		y1 := min(h, int(math.Ceil((win.Y+win.H-d.Y)*scale)))
		if x0 >= x1 || y0 >= y1 {
			continue
		}
		val := uint8(2)
		if allowed != nil && allowed(win.App) {
			val = 1
		}
		for y := y0; y < y1; y++ {
			row := y * w
			for x := x0; x < x1; x++ {
				if mask[row+x] == 0 {
					mask[row+x] = val
					if val == 2 {
						hasRedacted = true
					}
				}
			}
		}
	}
	if !hasRedacted {
		return jpg, nil
	}

	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	fill := color.RGBA{R: 28, G: 28, B: 30, A: 255}
	for y := 0; y < h; y++ {
		row := y * w
		for x := 0; x < w; x++ {
			if mask[row+x] == 2 {
				dst.SetRGBA(x, y, fill)
			} else {
				dst.Set(x, y, src.At(b.Min.X+x, b.Min.Y+y))
			}
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 80}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
