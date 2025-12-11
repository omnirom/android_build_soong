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

package unbundled

import (
	"android/soong/android"
	"android/soong/java"
	"strings"
	"testing"
)

func TestProguardZipWithOverrideApp(t *testing.T) {
	t.Parallel()
	testResult := android.GroupFixturePreparers(
		android.FixtureRegisterWithContext(registerUnbundledBuilder),
		java.PrepareForTestWithJavaDefaultModules,
		android.FixtureModifyConfig(func(config android.Config) {
			config.TestProductVariables.Unbundled_build_apps = []string{"foo", "fooOverride"}
		}),
		android.FixtureAddTextFile("build/soong/unbundled/Android.bp", `
		unbundled_builder {
			name: "unbundled_builder",
		}
		`),
		android.FixtureAddTextFile("build/soong/Android.bp", `
		android_app {
			name: "foo",
			sdk_version: "current",
			optimize: {
				enabled: true,
			},
			srcs: ["foo.java"],
		}
		override_android_app {
			name: "fooOverride",
			base: "foo",
		}
		`),
		android.FixtureAddTextFile("build/soong/foo.java", ""),
	).RunTest(t)

	m := testResult.ModuleForTests(t, "unbundled_builder", "")
	rule := m.Rule("proguard_dict_zip")

	// Test that foo and fooOverride get placed in different locations in the zip
	expected := strings.Join([]string{
		"-e out/target/common/obj/APPS/foo_intermediates/proguard_dictionary",
		"-f out/soong/.intermediates/build/soong/foo/android_common/proguard_dictionary",
		"-e out/target/common/obj/APPS/foo_intermediates/classes.jar",
		"-f out/soong/.intermediates/build/soong/foo/android_common/combined/foo.jar",
		"-e out/target/common/obj/APPS/fooOverride_intermediates/proguard_dictionary",
		"-f out/soong/.intermediates/build/soong/foo/android_common_fooOverride/proguard_dictionary",
		"-e out/target/common/obj/APPS/fooOverride_intermediates/classes.jar",
		"-f out/soong/.intermediates/build/soong/foo/android_common_fooOverride/combined/foo.jar",
	}, " ")

	if !strings.Contains(rule.RuleParams.Command, expected) {
		t.Fatalf("Expected command to contain:\n  %s\nBut was actually:\n  %s\n", expected, rule.RuleParams.Command)
	}
}
