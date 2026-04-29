// SPDX-License-Identifier: AGPL-3.0-only

// Copyright (C) 2026 MTN Media Group.

package codec

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"math"
	"testing"
)

var testPNGSignature = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A} // reason: PNG files start with this fixed eight-byte signature.

const testPNGUint32Size = 4       // reason: PNG chunk length, CRC, width, and height fields are 32-bit big-endian values.
const testPNGIHDRLength = 13      // reason: PNG IHDR chunk payload length is fixed by the PNG specification.
const testPNGIHDRWidthEnd = 4     // reason: PNG IHDR stores width in the first four payload bytes.
const testPNGIHDRHeightEnd = 8    // reason: PNG IHDR stores height in the next four payload bytes.
const testPNGIHDRBitDepth = 8     // reason: 8-bit samples keep the crafted IHDR accepted by png.DecodeConfig.
const testPNGIHDRColorRGBA = 6    // reason: PNG color type 6 is truecolor with alpha and needs no palette chunk.
const testPNGIHDRBitDepthOff = 8  // reason: PNG IHDR bit-depth byte follows width and height.
const testPNGIHDRColorTypeOff = 9 // reason: PNG IHDR color-type byte follows bit depth.
const testPNGIHDRType = "IHDR"    // reason: PNG dimension metadata is carried by the IHDR chunk.

func TestDecodeRejectsDecompressionBomb(t *testing.T) {
	const bombDim = 100000 // reason: regression target exceeds maxImageDim without allocating bomb pixels.
	_, _, err := Decode(pngWithIHDR(bombDim, bombDim))
	if !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("decode bomb error: got %v want %v", err, ErrImageTooLarge)
	}

	const normalDim = 2048 // reason: verifies common large PNGs still pass under decode limits.
	src := image.NewNRGBA(image.Rect(0, 0, normalDim, normalDim))
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatalf("encode normal png: %v", err)
	}
	decoded, format, err := Decode(buf.Bytes())
	if err != nil {
		t.Fatalf("decode normal png: %v", err)
	}
	if format != FormatPNG {
		t.Fatalf("decode normal format: got %q want %q", format, FormatPNG)
	}
	if decoded.Bounds() != src.Bounds() {
		t.Fatalf("decode normal bounds: got %v want %v", decoded.Bounds(), src.Bounds())
	}
}

func TestWebPRoundTrip(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 256, 256))
	for y := src.Bounds().Min.Y; y < src.Bounds().Max.Y; y++ {
		for x := src.Bounds().Min.X; x < src.Bounds().Max.X; x++ {
			i := src.PixOffset(x, y)
			src.Pix[i+0] = clip255(110 + 60*math.Sin(float64(x)*0.18) + 20*math.Cos(float64(y)*0.07))
			src.Pix[i+1] = clip255(130 + 50*math.Cos(float64(x+y)*0.11) + 15*math.Sin(float64(x-y)*0.05))
			src.Pix[i+2] = clip255(100 + 40*math.Sin(float64(x)*0.22+float64(y)*0.13) + 25*math.Cos(float64(y)*0.03))
			src.Pix[i+3] = 255
		}
	}

	encoded, err := Encode(src, FormatWebP, EncodeOptions{})
	if err != nil {
		t.Fatalf("encode webp: %v", err)
	}
	if got := Sniff(encoded); got != FormatWebP {
		t.Fatalf("sniff encoded webp: got %q want %q", got, FormatWebP)
	}

	decoded, format, err := Decode(encoded)
	if err != nil {
		t.Fatalf("decode webp: %v", err)
	}
	if format != FormatWebP {
		t.Fatalf("decode format: got %q want %q", format, FormatWebP)
	}
	if decoded.Bounds() != src.Bounds() {
		t.Fatalf("decoded bounds: got %v want %v", decoded.Bounds(), src.Bounds())
	}

	for y := src.Bounds().Min.Y; y < src.Bounds().Max.Y; y++ {
		for x := src.Bounds().Min.X; x < src.Bounds().Max.X; x++ {
			si := src.PixOffset(x, y)
			r, g, b, a := decoded.At(x, y).RGBA()
			if uint8(r>>8) != src.Pix[si+0] || uint8(g>>8) != src.Pix[si+1] || uint8(b>>8) != src.Pix[si+2] || uint8(a>>8) != src.Pix[si+3] {
				t.Fatalf("pixel mismatch at %d,%d: got RGBA(%d,%d,%d,%d) want RGBA(%d,%d,%d,%d)",
					x, y,
					uint8(r>>8), uint8(g>>8), uint8(b>>8), uint8(a>>8),
					src.Pix[si+0], src.Pix[si+1], src.Pix[si+2], src.Pix[si+3])
			}
		}
	}
}

