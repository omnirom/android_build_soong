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
	InitRegistrationContext.RegisterParallelSingletonType("owners_zip_singleton", ownersZipSingletonFactory)
}

func ownersZipSingletonFactory() Singleton {
	return &ownersZipSingleton{}
}

type ownersZipSingleton struct{}

func (s *ownersZipSingleton) GenerateBuildActions(ctx SingletonContext) {
	fileListFile := PathForArbitraryOutput(ctx, ".module_paths", "OWNERS.list")
	out := PathForOutput(ctx, "owners.zip")
	dep := PathForOutput(ctx, "owners.zip.d")

	builder := NewRuleBuilder(pctx, ctx)
	builder.Command().BuiltTool("soong_zip").
		FlagWithOutput("-o ", out).
		FlagWithInput("-l ", fileListFile)
	builder.Command().Textf("echo '%s : ' $(cat %s) > ", out, fileListFile).DepFile(dep)
	builder.Build("owners_zip", "Building owners.zip")

	ctx.Phony("owners", out)
	ctx.DistForGoals([]string{"general-tests"}, out)
}
