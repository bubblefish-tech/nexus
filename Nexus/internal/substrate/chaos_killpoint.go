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

import (
	"os"
	"sync"
)

var (
	chaosKillAt   string
	chaosKillOnce sync.Once
)

// ChaosKillPoint checks if the current execution should terminate at the
// named point. Used for crash recovery testing via the BF_CHAOS_KILL_AT
// environment variable. Zero cost when the variable is unset: the env
// read is cached on first call via sync.Once, and subsequent calls are
// a single string comparison.
//
// Named kill points in the substrate:
//   - "sketch_write"                  — after DB commit, before cuckoo insert
//   - "ratchet_advance_after_insert"  — after new state persisted, before old shredded
//   - "cuckoo_persist"                — after encode, before SQL write
//   - "shred_after_clear"             — after ciphertext cleared, before cuckoo delete
func ChaosKillPoint(name string) {
	chaosKillOnce.Do(func() {
		chaosKillAt = os.Getenv("BF_CHAOS_KILL_AT")
	})
	if chaosKillAt != "" && chaosKillAt == name {
		os.Exit(137) // simulate SIGKILL exit code
	}
}
