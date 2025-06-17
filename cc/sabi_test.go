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

package cc

import (
	"android/soong/android"
	"testing"
)

func TestSabi(t *testing.T) {
	bp := `
		cc_library {
			name: "libsabi",
			srcs: ["sabi.cpp"],
			static_libs: ["libdirect"],
			header_abi_checker: {
				enabled: true,
				symbol_file: "libsabi.map.txt",
                ref_dump_dirs: ["abi-dumps"],
			},
		}

		cc_library {
			name: "libdirect",
			srcs: ["direct.cpp"],
			whole_static_libs: ["libtransitive"],
		}

		cc_library {
			name: "libtransitive",
			srcs: ["transitive.cpp"],
		}
	`

	result := android.GroupFixturePreparers(
		PrepareForTestWithCcDefaultModules,
	).RunTestWithBp(t, bp)

	libsabiStatic := result.ModuleForTests(t, "libsabi", "android_arm64_armv8-a_static_sabi")
	sabiObjSDump := libsabiStatic.Output("obj/sabi.sdump")

	libDirect := result.ModuleForTests(t, "libdirect", "android_arm64_armv8-a_static_sabi")
	directObjSDump := libDirect.Output("obj/direct.sdump")

	libTransitive := result.ModuleForTests(t, "libtransitive", "android_arm64_armv8-a_static_sabi")
	transitiveObjSDump := libTransitive.Output("obj/transitive.sdump")

	libsabiShared := result.ModuleForTests(t, "libsabi", "android_arm64_armv8-a_shared")
	sabiLink := libsabiShared.Rule("sAbiLink")

	android.AssertStringListContains(t, "sabi link inputs", sabiLink.Inputs.Strings(), sabiObjSDump.Output.String())
	android.AssertStringListContains(t, "sabi link inputs", sabiLink.Inputs.Strings(), directObjSDump.Output.String())
	android.AssertStringListContains(t, "sabi link inputs", sabiLink.Inputs.Strings(), transitiveObjSDump.Output.String())
}
