// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 MTN Media Group.

package verum

import "fmt"

const (
	MinImageDim         = 256
	DefaultTileSize     = 64
	LargeTileSize       = 96
	largeImageThreshold = 2048
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

type MetadataMode string

const (
	MetadataNone     MetadataMode = "none"
	MetadataStandard MetadataMode = "standard"
)

// QualityConfig zero values fall back to per-profile defaults.
type QualityConfig struct {
	MinSSIM        float64
	MinPSNR        float64
	MaxDelta       float64
	MaxChangeRatio float64
	MaxRetries     int
}

// DetectionConfig.Scales is currently ignored (v1 detects at native scale only); values must be in (0, 2] when set.
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
	for _, scale := range c.Detection.Scales {
		if scale <= 0 || scale > 2 {
			return fmt.Errorf("%w: detection scale must be > 0 and <= 2", ErrInvalidConfig)
		}
	}
	return nil
}

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

func (c *Config) strengthProfile() StrengthProfile {
	if c.Strength == "" {
		return StrengthInvisible
	}
	return c.Strength
}

type profileDefaults struct {
	delta, minSSIM, minPSNR, maxDelta float64
}

var profileTable = map[StrengthProfile]profileDefaults{
	StrengthInvisible: {delta: 4, minSSIM: 0.999, minPSNR: 50, maxDelta: 12},
	StrengthBalanced:  {delta: 6, minSSIM: 0.997, minPSNR: 46, maxDelta: 18},
	StrengthRobust:    {delta: 9, minSSIM: 0.993, minPSNR: 42, maxDelta: 24},
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
		q.MaxChangeRatio = 0.6
	}
	if q.MaxRetries == 0 {
		q.MaxRetries = 2
	}
	return q
}
