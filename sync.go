// SPDX-License-Identifier: AGPL-3.0-only

// Copyright (C) 2026 MTN Media Group.

package verum

import (
	"context"
	"math"
	"math/cmplx"

	"github.com/MTN-Media-Group/mtn-verum/internal/tiles"
	"gonum.org/v1/gonum/dsp/fourier"
)

const (
	syncPeriod                   = 64.0 // reason: pixel period matches the default tile size and keeps one full sync cycle per 64px tile.
	syncSearchLo                 = 44.0 // reason: corpus crop/scale recovery keeps useful FFT periods above this lower 64px-window bracket.
	syncSearchHi                 = 76.0 // reason: corpus crop/scale recovery keeps useful FFT periods below this upper 64px-window bracket.
	syncTextureVarianceThreshold = 8.0  // reason: tile scorer corpus floor; flatter luminance regions showed visible sync energy before reliable recovery.
	syncTextureEdgeThreshold     = 1.5  // reason: tile scorer corpus edge floor; smoother gradients activated visibly without improving sync recovery.
	syncTextureActivationMargin  = 1.2  // reason: corpus flat-region guard keeps sync activation below the 5% visibility budget.
	syncRobustAmplitude          = 2.7  // reason: corpus robust marks raise sync FFT peaks by at least 5x over unmarked diff noise.
	syncBalancedAmplitude        = 0.6  // reason: corpus balanced marks remain detectable without exceeding flat-region visibility gates.
	syncInvisibleAmplitude       = 0.2  // reason: corpus invisible marks preserve native quality gates while retaining same-size detection.
	syncTextureRadius            = 2    // reason: 5x5 sample window matches DefaultTileSize/16 = 4 stride; balance between local and edge bias.
	syncScaleClampLow            = 0.4  // reason: corpus scale sweep brackets the lower recovery edge within 30%.
	syncScaleClampHigh           = 1.25 // reason: corpus scale sweep brackets the upper recovery edge within 30%.
	resampleIdentityLow          = 0.98 // reason: scales within 2% of 1.0 are treated as identity to avoid resample noise on a near-trivial transform.
	resampleIdentityHigh         = 1.02 // reason: scales within 2% of 1.0 are treated as identity to avoid resample noise on a near-trivial transform.
	maxResampleDim               = 4096 // reason: cap upscaled detection planes to keep peak memory bounded for large native inputs.
)

type syncEstimate struct {
	scale    float64
	cropX    float64
	cropY    float64
	strength float64
}

func applySyncPattern(ctx context.Context, y *tiles.Plane, profile StrengthProfile) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	amp := syncAmplitude(profile)
	snapshot := *y
	snapshot.Pixels = append([]float64(nil), y.Pixels...)
	for py := 0; py < y.H; py++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		cy := math.Cos(2 * math.Pi * float64(py) / syncPeriod)
		row := py * y.W
		for px := 0; px < y.W; px++ {
			idx := row + px
			if y.Alpha != nil && y.Alpha[idx] < tiles.MinAlpha {
				continue
			}
			mask := syncTextureMask(&snapshot, px, py)
			if mask == 0 {
				continue
			}
			v := y.Pixels[idx] + amp*mask*0.5*(math.Cos(2*math.Pi*float64(px)/syncPeriod)+cy)
			y.Pixels[idx] = min(255, max(0, v))
		}
	}
	return nil
}

