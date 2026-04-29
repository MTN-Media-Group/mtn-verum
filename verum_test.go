// SPDX-License-Identifier: AGPL-3.0-only

// Copyright (C) 2026 MTN Media Group.

package verum

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"math/cmplx"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/MTN-Media-Group/mtn-verum/internal/dct"
	"github.com/MTN-Media-Group/mtn-verum/internal/ecc"
	"github.com/MTN-Media-Group/mtn-verum/internal/quality"
	"github.com/MTN-Media-Group/mtn-verum/internal/tiles"
	xdraw "golang.org/x/image/draw"
	"gonum.org/v1/gonum/dsp/fourier"
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

func flatNoisyImage(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint8(128)
			if ((x*1103515245 + y*12345) & 1) == 1 {
				v++
			}
			img.Pix[y*img.Stride+x*4+0] = v
			img.Pix[y*img.Stride+x*4+1] = v
			img.Pix[y*img.Stride+x*4+2] = v
			img.Pix[y*img.Stride+x*4+3] = 255
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func flatImage(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	drawColor := color.RGBA{128, 128, 128, 255}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, drawColor)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func textureWithFlatPatchImage(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x < 128 && y < 128 {
				img.Pix[y*img.Stride+x*4+0] = 128
				img.Pix[y*img.Stride+x*4+1] = 128
				img.Pix[y*img.Stride+x*4+2] = 128
				img.Pix[y*img.Stride+x*4+3] = 255
				continue
			}
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

func transparentTextureImage(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x < w/2 && y < h/2 {
				continue
			}
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

func semitransparentTextureImage(w, h int, straight color.NRGBA) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x < w/4 && y < h/4 {
				img.SetNRGBA(x, y, straight)
				continue
			}
			r := 110 + 60*math.Sin(float64(x)*0.18) + 20*math.Cos(float64(y)*0.07)
			g := 130 + 50*math.Cos(float64(x+y)*0.11) + 15*math.Sin(float64(x-y)*0.05)
			b := 100 + 40*math.Sin(float64(x)*0.22+float64(y)*0.13) + 25*math.Cos(float64(y)*0.03)
			img.SetNRGBA(x, y, color.NRGBA{R: clip255(r), G: clip255(g), B: clip255(b), A: 255})
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

func rotationConfig(active Key, detection ...Key) Config {
	return Config{
		ActiveKey:     active,
		DetectionKeys: detection,
		Strength:      StrengthRobust,
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

func TestDetectSkipsScaleSweepOnNativeDetection(t *testing.T) {
	cfg := testConfig()
	cfg.Strength = StrengthRobust
	const nativeDetectionDim = 1024  // reason: large enough to make the skipped 0.5x scale sweep materially expensive.
	const nativeDetectionScale = 1.0 // reason: confirmed native detections must return before resampled scale candidates can win.
	src := textureImage(nativeDetectionDim, nativeDetectionDim)
	res, err := Embed(context.Background(), src, "image/png", testPayload(), cfg)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	det, err := Detect(context.Background(), res.Data, "image/png", cfg)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if !det.Detected {
		t.Fatalf("native detect failed: %s", diagnoseDetection(det))
	}
	if det.BestScale != nativeDetectionScale {
		t.Fatalf("native detect scale got %.3f want %.3f", det.BestScale, nativeDetectionScale)
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

func TestDetectMissReportsTilesChecked(t *testing.T) {
	cfg := testConfig()
	src := textureImage(512, 512)
	det, err := Detect(context.Background(), src, "image/png", cfg)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if det.Detected || det.Possible {
		t.Fatalf("unexpected detection on unmarked image: detected=%v possible=%v", det.Detected, det.Possible)
	}
	if det.TilesChecked == 0 {
		t.Fatalf("miss did not report checked tiles: %+v", det)
	}
}

func TestDetectRespectsCtxCancellation(t *testing.T) {
	cfg := testConfig()
	const largeCancelDim = 2048 // reason: exercises the large-image sync/resample path if cancellation is missed.
	src := textureImage(largeCancelDim, largeCancelDim)
	img, err := png.Decode(bytes.NewReader(src))
	if err != nil {
		t.Fatalf("decode large fixture: %v", err)
	}
	rgba, err := toRGBA(context.Background(), img)
	if err != nil {
		t.Fatalf("convert large fixture: %v", err)
	}
	plane, _, _, err := splitYCbCr(context.Background(), rgba)
	if err != nil {
		t.Fatalf("split large fixture: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	const cancelBound = 100 * time.Millisecond // reason: canceled detection must return before expensive image phases.
	start := time.Now()
	_, err = Detect(ctx, src, "image/png", cfg)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled from Detect, got %v", err)
	}
	if elapsed := time.Since(start); elapsed >= cancelBound {
		t.Fatalf("Detect returned after %v, want under %v", elapsed, cancelBound)
	}

	start = time.Now()
	_, err = detectFromY(ctx, plane, tileSizeForDimensions(largeCancelDim, largeCancelDim), cfg.detectionKeys(), cfg.Detection)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled from detectFromY, got %v", err)
	}
	if elapsed := time.Since(start); elapsed >= cancelBound {
		t.Fatalf("detectFromY returned after %v, want under %v", elapsed, cancelBound)
	}
}

func TestEmbedRespectsCtxCancellation(t *testing.T) {
	cfg := testConfig()
	cfg.Strength = StrengthRobust
	const largeCancelDim = 2048 // reason: preserves a large fixture while avoiding timing-dependent mid-pipeline cancellation.
	src := textureImage(largeCancelDim, largeCancelDim)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	_, err := Embed(ctx, src, "image/png", testPayload(), cfg)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled from Embed, got %v", err)
	}
	const cancelBound = 100 * time.Millisecond // reason: pre-canceled embed should return before full-image work starts.
	if elapsed := time.Since(start); elapsed >= cancelBound {
		t.Fatalf("Embed returned after %v, want under %v", elapsed, cancelBound)
	}
}

func TestEmbedRejectsImageBelowMinimum(t *testing.T) {
	cfg := testConfig()
	src := textureImage(128, 128)
	if _, err := Embed(context.Background(), src, "image/png", testPayload(), cfg); err != ErrImageTooSmall {
		t.Fatalf("expected ErrImageTooSmall, got %v", err)
	}
}

func TestIsEmbeddableRejectsImageTooLarge(t *testing.T) {
	cfg := testConfig()
	const tooWideDim = 4097 // reason: one pixel over the 4096 decode dimension cap exercises the public size error cheaply.
	const narrowDim = 1     // reason: keeps the oversized-dimension fixture small while preflight rejects before image-size checks.
	ok, err := IsEmbeddable(flatImage(tooWideDim, narrowDim), cfg)
	if ok {
		t.Fatal("expected oversized image to be rejected")
	}
	if !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("expected ErrImageTooLarge, got %v", err)
	}
}

func TestIsEmbeddableRejectsFlatImage(t *testing.T) {
	cfg := testConfig()
	ok, err := IsEmbeddable(flatImage(512, 512), cfg)
	if ok {
		t.Fatal("flat image reported embeddable")
	}
	if !errors.Is(err, ErrNoCapacity) {
		t.Fatalf("expected ErrNoCapacity for flat image, got %v", err)
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

func TestEmptyDetectionKeyIDRejected(t *testing.T) {
	cfg := Config{
		DetectionKeys: []Key{
			{ID: "", Secret: []byte("the quick brown fox jumps over the lazy dog")},
		},
	}
	if err := cfg.validate(false); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig for empty detection key ID, got %v", err)
	}
}

func TestKeyIDDoesNotHashCollide(t *testing.T) {
	const firstCollidingID = "uazLcNchboYU"  // reason: known 32-bit SHA-256 key-ID prefix collision from review P113.
	const secondCollidingID = "z18jx5rylzRy" // reason: pairs with firstCollidingID to prove widened key IDs diverge.
	first := keyIDBytes(firstCollidingID)
	second := keyIDBytes(secondCollidingID)
	if bytes.Equal(first[:], second[:]) {
		t.Fatalf("widened key IDs still collide: %x", first)
	}
}

func TestRejectsShortSecrets(t *testing.T) {
	cfg := testConfig()
	cfg.ActiveKey.Secret = []byte("short")
	src := textureImage(512, 512)
	if _, err := Embed(context.Background(), src, "image/png", testPayload(), cfg); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig for short active secret, got %v", err)
	}

	cfg = testConfig()
	cfg.ActiveKey = Key{}
	cfg.DetectionKeys[0].Secret = []byte("short")
	if _, err := Detect(context.Background(), src, "image/png", cfg); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig for short detection secret, got %v", err)
	}
}

func TestJPEGQualityRejectsBelow95(t *testing.T) {
	cfg := testConfig()
	const unsupportedJPEGQuality = 94 // reason: regression target exercises the rejected JPEG-output range just below Q95.
	cfg.JPEGQuality = unsupportedJPEGQuality
	const testDim = MinImageDim * 2 // reason: JPEG validation test needs a cheap valid source above the image-size floor.
	src := textureImage(testDim, testDim)
	_, err := Embed(context.Background(), src, "image/jpeg", testPayload(), cfg)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig for JPEGQuality 94, got %v", err)
	}
}

func TestJPEGOutputRejectsAlphaInput(t *testing.T) {
	cfg := testConfig()
	const alphaInputDim = MinImageDim                             // reason: alpha rejection should pass size validation before returning.
	const opaqueFill = 127                                        // reason: neutral opaque fill keeps every non-regression pixel JPEG-compatible.
	const opaqueAlpha = 255                                       // reason: all non-regression pixels must be fully opaque.
	const hiddenRed = 200                                         // reason: nonzero hidden RGB would become visible if JPEG flattened alpha.
	const hiddenGreen = 20                                        // reason: distinct hidden RGB catches channel-preservation regressions.
	const hiddenBlue = 180                                        // reason: distinct hidden RGB catches channel-preservation regressions.
	const semitransparentAlpha = 128                              // reason: exercises alpha that JPEG cannot preserve.
	const alphaInputX = 0                                         // reason: top-left pixel is easy to place without affecting image size.
	const alphaInputY = 0                                         // reason: top-left pixel is easy to place without affecting image size.
	const alphaJPEGMessage = "JPEG output does not support alpha" // reason: public rejection should identify the unsupported conversion.
	img := image.NewNRGBA(image.Rect(0, 0, alphaInputDim, alphaInputDim))
	for y := 0; y < alphaInputDim; y++ {
		for x := 0; x < alphaInputDim; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: opaqueFill, G: opaqueFill, B: opaqueFill, A: opaqueAlpha})
		}
	}
	img.SetNRGBA(alphaInputX, alphaInputY, color.NRGBA{R: hiddenRed, G: hiddenGreen, B: hiddenBlue, A: semitransparentAlpha})
	_, err := Embed(context.Background(), encodePNG(img), "image/jpeg", testPayload(), cfg)
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("expected ErrUnsupportedFormat for JPEG alpha input, got %v", err)
	}
	if !strings.Contains(err.Error(), alphaJPEGMessage) {
		t.Fatalf("JPEG alpha rejection should explain unsupported alpha, got %v", err)
	}
}

