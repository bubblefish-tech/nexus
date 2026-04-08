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

package oauth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
)

// KeyID is the kid used in JWT headers and JWKS responses.
const KeyID = "nexus-1"

// GenerateRSAKey generates a new RSA-2048 private key.
func GenerateRSAKey() (*rsa.PrivateKey, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("oauth: generate RSA key: %w", err)
	}
	return key, nil
}

// SaveRSAKey writes a PKCS#8 PEM-encoded private key to path with 0600 permissions.
// The private key MUST NEVER appear in logs or error messages.
func SaveRSAKey(key *rsa.PrivateKey, path string) error {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return fmt.Errorf("oauth: marshal private key: %w", err)
	}

	block := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: der,
	}

	data := pem.EncodeToMemory(block)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("oauth: write key file: %w", err)
	}
	return nil
}

// LoadRSAKey reads a PEM-encoded private key (PKCS#8 or PKCS#1) from path.
func LoadRSAKey(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("oauth: read key file: %w", err)
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("oauth: no PEM block found in key file")
	}

	// Try PKCS#8 first, then fall back to PKCS#1.
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		rsaKey, err2 := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err2 != nil {
			return nil, fmt.Errorf("oauth: parse private key: %w", err)
		}
		return rsaKey, nil
	}

	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("oauth: key is not RSA")
	}
	return rsaKey, nil
}

// JWK represents a single JSON Web Key for JWKS responses.
type JWK struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// PublicKeyToJWK converts an RSA public key to a JWK suitable for JWKS responses.
// The n and e fields are base64url encoded without padding per RFC 7517.
func PublicKeyToJWK(pub *rsa.PublicKey) JWK {
	return JWK{
		Kty: "RSA",
		Use: "sig",
		Alg: "RS256",
		Kid: KeyID,
		N:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(bigEndianUint(pub.E)),
	}
}

// JWKSResponse wraps a set of JWKs.
type JWKSResponse struct {
	Keys []JWK `json:"keys"`
}

// MarshalJWKS returns the JWKS JSON for the given public key.
func MarshalJWKS(pub *rsa.PublicKey) ([]byte, error) {
	resp := JWKSResponse{
		Keys: []JWK{PublicKeyToJWK(pub)},
	}
	return json.Marshal(resp)
}

// bigEndianUint encodes an int as big-endian bytes with no leading zeros.
func bigEndianUint(v int) []byte {
	if v == 0 {
		return []byte{0}
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte(v & 0xFF)}, b...)
		v >>= 8
	}
	return b
}