func syncTextureMask(p *tiles.Plane, px, py int) float64 {
	x0, x1 := max(0, px-syncTextureRadius), min(p.W-1, px+syncTextureRadius)
	y0, y1 := max(0, py-syncTextureRadius), min(p.H-1, py+syncTextureRadius)
	var sum, sumSq, edges float64
	var n int
	for y := y0; y <= y1; y++ {
		row := y * p.W
		for x := x0; x <= x1; x++ {
			v := p.Pixels[row+x]
			sum += v
			sumSq += v * v
			n++
			if x < x1 {
				edges += math.Abs(p.Pixels[row+x+1] - v)
			}
			if y < y1 {
				edges += math.Abs(p.Pixels[(y+1)*p.W+x] - v)
			}
		}
	}
	if n == 0 {
		return 0
	}
	mean := sum / float64(n)
	variance := max(0, sumSq/float64(n)-mean*mean)
	edgeMean := edges / float64(n)
	varianceThreshold := syncTextureVarianceThreshold * syncTextureActivationMargin
	edgeThreshold := syncTextureEdgeThreshold * syncTextureActivationMargin
	if variance < varianceThreshold || edgeMean < edgeThreshold {
		return 0
	}
	return min(1, 0.5*(math.Sqrt(variance/varianceThreshold)+edgeMean/edgeThreshold))
}

func syncAmplitude(profile StrengthProfile) float64 {
	switch profile {
	case StrengthRobust:
		return syncRobustAmplitude
	case StrengthBalanced:
		return syncBalancedAmplitude
	default:
		return syncInvisibleAmplitude
	}
}

func estimateSync(ctx context.Context, y *tiles.Plane) (syncEstimate, error) {
	if err := ctx.Err(); err != nil {
		return syncEstimate{}, err
	}
	x, err := projectionX(ctx, y)
	if err != nil {
		return syncEstimate{}, err
	}
	yr, err := projectionY(ctx, y)
	if err != nil {
		return syncEstimate{}, err
	}
	px, err := estimateAxisSync(ctx, x)
	if err != nil {
		return syncEstimate{}, err
	}
	py, err := estimateAxisSync(ctx, yr)
	if err != nil {
		return syncEstimate{}, err
	}
	scale := 1.0
	if px.period > 0 && py.period > 0 {
		scale = (px.period + py.period) * 0.5 / syncPeriod
	} else if px.period > 0 {
		scale = px.period / syncPeriod
	} else if py.period > 0 {
		scale = py.period / syncPeriod
	}
	strength := max(px.strength, py.strength)
	return syncEstimate{
		scale:    min(syncScaleClampHigh, max(syncScaleClampLow, scale)),
		cropX:    phaseToOffset(px.phase, px.period),
		cropY:    phaseToOffset(py.phase, py.period),
		strength: strength,
	}, nil
}

type axisSync struct {
	period   float64
	phase    float64
	strength float64
}

func estimateAxisSync(ctx context.Context, seq []float64) (axisSync, error) {
	if err := ctx.Err(); err != nil {
		return axisSync{}, err
	}
	if len(seq) < MinImageDim {
		return axisSync{period: syncPeriod}, nil
	}
	if err := center(ctx, seq); err != nil {
		return axisSync{}, err
	}
	fft := fourier.NewFFT(len(seq))
	coeff := fft.Coefficients(nil, seq)
	if err := ctx.Err(); err != nil {
		return axisSync{}, err
	}
	lo := max(1, int(math.Floor(float64(len(seq))/syncSearchHi)))
	hi := min(len(coeff)-1, int(math.Ceil(float64(len(seq))/syncSearchLo)))
	var bestK int
	var bestMag, total float64
	for k := lo; k <= hi; k++ {
		if err := ctx.Err(); err != nil {
			return axisSync{}, err
		}
		mag := cmplx.Abs(coeff[k])
		total += mag
		if mag > bestMag {
			bestMag = mag
			bestK = k
		}
	}
	if bestK == 0 {
		return axisSync{period: syncPeriod}, nil
	}
	period := float64(len(seq)) / float64(bestK)
	mean := total / float64(max(1, hi-lo+1))
	return axisSync{
		period:   period,
		phase:    cmplx.Phase(coeff[bestK]),
		strength: bestMag / max(1, mean),
	}, nil
}

func center(ctx context.Context, seq []float64) error {
	var sum float64
	for _, v := range seq {
		if err := ctx.Err(); err != nil {
			return err
		}
		sum += v
	}
	mean := sum / float64(len(seq))
	for i := range seq {
		if err := ctx.Err(); err != nil {
			return err
		}
		seq[i] -= mean
	}
	return nil
}

