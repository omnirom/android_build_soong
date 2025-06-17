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

import (
	"reflect"
	"testing"

	fid_proto "android/soong/cmd/find_input_delta/find_input_delta_proto"
)

func TestUpdateChangeInfo(t *testing.T) {
	testCases := []struct {
		Name     string
		Message  *fid_proto.FileList
		FileList *fileList
		Expected *fileList
	}{
		{
			Name: "various",
			Message: &fid_proto.FileList{
				Additions: []string{"file1", "file2", "file3", "file2"},
				Deletions: []string{"file5.go", "file6"},
			},
			FileList: &fileList{seenFiles: make(map[string]bool)},
			Expected: &fileList{
				seenFiles:    map[string]bool{"file1": true, "file2": true, "file3": true, "file5.go": true, "file6": true},
				totalChanges: 5,
				changes: fileChanges{
					additions: changeInfo{
						total:       3,
						list:        []string{"file1", "file2", "file3"},
						byExtension: map[string]uint32{"": 3},
					},
					deletions: changeInfo{
						total:       2,
						list:        []string{"file5.go", "file6"},
						byExtension: map[string]uint32{"": 1, ".go": 1},
					},
				},
			},
		},
	}
	for _, tc := range testCases {
		tc.FileList.aggregateFileList(tc.Message)
		if !reflect.DeepEqual(tc.FileList, tc.Expected) {
			t.Errorf("Expected: %v, Actual: %v", tc.Expected, tc.FileList)
		}
	}
}
