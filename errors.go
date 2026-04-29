// SPDX-License-Identifier: AGPL-3.0-only

// Copyright (C) 2026 MTN Media Group.

package verum

import (
	"errors"
	"fmt"

	"github.com/MTN-Media-Group/mtn-verum/internal/codec"
)

var (
	ErrUnsupportedFormat   = errors.New("verum: unsupported image format")
	ErrImageTooLarge       = errors.New("verum: image exceeds maximum dimensions")
	ErrImageTooSmall       = errors.New("verum: image smaller than minimum dimension")
	ErrNoCapacity          = errors.New("verum: not enough usable tiles to embed payload")
	ErrQualityGateFailed   = errors.New("verum: embed exceeded quality gates")
	ErrSelfDetectionFailed = errors.New("verum: self-detection could not confirm embedded mark")
	ErrInvalidConfig       = errors.New("verum: invalid configuration")
	ErrNoDetectionKeys     = errors.New("verum: detection requires at least one key")
)

func wrapCodecDecodeError(err error) error {
	if errors.Is(err, codec.ErrImageTooLarge) {
		return fmt.Errorf("%w: %w", ErrImageTooLarge, err)
	}
	return fmt.Errorf("%w: %w", ErrUnsupportedFormat, err)
}
