// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 MTN Media Group.

package verum

import (
	"bytes"
	"testing"
	"time"
)

func TestCanonicalEncodingNoAmbiguity(t *testing.T) {
	// Without length prefixes these two payloads would collide.
	at := time.Unix(1700000000, 0)
	a := Payload{GeneratedAt: at, Provider: "ab", Model: "cd"}
	b := Payload{GeneratedAt: at, Provider: "abcd", Model: ""}
	ea := canonicalPayload(a)
	eb := canonicalPayload(b)
	if bytes.Equal(ea, eb) {
		t.Fatalf("distinct payloads must produce distinct canonical encodings")
	}
}

func TestComputeDigestDeterministic(t *testing.T) {
	at := time.Unix(1700000000, 0)
	p := Payload{
		GeneratedAt:  at,
		Provider:     "mtnai",
		Model:        "flux.1-pro",
		GenerationID: "g_abc",
		AttachmentID: "att_xyz",
		Nonce:        []byte{1, 2, 3, 4},
	}
	secret := []byte("test-secret")
	d1, err := computeDigest(&Payload{
		GeneratedAt:  p.GeneratedAt,
		Provider:     p.Provider,
		Model:        p.Model,
		GenerationID: p.GenerationID,
		AttachmentID: p.AttachmentID,
		Nonce:        append([]byte(nil), p.Nonce...),
	}, secret)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := computeDigest(&Payload{
		GeneratedAt:  p.GeneratedAt,
		Provider:     p.Provider,
		Model:        p.Model,
		GenerationID: p.GenerationID,
		AttachmentID: p.AttachmentID,
		Nonce:        append([]byte(nil), p.Nonce...),
	}, secret)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(d1, d2) {
		t.Fatalf("digest not deterministic")
	}
	if len(d1) != 32 {
		t.Fatalf("digest length %d, want 32", len(d1))
	}
}

func TestComputeDigestRejectsEmptySecret(t *testing.T) {
	if _, err := computeDigest(&Payload{}, nil); err == nil {
		t.Fatal("empty secret must error")
	}
}
