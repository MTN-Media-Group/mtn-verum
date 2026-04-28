// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 MTN Media Group.

// Package dct implements the orthonormal 8x8 type-II DCT used by JPEG.
package dct

import "math"

const N = 8

// basis[n][k] holds the orthonormal basis pre-scaled, so forward/inverse are dot products.
var basis [N][N]float64

func init() {
	scale0 := 1.0 / math.Sqrt(N)
	scaleN := math.Sqrt(2.0 / N)
	for n := 0; n < N; n++ {
		s := scaleN
		if n == 0 {
			s = scale0
		}
		for k := 0; k < N; k++ {
			basis[n][k] = s * math.Cos(float64(2*k+1)*float64(n)*math.Pi/(2*N))
		}
	}
}

// Forward transforms b in place. JPEG callers subtract 128 first to centre around zero.
func Forward(b *[N * N]float64) {
	var tmp [N * N]float64
	for r := 0; r < N; r++ {
		row := b[r*N : r*N+N]
		for n := 0; n < N; n++ {
			var s float64
			for k := 0; k < N; k++ {
				s += row[k] * basis[n][k]
			}
			tmp[r*N+n] = s
		}
	}
	for c := 0; c < N; c++ {
		for n := 0; n < N; n++ {
			var s float64
			for k := 0; k < N; k++ {
				s += tmp[k*N+c] * basis[n][k]
			}
			b[n*N+c] = s
		}
	}
}

func Inverse(b *[N * N]float64) {
	var tmp [N * N]float64
	for r := 0; r < N; r++ {
		for k := 0; k < N; k++ {
			var s float64
			for n := 0; n < N; n++ {
				s += b[r*N+n] * basis[n][k]
			}
			tmp[r*N+k] = s
		}
	}
	for c := 0; c < N; c++ {
		for k := 0; k < N; k++ {
			var s float64
			for n := 0; n < N; n++ {
				s += tmp[n*N+c] * basis[n][k]
			}
			b[k*N+c] = s
		}
	}
}
