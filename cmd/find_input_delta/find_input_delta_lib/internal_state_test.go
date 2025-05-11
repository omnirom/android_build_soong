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

package find_input_delta_lib

import (
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"
	"time"

	// For Assert*.
	"android/soong/android"

	fid_proto "android/soong/cmd/find_input_delta/find_input_delta_proto_internal"
	"google.golang.org/protobuf/proto"
)

// Various state files

func marshalProto(t *testing.T, message proto.Message) []byte {
	data, err := proto.Marshal(message)
	if err != nil {
		t.Errorf("%v", err)
	}
	return data
}

func protoFile(name string, mtime_nsec int64, hash string, contents []*fid_proto.PartialCompileInput) (pci *fid_proto.PartialCompileInput) {
	pci = &fid_proto.PartialCompileInput{
		Name: proto.String(name),
	}
	if mtime_nsec != 0 {
		pci.MtimeNsec = proto.Int64(mtime_nsec)
	}
	if len(hash) > 0 {
		pci.Hash = proto.String(hash)
	}
	if contents != nil {
		pci.Contents = contents
	}
	return
}

func TestLoadState(t *testing.T) {
	testCases := []struct {
		Name     string
		Filename string
		Mapfs    fs.ReadFileFS
		Expected *fid_proto.PartialCompileInputs
		Err      error
	}{
		{
			Name:     "missing file",
			Filename: "missing",
			Mapfs:    fstest.MapFS{},
			Expected: &fid_proto.PartialCompileInputs{},
			Err:      nil,
		},
		{
			Name:     "bad file",
			Filename: ".",
			Mapfs:    OsFs,
			Expected: &fid_proto.PartialCompileInputs{},
			Err:      errors.New("read failed"),
		},
		{
			Name:     "file with mtime",
			Filename: "state.old",
			Mapfs: fstest.MapFS{
				"state.old": &fstest.MapFile{
					Data: marshalProto(t, &fid_proto.PartialCompileInputs{
						InputFiles: []*fid_proto.PartialCompileInput{
							protoFile("input1", 100, "", nil),
						},
					}),
				},
			},
			Expected: &fid_proto.PartialCompileInputs{
				InputFiles: []*fid_proto.PartialCompileInput{
					protoFile("input1", 100, "", nil),
				},
			},
			Err: nil,
		},
		{
			Name:     "file with mtime and hash",
			Filename: "state.old",
			Mapfs: fstest.MapFS{
				"state.old": &fstest.MapFile{
					Data: marshalProto(t, &fid_proto.PartialCompileInputs{
						InputFiles: []*fid_proto.PartialCompileInput{
							protoFile("input1", 100, "crc:crc_value", nil),
						},
					}),
				},
			},
			Expected: &fid_proto.PartialCompileInputs{
				InputFiles: []*fid_proto.PartialCompileInput{
					protoFile("input1", 100, "crc:crc_value", nil),
				},
			},
			Err: nil,
		},
	}
	for _, tc := range testCases {
		actual, err := LoadState(tc.Filename, tc.Mapfs)
		if tc.Err == nil {
			android.AssertSame(t, tc.Name, tc.Err, err)
		} else if err == nil {
			t.Errorf("%s: expected error, did not get one", tc.Name)
		}
		if !proto.Equal(tc.Expected, actual) {
			t.Errorf("%s: expected %v, actual %v", tc.Name, tc.Expected, actual)
		}
	}
}

func TestCreateState(t *testing.T) {
	testCases := []struct {
		Name     string
		Inputs   []string
		Inspect  bool
		Mapfs    StatReadFileFS
		Expected *fid_proto.PartialCompileInputs
		Err      error
	}{
		{
			Name:     "no inputs",
			Inputs:   []string{},
			Mapfs:    fstest.MapFS{},
			Expected: &fid_proto.PartialCompileInputs{},
			Err:      nil,
		},
		{
			Name:   "files found",
			Inputs: []string{"baz", "foo", "bar"},
			Mapfs: fstest.MapFS{
				"foo": &fstest.MapFile{ModTime: time.Unix(0, 100).UTC()},
				"baz": &fstest.MapFile{ModTime: time.Unix(0, 300).UTC()},
				"bar": &fstest.MapFile{ModTime: time.Unix(0, 200).UTC()},
			},
			Expected: &fid_proto.PartialCompileInputs{
				InputFiles: []*fid_proto.PartialCompileInput{
					// Files are always sorted.
					protoFile("bar", 200, "", nil),
					protoFile("baz", 300, "", nil),
					protoFile("foo", 100, "", nil),
				},
			},
			Err: nil,
		},
	}
	for _, tc := range testCases {
		actual, err := CreateState(tc.Inputs, tc.Inspect, tc.Mapfs)
		if tc.Err == nil {
			android.AssertSame(t, tc.Name, tc.Err, err)
		} else if err == nil {
			t.Errorf("%s: expected error, did not get one", tc.Name)
		}
		if !proto.Equal(tc.Expected, actual) {
			t.Errorf("%s: expected %v, actual %v", tc.Name, tc.Expected, actual)
		}
	}
}

