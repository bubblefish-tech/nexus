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
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// authCode represents a stored authorization code issued during the OAuth
// consent flow. Codes are single-use and expire after the configured TTL.
type authCode struct {
	Code          string
	ClientID      string
	RedirectURI   string
	CodeChallenge string // base64url(SHA256(verifier))
	Scope         string
	SourceName    string
	IssuedAt      time.Time
	ExpiresAt     time.Time
	Used          bool
}

// CodeStore is a thread-safe, in-memory store for OAuth authorization codes.
// It runs a background purge goroutine every 60 seconds to remove expired codes.
type CodeStore struct {
	mu    sync.RWMutex
	codes map[string]*authCode
	stop  chan struct{}
	once  sync.Once
}

// NewCodeStore creates and starts a CodeStore with its purge goroutine.
func NewCodeStore() *CodeStore {
	cs := &CodeStore{
		codes: make(map[string]*authCode),
		stop:  make(chan struct{}),
	}
	go cs.purgeLoop()
	return cs
}

// GenerateCode creates a cryptographically random 32-byte code (64 hex chars).
func GenerateCode() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("oauth: generate auth code: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// Store adds an authorization code to the store.
func (cs *CodeStore) Store(code *authCode) {
	cs.mu.Lock()
	cs.codes[code.Code] = code
	cs.mu.Unlock()
}

// Consume retrieves and marks an authorization code as used.
// Returns nil if the code does not exist, has expired, or was already used.
func (cs *CodeStore) Consume(code string) *authCode {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	ac, ok := cs.codes[code]
	if !ok {
		return nil
	}
	if ac.Used || time.Now().After(ac.ExpiresAt) {
		delete(cs.codes, code)
		return nil
	}
	ac.Used = true
	delete(cs.codes, code)
	return ac
}

// Stop shuts down the purge goroutine. Safe to call multiple times.
func (cs *CodeStore) Stop() {
	cs.once.Do(func() {
		close(cs.stop)
	})
}

// purgeLoop removes expired codes every 60 seconds.
func (cs *CodeStore) purgeLoop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-cs.stop:
			return
		case <-ticker.C:
			cs.purge()
		}
	}
}

// purge removes all expired codes from the store.
func (cs *CodeStore) purge() {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	now := time.Now()
	for code, ac := range cs.codes {
		if now.After(ac.ExpiresAt) || ac.Used {
			delete(cs.codes, code)
		}
	}
}

// Len returns the number of codes currently in the store (for testing).
func (cs *CodeStore) Len() int {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return len(cs.codes)
}
