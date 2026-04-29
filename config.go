// SPDX-License-Identifier: AGPL-3.0-only

// Copyright (C) 2026 MTN Media Group.

package verum

import (
	"fmt"
	"math"
)

const (
	MinImageDim               = 256  // reason: under 256px the 64px tile grid carries fewer than 16 RS frame copies, so detection cannot vote past noise.
	DefaultTileSize           = 64   // reason: 64px tiles balance frame capacity (8 per dimension) against perceptual masking granularity below 2048px source.
	LargeTileSize             = 128  // reason: 128px tiles preserve frame capacity at >=2048px source where 64px tiles produce too many micro-tiles to mask cleanly.
	minDetectionScale         = 0.25 // reason: below 0.25 the resized image falls under MinImageDim before detection.
	maxDetectionScale         = 2.0  // reason: above 2x the resampled image exceeds practical embed sizes for current calibration.
	minKeySecretLen           = 16   // reason: HMAC-SHA256 key minimum strength; below 16 bytes weakens the keyed digest below symmetric-encryption norms.
	maxKeyIDLen               = 16   // reason: 16-byte key ID limit aligns with HMAC blob frame budget and keeps the namespace fixed-width.
	maxJPEGQuality            = 100  // reason: JPEG quality is conventionally 1..100, with 100 the maximum.
	maxQualityDelta           = 255  // reason: quality deltas are measured on 8-bit luma samples, so higher limits are not meaningful.
	maxDetectionScalesEntries = 8    // reason: caps user-supplied scale hints to bound CPU sweep.
)

type Key struct {
	ID     string
	Secret []byte
}

type StrengthProfile string

const (
	StrengthInvisible StrengthProfile = "invisible"
	StrengthBalanced  StrengthProfile = "balanced"
	StrengthRobust    StrengthProfile = "robust"
)

type IncludeMetadata string

const (
	IncludeMetadataNone     IncludeMetadata = "none"
	IncludeMetadataStandard IncludeMetadata = "standard"
)

// QualityConfig zero values fall back to per-profile defaults.
type QualityConfig struct {
	MinSSIM        float64
	MinPSNR        float64
	MaxDelta       float64
	MaxChangeRatio float64
	MaxRetries     int
}

// DetectionConfig.Scales adds caller-supplied scale hints to the default sweep; supported values are from minDetectionScale to maxDetectionScale.
type DetectionConfig struct {
	Scales   []float64
	MinTiles int
}

type Config struct {
	ActiveKey     Key
	DetectionKeys []Key
	Strength      StrengthProfile
	// IncludeMetadata controls whether EmbedResult.Metadata is populated; DetectResult.Details is always populated.
	IncludeMetadata IncludeMetadata
	JPEGQuality     int // JPEG output quality; 0 uses the public API default of 95, JPEG output supports 95..100.
	Quality         QualityConfig
	Detection       DetectionConfig
}

