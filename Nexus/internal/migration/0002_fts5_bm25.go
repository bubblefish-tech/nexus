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

// FTS5BM25SQL creates the FTS5 virtual table for BM25 sparse keyword retrieval,
// populates it from existing memories, and installs triggers to keep it in sync.
const FTS5BM25SQL = `
CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
    content,
    subject,
    content='memories',
    content_rowid='rowid',
    tokenize='porter unicode61'
);

INSERT INTO memories_fts(rowid, content, subject)
SELECT rowid, content, COALESCE(subject, '') FROM memories;

CREATE TRIGGER IF NOT EXISTS memories_fts_insert AFTER INSERT ON memories BEGIN
    INSERT INTO memories_fts(rowid, content, subject)
    VALUES (new.rowid, new.content, COALESCE(new.subject, ''));
END;

CREATE TRIGGER IF NOT EXISTS memories_fts_delete AFTER DELETE ON memories BEGIN
    INSERT INTO memories_fts(memories_fts, rowid, content, subject)
    VALUES ('delete', old.rowid, old.content, COALESCE(old.subject, ''));
END;

CREATE TRIGGER IF NOT EXISTS memories_fts_update AFTER UPDATE ON memories BEGIN
    INSERT INTO memories_fts(memories_fts, rowid, content, subject)
    VALUES ('delete', old.rowid, old.content, COALESCE(old.subject, ''));
    INSERT INTO memories_fts(rowid, content, subject)
    VALUES (new.rowid, new.content, COALESCE(new.subject, ''));
END;
`
