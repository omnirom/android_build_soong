// Copyright 2019 Google Inc. All rights reserved.
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

func TestCcPreprocessNoConfiguration(t *testing.T) {
	bp := `
	cc_preprocess_no_configuration {
		name: "foo",
		srcs: ["main.cc"],
		cflags: ["-E", "-DANDROID"],
	}
	`

	fixture := android.GroupFixturePreparers(
		android.PrepareForIntegrationTestWithAndroid,
		android.FixtureRegisterWithContext(RegisterCCPreprocessNoConfiguration),
		android.FixtureAddTextFile("foo/bar/Android.bp", bp),
	)

	result := fixture.RunTest(t)

	foo := result.ModuleForTests(t, "foo", "")
	actual := foo.Rule("cc").Args["cFlags"]
	expected := "-E -DANDROID -Ifoo/bar"
	android.AssertStringEquals(t, "cflags should be correct", expected, actual)
}
