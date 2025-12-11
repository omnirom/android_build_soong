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

package testsuites

import (
	"android/soong/android"
	"testing"
)

func TestBuildTestList(t *testing.T) {
	t.Parallel()
	ctx := android.GroupFixturePreparers(
		prepareForFakeTestSuite,
		android.FixtureRegisterWithContext(func(ctx android.RegistrationContext) {
			ctx.RegisterParallelSingletonType("testsuites", testSuiteFilesFactory)
		}),
	).RunTestWithBp(t, `
		fake_module {
			name: "module1",
			outputs: [
				"Test1/Test1.config",
				"Test1/Test1.apk",
			],
			test_suites: ["ravenwood-tests"],
		}
		fake_module {
			name: "module2",
			outputs: [
				"Test2/Test21/Test21.config",
				"Test2/Test21/Test21.apk",
			],
			test_suites: ["ravenwood-tests", "robolectric-tests"],
		}
		fake_module {
			name: "module_without_config",
			outputs: [
				"BadTest/BadTest.jar",
			],
			test_suites: ["robolectric-tests"],
		}
	`)

	config := ctx.SingletonForTests(t, "testsuites")

	wantContents := map[string]string{
		"out/soong/packaging/robolectric-tests_list": `host/testcases/Test2/Test21/Test21.config
`,
		"out/soong/packaging/ravenwood-tests_list": `host/testcases/Test1/Test1.config
host/testcases/Test2/Test21/Test21.config
`,
	}
	for file, want := range wantContents {
		got := android.ContentFromFileRuleForTests(t, ctx.TestContext, config.Output(file))

		if want != got {
			t.Errorf("want %q, got %q", want, got)
		}
	}
}

type fake_module struct {
	android.ModuleBase
	props struct {
		Outputs     []string
		Test_suites []string
	}
}

func fakeTestSuiteFactory() android.Module {
	module := &fake_module{}
	module.AddProperties(&module.props)
	android.InitAndroidModule(module)
	return module
}

var prepareForFakeTestSuite = android.GroupFixturePreparers(
	android.FixtureRegisterWithContext(func(ctx android.RegistrationContext) {
		ctx.RegisterModuleType("fake_module", fakeTestSuiteFactory)
	}),
)

func (f *fake_module) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	for _, output := range f.props.Outputs {
		f := android.PathForModuleOut(ctx, output)
		ctx.InstallFile(pathForTestCases(ctx), output, f)
	}

	android.SetProvider(ctx, android.TestSuiteInfoProvider, android.TestSuiteInfo{
		TestSuites: f.TestSuites(),
	})
}

func (f *fake_module) TestSuites() []string {
	return f.props.Test_suites
}
