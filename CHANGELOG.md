# Changelog

## Unreleased

### Capabilities

- Small top-left crop offsets up to 2 tile periods, when the original image dimensions are a multiple of the tile size and the cropped image retains at least 5 tiles per side, via a low-amplitude luminance sync signal, FFT-based sync estimation, and crop-aware tile origins.
- 0.75x bilinear, 0.5x nearest-neighbor, and 2048→1024 bilinear large-image downscale detection with `StrengthRobust`. Downscale survival requires the resized image to keep both dimensions at least MinImageDim (256px), so the shortest side is the limiting dimension. Current measured coverage is 1024→512 for 0.5x nearest-neighbor, 1024→768 for 0.75x bilinear, 2048→1024 for 0.5x bilinear on the large-image path, and 384→288 for 0.75x bilinear at the boundary. The 1024→512 bilinear path is not claimed for PayloadVersion 2.
- Public JPEG output now defaults to quality 95, rejects quality below 95, and verifies JPEG Q95 transcode survival with the Robust profile. JPEG Q75/Q85 transcode survival remains unsupported in the current release under the restored Robust quality gates.
- `PayloadVersion` is now 2. Payload frames use an 8-byte SHA-256-truncated key ID and Reed-Solomon coding over a 60-byte frame; the 16 parity bytes correct up to 8 byte errors, or mixed errors and erasures where `2e + s <= 16`.
- Large images now use adaptive 128px tiles, with a 2048x2048 round-trip test covering the path.

### Quality

- JPEG capability claims, sync visibility on flat images, deterministic tile selection, checksum naming, examples, and buffered reader helper naming were tightened for release.
- Deterministic false-positive fixtures under `testdata/` and a corpus sweep now guard detection precision.
- Watson 1993 DCT masking constants from the published luminance threshold table now drive perceptual masking, including Watson's luminance and contrast exponents.
- The direct DCT matrix multiply was replaced with scaled AAN 1D passes while retaining strict round-trip and reference coverage for transform accuracy.
- JPEG quality sweep coverage now keeps Q95 active; Q75/Q85 skip-only tests were removed because those transcodes are unsupported current-release capability gaps.
- Corpus calibration now rejects fixture/profile runs with more than 30% embed failures instead of reporting survivor-only confidence distributions as representative.

### Tooling

- Embed and detect now use bounded per-tile concurrency.
- The public API surface is documented as functions (`Embed`, `Detect`, `Verify`, `IsEmbeddable`), types (`Config`, `Key`, `StrengthProfile`, `IncludeMetadata`, `QualityConfig`, `DetectionConfig`, `Payload`, `EmbedResult`, `QualityReport`, `DetectResult`), constants (`MinImageDim`, `DefaultTileSize`, `LargeTileSize`, `PayloadVersion`, `StrengthInvisible`, `StrengthBalanced`, `StrengthRobust`, `IncludeMetadataNone`, `IncludeMetadataStandard`), and errors (`ErrUnsupportedFormat`, `ErrImageTooSmall`, `ErrImageTooLarge`, `ErrNoCapacity`, `ErrQualityGateFailed`, `ErrSelfDetectionFailed`, `ErrInvalidConfig`, `ErrNoDetectionKeys`); buffered reader helpers were removed.
- `DetectResult.Details` now includes bit-confidence, tile-support, sync, scale, and crop metrics.
- Benchmarks now cover 512px and 1024px embed/detect paths.
- Go doc round-trip examples and this changelog were added.
