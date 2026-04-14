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

// EstimateInnerProduct computes the RaBitQ estimate of <canonical_store, canonical_query>
// given the store sketch (1-bit signs + correction factors) and query sketch (4-bit
// asymmetric coefficients).
//
// The estimator combines the 1-bit store signs with the 4-bit query coefficients,
// weighted by the correction factors. This is a clean-room port of the algorithm
// described in the RaBitQ paper (SIGMOD 2024), Section 4.
//
// Uses Kahan summation for cross-platform determinism.
//
// Returns an error if the sketches have mismatched dimensions or ratchet states.
func EstimateInnerProduct(store *StoreSketch, query *QuerySketch) (float64, error) {
	if store == nil || query == nil {
		return 0, errors.New("bbq: nil sketch")
	}
	if store.CanonicalDim != query.CanonicalDim {
		return 0, errors.New("bbq: dimension mismatch")
	}
	if store.StateID != query.StateID {
		return 0, errors.New("bbq: ratchet state mismatch; sketches incomparable")
	}

	d := int(store.CanonicalDim)
	posScale := float64(store.Corrections[0]) // positive-side L2 norm
	negScale := float64(store.Corrections[1]) // negative-side L2 norm

	var sum, c float64 // Kahan accumulator

	for i := 0; i < d; i++ {
		// Read store sign bit
		var storeSign float64
		if store.SignBits[i/8]&(1<<uint(i%8)) != 0 {
			storeSign = +1
		} else {
			storeSign = -1
		}

		// Read query 4-bit coefficient
		queryCoef := unpackQueryCoefficient(query.Coefficients, i)
		qVal := float64(queryCoef) / 7.0

		// Contribution weighted by the correction factor for the sign side
		var contribution float64
		if storeSign > 0 {
			contribution = posScale * qVal
		} else {
			contribution = -negScale * qVal
		}

		// Kahan accumulate
		y := contribution - c
		t := sum + y
		c = (t - sum) - y
		sum = t
	}

	return sum / float64(d), nil
}
