// SPDX-License-Identifier: AGPL-3.0-only

// Copyright (C) 2026 MTN Media Group.

package verum

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"runtime"
	"sync"

	"github.com/MTN-Media-Group/mtn-verum/internal/codec"
	"github.com/MTN-Media-Group/mtn-verum/internal/dct"
	"github.com/MTN-Media-Group/mtn-verum/internal/ecc"
	"github.com/MTN-Media-Group/mtn-verum/internal/quality"
	"github.com/MTN-Media-Group/mtn-verum/internal/tiles"
)

var robustFreqPositions = [][2]int{ // reason: low/mid-frequency 8x8 DCT positions for Robust profile, widened to eight pairs for the 480-bit frame.
	{0, 1}, {1, 0}, {1, 1}, {0, 2}, {2, 0}, {1, 2}, {2, 1},
	{2, 2}, {0, 3}, {3, 0}, {1, 3}, {3, 1}, {2, 3}, {3, 2},
	{0, 4}, {4, 0},
}

var qualityFreqPositions = [][2]int{ // reason: low/mid-frequency 8x8 DCT positions used by Invisible/Balanced profiles, widened to eight pairs for the 480-bit frame.
	{1, 2}, {2, 1}, {1, 3}, {3, 1}, {2, 2},
	{0, 4}, {4, 0}, {1, 4}, {4, 1}, {2, 3}, {3, 2},
	{1, 5}, {5, 1}, {2, 4}, {4, 2}, {3, 3},
}

const transparentAlphaThreshold = 5.0 / 255.0                                // reason: alpha below the 8-bit visibility floor should not receive luma changes.
const transparentAlphaByteThreshold = uint8(transparentAlphaThreshold * 255) // reason: byte-domain preservation must match the luma skip threshold.
const postEncodeRetryScale = 0.95                                            // reason: PNG/WebP retries need small attenuation to preserve robust downscale survival.
const jpegPostEncodeRetryScale = 0.90                                        // reason: JPEG Q95 needs attenuation below v2 robust strength before half-strength retries.
const deltaRetryStep = 0.5                                                   // reason: each retry halves the strength delta to converge on the smallest mark passing quality gates.
const changedPixelDeltaThreshold = 1.0                                       // reason: pixels differing by more than 1 unit count toward the changed-pixel ratio.
const tileBudgetDivisor = 64                                                 // reason: scales required-tile minimum with image size, matching DefaultTileSize.
const adaptiveTileThreshold = 2048                                           // reason: above this longest side, 64px tiles produce too many micro-tiles to mask cleanly; switch to 128px tiles.
const ctxPollInterval = 16                                                   // reason: poll context every 16 inner-loop iterations to balance cancellation latency against per-iteration overhead.

