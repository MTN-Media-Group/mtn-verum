// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 MTN Media Group.

// Package ecc frames the bit payload embedded into image tiles.
package ecc

import (
	"encoding/binary"
	"hash/crc32"
)

const Sync byte = 0xA5
const KeyIDSize = 4
const FrameSize = 48

// Frame layout: [sync][version][4-byte keyID][uvarint len][payload][zero pad][crc32].
func Frame(version uint8, keyID [KeyIDSize]byte, payload []byte) []byte {
	out := make([]byte, FrameSize)
	out[0] = Sync
	out[1] = version
	copy(out[2:], keyID[:])
	n := binary.PutUvarint(out[2+KeyIDSize:], uint64(len(payload)))
	copy(out[2+KeyIDSize+n:], payload)
	binary.BigEndian.PutUint32(out[FrameSize-4:], crc32.ChecksumIEEE(out[:FrameSize-4]))
	return out
}

func Unframe(frame []byte) (version uint8, keyID [KeyIDSize]byte, payload []byte, ok bool) {
	if len(frame) != FrameSize || frame[0] != Sync {
		return 0, keyID, nil, false
	}
	body := frame[:FrameSize-4]
	if crc32.ChecksumIEEE(body) != binary.BigEndian.Uint32(frame[FrameSize-4:]) {
		return 0, keyID, nil, false
	}
	version = body[1]
	copy(keyID[:], body[2:2+KeyIDSize])
	plen, n := binary.Uvarint(body[2+KeyIDSize:])
	if n <= 0 {
		return 0, keyID, nil, false
	}
	start := 2 + KeyIDSize + n
	end := start + int(plen)
	if end > len(body) {
		return 0, keyID, nil, false
	}
	payload = make([]byte, plen)
	copy(payload, body[start:end])
	return version, keyID, payload, true
}

// BitsOf expands b MSB first.
func BitsOf(b []byte) []uint8 {
	out := make([]uint8, len(b)*8)
	for i, v := range b {
		for k := 0; k < 8; k++ {
			out[i*8+k] = (v >> (7 - k)) & 1
		}
	}
	return out
}

// BytesOf packs bits MSB first. Trailing partial bytes are dropped.
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
