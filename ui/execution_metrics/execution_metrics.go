// Copyright 2024 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package execution_metrics represents the metrics system for Android Platform Build Systems.
package execution_metrics

// This is the main heart of the metrics system for Android Platform Build Systems.
// The starting of the soong_ui (cmd/soong_ui/main.go), the metrics system is
// initialized by the invocation of New and is then stored in the context
// (ui/build/context.go) to be used throughout the system. During the build
// initialization phase, several functions in this file are invoked to store
// information such as the environment, build configuration and build metadata.
// There are several scoped code that has Begin() and defer End() functions
// that captures the metrics and is them added as a perfInfo into the set
// of the collected metrics. Finally, when soong_ui has finished the build,
// the defer Dump function is invoked to store the collected metrics to the
// raw protobuf file in the $OUT directory and this raw protobuf file will be
// uploaded to the destination. See ui/build/upload.go for more details. The
// filename of the raw protobuf file and the list of files to be uploaded is
// defined in cmd/soong_ui/main.go. See ui/metrics/event.go for the explanation
// of what an event is and how the metrics system is a stack based system.

import (
	"context"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"android/soong/ui/logger"

	fid_proto "android/soong/cmd/find_input_delta/find_input_delta_proto"
	"android/soong/ui/metrics"
	soong_execution_proto "android/soong/ui/metrics/execution_metrics_proto"
	soong_metrics_proto "android/soong/ui/metrics/metrics_proto"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

type ExecutionMetrics struct {
	MetricsAggregationDir string
	ctx                   context.Context
	logger                logger.Logger
	waitGroup             sync.WaitGroup
	fileList              *fileList
}

type fileList struct {
	totalChanges uint32
	changes      fileChanges
	seenFiles    map[string]bool
}

type fileChanges struct {
	additions     changeInfo
	deletions     changeInfo
	modifications changeInfo
}

type fileChangeCounts struct {
	additions     uint32
	deletions     uint32
	modifications uint32
}

type changeInfo struct {
	total       uint32
	list        []string
	byExtension map[string]uint32
}

var MAXIMUM_FILES uint32 = 50

// Setup the handler for SoongExecutionMetrics.
func NewExecutionMetrics(log logger.Logger) *ExecutionMetrics {
	return &ExecutionMetrics{
		logger:   log,
		fileList: &fileList{seenFiles: make(map[string]bool)},
	}
}

// Save the path for ExecutionMetrics communications.
func (c *ExecutionMetrics) SetDir(path string) {
	c.MetricsAggregationDir = path
}

// Start collecting SoongExecutionMetrics.
func (c *ExecutionMetrics) Start() {
	if c.MetricsAggregationDir == "" {
		return
	}

	tmpDir := c.MetricsAggregationDir + ".rm"
	if _, err := fs.Stat(os.DirFS("."), c.MetricsAggregationDir); err == nil {
		if err = os.RemoveAll(tmpDir); err != nil {
			c.logger.Fatalf("Failed to remove %s: %v", tmpDir, err)
		}
		if err = os.Rename(c.MetricsAggregationDir, tmpDir); err != nil {
			c.logger.Fatalf("Failed to rename %s to %s: %v", c.MetricsAggregationDir, tmpDir)
		}
	}
	if err := os.MkdirAll(c.MetricsAggregationDir, 0777); err != nil {
		c.logger.Fatalf("Failed to create %s: %v", c.MetricsAggregationDir)
	}

	c.waitGroup.Add(1)
	go func(d string) {
		defer c.waitGroup.Done()
		os.RemoveAll(d)
	}(tmpDir)

	c.logger.Verbosef("ExecutionMetrics running\n")
}

type hasTrace interface {
	BeginTrace(name, desc string)
	EndTrace()
}

// Aggregate any execution metrics.
func (c *ExecutionMetrics) Finish(ctx hasTrace) {
	ctx.BeginTrace(metrics.RunSoong, "execution_metrics.Finish")
	defer ctx.EndTrace()
	if c.MetricsAggregationDir == "" {
		return
	}
	c.waitGroup.Wait()

	// Find and process all of the metrics files.
	aggFs := os.DirFS(c.MetricsAggregationDir)
	fs.WalkDir(aggFs, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			c.logger.Fatalf("ExecutionMetrics.Finish: Error walking %s: %v", c.MetricsAggregationDir, err)
		}
		if d.IsDir() {
			return nil
		}
		path = filepath.Join(c.MetricsAggregationDir, path)
		r, err := os.ReadFile(path)
		if err != nil {
			c.logger.Fatalf("ExecutionMetrics.Finish: Failed to read %s: %v", path, err)
		}
		msg := &soong_execution_proto.SoongExecutionMetrics{}
		err = proto.Unmarshal(r, msg)
		if err != nil {
			c.logger.Verbosef("ExecutionMetrics.Finish: Error unmarshalling SoongExecutionMetrics message: %v\n", err)
			return nil
		}
		switch {
		case msg.GetFileList() != nil:
			if err := c.fileList.aggregateFileList(msg.GetFileList()); err != nil {
				c.logger.Verbosef("ExecutionMetrics.Finish: Error processing SoongExecutionMetrics message: %v\n", err)
			}
		// Status update for all others.
		default:
			tag, _ := protowire.ConsumeVarint(r)
			id, _ := protowire.DecodeTag(tag)
			c.logger.Verbosef("ExecutionMetrics.Finish: Unexpected SoongExecutionMetrics submessage id=%d\n", id)
		}
		return nil
	})
}

