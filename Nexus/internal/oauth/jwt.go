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
	"encoding/hex"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Audience is the expected aud claim for Nexus JWT access tokens.
const Audience = "nexus-nexus"

// nexusClaims defines the JWT claims used by Nexus OAuth access tokens.
type nexusClaims struct {
	jwt.RegisteredClaims
	Scope     string `json:"scope"`
	BFNSource string `json:"bfn_source"` // maps to source config name
}

// SignJWT creates an RS256-signed JWT access token.
func SignJWT(key *rsa.PrivateKey, issuer, subject, scope, bfnSource string, ttl time.Duration) (string, error) {
	jti, err := generateJTI()
	if err != nil {
		return "", err
	}

	now := time.Now().UTC()
	claims := nexusClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   subject,
			Audience:  jwt.ClaimStrings{Audience},
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        jti,
		},
		Scope:     scope,
		BFNSource: bfnSource,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = KeyID

	signed, err := token.SignedString(key)
	if err != nil {
		return "", fmt.Errorf("oauth: sign JWT: %w", err)
	}
	return signed, nil
}

// ValidateJWT parses and validates an RS256-signed JWT against the given public key.
// It checks the signature, expiration, audience, and issuer claims.
// On success it returns the parsed claims.
func ValidateJWT(tokenString string, pub *rsa.PublicKey, expectedIssuer string) (*nexusClaims, error) {
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithAudience(Audience),
		jwt.WithIssuer(expectedIssuer),
		jwt.WithExpirationRequired(),
	)

	token, err := parser.ParseWithClaims(tokenString, &nexusClaims{}, func(t *jwt.Token) (any, error) {
		return pub, nil
	})
	if err != nil {
		return nil, fmt.Errorf("oauth: validate JWT: %w", err)
	}

	claims, ok := token.Claims.(*nexusClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("oauth: invalid token claims")
	}

	return claims, nil
}

// ValidateAccessToken checks whether tokenString is a valid RS256 JWT
// signed by this server. It validates the signature, exp, aud, and iss claims.
// Returns true only if ALL checks pass.
func (s *OAuthServer) ValidateAccessToken(tokenString string) bool {
	_, err := ValidateJWT(tokenString, s.publicKey, s.config.IssuerURL)
	if err != nil {
		s.logger.Debug("oauth: JWT validation failed",
			"component", "oauth",
			"error", err,
		)
		return false
	}
	return true
}

// generateJTI produces a random 16-byte hex-encoded JTI.
func generateJTI() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("oauth: generate JTI: %w", err)
	}
	return hex.EncodeToString(b), nil
}
