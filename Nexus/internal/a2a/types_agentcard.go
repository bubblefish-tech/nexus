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

import "encoding/json"

// TransportKind identifies the transport protocol for an Endpoint.
type TransportKind string

const (
	TransportHTTP    TransportKind = "http"
	TransportJSONRPC TransportKind = "jsonrpc"
	TransportSSE     TransportKind = "sse"
	TransportStdio   TransportKind = "stdio"
	TransportTunnel  TransportKind = "tunnel"
	TransportWSL     TransportKind = "wsl"
)

// allTransportKinds is the canonical list of valid transport kinds.
var allTransportKinds = []TransportKind{
	TransportHTTP,
	TransportJSONRPC,
	TransportSSE,
	TransportStdio,
	TransportTunnel,
	TransportWSL,
}

// AllTransportKinds returns all valid TransportKind values.
func AllTransportKinds() []TransportKind {
	out := make([]TransportKind, len(allTransportKinds))
	copy(out, allTransportKinds)
	return out
}

// String returns the string representation of a TransportKind.
func (tk TransportKind) String() string {
	return string(tk)
}

// ValidTransportKind returns true if tk is a known TransportKind value.
func ValidTransportKind(tk TransportKind) bool {
	for _, v := range allTransportKinds {
		if v == tk {
			return true
		}
	}
	return false
}

// Endpoint describes a reachable endpoint for an agent.
type Endpoint struct {
	URL       string        `json:"url"`
	Transport TransportKind `json:"transport"`
}

// AuthConfig describes the authentication methods an agent supports.
type AuthConfig struct {
	Schemes []string `json:"schemes,omitempty"`
	// Credentials is opaque to the protocol; agents define their own schema.
	Credentials json.RawMessage `json:"credentials,omitempty"`
}

// AgentCapabilities declares what an agent can do.
type AgentCapabilities struct {
	Streaming        bool `json:"streaming,omitempty"`
	PushNotifications bool `json:"pushNotifications,omitempty"`
	StateTransitions bool `json:"stateTransitions,omitempty"`
}

// Skill declares a skill that an agent can perform.
type Skill struct {
	ID                   string          `json:"id"`
	Name                 string          `json:"name"`
	Description          string          `json:"description,omitempty"`
	Tags                 []string        `json:"tags,omitempty"`
	RequiredCapabilities []string        `json:"requiredCapabilities,omitempty"`
	Destructive          bool            `json:"destructive,omitempty"`
	InputSchema          json.RawMessage `json:"inputSchema,omitempty"`
	OutputSchema         json.RawMessage `json:"outputSchema,omitempty"`
	Examples             json.RawMessage `json:"examples,omitempty"`
}

// ExtensionDecl declares an extension that an agent supports.
type ExtensionDecl struct {
	URI      string `json:"uri"`
	Required bool   `json:"required,omitempty"`
}

// PublicKeyJWK is a JSON Web Key used for Agent Card signing.
type PublicKeyJWK struct {
	Kty string `json:"kty"`
	Crv string `json:"crv,omitempty"`
	X   string `json:"x,omitempty"`
	Y   string `json:"y,omitempty"`
	N   string `json:"n,omitempty"`
	E   string `json:"e,omitempty"`
	Kid string `json:"kid,omitempty"`
	Alg string `json:"alg,omitempty"`
	Use string `json:"use,omitempty"`
}

// CardSignature is a detached signature over the canonical Agent Card.
type CardSignature struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"keyId"`
	Value     string `json:"value"`
}

// AgentCard is the self-description document published by an A2A agent.
type AgentCard struct {
	Name                string            `json:"name"`
	Description         string            `json:"description,omitempty"`
	URL                 string            `json:"url"`
	Version             string            `json:"version,omitempty"`
	ProtocolVersion     string            `json:"protocolVersion"`
	Implementation      string            `json:"implementation,omitempty"`
	Endpoints           []Endpoint        `json:"endpoints"`
	Auth                *AuthConfig       `json:"auth,omitempty"`
	Capabilities        AgentCapabilities `json:"capabilities"`
	Skills              []Skill           `json:"skills,omitempty"`
	Extensions          []ExtensionDecl   `json:"extensions,omitempty"`
	PublicKeys          []PublicKeyJWK    `json:"publicKeys,omitempty"`
	Signature           *CardSignature    `json:"signature,omitempty"`
	DocumentationURL    string            `json:"documentationUrl,omitempty"`
	TermsOfServiceURL   string            `json:"termsOfServiceUrl,omitempty"`
}
