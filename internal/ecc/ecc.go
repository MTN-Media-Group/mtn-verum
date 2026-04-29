// SPDX-License-Identifier: AGPL-3.0-only

// Copyright (C) 2026 MTN Media Group.

// Package ecc frames the bit payload embedded into image tiles.
package ecc

import (
	"encoding/binary"
	"sort"
)

const Sync byte = 0xA5                  // reason: distinctive sync byte 10100101 used to distinguish a verum frame from random bytes during scan.
const KeyIDSize = 8                     // reason: 8-byte key ID raises collision work beyond realistic configured key namespaces.
const DigestSize = 32                   // reason: full HMAC-SHA256 output preserves the 256-bit security claim.
const ChecksumSize = 2                  // reason: CRC-16/CCITT catches RS misdecodes that pass syndrome but yield wrong bytes.
const DataSize = 44                     // reason: sync(1) + version(1) + keyID(8) + digest(32) + checksum(2) = 44 bytes per frame.
const ParitySize = 16                   // reason: 16 parity bytes give RS(60,44) which corrects up to 8 byte errors or 16 erasures (2e+s<=16).
const FrameSize = DataSize + ParitySize // reason: derived from data + parity for clarity.
const ConfidenceErasureFactor = 0.80    // reason: corpus byte-confidence ratios keep erasures inside the RS parity boundary without discarding median-strength bytes.
const crc16Initial uint16 = 0xffff      // reason: CRC-16/CCITT initial register value (FFFF).
const crc16HighBit uint16 = 0x8000      // reason: CRC-16/CCITT high-bit mask.
const crc16Polynomial uint16 = 0x1021   // reason: CRC-16/CCITT polynomial (x^16 + x^12 + x^5 + 1).

type rankedByte struct {
	index      int
	confidence float64
}

// Frame layout: RS([sync][version][8-byte keyID][32-byte payload][crc16-ccitt]).
func Frame(version uint8, keyID [KeyIDSize]byte, payload []byte) []byte {
	data := make([]byte, DataSize)
	data[0] = Sync
	data[1] = version
	copy(data[2:], keyID[:])
	copy(data[2+KeyIDSize:], payload)
	binary.BigEndian.PutUint16(data[DataSize-ChecksumSize:], crc16CCITT(data[:DataSize-ChecksumSize]))
	out, ok := encodeRS(data)
	if !ok {
		panic("ecc: reed-solomon encoder rejected package constants")
	}
	return out
}

func Unframe(frame []byte) (version uint8, keyID [KeyIDSize]byte, payload []byte, ok bool) {
	return Decode(frame, nil)
}

func Decode(frame []byte, erasures []bool) (version uint8, keyID [KeyIDSize]byte, payload []byte, ok bool) {
	data, ok := decodeRS(frame, erasures)
	if !ok || data[0] != Sync {
		return 0, keyID, nil, false
	}
	if crc16CCITT(data[:DataSize-ChecksumSize]) != binary.BigEndian.Uint16(data[DataSize-ChecksumSize:]) {
		return 0, keyID, nil, false
	}
	version = data[1]
	copy(keyID[:], data[2:2+KeyIDSize])
	payload = make([]byte, DigestSize)
	copy(payload, data[2+KeyIDSize:2+KeyIDSize+DigestSize])
	return version, keyID, payload, true
}

func DecodeWithBitConfidence(frame []byte, bitConfidence []float64) (version uint8, keyID [KeyIDSize]byte, payload []byte, ok bool) {
	ranked := make([]rankedByte, FrameSize)
	for i := range ranked {
		var sum float64
		for b := 0; b < 8; b++ {
			j := i*8 + b
			if j < len(bitConfidence) {
				sum += bitConfidence[j]
			}
		}
		ranked[i] = rankedByte{index: i, confidence: sum / 8}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		return ranked[i].confidence < ranked[j].confidence
	})
	erasures := make([]bool, FrameSize)
	median := medianByteConfidence(ranked)
	threshold := median * ConfidenceErasureFactor
	for i, rb := range ranked {
		if i >= ParitySize {
			break
		}
		if median > 0 && rb.confidence < threshold {
			erasures[rb.index] = true
		}
	}
	if version, keyID, payload, ok = Decode(frame, erasures); ok {
		return version, keyID, payload, true
	}
	return Decode(frame, nil)
}

