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

package release_config_lib

import (
	"fmt"
	"path/filepath"
	"strings"

	rc_proto "android/soong/cmd/release_config/release_config_proto"
)

type FlagValue struct {
	// The path providing this value.
	path string

	// Protobuf
	proto rc_proto.FlagValue
}

func FlagValueFactory(protoPath string) (fv *FlagValue, err error) {
	fv = &FlagValue{path: protoPath}
	if protoPath == "" {
		return fv, nil
	}
	LoadMessage(protoPath, &fv.proto)

	if fv.proto.Name == nil {
		return nil, fmt.Errorf("%s does not set name", protoPath)
	}
	name := *fv.proto.Name
	switch {
	case name == "RELEASE_ACONFIG_VALUE_SETS":
		return nil, fmt.Errorf("%s: %s is a reserved build flag", protoPath, name)
	case fmt.Sprintf("%s.textproto", name) != filepath.Base(protoPath):
		return nil, fmt.Errorf("%s incorrectly sets value for flag %s", protoPath, name)
	}
	return fv, nil
}

func UnmarshalValue(str string) *rc_proto.Value {
	ret := &rc_proto.Value{}
	switch v := strings.ToLower(str); v {
	case "true":
		ret = &rc_proto.Value{Val: &rc_proto.Value_BoolValue{true}}
	case "false":
		ret = &rc_proto.Value{Val: &rc_proto.Value_BoolValue{false}}
	case "##obsolete":
		ret = &rc_proto.Value{Val: &rc_proto.Value_Obsolete{true}}
	default:
		ret = &rc_proto.Value{Val: &rc_proto.Value_StringValue{str}}
	}
	return ret
}

func MarshalValue(value *rc_proto.Value) string {
	if value == nil {
		return ""
	}
	switch val := value.Val.(type) {
	case *rc_proto.Value_UnspecifiedValue:
		// Value was never set.
		return ""
	case *rc_proto.Value_StringValue:
		return val.StringValue
	case *rc_proto.Value_BoolValue:
		if val.BoolValue {
			return "true"
		}
		// False ==> empty string
		return ""
	case *rc_proto.Value_Obsolete:
		return " #OBSOLETE"
	default:
		// Flagged as error elsewhere, so return empty string here.
		return ""
	}
}

// Returns a string representation of the type of the value for make
func ValueType(value *rc_proto.Value) string {
	if value == nil || value.Val == nil {
		return "unspecified"
	}
	switch value.Val.(type) {
	case *rc_proto.Value_UnspecifiedValue:
		return "unspecified"
	case *rc_proto.Value_StringValue:
		return "string"
	case *rc_proto.Value_BoolValue:
		return "bool"
	case *rc_proto.Value_Obsolete:
		return "obsolete"
	default:
		panic("Unhandled type")
	}
}