func TestEmbedRejectsUnsupportedOutputFormatBeforeDecode(t *testing.T) {
	cfg := testConfig()
	const unsupportedOutputMIME = "image/bmp" // reason: BMP is not one of the supported PNG, JPEG, or WebP output encoders.
	_, err := Embed(context.Background(), nil, unsupportedOutputMIME, testPayload(), cfg)
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("expected ErrUnsupportedFormat for unsupported output MIME, got %v", err)
	}
	if !strings.Contains(err.Error(), unsupportedOutputMIME) {
		t.Fatalf("unsupported output MIME should be reported before decode, got %v", err)
	}
}

func TestDetectPNGAllowsJPEGQualityBelow95(t *testing.T) {
	cfg := testConfig()
	const unsupportedJPEGQuality = 94 // reason: regression target proves non-JPEG detection ignores JPEG-output-only quality policy.
	cfg.JPEGQuality = unsupportedJPEGQuality
	const testDim = MinImageDim * 2 // reason: detection must pass the minimum image dimension gate before validating detection behavior.
	src := textureImage(testDim, testDim)
	if _, err := Detect(context.Background(), src, "image/png", cfg); err != nil {
		t.Fatalf("detect with non-JPEG data and JPEGQuality 94: %v", err)
	}
}

func TestDetectionScalesRejectsTinyValue(t *testing.T) {
	for _, scale := range []float64{1e-9, math.NaN()} {
		cfg := testConfig()
		cfg.Detection.Scales = []float64{scale}
		if err := cfg.validate(false); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("expected ErrInvalidConfig for detection scale %v, got %v", scale, err)
		}
	}
}

func TestDetectionScalesRejectsExcessiveCardinality(t *testing.T) {
	cfg := testConfig()
	const excessiveScaleHintCount = maxDetectionScalesEntries + 1 // reason: one above the validation cap exercises cardinality rejection.
	cfg.Detection.Scales = make([]float64, excessiveScaleHintCount)
	for i := range cfg.Detection.Scales {
		cfg.Detection.Scales[i] = minDetectionScale
	}
	if err := cfg.validate(false); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig for excessive detection scales, got %v", err)
	}
}

func TestDetectionScalesAreAdditive(t *testing.T) {
	const defaultHalfScale = 0.5          // reason: regression target keeps the default half-scale sweep when caller adds hints.
	const callerScaleHint = 0.6           // reason: caller hint must be distinct from default 0.5 and 0.75 scales.
	const defaultThreeQuarterScale = 0.75 // reason: regression target keeps the default three-quarter-scale sweep when caller adds hints.
	got := detectionScales([]float64{callerScaleHint})
	want := []float64{defaultHalfScale, callerScaleHint, defaultThreeQuarterScale}
	if len(got) != len(want) {
		t.Fatalf("detectionScales length got %d want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("detectionScales got %v want %v", got, want)
		}
	}
}

func TestQualityConfigRejectsInvalidValues(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Config)
	}{
		{
			name: "nan min ssim",
			mutate: func(cfg *Config) {
				cfg.Quality.MinSSIM = math.NaN()
			},
		},
		{
			name: "inf max delta",
			mutate: func(cfg *Config) {
				cfg.Quality.MaxDelta = math.Inf(1)
			},
		},
		{
			name: "min ssim above one",
			mutate: func(cfg *Config) {
				const invalidMinSSIM = 2 // reason: MinSSIM must not exceed the normalized upper bound.
				cfg.Quality.MinSSIM = invalidMinSSIM
			},
		},
		{
			name: "negative max retries",
			mutate: func(cfg *Config) {
				const invalidMaxRetries = -1 // reason: negative retries would invert retry-loop semantics.
				cfg.Quality.MaxRetries = invalidMaxRetries
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig()
			tc.mutate(&cfg)
			if err := cfg.validate(false); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("expected ErrInvalidConfig, got %v", err)
			}
		})
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

func TestVerifyRejectsDuplicateDetectionKeyIDs(t *testing.T) {
	cfg := Config{
		DetectionKeys: []Key{
			{ID: "dup", Secret: []byte("first duplicate key secret")},
			{ID: "dup", Secret: []byte("second duplicate key secret")},
		},
	}
	_, err := Verify(context.Background(), []byte("not an image"), "image/png", testPayload(), cfg)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig for duplicate detection key ID, got %v", err)
	}
}

func TestIncludeMetadataControlsEmbedMetadataOnly(t *testing.T) {
	cfg := testConfig()
	src := textureImage(512, 512)
	payload := testPayload()

	res, err := Embed(context.Background(), src, "image/png", payload, cfg)
	if err != nil {
		t.Fatalf("embed without metadata: %v", err)
	}
	if res.Metadata != nil {
		t.Fatalf("default metadata = %#v, want nil", res.Metadata)
	}
	det, err := Detect(context.Background(), res.Data, "image/png", cfg)
	if err != nil {
		t.Fatalf("detect without metadata: %v", err)
	}
	if len(det.Details) == 0 {
		t.Fatal("DetectResult.Details should always be populated")
	}

	cfg.IncludeMetadata = IncludeMetadataStandard
	res, err = Embed(context.Background(), src, "image/png", payload, cfg)
	if err != nil {
		t.Fatalf("embed with metadata: %v", err)
	}
	if res.Metadata["verum-key-id"] != cfg.ActiveKey.ID {
		t.Fatalf("metadata key ID = %q, want %q", res.Metadata["verum-key-id"], cfg.ActiveKey.ID)
	}
	det, err = Detect(context.Background(), res.Data, "image/png", cfg)
	if err != nil {
		t.Fatalf("detect with metadata: %v", err)
	}
	if len(det.Details) == 0 {
		t.Fatal("DetectResult.Details should always be populated when metadata is enabled")
	}
}

func TestEmbedQualityReportReflectsJPEGEncoding(t *testing.T) {
	cfg := testConfig()
	cfg.Strength = StrengthRobust
	cfg.JPEGQuality = 95
	src := textureImage(512, 512)
	payload := testPayload()

	noMarkSSIM := jpegTranscodeSSIM(t, src)
	jpegRes, err := Embed(context.Background(), src, "image/jpeg", payload, cfg)
	if err != nil {
		t.Fatalf("embed jpeg: %v", err)
	}
	if jpegRes.Quality.SSIM >= noMarkSSIM {
		t.Fatalf("jpeg quality report SSIM = %.9f, unmarked transcode SSIM = %.9f; want marked JPEG lower", jpegRes.Quality.SSIM, noMarkSSIM)
	}
}

func TestEmbedRejectsPostEncodeQualityViolation(t *testing.T) {
	cfg := testConfig()
	cfg.Strength = StrengthBalanced
	const testDim = MinImageDim * 2 // reason: JPEG probe needs multiple usable tiles above the minimum image size.
	src := textureImage(testDim, testDim)
	payload := testPayload()

	noMarkSSIM := jpegTranscodeSSIM(t, src)
	const ssimProbeMargin = 1e-6 // reason: keeps the quality gate just above the best possible JPEG Q95 transcode SSIM.
	cfg.Quality.MinSSIM = noMarkSSIM + ssimProbeMargin
	const postEncodeProbeRetries = 4 // reason: bounds retries while JPEG compression keeps the post-encode SSIM violation permanent.
	cfg.Quality.MaxRetries = postEncodeProbeRetries
	_, err := Embed(context.Background(), src, "image/jpeg", payload, cfg)
	if !errors.Is(err, ErrQualityGateFailed) {
		t.Fatalf("expected ErrQualityGateFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "post-encode") {
		t.Fatalf("quality gate error should identify post-encode failure: %v", err)
	}
}

func TestPostEncodeQualityGateChecksBeforeSelfDetect(t *testing.T) {
	probeCfg := testConfig()
	probeCfg.Strength = StrengthBalanced
	const testDim = MinImageDim * 2 // reason: JPEG probe needs multiple usable tiles above the minimum image size.
	src := textureImage(testDim, testDim)
	payload := testPayload()

	noMarkSSIM := jpegTranscodeSSIM(t, src)
	cfg := probeCfg
	const ssimProbeMargin = 1e-6 // reason: keeps the quality gate just above the best possible JPEG Q95 transcode SSIM.
	cfg.Quality.MinSSIM = noMarkSSIM + ssimProbeMargin
	const postEncodeProbeRetries = 4 // reason: exercises initial delta plus post-encode attenuation while keeping the test bounded.
	cfg.Quality.MaxRetries = postEncodeProbeRetries
	const impossibleMinTiles = 1 << 20 // reason: fixture cannot supply this many supporting tiles, so self-detect would fail.
	cfg.Detection.MinTiles = impossibleMinTiles
	_, err := Embed(context.Background(), src, "image/jpeg", payload, cfg)
	if !errors.Is(err, ErrQualityGateFailed) {
		t.Fatalf("expected ErrQualityGateFailed, got %v", err)
	}
	if errors.Is(err, ErrSelfDetectionFailed) {
		t.Fatalf("post-encode quality failure was hidden by self-detect failure: %v", err)
	}
	if !strings.Contains(err.Error(), "post-encode") {
		t.Fatalf("quality gate error should identify post-encode failure: %v", err)
	}

	selfCfg := probeCfg
	selfCfg.Strength = StrengthRobust
	selfCfg.Quality.MaxRetries = postEncodeProbeRetries
	selfCfg.Detection.MinTiles = impossibleMinTiles
	_, err = Embed(context.Background(), src, "image/jpeg", payload, selfCfg)
	if !errors.Is(err, ErrSelfDetectionFailed) {
		t.Fatalf("expected terminal ErrSelfDetectionFailed without weaker retries, got %v", err)
	}
}

func TestJPEGQuality95RoundTrip(t *testing.T) {
	cfg := testConfig()
	cfg.Strength = StrengthRobust
	explicit := cfg
	explicit.JPEGQuality = 95
	src := textureImage(512, 512)
	payload := testPayload()
	defaultRes, err := Embed(context.Background(), src, "image/jpeg", payload, cfg)
	if err != nil {
		t.Fatalf("embed default jpeg: %v", err)
	}
	explicitRes, err := Embed(context.Background(), src, "image/jpeg", payload, explicit)
	if err != nil {
		t.Fatalf("embed explicit jpeg: %v", err)
	}
	if !bytes.Equal(defaultRes.Data, explicitRes.Data) {
		t.Fatal("default JPEG output differs from explicit quality 95")
	}
	det, err := Detect(context.Background(), defaultRes.Data, "image/jpeg", cfg)
	if err != nil {
		t.Fatalf("detect default jpeg: %v", err)
	}
	if !det.Detected {
		t.Fatalf("default JPEG quality 95 did not survive: %s", diagnoseDetection(det))
	}
}

func TestCropSurvivalTopLeft12Percent(t *testing.T) {
	cfg := testConfig()
	cfg.Strength = StrengthRobust
	src := textureImage(512, 512)
	res, err := Embed(context.Background(), src, "image/png", testPayload(), cfg)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatal(err)
	}
	cropped := cropImage(img, 61, 61, 512, 512)
	data := encodePNG(cropped)
	det, err := Detect(context.Background(), data, "image/png", cfg)
	if err != nil {
		t.Fatalf("detect crop: %v", err)
	}
	if !det.Detected {
		t.Fatalf("crop detect failed: %s", diagnoseDetection(det))
	}
}