func embed(ctx context.Context, data []byte, mimeType string, payload Payload, cfg Config) (*EmbedResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := cfg.validate(true); err != nil {
		return nil, err
	}
	outFormat := codec.Format(mimeType)
	if mimeType != "" && !supportedOutputFormat(outFormat) {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedFormat, mimeType)
	}
	digest, err := computeDigest(&payload, cfg.ActiveKey.Secret)
	if err != nil {
		return nil, err
	}
	frame := ecc.Frame(byte(PayloadVersion), keyIDBytes(cfg.ActiveKey.ID), digest)
	bits := ecc.BitsOf(frame)

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	img, srcFormat, err := codec.Decode(data)
	if err != nil {
		return nil, wrapCodecDecodeError(err)
	}
	if outFormat == "" {
		outFormat = srcFormat
	}
	rect := img.Bounds()
	w, h := rect.Dx(), rect.Dy()
	if min(w, h) < MinImageDim {
		return nil, ErrImageTooSmall
	}

	rgba, err := toRGBA(ctx, img)
	if err != nil {
		return nil, err
	}
	if outFormat == codec.FormatJPEG {
		hasAlpha, err := hasNonOpaqueAlpha(ctx, rgba)
		if err != nil {
			return nil, err
		}
		if hasAlpha {
			return nil, fmt.Errorf("%w: JPEG output does not support alpha; re-encode to PNG/WebP or composite against an opaque background first", ErrUnsupportedFormat)
		}
	}
	plane, cb, cr, err := splitYCbCr(ctx, rgba)
	if err != nil {
		return nil, err
	}

	tileSize := tileSizeForDimensions(w, h)

	profile := cfg.strengthProfile()
	original, err := clonePlanePixels(ctx, plane)
	if err != nil {
		return nil, err
	}
	gates := qualityGates(profile, cfg.Quality)

	jpegQuality := cfg.JPEGQuality
	if jpegQuality == 0 {
		jpegQuality = codec.DefaultJPEGQuality
	}
	if outFormat == codec.FormatJPEG && jpegQuality < codec.DefaultJPEGQuality {
		return nil, fmt.Errorf("%w: JPEG quality below %d is not supported in the current release", ErrInvalidConfig, codec.DefaultJPEGQuality)
	}
	retryScale := postEncodeRetryScale
	if outFormat == codec.FormatJPEG {
		retryScale = jpegPostEncodeRetryScale
	}

	var (
		report       QualityReport
		finalQuality QualityReport
		finalRatio   float64
		self         *DetectResult
		encoded      []byte
		resultErr    error = ErrQualityGateFailed
	)
	tryDelta := func(delta float64) bool {
		if err := restorePlanePixels(ctx, plane, original); err != nil {
			resultErr = err
			return false
		}
		report, _, resultErr = embedAttempt(ctx, plane, original, tileSize, bits, cfg.ActiveKey, profile, gates, delta)
		if resultErr != nil {
			return false
		}

		if err := ctx.Err(); err != nil {
			resultErr = err
			return false
		}
		if err := mergeYCbCr(ctx, rgba, plane, cb, cr); err != nil {
			resultErr = err
			return false
		}

		outImage := image.Image(rgba)
		if err := ctx.Err(); err != nil {
			resultErr = err
			return false
		}
		if outFormat == codec.FormatPNG || outFormat == codec.FormatWebP {
			outImage, resultErr = rgbaToNRGBA(ctx, rgba)
			if resultErr != nil {
				return false
			}
		}
		if err := ctx.Err(); err != nil {
			resultErr = err
			return false
		}
		encoded, resultErr = codec.Encode(outImage, outFormat, codec.EncodeOptions{JPEGQuality: jpegQuality})
		if resultErr != nil {
			resultErr = fmt.Errorf("%w: %w", ErrUnsupportedFormat, resultErr)
			return false
		}

		if err := ctx.Err(); err != nil {
			resultErr = err
			return false
		}
		finalQuality, finalRatio, resultErr = qualityReportFromEncoded(ctx, encoded, original, report.Tiles)
		if resultErr != nil {
			return false
		}
		if qualityGateFailed(finalQuality, finalRatio, gates) {
			resultErr = qualityGateError(finalQuality, finalRatio, gates, "post-encode ")
			return false
		}

		if err := ctx.Err(); err != nil {
			resultErr = err
			return false
		}
		self, resultErr = selfDetect(ctx, encoded, tileSize, cfg)
		if resultErr != nil {
			if isContextError(ctx, resultErr) {
				return false
			}
			resultErr = fmt.Errorf("%w: %w", ErrSelfDetectionFailed, resultErr)
			return false
		}
		if !self.Detected {
			resultErr = fmt.Errorf("%w: confidence=%.3f tiles=%d", ErrSelfDetectionFailed, self.Confidence, self.TilesChecked)
			return false
		}
		resultErr = nil
		return true
	}

	for attempt := 0; attempt <= gates.MaxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		delta := strengthDelta(profile) * math.Pow(deltaRetryStep, float64(attempt))
		if tryDelta(delta) {
			break
		}
		if !errors.Is(resultErr, ErrQualityGateFailed) {
			return nil, resultErr
		}
		if errors.Is(resultErr, ErrQualityGateFailed) && tryDelta(delta*retryScale) {
			break
		}
		if !errors.Is(resultErr, ErrQualityGateFailed) {
			return nil, resultErr
		}
	}
	if resultErr != nil {
		if isContextError(ctx, resultErr) {
			return nil, resultErr
		}
		return nil, resultErr
	}

	res := &EmbedResult{
		Data:              encoded,
		MimeType:          string(outFormat),
		PayloadDigest:     hex.EncodeToString(digest),
		KeyID:             cfg.ActiveKey.ID,
		Version:           PayloadVersion,
		SelfDetection:     *self,
		Quality:           finalQuality,
		ChangedPixelRatio: finalRatio,
	}
	if cfg.IncludeMetadata == IncludeMetadataStandard {
		res.Metadata = map[string]string{
			"verum-version": fmt.Sprintf("%d", PayloadVersion),
			"verum-key-id":  cfg.ActiveKey.ID,
			"verum-digest":  res.PayloadDigest,
		}
	}
	return res, nil
}

