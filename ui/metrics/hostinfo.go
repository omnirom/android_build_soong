// Copyright 2024 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package metrics

// The hostinfo* files contain code to extract host information from
// /proc/cpuinfo and /proc/meminfo relevant to machine performance

import (
	"strconv"
	"strings"
)

// CpuInfo holds information regarding the host's CPU cores.
type CpuInfo struct {
	// The vendor id
	VendorId string

	// The model name
	ModelName string

	// The number of CPU cores
	CpuCores int32

	// The CPU flags
	Flags string
}

// MemInfo holds information regarding the host's memory.
// The memory size in each of the field is in bytes.
type MemInfo struct {
	// The total memory.
	MemTotal uint64

	// The amount of free memory.
	MemFree uint64

	// The amount of available memory.
	MemAvailable uint64
}

// fillCpuInfo takes the key and value, converts the value
// to the proper size unit and is stores it in CpuInfo.
func (c *CpuInfo) fillInfo(key, value string) {
	switch key {
	case "vendor_id":
		c.VendorId = value
	case "model name":
		c.ModelName = value
	case "cpu cores":
		v, err := strconv.ParseInt(value, 10, 32)
		if err == nil {
			c.CpuCores = int32(v)
		}
	case "flags":
		c.Flags = value
	default:
		// Ignore unknown keys
	}
}

// fillCpuInfo takes the key and value, converts the value
// to the proper size unit and is stores it in CpuInfo.
func (m *MemInfo) fillInfo(key, value string) {
	v := strToUint64(value)
	switch key {
	case "MemTotal":
		m.MemTotal = v
	case "MemFree":
		m.MemFree = v
	case "MemAvailable":
		m.MemAvailable = v
	default:
		// Ignore unknown keys
	}
}

// strToUint64 takes the string and converts to unsigned 64-bit integer.
// If the string contains a memory unit such as kB and is converted to
// bytes.
func strToUint64(v string) uint64 {
	// v could be "1024 kB" so scan for the empty space and
	// split between the value and the unit.
	var separatorIndex int
	if separatorIndex = strings.IndexAny(v, " "); separatorIndex < 0 {
		separatorIndex = len(v)
	}
	value, err := strconv.ParseUint(v[:separatorIndex], 10, 64)
	if err != nil {
		return 0
	}

	var scale uint64 = 1
	switch strings.TrimSpace(v[separatorIndex:]) {
	case "kB", "KB":
		scale = 1024
	case "mB", "MB":
		scale = 1024 * 1024
	}
	return value * scale
}