func TestCropSurvivalAtTilePeriodOffset(t *testing.T) {
	cfg := testConfig()
	cfg.Strength = StrengthRobust
	src := textureImage(512, 512)
	res, err := Embed(context.Background(), src, "image/png", testPayload(), cfg)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatal(err)
	}
	cropped := cropImage(img, DefaultTileSize, DefaultTileSize, 512, 512)
	det, err := Detect(context.Background(), encodePNG(cropped), "image/png", cfg)
	if err != nil {
		t.Fatalf("detect crop: %v", err)
	}
	if !det.Detected {
		t.Fatalf("tile-period crop detect failed: %s", diagnoseDetection(det))
	}
	if det.CropEstimate != DefaultTileSize {
		t.Fatalf("crop estimate got %.1f want %d", det.CropEstimate, DefaultTileSize)
	}
}

func TestCropSurvivalAtMinimumViableSource(t *testing.T) {
	cfg := testConfig()
	cfg.Strength = StrengthRobust
	const cropPeriods = 2                                                    // reason: crop capability covers up to two top-left tile periods.
	const retainedTilesPerSide = 5                                           // reason: empirical floor; four retained 64px tiles at 384→256 does not detect.
	const sourceDim = (cropPeriods + retainedTilesPerSide) * DefaultTileSize // reason: smallest tile-aligned source that leaves the retained tile floor.
	const croppedDim = retainedTilesPerSide * DefaultTileSize                // reason: verifies the test exercises the documented retained-tile floor.
	src := textureImage(sourceDim, sourceDim)
	res, err := Embed(context.Background(), src, "image/png", testPayload(), cfg)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatal(err)
	}
	offset := cropPeriods * DefaultTileSize
	cropped := cropImage(img, offset, offset, sourceDim, sourceDim)
	if cropped.Bounds().Dx() != croppedDim || cropped.Bounds().Dy() != croppedDim {
		t.Fatalf("cropped bounds got %v want %dx%d", cropped.Bounds(), croppedDim, croppedDim)
	}
	det, err := Detect(context.Background(), encodePNG(cropped), "image/png", cfg)
	if err != nil {
		t.Fatalf("detect crop: %v", err)
	}
	if !det.Detected {
		t.Fatalf("minimum-source two-tile-period crop detect failed: %s", diagnoseDetection(det))
	}
	if det.CropEstimate != float64(offset) {
		t.Fatalf("crop estimate got %.1f want %d", det.CropEstimate, offset)
	}
}

func TestCropSurvivalAt2048Source(t *testing.T) {
	cfg := testConfig()
	cfg.Strength = StrengthRobust
	src := textureImage(2048, 2048)
	res, err := Embed(context.Background(), src, "image/png", testPayload(), cfg)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatal(err)
	}
	const cropPeriods = 2 // reason: regression target crops two 128px tile periods from an adaptive-threshold source.
	offset := cropPeriods * LargeTileSize
	cropped := cropImage(img, offset, offset, 2048, 2048)
	det, err := Detect(context.Background(), encodePNG(cropped), "image/png", cfg)
	if err != nil {
		t.Fatalf("detect crop: %v", err)
	}
	if !det.Detected {
		t.Fatalf("2048-source crop detect failed: %s", diagnoseDetection(det))
	}
}

func TestDownscaleSurvival768LongestSide(t *testing.T) {
	cfg := testConfig()
	cfg.Strength = StrengthRobust
	src := textureImage(1024, 1024)
	res, err := Embed(context.Background(), src, "image/png", testPayload(), cfg)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatal(err)
	}
	data := encodePNG(resizeImageBilinear(img, 768, 768))
	det, err := Detect(context.Background(), data, "image/png", cfg)
	if err != nil {
		t.Fatalf("detect downscale: %v", err)
	}
	if !det.Detected {
		t.Fatalf("downscale detect failed: %s", diagnoseDetection(det))
	}
	if math.Abs(det.BestScale-0.75) > 0.12 {
		t.Fatalf("scale estimate got %.3f want about 0.75", det.BestScale)
	}
}

func TestNearestDownscaleSurvival512LongestSide(t *testing.T) {
	cfg := testConfig()
	cfg.Strength = StrengthRobust
	src := textureImage(1024, 1024)
	res, err := Embed(context.Background(), src, "image/png", testPayload(), cfg)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatal(err)
	}
	data := encodePNG(resizeImageNearest(img, 512, 512))
	det, err := Detect(context.Background(), data, "image/png", cfg)
	if err != nil {
		t.Fatalf("detect downscale: %v", err)
	}
	if !det.Detected {
		t.Fatalf("downscale detect failed: %s", diagnoseDetection(det))
	}
	if math.Abs(det.BestScale-0.5) > 0.12 {
		t.Fatalf("scale estimate got %.3f want about 0.5", det.BestScale)
	}
}

func TestKnownLimitTextureImage1024To512Bilinear(t *testing.T) {
	cfg := testConfig()
	cfg.Strength = StrengthRobust
	const sourceDim = 1024                          // reason: reproduces the synthetic texture 0.5x bilinear boundary kept as a known non-claim.
	const halfScaleDivisor = 2                      // reason: exact 0.5x bilinear downscale target.
	const resizedDim = sourceDim / halfScaleDivisor // reason: keeps the test tied to 1024→512 rather than a rounded scale helper.
	src := textureImage(sourceDim, sourceDim)
	res, err := Embed(context.Background(), src, "image/png", testPayload(), cfg)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatal(err)
	}
	data := encodePNG(resizeImageBilinear(img, resizedDim, resizedDim))
	det, err := Detect(context.Background(), data, "image/png", cfg)
	if err != nil {
		t.Fatalf("detect half bilinear: %v", err)
	}
	if det.Detected {
		t.Fatalf("synthetic texture half-bilinear unexpectedly detected; update capability docs and remove known-limit expectation: %s", diagnoseDetection(det))
	}
}

func TestDownscaleHalfAt512SourceDoesNotClaimDetection(t *testing.T) {
	cfg := testConfig()
	cfg.Strength = StrengthRobust
	const sourceDim = MinImageDim * 2 // reason: 512px source resized by 0.5x lands exactly at the detection image-size floor.
	src := textureImage(sourceDim, sourceDim)
	res, err := Embed(context.Background(), src, "image/png", testPayload(), cfg)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatal(err)
	}
	data := encodePNG(resizeImageBilinear(img, MinImageDim, MinImageDim))
	det, err := Detect(context.Background(), data, "image/png", cfg)
	if err != nil {
		t.Fatalf("detect half downscale: %v", err)
	}
	if det.Detected {
		t.Fatalf("half downscale claimed detection at unsupported 512 source boundary: %s", diagnoseDetection(det))
	}
}

func TestDownscaleHalfNearestAt512SourceDoesNotClaimDetection(t *testing.T) {
	cfg := testConfig()
	cfg.Strength = StrengthRobust
	const sourceDim = MinImageDim * 2 // reason: 512px source resized by 0.5x lands exactly at the detection image-size floor.
	src := textureImage(sourceDim, sourceDim)
	res, err := Embed(context.Background(), src, "image/png", testPayload(), cfg)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatal(err)
	}
	data := encodePNG(resizeImageNearest(img, MinImageDim, MinImageDim))
	det, err := Detect(context.Background(), data, "image/png", cfg)
	if err != nil {
		t.Fatalf("detect half downscale: %v", err)
	}
	if det.Detected {
		t.Fatalf("nearest half downscale claimed detection at unsupported 512 source boundary: %s", diagnoseDetection(det))
	}
}

func TestDownscaleHalfFailsWithSourceBelow512(t *testing.T) {
	cfg := testConfig()
	cfg.Strength = StrengthRobust
	const sourceDim = 400 // reason: below the 512 source needed for 0.5x detection coverage; 0.5x leaves the resized image at 200, below MinImageDim.
	src := textureImage(sourceDim, sourceDim)
	res, err := Embed(context.Background(), src, "image/png", testPayload(), cfg)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatal(err)
	}
	resized := encodePNG(resizeImageBilinear(img, sourceDim/2, sourceDim/2))
	_, err = Detect(context.Background(), resized, "image/png", cfg)
	if !errors.Is(err, ErrImageTooSmall) {
		t.Fatalf("expected ErrImageTooSmall for 200px image, got %v", err)
	}
}

