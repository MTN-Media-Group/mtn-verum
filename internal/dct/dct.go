// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 MTN Media Group.

// Package dct implements the orthonormal 8x8 type-II discrete cosine
// transform used by JPEG. Working in this basis lets the embedder choose
// coefficients that survive lossy recompression with predictable behaviour.
package dct

import "math"

const N = 8

var (
	cos    [N][N]float64
	scale0 = 1.0 / math.Sqrt(float64(N))
	scaleN = math.Sqrt(2.0 / float64(N))
)

func init() {
	for n := 0; n < N; n++ {
		for k := 0; k < N; k++ {
			cos[n][k] = math.Cos(float64(2*k+1) * float64(n) * math.Pi / (2 * N))
		}
	}
}

func factor(n int) float64 {
	if n == 0 {
		return scale0
	}
	return scaleN
}

// Forward applies the 2D forward DCT in place on an 8x8 block stored in
// row-major order. The block is treated as float64 samples in any range; the
// caller chooses centring (JPEG centres around 0 by subtracting 128).
func Forward(b *[N * N]float64) {
	var tmp [N * N]float64
	for r := 0; r < N; r++ {
		row := b[r*N : r*N+N]
		for n := 0; n < N; n++ {
			var s float64
			for k := 0; k < N; k++ {
				s += row[k] * cos[n][k]
			}
			tmp[r*N+n] = factor(n) * s
		}
	}
	for c := 0; c < N; c++ {
		for n := 0; n < N; n++ {
			var s float64
			for k := 0; k < N; k++ {
				s += tmp[k*N+c] * cos[n][k]
			}
			b[n*N+c] = factor(n) * s
		}
	}
}

// Inverse applies the 2D inverse DCT in place. Forward followed by Inverse
// reproduces the input within float rounding error.
func Inverse(b *[N * N]float64) {
	var tmp [N * N]float64
	for r := 0; r < N; r++ {
		for k := 0; k < N; k++ {
			var s float64
			for n := 0; n < N; n++ {
				s += factor(n) * b[r*N+n] * cos[n][k]
			}
			tmp[r*N+k] = s
		}
	}
	for c := 0; c < N; c++ {
		for k := 0; k < N; k++ {
			var s float64
			for n := 0; n < N; n++ {
				s += factor(n) * tmp[n*N+c] * cos[n][k]
			}
			b[k*N+c] = s
		}
	}
}
