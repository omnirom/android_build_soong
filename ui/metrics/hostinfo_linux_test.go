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

// This file contain code to extract host information on linux from
// /proc/cpuinfo and /proc/meminfo relevant to machine performance

import (
	"reflect"
	"testing"

	"android/soong/finder/fs"
)

func TestNewCpuInfo(t *testing.T) {
	fs := fs.NewMockFs(nil)

	if err := fs.MkDirs("/proc"); err != nil {
		t.Fatalf("failed to create /proc dir: %v", err)
	}
	cpuFileName := "/proc/cpuinfo"

	if err := fs.WriteFile(cpuFileName, cpuData, 0644); err != nil {
		t.Fatalf("failed to write file %s: %v", cpuFileName, err)
	}

	cpuInfo, err := NewCpuInfo(fs)
	if err != nil {
		t.Fatalf("got %v, want nil for error", err)
	}

	if !reflect.DeepEqual(cpuInfo, expectedCpuInfo) {
		t.Errorf("got %v, expecting %v for CpuInfo", cpuInfo, expectedCpuInfo)
	}

}

func TestNewMemInfo(t *testing.T) {
	fs := fs.NewMockFs(nil)

	if err := fs.MkDirs("/proc"); err != nil {
		t.Fatalf("failed to create /proc dir: %v", err)
	}
	memFileName := "/proc/meminfo"

	if err := fs.WriteFile(memFileName, memData, 0644); err != nil {
		t.Fatalf("failed to write file %s: %v", memFileName, err)
	}

	memInfo, err := NewMemInfo(fs)
	if err != nil {
		t.Fatalf("got %v, want nil for error", err)
	}

	if !reflect.DeepEqual(memInfo, expectedMemInfo) {
		t.Errorf("got %v, expecting %v for MemInfo", memInfo, expectedMemInfo)
	}

}

var cpuData = []byte(`processor	: 0
vendor_id	: %%VENDOR%%
cpu family	: 123
model		: 456
model name	: %%CPU MODEL NAME%%
stepping	: 0
cpu MHz		: 5555.555
cache size	: 512 KB
physical id	: 0
siblings	: 128
core id		: 0
cpu cores	: 64
apicid		: 0
initial apicid	: 0
fpu		: yes
fpu_exception	: yes
cpuid level	: 789
wp		: yes
flags		: %%cpu flags go here%%
bugs		: %%bugs go here%%

processor	: 1
vendor_id	: %%BADVENDOR%%
cpu family	: 234
model		: 567
model name	: %%BAD MODEL NAME%%
flags		: %%BAD cpu flags go here%%
`)

var expectedCpuInfo = &CpuInfo{
	VendorId:  "%%VENDOR%%",
	ModelName: "%%CPU MODEL NAME%%",
	CpuCores:  64,
	Flags:     "%%cpu flags go here%%",
}

var memData = []byte(`MemTotal:       1000 mB
MemFree:        10240000
MemAvailable:   3000 kB
Buffers:         7177844 kB
`)

var expectedMemInfo = &MemInfo{
	MemTotal:     1048576000,
	MemFree:      10240000,
	MemAvailable: 3072000,
}
