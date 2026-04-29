// SPDX-License-Identifier: AGPL-3.0-only

// Copyright (C) 2026 MTN Media Group.

// Package tiles partitions a luminance plane into a deterministic grid and scores per-tile capacity.
package tiles

import (
	"math"
	"sort"
)

const MinAlpha = 0.95          // reason: skip tiles with significant translucency to avoid alpha-bleed corruption.
const minTileVariance = 8      // reason: 8/255 luminance std dev separates flat regions from texture.
const minTileEdge = 1.5        // reason: average per-pixel gradient magnitude separating gradient from edge content.
const tileVarianceWeight = 0.6 // reason: corpus-tuned weighting between texture variance and edge density.
const tileEdgeWeight = 8.0     // reason: corpus-tuned weighting between texture variance and edge density.

// Plane is row-major; Alpha=nil means fully opaque.
type Plane struct {
	W, H   int
	Pixels []float64
	Alpha  []float64
}

type Tile struct {
	Index int
	X, Y  int
	Size  int
	Score float64
	Alpha float64
}

// Iterate drops incomplete trailing tiles so every returned tile holds a full set of 8x8 sub-blocks.
func Iterate(p *Plane, size int) []Tile {
	cols, rows := p.W/size, p.H/size
	out := make([]Tile, 0, rows*cols)
	for ty := 0; ty < rows; ty++ {
		for tx := 0; tx < cols; tx++ {
			t := Tile{
				Index: len(out),
				X:     tx * size,
				Y:     ty * size,
				Size:  size,
			}
			t.Score, t.Alpha = score(p, t.X, t.Y, size)
			out = append(out, t)
		}
	}
	return out
}

// score returns 0 for a skip tile, otherwise positive capacity. Thresholds are tuned for 8-bit luminance.
func score(p *Plane, x0, y0, size int) (float64, float64) {
	var sum, sumSq, edges, alphaSum float64
	n := float64(size * size)
	for y := y0; y < y0+size; y++ {
		row := y * p.W
		for x := x0; x < x0+size; x++ {
			v := p.Pixels[row+x]
			sum += v
			sumSq += v * v
			if p.Alpha != nil {
				alphaSum += p.Alpha[row+x]
			}
			if x+1 < x0+size {
				edges += math.Abs(p.Pixels[row+x+1] - v)
			}
			if y+1 < y0+size {
				edges += math.Abs(p.Pixels[(y+1)*p.W+x] - v)
			}
		}
	}
	mean := sum / n
	variance := sumSq/n - mean*mean
	edges /= n
	alpha := 1.0
	if p.Alpha != nil {
		alpha = alphaSum / n
	}
	if alpha < MinAlpha || variance < minTileVariance || edges < minTileEdge {
		return 0, alpha
	}
	return variance*tileVarianceWeight + edges*tileEdgeWeight, alpha
}

func SelectByScore(ts []Tile, max int) []int {
	idx := make([]int, 0, len(ts))
	for i, t := range ts {
		if t.Score > 0 {
			idx = append(idx, i)
		}
	}
	sort.SliceStable(idx, func(a, b int) bool {
		return ts[idx[a]].Score > ts[idx[b]].Score
	})
	if len(idx) > max {
		idx = idx[:max]
	}
	return idx
}

func Score(p *Plane, x0, y0, size int) float64 {
	score, _ := score(p, x0, y0, size)
	return score
}