func supportedOutputFormat(format codec.Format) bool {
	switch format {
	case codec.FormatPNG, codec.FormatJPEG, codec.FormatWebP:
		return true
	default:
		return false
	}
}

func isEmbeddable(data []byte, cfg Config) (bool, error) {
	if err := cfg.validate(true); err != nil {
		return false, err
	}
	img, _, err := codec.Decode(data)
	if err != nil {
		return false, wrapCodecDecodeError(err)
	}
	rect := img.Bounds()
	w, h := rect.Dx(), rect.Dy()
	if min(w, h) < MinImageDim {
		return false, ErrImageTooSmall
	}
	ctx := context.Background()
	rgba, err := toRGBA(ctx, img)
	if err != nil {
		return false, err
	}
	plane, _, _, err := splitYCbCr(ctx, rgba)
	if err != nil {
		return false, err
	}
	tileSize := tileSizeForDimensions(w, h)
	profile := cfg.strengthProfile()
	all := tiles.Iterate(plane, tileSize)
	pairsPerBlock := pairsPerSubBlock(tileSize, frameBits)
	bitsPerTile := usableBitsPerTile(tileSize, pairsPerBlock, frameBits)
	required := requiredEmbedTiles(frameBits, bitsPerTile, minEmbedTilesFor(profile, len(all)))
	usable := usableTileCount(all)
	if usable < required {
		return false, noCapacityError(usable, required)
	}
	return true, nil
}

func embedAttempt(ctx context.Context, y *tiles.Plane, original []float64, tileSize int, bits []uint8, key Key, profile StrengthProfile, gates QualityConfig, delta float64) (QualityReport, float64, error) {
	if err := ctx.Err(); err != nil {
		return QualityReport{}, 0, err
	}
	all := tiles.Iterate(y, tileSize)
	pairsPerBlock := pairsPerSubBlock(tileSize, len(bits))
	bitsPerTile := usableBitsPerTile(tileSize, pairsPerBlock, len(bits))
	idxs := selectTiles(all, len(bits), bitsPerTile, minEmbedTilesFor(profile, len(all)))
	if len(idxs) == 0 {
		return QualityReport{}, 0, noCapacityError(usableTileCount(all), requiredEmbedTiles(len(bits), bitsPerTile, minEmbedTilesFor(profile, len(all))))
	}

	positions := positionsForProfile(profile)
	if err := parallelTiles(ctx, len(idxs), func(n int) {
		applyTile(y, &all[idxs[n]], bits, pairsPerBlock, key, profile, delta, positions)
	}); err != nil {
		return QualityReport{}, 0, err
	}
	if err := ctx.Err(); err != nil {
		return QualityReport{}, 0, err
	}
	if err := applySyncPattern(ctx, y, profile); err != nil {
		return QualityReport{}, 0, err
	}

	if err := ctx.Err(); err != nil {
		return QualityReport{}, 0, err
	}
	ssim := quality.SSIM(original, y.Pixels, y.W, y.H)
	if err := ctx.Err(); err != nil {
		return QualityReport{}, 0, err
	}
	psnr := quality.PSNR(original, y.Pixels)
	if err := ctx.Err(); err != nil {
		return QualityReport{}, 0, err
	}
	maxDelta := quality.MaxAbsDelta(original, y.Pixels)
	if err := ctx.Err(); err != nil {
		return QualityReport{}, 0, err
	}
	ratio := quality.ChangedPixelRatio(original, y.Pixels, changedPixelDeltaThreshold)
	if err := ctx.Err(); err != nil {
		return QualityReport{}, 0, err
	}
	report := QualityReport{
		SSIM:     ssim,
		PSNR:     psnr,
		MaxDelta: maxDelta,
		Tiles:    len(idxs),
	}

	if qualityGateFailed(report, ratio, gates) {
		return report, ratio, qualityGateError(report, ratio, gates, "")
	}
	return report, ratio, nil
}

