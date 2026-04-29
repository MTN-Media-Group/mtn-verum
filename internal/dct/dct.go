// SPDX-License-Identifier: AGPL-3.0-only

// Copyright (C) 2026 MTN Media Group.

// Package dct implements the orthonormal 8x8 type-II DCT used by JPEG.
package dct

import "math"

const N = 8                         // reason: 8x8 sub-block size matches JPEG/AAN scaled DCT factorization.
const sqrtHalf = 0.7071067811865476 // reason: AAN scaled DCT factor: sqrt(2)/2 = cos(pi/4).

var aanForwardScale [N]float64
var aanInverseScale [N]float64

func init() {
	sqrt8 := math.Sqrt(8)
	for k := 0; k < N; k++ {
		scale := 1.0
		if k != 0 {
			scale = math.Sqrt2 * math.Cos(float64(k)*math.Pi/16)
		}
		aanForwardScale[k] = 1 / (sqrt8 * scale)
		aanInverseScale[k] = scale / sqrt8
	}
}

// Forward transforms b in place. JPEG callers subtract 128 first to centre around zero.
func Forward(b *[N * N]float64) {
	var tmp [N * N]float64
	for r := 0; r < N; r++ {
		aanForward1D(b[r*N:r*N+N], tmp[r*N:r*N+N])
	}
	for c := 0; c < N; c++ {
		src := [N]float64{tmp[c], tmp[N+c], tmp[2*N+c], tmp[3*N+c], tmp[4*N+c], tmp[5*N+c], tmp[6*N+c], tmp[7*N+c]}
		var dst [N]float64
		aanForward1D(src[:], dst[:])
		for k := 0; k < N; k++ {
			b[k*N+c] = dst[k]
		}
	}
}

func Inverse(b *[N * N]float64) {
	var tmp [N * N]float64
	for c := 0; c < N; c++ {
		src := [N]float64{b[c], b[N+c], b[2*N+c], b[3*N+c], b[4*N+c], b[5*N+c], b[6*N+c], b[7*N+c]}
		var dst [N]float64
		aanInverse1D(src[:], dst[:])
		for r := 0; r < N; r++ {
			tmp[r*N+c] = dst[r]
		}
	}
	for r := 0; r < N; r++ {
		aanInverse1D(tmp[r*N:r*N+N], b[r*N:r*N+N])
	}
}

func aanForward1D(src, dst []float64) {
	tmp0 := src[0] + src[7]
	tmp7 := src[0] - src[7]
	tmp1 := src[1] + src[6]
	tmp6 := src[1] - src[6]
	tmp2 := src[2] + src[5]
	tmp5 := src[2] - src[5]
	tmp3 := src[3] + src[4]
	tmp4 := src[3] - src[4]

	tmp10 := tmp0 + tmp3
	tmp13 := tmp0 - tmp3
	tmp11 := tmp1 + tmp2
	tmp12 := tmp1 - tmp2

	dst[0] = (tmp10 + tmp11) * aanForwardScale[0]
	dst[4] = (tmp10 - tmp11) * aanForwardScale[4]

	z1 := (tmp12 + tmp13) * sqrtHalf
	dst[2] = (tmp13 + z1) * aanForwardScale[2]
	dst[6] = (tmp13 - z1) * aanForwardScale[6]

	tmp10 = tmp4 + tmp5
	tmp11 = tmp5 + tmp6
	tmp12 = tmp6 + tmp7

	z5 := (tmp10 - tmp12) * 0.38268343236508984 // reason: AAN scaled DCT factor: sin(pi/8).
	z2 := 0.541196100146197*tmp10 + z5          // reason: AAN scaled DCT factor: sqrt(2)*cos(3*pi/8).
	z4 := 1.3065629648763766*tmp12 + z5         // reason: AAN scaled DCT factor: sqrt(2)*sin(3*pi/8).
	z3 := tmp11 * sqrtHalf

	z11 := tmp7 + z3
	z13 := tmp7 - z3

	dst[5] = (z13 + z2) * aanForwardScale[5]
	dst[3] = (z13 - z2) * aanForwardScale[3]
	dst[1] = (z11 + z4) * aanForwardScale[1]
	dst[7] = (z11 - z4) * aanForwardScale[7]
}

func aanInverse1D(src, dst []float64) {
	tmp0 := src[0] * aanInverseScale[0]
	tmp1 := src[2] * aanInverseScale[2]
	tmp2 := src[4] * aanInverseScale[4]
	tmp3 := src[6] * aanInverseScale[6]

	tmp10 := tmp0 + tmp2
	tmp11 := tmp0 - tmp2
	tmp13 := tmp1 + tmp3
	tmp12 := (tmp1-tmp3)*math.Sqrt2 - tmp13

	tmp0 = tmp10 + tmp13
	tmp3 = tmp10 - tmp13
	tmp1 = tmp11 + tmp12
	tmp2 = tmp11 - tmp12

	tmp4 := src[1] * aanInverseScale[1]
	tmp5 := src[3] * aanInverseScale[3]
	tmp6 := src[5] * aanInverseScale[5]
	tmp7 := src[7] * aanInverseScale[7]

	z13 := tmp6 + tmp5
	z10 := tmp6 - tmp5
	z11 := tmp4 + tmp7
	z12 := tmp4 - tmp7

	tmp7 = z11 + z13
	tmp11 = (z11 - z13) * math.Sqrt2

	z5 := (z10 + z12) * 1.8477590650225735 // reason: AAN scaled DCT 1D pass coefficient.
	tmp10 = z5 - z12*1.082392200292394     // reason: AAN scaled DCT 1D pass coefficient.
	tmp12 = z5 - z10*2.613125929752753     // reason: AAN scaled DCT 1D pass coefficient.

	tmp6 = tmp12 - tmp7
	tmp5 = tmp11 - tmp6
	tmp4 = tmp10 - tmp5

	dst[0] = tmp0 + tmp7
	dst[7] = tmp0 - tmp7
	dst[1] = tmp1 + tmp6
	dst[6] = tmp1 - tmp6
	dst[2] = tmp2 + tmp5
	dst[5] = tmp2 - tmp5
	dst[3] = tmp3 + tmp4
	dst[4] = tmp3 - tmp4
}
