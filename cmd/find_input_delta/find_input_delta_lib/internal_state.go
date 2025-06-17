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
	"fmt"
	"io/fs"
	"regexp"
	"slices"

	fid_proto "android/soong/cmd/find_input_delta/find_input_delta_proto_internal"
	"android/soong/third_party/zip"
	"github.com/google/blueprint/pathtools"
	"google.golang.org/protobuf/proto"
)

// Load the internal state from a file.
// If the file does not exist, an empty state is returned.
func LoadState(filename string, fsys fs.ReadFileFS) (*fid_proto.PartialCompileInputs, error) {
	var message = &fid_proto.PartialCompileInputs{}
	data, err := fsys.ReadFile(filename)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return message, err
	}
	proto.Unmarshal(data, message)
	return message, nil
}

type StatReadFileFS interface {
	fs.StatFS
	fs.ReadFileFS
}

// Create the internal state by examining the inputs.
func CreateState(inputs []string, inspect_contents bool, fsys StatReadFileFS) (*fid_proto.PartialCompileInputs, error) {
	ret := &fid_proto.PartialCompileInputs{}
	slices.Sort(inputs)
	for _, input := range inputs {
		stat, err := fs.Stat(fsys, input)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return ret, err
		}
		pci := &fid_proto.PartialCompileInput{
			Name:      proto.String(input),
			MtimeNsec: proto.Int64(stat.ModTime().UnixNano()),
			// If we ever have an easy hash, assign it here.
		}
		if inspect_contents {
			// NOTE: When we find it useful, we can parallelize the file inspection for speed.
			contents, err := InspectFileContents(input)
			if err != nil {
				return ret, err
			}
			if contents != nil {
				pci.Contents = contents
			}
		}
		ret.InputFiles = append(ret.InputFiles, pci)
	}
	return ret, nil
}

// We ignore any suffix digit caused by sharding.
var InspectExtsZipRegexp = regexp.MustCompile("\\.(jar|apex|apk)[0-9]*$")

// Inspect the file and extract the state of the elements in the archive.
// If this is not an archive of some sort, nil is returned.
func InspectFileContents(name string) ([]*fid_proto.PartialCompileInput, error) {
	if InspectExtsZipRegexp.Match([]byte(name)) {
		return inspectZipFileContents(name)
	}
	return nil, nil
}

func inspectZipFileContents(name string) ([]*fid_proto.PartialCompileInput, error) {
	rc, err := zip.OpenReader(name)
	if err != nil {
		return nil, err
	}
	ret := []*fid_proto.PartialCompileInput{}
	for _, v := range rc.File {
		// Only include timestamp when there is no CRC.
		timeNsec := proto.Int64(v.ModTime().UnixNano())
		if v.CRC32 != 0 {
			timeNsec = nil
		}
		pci := &fid_proto.PartialCompileInput{
			Name:      proto.String(v.Name),
			MtimeNsec: timeNsec,
			Hash:      proto.String(fmt.Sprintf("%08x", v.CRC32)),
		}
		ret = append(ret, pci)
		// We do not support nested inspection.
	}
	return ret, nil
}

func WriteState(s *fid_proto.PartialCompileInputs, path string) error {
	data, err := proto.Marshal(s)
	if err != nil {
		return err
	}
	return pathtools.WriteFileIfChanged(path, data, 0644)
}

func CompareInternalState(prior, other *fid_proto.PartialCompileInputs, target string) *FileList {
	return CompareInputFiles(prior.GetInputFiles(), other.GetInputFiles(), target)
}

func CompareInputFiles(prior, other []*fid_proto.PartialCompileInput, name string) *FileList {
	fl := FileListFactory(name)
	PriorMap := make(map[string]*fid_proto.PartialCompileInput, len(prior))
	// We know that the lists are properly sorted, so we can simply compare them.
	for _, v := range prior {
		PriorMap[v.GetName()] = v
	}
	otherMap := make(map[string]*fid_proto.PartialCompileInput, len(other))
	for _, v := range other {
		name = v.GetName()
		otherMap[name] = v
		if _, ok := PriorMap[name]; !ok {
			// Added file
			fl.addFile(name)
		} else if !proto.Equal(PriorMap[name], v) {
			// Changed file
			fl.changeFile(name, CompareInputFiles(PriorMap[name].GetContents(), v.GetContents(), name))
		}
	}
	for _, v := range prior {
		name := v.GetName()
		if _, ok := otherMap[name]; !ok {
			// Deleted file
			fl.deleteFile(name)
		}
	}
	return fl
}
