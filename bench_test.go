// SPDX-License-Identifier: AGPL-3.0-only

// Copyright (C) 2026 MTN Media Group.

package verum

import (
	"context"
	"testing"
)

func BenchmarkEmbed_512(b *testing.B) {
	benchmarkEmbed(b, 512, StrengthBalanced)
}

func BenchmarkEmbed_1024(b *testing.B) {
	benchmarkEmbed(b, 1024, StrengthRobust)
}

func BenchmarkDetect_512(b *testing.B) {
	benchmarkDetect(b, 512, StrengthBalanced)
}

func BenchmarkDetect_1024(b *testing.B) {
	benchmarkDetect(b, 1024, StrengthRobust)
}

func benchmarkEmbed(b *testing.B, size int, strength StrengthProfile) {
	cfg := testConfig()
	cfg.Strength = strength
	src := textureImage(size, size)
	payload := testPayload()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Embed(context.Background(), src, "image/png", payload, cfg); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkDetect(b *testing.B, size int, strength StrengthProfile) {
	cfg := testConfig()
	cfg.Strength = strength
	res, err := Embed(context.Background(), textureImage(size, size), "image/png", testPayload(), cfg)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Detect(context.Background(), res.Data, "image/png", cfg); err != nil {
			b.Fatal(err)
		}
	}
}
