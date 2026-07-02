# Plan: `mtn-verum` v1 format - region-survivable, rotation-invariant, key-secure watermarking

## Context

The current `mtn-verum` codebase implements a tile-grid-locked
DCT-coefficient-pair watermarking scheme. It survives top-left crops up to a
small tile-period offset, JPEG Q95 in the robust path, and a limited set of
downscales. It does not satisfy the actual requirement: a marked image region
cropped out, rotated, scaled, and pasted into another image should still
identify as MTN-generated when enough original source support remains.

Nothing has shipped. The on-image format, public result fields, package
constants, and `PayloadVersion` may reset cleanly. This plan replaces the
current scheme rather than preserving compatibility with the pre-release
tile-grid format.

The design stays clean-room and uses only established public watermarking,
cryptographic, and signal-processing literature:

- Cox, Kilian, Leighton, and Shamoon, "Secure Spread Spectrum Watermarking for
  Multimedia," IEEE Transactions on Image Processing, 1997.
- Ó Ruanaidh and Pun, "Rotation, Scale and Translation Invariant Digital Image
  Watermarking," Signal Processing, 1998.
- Lin, Wu, Bloom, Cox, Miller, and Lui, "Rotation, Scale, and Translation
  Resilient Watermarking for Images," IEEE Transactions on Image Processing,
  2001.
- Moulin and Malvar, "Detection-Theoretic Analysis of Desynchronization Attacks
  in Watermarking," Microsoft Research Technical Report MSR-TR-2002-24, 2002.
- Petitcolas, Anderson, and Kuhn, StirMark robustness benchmarking work.
- Craver, Memon, Yeo, and Yeung, "Resolving Rightful Ownerships with Invisible
  Watermarking Techniques," 1998.
- Kutter, Voloshynovskiy, and Herrigel, watermark copy-attack work.
- NIST FIPS 198-1 HMAC, NIST SP 800-90A HMAC-DRBG, and RFC 5869 HKDF.

## User-confirmed parameters

- **Minimum surviving region size**: `Config.MinPatchSize`, supported values
  128 / 192 / 256 px, default 192. Smaller values reduce the detectable patch
  floor but weaken JPEG/denoise margin and raise visibility risk.
- **Visibility budget at strongest profile**: SSIM >= 0.995, PSNR >= 44 on the
  calibration corpus. At maximum strength, artifacts may still be perceptible
  to expert viewers in smooth regions.
- **JPEG floor**: Q75. `StrengthBalanced` and `StrengthRobust` must demonstrate
  Q75 transcode survival in the calibration corpus.
- **Legacy compatibility**: none. `PayloadVersion` is exactly `1` for this
  implementation. Treat this as v1 of the first shippable format and abandon the
  pre-release tile-grid format completely. README / NOTICE / CHANGELOG must say
  the pre-release tile-grid format was abandoned before release.
- **Implementation constraint**: pure Go; no CGO dependency for lossy WebP.

## Architecture

### 1. Domain-separated keyed material

All keyed signal randomness derives from one tenant `SignalSecret` with
explicit domain separation. Frame authentication derives from per-frame
`Key.Secret` values. This split keeps sync/data detection keyed while allowing
cheap key rotation: with `K` configured frame keys, Fourier-Mellin sync and
spread payload extraction run once per `(signal group, window, scale)`, and only
frame-MAC validation iterates over the `K` frame keys.

```
const PayloadVersion = 1

signalRoot  = HKDF-Extract(SignalSecret, "mtn-verum-format-v1-signal")
frameRoot   = HKDF-Extract(Key.Secret,  "mtn-verum-format-v1-frame")

syncKey     = HKDF-Expand(signalRoot, "sync-carrier", 32)
dataKey     = HKDF-Expand(signalRoot, "data-chips",   32)
binKey      = HKDF-Expand(signalRoot, "bin-plan",     32)
nonceKey    = HKDF-Expand(signalRoot, "nonce-perturb", 32)
frameMACKey = HKDF-Expand(frameRoot,  "frame-mac" || keyID, 32)
```

The source is open under AGPL, so security must not depend on obscurity. The
attacker may know the algorithm, carrier band, windowing strategy, code, and
all public parameters. Without the per-tenant `SignalSecret`, they should not
be able to run the offline signal detector or selectively remove the mark
without broad image damage. Without the relevant frame key, they should not be
able to forge a valid frame.

