//go:build ignore

// SPDX-License-Identifier: AGPL-3.0-only

// Copyright (C) 2026 MTN Media Group.

// Regenerate deterministic calibration fixtures with: go run ./testdata/generate.go
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"math/rand"
	"os"
	"path/filepath"
)

const size = 512 // reason: fixture canvas size, big enough to exercise DefaultTileSize tiling.

func main() {
	if err := os.MkdirAll("testdata", 0o755); err != nil {
		panic(err)
	}
	for i := 0; i < 10; i++ {
		writePNG(fmt.Sprintf("gradient_%02d.png", i), gradientFixture(i))
		writePNG(fmt.Sprintf("dark_%02d.png", i), darkFixture(i))
		writePNG(fmt.Sprintf("screenshot_%02d.png", i), screenshotFixture(i))
		writePNG(fmt.Sprintf("portrait_%02d.png", i), portraitFixture(i))
		writePNG(fmt.Sprintf("noisy_%02d.png", i), noisyFixture(i))
	}
}

func gradientFixture(seed int) image.Image {
	rng := rand.New(rand.NewSource(1100 + int64(seed))) // reason: deterministic corpus reproducibility seed.
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	wisps := randomBlobs(rng, 18)
	sky := seed%2 == 0
	for y := 0; y < size; y++ {
		fy := float64(y) / (size - 1)
		for x := 0; x < size; x++ {
			fx := float64(x) / (size - 1)
			texture := 2.5*rng.NormFloat64() + 6*fbm(fx*11+float64(seed), fy*11-float64(seed), 4) // reason: corpus-tuned gradient texture amplitudes to match real-image statistics.
			texture += 7 * blobField(wisps, fx, fy)                                               // reason: corpus-tuned gradient texture amplitude to match real-image statistics.
			wave := 9*math.Sin(float64(x)*0.19+float64(seed)*0.7) +
				7*math.Cos(float64(y)*0.16+float64(x)*0.05) +
				4*math.Sin(float64(x+y)*0.11)
			if sky {
				r := 78 + 92*fy + 7*fx + texture + wave
				g := 124 + 78*fy + 3*fx + texture*0.7 + wave*0.5
				b := 184 + 42*fy - 8*fx + texture*0.5
				set(img, x, y, r, g, b)
			} else {
				r := 150 + 56*fy + 13*fx + texture + wave*0.4
				g := 104 + 35*fy + 6*fx + texture*0.8 + wave*0.35
				b := 83 + 25*fy + texture*0.6 + wave*0.25
				set(img, x, y, r, g, b)
			}
		}
	}
	return img
}

func darkFixture(seed int) image.Image {
	rng := rand.New(rand.NewSource(2200 + int64(seed))) // reason: deterministic corpus reproducibility seed.
	img := photoTexture(seed, 78, 42, 1.1)              // reason: corpus-tuned dark photo texture amplitude to match real-image statistics.
	for i := 0; i < 42; i++ {
		x := rng.Intn(size - 70)
		y := rng.Intn(size - 70)
		w := 24 + rng.Intn(90)
		h := 18 + rng.Intn(80)
		c := color.RGBA{uint8(52 + rng.Intn(58)), uint8(50 + rng.Intn(55)), uint8(45 + rng.Intn(50)), 255}
		drawRect(img, x, y, w, h, c, 0.25+0.25*rng.Float64()) // reason: class-specific blend ratio range selected for dark-fixture depth variation.
	}
	addFineTexture(img, rng, 1.2) // reason: corpus-tuned dark fine texture amplitude to match real-image statistics.
	return img
}

func screenshotFixture(seed int) image.Image {
	rng := rand.New(rand.NewSource(3300 + int64(seed)))                   // reason: deterministic corpus reproducibility seed.
	img := photoTexture(seed, 188, 50, 0.4)                               // reason: corpus-tuned screenshot photo texture amplitude to match real-image statistics.
	drawRect(img, 0, 0, size, size, color.RGBA{238, 240, 242, 255}, 0.34) // reason: class-specific blend ratio keeps screenshot background lightly textured.
	drawRect(img, 0, 0, size, 42, color.RGBA{58, 65, 76, 255}, 0.82)      // reason: class-specific blend ratio keeps screenshot header distinct from content rows.
	for i := 0; i < 6; i++ {
		drawRect(img, 18+i*58, 15, 34, 9, color.RGBA{130, 139, 152, 255}, 1) // reason: class-specific blend ratio makes header controls fully opaque anchors.
	}
	for row := 0; row < 9; row++ {
		y := 68 + row*44
		drawRect(img, 24, y, 464, 30, color.RGBA{255, 255, 255, 255}, 0.58)                  // reason: class-specific blend ratio keeps rows visible without erasing texture.
		drawRect(img, 42, y+10, 80+rng.Intn(130), 7, color.RGBA{70, 82, 96, 255}, 0.86)      // reason: class-specific blend ratio keeps primary text bars distinct.
		drawRect(img, 190, y+10, 220+rng.Intn(120), 7, color.RGBA{149, 158, 169, 255}, 0.78) // reason: class-specific blend ratio keeps secondary text bars lower contrast.
	}
	for i := 0; i < 36; i++ {
		x := 28 + rng.Intn(430)
		y := 76 + rng.Intn(380)
		drawRect(img, x, y, 3+rng.Intn(9), 2+rng.Intn(7), color.RGBA{100, 145, 192, 255}, 0.7) // reason: class-specific blend ratio keeps accent pixels visible but not dominant.
	}
	addFineTexture(img, rng, 0.5) // reason: corpus-tuned screenshot fine texture amplitude to match real-image statistics.
	return img
}

