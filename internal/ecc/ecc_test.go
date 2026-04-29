// SPDX-License-Identifier: AGPL-3.0-only

// Copyright (C) 2026 MTN Media Group.

package ecc

import (
	"bytes"
	"crypto/rand"
	mathrand "math/rand"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	digest := make([]byte, 32)
	if _, err := rand.Read(digest); err != nil {
		t.Fatal(err)
	}
	frame := Frame(1, [KeyIDSize]byte{0x42, 0x43, 0x44, 0x45}, digest)
	v, k, p, ok := Unframe(frame)
	if !ok || v != 1 || k != [KeyIDSize]byte{0x42, 0x43, 0x44, 0x45} || !bytes.Equal(p, digest) {
		t.Fatalf("round trip failed: ok=%v v=%d k=%x len(p)=%d", ok, v, k, len(p))
	}
}

func TestFrameDetectsCorruption(t *testing.T) {
	frame := Frame(1, [KeyIDSize]byte{0x10, 0x11, 0x12, 0x13}, bytes.Repeat([]byte{0xAB}, 32))
	for i := 0; i < 9; i++ {
		frame[i*6] ^= byte(0x11 + i)
	}
	if _, _, _, ok := Unframe(frame); ok {
		t.Fatal("uncorrectable frame must not unframe")
	}
}

func TestFrameRecoversEightErasures(t *testing.T) {
	digest := bytes.Repeat([]byte{0xCD}, 32)
	frame := Frame(1, [KeyIDSize]byte{0x20, 0x21, 0x22, 0x23}, digest)
	erasures := make([]bool, FrameSize)
	for _, i := range []int{0, 5, 11, 18, 27, 36, 44, 55} {
		frame[i] = 0
		erasures[i] = true
	}
	v, k, p, ok := Decode(frame, erasures)
	if !ok || v != 1 || k != [KeyIDSize]byte{0x20, 0x21, 0x22, 0x23} || !bytes.Equal(p, digest) {
		t.Fatalf("erasure recovery failed: ok=%v v=%d k=%x len(p)=%d", ok, v, k, len(p))
	}
}

func TestFrameRecoversFourBitErrors(t *testing.T) {
	digest := bytes.Repeat([]byte{0xEF}, 32)
	frame := Frame(1, [KeyIDSize]byte{0x30, 0x31, 0x32, 0x33}, digest)
	confidence := make([]float64, FrameSize*8)
	for i := range confidence {
		confidence[i] = 1
	}
	rng := mathrand.New(mathrand.NewSource(42))
	seen := make(map[int]bool)
	for len(seen) < 4 {
		i := rng.Intn(FrameSize)
		if seen[i] {
			continue
		}
		seen[i] = true
		frame[i] ^= byte(1 << uint(rng.Intn(8)))
		for b := 0; b < 8; b++ {
			confidence[i*8+b] = 0.01
		}
	}
	v, k, p, ok := DecodeWithBitConfidence(frame, confidence)
	if !ok || v != 1 || k != [KeyIDSize]byte{0x30, 0x31, 0x32, 0x33} || !bytes.Equal(p, digest) {
		t.Fatalf("bit-error recovery failed: ok=%v v=%d k=%x len(p)=%d", ok, v, k, len(p))
	}
}

func TestRSCorrectsThreeBitErrors(t *testing.T) {
	digest := bytes.Repeat([]byte{0x5A}, 32)
	frame := Frame(1, [KeyIDSize]byte{0x40, 0x41, 0x42, 0x43}, digest)
	rng := mathrand.New(mathrand.NewSource(7))
	seen := make(map[int]bool)
	for len(seen) < 3 {
		i := rng.Intn(FrameSize)
		if seen[i] {
			continue
		}
		seen[i] = true
		frame[i] ^= byte(1 << uint(rng.Intn(8)))
	}
	v, k, p, ok := Decode(frame, nil)
	if !ok || v != 1 || k != [KeyIDSize]byte{0x40, 0x41, 0x42, 0x43} || !bytes.Equal(p, digest) {
		t.Fatalf("three-error recovery failed: ok=%v v=%d k=%x len(p)=%d", ok, v, k, len(p))
	}
}

