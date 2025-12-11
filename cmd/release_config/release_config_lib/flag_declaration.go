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

var (
	// Allowlist: these flags may have duplicate (identical) declarations
	// without generating an error.  This will be removed once all such
	// declarations have been fixed.
	DuplicateDeclarationAllowlist = map[string]bool{}
)

func FlagDeclarationFactory(protoPath string) (fd *rc_proto.FlagDeclaration, err error) {
	fd = &rc_proto.FlagDeclaration{}
	if protoPath == "" {
		return fd, nil
	}
	LoadMessage(protoPath, fd)

	switch {
	case fd.Name == nil:
		return nil, fmt.Errorf("Flag declaration %s does not specify name", protoPath)
	case *fd.Name == "RELEASE_ACONFIG_VALUE_SETS":
		return nil, fmt.Errorf("%s: %s is a reserved build flag", protoPath, *fd.Name)
	case fmt.Sprintf("%s.textproto", *fd.Name) != filepath.Base(protoPath):
		return nil, fmt.Errorf("%s incorrectly declares flag %s", protoPath, *fd.Name)
	case !strings.HasPrefix(*fd.Name, "RELEASE_"):
		return nil, fmt.Errorf("%s: flag names must begin with 'RELEASE_'", protoPath)
	case fd.Namespace == nil:
		return nil, fmt.Errorf("Flag declaration %s has no namespace.", protoPath)
	case fd.Workflow == nil:
		return nil, fmt.Errorf("Flag declaration %s has no workflow.", protoPath)
	case fd.Containers != nil:
		for _, container := range fd.Containers {
			if !validContainer(container) {
				return nil, fmt.Errorf("Flag declaration %s has invalid container %s", protoPath, container)
			}
		}
	}

	// If the input didn't specify a value, create one (== UnspecifiedValue).
	if fd.Value == nil {
		fd.Value = &rc_proto.Value{Val: &rc_proto.Value_UnspecifiedValue{false}}
	}

	return fd, nil
}