### 2. Periodic keyed sync carrier

Embed a 2D keyed pseudorandom carrier tiled with period `P = MinPatchSize`.
The carrier lives in a mid-frequency annulus chosen during calibration, with an
initial target around `[0.08, 0.22]` cycles/pixel. The carrier provides local
pose synchronization, not the full payload.

```
carrier = P x P real-valued tile
support = mid-frequency FFT annulus
chips   = HMAC-DRBG(syncKey)
mark    = carrier tiled across the image
```

Each sufficiently supported `P x P` source window contains a complete sync
instance independent of absolute image origin. That is the core property needed
for cropped-region and composite detection.

Important implementation details:

- Generate a real-valued spatial carrier by enforcing Hermitian FFT symmetry.
- Remove DC and low-frequency leakage.
- Limit spectral peakiness so the carrier does not create obvious periodic
  artifacts or become easy to notch-filter.
- Partition the annulus into disjoint sync/data/reserve bin sets with a
  stratified HMAC-DRBG plan from `binKey`. For the initial calibration target,
  assign 50% of eligible bins to sync, 40% to data, and 10% to guard/reserve
  inside each radial bucket. Sync and data must never share FFT bins; if a
  patch size/profile cannot fit disjoint support with margin, reject it.
- Shape amplitude with Watson-style luminance/texture masking, alpha coverage,
  max-delta caps, changed-pixel-ratio caps, SSIM, and PSNR.
- Add per-image nonce perturbation in the reserve bin set so many images from
  the same tenant do not expose one reusable full-strength template by
  averaging. The perturbation is generated by HMAC-DRBG from `nonceKey` and the
  payload nonce / payload tag, is independent of sync/data bins, and is not used
  for authentication. Detection may ignore these bins, preserving cached
  sync/data templates.

### 3. Fourier-Mellin pose recovery

Detection slides windows over the suspect image and asks whether a window
contains the keyed carrier after rotation and scale.

For each candidate window:

1. Extract luminance and apply a 2D taper to reduce boundary leakage.
2. Compute `W = |FFT(window)|`; magnitude removes spatial translation phase.
3. Convert `W` and the carrier template to log-polar magnitude.
4. Phase-correlate the log-polar images.
5. Estimate rotation/scale from the primary peak, with subpixel refinement and
   a primary/secondary peak ratio confidence score.
6. Test the 180-degree ambiguity branch when FFT magnitude cannot resolve
   signed orientation.
7. Warp only the candidate window into canonical pose for data-layer decoding.

This is the standard Fourier-Mellin construction from the public watermarking
literature. The implementation must also handle its known limits: weak texture,
small windows, local affine warps, boundary masks, and ambiguous rotational
symmetries.

### 4. Translation-invariant spread payload

Do not encode the payload solely in raw FFT phase. Raw phase is sensitive to
translation; the FFT magnitude step intentionally discards translation phase for
pose recovery. If phase coding is ever used, decoding must first recover the
window's toroidal translation offset. The default v1 plan instead uses a
translation-tolerant spread-spectrum data layer.

Frame layout target:

```
version        1 byte
flags          1 byte
payloadTag    16 bytes  // truncated HMAC-SHA256 over canonical payload
frameMAC       8 bytes   // truncated HMAC over version || flags || payloadTag
crc/check      2 bytes   // early reject / RS misdecode guard
```

The coded payload should stay around 300-350 bits after ECC. This is
intentional: RS(60,44) / 480 bits is too expensive for 128 px windows once chip
spreading, JPEG survival, and false-positive margin are considered.

Encoding:

- Derive coefficient groups from `dataKey` with HMAC-DRBG.
- Use only the data bin set from the deterministic annulus partition. Data
  encoding must not alter sync-owned bins.
- Encode each coded bit as a spread sign/correlation over `K` coefficients or
  local subbands. Tune `K` per `MinPatchSize` and profile.
- Prefer repeated low-rate chips and soft-decision ECC over a large byte frame.
- Infer the external `KeyID` from the configured key that validates
  `frameMAC`; do not spend watermark capacity carrying the full key ID.

Capacity budgeting is mandatory before calibration. Approximate available real
FFT annulus bins for `[0.08, 0.22]` cycles/pixel:

