// SPDX-License-Identifier: AGPL-3.0-only

// Copyright (C) 2026 MTN Media Group.

// Package codec wraps PNG, JPEG, and lossless WebP I/O.
package codec

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"image/png"

	nativewebp "github.com/HugoSmits86/nativewebp"
	xwebp "golang.org/x/image/webp"
)

type Format string

const (
	FormatPNG     Format = "image/png"
	FormatJPEG    Format = "image/jpeg"
	FormatWebP    Format = "image/webp"
	FormatUnknown Format = ""
)

const DefaultJPEGQuality = 95   // reason: matches v1 quality calibration; embed enforces this floor for JPEG outputs.
const maxImageDim = 4096        // reason: 4K is the practical upper bound for v1; matches the 4096 resampling cap.
const maxImagePixels = 16 << 20 // reason: covers 4K with headroom; bounds each float64 plane near 128MB and full pipeline near 768MB.

var ErrUnsupported = errors.New("codec: unsupported format")
var ErrImageTooLarge = errors.New("codec: image too large")

func Sniff(data []byte) Format {
	switch {
	case len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}):
		return FormatPNG
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return FormatJPEG
	case len(data) >= 12 && bytes.Equal(data[0:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return FormatWebP
	}
	return FormatUnknown
}

// Decode picks the decoder from the bytes, not a caller-supplied MIME type.
func Decode(data []byte) (image.Image, Format, error) {
	f := Sniff(data)
	if f == FormatUnknown {
		return nil, FormatUnknown, ErrUnsupported
	}
	if err := rejectOversizedImage(data, f); err != nil {
		return nil, f, err
	}
	r := bytes.NewReader(data)
	switch f {
	case FormatPNG:
		img, err := png.Decode(r)
		return img, f, err
	case FormatJPEG:
		img, err := jpeg.Decode(r)
		return img, f, err
	case FormatWebP:
		img, err := xwebp.Decode(r)
		return img, f, err
	}
	return nil, FormatUnknown, ErrUnsupported
}

func rejectOversizedImage(data []byte, f Format) error {
	var cfg image.Config
	var err error
	r := bytes.NewReader(data)
	switch f {
	case FormatPNG:
		cfg, err = png.DecodeConfig(r)
	case FormatJPEG:
		cfg, err = jpeg.DecodeConfig(r)
	case FormatWebP:
		cfg, err = xwebp.DecodeConfig(r)
	default:
		return ErrUnsupported
	}
	if err != nil {
		return err
	}
	if cfg.Width > maxImageDim || cfg.Height > maxImageDim || cfg.Width*cfg.Height > maxImagePixels {
		return ErrImageTooLarge
	}
	return nil
}

type EncodeOptions struct {
	JPEGQuality int // 1..100; 0 means DefaultJPEGQuality.
	PNGFastest  bool
}

// Encode writes img. WebP output is always lossless (no CGO).
func Encode(img image.Image, format Format, opt EncodeOptions) ([]byte, error) {
	var buf bytes.Buffer
	switch format {
	case FormatPNG:
		level := png.DefaultCompression
		if opt.PNGFastest {
			level = png.BestSpeed
		}
		enc := png.Encoder{CompressionLevel: level}
		if err := enc.Encode(&buf, img); err != nil {
			return nil, err
		}
	case FormatJPEG:
		q := opt.JPEGQuality
		if q == 0 {
			q = DefaultJPEGQuality
		}
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
			return nil, err
		}
	case FormatWebP:
		if err := nativewebp.Encode(&buf, img, nil); err != nil {
			return nil, err
		}
	default:
		return nil, ErrUnsupported
	}
	return buf.Bytes(), nil
}
