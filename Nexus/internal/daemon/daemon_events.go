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

package daemon

import (
	"fmt"

	"github.com/bubblefish-tech/nexus/internal/eventbus"
	"github.com/bubblefish-tech/nexus/internal/events"
)

// runLiteBusBridge reads from the LiteBus and forwards every event to the
// eventBus so SSE clients at GET /api/events/stream receive all events.
// Runs as a goroutine; exits when liteBus.Close() closes the stream channel.
func (d *Daemon) runLiteBusBridge() {
	for e := range d.liteBus.Stream() {
		d.eventBus.Publish(liteToEventBus(e))
	}
}

// liteToEventBus converts a LiteEvent to an eventbus.Event.
// "source" and "agent_id" string fields in Data are promoted to their
// corresponding eventbus.Event fields; all other fields go into Meta.
func liteToEventBus(e events.LiteEvent) eventbus.Event {
	out := eventbus.Event{
		Type: eventbus.EventType(e.Type),
	}
	if len(e.Data) == 0 {
		return out
	}
	meta := make(map[string]string, len(e.Data))
	for k, v := range e.Data {
		s := fmt.Sprintf("%v", v)
		switch k {
		case "source":
			out.Source = s
		case "agent_id":
			out.AgentID = s
		default:
			meta[k] = s
		}
	}
	if len(meta) > 0 {
		out.Meta = meta
	}
	return out
}