func TestRSCorrectsEightByteErrors(t *testing.T) {
	digest := bytes.Repeat([]byte{0x7C}, 32)
	frame := Frame(1, [KeyIDSize]byte{0x60, 0x61, 0x62, 0x63}, digest)
	rng := mathrand.New(mathrand.NewSource(99))
	seen := make(map[int]bool)
	for len(seen) < 8 {
		i := rng.Intn(FrameSize)
		if seen[i] {
			continue
		}
		seen[i] = true
		frame[i] ^= byte(1 + rng.Intn(255))
	}
	v, k, p, ok := Decode(frame, nil)
	if !ok || v != 1 || k != [KeyIDSize]byte{0x60, 0x61, 0x62, 0x63} || !bytes.Equal(p, digest) {
		t.Fatalf("eight-byte-error recovery failed: ok=%v v=%d k=%x len(p)=%d", ok, v, k, len(p))
	}
}

func TestRSCorrectsCombinedErrorsErasures(t *testing.T) {
	digest := bytes.Repeat([]byte{0x6B}, 32)
	frame := Frame(1, [KeyIDSize]byte{0x50, 0x51, 0x52, 0x53}, digest)
	for _, i := range []int{3, 29} {
		frame[i] ^= 0xA7
	}
	erasures := make([]bool, FrameSize)
	for _, i := range []int{8, 17, 42, 54} {
		frame[i] = 0
		erasures[i] = true
	}
	v, k, p, ok := Decode(frame, erasures)
	if !ok || v != 1 || k != [KeyIDSize]byte{0x50, 0x51, 0x52, 0x53} || !bytes.Equal(p, digest) {
		t.Fatalf("combined recovery failed: ok=%v v=%d k=%x len(p)=%d", ok, v, k, len(p))
	}
}

func TestRSCorrectsSixteenErasures(t *testing.T) {
	digest := bytes.Repeat([]byte{0x71}, 32)
	frame := Frame(1, [KeyIDSize]byte{0x70, 0x71, 0x72, 0x73}, digest)
	erasures := make([]bool, FrameSize)
	for i := 0; i < ParitySize; i++ {
		frame[i*3] = 0
		erasures[i*3] = true
	}
	v, k, p, ok := Decode(frame, erasures)
	if !ok || v != 1 || k != [KeyIDSize]byte{0x70, 0x71, 0x72, 0x73} || !bytes.Equal(p, digest) {
		t.Fatalf("sixteen-erasure recovery failed: ok=%v v=%d k=%x len(p)=%d", ok, v, k, len(p))
	}
}

func TestRSCorrectsBoundaryMixed(t *testing.T) {
	digest := bytes.Repeat([]byte{0x82}, 32)
	frame := Frame(1, [KeyIDSize]byte{0x80, 0x81, 0x82, 0x83}, digest)
	for _, i := range []int{4, 15, 31, 50} {
		frame[i] ^= 0xA7
	}
	erasures := make([]bool, FrameSize)
	for _, i := range []int{1, 9, 18, 27, 36, 43, 49, 55} {
		frame[i] = 0
		erasures[i] = true
	}
	v, k, p, ok := Decode(frame, erasures)
	if !ok || v != 1 || k != [KeyIDSize]byte{0x80, 0x81, 0x82, 0x83} || !bytes.Equal(p, digest) {
		t.Fatalf("boundary mixed recovery failed: ok=%v v=%d k=%x len(p)=%d", ok, v, k, len(p))
	}
}

func TestRSRejectsBeyondBoundary(t *testing.T) {
	digest := bytes.Repeat([]byte{0x93}, 32)
	frame := Frame(1, [KeyIDSize]byte{0x90, 0x91, 0x92, 0x93}, digest)
	for _, i := range []int{4, 15, 31, 50} {
		frame[i] ^= 0xA7
	}
	erasures := make([]bool, FrameSize)
	for _, i := range []int{1, 9, 18, 27, 36, 43, 46, 49, 55} {
		frame[i] = 0
		erasures[i] = true
	}
	if _, _, _, ok := Decode(frame, erasures); ok {
		t.Fatal("decode succeeded beyond 2e+s <= 16 boundary")
	}
}

func TestBitsRoundTrip(t *testing.T) {
	src := []byte{0xA5, 0x00, 0xFF, 0xC3, 0x5A}
	bits := BitsOf(src)
	if got := BytesOf(bits); !bytes.Equal(got, src) {
		t.Fatalf("got %x want %x", got, src)
	}
}