func qualityGateFailed(report QualityReport, ratio float64, gates QualityConfig) bool {
	return report.SSIM < gates.MinSSIM || report.PSNR < gates.MinPSNR || report.MaxDelta > gates.MaxDelta || ratio > gates.MaxChangeRatio
}

func qualityGateError(report QualityReport, ratio float64, gates QualityConfig, phase string) error {
	return fmt.Errorf("%w: %sssim=%.6f min=%.6f psnr=%.2f min=%.2f max_delta=%.2f limit=%.2f changed=%.3f limit=%.3f", ErrQualityGateFailed, phase, report.SSIM, gates.MinSSIM, report.PSNR, gates.MinPSNR, report.MaxDelta, gates.MaxDelta, ratio, gates.MaxChangeRatio)
}

func qualityReportFromEncoded(ctx context.Context, encoded []byte, original []float64, tiles int) (QualityReport, float64, error) {
	if err := ctx.Err(); err != nil {
		return QualityReport{}, 0, err
	}
	img, _, err := codec.Decode(encoded)
	if err != nil {
		return QualityReport{}, 0, wrapCodecDecodeError(err)
	}
	rgba, err := toRGBA(ctx, img)
	if err != nil {
		return QualityReport{}, 0, err
	}
	plane, _, _, err := splitYCbCr(ctx, rgba)
	if err != nil {
		return QualityReport{}, 0, err
	}
	if len(plane.Pixels) != len(original) {
		return QualityReport{}, 0, fmt.Errorf("%w: final image dimensions changed", ErrUnsupportedFormat)
	}
	if err := ctx.Err(); err != nil {
		return QualityReport{}, 0, err
	}
	ssim := quality.SSIM(original, plane.Pixels, plane.W, plane.H)
	if err := ctx.Err(); err != nil {
		return QualityReport{}, 0, err
	}
	psnr := quality.PSNR(original, plane.Pixels)
	if err := ctx.Err(); err != nil {
		return QualityReport{}, 0, err
	}
	maxDelta := quality.MaxAbsDelta(original, plane.Pixels)
	if err := ctx.Err(); err != nil {
		return QualityReport{}, 0, err
	}
	ratio := quality.ChangedPixelRatio(original, plane.Pixels, changedPixelDeltaThreshold)
	if err := ctx.Err(); err != nil {
		return QualityReport{}, 0, err
	}
	return QualityReport{
		SSIM:     ssim,
		PSNR:     psnr,
		MaxDelta: maxDelta,
		Tiles:    tiles,
	}, ratio, nil
}

