// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 MTN Media Group.

package verum

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"image"
	"image/draw"
	"math"

	"github.com/MTN-Media-Group/mtn-verum/internal/codec"
	"github.com/MTN-Media-Group/mtn-verum/internal/dct"
	"github.com/MTN-Media-Group/mtn-verum/internal/ecc"
	"github.com/MTN-Media-Group/mtn-verum/internal/quality"
	"github.com/MTN-Media-Group/mtn-verum/internal/tiles"
)

// midFreqPositions excludes DC and the lowest two diagonals (visible) and the top frequencies (JPEG-quantised away).
var midFreqPositions = [...][2]int{
	{1, 2}, {2, 1}, {1, 3}, {3, 1}, {2, 2},
	{1, 4}, {4, 1}, {2, 3}, {3, 2},
	{1, 5}, {5, 1}, {2, 4}, {4, 2}, {3, 3},
	{1, 6}, {6, 1}, {2, 5}, {5, 2}, {3, 4}, {4, 3},
	{2, 6}, {6, 2}, {3, 5}, {5, 3}, {4, 4},
}

const transparentAlphaThreshold = 5.0 / 255.0

func embed(ctx context.Context, data []byte, mimeType string, payload Payload, cfg Config) (*EmbedResult, error) {
	if err := cfg.validate(true); err != nil {
		return nil, err
	}
	digest, err := computeDigest(&payload, cfg.ActiveKey.Secret)
	if err != nil {
		return nil, err
	}
	frame := ecc.Frame(byte(PayloadVersion), keyIDBytes(cfg.ActiveKey.ID), digest)
	bits := ecc.BitsOf(frame)

	img, srcFormat, err := codec.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnsupportedFormat, err)
	}
	rect := img.Bounds()
	w, h := rect.Dx(), rect.Dy()
	if min(w, h) < MinImageDim {
		return nil, ErrImageTooSmall
	}

	rgba := toRGBA(img)
	plane, cb, cr := splitYCbCr(rgba)

	tileSize := DefaultTileSize
	if max(w, h) >= largeImageThreshold {
		tileSize = LargeTileSize
	}

	profile := cfg.strengthProfile()
	original := append([]float64(nil), plane.Pixels...)
	gates := qualityGates(profile, cfg.Quality)

	var (
		report  QualityReport
		ratio   float64
		gateErr error
	)
	for attempt := 0; attempt <= gates.MaxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		copy(plane.Pixels, original)
		report, ratio, gateErr = embedAttempt(plane, original, tileSize, bits, cfg.ActiveKey, profile, gates)
		if gateErr == nil {
			break
		}
		profile = downgradeProfile(profile)
		gates = qualityGates(profile, cfg.Quality)
	}
	if gateErr != nil {
		return nil, gateErr
	}

	mergeYCbCr(rgba, plane, cb, cr)

	outFormat := codec.Format(mimeType)
	if outFormat == "" {
		outFormat = srcFormat
	}
	encoded, err := codec.Encode(rgba, outFormat, codec.EncodeOptions{})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnsupportedFormat, err)
	}

	self, err := selfDetect(encoded, tileSize, cfg)
	if err != nil || !self.Detected {
		return nil, ErrSelfDetectionFailed
	}

	res := &EmbedResult{
		Data:              encoded,
		MimeType:          string(outFormat),
		PayloadDigest:     hex.EncodeToString(digest),
		KeyID:             cfg.ActiveKey.ID,
		Version:           PayloadVersion,
		SelfDetection:     *self,
		Quality:           report,
		ChangedPixelRatio: ratio,
	}
	if cfg.MetadataMode == MetadataStandard {
		res.Metadata = map[string]string{
			"verum-version": fmt.Sprintf("%d", PayloadVersion),
			"verum-key-id":  cfg.ActiveKey.ID,
			"verum-digest":  res.PayloadDigest,
		}
	}
	return res, nil
}

func embedAttempt(y *tiles.Plane, original []float64, tileSize int, bits []uint8, key Key, profile StrengthProfile, gates QualityConfig) (QualityReport, float64, error) {
	all := tiles.Iterate(y, tileSize)
	pairsPerBlock := pairsPerSubBlock(tileSize, len(bits))
	bitsPerTile := pairsPerBlock * subBlocksPerTile(tileSize)
	idxs := selectTiles(all, len(bits), bitsPerTile)
	if len(idxs) == 0 {
		return QualityReport{}, 0, ErrNoCapacity
	}

	delta := strengthDelta(profile)
	for _, i := range idxs {
		applyTile(y, &all[i], bits, pairsPerBlock, key, delta)
	}

	report := QualityReport{
		SSIM:     quality.SSIM(original, y.Pixels, y.W, y.H),
		PSNR:     quality.PSNR(original, y.Pixels),
		MaxDelta: quality.MaxAbsDelta(original, y.Pixels),
		Tiles:    len(idxs),
	}
	ratio := quality.ChangedPixelRatio(original, y.Pixels, 0.5)

	if report.SSIM < gates.MinSSIM || report.PSNR < gates.MinPSNR || report.MaxDelta > gates.MaxDelta || ratio > gates.MaxChangeRatio {
		return report, ratio, ErrQualityGateFailed
	}
	return report, ratio, nil
}

func selfDetect(encoded []byte, tileSize int, cfg Config) (*DetectResult, error) {
	img, _, err := codec.Decode(encoded)
	if err != nil {
		return nil, err
	}
	plane, _, _ := splitYCbCr(toRGBA(img))
	return detectFromY(plane, tileSize, []Key{cfg.ActiveKey}, cfg.Detection)
}

