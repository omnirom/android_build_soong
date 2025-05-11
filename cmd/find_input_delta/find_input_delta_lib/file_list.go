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
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"text/template"

	fid_exp "android/soong/cmd/find_input_delta/find_input_delta_proto"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

var DefaultTemplate = `
	{{- define "contents"}}
		{{- range .Deletions}}-{{.}} {{end}}
		{{- range .Additions}}+{{.}} {{end}}
		{{- range .Changes}}+{{- .Name}} {{end}}
		{{- range .Changes}}
		  {{- if or .Additions .Deletions .Changes}}--file {{.Name}} {{template "contents" .}}--endfile {{end}}
		{{- end}}
	{{- end}}
	{{- template "contents" .}}`

type FileList struct {
	// The name of the parent for the list of file differences.
	// For the outermost FileList, this is the name of the ninja target.
	// Under `Changes`, it is the name of the changed file.
	Name string

	// The added files
	Additions []string

	// The deleted files
	Deletions []string

	// The modified files
	Changes []FileList

	// Map of file_extension:counts
	ExtCountMap map[string]*FileCounts

	// Total number of added/changed/deleted files.
	TotalDelta uint32
}

// The maximum number of files that will be recorded by name.
var MaxFilesRecorded uint32 = 50

type FileCounts struct {
	Additions uint32
	Deletions uint32
	Changes   uint32
}

func FileListFactory(name string) *FileList {
	return &FileList{
		Name:        name,
		ExtCountMap: make(map[string]*FileCounts),
	}
}

func (fl *FileList) addFile(name string) {
	fl.Additions = append(fl.Additions, name)
	fl.TotalDelta += 1
	ext := filepath.Ext(name)
	if _, ok := fl.ExtCountMap[ext]; !ok {
		fl.ExtCountMap[ext] = &FileCounts{}
	}
	fl.ExtCountMap[ext].Additions += 1
}

func (fl *FileList) deleteFile(name string) {
	fl.Deletions = append(fl.Deletions, name)
	fl.TotalDelta += 1
	ext := filepath.Ext(name)
	if _, ok := fl.ExtCountMap[ext]; !ok {
		fl.ExtCountMap[ext] = &FileCounts{}
	}
	fl.ExtCountMap[ext].Deletions += 1
}

func (fl *FileList) changeFile(name string, ch *FileList) {
	fl.Changes = append(fl.Changes, *ch)
	fl.TotalDelta += 1
	ext := filepath.Ext(name)
	if _, ok := fl.ExtCountMap[ext]; !ok {
		fl.ExtCountMap[ext] = &FileCounts{}
	}
	fl.ExtCountMap[ext].Changes += 1
}

func (fl FileList) ToProto() (*fid_exp.FileList, error) {
	var count uint32
	return fl.toProto(&count)
}

func (fl FileList) toProto(count *uint32) (*fid_exp.FileList, error) {
	ret := &fid_exp.FileList{
		Name: proto.String(fl.Name),
	}
	for _, a := range fl.Additions {
		if *count >= MaxFilesRecorded {
			break
		}
		ret.Additions = append(ret.Additions, a)
		*count += 1
	}
	for _, ch := range fl.Changes {
		if *count >= MaxFilesRecorded {
			break
		} else {
			// Pre-increment to limit what the call adds.
			*count += 1
			change, err := ch.toProto(count)
			if err != nil {
				return nil, err
			}
			ret.Changes = append(ret.Changes, change)
		}
	}
	for _, d := range fl.Deletions {
		if *count >= MaxFilesRecorded {
			break
		}
		ret.Deletions = append(ret.Deletions, d)
	}
	ret.TotalDelta = proto.Uint32(*count)
	exts := []string{}
	for k := range fl.ExtCountMap {
		exts = append(exts, k)
	}
	slices.Sort(exts)
	for _, k := range exts {
		v := fl.ExtCountMap[k]
		ret.Counts = append(ret.Counts, &fid_exp.FileCount{
			Extension:     proto.String(k),
			Additions:     proto.Uint32(v.Additions),
			Deletions:     proto.Uint32(v.Deletions),
			Modifications: proto.Uint32(v.Changes),
		})
	}
	return ret, nil
}

func (fl FileList) SendMetrics(path string) error {
	if path == "" {
		return fmt.Errorf("No path given")
	}
	message, err := fl.ToProto()
	if err != nil {
		return err
	}

	// Marshal the message wrapped in SoongCombinedMetrics.
	data := protowire.AppendVarint(
		[]byte{},
		protowire.EncodeTag(
			protowire.Number(fid_exp.FieldNumbers_FIELD_NUMBERS_FILE_LIST),
			protowire.BytesType))
	size := uint64(proto.Size(message))
	data = protowire.AppendVarint(data, size)
	data, err = proto.MarshalOptions{UseCachedSize: true}.MarshalAppend(data, message)
	if err != nil {
		return err
	}

	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if err := out.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to close %s: %v\n", path, err)
		}
	}()
	_, err = out.Write(data)
	return err
}

func (fl FileList) Format(wr io.Writer, format string) error {
	tmpl, err := template.New("filelist").Parse(format)
	if err != nil {
		return err
	}
	return tmpl.Execute(wr, fl)
}