func portraitFixture(seed int) image.Image {
	rng := rand.New(rand.NewSource(4400 + int64(seed))) // reason: deterministic corpus reproducibility seed.
	img := photoTexture(seed, 132, 48, 0.9)             // reason: corpus-tuned portrait photo texture amplitude to match real-image statistics.
	cx := 256 + rng.Intn(29) - 14
	drawEllipse(img, cx, 238, 92, 128, color.RGBA{185, 133, 101, 255}, 1)    // reason: class-specific blend ratio anchors the face region as fully opaque.
	drawEllipse(img, cx, 210, 120, 138, color.RGBA{58, 43, 38, 255}, 0.92)   // reason: class-specific blend ratio preserves hair texture while darkening it.
	drawEllipse(img, cx, 246, 82, 116, color.RGBA{196, 145, 111, 255}, 0.95) // reason: class-specific blend ratio keeps skin region dominant over base texture.
	drawRect(img, cx-142, 348, 284, 164, color.RGBA{52, 76, 104, 255}, 0.92) // reason: class-specific blend ratio preserves garment texture while separating it.
	drawEllipse(img, cx-34, 238, 8, 4, color.RGBA{38, 30, 28, 255}, 1)       // reason: class-specific blend ratio keeps eye contrast stable.
	drawEllipse(img, cx+34, 238, 8, 4, color.RGBA{38, 30, 28, 255}, 1)       // reason: class-specific blend ratio keeps eye contrast stable.
	drawRect(img, cx-34, 298, 68, 5, color.RGBA{132, 72, 67, 255}, 0.75)     // reason: class-specific blend ratio keeps mouth detail lower contrast than eyes.
	for i := 0; i < 800; i++ {
		x := cx - 118 + rng.Intn(236)
		y := 112 + rng.Intn(238)
		if ellipseContains(x, y, cx, 246, 90, 124) {
			addPixel(img, x, y, rng.NormFloat64()*5, rng.NormFloat64()*3, rng.NormFloat64()*2) // reason: corpus-tuned portrait skin texture amplitudes to match real-image statistics.
		}
	}
	for i := 0; i < 1800; i++ {
		x := cx - 132 + rng.Intn(264)
		y := 72 + rng.Intn(198)
		if ellipseContains(x, y, cx, 207, 124, 142) && !ellipseContains(x, y, cx, 254, 78, 112) {
			addPixel(img, x, y, -25+rng.NormFloat64()*8, -20+rng.NormFloat64()*6, -18+rng.NormFloat64()*5) // reason: corpus-tuned portrait hair texture amplitudes to match real-image statistics.
		}
	}
	addFineTexture(img, rng, 1.1) // reason: corpus-tuned portrait fine texture amplitude to match real-image statistics.
	return img
}

func noisyFixture(seed int) image.Image {
	return photoTexture(seed, 126, 60, 0.4) // reason: corpus-tuned noisy photo texture amplitude to match real-image statistics.
}

func photoTexture(seed int, base, contrast, grain float64) *image.RGBA {
	rng := rand.New(rand.NewSource(9900 + int64(seed))) // reason: deterministic corpus reproducibility seed.
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	phase := float64(seed) * 0.37 // reason: per-fixture phase offset selected to diverge fixtures within the same class.
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			xf, yf := float64(x), float64(y)
			r := base + contrast*(math.Sin(xf*0.18+phase)+0.33*math.Cos(yf*0.07-phase))              // reason: corpus-tuned photo texture amplitudes to match real-image statistics.
			g := base + contrast*(0.83*math.Cos((xf+yf)*0.11+phase*0.7)+0.25*math.Sin((xf-yf)*0.05)) // reason: corpus-tuned photo texture amplitudes to match real-image statistics.
			b := base + contrast*(0.66*math.Sin(xf*0.22+yf*0.13+phase)+0.42*math.Cos(yf*0.03))       // reason: corpus-tuned photo texture amplitudes to match real-image statistics.
			n := rng.NormFloat64() * grain
			set(img, x, y, r+n, g+n*0.8, b+n*0.7) // reason: corpus-tuned chroma noise ratios preserve luminance-dominant fine texture.
		}
	}
	return img
}

