// SPDX-License-Identifier: AGPL-3.0-only

// Copyright (C) 2026 MTN Media Group.

// Package verum embeds and detects a keyed mark in image pixels.
package verum

import (
	"context"
	"encoding/hex"
)

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

type QualityReport struct {
	SSIM     float64
	PSNR     float64
	MaxDelta float64
	Tiles    int
}

// DetectResult.Confidence is an empirical signal score, not a statistical probability.
type DetectResult struct {
	Detected      bool
	Possible      bool
	Confidence    float64
	KeyID         string
	Version       int
	PayloadDigest string
	TilesUsed     int
	TilesChecked  int
	BestScale     float64
	CropEstimate  float64
	Details       map[string]float64
}

// Embed writes the keyed mark into data; pixels below the alpha visibility floor are left alone. Empty mimeType keeps the source format. Returns ErrInvalidConfig, ErrUnsupportedFormat, ErrImageTooSmall, ErrImageTooLarge, ErrNoCapacity, ErrQualityGateFailed, ErrSelfDetectionFailed, or context errors.
func Embed(ctx context.Context, data []byte, mimeType string, payload Payload, cfg Config) (*EmbedResult, error) {
	return embed(ctx, data, mimeType, payload, cfg)
}

// IsEmbeddable reports whether data can carry a mark. Returns ErrInvalidConfig, ErrUnsupportedFormat, ErrImageTooSmall, ErrImageTooLarge, or ErrNoCapacity on input rejection.
func IsEmbeddable(srcImage []byte, cfg Config) (bool, error) {
	return isEmbeddable(srcImage, cfg)
}

// Detect tries every key in cfg and returns the best match. Returns ErrInvalidConfig, ErrNoDetectionKeys, ErrUnsupportedFormat, ErrImageTooSmall, ErrImageTooLarge, or context errors.
func Detect(ctx context.Context, data []byte, mimeType string, cfg Config) (*DetectResult, error) {
	return detect(ctx, data, mimeType, cfg)
}

// Verify detects, then recomputes the digest under the matched key and clears Detected/Possible on mismatch. Returns ErrInvalidConfig, ErrNoDetectionKeys, ErrUnsupportedFormat, ErrImageTooSmall, ErrImageTooLarge, or context errors.
func Verify(ctx context.Context, data []byte, mimeType string, expected Payload, cfg Config) (*DetectResult, error) {
	res, err := Detect(ctx, data, mimeType, cfg)
	if err != nil {
		return nil, err
	}
	var secret []byte
	for _, key := range cfg.detectionKeys() {
		if key.ID == res.KeyID {
			secret = key.Secret
			break
		}
	}
	if len(secret) == 0 {
		res.Detected = false
		res.Possible = false
		return res, nil
	}
	want, err := computeDigest(&expected, secret)
	if err != nil {
		return nil, err
	}
	if res.PayloadDigest != hex.EncodeToString(want) {
		res.Detected = false
		res.Possible = false
	}
	return res, nil
}
