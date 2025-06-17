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
	"android/soong/sh"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/google/blueprint"
)

const bp = `
		android_app {
			name: "foo",
			srcs: ["a.java"],
			sdk_version: "current",
		}

                android_test_helper_app {
                        name: "HelperApp",
                        srcs: ["helper.java"],
                }

		android_test {
			name: "base",
			sdk_version: "current",
                        data: [":HelperApp", "data/testfile"],
                        host_required: ["other-module"],
                        test_suites: ["general-tests"],
		}

                test_module_config {
                        name: "derived_test",
                        base: "base",
                        exclude_filters: ["android.test.example.devcodelab.DevCodelabTest#testHelloFail"],
                        include_annotations: ["android.platform.test.annotations.LargeTest"],
                        test_suites: ["general-tests"],
                }

`

const variant = "android_arm64_armv8-a"

// Ensure we create files needed and set the AndroidMkEntries needed
func TestModuleConfigAndroidTest(t *testing.T) {

	ctx := android.GroupFixturePreparers(
		java.PrepareForTestWithJavaDefaultModules,
		android.FixtureRegisterWithContext(RegisterTestModuleConfigBuildComponents),
	).RunTestWithBp(t, bp)

	derived := ctx.ModuleForTests(t, "derived_test", variant)
	// Assert there are rules to create these files.
	derived.Output("test_module_config.manifest")
	derived.Output("test_config_fixer/derived_test.config")

	// Ensure some basic rules exist.
	ctx.ModuleForTests(t, "base", "android_common").Output("package-res.apk")
	entries := android.AndroidMkEntriesForTest(t, ctx.TestContext, derived.Module())[0]

	// Ensure some entries from base are there, specifically support files for data and helper apps.
	// Do not use LOCAL_COMPATIBILITY_SUPPORT_FILES, but instead use LOCAL_SOONG_INSTALLED_COMPATIBILITY_SUPPORT_FILES
	android.AssertStringPathsRelativeToTopEquals(t, "support-files", ctx.Config,
		[]string{"out/target/product/test_device/testcases/derived_test/arm64/base.apk",
			"out/target/product/test_device/testcases/derived_test/HelperApp.apk",
			"out/target/product/test_device/testcases/derived_test/data/testfile"},
		entries.EntryMap["LOCAL_SOONG_INSTALLED_COMPATIBILITY_SUPPORT_FILES"])
	android.AssertArrayString(t, "", entries.EntryMap["LOCAL_COMPATIBILITY_SUPPORT_FILES"], []string{})

	android.AssertArrayString(t, "", entries.EntryMap["LOCAL_REQUIRED_MODULES"], []string{"base"})
	android.AssertArrayString(t, "", entries.EntryMap["LOCAL_HOST_REQUIRED_MODULES"], []string{"other-module"})
	android.AssertArrayString(t, "", entries.EntryMap["LOCAL_CERTIFICATE"], []string{"build/make/target/product/security/testkey.x509.pem"})
	android.AssertStringEquals(t, "", entries.Class, "APPS")

	// And some new derived entries are there.
	android.AssertArrayString(t, "", entries.EntryMap["LOCAL_MODULE_TAGS"], []string{"tests"})

	android.AssertStringMatches(t, "", entries.EntryMap["LOCAL_FULL_TEST_CONFIG"][0], fmt.Sprintf("derived_test/%s/test_config_fixer/derived_test.config", variant))

	// Check the footer lines.  Our support files should depend on base's support files.
	convertedActual := make([]string, 5)
	for i, e := range entries.FooterLinesForTests() {
		// AssertStringPathsRelativeToTop doesn't replace both instances
		convertedActual[i] = strings.Replace(e, ctx.Config.OutDir(), "", 2)
	}
	android.AssertArrayString(t, fmt.Sprintf("%s", ctx.Config.OutDir()), []string{
		"include $(BUILD_SYSTEM)/soong_app_prebuilt.mk",
		"/target/product/test_device/testcases/derived_test/arm64/base.apk: /target/product/test_device/testcases/base/arm64/base.apk",
		"/target/product/test_device/testcases/derived_test/HelperApp.apk: /target/product/test_device/testcases/base/HelperApp.apk",
		"/target/product/test_device/testcases/derived_test/data/testfile: /target/product/test_device/testcases/base/data/testfile",
		"",
	}, convertedActual)
}