func TestDownscaleThreeQuartersAt342SourceDoesNotClaimDetection(t *testing.T) {
	cfg := testConfig()
	cfg.Strength = StrengthRobust
	const sourceDim = 342 // reason: smallest integer source where 0.75x rounds above MinImageDim, but measured decode support still needs more tiles.
	const scale = 0.75    // reason: default downscale sweep includes a three-quarter source-size hypothesis.
	resizedDim := int(math.Round(sourceDim * scale))
	if resizedDim != MinImageDim+1 {
		t.Fatalf("test setup resized dim got %d want %d", resizedDim, MinImageDim+1)
	}
	src := textureImage(sourceDim, sourceDim)
	res, err := Embed(context.Background(), src, "image/png", testPayload(), cfg)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatal(err)
	}
	data := encodePNG(resizeImageBilinear(img, resizedDim, resizedDim))
	det, err := Detect(context.Background(), data, "image/png", cfg)
	if err != nil {
		t.Fatalf("detect three-quarter downscale: %v", err)
	}
	if det.Detected {
		t.Fatalf("three-quarter downscale claimed detection at unsupported 342px source boundary: %s", diagnoseDetection(det))
	}
}

func TestDownscaleThreeQuartersRequires384Source(t *testing.T) {
	cfg := testConfig()
	cfg.Strength = StrengthRobust
	const sourceDim = 384 // reason: measured 0.75x Robust embed/detect floor for 64px tiles is 6x6 source tiles.
	const scale = 0.75    // reason: default downscale sweep includes a three-quarter source-size hypothesis.
	resizedDim := int(math.Round(sourceDim * scale))
	src := textureImage(sourceDim, sourceDim)
	res, err := Embed(context.Background(), src, "image/png", testPayload(), cfg)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatal(err)
	}
	data := encodePNG(resizeImageBilinear(img, resizedDim, resizedDim))
	det, err := Detect(context.Background(), data, "image/png", cfg)
	if err != nil {
		t.Fatalf("detect three-quarter downscale: %v", err)
	}
	if !det.Detected {
		t.Fatalf("three-quarter downscale detect failed: %s", diagnoseDetection(det))
	}
}

func TestLargeImageDownscale(t *testing.T) {
	cfg := testConfig()
	cfg.Strength = StrengthRobust
	src := textureImage(2048, 2048)
	res, err := Embed(context.Background(), src, "image/png", testPayload(), cfg)
	if err != nil {
		t.Fatalf("embed large image: %v", err)
	}
	if tileSizeForDimensions(2048, 2048) != LargeTileSize {
		t.Fatal("large image did not select large tile size")
	}
	img, err := png.Decode(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatal(err)
	}
	data := encodePNG(resizeImageBilinear(img, 1024, 1024))
	det, err := Detect(context.Background(), data, "image/png", cfg)
	if err != nil {
		t.Fatalf("detect large downscale: %v", err)
	}
	if !det.Detected {
		t.Fatalf("large downscale detect failed: %s", diagnoseDetection(det))
	}
	if math.Abs(det.BestScale-0.5) > 0.12 {
		t.Fatalf("scale estimate got %.3f want about 0.5", det.BestScale)
	}
}

func TestProductionDecodeRecovers12Erasures(t *testing.T) {
	cfg := testConfig()
	cfg.Strength = StrengthRobust
	payload := testPayload()
	digest, err := computeDigest(&payload, cfg.ActiveKey.Secret)
	if err != nil {
		t.Fatalf("compute digest: %v", err)
	}
	wantFrame := ecc.Frame(byte(PayloadVersion), keyIDBytes(cfg.ActiveKey.ID), digest)
	detectedFrame := append([]byte(nil), wantFrame...)
	lowConfidenceBytes := make(map[int]bool)
	for i := 0; i < 12; i++ {
		lowConfidenceBytes[i] = true
		detectedFrame[i] ^= 0xff
	}
	// Pin this to the RS boundary: 12 erasures plus two byte errors must recover.
	for _, i := range []int{12, 13} {
		detectedFrame[i] ^= 0xff
	}

	data := forgedDetectionPNG(detectedFrame, lowConfidenceBytes, cfg.ActiveKey)
	det, err := Detect(context.Background(), data, "image/png", cfg)
	if err != nil {
		t.Fatalf("detect forged frame: %v", err)
	}
	if !det.Detected {
		t.Fatalf("forged frame detect failed: %s", diagnoseDetection(det))
	}
	if det.PayloadDigest != fmt.Sprintf("%x", digest) {
		t.Fatalf("payload digest got %s want %x", det.PayloadDigest, digest)
	}
}

func TestJPEGQualitySweep(t *testing.T) {
	for _, q := range []int{95} {
		t.Run(fmt.Sprintf("q%d", q), func(t *testing.T) {
			cfg := testConfig()
			cfg.Strength = StrengthRobust
			src := textureImage(512, 512)
			res, err := Embed(context.Background(), src, "image/png", testPayload(), cfg)
			if err != nil {
				t.Fatalf("embed: %v", err)
			}
			img, err := png.Decode(bytes.NewReader(res.Data))
			if err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
				t.Fatal(err)
			}
			det, err := Detect(context.Background(), buf.Bytes(), "image/jpeg", cfg)
			if err != nil {
				t.Fatal(err)
			}
			if !det.Detected {
				t.Fatalf("JPEG quality %d did not survive: %s", q, diagnoseDetection(det))
			}
		})
	}
}

func TestFalsePositiveSweep(t *testing.T) {
	cfg := testConfig()
	for _, kind := range []string{"gradient", "dark", "screenshot", "portrait", "noisy"} {
		for i := 0; i < 10; i++ {
			path := filepath.Join("testdata", fmt.Sprintf("%s_%02d.png", kind, i))
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture %s: %v", path, err)
			}
			det, err := Detect(context.Background(), data, "image/png", cfg)
			if err != nil {
				t.Fatalf("detect fixture %s: %v", path, err)
			}
			if det.Confidence >= 0.50 {
				t.Fatalf("false positive risk on %s: confidence=%v", path, det.Confidence)
			}
		}
	}
}

func TestDetectNoAlphaTilesPopulatesDetails(t *testing.T) {
	cfg := testConfig()
	img := image.NewNRGBA(image.Rect(0, 0, MinImageDim, MinImageDim))
	det, err := Detect(context.Background(), encodePNG(img), "image/png", cfg)
	if err != nil {
		t.Fatalf("detect transparent image: %v", err)
	}
	if det.Details == nil {
		t.Fatal("DetectResult.Details is nil")
	}
	for _, key := range []string{"sync_peak_strength", "scale_estimate", "crop_x_pixels", "crop_y_pixels", "tiles_checked"} {
		if _, ok := det.Details[key]; !ok {
			t.Fatalf("DetectResult.Details missing %q: %#v", key, det.Details)
		}
	}
	if det.Details["tiles_checked"] != 0 || det.TilesChecked != 0 {
		t.Fatalf("transparent image checked tiles: field=%d details=%.0f", det.TilesChecked, det.Details["tiles_checked"])
	}
}

func TestTransparentPNGPixelsUnmodified(t *testing.T) {
	cfg := testConfig()
	cfg.Strength = StrengthRobust
	src := transparentTextureImage(512, 512)
	res, err := Embed(context.Background(), src, "image/png", testPayload(), cfg)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	det, err := Detect(context.Background(), res.Data, "image/png", cfg)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if !det.Detected {
		t.Fatalf("detect failed: confidence=%v", det.Confidence)
	}
	before, err := png.Decode(bytes.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	after, err := png.Decode(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			if before.At(x, y) != after.At(x, y) {
				t.Fatalf("transparent pixel modified at %d,%d: before=%v after=%v", x, y, before.At(x, y), after.At(x, y))
			}
		}
	}
}

