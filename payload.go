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

const PayloadVersion = 2 // reason: on-image frame version 2 widens key IDs to 8 bytes.

type Payload struct {
	GeneratedAt  time.Time
	Provider     string
	Model        string
	GenerationID string
	AttachmentID string
	Nonce        []byte
	Digest       []byte
}

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