func TestModuleConfigShTest(t *testing.T) {
	ctx := android.GroupFixturePreparers(
		sh.PrepareForTestWithShBuildComponents,
		android.PrepareForTestWithAndroidBuildComponents,
		android.FixtureMergeMockFs(android.MockFS{
			"test.sh":            nil,
			"testdata/data1":     nil,
			"testdata/sub/data2": nil,
		}),
		android.FixtureRegisterWithContext(RegisterTestModuleConfigBuildComponents),
	).RunTestWithBp(t, `
		sh_test {
			name: "shell_test",
			src: "test.sh",
			filename: "test.sh",
                        test_suites: ["general-tests"],
			data: [
				"testdata/data1",
				"testdata/sub/data2",
			],
		}
                test_module_config {
                        name: "conch",
                        base: "shell_test",
                        test_suites: ["general-tests"],
                        options: [{name: "SomeName", value: "OptionValue"}],
                }
         `)
	derived := ctx.ModuleForTests(t, "conch", variant) //
	conch := derived.Module().(*testModuleConfigModule)
	android.AssertArrayString(t, "TestcaseRelDataFiles", []string{"arm64/testdata/data1", "arm64/testdata/sub/data2"}, conch.provider.TestcaseRelDataFiles)
	android.AssertStringEquals(t, "Rel OutputFile", "test.sh", conch.provider.OutputFile.Rel())

	// Assert there are rules to create these files.
	derived.Output("test_module_config.manifest")
	derived.Output("test_config_fixer/conch.config")

	// Ensure some basic rules exist.
	entries := android.AndroidMkEntriesForTest(t, ctx.TestContext, derived.Module())[0]

	// Ensure some entries from base are there, specifically support files for data and helper apps.
	// Do not use LOCAL_COMPATIBILITY_SUPPORT_FILES, but instead use LOCAL_SOONG_INSTALLED_COMPATIBILITY_SUPPORT_FILES
	android.AssertStringPathsRelativeToTopEquals(t, "support-files", ctx.Config,
		[]string{"out/target/product/test_device/testcases/conch/arm64/testdata/data1",
			"out/target/product/test_device/testcases/conch/arm64/testdata/sub/data2"},
		entries.EntryMap["LOCAL_SOONG_INSTALLED_COMPATIBILITY_SUPPORT_FILES"])
	android.AssertArrayString(t, "", entries.EntryMap["LOCAL_COMPATIBILITY_SUPPORT_FILES"], []string{})

	android.AssertStringEquals(t, "app class", "NATIVE_TESTS", entries.Class)
	android.AssertArrayString(t, "required modules", []string{"shell_test"}, entries.EntryMap["LOCAL_REQUIRED_MODULES"])
	android.AssertArrayString(t, "host required modules", []string{}, entries.EntryMap["LOCAL_HOST_REQUIRED_MODULES"])
	android.AssertArrayString(t, "cert", []string{}, entries.EntryMap["LOCAL_CERTIFICATE"])

	// And some new derived entries are there.
	android.AssertArrayString(t, "tags", []string{}, entries.EntryMap["LOCAL_MODULE_TAGS"])

	android.AssertStringMatches(t, "", entries.EntryMap["LOCAL_FULL_TEST_CONFIG"][0],
		fmt.Sprintf("conch/%s/test_config_fixer/conch.config", variant))

	// Check the footer lines.  Our support files should depend on base's support files.
	convertedActual := make([]string, 4)
	for i, e := range entries.FooterLinesForTests() {
		// AssertStringPathsRelativeToTop doesn't replace both instances
		convertedActual[i] = strings.Replace(e, ctx.Config.OutDir(), "", 2)
	}
	android.AssertArrayString(t, fmt.Sprintf("%s", ctx.Config.OutDir()), []string{
		"include $(BUILD_SYSTEM)/soong_cc_rust_prebuilt.mk",
		"/target/product/test_device/testcases/conch/arm64/testdata/data1: /target/product/test_device/testcases/shell_test/arm64/testdata/data1",
		"/target/product/test_device/testcases/conch/arm64/testdata/sub/data2: /target/product/test_device/testcases/shell_test/arm64/testdata/sub/data2",
		"",
	}, convertedActual)

}

// Make sure we call test-config-fixer with the right args.
func TestModuleConfigOptions(t *testing.T) {

	ctx := android.GroupFixturePreparers(
		java.PrepareForTestWithJavaDefaultModules,
		android.FixtureRegisterWithContext(RegisterTestModuleConfigBuildComponents),
	).RunTestWithBp(t, bp)

	// Check that we generate a rule to make a new AndroidTest.xml/Module.config file.
	derived := ctx.ModuleForTests(t, "derived_test", variant)
	rule_cmd := derived.Rule("fix_test_config").RuleParams.Command
	android.AssertStringDoesContain(t, "Bad FixConfig rule inputs", rule_cmd,
		`--test-runner-options='[{"Name":"exclude-filter","Key":"","Value":"android.test.example.devcodelab.DevCodelabTest#testHelloFail"},{"Name":"include-annotation","Key":"","Value":"android.platform.test.annotations.LargeTest"}]'`)
}