func TestTransparentNRGBARGBPreserved(t *testing.T) {
	cfg := testConfig()
	cfg.Strength = StrengthRobust
	want := color.NRGBA{R: 200, G: 20, B: 180, A: 0}
	img := image.NewNRGBA(image.Rect(0, 0, 512, 512))
	for y := 0; y < 512; y++ {
		for x := 0; x < 512; x++ {
			if x < 64 && y < 64 {
				img.SetNRGBA(x, y, want)
				continue
			}
			r := 110 + 60*math.Sin(float64(x)*0.18) + 20*math.Cos(float64(y)*0.07)
			g := 130 + 50*math.Cos(float64(x+y)*0.11) + 15*math.Sin(float64(x-y)*0.05)
			b := 100 + 40*math.Sin(float64(x)*0.22+float64(y)*0.13) + 25*math.Cos(float64(y)*0.03)
			img.SetNRGBA(x, y, color.NRGBA{R: clip255(r), G: clip255(g), B: clip255(b), A: 255})
		}
	}
	res, err := Embed(context.Background(), encodePNG(img), "image/png", testPayload(), cfg)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	decoded, err := png.Decode(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 64; y += 11 {
		for x := 0; x < 64; x += 13 {
			got := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
			if got != want {
				t.Fatalf("transparent NRGBA pixel changed at %d,%d: got=%v want=%v", x, y, got, want)
			}
		}
	}
}

func TestLowAlphaRGBPreserved(t *testing.T) {
	cfg := testConfig()
	cfg.Strength = StrengthRobust
	want := color.NRGBA{R: 200, G: 20, B: 180, A: 1} // reason: regression pixel is below the alpha visibility threshold but carries hidden RGB.
	const lowAlphaDim = MinImageDim * 2              // reason: robust embedding needs enough opaque texture around the low-alpha sample.
	const lowAlphaX = 0                              // reason: top-left pixel is easy to resample after PNG round trip.
	const lowAlphaY = 0                              // reason: top-left pixel is easy to resample after PNG round trip.
	img := image.NewNRGBA(image.Rect(0, 0, lowAlphaDim, lowAlphaDim))
	for y := 0; y < lowAlphaDim; y++ {
		for x := 0; x < lowAlphaDim; x++ {
			r := 110 + 60*math.Sin(float64(x)*0.18) + 20*math.Cos(float64(y)*0.07)
			g := 130 + 50*math.Cos(float64(x+y)*0.11) + 15*math.Sin(float64(x-y)*0.05)
			b := 100 + 40*math.Sin(float64(x)*0.22+float64(y)*0.13) + 25*math.Cos(float64(y)*0.03)
			img.SetNRGBA(x, y, color.NRGBA{R: clip255(r), G: clip255(g), B: clip255(b), A: 255})
		}
	}
	img.SetNRGBA(lowAlphaX, lowAlphaY, want)
	res, err := Embed(context.Background(), encodePNG(img), "image/png", testPayload(), cfg)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	decoded, err := png.Decode(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatal(err)
	}
	got := color.NRGBAModel.Convert(decoded.At(lowAlphaX, lowAlphaY)).(color.NRGBA)
	if got != want {
		t.Fatalf("low-alpha pixel got=%v want=%v", got, want)
	}
}

func TestPalettedPNGTransparentPixelsPreserved(t *testing.T) {
	cfg := testConfig()
	cfg.Strength = StrengthRobust
	want := color.NRGBA{R: 200, G: 100, B: 50, A: 0} // reason: non-black transparent RGB catches premultiplication loss.
	const palettedPNGDim = MinImageDim * 2           // reason: robust embedding needs enough opaque tiles around the transparent patch.
	const patchDivisor = 4                           // reason: leaves most tiles opaque while still sampling a visible tRNS region.
	const paletteEntries = 256                       // reason: paletted PNG color indexes are byte-sized.
	const transparentIndex = 0                       // reason: index zero carries the tRNS regression color.
	const firstOpaqueIndex = 1                       // reason: opaque texture entries must not overwrite the transparent regression index.
	const opaqueAlpha = 255                          // reason: non-regression palette entries should contribute normal embedding texture.
	const blueStepDivisor = 2                        // reason: creates a different opaque blue sequence without another lookup table.
	const sampleYStep = 11                           // reason: samples multiple transparent rows without checking every pixel.
	const sampleXStep = 13                           // reason: samples columns at a different cadence from rows.

	palette := make(color.Palette, paletteEntries)
	palette[transparentIndex] = want
	for i := firstOpaqueIndex; i < len(palette); i++ {
		palette[i] = color.NRGBA{R: uint8(i), G: uint8(len(palette) - i), B: uint8(i ^ (i / blueStepDivisor)), A: opaqueAlpha}
	}
	img := image.NewPaletted(image.Rect(0, 0, palettedPNGDim, palettedPNGDim), palette)
	patchDim := palettedPNGDim / patchDivisor
	for y := 0; y < palettedPNGDim; y++ {
		for x := 0; x < palettedPNGDim; x++ {
			if x < patchDim && y < patchDim {
				img.SetColorIndex(x, y, transparentIndex)
				continue
			}
			idx := firstOpaqueIndex + (x+y+x*y/palettedPNGDim)%(len(palette)-firstOpaqueIndex)
			img.SetColorIndex(x, y, uint8(idx))
		}
	}
	src := encodePNG(img)
	decodedSrc, err := png.Decode(bytes.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := decodedSrc.(*image.Paletted); !ok {
		t.Fatalf("source decoded as %T, want *image.Paletted", decodedSrc)
	}
	if got := color.NRGBAModel.Convert(decodedSrc.At(0, 0)).(color.NRGBA); got != want {
		t.Fatalf("source transparent palette got=%v want=%v", got, want)
	}
	res, err := Embed(context.Background(), src, "image/png", testPayload(), cfg)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	decoded, err := png.Decode(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < patchDim; y += sampleYStep {
		for x := 0; x < patchDim; x += sampleXStep {
			got := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
			if got != want {
				t.Fatalf("transparent paletted pixel changed at %d,%d: got=%v want=%v", x, y, got, want)
			}
		}
	}
}

func TestKeyRotationDetectsEmbeddedKey(t *testing.T) {
	keyA := Key{ID: "key-a", Secret: []byte("secret for key a with enough bytes")}
	keyB := Key{ID: "key-b", Secret: []byte("secret for key b with enough bytes")}
	embedCfg := rotationConfig(keyA, keyA)
	src := textureImage(512, 512)
	res, err := Embed(context.Background(), src, "image/png", testPayload(), embedCfg)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	detectCfg := rotationConfig(Key{}, keyA, keyB)
	det, err := Detect(context.Background(), res.Data, "image/png", detectCfg)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if !det.Detected || det.KeyID != keyA.ID {
		t.Fatalf("detect key mismatch: detected=%v keyID=%q confidence=%v", det.Detected, det.KeyID, det.Confidence)
	}
}

func TestDetectUsesActiveKeyOverDetectionKeysSameID(t *testing.T) {
	active := Key{ID: "k1", Secret: []byte("active secret for same-id regression")}
	stale := Key{ID: "k1", Secret: []byte("stale secret for same-id regression")}
	cfg := Config{ActiveKey: active, Strength: StrengthBalanced}
	const testDim = MinImageDim * 2 // reason: keeps this regression image cheap while satisfying the minimum image dimension gate.
	src := textureImage(testDim, testDim)
	res, err := Embed(context.Background(), src, "image/png", testPayload(), cfg)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}

	cfg.DetectionKeys = []Key{stale, active}
	keys := cfg.detectionKeys()
	const activeOnlyKeyCount = 1 // reason: same-ID detection keys are filtered so ActiveKey is the only returned key.
	if len(keys) != activeOnlyKeyCount || keys[0].ID != active.ID || !bytes.Equal(keys[0].Secret, active.Secret) {
		t.Fatalf("detection keys did not prefer active key: %#v", keys)
	}
	det, err := Detect(context.Background(), res.Data, "image/png", cfg)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if !det.Detected || det.KeyID != active.ID || det.PayloadDigest != res.PayloadDigest {
		t.Fatalf("detect mismatch: detected=%v keyID=%q digest=%q want %q", det.Detected, det.KeyID, det.PayloadDigest, res.PayloadDigest)
	}
}

func TestVerifyUsesRotatedDetectionKey(t *testing.T) {
	keyA := Key{ID: "key-a", Secret: []byte("secret for key a with enough bytes")}
	keyB := Key{ID: "key-b", Secret: []byte("secret for key b with enough bytes")}
	payload := testPayload()
	src := textureImage(512, 512)
	res, err := Embed(context.Background(), src, "image/png", payload, rotationConfig(keyA, keyA))
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	verifyCfg := rotationConfig(keyB, keyA)
	det, err := Verify(context.Background(), res.Data, "image/png", payload, verifyCfg)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !det.Detected || det.KeyID != keyA.ID {
		t.Fatalf("verify key mismatch: detected=%v keyID=%q confidence=%v", det.Detected, det.KeyID, det.Confidence)
	}
}

func TestVoteWeightUniformity(t *testing.T) {
	for _, tileSize := range []int{DefaultTileSize, LargeTileSize} {
		data := textureImage(tileSize*8, tileSize*8)
		plane := luminancePlaneFromPNG(t, data)
		all := tiles.Iterate(plane, tileSize)
		pairsPerBlock := pairsPerSubBlock(tileSize, frameBits)
		bitsPerTile := usableBitsPerTile(tileSize, pairsPerBlock, frameBits)
		idxs := selectTiles(all, frameBits, bitsPerTile, minEmbedTilesFor(StrengthRobust, len(all)))
		if len(idxs) == 0 {
			t.Fatalf("tile size %d selected no tiles", tileSize)
		}
		counts := make([]int, frameBits)
		subBlocks := subBlocksPerTile(tileSize)
		rawBits := pairsPerBlock * subBlocks
		for _, ti := range idxs {
			tile := all[ti]
			for subIdx := 0; subIdx < subBlocks; subIdx++ {
				for j := range derivePairs(testConfig().ActiveKey, tile.Index, subIdx, pairsPerBlock, robustFreqPositions) {
					bitIdx, ok := tileBitIndex(subIdx*pairsPerBlock+j, rawBits, bitsPerTile)
					if !ok {
						continue
					}
					counts[bitIdx%frameBits]++
				}
			}
		}
		want := counts[0]
		if want == 0 {
			t.Fatalf("tile size %d did not write bit 0", tileSize)
		}
		for bit, got := range counts {
			if got != want {
				t.Fatalf("tile size %d bit %d vote count got %d want %d", tileSize, bit, got, want)
			}
		}
	}
}

func TestLargeImageRoundTrip(t *testing.T) {
	cfg := testConfig()
	cfg.Strength = StrengthBalanced
	src := textureImage(2048, 2048)
	res, err := Embed(context.Background(), src, "image/png", testPayload(), cfg)
	if err != nil {
		t.Fatalf("embed large image: %v", err)
	}
	if res.Quality.Tiles == 0 || tileSizeForDimensions(2048, 2048) != LargeTileSize {
		t.Fatalf("large tile path not used: tiles=%d", res.Quality.Tiles)
	}
	det, err := Detect(context.Background(), res.Data, "image/png", cfg)
	if err != nil {
		t.Fatalf("detect large image: %v", err)
	}
	if !det.Detected {
		t.Fatalf("large image detect failed: %s", diagnoseDetection(det))
	}
}

func TestCalibrationCorpusThresholds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipped in short mode; corpus calibration takes ~30s")
	}
	cfg := testConfig()
	paths := fixturePaths(t)
	var falseConf, trueConf []float64
	var falseSignal, trueSignal []float64
	var trueBitConfidence []float64
	var unmarkedTileMagnitude, markedTileMagnitude []float64
	var erasureRatios, eccRatios []float64
	var robustSyncPeakRatio []float64
	var flatSyncActive, flatSyncPixels int
	scaleSweep := []float64{0.3, 0.5, 0.75, 1.0, 1.25, 1.5}
	nearestScaleSweep := newScaleSweepStats()
	bilinearScaleSweep := newScaleSweepStats()
	buckets := make([]float64, bitConfidenceBucketCount)
	var bucketSamples float64
	var embedFailures int
	var embedAttempts int
	var scaleEmbedFailures int
	var scaleEmbedAttempts int
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read fixture %s: %v", path, err)
		}
		det, err := Detect(context.Background(), data, "image/png", cfg)
		if err != nil {
			t.Fatalf("detect fixture %s: %v", path, err)
		}
		falseConf = append(falseConf, det.Confidence)
		plane := luminancePlaneFromPNG(t, data)
		active, pixels := syncFlatActivationStats(plane)
		flatSyncActive += active
		flatSyncPixels += pixels
		unmarkedStats := calibrationRecoveryStats(t, data, cfg.ActiveKey, StrengthBalanced)
		falseSignal = append(falseSignal, unmarkedStats.signal)
		unmarkedTileMagnitude = append(unmarkedTileMagnitude, unmarkedStats.tileMagnitude...)
		for _, profile := range []StrengthProfile{StrengthInvisible, StrengthBalanced, StrengthRobust} {
			embedAttempts++
			embedCfg := cfg
			embedCfg.Strength = profile
			res, err := Embed(context.Background(), data, "image/png", testPayload(), embedCfg)
			if err != nil {
				embedFailures++
				t.Logf("calibration embed failed fixture=%s profile=%s err=%v", path, profile, err)
				if profile == StrengthRobust {
					scaleEmbedAttempts++
					scaleEmbedFailures++
					recordScaleSweepFailure(scaleSweep, &nearestScaleSweep, &bilinearScaleSweep)
				}
				continue
			}
			det, err := Detect(context.Background(), res.Data, "image/png", embedCfg)
			if err != nil {
				t.Fatalf("detect marked fixture %s profile %s: %v", path, profile, err)
			}
			if !det.Detected {
				t.Fatalf("marked fixture %s profile %s missed: %s", path, profile, diagnoseDetection(det))
			}
			trueConf = append(trueConf, det.Confidence)
			markedPlane := luminancePlaneFromPNG(t, res.Data)
			if profile == StrengthRobust {
				peak, noise := syncDiffPeak2D(plane.Pixels, markedPlane.Pixels, plane.W, plane.H)
				robustSyncPeakRatio = append(robustSyncPeakRatio, peak/max(noise, 1e-9))
				scaleEmbedAttempts++
				if !recordScaleSweep(t, path, data, embedCfg, scaleSweep, &nearestScaleSweep, &bilinearScaleSweep) {
					scaleEmbedFailures++
				}
			}
			markedStats := calibrationRecoveryStats(t, res.Data, embedCfg.ActiveKey, profile)
			trueSignal = append(trueSignal, markedStats.signal)
			trueBitConfidence = append(trueBitConfidence, markedStats.bitConfidence...)
			markedTileMagnitude = append(markedTileMagnitude, markedStats.selectedTileMagnitude...)
			erasureRatios = append(erasureRatios, markedStats.lowestByteToBitMedianRatio)
			eccRatios = append(eccRatios, markedStats.byteToByteMedianRatios...)
			for i := 0; i < bitConfidenceBucketCount; i++ {
				buckets[i] += det.Details[fmt.Sprintf("bit_confidence_bucket_%d", i)]
			}
			bucketSamples++
		}
	}
	sort.Float64s(falseConf)
	sort.Float64s(trueConf)
	sort.Float64s(falseSignal)
	sort.Float64s(trueSignal)
	sort.Float64s(trueBitConfidence)
	sort.Float64s(unmarkedTileMagnitude)
	sort.Float64s(markedTileMagnitude)
	sort.Float64s(erasureRatios)
	sort.Float64s(eccRatios)
	sort.Float64s(robustSyncPeakRatio)
	fp95 := percentile(falseConf, 0.95)
	tp25 := percentile(trueConf, 0.25)
	tp50 := percentile(trueConf, 0.50)
	tp05 := percentile(trueConf, 0.05)
	falseSignal05 := percentile(falseSignal, 0.05)
	falseSignal95 := percentile(falseSignal, 0.95)
	trueSignal05 := percentile(trueSignal, 0.05)
	trueSignal50 := percentile(trueSignal, 0.50)
	trueBit50 := percentile(trueBitConfidence, 0.50)
	trueBit95 := percentile(trueBitConfidence, 0.95)
	unmarkedTile95 := percentile(unmarkedTileMagnitude, 0.95)
	markedTile05 := percentile(markedTileMagnitude, 0.05)
	erasureTarget := percentile(erasureRatios, 0.05)
	eccRatio25 := percentile(eccRatios, 0.25)
	eccRatio75 := percentile(eccRatios, 0.75)
	robustSyncPeak05 := percentile(robustSyncPeakRatio, 0.05)
	for i := range buckets {
		buckets[i] /= max(1, bucketSamples)
	}
	lowNoiseFloorTarget := falseSignal05
	highSignalFloorTarget := trueSignal50
	possibleTarget := (fp95 + tp50) / 2
	flatActivationRatio := float64(flatSyncActive) / float64(max(1, flatSyncPixels))
	nearestScaleLow, nearestScaleHigh, nearestScaleOK := successfulScaleRange(scaleSweep, nearestScaleSweep)
	_, bilinearScaleHigh, bilinearScaleOK := successfulScaleRange(scaleSweep, bilinearScaleSweep)
	t.Logf("calibration fixtures=%d profiles=3 embed_failures=%d false_conf_p95=%.3f true_conf_p05=%.3f true_conf_p25=%.3f true_conf_p50=%.3f false_signal_p05=%.4f false_signal_p95=%.4f true_signal_p05=%.4f true_signal_p50=%.4f true_bit_p50=%.4f true_bit_p95=%.4f unmarked_tile_p95=%.4f marked_tile_p05=%.4f erasure_ratio_p05=%.4f ecc_ratio_p25=%.4f ecc_ratio_p75=%.4f flat_sync_activation=%.4f robust_sync_diff_peak_p05=%.3f nearest_scale_successes=%v bilinear_scale_successes=%v buckets=%v", len(paths), embedFailures, fp95, tp05, tp25, tp50, falseSignal05, falseSignal95, trueSignal05, trueSignal50, trueBit50, trueBit95, unmarkedTile95, markedTile05, erasureTarget, eccRatio25, eccRatio75, flatActivationRatio, robustSyncPeak05, nearestScaleSweep.successes, bilinearScaleSweep.successes, buckets)
	if ratio, ok := embedFailureRatio(embedFailures, embedAttempts); !ok || ratio > maxCalibrationEmbedFailureRatio {
		t.Fatalf("calibration embed failure ratio %.2f%% exceeds %.2f%%: failures=%d attempts=%d", ratio*100, maxCalibrationEmbedFailureRatio*100, embedFailures, embedAttempts)
	}
	if ratio, ok := embedFailureRatio(scaleEmbedFailures, scaleEmbedAttempts); !ok || ratio > maxCalibrationScaleSweepEmbedFailureRatio {
		t.Fatalf("scale sweep embed failure ratio %.2f%% exceeds %.2f%%: failures=%d attempts=%d", ratio*100, maxCalibrationScaleSweepEmbedFailureRatio*100, scaleEmbedFailures, scaleEmbedAttempts)
	}
	if fp95 >= detectionPossibleThreshold {
		t.Fatalf("false-positive confidence p95 %.3f must stay below possible threshold", fp95)
	}
	if tp05 < detectionDetectedThreshold {
		t.Fatalf("true-positive confidence p05 %.3f must meet detection threshold", tp05)
	}
	assertWithin30Percent(t, "lowNoiseFloor", lowNoiseFloor, lowNoiseFloorTarget)
	assertWithin30Percent(t, "highSignalFloor", highSignalFloor, highSignalFloorTarget)
	assertWithin30Percent(t, "tileMagnitudeGate", tileMagnitudeGate, unmarkedTile95)
	assertWithin30Percent(t, "erasureMedianFactor", erasureMedianFactor, erasureTarget)
	assertWithin30Percent(t, "detectionPossibleThreshold", detectionPossibleThreshold, possibleTarget)
	assertWithin30Percent(t, "detectionDetectedThreshold", detectionDetectedThreshold, tp25)
	if ecc.ConfidenceErasureFactor < eccRatio25*0.70 || ecc.ConfidenceErasureFactor > eccRatio75*1.30 {
		t.Fatalf("ECC confidence factor %.3f drifted more than 30%% outside marked byte/median ratio p25 %.3f and p75 %.3f", ecc.ConfidenceErasureFactor, eccRatio25, eccRatio75)
	}
	if flatActivationRatio >= 0.05 {
		t.Fatalf("sync texture mask activated on %.2f%% of corpus flat-region pixels, want < 5%%", flatActivationRatio*100)
	}
	if robustSyncPeak05 < 5 {
		t.Fatalf("robust sync FFT diff peak p05 %.3f must be at least 5x the unmarked diff baseline", robustSyncPeak05)
	}
	if !nearestScaleOK {
		t.Fatalf("nearest scale sweep had no successful corpus recovery points: successes=%v trials=%v", nearestScaleSweep.successes, nearestScaleSweep.trials)
	}
	if !bilinearScaleOK {
		t.Fatalf("bilinear scale sweep had no successful corpus recovery points: successes=%v trials=%v", bilinearScaleSweep.successes, bilinearScaleSweep.trials)
	}
	const claimedHalfScale = 0.5          // reason: README/NOTICE/CHANGELOG claim 0.5x robust downscale detection.
	const claimedThreeQuarterScale = 0.75 // reason: README/NOTICE/CHANGELOG claim 0.75x bilinear robust downscale detection.
	assertClaimedScaleSuccess(t, claimedHalfScale, "nearest", nearestScaleSweep)
	assertClaimedScaleSuccess(t, claimedThreeQuarterScale, "bilinear", bilinearScaleSweep)
	assertWithin30Percent(t, "syncScaleClampLow nearest", syncScaleClampLow, nearestScaleLow)
	assertWithin30Percent(t, "syncScaleClampHigh nearest", syncScaleClampHigh, nearestScaleHigh)
	assertWithin30Percent(t, "syncScaleClampHigh bilinear", syncScaleClampHigh, bilinearScaleHigh)
}

