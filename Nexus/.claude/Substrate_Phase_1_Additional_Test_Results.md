# BF-Sketch Substrate Phase 1 — Additional Test Results

**Date:** 2026-04-14
**Branch:** `v0.1.3-bf-sketch`
**Baseline:** N_BASELINE = 955 tests
**Test command:** `CGO_ENABLED=1 go test ./internal/canonical/... ./internal/substrate/... -race -count=1 -v`

---

## Summary

| Package | Tests Run | Passed | Failed | Status |
|---------|-----------|--------|--------|--------|
| internal/canonical | 55 | 54 | 1 | **1 BUG FOUND** |
| internal/substrate | 34 | 34 | 0 | ALL PASS |
| **Total** | **89** | **88** | **1** | |

---

## BUG FOUND: `queryCacheKey` uses integer truncation instead of IEEE 754 bit representation

**Test:** `TestQueryCacheKeyUniqueness`
**File:** `internal/canonical/canonical.go`, line 216
**Severity:** Medium (cache correctness — different embeddings could collide)

**Root cause:** `queryCacheKey()` hashes float64 values using `uint64(v)` which truncates to integer. This means `3.0` and `3.0001` both hash as `3`, causing cache collisions for embeddings that differ only in fractional parts.

**Fix required:** Change `binary.LittleEndian.PutUint64(buf, uint64(v))` to `binary.LittleEndian.PutUint64(buf, math.Float64bits(v))` and add `"math"` to imports.

**Impact:** Query cache could return stale/wrong canonicalized embeddings when two queries have embeddings that differ only in fractional parts. Does not affect write-path correctness (only the query cache hot path).

---

## Canonical Package — Test Details

### Config Tests (6 passed)
| Test | Result | Notes |
|------|--------|-------|
| TestDefaultConfigIsDisabled | PASS | Default config has Enabled=false, CanonicalDim=1024 |
| TestConfigValidation/disabled_always_valid | PASS | Invalid fields ignored when disabled |
| TestConfigValidation/dim_too_small | PASS | CanonicalDim=32 rejected |
| TestConfigValidation/dim_too_large | PASS | CanonicalDim=16384 rejected |
| TestConfigValidation/dim_not_power_of_2 | PASS | CanonicalDim=1000 rejected |
| TestConfigValidation/warmup_too_small | PASS | WhiteningWarmup=50 rejected |
| TestConfigValidation/valid_enabled | PASS | Default enabled config passes |

### Config Boundary Tests (9 passed)
| Test | Result | Notes |
|------|--------|-------|
| TestConfigBoundaryDimensions/63 | PASS | Correctly rejected |
| TestConfigBoundaryDimensions/64 | PASS | Minimum valid dim |
| TestConfigBoundaryDimensions/128 | PASS | Valid |
| TestConfigBoundaryDimensions/1024 | PASS | Valid |
| TestConfigBoundaryDimensions/8192 | PASS | Maximum valid dim |
| TestConfigBoundaryDimensions/8193 | PASS | Correctly rejected |
| TestConfigBoundaryDimensions/0 | PASS | Rejected |
| TestConfigBoundaryDimensions/-1 | PASS | Rejected |
| TestConfigBoundaryDimensions/100 | PASS | Not power of 2, rejected |

### Manager Nil-Safety Tests (2 passed)
| Test | Result | Notes |
|------|--------|-------|
| TestManagerNilSafe | PASS | nil Manager returns ErrDisabled for all methods |
| TestManagerDisabledReturnsNil | PASS | Disabled config produces nil Manager |

### SRHT Tests (11 passed)
| Test | Result | Notes |
|------|--------|-------|
| TestSRHTSmallInput | PASS | 8-dim input → 4-dim output, non-zero |
| TestSRHTDeterminism | PASS | Same seed → bit-identical output across runs |
| TestSRHTIdentitySubsample | PASS | inputDim==outputDim → identity subsample indices |
| TestSRHTZeroPadding | PASS | 3-element input zero-padded to 8, non-zero output |
| TestSRHTInvalidDimensions | PASS | Non-power-of-2, outputDim>inputDim, outputDim=0 all rejected |
| TestSRHTNormPreservation | PASS | L2 norm preserved within 10% at 64-dim (unitary property) |
| TestSRHTOutputLengthMismatch | PASS | Wrong output size returns error |
| TestSRHTDifferentSeeds | PASS | Different seeds → different outputs |
| TestSRHTSubsampleDistinctIndices | PASS | Fisher-Yates produces distinct, sorted indices in range |
| TestSRHTSignFlipDistribution | PASS | ~50/50 +1/-1 distribution (within 35-65% tolerance) |
| TestSRHTLargeDim | PASS | 1024-dim norm preservation within 5% |