func (c *Config) validate(forEmbed bool) error {
	if forEmbed {
		if c.ActiveKey.ID == "" {
			return fmt.Errorf("%w: ActiveKey.ID and ActiveKey.Secret are required", ErrInvalidConfig)
		}
		if len(c.ActiveKey.Secret) < minKeySecretLen {
			return fmt.Errorf("%w: ActiveKey.Secret must be at least %d bytes", ErrInvalidConfig, minKeySecretLen)
		}
		if len(c.ActiveKey.ID) > maxKeyIDLen {
			return fmt.Errorf("%w: ActiveKey.ID must be at most %d bytes", ErrInvalidConfig, maxKeyIDLen)
		}
	}
	keys := c.detectionKeys()
	if len(keys) == 0 {
		return ErrNoDetectionKeys
	}
	seen := make(map[string]bool)
	for _, key := range keys {
		if key.ID == "" {
			return fmt.Errorf("%w: detection key ID is required", ErrInvalidConfig)
		}
		if seen[key.ID] {
			return fmt.Errorf("%w: duplicate detection key ID %q", ErrInvalidConfig, key.ID)
		}
		seen[key.ID] = true
		if len(key.Secret) < minKeySecretLen {
			return fmt.Errorf("%w: detection key %q secret must be at least %d bytes", ErrInvalidConfig, key.ID, minKeySecretLen)
		}
		if len(key.ID) > maxKeyIDLen {
			return fmt.Errorf("%w: detection key ID must be at most %d bytes", ErrInvalidConfig, maxKeyIDLen)
		}
	}
	switch c.Strength {
	case "", StrengthInvisible, StrengthBalanced, StrengthRobust:
	default:
		return fmt.Errorf("%w: unknown strength %q", ErrInvalidConfig, c.Strength)
	}
	switch c.IncludeMetadata {
	case "", IncludeMetadataNone, IncludeMetadataStandard:
	default:
		return fmt.Errorf("%w: unknown metadata mode %q", ErrInvalidConfig, c.IncludeMetadata)
	}
	if c.JPEGQuality < 0 || c.JPEGQuality > maxJPEGQuality {
		return fmt.Errorf("%w: JPEG quality must be between 0 and %d", ErrInvalidConfig, maxJPEGQuality)
	}
	if math.IsNaN(c.Quality.MinSSIM) || math.IsInf(c.Quality.MinSSIM, 0) || c.Quality.MinSSIM < 0 || c.Quality.MinSSIM > 1 {
		return fmt.Errorf("%w: MinSSIM must be a finite value in [0, 1]", ErrInvalidConfig)
	}
	if math.IsNaN(c.Quality.MinPSNR) || math.IsInf(c.Quality.MinPSNR, 0) || c.Quality.MinPSNR < 0 {
		return fmt.Errorf("%w: MinPSNR must be a finite value >= 0", ErrInvalidConfig)
	}
	if math.IsNaN(c.Quality.MaxDelta) || math.IsInf(c.Quality.MaxDelta, 0) || c.Quality.MaxDelta < 0 || c.Quality.MaxDelta > maxQualityDelta {
		return fmt.Errorf("%w: MaxDelta must be a finite value in [0, %d]", ErrInvalidConfig, maxQualityDelta)
	}
	if math.IsNaN(c.Quality.MaxChangeRatio) || math.IsInf(c.Quality.MaxChangeRatio, 0) || c.Quality.MaxChangeRatio < 0 || c.Quality.MaxChangeRatio > 1 {
		return fmt.Errorf("%w: MaxChangeRatio must be a finite value in [0, 1]", ErrInvalidConfig)
	}
	if c.Quality.MaxRetries < 0 {
		return fmt.Errorf("%w: MaxRetries must be >= 0", ErrInvalidConfig)
	}
	if len(c.Detection.Scales) > maxDetectionScalesEntries {
		return fmt.Errorf("%w: detection scales must have at most %d entries", ErrInvalidConfig, maxDetectionScalesEntries)
	}
	for _, scale := range c.Detection.Scales {
		if math.IsNaN(scale) || scale < minDetectionScale || scale > maxDetectionScale {
			return fmt.Errorf("%w: detection scale must be >= %.2f and <= %.1f", ErrInvalidConfig, minDetectionScale, maxDetectionScale)
		}
	}
	return nil
}

func (c *Config) detectionKeys() []Key {
	if c.ActiveKey.ID == "" {
		return c.DetectionKeys
	}
	out := make([]Key, 0, len(c.DetectionKeys)+1)
	out = append(out, c.ActiveKey)
	for _, k := range c.DetectionKeys {
		if k.ID != c.ActiveKey.ID {
			out = append(out, k)
		}
	}
	return out
}

func (c *Config) strengthProfile() StrengthProfile {
	if c.Strength == "" {
		return StrengthInvisible
	}
	return c.Strength
}

type profileDefaults struct {
	delta, minSSIM, minPSNR, maxDelta, maxChangeRatio float64
}

var profileTable = map[StrengthProfile]profileDefaults{
	StrengthInvisible: {delta: 0.6, minSSIM: 0.999, minPSNR: 50, maxDelta: 12, maxChangeRatio: 0.2},  // reason: calibrated to keep visual quality above published SSIM/PSNR thresholds.
	StrengthBalanced:  {delta: 0.6, minSSIM: 0.997, minPSNR: 46, maxDelta: 18, maxChangeRatio: 0.4},  // reason: calibrated to keep visual quality above published SSIM/PSNR thresholds.
	StrengthRobust:    {delta: 22.0, minSSIM: 0.985, minPSNR: 38, maxDelta: 80, maxChangeRatio: 0.6}, // reason: calibration corpus trades quality for JPEG Q95 and downscale survival after v2 pair reseeding.
}

func strengthDelta(p StrengthProfile) float64 {
	return profileTable[p].delta
}

func qualityGates(p StrengthProfile, q QualityConfig) QualityConfig {
	d := profileTable[p]
	if q.MinSSIM == 0 {
		q.MinSSIM = d.minSSIM
	}
	if q.MinPSNR == 0 {
		q.MinPSNR = d.minPSNR
	}
	if q.MaxDelta == 0 {
		q.MaxDelta = d.maxDelta
	}
	if q.MaxChangeRatio == 0 {
		q.MaxChangeRatio = d.maxChangeRatio
	}
	if q.MaxRetries == 0 {
		q.MaxRetries = 6 // reason: six retries cover three strength steps with two delta drops each before yielding ErrQualityGateFailed.
	}
	return q
}