| P | unique real bins | 480 bits x K=16 load | 320 bits x K=8 load |
|---|------------------|----------------------|---------------------|
| 128 | ~1,090 | ~7.0x | ~2.3x |
| 192 | ~2,463 | ~3.1x | ~1.0x |
| 256 | ~4,362 | ~1.8x | ~0.6x |

The implementation must reject profile/frame/spreading combinations that do not
fit the selected `MinPatchSize` with calibrated margin.

### 5. Sliding-window detector and aggregation

```
for scale in default_scales union user_scales:
    resized = resize(image, scale)
    for window in capacity_screened_windows(resized, size=P, stride=P/4):
        pose = fmellin_correlate(window, syncCarrier)
        if pose.confidence >= partialThreshold:
            partialEvidence.append(region(window, pose, coverage(window)))
        if coverage(window) < MinWindowCoverage || pose.confidence < syncThreshold:
            continue
        for branch in pose.orientationBranches:
            frame = decode_spread_payload(window, branch)
            if frameMAC validates under a configured key:
                detections.append(region(window, pose, frame, keyID))
for fragment in smaller_supported_fragments(image, sizes=P/2..P):
    evidence = fragment_correlate(fragment, syncCarrier)
    if evidence.confidence >= partialThreshold:
        partialEvidence.append(region(fragment, evidence, coverage(fragment)))
return aggregate_regions(detections, partialEvidence)
```

Aggregation should cluster overlapping detections, merge repeated evidence, and
return both validated matches and partial evidence. A single global confidence
is not enough for composites.

Partial evidence is not optional. The detector must always report lower
confidence sync/carrier evidence when available, including evidence from chunks
that are too small or too incomplete to decode a frame. `Detected=true` remains
reserved for cryptographically validated frame matches; partial evidence may set
`Possible=true` but must not set `Detected=true`.

Public result additions:

```go
type DetectionRegion struct {
    Bounds         image.Rectangle
    Confidence     float64
    RotationDegrees float64
    Scale          float64
    Coverage       float64
    Verified       bool // true only when frame MAC validates
    Partial        bool // true for sync/carrier evidence without valid frame
}

type DetectResult struct {
    // existing fields retained where still meaningful
    RotationEstimate float64
    RegionsDetected  int
    PartialRegions   int
    BestPartialConfidence float64
    Regions               []DetectionRegion
}
```

`DetectResult.Details` may remain for local observability, but raw
per-window/per-key detector scores should be opt-in debug output. If this
library backs a network detector, the service must rate-limit requests and avoid
returning enough score detail to act as a watermark-removal oracle.

CPU controls:

- `DetectionConfig.WindowStride`, default `P/4`.
- `DetectionConfig.MaxWindowsToCheck`, default bounded by image size/profile.
- `DetectionConfig.MaxRotationDegrees`, where `0` means full 360 degrees.
- `DetectionConfig.MinWindowCoverage`, default calibrated around 0.70-0.80.
- `DetectionConfig.ScaleMin` / `ScaleMax`, default to the calibrated range.
- `DetectionConfig.ScaleStepRatio`, default to a log-spaced calibrated pyramid.
- Process scale levels sequentially with one resample buffer in flight. Effective
  `ScaleMax` is capped by `MaxScaledDimension` and `MaxScaledPixels`; detection
  must refuse or clamp settings that would create oversized intermediates.
- Capacity prefilter by alpha coverage, texture, and cheap band-energy checks.
- Cache keyed carrier/log-polar templates per `(SignalSecret identity, P,
  profile)`.
- Early exit once enough independent validated regions agree.

## Public API changes

```go
type Config struct {
    // ... existing fields ...

    // SignalSecret is the per-tenant signal key used for sync/data carriers.
    // It is required for Embed and Detect in the v1 format.
    SignalSecret []byte

    // MinPatchSize sets the tiled carrier period and the nominal minimum
    // source-support window. Supported: 128, 192, 256. Default: 192.
    MinPatchSize int
}

type DetectionConfig struct {
    // ... existing fields ...

    MaxRotationDegrees float64 // 0 = full 360
    WindowStride       int     // 0 = P/4
    MaxWindowsToCheck  int     // 0 = calibrated default
    MinWindowCoverage  float64 // 0 = calibrated default
    ScaleMin           float64 // 0 = calibrated default lower bound
    ScaleMax           float64 // 0 = calibrated default upper bound
    ScaleStepRatio     float64 // 0 = calibrated log-pyramid step
    MaxScaledDimension int     // 0 = calibrated default dimension cap
    MaxScaledPixels    int     // 0 = calibrated default pixel cap
    DebugScores        bool    // expose extra diagnostic Details locally only
}
```

