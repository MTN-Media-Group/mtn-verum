// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 MTN Media Group.

package verum

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
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

// midFreqPositions are the (u, v) DCT coefficient indices the embedder draws
// pairs from. The set excludes DC and the lowest two diagonals (visible at
// any strength), and the highest frequencies (quantised away by JPEG).
var midFreqPositions = [...][2]int{
	{1, 2}, {2, 1}, {1, 3}, {3, 1}, {2, 2},
	{1, 4}, {4, 1}, {2, 3}, {3, 2},
	{1, 5}, {5, 1}, {2, 4}, {4, 2}, {3, 3},
	{1, 6}, {6, 1}, {2, 5}, {5, 2}, {3, 4}, {4, 3},
	{2, 6}, {6, 2}, {3, 5}, {5, 3}, {4, 4},
}

func embed(ctx context.Context, data []byte, mimeType string, payload Payload, cfg Config) (*EmbedResult, error) {
	if err := cfg.validate(true); err != nil {
		return nil, err
	}
	digest, err := computeDigest(&payload, cfg.ActiveKey.Secret)
	if err != nil {
		return nil, err
	}
	frame := ecc.Frame(byte(PayloadVersion), keyIDByte(cfg.ActiveKey.ID), digest)
	bits := ecc.BitsOf(frame)

	img, srcFormat, err := codec.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedFormat, err)
	}
	rect := img.Bounds()
	w, h := rect.Dx(), rect.Dy()
	if min(w, h) < MinImageDim {
		return nil, ErrImageTooSmall
	}

	rgba := toRGBA(img)
	yPlane, cbPlane, crPlane, _ := splitYCbCrA(rgba)

	tileSize := DefaultTileSize
	if max(w, h) >= largeImageThreshold {
		tileSize = LargeTileSize
	}

	profile := cfg.strengthProfile()
	gates := qualityGates(profile, cfg.Quality)

	originalY := append([]float64(nil), yPlane.Pixels...)

	var (
		report  *embedReport
		gateErr error
	)
	for attempt := 0; attempt <= gates.MaxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		copy(yPlane.Pixels, originalY)
		delta := strengthDelta(profile) * (1.0 - 0.18*float64(attempt))
		report, gateErr = embedAttempt(yPlane, tileSize, bits, cfg.ActiveKey, delta, gates)
		if gateErr == nil {
			break
		}
		profile = downgradeProfile(profile)
	}
	if gateErr != nil {
		return nil, gateErr
	}

	mergeYCbCr(rgba, yPlane, cbPlane, crPlane)

	outFormat := codec.Format(mimeType)
	if outFormat == "" {
		outFormat = srcFormat
	}
	encoded, err := codec.Encode(rgba, outFormat, codec.EncodeOptions{})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedFormat, err)
	}

	// Self-detection runs against the encoded bytes so we catch losses
	// introduced by JPEG quantisation, not only the in-memory plane.
	self, err := selfDetect(encoded, tileSize, cfg)
	if err != nil || !self.Detected {
		return nil, ErrSelfDetectionFailed
	}

	res := &EmbedResult{
		Data:              encoded,
		MimeType:          string(outFormat),
		PayloadDigest:     hexDigest(digest),
		KeyID:             cfg.ActiveKey.ID,
		Version:           PayloadVersion,
		SelfDetection:     *self,
		Quality:           report.toReport(),
		ChangedPixelRatio: report.changeRatio,
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

type embedReport struct {
	ssim, psnr, maxDelta, changeRatio float64
	usedTiles                         int
}

func (r *embedReport) toReport() QualityReport {
	return QualityReport{
		SSIM:     r.ssim,
		PSNR:     r.psnr,
		MaxDelta: r.maxDelta,
		Tiles:    r.usedTiles,
	}
}

func embedAttempt(y *tiles.Plane, tileSize int, bits []uint8, key Key, delta float64, gates QualityConfig) (*embedReport, error) {
	all := tiles.Iterate(y, tileSize)
	pairsPerBlock := pairsPerSubBlock(len(bits))
	bitsPerTile := pairsPerBlock * subBlocksPerTile(tileSize)

	oneCopy := minTilesNeeded(len(bits), bitsPerTile)
	idxs := tiles.SelectByScore(all, len(all))
	if len(idxs) < oneCopy {
		return nil, ErrNoCapacity
	}
	idxs = idxs[:oneCopy*minRepetition(len(idxs), oneCopy)]

	original := append([]float64(nil), y.Pixels...)
	for _, i := range idxs {
		applyTile(y.Pixels, y.W, &all[i], bits, pairsPerBlock, key, delta)
	}

	rep := &embedReport{usedTiles: len(idxs)}
	rep.ssim = quality.SSIM(original, y.Pixels, y.W, y.H)
	rep.psnr = quality.PSNR(original, y.Pixels)
	rep.maxDelta = quality.MaxAbsDelta(original, y.Pixels)
	rep.changeRatio = quality.ChangedPixelRatio(original, y.Pixels, 0.5)

	if rep.ssim < gates.MinSSIM || rep.psnr < gates.MinPSNR || rep.changeRatio > gates.MaxChangeRatio {
		return rep, ErrQualityGateFailed
	}
	return rep, nil
}

// selfDetect runs the public detection pipeline against freshly-encoded
// bytes. It uses only the active key and native scale so its cost is closer
// to a single embed pass than a full multi-key sweep.
func selfDetect(encoded []byte, tileSize int, cfg Config) (*DetectResult, error) {
	img, _, err := codec.Decode(encoded)
	if err != nil {
		return nil, err
	}
	plane := luminancePlane(img)
	return detectFromY(plane, plane.W, plane.H, tileSize, []Key{cfg.ActiveKey}, []float64{1.0}, cfg.Detection)
}

func applyTile(pixels []float64, w int, t *tiles.Tile, bits []uint8, pairsPerBlock int, key Key, delta float64) {
	subRows := t.Size / dct.N
	subCols := t.Size / dct.N
	for sr := 0; sr < subRows; sr++ {
		for sc := 0; sc < subCols; sc++ {
			subIdx := sr*subCols + sc
			origin := (t.Y+sr*dct.N)*w + (t.X + sc*dct.N)
			var block [dct.N * dct.N]float64
			loadBlock(pixels, w, origin, &block)
			dct.Forward(&block)
			pairs := derivePairs(key.Secret, t.Index, subIdx, pairsPerBlock)
			for j, pr := range pairs {
				bit := bits[(subIdx*pairsPerBlock+j)%len(bits)]
				biasPair(&block, pr, bit, delta)
			}
			dct.Inverse(&block)
			storeBlock(pixels, w, origin, &block)
		}
	}
}

func biasPair(b *[dct.N * dct.N]float64, p [2][2]int, bit uint8, delta float64) {
	a := p[0][0]*dct.N + p[0][1]
	c := p[1][0]*dct.N + p[1][1]
	mid := (b[a] + b[c]) * 0.5
	half := delta * 0.5
	if bit == 1 {
		b[a] = mid + half
		b[c] = mid - half
	} else {
		b[a] = mid - half
		b[c] = mid + half
	}
}

func loadBlock(pixels []float64, w, origin int, b *[dct.N * dct.N]float64) {
	for r := 0; r < dct.N; r++ {
		row := pixels[origin+r*w : origin+r*w+dct.N]
		for c := 0; c < dct.N; c++ {
			b[r*dct.N+c] = row[c] - 128
		}
	}
}

func storeBlock(pixels []float64, w, origin int, b *[dct.N * dct.N]float64) {
	for r := 0; r < dct.N; r++ {
		for c := 0; c < dct.N; c++ {
			v := b[r*dct.N+c] + 128
			if v < 0 {
				v = 0
			} else if v > 255 {
				v = 255
			}
			pixels[origin+r*w+c] = v
		}
	}
}

// derivePairs picks pairsPerBlock disjoint pairs from midFreqPositions,
// shuffled by an HMAC stream keyed by (secret, tileIndex, subBlockIndex).
// Pairs are stable per (key, tile, sub-block) so the detector can recompute
// them without seeing the digest.
func derivePairs(secret []byte, tileIdx, subIdx, pairsPerBlock int) [][2][2]int {
	mac := hmac.New(sha256.New, secret)
	var buf [12]byte
	binary.BigEndian.PutUint32(buf[0:4], uint32(tileIdx))
	binary.BigEndian.PutUint32(buf[4:8], uint32(subIdx))
	binary.BigEndian.PutUint32(buf[8:12], 0)
	mac.Write(buf[:])
	seed := mac.Sum(nil)

	pool := make([][2]int, len(midFreqPositions))
	copy(pool, midFreqPositions[:])
	for i := len(pool) - 1; i > 0; i-- {
		j := int(seed[i%len(seed)]) % (i + 1)
		pool[i], pool[j] = pool[j], pool[i]
	}
	out := make([][2][2]int, pairsPerBlock)
	for i := 0; i < pairsPerBlock; i++ {
		out[i] = [2][2]int{pool[2*i], pool[2*i+1]}
	}
	return out
}

func pairsPerSubBlock(frameBits int) int {
	const subBlocksWithSlack = 60
	k := frameBits / subBlocksWithSlack
	if k*subBlocksWithSlack < frameBits {
		k++
	}
	if k < 1 {
		k = 1
	}
	if k*2 > len(midFreqPositions) {
		k = len(midFreqPositions) / 2
	}
	return k
}

func subBlocksPerTile(tileSize int) int {
	n := tileSize / dct.N
	return n * n
}

func minTilesNeeded(frameBits, bitsPerTile int) int {
	if bitsPerTile <= 0 {
		return 0
	}
	t := frameBits / bitsPerTile
	if t*bitsPerTile < frameBits {
		t++
	}
	if t < 1 {
		t = 1
	}
	return t
}

// minRepetition decides how many tile copies of the frame to write given the
// pool of usable tiles and the minimum needed for one copy. The cap of 12
// keeps embed work bounded on very large images that would otherwise touch
// every tile.
func minRepetition(available, oneCopy int) int {
	if oneCopy <= 0 {
		return 1
	}
	r := available / oneCopy
	if r < 1 {
		return 1
	}
	if r > 12 {
		return 12
	}
	return r
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

func keyIDByte(id string) byte {
	if id == "" {
		return 0
	}
	h := sha256.Sum256([]byte(id))
	return h[0]
}

func hexDigest(d []byte) string {
	const hex = "0123456789abcdef"
	out := make([]byte, len(d)*2)
	for i, b := range d {
		out[i*2] = hex[b>>4]
		out[i*2+1] = hex[b&0x0f]
	}
	return string(out)
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

func splitYCbCrA(img *image.RGBA) (y *tiles.Plane, cb, cr, alpha []float64) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	yPx := make([]float64, w*h)
	cb = make([]float64, w*h)
	cr = make([]float64, w*h)
	alpha = make([]float64, w*h)
	for j := 0; j < h; j++ {
		row := j * img.Stride
		out := j * w
		for i := 0; i < w; i++ {
			r := float64(img.Pix[row+i*4])
			g := float64(img.Pix[row+i*4+1])
			b := float64(img.Pix[row+i*4+2])
			a := float64(img.Pix[row+i*4+3])
			yPx[out+i] = 0.299*r + 0.587*g + 0.114*b
			cb[out+i] = -0.168736*r - 0.331264*g + 0.5*b + 128
			cr[out+i] = 0.5*r - 0.418688*g - 0.081312*b + 128
			alpha[out+i] = a / 255
		}
	}
	y = &tiles.Plane{W: w, H: h, Pixels: yPx, Alpha: alpha}
	return
}

func mergeYCbCr(img *image.RGBA, y *tiles.Plane, cb, cr []float64) {
	w := y.W
	for j := 0; j < y.H; j++ {
		row := j * img.Stride
		in := j * w
		for i := 0; i < w; i++ {
			yv := y.Pixels[in+i]
			cbv := cb[in+i] - 128
			crv := cr[in+i] - 128
			r := yv + 1.402*crv
			g := yv - 0.344136*cbv - 0.714136*crv
			b := yv + 1.772*cbv
			img.Pix[row+i*4+0] = clip255(r)
			img.Pix[row+i*4+1] = clip255(g)
			img.Pix[row+i*4+2] = clip255(b)
			// alpha left untouched
		}
	}
}

func clip255(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(math.Round(v))
}
