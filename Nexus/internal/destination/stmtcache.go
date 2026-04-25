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

package destination

import "database/sql"

// StmtCache holds prepared statements for hot-path queries.
type StmtCache struct {
	write  *sql.Stmt
	search *sql.Stmt
	read   *sql.Stmt
}

// NewStmtCache prepares the given SQL statements against db.
// Pass empty string for any statement that should not be cached.
func NewStmtCache(db *sql.DB, writeSQL, searchSQL, readSQL string) (*StmtCache, error) {
	c := &StmtCache{}
	var err error
	if writeSQL != "" {
		c.write, err = db.Prepare(writeSQL)
		if err != nil {
			return nil, err
		}
	}
	if searchSQL != "" {
		c.search, err = db.Prepare(searchSQL)
		if err != nil {
			c.Close()
			return nil, err
		}
	}
	if readSQL != "" {
		c.read, err = db.Prepare(readSQL)
		if err != nil {
			c.Close()
			return nil, err
		}
	}
	return c, nil
}

// Write returns the prepared write statement, or nil.
func (c *StmtCache) Write() *sql.Stmt { return c.write }

// Search returns the prepared search statement, or nil.
func (c *StmtCache) Search() *sql.Stmt { return c.search }

// Read returns the prepared read statement, or nil.
func (c *StmtCache) Read() *sql.Stmt { return c.read }

// Close closes all cached prepared statements.
func (c *StmtCache) Close() error {
	var firstErr error
	for _, s := range []*sql.Stmt{c.write, c.search, c.read} {
		if s != nil {
			if err := s.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}