`Embed`, `Detect`, `Verify`, and `IsEmbeddable` keep their signatures. The
format reset means result fields and frame internals may still change during
implementation, but `PayloadVersion` is fixed at `1` for this first shippable
format.

## Capability claims

- **Cropped/composited region detection**: a transformed region can be detected
  when it contains a mostly valid `MinPatchSize x MinPatchSize` window of
  original source support after alpha/mask coverage checks. Area alone is not
  enough; irregular cut-outs must retain sufficient valid pixels inside at
  least one detection window.
- **Rotation**: 360-degree detection via Fourier-Mellin, with ambiguity branches
  resolved by payload validation when necessary.
- **Scale**: calibrated default target starts at 0.5x to 2x, with the detector
  designed to support a wider configurable range such as 0.25x to 4x when
  memory, CPU, surviving source support, and carrier-band survival allow it.
  README / NOTICE claims must publish measured ranges, not aspirational bounds.
- **Partial evidence**: chunks below the full validation floor can still return
  `Possible=true` and partial region evidence when keyed sync/carrier
  correlation is strong. They must not return `Detected=true` unless a valid
  frame MAC is decoded.
- **JPEG**: Q75 round-trip survival for `StrengthBalanced` and
  `StrengthRobust` on the calibration corpus.
- **PNG**: lossless, full survival except for insufficient support/capacity.
- **WebP**: decode any supported WebP; encode lossless only in pure Go.
- **JPEG with alpha**: if JPEG output is requested for input with non-opaque
  alpha, return `ErrUnsupportedFormat` and require the caller to composite to an
  opaque background first. Do not silently flatten transparency.
- **Multi-key detection**: try configured keys; successful frame MAC determines
  the matched `KeyID`.
- **Transparent pixels**: preserve transparent NRGBA RGB and avoid embedding
  below the alpha visibility floor.
- **Security**: offline signal detection and selective stripping require the
  tenant `SignalSecret`; valid frame forging requires the relevant frame key.
  Online detectors need oracle-hardening controls outside the image format.

## Known limitations

- Regions below the configured support floor cannot reliably carry a full local
  frame. This is an information-capacity limit, not an implementation bug.
  They may produce partial evidence, but that evidence is not cryptographic
  proof of a payload match.
- Heavy local warps, perspective transforms, content-aware scaling, and
  StirMark-style random bending can desynchronize local windows.
- AI regeneration, diffusion inpainting, and semantic re-rendering may rewrite
  pixels faster than the mark can survive. Partial defeat is acknowledged.
- Print/photograph cycles introduce analog blur, color transforms, geometry
  distortion, and sensor noise that may exceed `StrengthBalanced` margin.
- Pure-flat images and very smooth gradients may fail capacity checks or require
  lower strength to avoid visible periodic artifacts.
- Collusion across many differently watermarked versions of the same source can
  estimate reusable components. Reserve-bin nonce perturbation and spectral
  peak caps reduce this risk but do not eliminate it.
- Copy attacks, where an attacker estimates a mark from one image and applies it
  to another, must be explicitly tested and documented.
- Lossy WebP output remains unsupported without CGO.
- Runtime cost is expected to be materially higher than the pre-release
  tile-grid implementation. Initial estimates are 100-400 ms for embed and
  200-800 ms for detect per 1024x1024 image on one modern CPU core, before
  tuning and parallelism. These are estimates to measure and optimize against,
  not reasons to weaken quality, security, or robustness.

## Phased implementation roadmap

**Phase 1 - carrier construction** (`internal/carrier/`)
- HKDF/HMAC-DRBG key schedule with domain-separated labels.
- `Config.validate` enforces `SignalSecret` length >= 32 bytes for Embed and
  Detect.
- Real-valued periodic sync carrier in calibrated FFT annulus.
- Stratified disjoint bin partition for sync, data, and reserve support.
- Reserve-bin nonce perturbation from `nonceKey` and payload nonce/tag;
  detector ignores reserve bins so template caching remains stable.