type calibrationStats struct {
	signal                     float64
	bitConfidence              []float64
	tileMagnitude              []float64
	rawTileMagnitude           []float64
	selectedTileMagnitude      []float64
	lowestByteToBitMedianRatio float64
	byteToByteMedianRatios     []float64
}

func calibrationRecoveryStats(t *testing.T, data []byte, key Key, profile StrengthProfile) calibrationStats {
	t.Helper()
	payload := testPayload()
	digest, err := computeDigest(&payload, key.Secret)
	if err != nil {
		t.Fatalf("compute calibration digest: %v", err)
	}
	expectedBits := ecc.BitsOf(ecc.Frame(byte(PayloadVersion), keyIDBytes(key.ID), digest))
	plane := luminancePlaneFromPNG(t, data)
	tileSize := tileSizeForDimensions(plane.W, plane.H)
	all, err := shiftedTiles(context.Background(), plane, tileSize, 0, 0)
	if err != nil {
		t.Fatalf("shift calibration tiles: %v", err)
	}
	idxs := alphaGatedTiles(all)
	if len(idxs) == 0 {
		return calibrationStats{}
	}
	pairsPerBlock := pairsPerSubBlock(tileSize, frameBits)
	selected := selectDetectTiles(all, idxs, minEmbedTilesFor(profile, len(all)))
	var best calibrationStats
	for _, positions := range detectionPositionSets() {
		allStats := calibrationStatsForIdxs(plane, all, idxs, pairsPerBlock, key, positions, expectedBits)
		selectedStats := calibrationStatsForIdxs(plane, all, selected, pairsPerBlock, key, positions, expectedBits)
		allStats.selectedTileMagnitude = selectedStats.rawTileMagnitude
		if allStats.signal > best.signal {
			best = allStats
		}
	}
	return best
}

