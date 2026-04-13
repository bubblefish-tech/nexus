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

package ingest

import "errors"

var (
	// ErrNotImplemented is returned by scaffolded watcher stubs whose parser
	// is not yet shipped. The watcher's Detect method works (so status reports
	// can show "detected, not yet supported"), but Parse returns this error.
	ErrNotImplemented = errors.New("ingest: parser not implemented in this version")

	// ErrDisabled is returned when Ingest is disabled via kill switch.
	ErrDisabled = errors.New("ingest: disabled via kill switch")

	// ErrFileTooLarge is returned when a watched file exceeds MaxFileSize.
	ErrFileTooLarge = errors.New("ingest: file exceeds maximum size")

	// ErrSymlinkRejected is returned when a path resolves to a symlink.
	ErrSymlinkRejected = errors.New("ingest: symlinks are not followed")

	// ErrPathNotAllowed is returned when a path is outside the allowlist.
	ErrPathNotAllowed = errors.New("ingest: path not in allowlist")
)
