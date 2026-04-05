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

//go:build windows

package doctor

import (
	"golang.org/x/sys/windows"
)

// diskFreeBytes returns the number of free bytes available to the caller on
// the volume that contains path. Uses GetDiskFreeSpaceEx on Windows.
func diskFreeBytes(path string) (uint64, error) {
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var freeBytesAvailableToCaller, totalNumberOfBytes, totalNumberOfFreeBytes uint64
	if err := windows.GetDiskFreeSpaceEx(
		ptr,
		&freeBytesAvailableToCaller,
		&totalNumberOfBytes,
		&totalNumberOfFreeBytes,
	); err != nil {
		return 0, err
	}
	return freeBytesAvailableToCaller, nil
}
