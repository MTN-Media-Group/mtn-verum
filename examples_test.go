// SPDX-License-Identifier: AGPL-3.0-only

// Copyright (C) 2026 MTN Media Group.

package verum

import (
	"context"
	"time"
)

func Example_embed() {
	key := Key{ID: "k1", Secret: []byte("a high-entropy secret of at least 16 bytes")}
	cfg := Config{ActiveKey: key, DetectionKeys: []Key{key}, Strength: StrengthBalanced}
	payload := Payload{GeneratedAt: time.Unix(1700000000, 0).UTC(), Provider: "mtnai", Model: "flux.1-pro", GenerationID: "gen_123"}
	res, err := Embed(context.Background(), textureImage(512, 512), "image/png", payload, cfg)
	if err != nil || res == nil || len(res.Data) == 0 {
		panic("embed failed")
	}
	// Output:
}

func Example_detect() {
	key := Key{ID: "k1", Secret: []byte("a high-entropy secret of at least 16 bytes")}
	cfg := Config{ActiveKey: key, DetectionKeys: []Key{key}, Strength: StrengthBalanced}
	payload := Payload{GeneratedAt: time.Unix(1700000000, 0).UTC(), Provider: "mtnai", Model: "flux.1-pro", GenerationID: "gen_123"}
	res, err := Embed(context.Background(), textureImage(512, 512), "image/png", payload, cfg)
	if err != nil {
		panic("embed failed")
	}
	det, err := Detect(context.Background(), res.Data, "image/png", cfg)
	if err != nil || !det.Detected {
		panic("detect failed")
	}
	// Output:
}