func crc16CCITT(data []byte) uint16 {
	crc := crc16Initial
	for _, b := range data {
		crc ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if crc&crc16HighBit != 0 {
				crc = (crc << 1) ^ crc16Polynomial
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

func encodeRS(data []byte) ([]byte, bool) {
	if len(data) != DataSize {
		return nil, false
	}
	out := make([]byte, FrameSize)
	copy(out, data)
	gen := rsGenerator(ParitySize)
	for i := 0; i < DataSize; i++ {
		coef := out[i]
		if coef == 0 {
			continue
		}
		for j := 1; j < len(gen); j++ {
			out[i+j] ^= gfMul(gen[j], coef)
		}
	}
	copy(out, data)
	return out, true
}

func decodeRS(frame []byte, erasures []bool) ([]byte, bool) {
	if len(frame) != FrameSize {
		return nil, false
	}
	codeword := append([]byte(nil), frame...)
	erasePos := make([]int, 0, ParitySize)
	for i := 0; i < len(codeword) && i < len(erasures); i++ {
		if erasures[i] {
			codeword[i] = 0
			erasePos = append(erasePos, i)
		}
	}
	if len(erasePos) > ParitySize {
		return nil, false
	}
	if !rsCorrect(codeword, erasePos) {
		return nil, false
	}
	out := make([]byte, DataSize)
	copy(out, codeword[:DataSize])
	return out, true
}

func medianByteConfidence(ranked []rankedByte) float64 {
	if len(ranked) == 0 {
		return 0
	}
	cp := append([]rankedByte(nil), ranked...)
	sort.Slice(cp, func(i, j int) bool {
		return cp[i].confidence < cp[j].confidence
	})
	return cp[len(cp)/2].confidence
}

const gfCycleSize = 255             // reason: non-zero GF(256) elements repeat every 255 powers of the primitive element.
const gfPolynomialWrap = 0x100      // reason: GF(2^8) polynomial wrap; values >=0x100 must reduce by xor with the primitive polynomial.
const gfPrimitivePolynomial = 0x11d // reason: GF(2^8) primitive polynomial 0x11d (x^8 + x^4 + x^3 + x^2 + 1), the QR / DSPro standard.

var gfExp [512]byte // reason: doubled GF(256) exponent table size to avoid mod 255 in inner loops.
var gfLog [256]int  // reason: GF(256) discrete log table indexed by byte value.

func init() {
	x := 1
	for i := 0; i < gfCycleSize; i++ {
		gfExp[i] = byte(x)
		gfLog[byte(x)] = i
		x <<= 1
		if x&gfPolynomialWrap != 0 {
			x ^= gfPrimitivePolynomial
		}
	}
	for i := gfCycleSize; i < len(gfExp); i++ {
		gfExp[i] = gfExp[i-gfCycleSize]
	}
}

func gfMul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[gfLog[a]+gfLog[b]]
}

func gfDiv(a, b byte) byte {
	if b == 0 {
		panic("ecc: divide by zero")
	}
	if a == 0 {
		return 0
	}
	return gfExp[(gfLog[a]+gfCycleSize-gfLog[b])%gfCycleSize]
}

func gfPowAlpha(power int) byte {
	power %= gfCycleSize
	if power < 0 {
		power += gfCycleSize
	}
	return gfExp[power]
}

func rsGenerator(nsym int) []byte {
	gen := []byte{1}
	for i := 0; i < nsym; i++ {
		gen = gfPolyMul(gen, []byte{1, gfPowAlpha(i)})
	}
	return gen
}

func gfPolyMul(a, b []byte) []byte {
	out := make([]byte, len(a)+len(b)-1)
	for i, av := range a {
		for j, bv := range b {
			out[i+j] ^= gfMul(av, bv)
		}
	}
	return out
}

func gfPolyEvalDescending(poly []byte, x byte) byte {
	var y byte
	for _, c := range poly {
		y = gfMul(y, x) ^ c
	}
	return y
}

func gfPolyEvalAscending(poly []byte, x byte) byte {
	var y byte
	for i := len(poly) - 1; i >= 0; i-- {
		y = gfMul(y, x) ^ poly[i]
	}
	return y
}

func rsSyndromes(codeword []byte) []byte {
	synd := make([]byte, ParitySize)
	for i := range synd {
		synd[i] = gfPolyEvalDescending(codeword, gfPowAlpha(i))
	}
	return synd
}

func allZero(v []byte) bool {
	for _, x := range v {
		if x != 0 {
			return false
		}
	}
	return true
}

func rsCorrect(codeword []byte, erasePos []int) bool {
	synd := rsSyndromes(codeword)
	if allZero(synd) {
		return true
	}
	if len(erasePos) > ParitySize {
		return false
	}
	forney := forneySyndromes(synd, erasePos, len(codeword))
	errLoc := berlekampMassey(forney)
	errPos := chienSearch(errLoc, len(codeword))
	if len(errPos) == 0 && !allZero(forney) {
		return false
	}
	errata := append([]int(nil), erasePos...)
	for _, pos := range errPos {
		if !containsInt(errata, pos) {
			errata = append(errata, pos)
		}
	}
	if len(errata) == 0 || len(errata) > ParitySize || len(errata)*2-len(erasePos) > ParitySize {
		return false
	}
	magnitudes, ok := solveErrataMagnitudes(synd, errata, len(codeword))
	if !ok {
		return false
	}
	for i, pos := range errata {
		codeword[pos] ^= magnitudes[i]
	}
	return allZero(rsSyndromes(codeword))
}

func forneySyndromes(synd []byte, erasePos []int, msgLen int) []byte {
	out := append([]byte(nil), synd...)
	for _, pos := range erasePos {
		x := gfPowAlpha(msgLen - 1 - pos)
		for i := 0; i < len(out)-1; i++ {
			out[i] = gfMul(out[i], x) ^ out[i+1]
		}
		out = out[:len(out)-1]
	}
	return out
}

func berlekampMassey(synd []byte) []byte {
	c := []byte{1}
	b := []byte{1}
	l, m := 0, 1
	var bb byte = 1
	for n := 0; n < len(synd); n++ {
		d := synd[n]
		for i := 1; i <= l; i++ {
			if i < len(c) {
				d ^= gfMul(c[i], synd[n-i])
			}
		}
		if d == 0 {
			m++
			continue
		}
		t := append([]byte(nil), c...)
		coef := gfDiv(d, bb)
		if len(c) < len(b)+m {
			c = append(c, make([]byte, len(b)+m-len(c))...)
		}
		for j := range b {
			c[j+m] ^= gfMul(coef, b[j])
		}
		if 2*l <= n {
			l = n + 1 - l
			b = t
			bb = d
			m = 1
		} else {
			m++
		}
	}
	for len(c) > 1 && c[len(c)-1] == 0 {
		c = c[:len(c)-1]
	}
	return c
}

func chienSearch(locator []byte, msgLen int) []int {
	degree := len(locator) - 1
	if degree <= 0 {
		return nil
	}
	out := make([]int, 0, degree)
	for pos := 0; pos < msgLen; pos++ {
		x := gfPowAlpha(-(msgLen - 1 - pos))
		if gfPolyEvalAscending(locator, x) == 0 {
			out = append(out, pos)
		}
	}
	if len(out) != degree {
		return nil
	}
	return out
}

func solveErrataMagnitudes(synd []byte, positions []int, msgLen int) ([]byte, bool) {
	n := len(positions)
	matrix := make([][]byte, n)
	for r := 0; r < n; r++ {
		matrix[r] = make([]byte, n+1)
		for c, pos := range positions {
			matrix[r][c] = gfPowAlpha((msgLen - 1 - pos) * r)
		}
		matrix[r][n] = synd[r]
	}
	for col := 0; col < n; col++ {
		pivot := col
		for pivot < n && matrix[pivot][col] == 0 {
			pivot++
		}
		if pivot == n {
			return nil, false
		}
		matrix[col], matrix[pivot] = matrix[pivot], matrix[col]
		inv := gfDiv(1, matrix[col][col])
		for c := col; c <= n; c++ {
			matrix[col][c] = gfMul(matrix[col][c], inv)
		}
		for r := 0; r < n; r++ {
			if r == col || matrix[r][col] == 0 {
				continue
			}
			factor := matrix[r][col]
			for c := col; c <= n; c++ {
				matrix[r][c] ^= gfMul(factor, matrix[col][c])
			}
		}
	}
	out := make([]byte, n)
	for i := range out {
		out[i] = matrix[i][n]
	}
	return out, true
}

func containsInt(values []int, needle int) bool {
	for _, v := range values {
		if v == needle {
			return true
		}
	}
	return false
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
