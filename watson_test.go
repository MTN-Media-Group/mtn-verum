// SPDX-License-Identifier: AGPL-3.0-only

// Copyright (C) 2026 MTN Media Group.

package verum

import (
	"math"
	"testing"

	"github.com/MTN-Media-Group/mtn-verum/internal/dct"
)

// Watson, "DCT quantization matrices visually optimized for individual images," Proc. SPIE 1913 (1993), Table 1 / DCTune Tech Report NASA-TM-103890 §2.1.
var watsonReferenceLuminanceThreshold = [dct.N][dct.N]float64{
	{1.40, 1.01, 1.16, 1.66, 2.40, 3.43, 4.79, 6.56},
	{1.01, 1.45, 1.32, 1.52, 2.00, 2.71, 3.67, 4.93},
	{1.16, 1.32, 2.24, 2.59, 2.98, 3.64, 4.60, 5.88},
	{1.66, 1.52, 2.59, 3.77, 4.55, 5.30, 6.28, 7.60},
	{2.40, 2.00, 2.98, 4.55, 6.15, 7.46, 8.71, 10.17},
	{3.43, 2.71, 3.64, 5.30, 7.46, 9.62, 11.58, 13.51},
	{4.79, 3.67, 4.60, 6.28, 8.71, 11.58, 14.50, 17.29},
	{6.56, 4.93, 5.88, 7.60, 10.17, 13.51, 17.29, 21.15},
}

func TestWatsonAgainstReference(t *testing.T) {
	var block [dct.N * dct.N]float64
	block[0] = 128 * dct.N
	for u := 0; u < dct.N; u++ {
		for v := 0; v < dct.N; v++ {
			got := watsonCoefficientJND(&block, u, v)
			want := watsonReferenceLuminanceThreshold[u][v]
			if math.Abs(got-want)/want > 0.05 {
				t.Fatalf("JND[%d,%d] got %.4f want %.4f", u, v, got, want)
			}
		}
	}
}

func TestWatsonContrastMasking(t *testing.T) {
	var block [dct.N * dct.N]float64
	block[0] = 128 * dct.N
	block[1*dct.N+1] = 50
	base := watsonReferenceLuminanceThreshold[1][1]
	got := watsonCoefficientJND(&block, 1, 1)
	const referenceContrastExponent = 0.7 // reason: Watson 1993 contrast masking exponent literal, intentionally hard-coded to detect drift in the production constant.
	want := base * math.Pow(1+50/base, referenceContrastExponent)
	if math.Abs(got-want)/want > 0.05 {
		t.Fatalf("contrast masked JND got %.4f want %.4f", got, want)
	}
}

func TestQualityProfileDefaults(t *testing.T) {
	cases := []struct {
		profile        StrengthProfile
		minSSIM        float64
		minPSNR        float64
		maxDelta       float64
		maxChangeRatio float64
	}{
		{StrengthInvisible, 0.999, 50, 12, 0.2},
		{StrengthBalanced, 0.997, 46, 18, 0.4},
		{StrengthRobust, 0.985, 38, 80, 0.6},
	}
	for _, tc := range cases {
		got := qualityGates(tc.profile, QualityConfig{})
		if got.MinSSIM < tc.minSSIM || got.MinPSNR < tc.minPSNR || got.MaxDelta > tc.maxDelta || got.MaxChangeRatio > tc.maxChangeRatio {
			t.Fatalf("%s gates got %+v", tc.profile, got)
		}
	}
}
