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
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"strings"
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

// Write a SoongExecutionMetrics FileList proto to `dir`.
//
// Path
// Prune any paths that
// begin with `pruneDir` (usually ${OUT_DIR}).  The file is only written if any
// non-pruned changes are present.
func (fl *FileList) WriteMetrics(dir, pruneDir string) (err error) {
	if dir == "" {
		return fmt.Errorf("No directory given")
	}
	var needed bool

	if !strings.HasSuffix(pruneDir, "/") {
		pruneDir += "/"
	}

	// Hash the dir and `fl.Name` to simplify scanning the metrics
	// aggregation directory.
	h := fnv.New128()
	h.Write([]byte(dir + " " + fl.Name + ".FileList"))
	path := fmt.Sprintf("%x.pb", h.Sum([]byte{}))
	path = filepath.Join(dir, path[0:2], path[2:])

	var msg = &fid_exp.FileList{Name: proto.String(fl.Name)}
	for _, a := range fl.Additions {
		if strings.HasPrefix(a, pruneDir) {
			continue
		}
		msg.Additions = append(msg.Additions, a)
		needed = true
	}
	for _, ch := range fl.Changes {
		if strings.HasPrefix(ch.Name, pruneDir) {
			continue
		}
		msg.Changes = append(msg.Changes, ch.Name)
		needed = true
	}
	for _, d := range fl.Deletions {
		if strings.HasPrefix(d, pruneDir) {
			continue
		}
		msg.Deletions = append(msg.Deletions, d)
		needed = true
	}
	if !needed {
		return nil
	}
	data := protowire.AppendVarint(
		[]byte{},
		protowire.EncodeTag(
			protowire.Number(fid_exp.FieldNumbers_FIELD_NUMBERS_FILE_LIST),
			protowire.BytesType))
	size := uint64(proto.Size(msg))
	data = protowire.AppendVarint(data, size)
	data, err = proto.MarshalOptions{UseCachedSize: true}.MarshalAppend(data, msg)
	if err != nil {
		return err
	}

	err = os.MkdirAll(filepath.Dir(path), 0777)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

func (fl *FileList) Format(wr io.Writer, format string) error {
	tmpl, err := template.New("filelist").Parse(format)
	if err != nil {
		return err
	}
	return tmpl.Execute(wr, fl)
}
