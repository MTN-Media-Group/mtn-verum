// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 MTN Media Group.

// Package tiles partitions a luminance plane into deterministic blocks and
// scores how much hidden modulation each one can absorb without a visible
// artefact. The score combines local texture, edge energy, and alpha so the
// embedder can avoid skin, flat gradients, and transparent regions.
package tiles

import "sort"

// Plane is a planar single-channel image. Alpha is optional; when nil all
// pixels are treated as fully opaque. Pixels are stored row-major.
type Plane struct {
	W, H   int
	Pixels []float64
	Alpha  []float64
}

// Tile is one rectangular region of the plane plus its capacity score.
type Tile struct {
	Index   int
	X, Y    int
	Size    int
	Score   float64 // higher is more capacity; <=0 means skip
	Texture float64
	Edges   float64
	Alpha   float64
}

// Iterate produces a deterministic grid of size×size tiles starting at the
// top-left. Incomplete trailing rows/columns are dropped because they cannot
// hold a full set of 8×8 sub-blocks.
func Iterate(p *Plane, size int) []Tile {
	cols := p.W / size
	rows := p.H / size
	out := make([]Tile, 0, rows*cols)
	idx := 0
	for ty := 0; ty < rows; ty++ {
		for tx := 0; tx < cols; tx++ {
			t := Tile{
				Index: idx,
				X:     tx * size,
				Y:     ty * size,
				Size:  size,
			}
			scoreTile(p, &t)
			out = append(out, t)
			idx++
		}
	}
	return out
}

func scoreTile(p *Plane, t *Tile) {
	var sum, sumSq float64
	var edges float64
	var alphaSum float64
	n := float64(t.Size * t.Size)
	for y := t.Y; y < t.Y+t.Size; y++ {
		row := y * p.W
		for x := t.X; x < t.X+t.Size; x++ {
			v := p.Pixels[row+x]
			sum += v
			sumSq += v * v
			if p.Alpha != nil {
				alphaSum += p.Alpha[row+x]
			}
			if x+1 < t.X+t.Size {
				dx := p.Pixels[row+x+1] - v
				if dx < 0 {
					dx = -dx
				}
				edges += dx
			}
			if y+1 < t.Y+t.Size {
				dy := p.Pixels[(y+1)*p.W+x] - v
				if dy < 0 {
					dy = -dy
				}
				edges += dy
			}
		}
	}
	mean := sum / n
	variance := sumSq/n - mean*mean
	if variance < 0 {
		variance = 0
	}
	edges /= n
	alpha := 1.0
	if p.Alpha != nil {
		alpha = alphaSum / n
	}

	t.Texture = variance
	t.Edges = edges
	t.Alpha = alpha

	// Discard near-flat or mostly-transparent tiles outright. The thresholds
	// are tuned for 8-bit luminance scaled to [-128, 127]; well below typical
	// JPEG noise floors.
	if alpha < 0.95 || variance < 8 || edges < 1.5 {
		t.Score = 0
		return
	}
	t.Score = variance*0.6 + edges*8.0
}

// SelectByScore returns the indices of the highest-scoring tiles, capped to
// max. Tiles with non-positive scores are excluded.
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
