// SPDX-License-Identifier: AGPL-3.0-only

// Copyright (C) 2026 MTN Media Group.

package verum

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"sort"

	"github.com/MTN-Media-Group/mtn-verum/internal/codec"
	"github.com/MTN-Media-Group/mtn-verum/internal/dct"
	"github.com/MTN-Media-Group/mtn-verum/internal/ecc"
	"github.com/MTN-Media-Group/mtn-verum/internal/tiles"
)

const frameBits = ecc.FrameSize * 8                           // reason: derived from RS frame size; bits per frame for embed/detect bit accounting.
const lowNoiseFloor = 0.088                                   // reason: calibration corpus false-signal 5th percentile; below this is noise.
const highSignalFloor = 0.141                                 // reason: calibration corpus true-signal 50th percentile; at this is genuine.
const detectionPossibleThreshold = 0.65                       // reason: corpus bound separating noise from possible detections.
const detectionDetectedThreshold = 0.85                       // reason: corpus confirmed bound had no false positives.
const minDetectSupportingTiles = 4                            // reason: asymmetric tile model needs multiple agreeing tiles.
const tileMagnitudeGate = 0.05                                // reason: corpus unmarked-tile p95 rejects noise-only votes.
const erasureMedianFactor = 0.43                              // reason: model erases bytes below factor*median bit confidence as corrupted.
const bitConfidenceBucketCount = 5                            // reason: stable observability buckets exposed via DetectResult.Details bit_confidence_bucket_0..4 keys.
const maxCropTilePeriods = 2                                  // reason: matches the crop survival capability claim of up to 2 tile periods.
const cropFallbackBudget = maxCropTilePeriods * LargeTileSize // reason: crop survival claim covers two 128px tile periods at the adaptive threshold.

var defaultDetectionScales = []float64{0.75, 0.5} // reason: match bilinear and nearest-neighbor downscale survival claims.

func detect(ctx context.Context, data []byte, _ string, cfg Config) (*DetectResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := cfg.validate(false); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	img, _, err := codec.Decode(data)
	if err != nil {
		return nil, wrapCodecDecodeError(err)
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if min(w, h) < MinImageDim {
		return nil, ErrImageTooSmall
	}

	tileSize := tileSizeForDimensions(w, h)
	rgba, err := toRGBA(ctx, img)
	if err != nil {
		return nil, err
	}
	plane, _, _, err := splitYCbCr(ctx, rgba)
	if err != nil {
		return nil, err
	}
	res, err := detectFromY(ctx, plane, tileSize, cfg.detectionKeys(), cfg.Detection)
	if err != nil {
		return nil, err
	}
	if tileSize == DefaultTileSize && !res.Detected && max(w+cropFallbackBudget, h+cropFallbackBudget) >= adaptiveTileThreshold {
		fallback, err := detectFromY(ctx, plane, LargeTileSize, cfg.detectionKeys(), cfg.Detection)
		if err != nil {
			return nil, err
		}
		if betterDetectResult(fallback, res) {
			return fallback, nil
		}
	}
	return res, nil
}

func detectFromY(ctx context.Context, y *tiles.Plane, tileSize int, keys []Key, dc DetectionConfig) (*DetectResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sync, err := estimateSync(ctx, y)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	best, err := detectOnPlane(ctx, y, tileSize, keys, dc, 1, 0, 0, sync.strength)
	if err != nil {
		return nil, err
	}
	for _, crop := range topLeftCropCandidates(y, tileSize) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if r, err := detectOnPlane(ctx, y, tileSize, keys, dc, 1, crop.x, crop.y, sync.strength); err != nil {
			return nil, err
		} else if r != nil && r.Confidence > best.Confidence {
			best = r
		}
	}
	if best.Detected {
		return best, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if resamplePlaneFitsLimit(y, sync.scale) {
		work, err := resamplePlane(ctx, y, sync.scale)
		if err != nil {
			return nil, err
		}
		workTileSize := tileSizeForDimensions(work.W, work.H)
		cropX := sync.cropX / sync.scale
		cropY := sync.cropY / sync.scale
		if r, err := detectOnPlane(ctx, work, workTileSize, keys, dc, sync.scale, cropX, cropY, sync.strength); err != nil {
			return nil, err
		} else if r != nil && r.Confidence > best.Confidence {
			best = r
		}
	}
	if best.Detected {
		return best, nil
	}
	for _, scale := range detectionScales(dc.Scales) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !resamplePlaneFitsLimit(y, scale) {
			continue
		}
		work, err := resamplePlane(ctx, y, scale)
		if err != nil {
			return nil, err
		}
		workTileSize := tileSizeForDimensions(work.W, work.H)
		if r, err := detectOnPlane(ctx, work, workTileSize, keys, dc, scale, 0, 0, sync.strength); err != nil {
			return nil, err
		} else if r != nil && r.Confidence > best.Confidence {
			best = r
		}
	}
	return best, nil
}

