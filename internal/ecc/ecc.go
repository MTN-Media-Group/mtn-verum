// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 MTN Media Group.

// Package ecc handles framing of the bit payload that the embedder writes
// into image tiles. The frame carries a sync byte, version, key id, and a
// CRC32 over its body so the detector can reject noise quickly. Per-bit
// repetition is implicit in the embed/detect pipeline (each tile holds one
// frame copy and the detector sums soft votes across tiles).
package ecc

import (
	"encoding/binary"
	"hash/crc32"
)

// Sync is a fixed leading byte the detector looks for after majority-voting.
// It is not a security feature — it just fails fast on noise.
const Sync byte = 0xA5

// Frame builds the byte sequence that gets bit-expanded and embedded:
// [Sync][version][keyID][len(payload) uvarint][payload][crc32 over preceding].
// The frame is the smallest unit before repetition; the detector recovers
// it as a whole and rejects on CRC mismatch.
func Frame(version uint8, keyID uint8, payload []byte) []byte {
	out := make([]byte, 0, 4+binary.MaxVarintLen32+len(payload)+4)
	out = append(out, Sync, version, keyID)
	var buf [binary.MaxVarintLen32]byte
	n := binary.PutUvarint(buf[:], uint64(len(payload)))
	out = append(out, buf[:n]...)
	out = append(out, payload...)
	c := crc32.ChecksumIEEE(out)
	out = binary.BigEndian.AppendUint32(out, c)
	return out
}

// Unframe verifies the CRC and returns the version, keyID, and payload.
// ok is false on any framing or integrity error.
func Unframe(frame []byte) (version, keyID uint8, payload []byte, ok bool) {
	if len(frame) < 4+1+4 || frame[0] != Sync {
		return 0, 0, nil, false
	}
	body := frame[:len(frame)-4]
	want := binary.BigEndian.Uint32(frame[len(frame)-4:])
	if crc32.ChecksumIEEE(body) != want {
		return 0, 0, nil, false
	}
	version = body[1]
	keyID = body[2]
	plen, n := binary.Uvarint(body[3:])
	if n <= 0 || int(plen) != len(body)-3-n {
		return 0, 0, nil, false
	}
	payload = make([]byte, plen)
	copy(payload, body[3+n:])
	return version, keyID, payload, true
}

// BitsOf expands a byte slice into a bit slice, MSB first per byte.
func BitsOf(b []byte) []uint8 {
	out := make([]uint8, len(b)*8)
	for i, v := range b {
		for k := 0; k < 8; k++ {
			out[i*8+k] = (v >> (7 - k)) & 1
		}
	}
	return out
}

// BytesOf packs a bit slice (MSB first per byte) back into bytes. Trailing
// bits that don't complete a byte are ignored.
func BytesOf(bits []uint8) []byte {
	out := make([]byte, len(bits)/8)
	for i := range out {
		var v byte
		for k := 0; k < 8; k++ {
			v = (v << 1) | (bits[i*8+k] & 1)
		}
		out[i] = v
	}
	return out
}