// Ensure we error for a base we don't support.
func TestModuleConfigWithHostBaseShouldFailWithExplicitMessage(t *testing.T) {
	badBp := `
        java_test {
            name: "base",
            srcs: ["a.java"],
		}

        test_module_config {
            name: "derived_test",
            base: "base",
            exclude_filters: ["android.test.example.devcodelab.DevCodelabTest#testHelloFail"],
            include_annotations: ["android.platform.test.annotations.LargeTest"],
            test_suites: ["general-tests"],
        }`

	android.GroupFixturePreparers(
		java.PrepareForTestWithJavaDefaultModules,
		android.FixtureRegisterWithContext(RegisterTestModuleConfigBuildComponents),
	).ExtendWithErrorHandler(
		android.FixtureExpectsAtLeastOneErrorMatchingPattern("'base' module used as base but it is not a 'android_test' module.")).
		RunTestWithBp(t, badBp)
}

func TestModuleConfigBadBaseShouldFailWithGeneralMessage(t *testing.T) {
	badBp := `
		java_library {
			name: "base",
                        srcs: ["a.java"],
		}

                test_module_config {
                        name: "derived_test",
                        base: "base",
                        exclude_filters: ["android.test.example.devcodelab.DevCodelabTest#testHelloFail"],
                        include_annotations: ["android.platform.test.annotations.LargeTest"],
                        test_suites: ["general-tests"],
                }`

	android.GroupFixturePreparers(
		java.PrepareForTestWithJavaDefaultModules,
		android.FixtureRegisterWithContext(RegisterTestModuleConfigBuildComponents),
	).ExtendWithErrorHandler(
		android.FixtureExpectsAtLeastOneErrorMatchingPattern("'base' module used as base but it is not a 'android_test' module.")).
		RunTestWithBp(t, badBp)
}

func TestModuleConfigNoBaseShouldFail(t *testing.T) {
	badBp := `
		java_library {
			name: "base",
                        srcs: ["a.java"],
		}

                test_module_config {
                        name: "derived_test",
                        exclude_filters: ["android.test.example.devcodelab.DevCodelabTest#testHelloFail"],
                        include_annotations: ["android.platform.test.annotations.LargeTest"],
                        test_suites: ["general-tests"],
                }`

	android.GroupFixturePreparers(
		java.PrepareForTestWithJavaDefaultModules,
		android.FixtureRegisterWithContext(RegisterTestModuleConfigBuildComponents),
	).ExtendWithErrorHandler(
		android.FixtureExpectsOneErrorPattern("'base' field must be set to a 'android_test' module.")).
		RunTestWithBp(t, badBp)
}

// Ensure we error for a base we don't support.
func TestModuleConfigNoFiltersOrAnnotationsShouldFail(t *testing.T) {
	badBp := `
		android_test {
			name: "base",
			sdk_version: "current",
                        srcs: ["a.java"],
                        test_suites: ["general-tests"],
		}

                test_module_config {
                        name: "derived_test",
                        base: "base",
                        test_suites: ["general-tests"],
                }`

	ctx := android.GroupFixturePreparers(
		java.PrepareForTestWithJavaDefaultModules,
		android.FixtureRegisterWithContext(RegisterTestModuleConfigBuildComponents),
	).ExtendWithErrorHandler(
		android.FixtureExpectsAtLeastOneErrorMatchingPattern("Test options must be given")).
		RunTestWithBp(t, badBp)
	ctx.ModuleForTests(t, "derived_test", variant)
}

