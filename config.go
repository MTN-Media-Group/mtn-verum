// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 MTN Media Group.

package verum

import "fmt"

const (
	MinImageDim    = 256
	DefaultTileSize = 64
	LargeTileSize   = 96
	largeImageThreshold = 2048
)

// Key is one HMAC secret plus a stable identifier. Embedders use ActiveKey;
// detectors try every key in DetectionKeys (which should include the active
// one) so callers can rotate without breaking historic images.
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

type MetadataMode string

const (
	MetadataNone     MetadataMode = "none"
	MetadataStandard MetadataMode = "standard"
)

// QualityConfig holds the gates the embedder enforces after writing pixels.
// Zero values fall back to per-profile defaults; explicit values override.
type QualityConfig struct {
	MinSSIM        float64
	MinPSNR        float64
	MaxChangeRatio float64
	MaxRetries     int
}

// DetectionConfig limits the search space for Detect. Zero values pick safe
// defaults: native + 0.75x + 0.5x sweep, every configured key.
type DetectionConfig struct {
	Scales   []float64
	MinTiles int
}

type Config struct {
	ActiveKey     Key
	DetectionKeys []Key
	Strength      StrengthProfile
	MetadataMode  MetadataMode
	Quality       QualityConfig
	Detection     DetectionConfig
}

func (c *Config) validate(forEmbed bool) error {
	if forEmbed {
		if c.ActiveKey.ID == "" || len(c.ActiveKey.Secret) == 0 {
			return fmt.Errorf("%w: ActiveKey.ID and ActiveKey.Secret are required", ErrInvalidConfig)
		}
		if len(c.ActiveKey.ID) > 16 {
			return fmt.Errorf("%w: ActiveKey.ID must be at most 16 bytes", ErrInvalidConfig)
		}
	}
	if len(c.detectionKeys()) == 0 {
		return ErrNoDetectionKeys
	}
	switch c.Strength {
	case "", StrengthInvisible, StrengthBalanced, StrengthRobust:
	default:
		return fmt.Errorf("%w: unknown strength %q", ErrInvalidConfig, c.Strength)
	}
	return nil
}

// detectionKeys returns DetectionKeys augmented with ActiveKey when the
// active key is set and not already present. Detection always tries the
// active key.
func (c *Config) detectionKeys() []Key {
	if c.ActiveKey.ID == "" {
		return c.DetectionKeys
	}
	for _, k := range c.DetectionKeys {
		if k.ID == c.ActiveKey.ID {
			return c.DetectionKeys
		}
	}
	out := make([]Key, 0, len(c.DetectionKeys)+1)
	out = append(out, c.ActiveKey)
	out = append(out, c.DetectionKeys...)
	return out
}

// strengthProfile resolves the configured profile, falling back to invisible.
func (c *Config) strengthProfile() StrengthProfile {
	if c.Strength == "" {
		return StrengthInvisible
	}
	return c.Strength
}

// strengthDelta is the coefficient-pair bias the embedder applies. Larger
// values mean stronger detection at the cost of higher visual risk.
func strengthDelta(p StrengthProfile) float64 {
	switch p {
	case StrengthBalanced:
		return 6.0
	case StrengthRobust:
		return 9.0
	}
	return 4.0
}

// qualityGates returns effective gates for the profile, honouring overrides.
func qualityGates(p StrengthProfile, q QualityConfig) QualityConfig {
	out := q
	if out.MinSSIM == 0 {
		switch p {
		case StrengthBalanced:
			out.MinSSIM = 0.997
		case StrengthRobust:
			out.MinSSIM = 0.993
		default:
			out.MinSSIM = 0.999
		}
	}
	if out.MinPSNR == 0 {
		switch p {
		case StrengthBalanced:
			out.MinPSNR = 46
		case StrengthRobust:
			out.MinPSNR = 42
		default:
			out.MinPSNR = 50
		}
	}
	if out.MaxChangeRatio == 0 {
		out.MaxChangeRatio = 0.6
	}
	if out.MaxRetries == 0 {
		out.MaxRetries = 2
	}
	return out
}

// detectionScales returns the candidate downscale factors for Detect. The
// detector also always tries 1.0 (native).
func (c *Config) detectionScales() []float64 {
	if len(c.Detection.Scales) > 0 {
		return c.Detection.Scales
	}
	return []float64{1.0, 0.75, 0.5}
}