- Spectral peak caps, DC/low-band suppression, Hermitian symmetry tests.
- Watson/texture/alpha-aware amplitude shaping and quality gates.

**Phase 2 - Fourier-Mellin detection** (`internal/fmellin/`)
- Tapered FFT magnitude, log-polar sampling, phase correlation.
- Subpixel peak refinement, peak-ratio confidence, 180-degree branch handling.
- Synthetic pose tests for rotation, scale, translation phase, and boundary
  masks.

**Phase 3 - spread payload** (`internal/spreadbits/`)
- Compact frame format with truncated payload tag and frame MAC.
- Soft-decision spread-spectrum bit encoding over keyed coefficient groups.
- ECC tuned to `MinPatchSize` rather than reusing RS(60,44) blindly.
- Capacity checks for 128 / 192 / 256 px windows.

**Phase 4 - embed pipeline rewrite** (`embed.go`)
- Add sync carrier and data layer in the frequency/spatial hybrid path.
- Calibrated amplitude tables per strength and patch size.
- Self-detection fast path using active key, native scale, and representative
  windows. Embed succeeds only when self-detection decodes and validates the
  frame MAC for the embedded payload; sync-only or partial evidence is not a
  passing self-detection result.
- Post-encode quality gates: SSIM, PSNR, max delta, changed-pixel ratio, alpha
  preservation, and spectral peak limits.
- Reject JPEG output for non-opaque input with `ErrUnsupportedFormat`; callers
  must explicitly composite alpha before requesting JPEG.

**Phase 5 - detect pipeline rewrite** (`detect.go`)
- Sliding multi-scale window detector with coverage and capacity prefilters.
- Mandatory partial-evidence pass for fragments and incomplete windows that
  cannot decode a full frame.
- Per-signal-group carrier/template cache. Multiple frame keys in the same
  signal group share FM pose recovery and spread extraction work.
- FM pose recovery, spread payload decode, frame MAC validation.
- Region clustering and aggregate result construction.
- Bounded CPU via stride, max windows, calibrated scale pyramid, early exit, and
  concurrency.

**Phase 6 - corpus calibration and claims alignment**
- Composite fixtures: rectangular crops, irregular masks, pasted subjects,
  unrelated backgrounds, rotation, scale, JPEG sweeps.
- Patch-size floor calibration for 128 / 192 / 256.
- Rotation sweep at least every 15 degrees, with extra tests near ambiguous
  branches.
- Scale sweep for the default calibrated range plus extended sweeps toward
  0.25x and 4x where source support and CPU/memory limits permit.
- Partial-evidence calibration for fragments below `MinPatchSize`, including
  false-positive thresholds that keep partial evidence separate from validated
  detections.
- False-positive ROC corpus across unmarked images, wrong keys, window offsets,
  rotations, and scales.
- README / NOTICE / CHANGELOG updated only with measured numbers.

**Phase 7 - adversarial review**
- Desynchronization attacks: local warp, random bending, perspective, crop
  phase sweep, border padding, and content-aware resize.
- Signal attacks: JPEG Q75/Q85/Q95, blur, denoise, sharpen, color conversion,
  resizing kernels, and blind mid-band suppression.
- Security attacks: wrong secret, random key confusion, detector-oracle
  sensitivity simulation, copy attack, template estimation, and collusion from
  many marked images.
- Loop until capability claims match measured results and known limitations are
  reflected in NOTICE.

## Critical files

**New**:
- `internal/carrier/` - keyed carrier generation and amplitude shaping.
- `internal/fmellin/` - Fourier-Mellin pose recovery.
- `internal/spreadbits/` - compact frame, ECC, and spread payload coding.
- `testdata/composites/` - composite/cut-out fixture set.

**Rewritten**:
- `embed.go`, `detect.go`, `verum.go`, `config.go`, `payload.go`,
  `verum_test.go`.

**Retained with changes where useful**:
- `internal/codec/` - image I/O, still pure Go.
- `internal/quality/` - SSIM/PSNR plus added artifact gates.
- `watson.go` and `internal/dct/` - perceptual masking support, not the primary
  embedding primitive.
- `internal/tiles/` - repurposed or replaced for local texture/coverage scoring.
- `internal/ecc/` - reuse only if the chosen compact code fits v1 capacity.

## Verification

End-to-end:

