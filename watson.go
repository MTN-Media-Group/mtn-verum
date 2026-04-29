// SPDX-License-Identifier: AGPL-3.0-only

// Copyright (C) 2026 MTN Media Group.

package verum

import (
	"math"

	"github.com/MTN-Media-Group/mtn-verum/internal/dct"
)

var watsonCSFThreshold = [dct.N][dct.N]float64{ // reason: Watson 1993 luminance JND threshold table from DCTune Tech Report NASA-TM-103890 Table 1; per-coefficient minimum-detectable-distortion in 8-bit luminance.
	{1.40, 1.01, 1.16, 1.66, 2.40, 3.43, 4.79, 6.56},
	{1.01, 1.45, 1.32, 1.52, 2.00, 2.71, 3.67, 4.93},
	{1.16, 1.32, 2.24, 2.59, 2.98, 3.64, 4.60, 5.88},
	{1.66, 1.52, 2.59, 3.77, 4.55, 5.30, 6.28, 7.60},
	{2.40, 2.00, 2.98, 4.55, 6.15, 7.46, 8.71, 10.17},
	{3.43, 2.71, 3.64, 5.30, 7.46, 9.62, 11.58, 13.51},
	{4.79, 3.67, 4.60, 6.28, 8.71, 11.58, 14.50, 17.29},
	{6.56, 4.93, 5.88, 7.60, 10.17, 13.51, 17.29, 21.15},
}

const watsonLuminanceExponent = 0.649     // reason: Watson 1993 luminance masking exponent, DCTune §2.2.
const watsonContrastMaskingExponent = 0.7 // reason: Watson 1993 contrast masking exponent, DCTune §2.3.

func watsonPairScale(block *[dct.N * dct.N]float64, p [2][2]int, _ StrengthProfile) float64 {
	a := watsonCoefficientJND(block, p[0][0], p[0][1])
	b := watsonCoefficientJND(block, p[1][0], p[1][1])
	return (a + b) / 2
}

func watsonCoefficientJND(block *[dct.N * dct.N]float64, u, v int) float64 {
	idx := u*dct.N + v
	meanLuma := watsonMeanLuma(block[0])
	luminance := math.Pow(meanLuma/128, watsonLuminanceExponent)
	base := watsonCSFThreshold[u][v] * luminance
	if u == 0 && v == 0 {
		return base
	}
	contrast := math.Pow(1+math.Abs(block[idx])/base, watsonContrastMaskingExponent)
	return base * contrast
}

func watsonMeanLuma(dc float64) float64 {
	if dc > 127*float64(dct.N) {
		return max(1, min(255, dc/float64(dct.N)))
	}
	return max(1, min(255, 128+dc/float64(dct.N)))
}
