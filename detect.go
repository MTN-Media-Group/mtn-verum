// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 MTN Media Group.

package verum

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"math"

	"github.com/MTN-Media-Group/mtn-verum/internal/codec"
	"github.com/MTN-Media-Group/mtn-verum/internal/dct"
	"github.com/MTN-Media-Group/mtn-verum/internal/ecc"
	"github.com/MTN-Media-Group/mtn-verum/internal/tiles"
)

const frameBits = ecc.FrameSize * 8
const lowNoiseFloor = 0.03

func detect(ctx context.Context, data []byte, _ string, cfg Config) (*DetectResult, error) {
	if err := cfg.validate(false); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	img, _, err := codec.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnsupportedFormat, err)
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if min(w, h) < MinImageDim {
		return nil, ErrImageTooSmall
	}

	tileSize := DefaultTileSize
	if max(w, h) >= largeImageThreshold {
		tileSize = LargeTileSize
	}
	plane, _, _ := splitYCbCr(toRGBA(img))
	return detectFromY(plane, tileSize, cfg.detectionKeys(), cfg.Detection)
}

func detectFromY(y *tiles.Plane, tileSize int, keys []Key, dc DetectionConfig) (*DetectResult, error) {
	all := tiles.Iterate(y, tileSize)
	pairsPerBlock := pairsPerSubBlock(tileSize, frameBits)
	bitsPerTile := pairsPerBlock * subBlocksPerTile(tileSize)
	idxs := selectTiles(all, frameBits, bitsPerTile)
	if len(idxs) == 0 {
		return &DetectResult{}, nil
	}

	best := &DetectResult{}
	for _, key := range keys {
		if r := detectKey(y, all, idxs, pairsPerBlock, key, dc); r != nil && r.Confidence > best.Confidence {
			best = r
		}
	}
	return best, nil
}

func detectKey(y *tiles.Plane, all []tiles.Tile, idxs []int, pairsPerBlock int, key Key, dc DetectionConfig) *DetectResult {
	expectedKeyID := keyIDBytes(key.ID)
	acc := make([]float64, frameBits)
	supporting := 0
	for _, ti := range idxs {
		votes := recoverTileVotes(y.Pixels, y.W, &all[ti], pairsPerBlock, key)
		var mag float64
		for _, v := range votes {
			mag += math.Abs(v)
		}
		if mag < float64(len(votes))*0.05 {
			continue
		}
		supporting++
		for j := range acc {
			acc[j] += votes[j%len(votes)]
		}
	}

	bits := make([]uint8, frameBits)
	var totalMag float64
	for i, v := range acc {
		if v >= 0 {
			bits[i] = 1
		}
		totalMag += math.Abs(v)
	}

	ver, keyID, payload, ok := ecc.Unframe(ecc.BytesOf(bits))
	if !ok || ver != PayloadVersion || !bytes.Equal(keyID[:], expectedKeyID[:]) {
		return nil
	}

	conf := scoreConfidence(totalMag, supporting, len(idxs))
	if dc.MinTiles > 0 && supporting < dc.MinTiles {
		conf = 0
	}
	return &DetectResult{
		Detected:      conf >= 0.85,
		Possible:      conf >= 0.65,
		Confidence:    conf,
		KeyID:         key.ID,
		Version:       int(ver),
		PayloadDigest: hex.EncodeToString(payload),
		TilesUsed:     supporting,
		TilesChecked:  len(idxs),
	}
}

func recoverTileVotes(pixels []float64, w int, t *tiles.Tile, pairsPerBlock int, key Key) []float64 {
	subN := t.Size / dct.N
	out := make([]float64, pairsPerBlock*subN*subN)
	for sr := 0; sr < subN; sr++ {
		for sc := 0; sc < subN; sc++ {
			subIdx := sr*subN + sc
			origin := (t.Y+sr*dct.N)*w + (t.X + sc*dct.N)
			var block [dct.N * dct.N]float64
			loadBlock(pixels, w, origin, &block)
			dct.Forward(&block)
			for j, pr := range derivePairs(key, t.Index, subIdx, pairsPerBlock) {
				a, c := pr[0][0]*dct.N+pr[0][1], pr[1][0]*dct.N+pr[1][1]
				ca, cb := block[a], block[c]
				denom := math.Abs(ca) + math.Abs(cb)
				if denom >= 1 {
					out[subIdx*pairsPerBlock+j] = (ca - cb) / denom
				}
			}
		}
	}
	return out
}

func scoreConfidence(totalMag float64, supporting, totalTiles int) float64 {
	if supporting == 0 {
		return 0
	}
	avg := totalMag / float64(frameBits*supporting)
	cov := float64(supporting) / float64(totalTiles)
	signal := (avg - lowNoiseFloor) / (1 - lowNoiseFloor)
	switch {
	case signal < 0:
		signal = 0
	case signal > 1:
		signal = 1
	}
	if cov > 1 {
		cov = 1
	}
	return signal * cov
}
