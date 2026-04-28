// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 MTN Media Group.

package verum

import (
	"context"
	"fmt"
	"image"
	"math"

	"golang.org/x/image/draw"

	"github.com/MTN-Media-Group/mtn-verum/internal/codec"
	"github.com/MTN-Media-Group/mtn-verum/internal/dct"
	"github.com/MTN-Media-Group/mtn-verum/internal/ecc"
	"github.com/MTN-Media-Group/mtn-verum/internal/tiles"
)

// frameBits is the wire length of a v1 frame: 1 sync + 1 version + 1 keyID +
// 1 uvarint length (digest is always 32B so length encodes in one byte) +
// 32 digest + 4 crc = 40 bytes.
const frameBits = 40 * 8

func detect(ctx context.Context, data []byte, _ string, cfg Config) (*DetectResult, error) {
	if err := cfg.validate(false); err != nil {
		return nil, err
	}
	img, _, err := codec.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedFormat, err)
	}
	rect := img.Bounds()
	w, h := rect.Dx(), rect.Dy()
	if min(w, h) < MinImageDim {
		return nil, ErrImageTooSmall
	}

	keys := cfg.detectionKeys()
	scales := cfg.detectionScales()

	best := &DetectResult{}
	for _, scale := range scales {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		scaled := resample(img, scale)
		sw, sh := scaled.Bounds().Dx(), scaled.Bounds().Dy()
		if min(sw, sh) < MinImageDim {
			continue
		}
		yPlane := luminancePlane(scaled)
		tileSize := DefaultTileSize
		if max(sw, sh) >= largeImageThreshold {
			tileSize = LargeTileSize
		}
		r, err := detectFromY(yPlane, sw, sh, tileSize, keys, []float64{scale}, cfg.Detection)
		if err != nil {
			continue
		}
		if r.Confidence > best.Confidence {
			best = r
		}
	}
	return best, nil
}

func detectFromY(y *tiles.Plane, w, h, tileSize int, keys []Key, scales []float64, dc DetectionConfig) (*DetectResult, error) {
	all := tiles.Iterate(y, tileSize)
	if len(all) == 0 {
		return &DetectResult{TotalTilesChecked: 0}, nil
	}

	pairsPerBlock := pairsPerSubBlock(frameBits)
	subPerTile := subBlocksPerTile(tileSize)

	bestRes := &DetectResult{}
	for _, key := range keys {
		acc := make([]float64, frameBits)
		support := make([]int, frameBits)
		var supportingTiles int
		for ti := range all {
			t := &all[ti]
			tileVotes := recoverTileVotes(y.Pixels, w, t, pairsPerBlock, subPerTile, key)
			tileMag := 0.0
			for _, v := range tileVotes {
				if v < 0 {
					tileMag -= v
				} else {
					tileMag += v
				}
			}
			if tileMag < float64(len(tileVotes))*0.05 {
				continue
			}
			supportingTiles++
			for j, v := range tileVotes {
				acc[j%frameBits] += v
				support[j%frameBits]++
			}
		}

		bits := make([]uint8, frameBits)
		var bitMag float64
		for i := range acc {
			if acc[i] >= 0 {
				bits[i] = 1
			}
			if v := acc[i]; v < 0 {
				bitMag -= v
			} else {
				bitMag += v
			}
		}
		frame := ecc.BytesOf(bits)
		ver, keyID, payload, ok := ecc.Unframe(frame)
		if !ok || keyID != keyIDByte(key.ID) || int(ver) != PayloadVersion {
			continue
		}

		conf := scoreConfidence(bitMag, supportingTiles, len(all))
		if conf <= bestRes.Confidence {
			continue
		}
		bestRes = &DetectResult{
			Detected:          conf >= 0.85,
			Possible:          conf >= 0.65,
			Confidence:        conf,
			KeyID:             key.ID,
			Version:           int(ver),
			PayloadDigest:     hexDigest(payload),
			RecoveredBits:     supportingTiles * frameBits,
			TotalBits:         len(all) * frameBits,
			SupportingTiles:   supportingTiles,
			TotalTilesChecked: len(all),
			BestScale:         scales[0],
		}
		if minTiles := dc.MinTiles; minTiles > 0 && supportingTiles < minTiles {
			bestRes.Detected = false
		}
	}
	return bestRes, nil
}

func recoverTileVotes(pixels []float64, w int, t *tiles.Tile, pairsPerBlock, subPerTile int, key Key) []float64 {
	out := make([]float64, pairsPerBlock*subPerTile)
	subCols := t.Size / dct.N
	subRows := t.Size / dct.N
	for sr := 0; sr < subRows; sr++ {
		for sc := 0; sc < subCols; sc++ {
			subIdx := sr*subCols + sc
			origin := (t.Y+sr*dct.N)*w + (t.X + sc*dct.N)
			var block [dct.N * dct.N]float64
			loadBlock(pixels, w, origin, &block)
			dct.Forward(&block)
			pairs := derivePairs(key.Secret, t.Index, subIdx, pairsPerBlock)
			for j, pr := range pairs {
				a := pr[0][0]*dct.N + pr[0][1]
				c := pr[1][0]*dct.N + pr[1][1]
				ca := block[a]
				cb := block[c]
				denom := math.Abs(ca) + math.Abs(cb)
				if denom < 1 {
					out[subIdx*pairsPerBlock+j] = 0
					continue
				}
				out[subIdx*pairsPerBlock+j] = (ca - cb) / denom
			}
		}
	}
	return out
}

func scoreConfidence(bitMag float64, supportingTiles, totalTiles int) float64 {
	if totalTiles == 0 {
		return 0
	}
	avg := bitMag / float64(frameBits)          // per-bit average soft magnitude in [0, 1]
	cov := float64(supportingTiles) / float64(totalTiles)
	if cov > 1 {
		cov = 1
	}
	// CRC has already passed if we reach here — that alone is ~32 bits of
	// integrity, so we start above the "possible" band and let bit-level
	// certainty plus tile coverage push us into "detected".
	return clamp(0.65+0.25*avg+0.10*cov, 0, 1)
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func resample(src image.Image, scale float64) image.Image {
	if scale == 1.0 {
		return src
	}
	b := src.Bounds()
	w := int(math.Round(float64(b.Dx()) * scale))
	h := int(math.Round(float64(b.Dy()) * scale))
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	return dst
}

func luminancePlane(src image.Image) *tiles.Plane {
	rgba := toRGBA(src)
	y, _, _, _ := splitYCbCrA(rgba)
	return y
}
