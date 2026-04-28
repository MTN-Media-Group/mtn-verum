// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 MTN Media Group.

// Package codec wraps decode and encode for the formats verum supports.
// It exists so the rest of the package never imports image/jpeg etc. directly,
// keeping format-specific concerns (quality settings, lossless flags) here.
package codec

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"

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

// ErrUnsupported is returned when a format cannot be decoded or encoded.
// Callers translate this to verum.ErrUnsupportedFormat at the API boundary.
var ErrUnsupported = errors.New("codec: unsupported format")

// Sniff identifies the input image format. It looks at the first bytes only —
// no full decode. Returns FormatUnknown if no signature matches.
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

// Decode returns the decoded image and the detected format. The format is
// derived from the bytes, not from any caller-supplied MIME type, so callers
// can't trick the decoder into the wrong codec.
func Decode(data []byte) (image.Image, Format, error) {
	f := Sniff(data)
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

// EncodeOptions controls per-format encoding choices. Only fields relevant
// to the selected format are honoured.
type EncodeOptions struct {
	JPEGQuality int  // 1..100; 0 means default (85)
	PNGFastest  bool // png.BestSpeed instead of DefaultCompression
}

// Encode writes the image in the requested format. WebP encoding is always
// lossless in v1 (nativewebp is pure-Go and lossless-only); a lossy WebP path
// would require a CGO build, which is intentionally avoided.
func Encode(img image.Image, format Format, opt EncodeOptions) ([]byte, error) {
	var buf bytes.Buffer
	if err := encodeTo(&buf, img, format, opt); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodeTo(w io.Writer, img image.Image, format Format, opt EncodeOptions) error {
	switch format {
	case FormatPNG:
		enc := png.Encoder{CompressionLevel: png.DefaultCompression}
		if opt.PNGFastest {
			enc.CompressionLevel = png.BestSpeed
		}
		return enc.Encode(w, img)
	case FormatJPEG:
		q := opt.JPEGQuality
		if q == 0 {
			q = 85
		}
		return jpeg.Encode(w, img, &jpeg.Options{Quality: q})
	case FormatWebP:
		return nativewebp.Encode(w, img, nil)
	}
	return ErrUnsupported
}
