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

// Package a2a implements the Nexus A2A Protocol — a clean-room Go
// implementation of the A2A v1.0 wire format plus the
// sh.nexus.nexus.governance/v1 extension.
//
// NA2A is wire-compatible with the public A2A v1.0 specification. No source
// code from github.com/a2aproject/a2a-go, @a2a-js/sdk, or any other A2A SDK
// has been read or used. All design decisions, struct layouts, state machines,
// and error handling are original.
//
// The governance extension is how Nexus expresses capability-scoped
// permission grants, two-step elevated consent, and forward-secure audit
// records tied to the Phase-4 cryptographic provenance chain.
//
// This package provides the foundational types, constants, and utilities.
// Sub-packages provide the JSON-RPC dispatcher (jsonrpc/), task storage
// (store/), governance engine (governance/), physical transports
// (transport/), agent registry (registry/), and the NA2A client (client/).
package a2a
