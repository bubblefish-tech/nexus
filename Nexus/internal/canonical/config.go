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

package canonical

// Config holds the [canonical] TOML section configuration.
type Config struct {
	// Enabled controls whether the canonicalization pipeline is active.
	// Default: false.
	Enabled bool `toml:"enabled"`

	// CanonicalDim is the target dimension for all canonical vectors.
	// Must be a power of 2 in [64, 8192]. Default: 1024.
	CanonicalDim int `toml:"canonical_dim"`

	// WhiteningWarmup is the number of samples required before per-source
	// whitening engages. Below this threshold, whitening is the identity
	// transform. Must be >= 100. Default: 1000.
	WhiteningWarmup int `toml:"whitening_warmup"`

	// QueryCacheTTLSeconds is the TTL for the query-path canonicalization
	// cache. Default: 60.
	QueryCacheTTLSeconds int `toml:"query_cache_ttl_seconds"`
}

// DefaultConfig returns a Config with all defaults applied.
// Canonical is disabled by default.
func DefaultConfig() Config {
	return Config{
		Enabled:              false,
		CanonicalDim:         1024,
		WhiteningWarmup:      1000,
		QueryCacheTTLSeconds: 60,
	}
}

// Validate checks that all fields are within acceptable ranges.
func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.CanonicalDim < 64 || c.CanonicalDim > 8192 {
		return ErrInvalidCanonicalDim
	}
	if c.CanonicalDim&(c.CanonicalDim-1) != 0 {
		return ErrCanonicalDimNotPowerOfTwo
	}
	if c.WhiteningWarmup < 100 {
		return ErrWhiteningWarmupTooSmall
	}
	return nil
}