func detectionScales(scales []float64) []float64 {
	merged := make([]float64, 0, len(defaultDetectionScales)+len(scales))
	merged = append(merged, defaultDetectionScales...)
	merged = append(merged, scales...)
	sort.Float64s(merged)
	out := merged[:0]
	for _, scale := range merged {
		if len(out) == 0 || scale != out[len(out)-1] {
			out = append(out, scale)
		}
	}
	return out
}

type cropCandidate struct {
	x float64
	y float64
}

func topLeftCropCandidates(y *tiles.Plane, tileSize int) []cropCandidate {
	baseX := moduloCropOffset(y.W, tileSize)
	baseY := moduloCropOffset(y.H, tileSize)
	out := make([]cropCandidate, 0, (maxCropTilePeriods+1)*(maxCropTilePeriods+1))
	seen := make(map[cropCandidate]bool)
	for ty := 0; ty <= maxCropTilePeriods; ty++ {
		for tx := 0; tx <= maxCropTilePeriods; tx++ {
			c := cropCandidate{
				x: baseX + float64(tx*tileSize),
				y: baseY + float64(ty*tileSize),
			}
			if (c.x == 0 && c.y == 0) || seen[c] {
				continue
			}
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

func moduloCropOffset(n, tileSize int) float64 {
	rem := n % tileSize
	if rem == 0 {
		return 0
	}
	return float64(tileSize - rem)
}

func detectOnPlane(ctx context.Context, y *tiles.Plane, tileSize int, keys []Key, dc DetectionConfig, scale, cropX, cropY, syncStrength float64) (*DetectResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	all, err := shiftedTiles(ctx, y, tileSize, cropX, cropY)
	if err != nil {
		return nil, err
	}
	pairsPerBlock := pairsPerSubBlock(tileSize, frameBits)
	idxs := alphaGatedTiles(all)
	if len(idxs) == 0 {
		return emptyDetectionResultWithContext(scale, cropX, cropY, syncStrength), nil
	}

	best := emptyDetectionResultWithContext(scale, cropX, cropY, syncStrength)
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if r, err := detectKey(ctx, y, all, idxs, pairsPerBlock, key, dc); err != nil {
			return nil, err
		} else if r != nil && betterDetectResult(r, best) {
			r.BestScale = scale
			r.CropEstimate = max(cropX, cropY)
			best = detectionResultWithContext(r, scale, cropX, cropY, syncStrength)
		}
	}
	return best, nil
}

func emptyDetectionResultWithContext(scale, cropX, cropY, syncStrength float64) *DetectResult {
	return detectionResultWithContext(&DetectResult{Details: detectionDetails(nil, 0, 0)}, scale, cropX, cropY, syncStrength)
}

func detectionResultWithContext(r *DetectResult, scale, cropX, cropY, syncStrength float64) *DetectResult {
	r.Details["sync_peak_strength"] = syncStrength
	r.Details["scale_estimate"] = scale
	r.Details["crop_x_pixels"] = cropX
	r.Details["crop_y_pixels"] = cropY
	r.Details["tiles_checked"] = float64(r.TilesChecked)
	return r
}

func betterDetectResult(candidate, current *DetectResult) bool {
	if candidate.Confidence > current.Confidence {
		return true
	}
	return current.TilesChecked == 0 && candidate.TilesChecked > 0
}

func detectKey(ctx context.Context, y *tiles.Plane, all []tiles.Tile, idxs []int, pairsPerBlock int, key Key, dc DetectionConfig) (*DetectResult, error) {
	best := &DetectResult{}
	for _, positions := range detectionPositionSets() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if r, err := detectKeyWithPositions(ctx, y, all, idxs, pairsPerBlock, key, dc, positions); err != nil {
			return nil, err
		} else if r != nil && betterDetectResult(r, best) {
			best = r
		}
	}
	return best, nil
}

func detectKeyWithPositions(ctx context.Context, y *tiles.Plane, all []tiles.Tile, idxs []int, pairsPerBlock int, key Key, dc DetectionConfig, positions [][2]int) (*DetectResult, error) {
	best, err := detectKeyOnIdxs(ctx, y, all, idxs, pairsPerBlock, key, dc, positions)
	if err != nil {
		return nil, err
	}
	if len(idxs) > minEmbedTiles {
		selected := selectDetectTiles(all, idxs, minEmbedTilesFor(StrengthBalanced, len(all)))
		if r, err := detectKeyOnIdxs(ctx, y, all, selected, pairsPerBlock, key, dc, positions); err != nil {
			return nil, err
		} else if r != nil && betterDetectResult(r, best) {
			best = r
		}
	}
	return best, nil
}

func detectKeyOnIdxs(ctx context.Context, y *tiles.Plane, all []tiles.Tile, idxs []int, pairsPerBlock int, key Key, dc DetectionConfig, positions [][2]int) (*DetectResult, error) {
	expectedKeyID := keyIDBytes(key.ID)
	acc := make([]float64, frameBits)
	tileMag := make([]float64, len(idxs))
	tileVotes := make([][]float64, len(idxs))
	supporting := 0
	if err := parallelTiles(ctx, len(idxs), func(n int) {
		ti := idxs[n]
		votes := recoverTileVotes(y.Pixels, y.W, &all[ti], pairsPerBlock, key, positions)
		var mag float64
		for _, v := range votes {
			mag += math.Abs(v)
		}
		tileMag[n] = mag
		tileVotes[n] = votes
	}); err != nil {
		return nil, err
	}
	for n, votes := range tileVotes {
		if n%ctxPollInterval == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		// 5% mean per-bit magnitude; below this the tile contributes pure noise.
		if tileMag[n] < float64(len(votes))*tileMagnitudeGate {
			continue
		}
		supporting++
		for j, v := range votes {
			acc[j%len(acc)] += v
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
	bitConfidence := make([]float64, frameBits)
	for i, v := range acc {
		if supporting > 0 {
			bitConfidence[i] = math.Abs(v) / float64(supporting)
		}
	}
	medianBitConfidence := medianPositive(bitConfidence)
	decodeConfidence := append([]float64(nil), bitConfidence...)
	// 43% of median; bytes well below the median are likely corrupted.
	erasureThreshold := medianBitConfidence * erasureMedianFactor
	for i := 0; i < ecc.FrameSize; i++ {
		var sum float64
		for b := 0; b < 8; b++ {
			sum += bitConfidence[i*8+b]
		}
		if medianBitConfidence > 0 && sum/8 < erasureThreshold {
			for b := 0; b < 8; b++ {
				decodeConfidence[i*8+b] = 0
			}
		}
	}

	frame := ecc.BytesOf(bits)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ver, keyID, payload, ok := ecc.DecodeWithBitConfidence(frame, decodeConfidence)
	if !ok || ver != PayloadVersion || !bytes.Equal(keyID[:], expectedKeyID[:]) {
		return &DetectResult{
			TilesUsed:    supporting,
			TilesChecked: len(idxs),
			Details:      detectionDetails(bitConfidence, supporting, len(idxs)),
		}, nil
	}

	conf := scoreConfidence(totalMag, supporting)
	minTiles := minDetectSupportingTiles
	if dc.MinTiles > minTiles {
		minTiles = dc.MinTiles
	}
	if supporting < minTiles {
		conf = 0
	}
	return &DetectResult{
		Detected:      conf >= detectionDetectedThreshold,
		Possible:      conf >= detectionPossibleThreshold,
		Confidence:    conf,
		KeyID:         key.ID,
		Version:       int(ver),
		PayloadDigest: hex.EncodeToString(payload),
		TilesUsed:     supporting,
		TilesChecked:  len(idxs),
		Details:       detectionDetails(bitConfidence, supporting, len(idxs)),
	}, nil
}

func selectDetectTiles(all []tiles.Tile, idxs []int, n int) []int {
	cp := append([]int(nil), idxs...)
	sort.SliceStable(cp, func(i, j int) bool {
		a, b := all[cp[i]], all[cp[j]]
		if a.Score == b.Score {
			return a.Index < b.Index
		}
		return a.Score > b.Score
	})
	if len(cp) > n {
		cp = cp[:n]
	}
	return cp
}

func alphaGatedTiles(all []tiles.Tile) []int {
	idxs := make([]int, 0, len(all))
	for i, t := range all {
		if t.Alpha >= tiles.MinAlpha {
			idxs = append(idxs, i)
		}
	}
	return idxs
}

func shiftedTiles(ctx context.Context, p *tiles.Plane, tileSize int, cropX, cropY float64) ([]tiles.Tile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	startX := gridStart(cropX, tileSize)
	startY := gridStart(cropY, tileSize)
	origCols := max(1, int((float64(p.W)+cropX)/float64(tileSize)))
	out := make([]tiles.Tile, 0, (p.W/tileSize)*(p.H/tileSize))
	for y := startY; y+tileSize <= p.H; y += tileSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for x := startX; x+tileSize <= p.W; x += tileSize {
			ox := int(math.Round((cropX + float64(x)) / float64(tileSize)))
			oy := int(math.Round((cropY + float64(y)) / float64(tileSize)))
			out = append(out, tiles.Tile{
				Index: oy*origCols + ox,
				X:     x,
				Y:     y,
				Size:  tileSize,
				Score: tiles.Score(p, x, y, tileSize),
				Alpha: tileAlpha(p, x, y, tileSize),
			})
		}
	}
	if len(out) == 0 {
		return shiftedBaseTiles(ctx, p, tileSize)
	}
	return out, nil
}

func shiftedBaseTiles(ctx context.Context, p *tiles.Plane, tileSize int) ([]tiles.Tile, error) {
	cols, rows := p.W/tileSize, p.H/tileSize
	out := make([]tiles.Tile, 0, rows*cols)
	for ty := 0; ty < rows; ty++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for tx := 0; tx < cols; tx++ {
			x := tx * tileSize
			y := ty * tileSize
			out = append(out, tiles.Tile{
				Index: len(out),
				X:     x,
				Y:     y,
				Size:  tileSize,
				Score: tiles.Score(p, x, y, tileSize),
				Alpha: tileAlpha(p, x, y, tileSize),
			})
		}
	}
	return out, nil
}

func tileAlpha(p *tiles.Plane, x0, y0, size int) float64 {
	if p.Alpha == nil {
		return 1
	}
	var sum float64
	for y := y0; y < y0+size; y++ {
		row := y * p.W
		for x := x0; x < x0+size; x++ {
			sum += p.Alpha[row+x]
		}
	}
	return sum / float64(size*size)
}

func gridStart(crop float64, tileSize int) int {
	m := math.Mod(crop, float64(tileSize))
	if m < 1 || m > float64(tileSize)-1 {
		return 0
	}
	return int(math.Round(float64(tileSize) - m))
}

func detectionDetails(conf []float64, supporting, checked int) map[string]float64 {
	d := map[string]float64{
		"supporting_tiles_ratio": 0,
		"tiles_checked":          float64(checked),
	}
	if checked > 0 {
		d["supporting_tiles_ratio"] = float64(supporting) / float64(checked)
	}
	for _, v := range conf {
		bucket := min(bitConfidenceBucketCount-1, int(v*bitConfidenceBucketCount))
		d[fmt.Sprintf("bit_confidence_bucket_%d", bucket)]++
	}
	for i := 0; i < bitConfidenceBucketCount; i++ {
		k := fmt.Sprintf("bit_confidence_bucket_%d", i)
		d[k] /= float64(max(1, len(conf)))
	}
	return d
}

func medianPositive(values []float64) float64 {
	tmp := make([]float64, 0, len(values))
	for _, v := range values {
		if v > 0 {
			tmp = append(tmp, v)
		}
	}
	if len(tmp) == 0 {
		return 0
	}
	sort.Float64s(tmp)
	return tmp[len(tmp)/2]
}

func recoverTileVotes(pixels []float64, w int, t *tiles.Tile, pairsPerBlock int, key Key, positions [][2]int) []float64 {
	subN := t.Size / dct.N
	rawBits := pairsPerBlock * subN * subN
	out := make([]float64, usableBitsPerTile(t.Size, pairsPerBlock, frameBits))
	for sr := 0; sr < subN; sr++ {
		for sc := 0; sc < subN; sc++ {
			subIdx := sr*subN + sc
			origin := (t.Y+sr*dct.N)*w + (t.X + sc*dct.N)
			var block [dct.N * dct.N]float64
			loadBlock(pixels, w, origin, &block)
			dct.Forward(&block)
			for j, pr := range derivePairs(key, t.Index, subIdx, pairsPerBlock, positions) {
				bitIdx, ok := tileBitIndex(subIdx*pairsPerBlock+j, rawBits, len(out))
				if !ok {
					continue
				}
				a, c := pr[0][0]*dct.N+pr[0][1], pr[1][0]*dct.N+pr[1][1]
				ca, cb := block[a], block[c]
				denom := math.Abs(ca) + math.Abs(cb)
				if denom >= 1 {
					out[bitIdx] = (ca - cb) / denom
				}
			}
		}
	}
	return out
}

func detectionPositionSets() [][][2]int {
	return [][][2]int{qualityFreqPositions, robustFreqPositions}
}

func scoreConfidence(totalMag float64, supporting int) float64 {
	if supporting == 0 {
		return 0
	}
	avg := totalMag / float64(frameBits*supporting)
	signal := (avg - lowNoiseFloor) / (highSignalFloor - lowNoiseFloor)
	switch {
	case signal < 0:
		signal = 0
	case signal > 1:
		signal = 1
	}
	return signal
}
