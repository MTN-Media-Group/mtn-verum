// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 MTN Media Group.

package ecc

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	digest := make([]byte, 32)
	if _, err := rand.Read(digest); err != nil {
		t.Fatal(err)
	}
	frame := Frame(1, 0x42, digest)
	v, k, p, ok := Unframe(frame)
	if !ok || v != 1 || k != 0x42 || !bytes.Equal(p, digest) {
		t.Fatalf("round trip failed: ok=%v v=%d k=%x len(p)=%d", ok, v, k, len(p))
	}
}

func TestFrameDetectsCorruption(t *testing.T) {
	frame := Frame(1, 0x10, bytes.Repeat([]byte{0xAB}, 32))
	frame[10] ^= 0x01
	if _, _, _, ok := Unframe(frame); ok {
		t.Fatal("corrupted frame must not unframe")
	}
}

func TestBitsRoundTrip(t *testing.T) {
	src := []byte{0xA5, 0x00, 0xFF, 0xC3, 0x5A}
	bits := BitsOf(src)
	if got := BytesOf(bits); !bytes.Equal(got, src) {
		t.Fatalf("got %x want %x", got, src)
	}
}