func TestCompareInternalState(t *testing.T) {
	testCases := []struct {
		Name     string
		Target   string
		Prior    *fid_proto.PartialCompileInputs
		New      *fid_proto.PartialCompileInputs
		Expected *FileList
	}{
		{
			Name:   "prior is empty",
			Target: "foo",
			Prior:  &fid_proto.PartialCompileInputs{},
			New: &fid_proto.PartialCompileInputs{
				InputFiles: []*fid_proto.PartialCompileInput{
					protoFile("file1", 100, "", nil),
				},
			},
			Expected: &FileList{
				Name:      "foo",
				Additions: []string{"file1"},
			},
		},
		{
			Name:   "one each add modify delete",
			Target: "foo",
			Prior: &fid_proto.PartialCompileInputs{
				InputFiles: []*fid_proto.PartialCompileInput{
					protoFile("file0", 100, "", nil),
					protoFile("file1", 100, "", nil),
					protoFile("file2", 200, "", nil),
				},
			},
			New: &fid_proto.PartialCompileInputs{
				InputFiles: []*fid_proto.PartialCompileInput{
					protoFile("file0", 100, "", nil),
					protoFile("file1", 200, "", nil),
					protoFile("file3", 300, "", nil),
				},
			},
			Expected: &FileList{
				Name:      "foo",
				Additions: []string{"file3"},
				Changes:   []FileList{FileList{Name: "file1"}},
				Deletions: []string{"file2"},
			},
		},
		{
			Name:   "interior one each add modify delete",
			Target: "bar",
			Prior: &fid_proto.PartialCompileInputs{
				InputFiles: []*fid_proto.PartialCompileInput{
					protoFile("file1", 405, "", []*fid_proto.PartialCompileInput{
						protoFile("innerC", 400, "crc32:11111111", nil),
						protoFile("innerD", 400, "crc32:44444444", nil),
					}),
				},
			},
			New: &fid_proto.PartialCompileInputs{
				InputFiles: []*fid_proto.PartialCompileInput{
					protoFile("file1", 505, "", []*fid_proto.PartialCompileInput{
						protoFile("innerA", 400, "crc32:55555555", nil),
						protoFile("innerC", 500, "crc32:66666666", nil),
					}),
				},
			},
			Expected: &FileList{
				Name: "bar",
				Changes: []FileList{FileList{
					Name:      "file1",
					Additions: []string{"innerA"},
					Changes:   []FileList{FileList{Name: "innerC"}},
					Deletions: []string{"innerD"},
				}},
			},
		},
	}
	for _, tc := range testCases {
		actual := CompareInternalState(tc.Prior, tc.New, tc.Target)
		if !tc.Expected.Equal(actual) {
			t.Errorf("%s: expected %v, actual %v", tc.Name, tc.Expected, actual)
		}
	}
}

func TestCompareInspectExtsZipRegexp(t *testing.T) {
	testCases := []struct {
		Name     string
		Expected bool
	}{
		{Name: ".jar", Expected: true},
		{Name: ".jar5", Expected: true},
		{Name: ".apex", Expected: true},
		{Name: ".apex9", Expected: true},
		{Name: ".apexx", Expected: false},
		{Name: ".apk", Expected: true},
		{Name: ".apk3", Expected: true},
		{Name: ".go", Expected: false},
	}
	for _, tc := range testCases {
		actual := InspectExtsZipRegexp.Match([]byte(tc.Name))
		if tc.Expected != actual {
			t.Errorf("%s: expected %v, actual %v", tc.Name, tc.Expected, actual)
		}
	}
}
