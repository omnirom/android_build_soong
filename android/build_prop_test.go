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

package android

import (
	"testing"
)

func TestPropFileInputs(t *testing.T) {
	bp := `
build_prop {
    name: "vendor-build.prop",
    stem: "build.prop",
    vendor: true,
    android_info: ":board-info",
    //product_config: ":product_config",
}
android_info {
    name: "board-info",
    stem: "android-info.txt",
}
`

	res := GroupFixturePreparers(
		FixtureRegisterWithContext(registerBuildPropComponents),
	).RunTestWithBp(t, bp)
	buildPropCmd := res.ModuleForTests(t, "vendor-build.prop", "").Rule("vendor-build.prop_.vendor-build.prop").RuleParams.Command
	AssertStringDoesContain(t, "Could not find android-info in prop files of vendor build.prop", buildPropCmd, "--prop-files=out/soong/.intermediates/board-info/android-info.prop")
}