func TestWebPAlphaRoundTrip(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := src.Bounds().Min.Y; y < src.Bounds().Max.Y; y++ {
		for x := src.Bounds().Min.X; x < src.Bounds().Max.X; x++ {
			i := src.PixOffset(x, y)
			switch (x/8 + y/8) % 3 {
			case 0:
				src.Pix[i+0] = clip255(70 + float64(x%17)*4)
				src.Pix[i+1] = clip255(80 + float64(y%19)*3)
				src.Pix[i+2] = clip255(90 + float64((x+y)%23)*3)
				src.Pix[i+3] = 255
			case 1:
				src.Pix[i+0] = 128
				src.Pix[i+1] = 0
				src.Pix[i+2] = 128
				src.Pix[i+3] = 128
			default:
				src.Pix[i+0] = 0
				src.Pix[i+1] = 0
				src.Pix[i+2] = 0
				src.Pix[i+3] = 0
			}
		}
	}

	encoded, err := Encode(src, FormatWebP, EncodeOptions{})
	if err != nil {
		t.Fatalf("encode webp: %v", err)
	}
	decoded, format, err := Decode(encoded)
	if err != nil {
		t.Fatalf("decode webp: %v", err)
	}
	if format != FormatWebP {
		t.Fatalf("decode format: got %q want %q", format, FormatWebP)
	}
	if decoded.Bounds() != src.Bounds() {
		t.Fatalf("decoded bounds: got %v want %v", decoded.Bounds(), src.Bounds())
	}

	for y := src.Bounds().Min.Y; y < src.Bounds().Max.Y; y++ {
		for x := src.Bounds().Min.X; x < src.Bounds().Max.X; x++ {
			want := color.RGBAModel.Convert(src.At(x, y)).(color.RGBA)
			got := color.RGBAModel.Convert(decoded.At(x, y)).(color.RGBA)
			if got != want {
				t.Fatalf("pixel mismatch at %d,%d: got RGBA(%d,%d,%d,%d) want RGBA(%d,%d,%d,%d)",
					x, y,
					got.R, got.G, got.B, got.A,
					want.R, want.G, want.B, want.A)
			}
		}
	}
}

func pngWithIHDR(width, height int) []byte {
	ihdr := make([]byte, testPNGIHDRLength)
	binary.BigEndian.PutUint32(ihdr[:testPNGIHDRWidthEnd], uint32(width))
	binary.BigEndian.PutUint32(ihdr[testPNGIHDRWidthEnd:testPNGIHDRHeightEnd], uint32(height))
	ihdr[testPNGIHDRBitDepthOff] = testPNGIHDRBitDepth
	ihdr[testPNGIHDRColorTypeOff] = testPNGIHDRColorRGBA
	return appendPNGChunk(append([]byte{}, testPNGSignature...), testPNGIHDRType, ihdr)
}

func appendPNGChunk(dst []byte, typ string, payload []byte) []byte {
	var field [testPNGUint32Size]byte
	binary.BigEndian.PutUint32(field[:], uint32(len(payload)))
	dst = append(dst, field[:]...)
	dst = append(dst, typ...)
	dst = append(dst, payload...)
	crcInput := append([]byte(typ), payload...)
	binary.BigEndian.PutUint32(field[:], crc32.ChecksumIEEE(crcInput))
	return append(dst, field[:]...)
}

func clip255(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(math.Round(v))
}
