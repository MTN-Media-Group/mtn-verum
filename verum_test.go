// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 MTN Media Group.

package verum

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"math"
	"testing"
	"time"
)

func textureImage(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r := 110 + 60*math.Sin(float64(x)*0.18) + 20*math.Cos(float64(y)*0.07)
			g := 130 + 50*math.Cos(float64(x+y)*0.11) + 15*math.Sin(float64(x-y)*0.05)
			b := 100 + 40*math.Sin(float64(x)*0.22+float64(y)*0.13) + 25*math.Cos(float64(y)*0.03)
			img.Pix[y*img.Stride+x*4+0] = clip255(r)
			img.Pix[y*img.Stride+x*4+1] = clip255(g)
			img.Pix[y*img.Stride+x*4+2] = clip255(b)
			img.Pix[y*img.Stride+x*4+3] = 255
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func testConfig() Config {
	return Config{
		ActiveKey: Key{ID: "k1", Secret: []byte("the quick brown fox jumps over the lazy dog")},
		DetectionKeys: []Key{
			{ID: "k1", Secret: []byte("the quick brown fox jumps over the lazy dog")},
		},
		Strength: StrengthBalanced,
	}
}

func testPayload() Payload {
	return Payload{
		GeneratedAt:  time.Unix(1700000000, 0).UTC(),
		Provider:     "mtnai",
		Model:        "flux.1-pro",
		GenerationID: "gen_123",
		AttachmentID: "att_xyz",
		Nonce:        []byte{1, 2, 3, 4},
	}
}

func TestEmbedDetectRoundTrip(t *testing.T) {
	cfg := testConfig()
	src := textureImage(512, 512)
	res, err := Embed(context.Background(), src, "image/png", testPayload(), cfg)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if !res.SelfDetection.Detected {
		t.Fatalf("self-detect did not fire: confidence=%v", res.SelfDetection.Confidence)
	}

	det, err := Detect(context.Background(), res.Data, "image/png", cfg)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if !det.Detected {
		t.Fatalf("detect failed: confidence=%v keyID=%q", det.Confidence, det.KeyID)
	}
	if det.PayloadDigest != res.PayloadDigest {
		t.Fatalf("digest mismatch: got %s want %s", det.PayloadDigest, res.PayloadDigest)
	}
}

func TestDetectMissOnUnmarkedImage(t *testing.T) {
	cfg := testConfig()
	src := textureImage(512, 512)
	det, err := Detect(context.Background(), src, "image/png", cfg)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if det.Detected {
		t.Fatalf("false positive on unmarked image: confidence=%v", det.Confidence)
	}
}

func TestEmbedRejectsImageBelowMinimum(t *testing.T) {
	cfg := testConfig()
	src := textureImage(128, 128)
	if _, err := Embed(context.Background(), src, "image/png", testPayload(), cfg); err != ErrImageTooSmall {
		t.Fatalf("expected ErrImageTooSmall, got %v", err)
	}
}

func TestEmbedRejectsEmptyKey(t *testing.T) {
	cfg := testConfig()
	cfg.ActiveKey = Key{}
	src := textureImage(512, 512)
	if _, err := Embed(context.Background(), src, "image/png", testPayload(), cfg); err == nil {
		t.Fatal("expected ErrInvalidConfig for empty key")
	}
}

func TestVerifyMatchesDigest(t *testing.T) {
	cfg := testConfig()
	src := textureImage(512, 512)
	payload := testPayload()
	res, err := Embed(context.Background(), src, "image/png", payload, cfg)
	if err != nil {
		t.Fatal(err)
	}
	det, err := Verify(context.Background(), res.Data, "image/png", payload, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !det.Detected {
		t.Fatalf("verify failed despite matching payload: confidence=%v", det.Confidence)
	}
}