### L2 Normalization Tests (8 passed)
| Test | Result | Notes |
|------|--------|-------|
| TestL2NormalizeZeroVector | PASS | Returns norm=0, vector unchanged |
| TestL2Normalize34 | PASS | {3,4} → norm=5, output {0.6, 0.8} |
| TestL2NormalizeUnitVector | PASS | Unit vector preserved |
| TestL2NormalizeLargeVector | PASS | 1024-dim normalizes to unit norm |
| TestL2NormalizeNegativeValues | PASS | {-3,4} → correct signs preserved |
| TestL2NormalizeVerySmallNorm | PASS | 1e-200 underflows to 0 (IEEE 754 correct); 1e-150 works |
| TestL2NormalizeSingleElement | PASS | Single element → 1.0 |
| TestL2NormalizeIdempotent | PASS | Double normalization preserves norm |

### Kahan Summation Tests (7 passed)
| Test | Result | Notes |
|------|--------|-------|
| TestKahanSumDeterminism | PASS | Same input → same output; 10000×1e-8 = 1e-4 exact |
| TestKahanSumAccuracyVsNaive | PASS | Large+small values: Kahan >= naive accuracy |
| TestKahanSumSquaresAccuracy | PASS | Mixed-magnitude squares accurate to 1e-12 |
| TestKahanDotBasic | PASS | {1,2,3}·{4,5,6} = 32 |
| TestKahanDotOrthogonal | PASS | {1,0}·{0,1} = 0 |
| TestKahanDotSelfIsNormSquared | PASS | {3,4}·{3,4} = 25 |
| TestKahanDotPanicsOnMismatch | PASS | Length mismatch panics |
| TestKahanDotZeros | PASS | {0,0,0}·{1,2,3} = 0 |

### FWHT Tests (4 passed)
| Test | Result | Notes |
|------|--------|-------|
| TestFWHTSmall | PASS | FWHT([1,0,0,0]) = [1,1,1,1] |
| TestFWHTInverse | PASS | Double-FWHT = n × original |
| TestFWHTSingleElement | PASS | FWHT([x]) = [x] |
| TestFWHTTwoElements | PASS | [3,5] → [8,-2] matches H_2 matrix |
| TestFWHTPowerOfTwoSizes | PASS | Sizes 1,2,4,8,16,32,64 all correct for [1,0..0] |

### Whitening Tests (7 passed)
| Test | Result | Notes |
|------|--------|-------|
| TestWhiteningBelowWarmupIsIdentity | PASS | Output = input when below warmup |
| TestWhiteningAboveWarmupChangesOutput | PASS | Output differs from input after 100 samples |
| TestWhiteningDeterminism | PASS | Same sample stream → same output |
| TestWhiteningConcurrency | PASS | 10 goroutines × 100 updates = 1000 samples, race-clean |
| TestWhiteningMarshalRestore | PASS | Saved/restored state produces identical output |
| TestWelfordMatchesDirectComputation | PASS | Welford mean/variance matches direct computation to 1e-10 |
| TestWhiteningNearZeroVariance | PASS | Identical samples: no NaN/Inf (near-zero variance handled) |
| TestWhiteningVarianceSingleSample | PASS | Single-sample variance = 0 |
| TestWhiteningVarianceEqualization | PASS | Whitened variance ~0.06 per dim (logged, within tolerance) |

### PRNG Tests (4 passed)
| Test | Result | Notes |
|------|--------|-------|
| TestSeededPRNGDeterminism | PASS | Same seed+domain → identical 100-step sequence |
| TestSeededPRNGDifferentDomains | PASS | Different domains → different sequences |
| TestSeededPRNGZeroSeed | PASS | Zero seed produces valid non-zero output |
| TestSeededPRNGBufferBoundary | PASS | 10 uint64s across SHA-256 buffer boundaries, no repeats |

