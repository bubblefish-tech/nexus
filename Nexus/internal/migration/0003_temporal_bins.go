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

package migration

// TemporalBinsSQL populates temporal bins from existing timestamps and creates
// a composite index for bin-filtered queries. The temporal_bin column is added
// by applyPragmasAndSchema() to ensure test DBs have it at open time.
const TemporalBinsSQL = `
UPDATE memories SET temporal_bin = CASE
    WHEN timestamp > datetime('now', '-1 hour')  THEN 0
    WHEN timestamp > datetime('now', '-1 day')    THEN 1
    WHEN timestamp > datetime('now', '-2 days')   THEN 2
    WHEN timestamp > datetime('now', '-7 days')   THEN 3
    WHEN timestamp > datetime('now', '-14 days')  THEN 4
    WHEN timestamp > datetime('now', '-30 days')  THEN 5
    WHEN timestamp > datetime('now', '-60 days')  THEN 6
    WHEN timestamp > datetime('now', '-90 days')  THEN 7
    WHEN timestamp > datetime('now', '-365 days') THEN 8
    WHEN timestamp > datetime('now', '-730 days') THEN 9
    ELSE 10
END;

CREATE INDEX IF NOT EXISTS idx_memories_temporal_bin
ON memories(temporal_bin, namespace, destination);
`