func applyTile(y *tiles.Plane, t *tiles.Tile, bits []uint8, pairsPerBlock int, key Key, delta float64) {
	subN := t.Size / dct.N
	for sr := 0; sr < subN; sr++ {
		for sc := 0; sc < subN; sc++ {
			subIdx := sr*subN + sc
			origin := (t.Y+sr*dct.N)*y.W + (t.X + sc*dct.N)
			var block [dct.N * dct.N]float64
			loadBlock(y.Pixels, y.W, origin, &block)
			dct.Forward(&block)
			for j, pr := range derivePairs(key, t.Index, subIdx, pairsPerBlock) {
				biasPair(&block, pr, bits[subIdx*pairsPerBlock+j], delta)
			}
			dct.Inverse(&block)
			storeBlock(y.Pixels, y.Alpha, y.W, origin, &block)
		}
	}
}

func biasPair(b *[dct.N * dct.N]float64, p [2][2]int, bit uint8, delta float64) {
	a, c := p[0][0]*dct.N+p[0][1], p[1][0]*dct.N+p[1][1]
	mid := (b[a] + b[c]) * 0.5
	half := delta * 0.5
	if bit == 0 {
		half = -half
	}
	b[a] = mid + half
	b[c] = mid - half
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

func derivePairs(key Key, tileIdx, subIdx, pairsPerBlock int) [][2][2]int {
	pool := make([][2]int, len(midFreqPositions))
	copy(pool, midFreqPositions[:])
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
	mac.Write([]byte("verum-pairs-v1"))
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

// pairsPerSubBlock picks k so each tile holds an integer number of frame copies and every bit is voted equally.
func pairsPerSubBlock(tileSize, frameBits int) int {
	subBlocks := subBlocksPerTile(tileSize)
	maxPairs := len(midFreqPositions) / 2
	for k := 1; k <= maxPairs; k++ {
		if bits := k * subBlocks; bits >= frameBits && bits%frameBits == 0 {
			return k
		}
	}
	return maxPairs
}

func subBlocksPerTile(tileSize int) int {
	n := tileSize / dct.N
	return n * n
}

const maxRepetition = 12

// selectTiles takes the top-scoring tiles in whole-frame-copy groups, capped at maxRepetition.
func selectTiles(all []tiles.Tile, frameBits, bitsPerTile int) []int {
	perCopy := (frameBits + bitsPerTile - 1) / bitsPerTile
	idxs := tiles.SelectByScore(all, len(all))
	copies := len(idxs) / perCopy
	if copies < 1 {
		return nil
	}
	if copies > maxRepetition {
		copies = maxRepetition
	}
	return idxs[:perCopy*copies]
}

func downgradeProfile(p StrengthProfile) StrengthProfile {
	switch p {
	case StrengthRobust:
		return StrengthBalanced
	case StrengthBalanced:
		return StrengthInvisible
	}
	return StrengthInvisible
}

func keyIDBytes(id string) [ecc.KeyIDSize]byte {
	var out [ecc.KeyIDSize]byte
	if id != "" {
		h := sha256.Sum256([]byte(id))
		copy(out[:], h[:])
	}
	return out
}

func toRGBA(src image.Image) *image.RGBA {
	if r, ok := src.(*image.RGBA); ok {
		return r
	}
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	return dst
}

// splitYCbCr un-premultiplies semi-transparent pixels so colour math runs on straight RGB; mergeYCbCr re-premultiplies.
func splitYCbCr(img *image.RGBA) (y *tiles.Plane, cb, cr []float64) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	n := w * h
	yPx := make([]float64, n)
	cb = make([]float64, n)
	cr = make([]float64, n)
	alpha := make([]float64, n)
	for j := 0; j < h; j++ {
		row := j * img.Stride
		out := j * w
		for i := 0; i < w; i++ {
			px := img.Pix[row+i*4 : row+i*4+4]
			r, g, b, a := float64(px[0]), float64(px[1]), float64(px[2]), float64(px[3])
			if a > 0 && a < 255 {
				s := 255 / a
				r, g, b = r*s, g*s, b*s
			}
			idx := out + i
			yPx[idx] = 0.299*r + 0.587*g + 0.114*b
			cb[idx] = -0.168736*r - 0.331264*g + 0.5*b + 128
			cr[idx] = 0.5*r - 0.418688*g - 0.081312*b + 128
			alpha[idx] = a / 255
		}
	}
	y = &tiles.Plane{W: w, H: h, Pixels: yPx, Alpha: alpha}
	return
}

func mergeYCbCr(img *image.RGBA, y *tiles.Plane, cb, cr []float64) {
	for j := 0; j < y.H; j++ {
		row := j * img.Stride
		in := j * y.W
		for i := 0; i < y.W; i++ {
			idx := in + i
			if y.Alpha[idx] < transparentAlphaThreshold {
				continue
			}
			yv, cbv, crv := y.Pixels[idx], cb[idx]-128, cr[idx]-128
			r := yv + 1.402*crv
			g := yv - 0.344136*cbv - 0.714136*crv
			b := yv + 1.772*cbv
			if a := img.Pix[row+i*4+3]; a < 255 {
				s := float64(a) / 255
				r, g, b = r*s, g*s, b*s
			}
			img.Pix[row+i*4+0] = clip255(r)
			img.Pix[row+i*4+1] = clip255(g)
			img.Pix[row+i*4+2] = clip255(b)
		}
	}
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
