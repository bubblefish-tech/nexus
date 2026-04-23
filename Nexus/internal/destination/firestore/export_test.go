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

package firestore

// ExportDocFromPayload exposes docFromPayload for white-box testing.
var ExportDocFromPayload = docFromPayload

// ExportPayloadFromDoc exposes payloadFromDoc for white-box testing.
var ExportPayloadFromDoc = payloadFromDoc

// ExportFloat32ToFloat64 exposes float32SliceToFloat64 for white-box testing.
var ExportFloat32ToFloat64 = float32SliceToFloat64

// ExportFloat64ToFloat32 exposes float64SliceToFloat32 for white-box testing.
var ExportFloat64ToFloat32 = float64SliceToFloat32