func calibrationStatsForIdxs(y *tiles.Plane, all []tiles.Tile, idxs []int, pairsPerBlock int, key Key, positions [][2]int, expectedBits []uint8) calibrationStats {
	acc := make([]float64, frameBits)
	tileMagnitude := make([]float64, 0, len(idxs))
	rawTileMagnitude := make([]float64, 0, len(idxs))
	supporting := 0
	for _, ti := range idxs {
		votes := recoverTileVotes(y.Pixels, y.W, &all[ti], pairsPerBlock, key, positions)
		var mag float64
		var aligned float64
		for j, v := range votes {
			mag += math.Abs(v)
			if expectedBits[j%len(expectedBits)] == 1 {
				aligned += v
			} else {
				aligned -= v
			}
		}
		meanAbsMag := mag / float64(max(1, len(votes)))
		tileMagnitude = append(tileMagnitude, aligned/float64(max(1, len(votes))))
		rawTileMagnitude = append(rawTileMagnitude, meanAbsMag)
		if meanAbsMag < tileMagnitudeGate {
			continue
		}
		supporting++
		for j, v := range votes {
			acc[j%len(acc)] += v
		}
	}
	bitConfidence := make([]float64, frameBits)
	var totalMag float64
	for i, v := range acc {
		totalMag += math.Abs(v)
		if supporting > 0 {
			bitConfidence[i] = math.Abs(v) / float64(supporting)
		}
	}
	var signal float64
	if supporting > 0 {
		signal = totalMag / float64(frameBits*supporting)
	}
	lowestByteToBitMedianRatio, byteToByteMedianRatios := calibrationByteConfidenceRatios(bitConfidence)
	return calibrationStats{
		signal:                     signal,
		bitConfidence:              bitConfidence,
		tileMagnitude:              tileMagnitude,
		rawTileMagnitude:           rawTileMagnitude,
		lowestByteToBitMedianRatio: lowestByteToBitMedianRatio,
		byteToByteMedianRatios:     byteToByteMedianRatios,
	}
}

func calibrationByteConfidenceRatios(bitConfidence []float64) (float64, []float64) {
	bitMedian := medianPositive(bitConfidence)
	byteConfidence := make([]float64, ecc.FrameSize)
	for i := 0; i < ecc.FrameSize; i++ {
		for b := 0; b < 8; b++ {
			byteConfidence[i] += bitConfidence[i*8+b]
		}
		byteConfidence[i] /= 8
	}
	sortedBytes := append([]float64(nil), byteConfidence...)
	sort.Float64s(sortedBytes)
	byteMedian := percentile(sortedBytes, 0.50)
	var lowestByteToBitMedianRatio float64
	if bitMedian > 0 {
		lowestByteToBitMedianRatio = sortedBytes[0] / bitMedian
	}
	byteToByteMedianRatios := make([]float64, 0, len(byteConfidence))
	for _, v := range byteConfidence {
		if byteMedian > 0 {
			byteToByteMedianRatios = append(byteToByteMedianRatios, v/byteMedian)
		}
	}
	return lowestByteToBitMedianRatio, byteToByteMedianRatios
}

func assertWithin30Percent(t *testing.T, name string, got, want float64) {
	t.Helper()
	if want == 0 {
		if got != 0 {
			t.Fatalf("%s %.4f drifted from zero target; relative drift is undefined", name, got)
		}
		return
	}
	if math.Abs(got-want)/want > 0.30+1e-12 {
		t.Fatalf("%s %.4f drifted more than 30%% from target %.4f", name, got, want)
	}
}

const maxCalibrationEmbedFailureRatio = 0.30           // reason: corpus tolerance for embed failures across realistic textures.
const maxCalibrationScaleSweepEmbedFailureRatio = 0.40 // reason: 1024px scale probes re-embed resized fixtures with less usable capacity.
const corpusSuccessRate = 0.80                         // reason: permits at most 20% corpus misses while preserving broad-scale calibration coverage.

func embedFailureRatio(failures, attempts int) (float64, bool) {
	if attempts == 0 {
		return 0, false
	}
	return float64(failures) / float64(attempts), true
}

func TestCalibrationRejectsHighEmbedFailureRatio(t *testing.T) {
	ratio, ok := embedFailureRatio(31, 100)
	if !ok {
		t.Fatal("expected ratio for nonzero attempts")
	}
	if ratio <= maxCalibrationEmbedFailureRatio {
		t.Fatalf("high embed failure ratio %.2f should exceed %.2f", ratio, maxCalibrationEmbedFailureRatio)
	}
}

func syncFlatActivationStats(p *tiles.Plane) (active, total int) {
	const nearFlatMargin = 1.30 // reason: defines the near-flat corpus slice used to enforce the 5% sync-activation ceiling.
	for py := 0; py < p.H; py++ {
		for px := 0; px < p.W; px++ {
			variance, edgeMean := syncLocalTextureStats(p, px, py)
			if variance >= syncTextureVarianceThreshold*nearFlatMargin && edgeMean >= syncTextureEdgeThreshold*nearFlatMargin {
				continue
			}
			total++
			if syncTextureMask(p, px, py) > 0 {
				active++
			}
		}
	}
	return active, total
}

func syncLocalTextureStats(p *tiles.Plane, px, py int) (variance, edgeMean float64) {
	x0, x1 := max(0, px-syncTextureRadius), min(p.W-1, px+syncTextureRadius)
	y0, y1 := max(0, py-syncTextureRadius), min(p.H-1, py+syncTextureRadius)
	var sum, sumSq, edges float64
	var n int
	for y := y0; y <= y1; y++ {
		row := y * p.W
		for x := x0; x <= x1; x++ {
			v := p.Pixels[row+x]
			sum += v
			sumSq += v * v
			n++
			if x < x1 {
				edges += math.Abs(p.Pixels[row+x+1] - v)
			}
			if y < y1 {
				edges += math.Abs(p.Pixels[(y+1)*p.W+x] - v)
			}
		}
	}
	if n == 0 {
		return 0, 0
	}
	mean := sum / float64(n)
	return max(0, sumSq/float64(n)-mean*mean), edges / float64(n)
}

type scaleSweepStats struct {
	trials    map[float64]int
	successes map[float64]int
}

func newScaleSweepStats() scaleSweepStats {
	return scaleSweepStats{
		trials:    make(map[float64]int),
		successes: make(map[float64]int),
	}
}

func recordScaleSweepFailure(scales []float64, nearest, bilinear *scaleSweepStats) {
	for _, scale := range scales {
		nearest.trials[scale]++
		bilinear.trials[scale]++
	}
}

func recordScaleSweep(t *testing.T, fixture string, data []byte, cfg Config, scales []float64, nearest, bilinear *scaleSweepStats) bool {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode scale fixture %s: %v", fixture, err)
	}
	base := encodePNG(resizeImageNearest(img, 1024, 1024))
	res, err := Embed(context.Background(), base, "image/png", testPayload(), cfg)
	if err != nil {
		t.Logf("scale sweep omitted fixture=%s err=%v", fixture, err)
		recordScaleSweepFailure(scales, nearest, bilinear)
		return false
	}
	marked, err := png.Decode(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatalf("decode scale marked fixture %s: %v", fixture, err)
	}
	b := marked.Bounds()
	for _, scale := range scales {
		w := max(1, int(math.Round(float64(b.Dx())*scale)))
		h := max(1, int(math.Round(float64(b.Dy())*scale)))
		recordScaledDetection(t, fixture, "nearest", marked, w, h, scale, cfg, nearest, resizeImageNearest)
		recordScaledDetection(t, fixture, "bilinear", marked, w, h, scale, cfg, bilinear, resizeImageBilinear)
	}
	return true
}

func recordScaledDetection(t *testing.T, fixture, resampler string, marked image.Image, w, h int, scale float64, cfg Config, stats *scaleSweepStats, resize func(image.Image, int, int) image.Image) {
	t.Helper()
	stats.trials[scale]++
	det, err := Detect(context.Background(), encodeFastPNG(resize(marked, w, h)), "image/png", cfg)
	if err != nil {
		t.Fatalf("detect %s scaled fixture %s scale %.2f: %v", resampler, fixture, scale, err)
	}
	if det.Detected {
		stats.successes[scale]++
	}
}

func successfulScaleRange(scales []float64, stats scaleSweepStats) (low, high float64, ok bool) {
	for _, scale := range scales {
		if stats.trials[scale] == 0 || float64(stats.successes[scale])/float64(stats.trials[scale]) < corpusSuccessRate {
			continue
		}
		if !ok {
			low = scale
			ok = true
		}
		high = scale
	}
	return low, high, ok
}

func assertClaimedScaleSuccess(t *testing.T, scale float64, resampler string, stats scaleSweepStats) {
	t.Helper()
	trials := stats.trials[scale]
	if trials == 0 {
		t.Fatalf("scale=%.2f resampler=%s had no corpus trials", scale, resampler)
	}
	rate := float64(stats.successes[scale]) / float64(trials)
	if rate < corpusSuccessRate {
		t.Fatalf("scale=%.2f resampler=%s corpus success rate %.3f below %.3f", scale, resampler, rate, corpusSuccessRate)
	}
}