func isContextError(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
		return true
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func selfDetect(ctx context.Context, encoded []byte, tileSize int, cfg Config) (*DetectResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	img, _, err := codec.Decode(encoded)
	if err != nil {
		return nil, err
	}
	rgba, err := toRGBA(ctx, img)
	if err != nil {
		return nil, err
	}
	plane, _, _, err := splitYCbCr(ctx, rgba)
	if err != nil {
		return nil, err
	}
	return detectFromY(ctx, plane, tileSize, []Key{cfg.ActiveKey}, cfg.Detection)
}

func tileSizeForDimensions(w, h int) int {
	if max(w, h) >= adaptiveTileThreshold {
		return LargeTileSize
	}
	return DefaultTileSize
}

func applyTile(y *tiles.Plane, t *tiles.Tile, bits []uint8, pairsPerBlock int, key Key, profile StrengthProfile, delta float64, positions [][2]int) {
	subN := t.Size / dct.N
	rawBits := pairsPerBlock * subN * subN
	usableBits := usableBitsPerTile(t.Size, pairsPerBlock, len(bits))
	for sr := 0; sr < subN; sr++ {
		for sc := 0; sc < subN; sc++ {
			subIdx := sr*subN + sc
			origin := (t.Y+sr*dct.N)*y.W + (t.X + sc*dct.N)
			var block [dct.N * dct.N]float64
			loadBlock(y.Pixels, y.W, origin, &block)
			dct.Forward(&block)
			for j, pr := range derivePairs(key, t.Index, subIdx, pairsPerBlock, positions) {
				bitIdx, ok := tileBitIndex(subIdx*pairsPerBlock+j, rawBits, usableBits)
				if !ok {
					continue
				}
				pairDelta := delta * watsonPairScale(&block, pr, profile)
				biasPair(&block, pr, bits[bitIdx%len(bits)], pairDelta)
			}
			dct.Inverse(&block)
			storeBlock(y.Pixels, y.Alpha, y.W, origin, &block)
		}
	}
}

func parallelTiles(ctx context.Context, n int, fn func(int)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	workers := min(max(1, runtime.NumCPU()), max(1, n))
	if workers == 1 {
		for i := 0; i < n; i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			fn(i)
		}
		return ctx.Err()
	}
	jobs := make(chan int, workers*2)
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					continue
				}
				fn(i)
			}
		}()
	}
	for i := 0; i < n; i++ {
		if i%ctxPollInterval == 0 {
			if err := ctx.Err(); err != nil {
				close(jobs)
				wg.Wait()
				return err
			}
		}
		select {
		case jobs <- i:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	return ctx.Err()
}

func biasPair(b *[dct.N * dct.N]float64, p [2][2]int, bit uint8, delta float64) {
	a, c := p[0][0]*dct.N+p[0][1], p[1][0]*dct.N+p[1][1]
	diff := b[a] - b[c]
	target := delta
	if bit == 0 {
		target = -delta
	}
	if (bit == 1 && diff >= target) || (bit == 0 && diff <= target) {
		return
	}
	shift := math.Copysign(min(math.Abs(target-diff)*0.5, delta), target-diff)
	b[a] += shift
	b[c] -= shift
}

func loadBlock(pixels []float64, w, origin int, b *[dct.N * dct.N]float64) {
	for r := 0; r < dct.N; r++ {
		row := pixels[origin+r*w : origin+r*w+dct.N]
		for c := 0; c < dct.N; c++ {
			b[r*dct.N+c] = row[c] - 128
		}
	}
}

func storeBlock(pixels, alpha []float64, w, origin int, b *[dct.N * dct.N]float64) {
	for r := 0; r < dct.N; r++ {
		for c := 0; c < dct.N; c++ {
			idx := origin + r*w + c
			if alpha != nil && alpha[idx] < transparentAlphaThreshold {
				continue
			}
			v := b[r*dct.N+c] + 128
			switch {
			case v < 0:
				v = 0
			case v > 255:
				v = 255
			}
			pixels[idx] = v
		}
	}
}

func derivePairs(key Key, tileIdx, subIdx, pairsPerBlock int, positions [][2]int) [][2][2]int {
	pool := make([][2]int, len(positions))
	copy(pool, positions)
	rng := newPairRNG(key, tileIdx, subIdx)
	for i := len(pool) - 1; i > 0; i-- {
		j := rng.intn(i + 1)
		pool[i], pool[j] = pool[j], pool[i]
	}
	out := make([][2][2]int, pairsPerBlock)
	for i := 0; i < pairsPerBlock; i++ {
		out[i] = [2][2]int{pool[2*i], pool[2*i+1]}
	}
	return out
}

// pairRNG expands an HMAC-SHA256 counter-mode stream keyed on (secret, keyID, tileIdx, subIdx).
type pairRNG struct {
	key     Key
	tileIdx int
	subIdx  int
	counter uint32
	buf     []byte
	pos     int
}

func newPairRNG(key Key, tileIdx, subIdx int) *pairRNG {
	r := &pairRNG{key: key, tileIdx: tileIdx, subIdx: subIdx}
	r.refill()
	return r
}

func (r *pairRNG) refill() {
	mac := hmac.New(sha256.New, r.key.Secret)
	mac.Write([]byte("verum-pairs-v2"))
	kid := keyIDBytes(r.key.ID)
	mac.Write(kid[:])
	var seed [12]byte
	binary.BigEndian.PutUint32(seed[0:4], uint32(r.tileIdx))
	binary.BigEndian.PutUint32(seed[4:8], uint32(r.subIdx))
	binary.BigEndian.PutUint32(seed[8:12], r.counter)
	mac.Write(seed[:])
	r.buf = mac.Sum(r.buf[:0])
	r.pos = 0
	r.counter++
}

func (r *pairRNG) uint32() uint32 {
	if r.pos+4 > len(r.buf) {
		r.refill()
	}
	v := binary.BigEndian.Uint32(r.buf[r.pos:])
	r.pos += 4
	return v
}

// intn returns a uniform integer in [0, n) via rejection sampling.
func (r *pairRNG) intn(n int) int {
	limit := (uint64(1) << 32) / uint64(n) * uint64(n)
	for {
		if v := uint64(r.uint32()); v < limit {
			return int(v % uint64(n))
		}
	}
}

// pairsPerSubBlock picks enough coefficient pairs to carry at least one frame copy per tile.
func pairsPerSubBlock(tileSize, frameBits int) int {
	subBlocks := subBlocksPerTile(tileSize)
	maxPairs := min(len(robustFreqPositions), len(qualityFreqPositions)) / 2
	for k := 1; k <= maxPairs; k++ {
		if bits := k * subBlocks; bits >= frameBits && bits%frameBits == 0 {
			return k
		}
	}
	return maxPairs
}

func usableBitsPerTile(tileSize, pairsPerBlock, frameBits int) int {
	raw := pairsPerBlock * subBlocksPerTile(tileSize)
	if raw < frameBits {
		return raw
	}
	return raw / frameBits * frameBits
}

func tileBitIndex(rawIdx, rawBits, usableBits int) (int, bool) {
	if rawBits == usableBits {
		return rawIdx, true
	}
	extra := rawBits - usableBits
	skippedBefore := rawIdx * extra / rawBits
	skippedThrough := (rawIdx + 1) * extra / rawBits
	if skippedThrough > skippedBefore {
		return 0, false
	}
	return rawIdx - skippedBefore, true
}

func positionsForProfile(profile StrengthProfile) [][2]int {
	if profile == StrengthRobust {
		return robustFreqPositions
	}
	return qualityFreqPositions
}

func subBlocksPerTile(tileSize int) int {
	n := tileSize / dct.N
	return n * n
}

const (
	minEmbedTiles       = 12 // reason: balanced/invisible embedding needs enough repeated votes to cover one RS frame above the bit budget.
	minRobustEmbedTiles = 14 // reason: robust embedding spends stronger per-tile energy and needs extra RS-frame vote diversity.
)

// selectTiles keeps the highest-capacity tiles in deterministic score order.
func selectTiles(all []tiles.Tile, frameBits, bitsPerTile, minTiles int) []int {
	needed := requiredEmbedTiles(frameBits, bitsPerTile, minTiles)
	idxs := tiles.SelectByScore(all, needed)
	if len(idxs) < needed {
		return nil
	}
	return idxs
}

func requiredEmbedTiles(frameBits, bitsPerTile, minTiles int) int {
	needed := (frameBits + bitsPerTile - 1) / bitsPerTile
	if needed < minTiles {
		needed = minTiles
	}
	return needed
}

func usableTileCount(all []tiles.Tile) int {
	var usable int
	for _, t := range all {
		if t.Score > 0 {
			usable++
		}
	}
	return usable
}

func noCapacityError(usable, required int) error {
	return fmt.Errorf("%w: usable_tiles=%d required_tiles=%d", ErrNoCapacity, usable, required)
}

func minEmbedTilesFor(profile StrengthProfile, totalTiles int) int {
	if profile == StrengthRobust {
		return max(minRobustEmbedTiles, totalTiles/2)
	}
	scale := max(1, totalTiles/tileBudgetDivisor)
	return minEmbedTiles * scale
}

func keyIDBytes(id string) [ecc.KeyIDSize]byte {
	var out [ecc.KeyIDSize]byte
	if id != "" {
		h := sha256.Sum256([]byte(id))
		copy(out[:], h[:])
	}
	return out
}

func toRGBA(ctx context.Context, src image.Image) (*image.RGBA, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r, ok := src.(*image.RGBA); ok {
		return r, nil
	}
	if paletted, ok := src.(*image.Paletted); ok {
		nrgba, err := palettedToNRGBA(ctx, paletted)
		if err != nil {
			return nil, err
		}
		src = nrgba
	}
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		draw.Draw(dst, image.Rect(0, y, b.Dx(), y+1), src, image.Point{X: b.Min.X, Y: b.Min.Y + y}, draw.Src)
	}
	if nrgba, ok := src.(*image.NRGBA); ok {
		if err := restoreTransparentNRGBA(ctx, dst, nrgba); err != nil {
			return nil, err
		}
	}
	return dst, nil
}

