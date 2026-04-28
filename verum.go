// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 MTN Media Group.

// Package verum embeds and detects a small, keyed, machine-readable mark in
// the pixels of an image. The mark is invisible at normal viewing distance,
// survives common transformations, and is verifiable without the original.
//
// The high-level pipeline:
//
//	original → decode → YCbCr planes → tile → 8x8 DCT → bias mid-frequency
//	coefficient pairs by an HMAC-keyed bit pattern → IDCT → encode
//
// Detection inverts the same steps and votes across tile copies of the
// frame, gated by an HMAC-derived sync header and CRC32.
package verum

import (
	"context"
)

// EmbedResult is what Embed returns. Data is the encoded image; the rest is
// diagnostic and lets callers decide whether to ship the result.
type EmbedResult struct {
	Data              []byte
	MimeType          string
	PayloadDigest     string
	KeyID             string
	Version           int
	SelfDetection     DetectResult
	Quality           QualityReport
	Metadata          map[string]string
	ChangedPixelRatio float64
}

// QualityReport captures the gate measurements taken on the embedded image
// against the original. Tiles is the count of tiles that received bits.
type QualityReport struct {
	SSIM     float64
	PSNR     float64
	MaxDelta float64
	Tiles    int
}

// DetectResult is the union of every signal Detect produces. Detected and
// Possible are derived from Confidence using the configured bands.
type DetectResult struct {
	Detected          bool
	Possible          bool
	Confidence        float64
	KeyID             string
	Version           int
	PayloadDigest     string
	RecoveredBits     int
	TotalBits         int
	SupportingTiles   int
	TotalTilesChecked int
	BestScale         float64
	CropEstimate      float64
	Details           map[string]float64
}

// Embed writes the keyed mark into the pixels of data. The returned image
// preserves alpha and the original colour space approximation; the caller
// chooses output mimeType (empty string keeps the source format).
func Embed(ctx context.Context, data []byte, mimeType string, payload Payload, cfg Config) (*EmbedResult, error) {
	return embed(ctx, data, mimeType, payload, cfg)
}

// Detect scans data for any mark produced by any of cfg.DetectionKeys (plus
// cfg.ActiveKey if set). It returns the highest-confidence match across the
// configured detection scales. mimeType is advisory; the real format is
// always determined by signature sniffing.
func Detect(ctx context.Context, data []byte, mimeType string, cfg Config) (*DetectResult, error) {
	return detect(ctx, data, mimeType, cfg)
}

// Verify is Detect with an additional check that the recovered payload
// digest matches the digest computed from expected. It returns the
// underlying DetectResult; callers compare its PayloadDigest to expected's.
// Mismatched digests are returned with Detected=false.
func Verify(ctx context.Context, data []byte, mimeType string, expected Payload, cfg Config) (*DetectResult, error) {
	res, err := Detect(ctx, data, mimeType, cfg)
	if err != nil {
		return nil, err
	}
	want, err := computeDigest(&expected, cfg.ActiveKey.Secret)
	if err != nil {
		return nil, err
	}
	if res.PayloadDigest != hexDigest(want) {
		res.Detected = false
		res.Possible = false
	}
	return res, nil
}
