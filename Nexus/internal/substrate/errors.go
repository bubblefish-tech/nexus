// Copyright © 2026 BubbleFish Technologies, Inc.
//
// This file is part of BubbleFish Nexus.
//
// BubbleFish Nexus is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// BubbleFish Nexus is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with BubbleFish Nexus. If not, see <https://www.gnu.org/licenses/>.

package substrate

import "errors"

var (
	// ErrDisabled is returned when substrate is not enabled.
	ErrDisabled = errors.New("substrate: disabled")

	// ErrRatchetShredded is returned when a ratchet state has been
	// shredded and can no longer be used for key derivation.
	ErrRatchetShredded = errors.New("substrate: ratchet state shredded")

	// ErrCorruptSketch is returned when sketch bytes are invalid.
	ErrCorruptSketch = errors.New("substrate: corrupt sketch data")

	// ErrMigration is returned when a schema migration fails.
	ErrMigration = errors.New("substrate: migration failed")

	// ErrCuckooFull is returned when the cuckoo filter is full even
	// after capacity doubling.
	ErrCuckooFull = errors.New("substrate: cuckoo filter full after capacity doubling")

	// ErrCuckooNeedsRebuild is returned when the cuckoo filter needs
	// to be rebuilt from the database.
	ErrCuckooNeedsRebuild = errors.New("substrate: cuckoo filter needs rebuild from database")

	// ErrCuckooNotPersisted is returned when no persisted filter exists.
	ErrCuckooNotPersisted = errors.New("substrate: cuckoo filter not persisted; rebuild required")

	// ErrCuckooCorrupt is returned when the persisted filter fails to decode.
	ErrCuckooCorrupt = errors.New("substrate: cuckoo filter serialization corrupt")

	// ErrEmbeddingUnreachable is returned when an embedding cannot be
	// decrypted because its key has been shredded. This is the expected
	// behavior after a shred-seed delete and must not be treated as a bug.
	ErrEmbeddingUnreachable = errors.New("substrate: embedding unreachable (key shredded or corrupt)")

	// ErrUnsupportedSketchBits is returned when sketch_bits != 1.
	ErrUnsupportedSketchBits = errors.New("substrate: only sketch_bits=1 is supported in v0.1.3")

	// ErrRatchetRotationTooFast is returned when ratchet_rotation_period < 1h.
	ErrRatchetRotationTooFast = errors.New("substrate: ratchet_rotation_period must be >= 1h")

	// ErrPrefilterThresholdTooSmall is returned when prefilter_threshold < 50.
	ErrPrefilterThresholdTooSmall = errors.New("substrate: prefilter_threshold must be >= 50")

	// ErrInvalidPrefilterTopK is returned when prefilter_top_k is out of range.
	ErrInvalidPrefilterTopK = errors.New("substrate: prefilter_top_k must be in [10, prefilter_threshold]")

	// ErrCuckooCapacityTooSmall is returned when cuckoo_capacity < 1000.
	ErrCuckooCapacityTooSmall = errors.New("substrate: cuckoo_capacity must be >= 1000")

	// ErrInvalidRebuildThreshold is returned when cuckoo_rebuild_threshold
	// is not in (0.5, 1.0).
	ErrInvalidRebuildThreshold = errors.New("substrate: cuckoo_rebuild_threshold must be in (0.5, 1.0)")
)
