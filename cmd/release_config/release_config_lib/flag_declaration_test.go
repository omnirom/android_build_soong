// Copyright 2025 Google Inc. All rights reserved.
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
	"os"
	"path/filepath"
	"strings"
	"testing"

	rc_proto "android/soong/cmd/release_config/release_config_proto"
	// For Assert*.
	"android/soong/android"

	"google.golang.org/protobuf/proto"
)

type testCaseFlagDeclarationFactory struct {
	protoPath string
	name      string
	data      []byte
	expected  *rc_proto.FlagDeclaration
	err       error
}

func (tc testCaseFlagDeclarationFactory) assertProtoEqual(t *testing.T, expected, actual proto.Message) {
	if !proto.Equal(expected, actual) {
		t.Errorf("Expected %q found %q", expected, actual)
	}
}

func TestFlagDeclarationFactory(t *testing.T) {
	testCases := []testCaseFlagDeclarationFactory{
		{
			name:      "boolVal",
			protoPath: "build/release/flag_values/test/RELEASE_FOO.textproto",
			data:      []byte(`name: "RELEASE_FOO" namespace: "soong_test" value {bool_value: false} workflow: LAUNCH containers: "product"`),
			expected: &rc_proto.FlagDeclaration{
				Name:       proto.String("RELEASE_FOO"),
				Namespace:  proto.String("soong_test"),
				Value:      &rc_proto.Value{Val: &rc_proto.Value_BoolValue{false}},
				Workflow:   rc_proto.Workflow_LAUNCH.Enum(),
				Containers: []string{"product"},
			},
			err: nil,
		},
		{
			name:      "missingWorkflow",
			protoPath: "build/release/flag_values/test/RELEASE_FOO.textproto",
			data:      []byte(`name: "RELEASE_FOO" namespace: "soong_test" value {bool_value: false} containers: "product"`),
			expected:  nil,
			err:       fmt.Errorf("has no workflow"),
		},
		{
			name:      "badName",
			protoPath: "build/release/flag_declarations/FOO.textproto",
			data:      []byte(`name: "FOO" namespace: "soong_test" workflow: MANUAL`),
			expected:  nil,
			err:       fmt.Errorf("flag names must begin with 'RELEASE_'"),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			tempdir := t.TempDir()
			path := filepath.Join(tempdir, tc.protoPath)
			if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, tc.data, 0644); err != nil {
				t.Fatal(err)
			}
			actual, err := FlagDeclarationFactory(path)
			if tc.err == nil {
				android.AssertSame(t, "Expected %v got %v", tc.err, err)
				tc.assertProtoEqual(t, tc.expected, actual)
			} else if err == nil {
				t.Errorf("Expected error containing '%q' got nil", tc.err.Error())
			} else if !strings.Contains(err.Error(), tc.err.Error()) {
				t.Errorf("Error %v does not include %v", err.Error(), tc.err.Error())
			}
		})
	}
}