func palettedToNRGBA(ctx context.Context, src *image.Paletted) (*image.NRGBA, error) {
	const nrgbaChannels = 4 // reason: image.NRGBA stores red, green, blue, and alpha as adjacent bytes.
	b := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		srcRow := src.PixOffset(b.Min.X, b.Min.Y+y)
		dstRow := y * dst.Stride
		for x := 0; x < b.Dx(); x++ {
			c := color.NRGBAModel.Convert(src.Palette[src.Pix[srcRow+x]]).(color.NRGBA)
			dp := dst.Pix[dstRow+x*nrgbaChannels : dstRow+(x+1)*nrgbaChannels]
			dp[0], dp[1], dp[2], dp[3] = c.R, c.G, c.B, c.A
		}
	}
	return dst, nil
}

func restoreTransparentNRGBA(ctx context.Context, dst *image.RGBA, src *image.NRGBA) error {
	b := src.Bounds()
	for y := 0; y < b.Dy(); y++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		dstRow := y * dst.Stride
		srcRow := src.PixOffset(b.Min.X, b.Min.Y+y)
		for x := 0; x < b.Dx(); x++ {
			sp := src.Pix[srcRow+x*4 : srcRow+x*4+4]
			if sp[3] >= transparentAlphaByteThreshold {
				continue
			}
			dp := dst.Pix[dstRow+x*4 : dstRow+x*4+4]
			dp[0], dp[1], dp[2], dp[3] = sp[0], sp[1], sp[2], sp[3]
		}
	}
	return nil
}

