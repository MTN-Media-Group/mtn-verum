// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 MTN Media Group.

// Package tiles partitions a luminance plane into a deterministic grid and scores per-tile capacity.
package tiles

import (
	"math"
	"sort"
)

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
	Score float64 // <=0 means skip
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
			t.Score = score(p, t.X, t.Y, size)
			out = append(out, t)
		}
	}
	return out
}

// score returns 0 for a skip tile, otherwise positive capacity. Thresholds are tuned for 8-bit luminance.
func score(p *Plane, x0, y0, size int) float64 {
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
	if alpha < 0.95 || variance < 8 || edges < 1.5 {
		return 0
	}
	return variance*0.6 + edges*8.0
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
