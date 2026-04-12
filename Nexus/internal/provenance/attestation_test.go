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

package provenance

import (
	"testing"
)

func TestBuildAndVerifyQueryAttestation(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	query := []byte(`{"destination":"sqlite","subject":"test","limit":10}`)
	results := [][]byte{
		[]byte(`{"payload_id":"m1","content":"hello"}`),
		[]byte(`{"payload_id":"m2","content":"world"}`),
	}

	att, err := BuildQueryAttestation(query, results, kp)
	if err != nil {
		t.Fatalf("BuildQueryAttestation: %v", err)
	}

	if att.QueryHash == "" || att.ResultSetHash == "" || att.DaemonSignature == "" {
		t.Error("missing fields in attestation")
	}
	if att.ResultCount != 2 {
		t.Errorf("result_count = %d, want 2", att.ResultCount)
	}

	valid, err := VerifyQueryAttestation(att)
	if err != nil {
		t.Fatalf("VerifyQueryAttestation: %v", err)
	}
	if !valid {
		t.Error("attestation should be valid")
	}
}

func TestVerifyQueryAttestation_Tampered(t *testing.T) {
	kp, _ := GenerateKeyPair()
	query := []byte(`{"q":"test"}`)
	results := [][]byte{[]byte(`{"content":"c"}`)}

	att, _ := BuildQueryAttestation(query, results, kp)

	// Tamper with result set hash.
	att.ResultSetHash = "0000000000000000000000000000000000000000000000000000000000000000"

	valid, err := VerifyQueryAttestation(att)
	if err != nil {
		t.Fatal(err)
	}
	if valid {
		t.Error("tampered attestation should fail")
	}
}

func TestBuildQueryAttestation_EmptyResults(t *testing.T) {
	kp, _ := GenerateKeyPair()
	query := []byte(`{"q":"nothing"}`)

	att, err := BuildQueryAttestation(query, nil, kp)
	if err != nil {
		t.Fatal(err)
	}
	if att.ResultCount != 0 {
		t.Errorf("result_count = %d, want 0", att.ResultCount)
	}

	valid, _ := VerifyQueryAttestation(att)
	if !valid {
		t.Error("empty result attestation should be valid")
	}
}