func hasNonOpaqueAlpha(ctx context.Context, img *image.RGBA) (bool, error) {
	const rgbaChannels = 4       // reason: image.RGBA stores red, green, blue, and alpha as adjacent bytes.
	const alphaChannelOffset = 3 // reason: alpha is the fourth byte in image.RGBA pixel layout.
	const opaqueAlpha = 255      // reason: JPEG output can only preserve fully opaque pixels.
	b := img.Bounds()
	for y := 0; y < b.Dy(); y++ {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		row := y * img.Stride
		for x := 0; x < b.Dx(); x++ {
			if img.Pix[row+x*rgbaChannels+alphaChannelOffset] < opaqueAlpha {
				return true, nil
			}
		}
	}
	return false, nil
}

func rgbaToNRGBA(ctx context.Context, src *image.RGBA) (*image.NRGBA, error) {
	b := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		srcRow := (b.Min.Y+y-src.Rect.Min.Y)*src.Stride + (b.Min.X-src.Rect.Min.X)*4
		dstRow := y * dst.Stride
		for x := 0; x < b.Dx(); x++ {
			sp := src.Pix[srcRow+x*4 : srcRow+x*4+4]
			dp := dst.Pix[dstRow+x*4 : dstRow+x*4+4]
			a := sp[3]
			dp[3] = a
			if a < transparentAlphaByteThreshold {
				dp[0], dp[1], dp[2] = sp[0], sp[1], sp[2]
				continue
			}
			if a == 255 {
				dp[0], dp[1], dp[2] = sp[0], sp[1], sp[2]
				continue
			}
			dp[0] = clip255(float64(sp[0]) * 255 / float64(a))
			dp[1] = clip255(float64(sp[1]) * 255 / float64(a))
			dp[2] = clip255(float64(sp[2]) * 255 / float64(a))
		}
	}
	return dst, nil
}

