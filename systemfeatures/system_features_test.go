// Copyright 2024 Google Inc. All rights reserved.
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

package systemfeatures

import (
	"android/soong/android"

	"testing"
)

func TestJavaSystemFeaturesSrcs(t *testing.T) {
	bp := `
java_system_features_srcs {
    name: "system-features-srcs",
	full_class_name: "com.android.test.RoSystemFeatures",
}
`

	res := android.GroupFixturePreparers(
		android.FixtureRegisterWithContext(registerSystemFeaturesComponents),
		android.PrepareForTestWithBuildFlag("RELEASE_USE_SYSTEM_FEATURE_BUILD_FLAGS", "true"),
		android.PrepareForTestWithBuildFlag("RELEASE_SYSTEM_FEATURE_AUTOMOTIVE", "0"),
		android.PrepareForTestWithBuildFlag("RELEASE_SYSTEM_FEATURE_TELEVISION", "UNAVAILABLE"),
		android.PrepareForTestWithBuildFlag("RELEASE_SYSTEM_FEATURE_WATCH", ""),
		android.PrepareForTestWithBuildFlag("RELEASE_NOT_SYSTEM_FEATURE_FOO", "BAR"),
	).RunTestWithBp(t, bp)

	module := res.ModuleForTests(t, "system-features-srcs", "")
	cmd := module.Rule("system-features-srcs").RuleParams.Command
	android.AssertStringDoesContain(t, "Expected fully class name", cmd, " com.android.test.RoSystemFeatures ")
	android.AssertStringDoesContain(t, "Expected readonly flag", cmd, "--readonly=true")
	android.AssertStringDoesContain(t, "Expected AUTOMOTIVE feature flag", cmd, "--feature=AUTOMOTIVE:0 ")
	android.AssertStringDoesContain(t, "Expected TELEVISION feature flag", cmd, "--feature=TELEVISION:UNAVAILABLE ")
	android.AssertStringDoesContain(t, "Expected WATCH feature flag", cmd, "--feature=WATCH: ")
	android.AssertStringDoesNotContain(t, "Unexpected FOO arg from non-system feature flag", cmd, "FOO")

	systemFeaturesModule := module.Module().(*javaSystemFeaturesSrcs)
	expectedOutputPath := "out/soong/.intermediates/system-features-srcs/gen/RoSystemFeatures.java"
	android.AssertPathsRelativeToTopEquals(t, "Expected output file", []string{expectedOutputPath}, systemFeaturesModule.Srcs())
}