func TestSyncInvisibleOnFlatImage(t *testing.T) {
	cfg := testConfig()
	cfg.Strength = StrengthBalanced
	src := textureWithFlatPatchImage(512, 512)
	res, err := Embed(context.Background(), src, "image/png", testPayload(), cfg)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	before := luminancePlaneFromPNG(t, src)
	after := luminancePlaneFromPNG(t, res.Data)
	beforeFlat := planeRect(before, image.Rect(16, 16, 112, 112))
	afterFlat := planeRect(after, image.Rect(16, 16, 112, 112))
	ssim := quality.SSIM(beforeFlat.Pixels, afterFlat.Pixels, beforeFlat.W, beforeFlat.H)
	if ssim < 0.998 {
		t.Fatalf("flat-region SSIM got %.6f want >= 0.998", ssim)
	}
	peak, noise := syncDiffPeak2D(beforeFlat.Pixels, afterFlat.Pixels, beforeFlat.W, beforeFlat.H)
	if peak > noise*8 {
		t.Fatalf("sync-frequency diff peak %.6f exceeds noise floor %.6f", peak, noise)
	}
}

func TestPremultipliedAlphaRoundTrip(t *testing.T) {
	cfg := testConfig()
	cfg.Strength = StrengthRobust
	want := color.NRGBA{R: 220, G: 40, B: 150, A: 128}
	src := semitransparentTextureImage(512, 512, want)
	res, err := Embed(context.Background(), src, "image/png", testPayload(), cfg)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 128; y += 17 {
		for x := 0; x < 128; x += 19 {
			got := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			if absByteDiff(got.R, want.R) > 1 || absByteDiff(got.G, want.G) > 1 || absByteDiff(got.B, want.B) > 1 || absByteDiff(got.A, want.A) > 0 {
				t.Fatalf("semi-transparent color changed at %d,%d: got=%v want=%v", x, y, got, want)
			}
		}
	}
}

func TestSyncBilinearBorderClamp(t *testing.T) {
	src := &tiles.Plane{
		W:      2,
		H:      2,
		Pixels: []float64{10, 20, 200, 220},
		Alpha:  []float64{0.1, 0.2, 0.9, 1.0},
	}
	got, err := resizePlane(context.Background(), src, 4, 4)
	if err != nil {
		t.Fatalf("resize: %v", err)
	}
	wantPixels := map[int]float64{
		0: 10, 1: 12.5, 2: 17.5, 3: 20,
		4: 57.5, 7: 70,
		8: 152.5, 11: 170,
		12: 200, 13: 205, 14: 215, 15: 220,
	}
	wantAlpha := map[int]float64{
		0: 0.1, 1: 0.125, 2: 0.175, 3: 0.2,
		4: 0.3, 7: 0.4,
		8: 0.7, 11: 0.8,
		12: 0.9, 13: 0.925, 14: 0.975, 15: 1.0,
	}
	for idx, want := range wantPixels {
		if math.Abs(got.Pixels[idx]-want) > 1e-9 {
			t.Fatalf("border pixel %d: got %.12f want %.12f", idx, got.Pixels[idx], want)
		}
	}
	for idx, want := range wantAlpha {
		if math.Abs(got.Alpha[idx]-want) > 1e-9 {
			t.Fatalf("border alpha %d: got %.12f want %.12f", idx, got.Alpha[idx], want)
		}
	}
}

func absByteDiff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

func cropImage(src image.Image, x0, y0, x1, y1 int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, x1-x0, y1-y0))
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			dst.Set(x-x0, y-y0, src.At(x, y))
		}
	}
	return dst
}

func resizeImageNearest(src image.Image, w, h int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	sb := src.Bounds()
	for y := 0; y < h; y++ {
		sy := sb.Min.Y + int(float64(y)*float64(sb.Dy())/float64(h))
		for x := 0; x < w; x++ {
			sx := sb.Min.X + int(float64(x)*float64(sb.Dx())/float64(w))
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

func resizeImageBilinear(src image.Image, w, h int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.BiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	return dst
}

func forgedDetectionPNG(frame []byte, lowConfidenceBytes map[int]bool, key Key) []byte {
	const size = 512 // reason: forged detection fixture canvas size, big enough to exercise DefaultTileSize tiling.
	bits := ecc.BitsOf(frame)
	plane := &tiles.Plane{
		W:      size,
		H:      size,
		Pixels: make([]float64, size*size),
		Alpha:  make([]float64, size*size),
	}
	for i := range plane.Pixels {
		plane.Pixels[i] = 128
		plane.Alpha[i] = 1
	}
	all := tiles.Iterate(plane, DefaultTileSize)
	pairsPerBlock := pairsPerSubBlock(DefaultTileSize, frameBits)
	for ti := range all {
		writeForgedTile(plane, &all[ti], bits, lowConfidenceBytes, pairsPerBlock, key)
	}
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		row := y * size
		for x := 0; x < size; x++ {
			v := clip255(plane.Pixels[row+x])
			img.SetRGBA(x, y, color.RGBA{R: v, G: v, B: v, A: 255})
		}
	}
	return encodePNG(img)
}

func writeForgedTile(y *tiles.Plane, t *tiles.Tile, bits []uint8, lowConfidenceBytes map[int]bool, pairsPerBlock int, key Key) {
	subN := t.Size / dct.N
	rawBits := pairsPerBlock * subN * subN
	usableBits := usableBitsPerTile(t.Size, pairsPerBlock, len(bits))
	for sr := 0; sr < subN; sr++ {
		for sc := 0; sc < subN; sc++ {
			subIdx := sr*subN + sc
			var block [dct.N * dct.N]float64
			for j, pr := range derivePairs(key, t.Index, subIdx, pairsPerBlock, robustFreqPositions) {
				bitIdx, ok := tileBitIndex(subIdx*pairsPerBlock+j, rawBits, usableBits)
				if !ok {
					continue
				}
				frameBitIdx := bitIdx % len(bits)
				setForgedPair(&block, pr, bits[frameBitIdx], lowConfidenceBytes[frameBitIdx/8])
			}
			dct.Inverse(&block)
			origin := (t.Y+sr*dct.N)*y.W + (t.X + sc*dct.N)
			storeBlock(y.Pixels, y.Alpha, y.W, origin, &block)
		}
	}
}

func setForgedPair(block *[dct.N * dct.N]float64, pair [2][2]int, bit uint8, lowConfidence bool) {
	a := pair[0][0]*dct.N + pair[0][1]
	b := pair[1][0]*dct.N + pair[1][1]
	if lowConfidence {
		if bit == 1 {
			block[a], block[b] = 18, 12
		} else {
			block[a], block[b] = 12, 18
		}
		return
	}
	if bit == 1 {
		block[a], block[b] = 24, 0
	} else {
		block[a], block[b] = 0, 24
	}
}

func encodePNG(img image.Image) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func encodeFastPNG(img image.Image) []byte {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.NoCompression}
	if err := enc.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func jpegTranscodeSSIM(t *testing.T, data []byte) float64 {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	const jpegProbeQuality = 95 // reason: matches the public JPEG Q95 output capability under test.
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegProbeQuality}); err != nil {
		t.Fatal(err)
	}
	plane := luminancePlaneFromPNG(t, data)
	report, _, err := qualityReportFromEncoded(context.Background(), buf.Bytes(), plane.Pixels, 0)
	if err != nil {
		t.Fatal(err)
	}
	return report.SSIM
}

func fixturePaths(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "*.png"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		t.Fatal("no calibration fixtures found")
	}
	return paths
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Round(p * float64(len(sorted)-1)))
	idx = max(0, min(len(sorted)-1, idx))
	return sorted[idx]
}

func luminancePlaneFromPNG(t *testing.T, data []byte) *tiles.Plane {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	rgba, err := toRGBA(context.Background(), img)
	if err != nil {
		t.Fatal(err)
	}
	plane, _, _, err := splitYCbCr(context.Background(), rgba)
	if err != nil {
		t.Fatal(err)
	}
	return plane
}

func planeRect(src *tiles.Plane, r image.Rectangle) *tiles.Plane {
	w, h := r.Dx(), r.Dy()
	out := &tiles.Plane{
		W:      w,
		H:      h,
		Pixels: make([]float64, w*h),
	}
	for y := 0; y < h; y++ {
		copy(out.Pixels[y*w:(y+1)*w], src.Pixels[(r.Min.Y+y)*src.W+r.Min.X:(r.Min.Y+y)*src.W+r.Max.X])
	}
	return out
}

func syncDiffPeak2D(before, after []float64, w, h int) (peak, noise float64) {
	rowFFT := fourier.NewFFT(w)
	cols := w/2 + 1
	spectrum := make([][]complex128, h)
	diff := make([]float64, w)
	for y := 0; y < h; y++ {
		row := y * w
		for x := 0; x < w; x++ {
			diff[x] = after[row+x] - before[row+x]
		}
		spectrum[y] = rowFFT.Coefficients(nil, diff)
	}
	colFFT := fourier.NewCmplxFFT(h)
	column := make([]complex128, h)
	for x := 0; x < cols; x++ {
		for y := 0; y < h; y++ {
			column[y] = spectrum[y][x]
		}
		coeff := colFFT.Coefficients(nil, column)
		for y := 0; y < h; y++ {
			spectrum[y][x] = coeff[y]
		}
	}
	kx := int(math.Round(float64(w) / syncPeriod))
	ky := int(math.Round(float64(h) / syncPeriod))
	candidates := [][2]int{{kx, 0}, {0, ky}, {0, h - ky}}
	for _, c := range candidates {
		if c[0] < cols && c[1] < h {
			peak = max(peak, cmplx.Abs(spectrum[c[1]][c[0]]))
		}
	}
	var total float64
	var count int
	for y := 0; y < h; y++ {
		for x := 0; x < cols; x++ {
			if x == 0 && y == 0 {
				continue
			}
			if (x == kx && y == 0) || (x == 0 && (y == ky || y == h-ky)) {
				continue
			}
			total += cmplx.Abs(spectrum[y][x])
			count++
		}
	}
	return peak, total / float64(max(1, count))
}

func diagnoseDetection(det *DetectResult) string {
	if det == nil {
		return "sync detection failed before bit recovery"
	}
	if det.Details["sync_peak_strength"] < 1.5 {
		return fmt.Sprintf("sync detection weak: strength=%.3f confidence=%.3f", det.Details["sync_peak_strength"], det.Confidence)
	}
	if det.TilesUsed == 0 {
		return fmt.Sprintf("recovery of bits failed: no supporting tiles, confidence=%.3f", det.Confidence)
	}
	if det.PayloadDigest == "" {
		return fmt.Sprintf("ECC failed: supporting_tiles=%d confidence=%.3f", det.TilesUsed, det.Confidence)
	}
	return fmt.Sprintf("confidence below threshold: confidence=%.3f", det.Confidence)
}