// splitYCbCr un-premultiplies semi-transparent pixels so colour math runs on straight RGB; mergeYCbCr re-premultiplies.
func splitYCbCr(ctx context.Context, img *image.RGBA) (y *tiles.Plane, cb, cr []float64, err error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	n := w * h
	yPx := make([]float64, n)
	cb = make([]float64, n)
	cr = make([]float64, n)
	alpha := make([]float64, n)
	for j := 0; j < h; j++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, err
		}
		row := j * img.Stride
		out := j * w
		for i := 0; i < w; i++ {
			px := img.Pix[row+i*4 : row+i*4+4]
			r, g, b, a := float64(px[0]), float64(px[1]), float64(px[2]), float64(px[3])
			if a > 0 && a < 255 && a/255 >= transparentAlphaThreshold {
				s := 255 / a
				r, g, b = r*s, g*s, b*s
			}
			idx := out + i
			yPx[idx] = 0.299*r + 0.587*g + 0.114*b           // reason: ITU-R BT.601 luma transform.
			cb[idx] = -0.168736*r - 0.331264*g + 0.5*b + 128 // reason: ITU-R BT.601 Cb chroma transform.
			cr[idx] = 0.5*r - 0.418688*g - 0.081312*b + 128  // reason: ITU-R BT.601 Cr chroma transform.
			alpha[idx] = a / 255
		}
	}
	y = &tiles.Plane{W: w, H: h, Pixels: yPx, Alpha: alpha}
	return
}

func clonePlanePixels(ctx context.Context, y *tiles.Plane) ([]float64, error) {
	out := make([]float64, len(y.Pixels))
	for row := 0; row < y.H; row++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		start := row * y.W
		copy(out[start:start+y.W], y.Pixels[start:start+y.W])
	}
	return out, nil
}

func restorePlanePixels(ctx context.Context, y *tiles.Plane, original []float64) error {
	for row := 0; row < y.H; row++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		start := row * y.W
		copy(y.Pixels[start:start+y.W], original[start:start+y.W])
	}
	return nil
}

func mergeYCbCr(ctx context.Context, img *image.RGBA, y *tiles.Plane, cb, cr []float64) error {
	for j := 0; j < y.H; j++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		row := j * img.Stride
		in := j * y.W
		for i := 0; i < y.W; i++ {
			idx := in + i
			if y.Alpha[idx] < transparentAlphaThreshold {
				continue
			}
			yv, cbv, crv := y.Pixels[idx], cb[idx]-128, cr[idx]-128
			r := yv + 1.402*crv                   // reason: ITU-R BT.601 inverse Cr gain.
			g := yv - 0.344136*cbv - 0.714136*crv // reason: ITU-R BT.601 inverse Cb/Cr cross terms.
			b := yv + 1.772*cbv                   // reason: ITU-R BT.601 inverse Cb gain.
			if a := img.Pix[row+i*4+3]; a < 255 {
				s := float64(a) / 255
				r, g, b = r*s, g*s, b*s
			}
			img.Pix[row+i*4+0] = clip255(r)
			img.Pix[row+i*4+1] = clip255(g)
			img.Pix[row+i*4+2] = clip255(b)
		}
	}
	return nil
}

func clip255(v float64) uint8 {
	switch {
	case v < 0:
		return 0
	case v > 255:
		return 255
	default:
		return uint8(math.Round(v))
	}
}
