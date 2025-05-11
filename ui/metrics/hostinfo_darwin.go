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
	"android/soong/finder/fs"
)

func NewCpuInfo(fileSystem fs.FileSystem) (*CpuInfo, error) {
	return &CpuInfo{}, nil
}

func NewMemInfo(fileSystem fs.FileSystem) (*MemInfo, error) {
	return &MemInfo{}, nil
}