func projectionX(ctx context.Context, y *tiles.Plane) ([]float64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]float64, y.W)
	for py := 0; py < y.H; py++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		row := py * y.W
		for px := 0; px < y.W; px++ {
			out[px] += y.Pixels[row+px]
		}
	}
	inv := 1 / float64(y.H)
	for i := range out {
		out[i] *= inv
	}
	return out, nil
}

func projectionY(ctx context.Context, y *tiles.Plane) ([]float64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]float64, y.H)
	for py := 0; py < y.H; py++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		row := py * y.W
		for px := 0; px < y.W; px++ {
			out[py] += y.Pixels[row+px]
		}
		out[py] /= float64(y.W)
	}
	return out, nil
}

func phaseToOffset(phase, period float64) float64 {
	if period <= 0 {
		return 0
	}
	offset := -phase * period / (2 * math.Pi)
	offset = math.Mod(offset, period)
	if offset < 0 {
		offset += period
	}
	return offset
}

func resamplePlane(ctx context.Context, src *tiles.Plane, scale float64) (*tiles.Plane, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if scale < resampleIdentityLow || scale > resampleIdentityHigh {
		w, h := resamplePlaneDimensions(src, scale)
		return resizePlane(ctx, src, w, h)
	}
	cp := *src
	cp.Pixels = make([]float64, len(src.Pixels))
	if src.Alpha != nil {
		cp.Alpha = make([]float64, len(src.Alpha))
	}
	for y := 0; y < src.H; y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		row := y * src.W
		copy(cp.Pixels[row:row+src.W], src.Pixels[row:row+src.W])
		if src.Alpha != nil {
			copy(cp.Alpha[row:row+src.W], src.Alpha[row:row+src.W])
		}
	}
	return &cp, nil
}

func resamplePlaneDimensions(src *tiles.Plane, scale float64) (int, int) {
	if scale < resampleIdentityLow || scale > resampleIdentityHigh {
		w := max(MinImageDim, int(math.Round(float64(src.W)/scale)))
		h := max(MinImageDim, int(math.Round(float64(src.H)/scale)))
		return w, h
	}
	return src.W, src.H
}

func resamplePlaneFitsLimit(src *tiles.Plane, scale float64) bool {
	w, h := resamplePlaneDimensions(src, scale)
	return max(w, h) <= maxResampleDim
}

func resizePlane(ctx context.Context, src *tiles.Plane, w, h int) (*tiles.Plane, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dst := &tiles.Plane{W: w, H: h, Pixels: make([]float64, w*h)}
	if src.Alpha != nil {
		dst.Alpha = make([]float64, w*h)
	}
	sx := float64(src.W) / float64(w)
	sy := float64(src.H) / float64(h)
	for y := 0; y < h; y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		fy := (float64(y)+0.5)*sy - 0.5
		y0, y1, wy := sampleBounds(fy, src.H)
		for x := 0; x < w; x++ {
			fx := (float64(x)+0.5)*sx - 0.5
			x0, x1, wx := sampleBounds(fx, src.W)
			idx := y*w + x
			dst.Pixels[idx] = bilinear(src.Pixels, src.W, x0, x1, y0, y1, wx, wy)
			if src.Alpha != nil {
				dst.Alpha[idx] = bilinear(src.Alpha, src.W, x0, x1, y0, y1, wx, wy)
			}
		}
	}
	return dst, nil
}

func sampleBounds(f float64, limit int) (int, int, float64) {
	if limit <= 1 {
		return 0, 0, 0
	}
	i := int(math.Floor(f))
	if i < 0 {
		return 0, 0, 0
	}
	if i >= limit-1 {
		return limit - 1, limit - 1, 0
	}
	return i, i + 1, f - float64(i)
}

func bilinear(p []float64, stride, x0, x1, y0, y1 int, wx, wy float64) float64 {
	a := p[y0*stride+x0]*(1-wx) + p[y0*stride+x1]*wx
	b := p[y1*stride+x0]*(1-wx) + p[y1*stride+x1]*wx
	return a*(1-wy) + b*wy
}