func (fl *fileList) aggregateFileList(msg *fid_proto.FileList) error {
	fl.updateChangeInfo(msg.GetAdditions(), &fl.changes.additions)
	fl.updateChangeInfo(msg.GetDeletions(), &fl.changes.deletions)
	fl.updateChangeInfo(msg.GetChanges(), &fl.changes.modifications)
	return nil
}

func (fl *fileList) updateChangeInfo(list []string, info *changeInfo) {
	for _, filename := range list {
		if fl.seenFiles[filename] {
			continue
		}
		fl.seenFiles[filename] = true
		if info.total < MAXIMUM_FILES {
			info.list = append(info.list, filename)
		}
		ext := filepath.Ext(filename)
		if info.byExtension == nil {
			info.byExtension = make(map[string]uint32)
		}
		info.byExtension[ext] += 1
		info.total += 1
		fl.totalChanges += 1
	}
}

func (c *ExecutionMetrics) Dump(path string, args []string) error {
	if c.MetricsAggregationDir == "" {
		return nil
	}
	msg := c.GetMetrics(args)

	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		if err = os.MkdirAll(filepath.Dir(path), 0775); err != nil {
			return err
		}
	}
	data, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (c *ExecutionMetrics) GetMetrics(args []string) *soong_metrics_proto.ExecutionMetrics {
	return &soong_metrics_proto.ExecutionMetrics{
		CommandArgs:  args,
		ChangedFiles: c.getChangedFiles(),
	}
}

func (c *ExecutionMetrics) getChangedFiles() *soong_metrics_proto.AggregatedFileList {
	fl := c.fileList
	if fl == nil {
		return nil
	}
	var count uint32
	fileCounts := make(map[string]*soong_metrics_proto.FileCount)
	ret := &soong_metrics_proto.AggregatedFileList{TotalDelta: proto.Uint32(c.fileList.totalChanges)}

	// MAXIMUM_FILES is the upper bound on total file names reported.
	if limit := min(MAXIMUM_FILES-min(MAXIMUM_FILES, count), fl.changes.additions.total); limit > 0 {
		ret.Additions = fl.changes.additions.list[:limit]
		count += limit
	}
	if limit := min(MAXIMUM_FILES-min(MAXIMUM_FILES, count), fl.changes.modifications.total); limit > 0 {
		ret.Changes = fl.changes.modifications.list[:limit]
		count += limit
	}
	if limit := min(MAXIMUM_FILES-min(MAXIMUM_FILES, count), fl.changes.deletions.total); limit > 0 {
		ret.Deletions = fl.changes.deletions.list[:limit]
		count += limit
	}

	addExt := func(key string) *soong_metrics_proto.FileCount {
		// Create the fileCounts map entry if needed, and return the address to the caller.
		if _, ok := fileCounts[key]; !ok {
			fileCounts[key] = &soong_metrics_proto.FileCount{Extension: proto.String(key)}
		}
		return fileCounts[key]
	}
	addCount := func(loc **uint32, count uint32) {
		if *loc == nil {
			*loc = proto.Uint32(0)
		}
		**loc += count
	}
	for k, v := range fl.changes.additions.byExtension {
		addCount(&addExt(k).Additions, v)
	}
	for k, v := range fl.changes.modifications.byExtension {
		addCount(&addExt(k).Modifications, v)
	}
	for k, v := range fl.changes.deletions.byExtension {
		addCount(&addExt(k).Deletions, v)
	}

	keys := slices.Sorted(maps.Keys(fileCounts))
	for _, k := range keys {
		ret.Counts = append(ret.Counts, fileCounts[k])
	}
	return ret
}
