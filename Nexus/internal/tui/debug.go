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

package tui

import (
	"fmt"
	"log"
	"os"
	"time"
)

var debugLog *log.Logger

func init() {
	if os.Getenv("DEBUG") != "" {
		f, err := os.OpenFile("tui_debug.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err == nil {
			debugLog = log.New(f, "", 0)
			debugLog.Printf("=== TUI debug log started %s ===\n", time.Now().Format(time.RFC3339))
		}
	}
}

func dbg(format string, args ...interface{}) {
	if debugLog == nil {
		return
	}
	ts := time.Now().Format("15:04:05.000")
	debugLog.Printf("%s  %s", ts, fmt.Sprintf(format, args...))
}