```bash
cd /mnt/devdrive/MTN/Webpanel/mtn-verum && go test ./... -timeout 600s
cd /mnt/devdrive/MTN/Webpanel/mtn-verum && go test ./... -race -short
cd /mnt/devdrive/MTN/Webpanel/mtn-verum && go test -run TestCalibrationCorpus -v
cd /mnt/devdrive/MTN/Webpanel/mtn-verum && go test -run TestCompositeRegion -v
cd /mnt/devdrive/MTN/Webpanel/mtn-verum && go test -run TestRotationSweep -v
cd /mnt/devdrive/MTN/Webpanel/mtn-verum && go test -run TestJPEGSurvival -v
cd /mnt/devdrive/MTN/Webpanel/mtn-verum && go test -run TestAdversarial -v
```

Required test classes:

- `TestCompositeRegionDetection` - embed 1024x1024, cut at least one
  `MinPatchSize` supported region, paste onto unrelated background, detect and
  verify key + payload tag.
- `TestPayloadVersionReset` - asserts `PayloadVersion == 1`, rejects the
  pre-release tile-grid format, and verifies NOTICE documents the abandonment.
- `TestSignalSecretRequired` - embed/detect require `SignalSecret`; wrong
  signal secret gives no validated detection even when frame keys are correct.
- `TestSignalSecretLengthFloor` - `Config.validate` rejects `SignalSecret`
  shorter than 32 bytes for Embed and Detect.
- `TestSyncDataBinPartition` - sync, data, and reserve FFT bin sets are
  disjoint for 128 / 192 / 256 px and preserve radial coverage.
- `TestNoncePerturbationReserveBins` - same tenant/payload variants perturb
  reserve bins by payload nonce/tag without changing sync/data templates or
  authentication semantics.
- `TestSelfDetectionRequiresFrameMAC` - embed self-detection fails on sync-only
  or partial evidence and passes only after frame MAC validation.
- `TestMultiKeySearchCostBound` - with many frame keys in one signal group,
  FM/pose and spread extraction run once per window/scale while frame-MAC
  checks iterate over keys.
- `TestIrregularMaskCompositeDetection` - subject-like alpha mask with partial
  support; succeeds above `MinWindowCoverage`, fails below it.
- `TestCropPhaseSweep` - every crop offset modulo `MinPatchSize` for native
  orientation.
- `TestRotationSweep` - every 15 degrees from 0 to 345 degrees, including 180
  ambiguity branch validation.
- `TestScaleSweep` - default calibrated range in dense steps with multiple
  resize kernels.
- `TestExtendedScaleSweep` - probe wider configured ranges toward 0.25x and 4x
  and publish only the measured passing bounds.
- `TestScalePyramidMemoryCap` - wide scale ranges process one resample buffer at
  a time and refuse/clamp settings exceeding scaled dimension or pixel caps.
- `TestPartialEvidenceFragments` - chunks below `MinPatchSize` can produce
  `Possible=true` / partial regions but never `Detected=true` without a valid
  frame MAC.
- `TestCombinedTransformSweep` - crop + rotate + scale + JPEG in one pipeline.
- `TestJPEGSurvival_Q75` - Balanced and Robust Q75 round-trip survival.
- `TestJPEGAlphaRejected` - JPEG output requested for non-opaque input returns
  `ErrUnsupportedFormat` without silently flattening alpha.
- `TestPatchFloorCalibration` - configured floors 128 / 192 / 256 succeed at
  the floor and do not overclaim below the floor.
- `TestWrongSecretAndKeyConfusion` - wrong secrets and random key IDs do not
  validate frames.
- `TestFalsePositiveROC` - large unmarked corpus across windows, scales,
  rotations, and configured keys.
- `TestVisibilityBudget` - SSIM/PSNR/max-delta/changed-ratio/spectral-peak
  claims across photos, screenshots, portraits, gradients, dark images, and
  smooth skin-like regions.
- `TestTemplateEstimationResistance` - average many marked images and confirm
  reusable carrier/data estimates do not create practical removal or copy.
- `TestCopyAttackResistance` - extracted signal from one image does not validate
  as a forged mark on another payload.
- `TestDetectorOracleHardening` - debug scores are opt-in; default API output is
  not granular enough for iterative removal.
- `BenchmarkEmbedDetectCost` - records embed and detect costs for representative
  1024x1024 images. Results guide optimization only; they do not lower quality,
  security, or robustness thresholds.
