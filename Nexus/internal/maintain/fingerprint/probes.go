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

package fingerprint

import "encoding/json"

// defaultProbes returns the ordered probe list. More-specific probes come first
// so that tools implementing multiple API layers are identified by their native
// protocol rather than the generic OpenAI-compat fallback.
func defaultProbes() []Probe {
	return []Probe{
		{
			// Ollama exposes /api/tags which returns {"models":[...]}.
			// This endpoint is unique to Ollama — check it before OpenAI compat.
			Name:  "ollama-tags",
			Path:  "/api/tags",
			Proto: ProtocolOllamaNative,
			Match: func(status int, body []byte) bool {
				return status == 200 && hasField(body, "models")
			},
		},
		{
			// TGI /info returns {"model_id":"...","max_total_tokens":...}.
			Name:  "tgi-info",
			Path:  "/info",
			Proto: ProtocolTGI,
			Match: func(status int, body []byte) bool {
				return status == 200 && hasField(body, "model_id") && hasField(body, "max_total_tokens")
			},
		},
		{
			// KoboldCpp /api/v1/info returns {"result":"KoboldCpp",...}.
			Name:  "koboldcpp-info",
			Path:  "/api/v1/info",
			Proto: ProtocolKoboldCpp,
			Match: func(status int, body []byte) bool {
				if status != 200 || !hasField(body, "result") {
					return false
				}
				var m map[string]json.RawMessage
				if err := json.Unmarshal(body, &m); err != nil {
					return false
				}
				var result string
				if err := json.Unmarshal(m["result"], &result); err != nil {
					return false
				}
				return result == "KoboldCpp"
			},
		},
		{
			// Tabby ML /v1/health returns {"device":"cuda",...} or similar.
			Name:  "tabby-health",
			Path:  "/v1/health",
			Proto: ProtocolTabby,
			Match: func(status int, body []byte) bool {
				return status == 200 && hasField(body, "device")
			},
		},
		{
			// OpenAI-compat /v1/models returns {"object":"list","data":[...]}.
			// This is the most generic probe — kept last.
			Name:  "openai-models",
			Path:  "/v1/models",
			Proto: ProtocolOpenAICompat,
			Match: func(status int, body []byte) bool {
				return status == 200 && hasField(body, "data")
			},
		},
		{
			// Some OpenAI-compat servers respond to /v1/models with a different shape.
			// Fall back to checking the /v1/ prefix alone via /v1/completions 400.
			// A 400 (not 404) means the server understands the OpenAI route.
			Name:  "openai-completions-probe",
			Path:  "/v1/completions",
			Proto: ProtocolOpenAICompat,
			Match: func(status int, body []byte) bool {
				// 400 = server understood the route but rejected missing body → OpenAI compat
				return status == 400 && (hasField(body, "error") || hasField(body, "detail"))
			},
		},
	}
}

// hasField returns true when body is a valid JSON object containing fieldName
// at the top level.
func hasField(body []byte, fieldName string) bool {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return false
	}
	_, ok := m[fieldName]
	return ok
}