func TestModuleConfigMultipleDerivedTestsWriteDistinctMakeEntries(t *testing.T) {
	multiBp := `
		android_test {
			name: "base",
			sdk_version: "current",
                        srcs: ["a.java"],
                        data: [":HelperApp", "data/testfile"],
                        test_suites: ["general-tests"],
		}

                android_test_helper_app {
                        name: "HelperApp",
                        srcs: ["helper.java"],
                }

                test_module_config {
                        name: "derived_test",
                        base: "base",
                        include_annotations: ["android.platform.test.annotations.LargeTest"],
                        test_suites: ["general-tests"],
                }

                test_module_config {
                        name: "another_derived_test",
                        base: "base",
                        include_annotations: ["android.platform.test.annotations.LargeTest"],
                        test_suites: ["general-tests"],
                }`

	ctx := android.GroupFixturePreparers(
		java.PrepareForTestWithJavaDefaultModules,
		android.FixtureRegisterWithContext(RegisterTestModuleConfigBuildComponents),
	).RunTestWithBp(t, multiBp)

	{
		derived := ctx.ModuleForTests(t, "derived_test", variant)
		entries := android.AndroidMkEntriesForTest(t, ctx.TestContext, derived.Module())[0]
		// All these should be the same in both derived tests
		android.AssertStringPathsRelativeToTopEquals(t, "support-files", ctx.Config,
			[]string{"out/target/product/test_device/testcases/derived_test/arm64/base.apk",
				"out/target/product/test_device/testcases/derived_test/HelperApp.apk",
				"out/target/product/test_device/testcases/derived_test/data/testfile"},
			entries.EntryMap["LOCAL_SOONG_INSTALLED_COMPATIBILITY_SUPPORT_FILES"])

		// Except this one, which points to the updated tradefed xml file.
		android.AssertStringMatches(t, "", entries.EntryMap["LOCAL_FULL_TEST_CONFIG"][0], fmt.Sprintf("derived_test/%s/test_config_fixer/derived_test.config", variant))
		// And this one, the module name.
		android.AssertArrayString(t, "", entries.EntryMap["LOCAL_MODULE"], []string{"derived_test"})
	}

	{
		derived := ctx.ModuleForTests(t, "another_derived_test", variant)
		entries := android.AndroidMkEntriesForTest(t, ctx.TestContext, derived.Module())[0]
		// All these should be the same in both derived tests
		android.AssertStringPathsRelativeToTopEquals(t, "support-files", ctx.Config,
			[]string{"out/target/product/test_device/testcases/another_derived_test/arm64/base.apk",
				"out/target/product/test_device/testcases/another_derived_test/HelperApp.apk",
				"out/target/product/test_device/testcases/another_derived_test/data/testfile"},
			entries.EntryMap["LOCAL_SOONG_INSTALLED_COMPATIBILITY_SUPPORT_FILES"])
		// Except this one, which points to the updated tradefed xml file.
		android.AssertStringMatches(t, "", entries.EntryMap["LOCAL_FULL_TEST_CONFIG"][0],
			fmt.Sprintf("another_derived_test/%s/test_config_fixer/another_derived_test.config", variant))
		// And this one, the module name.
		android.AssertArrayString(t, "", entries.EntryMap["LOCAL_MODULE"], []string{"another_derived_test"})
	}
}

// Test_module_config_host rule is allowed to depend on java_test_host
func TestModuleConfigHostBasics(t *testing.T) {
	bp := `
               java_test_host {
                        name: "base",
                        srcs: ["a.java"],
                        test_suites: ["suiteA", "general-tests",  "suiteB"],
               }

                test_module_config_host {
                        name: "derived_test",
                        base: "base",
                        exclude_filters: ["android.test.example.devcodelab.DevCodelabTest#testHelloFail"],
                        include_annotations: ["android.platform.test.annotations.LargeTest"],
                        test_suites: ["general-tests"],
                }`

	ctx := android.GroupFixturePreparers(
		java.PrepareForTestWithJavaDefaultModules,
		android.FixtureRegisterWithContext(RegisterTestModuleConfigBuildComponents),
	).RunTestWithBp(t, bp)

	variant := ctx.Config.BuildOS.String() + "_common"
	derived := ctx.ModuleForTests(t, "derived_test", variant)
	mod := derived.Module().(*testModuleConfigHostModule)
	allEntries := android.AndroidMkEntriesForTest(t, ctx.TestContext, mod)
	entries := allEntries[0]
	android.AssertArrayString(t, "", entries.EntryMap["LOCAL_MODULE"], []string{"derived_test"})
	android.AssertArrayString(t, "", entries.EntryMap["LOCAL_SDK_VERSION"], []string{"private_current"})
	android.AssertStringEquals(t, "", entries.Class, "JAVA_LIBRARIES")

	if !mod.Host() {
		t.Errorf("host bit is not set for a java_test_host module.")
	}
	actualData, _ := strconv.ParseBool(entries.EntryMap["LOCAL_IS_UNIT_TEST"][0])
	android.AssertBoolEquals(t, "LOCAL_IS_UNIT_TEST", true, actualData)

}

