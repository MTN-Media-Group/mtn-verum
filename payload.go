// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 MTN Media Group.

package verum

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"time"
)

// PayloadVersion is the wire version of the canonical payload encoding. Bump
// it on any change to the byte layout of canonicalPayload so old detectors
// cleanly reject mismatched embeds rather than mis-parsing them.
const PayloadVersion = 1

// Payload is the application-level data the caller wants tied to an image.
// Only Digest ends up in the pixels; the rest is hashed into the digest so
// that a verifier with the same Payload can reconstruct it and check.
type Payload struct {
	Version      int
	GeneratedAt  time.Time
	Provider     string
	Model        string
	GenerationID string
	AttachmentID string
	Nonce        []byte
	Digest       []byte
}

// canonicalPayload serialises the payload deterministically with explicit
// length prefixes so that no two distinct payloads can produce the same byte
// string. Without this, ambiguity attacks would let a caller produce
// matching digests with different field splits.
func canonicalPayload(p Payload) []byte {
	out := make([]byte, 0, 64+len(p.Provider)+len(p.Model)+len(p.GenerationID)+len(p.AttachmentID)+len(p.Nonce))
	out = append(out, byte(PayloadVersion))
	out = binary.BigEndian.AppendUint64(out, uint64(p.GeneratedAt.UTC().UnixNano()))
	out = appendLP(out, []byte(p.Provider))
	out = appendLP(out, []byte(p.Model))
	out = appendLP(out, []byte(p.GenerationID))
	out = appendLP(out, []byte(p.AttachmentID))
	out = appendLP(out, p.Nonce)
	return out
}

func appendLP(dst, b []byte) []byte {
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(buf[:], uint64(len(b)))
	dst = append(dst, buf[:n]...)
	return append(dst, b...)
}

// computeDigest fills p.Digest with HMAC-SHA256 over the canonical payload.
// It returns the 32-byte digest separately for the caller's convenience.
func computeDigest(p *Payload, secret []byte) ([]byte, error) {
	if len(secret) == 0 {
		return nil, errors.New("verum: empty key secret")
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(canonicalPayload(*p))
	d := mac.Sum(nil)
	p.Digest = d
	return d, nil
}
