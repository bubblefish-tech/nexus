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

// Package dashboard embeds the v4 dashboard HTML and static assets so they
// are baked into the binary at build time.
package dashboard

import "embed"

// HTML is the v4 dashboard HTML content.
//
//go:embed index.html
var HTML string

// LogoPNG is the BubbleFish logo used in the sidebar and witness HUD.
//
//go:embed assets/logo_metal.png
var LogoPNG []byte

// Assets embeds the entire assets directory for future static files.
//
//go:embed assets/*
var Assets embed.FS