type blob struct {
	x, y, r, amp float64
}

func randomBlobs(rng *rand.Rand, n int) []blob {
	out := make([]blob, n)
	for i := range out {
		out[i] = blob{rng.Float64(), rng.Float64(), 0.04 + rng.Float64()*0.11, rng.NormFloat64()} // reason: per-fixture blob radius variation selected to diverge fixtures within the same class.
	}
	return out
}

func blobField(blobs []blob, x, y float64) float64 {
	var out float64
	for _, b := range blobs {
		dx, dy := x-b.x, y-b.y
		out += b.amp * math.Exp(-(dx*dx+dy*dy)/(2*b.r*b.r))
	}
	return out
}

func fbm(x, y float64, octaves int) float64 {
	var sum, amp, norm float64
	amp = 1
	for i := 0; i < octaves; i++ {
		sum += amp * valueNoise(x, y)
		norm += amp
		x *= 2.03
		y *= 1.97
		amp *= 0.5
	}
	return sum / norm
}

func valueNoise(x, y float64) float64 {
	x0 := math.Floor(x)
	y0 := math.Floor(y)
	fx := smooth(x - x0)
	fy := smooth(y - y0)
	a := hash2(int(x0), int(y0))
	b := hash2(int(x0)+1, int(y0))
	c := hash2(int(x0), int(y0)+1)
	d := hash2(int(x0)+1, int(y0)+1)
	return lerp(lerp(a, b, fx), lerp(c, d, fx), fy)*2 - 1
}

func hash2(x, y int) float64 {
	n := uint32(x*374761393 + y*668265263)
	n = (n ^ (n >> 13)) * 1274126177
	n ^= n >> 16
	return float64(n&0xffff) / 65535
}

func smooth(t float64) float64     { return t * t * (3 - 2*t) }
func lerp(a, b, t float64) float64 { return a + (b-a)*t }

func set(img *image.RGBA, x, y int, r, g, b float64) {
	i := y*img.Stride + x*4
	img.Pix[i+0] = clip(r)
	img.Pix[i+1] = clip(g)
	img.Pix[i+2] = clip(b)
	img.Pix[i+3] = 255
}

func addPixel(img *image.RGBA, x, y int, dr, dg, db float64) {
	if x < 0 || y < 0 || x >= size || y >= size {
		return
	}
	i := y*img.Stride + x*4
	img.Pix[i+0] = clip(float64(img.Pix[i+0]) + dr)
	img.Pix[i+1] = clip(float64(img.Pix[i+1]) + dg)
	img.Pix[i+2] = clip(float64(img.Pix[i+2]) + db)
}

func addFineTexture(img *image.RGBA, rng *rand.Rand, amp float64) {
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			n := rng.NormFloat64() * amp
			addPixel(img, x, y, n, n*0.9, n*0.75) // reason: corpus-tuned chroma noise ratios preserve luminance-dominant fine texture.
		}
	}
}

func drawRect(img *image.RGBA, x, y, w, h int, c color.RGBA, alpha float64) {
	for py := max(0, y); py < min(size, y+h); py++ {
		for px := max(0, x); px < min(size, x+w); px++ {
			blend(img, px, py, c, alpha)
		}
	}
}

func drawEllipse(img *image.RGBA, cx, cy, rx, ry int, c color.RGBA, alpha float64) {
	for y := max(0, cy-ry); y < min(size, cy+ry); y++ {
		for x := max(0, cx-rx); x < min(size, cx+rx); x++ {
			if ellipseContains(x, y, cx, cy, rx, ry) {
				blend(img, x, y, c, alpha)
			}
		}
	}
}

func ellipseContains(x, y, cx, cy, rx, ry int) bool {
	dx := float64(x-cx) / float64(rx)
	dy := float64(y-cy) / float64(ry)
	return dx*dx+dy*dy <= 1
}

func blend(img *image.RGBA, x, y int, c color.RGBA, alpha float64) {
	i := y*img.Stride + x*4
	img.Pix[i+0] = clip(float64(img.Pix[i+0])*(1-alpha) + float64(c.R)*alpha)
	img.Pix[i+1] = clip(float64(img.Pix[i+1])*(1-alpha) + float64(c.G)*alpha)
	img.Pix[i+2] = clip(float64(img.Pix[i+2])*(1-alpha) + float64(c.B)*alpha)
	img.Pix[i+3] = 255
}

func jpegRoundTrip(img image.Image, q int) image.Image {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
		panic(err)
	}
	out, err := jpeg.Decode(&buf)
	if err != nil {
		panic(err)
	}
	return out
}

func writePNG(name string, img image.Image) {
	f, err := os.Create(filepath.Join("testdata", name))
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
}

func clip(v float64) uint8 {
	return uint8(max(0, min(255, int(math.Round(v)))))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
