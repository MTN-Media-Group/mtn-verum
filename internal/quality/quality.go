// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 MTN Media Group.

// Package quality computes per-image distortion metrics.
package quality

import (
	"math"
)

// PSNR is in dB; identical inputs return +Inf.
func PSNR(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var sse float64
	for i := range a {
		d := a[i] - b[i]
		sse += d * d
	}
	if sse == 0 {
		return math.Inf(1)
	}
	mse := sse / float64(len(a))
	return 10 * math.Log10(255*255/mse)
}

// SSIM uses an 8x8 box window — coarser than the canonical 11x11 Gaussian, but adequate for embed gating.
func SSIM(a, b []float64, width, height int) float64 {
	if len(a) != width*height || len(b) != width*height || width < 8 || height < 8 {
		return 0
	}
	const win = 8
	const c1 = (0.01 * 255) * (0.01 * 255)
	const c2 = (0.03 * 255) * (0.03 * 255)
	var sum float64
	var n int
	rows := height - win + 1
	cols := width - win + 1
	for y := 0; y < rows; y += win / 2 {
		for x := 0; x < cols; x += win / 2 {
			var muA, muB, sigA, sigB, sigAB float64
			for j := 0; j < win; j++ {
				row := (y + j) * width
				for i := 0; i < win; i++ {
					va := a[row+x+i]
					vb := b[row+x+i]
					muA += va
					muB += vb
					sigA += va * va
					sigB += vb * vb
					sigAB += va * vb
				}
			}
			const inv = 1.0 / (win * win)
			muA *= inv
			muB *= inv
			sigA = sigA*inv - muA*muA
			sigB = sigB*inv - muB*muB
			sigAB = sigAB*inv - muA*muB
			num := (2*muA*muB + c1) * (2*sigAB + c2)
			den := (muA*muA + muB*muB + c1) * (sigA + sigB + c2)
			if den > 0 {
				sum += num / den
				n++
			}
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// MaxAbsDelta catches localised spikes that PSNR averages away.
func MaxAbsDelta(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0
	}
	var m float64
	for i := range a {
		d := a[i] - b[i]
		if d < 0 {
			d = -d
		}
		if d > m {
			m = d
		}
	}
	return m
}

// ChangedPixelRatio catches thin distributed patterns that pass PSNR/SSIM but band.
func ChangedPixelRatio(a, b []float64, threshold float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var c int
	for i := range a {
		d := a[i] - b[i]
		if d < 0 {
			d = -d
		}
		if d > threshold {
			c++
		}
	}
	return float64(c) / float64(len(a))
}
