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

package a2a

import (
	"encoding/json"
	"sort"
	"testing"
)

func TestExtensionMapGetSet(t *testing.T) {
	em := make(ExtensionMap)
	em.Set("ext/v1", json.RawMessage(`{"enabled":true}`))

	got := em.Get("ext/v1")
	if string(got) != `{"enabled":true}` {
		t.Errorf("Get = %s", got)
	}
	if em.Get("missing") != nil {
		t.Error("missing key should return nil")
	}
}

func TestExtensionMapGetNil(t *testing.T) {
	var em ExtensionMap
	if em.Get("anything") != nil {
		t.Error("nil map Get should return nil")
	}
}

func TestExtensionMapHas(t *testing.T) {
	em := make(ExtensionMap)
	em.Set("ext/v1", json.RawMessage(`{}`))
	if !em.Has("ext/v1") {
		t.Error("Has should return true for existing key")
	}
	if em.Has("ext/v2") {
		t.Error("Has should return false for missing key")
	}
}

func TestExtensionMapHasNil(t *testing.T) {
	var em ExtensionMap
	if em.Has("anything") {
		t.Error("nil map Has should return false")
	}
}

func TestExtensionMapDelete(t *testing.T) {
	em := make(ExtensionMap)
	em.Set("ext/v1", json.RawMessage(`{}`))
	em.Delete("ext/v1")
	if em.Has("ext/v1") {
		t.Error("key should be deleted")
	}
}

func TestExtensionMapMerge(t *testing.T) {
	em := make(ExtensionMap)
	em.Set("ext/v1", json.RawMessage(`{"a":1}`))
	em.Set("ext/v2", json.RawMessage(`{"b":2}`))

	other := make(ExtensionMap)
	other.Set("ext/v2", json.RawMessage(`{"b":99}`))
	other.Set("ext/v3", json.RawMessage(`{"c":3}`))

	em.Merge(other)

	if string(em.Get("ext/v1")) != `{"a":1}` {
		t.Error("ext/v1 should be unchanged")
	}
	if string(em.Get("ext/v2")) != `{"b":99}` {
		t.Error("ext/v2 should be overwritten")
	}
	if string(em.Get("ext/v3")) != `{"c":3}` {
		t.Error("ext/v3 should be added")
	}
}

func TestExtensionMapSetValue(t *testing.T) {
	em := make(ExtensionMap)
	gov := GovernanceExtension{
		SourceAgentID: "agent-a",
		TargetAgentID: "agent-b",
		Decision:      GovernanceAllow,
	}
	if err := em.SetValue(GovernanceExtensionURI, gov); err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	if !em.Has(GovernanceExtensionURI) {
		t.Error("governance extension should be present")
	}
}

func TestExtensionMapGetValue(t *testing.T) {
	em := make(ExtensionMap)
	em.Set("ext/v1", json.RawMessage(`{"sourceAgentId":"a","targetAgentId":"b","decision":"allow"}`))

	var gov GovernanceExtension
	found, err := em.GetValue("ext/v1", &gov)
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	if !found {
		t.Error("should be found")
	}
	if gov.SourceAgentID != "a" {
		t.Errorf("sourceAgentId = %q", gov.SourceAgentID)
	}
}

func TestExtensionMapGetValueMissing(t *testing.T) {
	em := make(ExtensionMap)
	var v interface{}
	found, err := em.GetValue("missing", &v)
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	if found {
		t.Error("should not be found")
	}
}

func TestExtensionMapKeys(t *testing.T) {
	em := make(ExtensionMap)
	em.Set("b", json.RawMessage(`{}`))
	em.Set("a", json.RawMessage(`{}`))
	em.Set("c", json.RawMessage(`{}`))

	keys := em.Keys()
	sort.Strings(keys)
	if len(keys) != 3 || keys[0] != "a" || keys[1] != "b" || keys[2] != "c" {
		t.Errorf("keys = %v", keys)
	}
}

func TestExtensionMapJSONRoundtrip(t *testing.T) {
	em := make(ExtensionMap)
	em.Set("ext/v1", json.RawMessage(`{"x":1}`))
	em.Set("ext/v2", json.RawMessage(`[1,2,3]`))

	data, err := json.Marshal(em)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got ExtensionMap
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(got.Get("ext/v1")) != `{"x":1}` {
		t.Errorf("ext/v1 = %s", got.Get("ext/v1"))
	}
	if string(got.Get("ext/v2")) != `[1,2,3]` {
		t.Errorf("ext/v2 = %s", got.Get("ext/v2"))
	}
}
