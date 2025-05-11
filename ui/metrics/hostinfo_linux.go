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
	"io/ioutil"
	"strings"

	"android/soong/finder/fs"
)

type fillable interface {
	fillInfo(key, value string)
}

func NewCpuInfo(fileSystem fs.FileSystem) (*CpuInfo, error) {
	c := &CpuInfo{}
	if err := parseFile(c, "/proc/cpuinfo", true, fileSystem); err != nil {
		return &CpuInfo{}, err
	}
	return c, nil
}

func NewMemInfo(fileSystem fs.FileSystem) (*MemInfo, error) {
	m := &MemInfo{}
	if err := parseFile(m, "/proc/meminfo", false, fileSystem); err != nil {
		return &MemInfo{}, err
	}
	return m, nil
}

func parseFile(obj fillable, fileName string, endOnBlank bool, fileSystem fs.FileSystem) error {
	fd, err := fileSystem.Open(fileName)
	if err != nil {
		return err
	}
	defer fd.Close()

	data, err := ioutil.ReadAll(fd)
	if err != nil {
		return err
	}

	for _, l := range strings.Split(string(data), "\n") {
		if !strings.Contains(l, ":") {
			// Terminate after the first blank line.
			if endOnBlank && strings.TrimSpace(l) == "" {
				break
			}
			// If the line is not of the form "key: values", just skip it.
			continue
		}

		kv := strings.SplitN(l, ":", 2)
		obj.fillInfo(strings.TrimSpace(kv[0]), strings.TrimSpace(kv[1]))
	}
	return nil
}
