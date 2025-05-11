// Copyright 2024 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package tradefed_modules

import (
	"android/soong/android"
	"android/soong/java"
	"encoding/json"
	"slices"
	"testing"
)

func TestTestSuites(t *testing.T) {
	t.Parallel()
	ctx := android.GroupFixturePreparers(
		java.PrepareForTestWithJavaDefaultModules,
		android.FixtureRegisterWithContext(RegisterTestSuiteBuildComponents),
	).RunTestWithBp(t, `
		android_test {
			name: "TestModule1",
			sdk_version: "current",
		}

		android_test {
			name: "TestModule2",
			sdk_version: "current",
		}

		test_suite {
			name: "my-suite",
			description: "a test suite",
			tests: [
				"TestModule1",
				"TestModule2",
			]
		}
	`)
	manifestPath := ctx.ModuleForTests("my-suite", "android_common").Output("out/soong/test_suites/my-suite/my-suite.json")
	var actual testSuiteManifest
	if err := json.Unmarshal([]byte(android.ContentFromFileRuleForTests(t, ctx.TestContext, manifestPath)), &actual); err != nil {
		t.Errorf("failed to unmarshal manifest: %v", err)
	}
	slices.Sort(actual.Files)

	expected := testSuiteManifest{
		Name: "my-suite",
		Files: []string{
			"target/testcases/TestModule1/TestModule1.config",
			"target/testcases/TestModule1/arm64/TestModule1.apk",
			"target/testcases/TestModule2/TestModule2.config",
			"target/testcases/TestModule2/arm64/TestModule2.apk",
		},
	}

	android.AssertDeepEquals(t, "manifests differ", expected, actual)
}

func TestTestSuitesWithNested(t *testing.T) {
	t.Parallel()
	ctx := android.GroupFixturePreparers(
		java.PrepareForTestWithJavaDefaultModules,
		android.FixtureRegisterWithContext(RegisterTestSuiteBuildComponents),
	).RunTestWithBp(t, `
		android_test {
			name: "TestModule1",
			sdk_version: "current",
		}

		android_test {
			name: "TestModule2",
			sdk_version: "current",
		}

		android_test {
			name: "TestModule3",
			sdk_version: "current",
		}

		test_suite {
			name: "my-child-suite",
			description: "a child test suite",
			tests: [
				"TestModule1",
				"TestModule2",
			]
		}

		test_suite {
			name: "my-all-tests-suite",
			description: "a parent test suite",
			tests: [
				"TestModule1",
				"TestModule3",
				"my-child-suite",
			]
		}
	`)
	manifestPath := ctx.ModuleForTests("my-all-tests-suite", "android_common").Output("out/soong/test_suites/my-all-tests-suite/my-all-tests-suite.json")
	var actual testSuiteManifest
	if err := json.Unmarshal([]byte(android.ContentFromFileRuleForTests(t, ctx.TestContext, manifestPath)), &actual); err != nil {
		t.Errorf("failed to unmarshal manifest: %v", err)
	}
	slices.Sort(actual.Files)

	expected := testSuiteManifest{
		Name: "my-all-tests-suite",
		Files: []string{
			"target/testcases/TestModule1/TestModule1.config",
			"target/testcases/TestModule1/arm64/TestModule1.apk",
			"target/testcases/TestModule2/TestModule2.config",
			"target/testcases/TestModule2/arm64/TestModule2.apk",
			"target/testcases/TestModule3/TestModule3.config",
			"target/testcases/TestModule3/arm64/TestModule3.apk",
		},
	}

	android.AssertDeepEquals(t, "manifests differ", expected, actual)
}

func TestTestSuitesNotInstalledInTestcases(t *testing.T) {
	t.Parallel()
	android.GroupFixturePreparers(
		java.PrepareForTestWithJavaDefaultModules,
		android.FixtureRegisterWithContext(RegisterTestSuiteBuildComponents),
	).ExtendWithErrorHandler(android.FixtureExpectsAllErrorsToMatchAPattern([]string{
		`"SomeHostTest" is not installed in testcases`,
	})).RunTestWithBp(t, `
			java_test_host {
				name: "SomeHostTest",
				srcs: ["a.java"],
			}
			test_suite {
				name: "my-suite",
				description: "a test suite",
				tests: [
					"SomeHostTest",
				]
			}
	`)
}
