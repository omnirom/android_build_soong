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

package android

func init() {
	InitRegistrationContext.RegisterSingletonType("test_mapping_zip_singleton", testMappingZipSingletonFactory)
}

func testMappingZipSingletonFactory() Singleton {
	return &testMappingZipSingleton{}
}

type testMappingZipSingleton struct{}

func (s *testMappingZipSingleton) GenerateBuildActions(ctx SingletonContext) {
	fileListFile := PathForArbitraryOutput(ctx, ".module_paths", "TEST_MAPPING.list")
	out := PathForOutput(ctx, "test_mappings.zip")
	dep := PathForOutput(ctx, "test_mappings.zip.d")

	// disabled-presubmit-tests used to be filled out based on modules that set
	// LOCAL_PRESUBMIT_DISABLED. But that's no longer used and there was never a soong equivalent
	// anyways, so just always create an empty file.
	disabledPresubmitTestsFile := PathForOutput(ctx, "disabled-presubmit-tests")
	WriteFileRule(ctx, disabledPresubmitTestsFile, "")

	builder := NewRuleBuilder(pctx, ctx)
	builder.Command().BuiltTool("soong_zip").
		FlagWithOutput("-o ", out).
		FlagWithInput("-l ", fileListFile).
		FlagWithArg("-e ", "disabled-presubmit-tests").
		FlagWithInput("-f ", disabledPresubmitTestsFile)
	builder.Command().Textf("echo '%s : ' $(cat %s) > ", out, fileListFile).DepFile(dep)
	builder.Build("test_mappings_zip", "build TEST_MAPPING zip")

	ctx.Phony("test_mapping", out)
	ctx.DistForGoals([]string{"dist_files", "test_mapping"}, out)
}
