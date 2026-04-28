// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 MTN Media Group.

package dct

import (
	"math"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	src := [N * N]float64{
		-72, 36, -28, 12, 4, -2, 1, 0,
		38, -90, 24, -10, 6, -1, 0, 0,
		-22, 18, -55, 14, -8, 2, -1, 0,
		8, -6, 12, -33, 7, -4, 1, 0,
		2, -2, -3, 5, -19, 6, -2, 0,
		0, 1, -1, -2, 4, -11, 3, -1,
		-1, 0, 1, 0, -1, 2, -6, 1,
		0, 0, 0, 0, 0, -1, 1, -3,
	}
	got := src
	Forward(&got)
	Inverse(&got)
	for i, v := range got {
		if math.Abs(v-src[i]) > 1e-9 {
			t.Fatalf("idx %d: expected %v got %v", i, src[i], v)
		}
	}
}

func TestForwardLinear(t *testing.T) {
	// DCT must be linear: F(a + b) == F(a) + F(b).
	a := [N * N]float64{1, 2, 3, 4, 5, 6, 7, 8, 8, 7, 6, 5, 4, 3, 2, 1}
	b := [N * N]float64{}
	for i := range b {
		b[i] = float64((i * 17) % 23)
	}
	sum := a
	for i := range sum {
		sum[i] += b[i]
	}
	fa, fb, fsum := a, b, sum
	Forward(&fa)
	Forward(&fb)
	Forward(&fsum)
	for i := range fsum {
		if math.Abs(fsum[i]-(fa[i]+fb[i])) > 1e-9 {
			t.Fatalf("non-linear at %d", i)
		}
	}
}