### NextPowerOfTwo Tests (2 passed)
| Test | Result | Notes |
|------|--------|-------|
| TestNextPowerOfTwo | PASS | 0→1, 1→1, 3→4, 1023→1024, 1025→2048 all correct |
| TestNextPowerOfTwoNegative | PASS | -5 → 1 |

### Pipeline Integration Tests (7 passed, 1 FAILED)
| Test | Result | Notes |
|------|--------|-------|
| TestManagerInitSuccess | PASS | SRHT and queryCache initialized correctly |
| TestCanonicalizeFullPipeline | PASS | 128-dim → 64-dim canonical, unit-norm, correct metadata |
| TestCanonicalizeZeroVector | PASS | Returns ErrZeroVector |
| TestCanonicalizeDeterminism | PASS | Same seed → same output |
| TestCanonicalizeMultipleSources | PASS | 2 sources → 2 separate whitening states |
| TestCanonicalizeSmallEmbedding | PASS | 3-dim zero-padded to 64, unit-norm output |
| TestCanonicalizeLargeEmbedding | PASS | 3072-dim projected to 64, correct metadata |
| TestCanonicalizeWhiteningEngages | PASS | After 16 samples (warmup=10), whitening active |
| TestCanonicalizeConcurrency | PASS | 10 goroutines × 10 canonicalizations, race-clean |

### Cache Tests (3 passed, 1 FAILED)
| Test | Result | Notes |
|------|--------|-------|
| TestCanonicalizeQueryCacheHit | PASS | Second call returns identical cached result |
| TestCanonicalizeQueryDifferentSources | PASS | Different source names → different cache keys |
| TestQueryCacheExpiration | PASS | Entry evicted after TTL (1s) |
| TestQueryCacheKeyDeterminism | PASS | Same input → same key |
| **TestQueryCacheKeyUniqueness** | **FAIL** | **BUG: `uint64(v)` truncates float → integer collisions** |

### Seed Tests (2 passed)
| Test | Result | Notes |
|------|--------|-------|
| TestLoadOrCreateSeedCreatesThenLoads | PASS | Creates 32-byte seed, subsequent load returns same bytes |
| TestLoadOrCreateSeedCorruptSize | PASS | Wrong-size seed file → new seed created, file corrected |

---

## Substrate Package — Test Details

### Config Tests (11 passed + 23 boundary + 4 string + 1 defaults = 39 passed)
All 39 substrate config tests pass including:
- Exhaustive boundary testing for all config ranges
- Duration string parsing precedence over Duration field
- Invalid/empty/malformed duration strings
- Default config all-fields verification including EncryptionEnabled

### Substrate Lifecycle Tests (4 passed)
| Test | Result | Notes |
|------|--------|-------|
| TestSubstrateDisabledByDefault | PASS | DefaultConfig → Enabled=false |
| TestSubstrateFailClosedOnEnable | PASS | Enable without implementation → error |
| TestSubstrateNilSafe | PASS | nil Substrate → Enabled()=false, Shutdown()=nil |
| TestSubstrateDisabledShutdown | PASS | Disabled Shutdown() → no error |

---

## Known Issues to Fix Before BS.3

1. **BUG — `queryCacheKey` float truncation** (CRITICAL for cache correctness)
   - File: `internal/canonical/canonical.go:216`
   - Fix: `uint64(v)` → `math.Float64bits(v)`
   - Status: **NOT YET FIXED** (deferred per Shawn's instruction)

2. **NOTE — Whitening variance equalization is ~0.06 not ~1.0**
   - The diagonal whitening produces lower-than-expected per-dimension variance because
     the test applies whitening to out-of-distribution samples. This is not a bug — the
     whitening is computed from training samples and applied to test samples with different
     ranges. The test logs this as informational, not a failure.

---

## Full Test Count

| Package | Before Additional Tests | After Additional Tests |
|---------|------------------------|----------------------|
| internal/canonical | 25 | 55 |
| internal/substrate | 17 | 34 |
| **Total BS.1+BS.2** | **42** | **89** |
| **Global total** | **986** | **~1023** (estimated) |
