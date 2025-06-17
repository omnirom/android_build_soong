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

package aconfig

import (
	"strings"
	"testing"

	"android/soong/android"
)

func TestAllAconfigDeclarationsExtension(t *testing.T) {
	result := android.GroupFixturePreparers(
		PrepareForTestWithAconfigBuildComponents,
		android.FixtureMergeMockFs(
			android.MockFS{
				"a.txt":     nil,
				"flags.txt": nil,
			},
		),
	).RunTestWithBp(t, `
		all_aconfig_declarations {
			name: "all_aconfig_declarations",
		}

		all_aconfig_declarations_extension {
			name: "custom_aconfig_declarations",
			base: "all_aconfig_declarations",
			api_signature_files: [
				"a.txt",
			],
			finalized_flags_file: "flags.txt",
		}
	`)

	finalizedFlags := result.ModuleForTests(t, "custom_aconfig_declarations", "").Output("finalized-flags.txt")
	android.AssertStringContainsEquals(t, "must depend on all_aconfig_declarations", strings.Join(finalizedFlags.Inputs.Strings(), " "), "all_aconfig_declarations.pb", true)
}