// When you pass an 'android_test' as base, the warning message is a bit obscure,
// talking about variants, but it is something.  Ideally we could do better.
func TestModuleConfigHostBadBaseShouldFailWithVariantWarning(t *testing.T) {
	badBp := `
		android_test {
			name: "base",
			sdk_version: "current",
                        srcs: ["a.java"],
		}

                test_module_config_host {
                        name: "derived_test",
                        base: "base",
                        exclude_filters: ["android.test.example.devcodelab.DevCodelabTest#testHelloFail"],
                        include_annotations: ["android.platform.test.annotations.LargeTest"],
                }`

	android.GroupFixturePreparers(
		java.PrepareForTestWithJavaDefaultModules,
		android.FixtureRegisterWithContext(RegisterTestModuleConfigBuildComponents),
	).ExtendWithErrorHandler(
		android.FixtureExpectsAtLeastOneErrorMatchingPattern("missing variant")).
		RunTestWithBp(t, badBp)
}

func TestModuleConfigHostNeedsATestSuite(t *testing.T) {
	badBp := `
		java_test_host {
			name: "base",
                        srcs: ["a.java"],
		}

                test_module_config_host {
                        name: "derived_test",
                        base: "base",
                        exclude_filters: ["android.test.example.devcodelab.DevCodelabTest#testHelloFail"],
                        include_annotations: ["android.platform.test.annotations.LargeTest"],
                }`

	android.GroupFixturePreparers(
		java.PrepareForTestWithJavaDefaultModules,
		android.FixtureRegisterWithContext(RegisterTestModuleConfigBuildComponents),
	).ExtendWithErrorHandler(
		android.FixtureExpectsAtLeastOneErrorMatchingPattern("At least one test-suite must be set")).
		RunTestWithBp(t, badBp)
}

func TestModuleConfigNonMatchingTestSuitesGiveErrors(t *testing.T) {
	badBp := `
		java_test_host {
			name: "base",
                        srcs: ["a.java"],
                        test_suites: ["general-tests", "some-compat"],
		}

                test_module_config_host {
                        name: "derived_test",
                        base: "base",
                        exclude_filters: ["android.test.example.devcodelab.DevCodelabTest#testHelloFail"],
                        include_annotations: ["android.platform.test.annotations.LargeTest"],
                        test_suites: ["device-tests", "random-suite"],
                }`

	android.GroupFixturePreparers(
		java.PrepareForTestWithJavaDefaultModules,
		android.FixtureRegisterWithContext(RegisterTestModuleConfigBuildComponents),
	).ExtendWithErrorHandler(
		// Use \\ to escape bracket so it isn't used as [] set for regex.
		android.FixtureExpectsAtLeastOneErrorMatchingPattern("Suites: \\[device-tests, random-suite] listed but do not exist in base module")).
		RunTestWithBp(t, badBp)
}

func TestTestOnlyProvider(t *testing.T) {
	t.Parallel()
	ctx := android.GroupFixturePreparers(
		java.PrepareForTestWithJavaDefaultModules,
		android.FixtureRegisterWithContext(RegisterTestModuleConfigBuildComponents),
	).RunTestWithBp(t, `
                // These should be test-only
                test_module_config_host {
                        name: "host-derived-test",
                        base: "host-base",
                        exclude_filters: ["android.test.example.devcodelab.DevCodelabTest#testHelloFail"],
                        include_annotations: ["android.platform.test.annotations.LargeTest"],
                        test_suites: ["general-tests"],
                }

                test_module_config {
                        name: "derived-test",
                        base: "base",
                        exclude_filters: ["android.test.example.devcodelab.DevCodelabTest#testHelloFail"],
                        include_annotations: ["android.platform.test.annotations.LargeTest"],
                        test_suites: ["general-tests"],
                }

		android_test {
			name: "base",
			sdk_version: "current",
                        data: ["data/testfile"],
                        test_suites: ["general-tests"],
		}

		java_test_host {
			name: "host-base",
                        srcs: ["a.java"],
                        test_suites: ["general-tests"],
		}`,
	)

	// Visit all modules and ensure only the ones that should
	// marked as test-only are marked as test-only.

	actualTestOnly := []string{}
	ctx.VisitAllModules(func(m blueprint.Module) {
		if provider, ok := android.OtherModuleProvider(ctx.TestContext.OtherModuleProviderAdaptor(), m, android.TestOnlyProviderKey); ok {
			if provider.TestOnly {
				actualTestOnly = append(actualTestOnly, m.Name())
			}
		}
	})
	expectedTestOnlyModules := []string{
		"host-derived-test",
		"derived-test",
		// android_test and java_test_host are tests too.
		"host-base",
		"base",
	}

	notEqual, left, right := android.ListSetDifference(expectedTestOnlyModules, actualTestOnly)
	if notEqual {
		t.Errorf("test-only: Expected but not found: %v, Found but not expected: %v", left, right)
	}
}
