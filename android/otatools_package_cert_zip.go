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

import (
	"github.com/google/blueprint"
)

func init() {
	RegisterOtatoolsPackageBuildComponents(InitRegistrationContext)
	pctx.HostBinToolVariable("SoongZipCmd", "soong_zip")
}

func RegisterOtatoolsPackageBuildComponents(ctx RegistrationContext) {
	ctx.RegisterModuleType("otatools_package_cert_files", OtatoolsPackageFactory)
}

type OtatoolsPackage struct {
	ModuleBase
}

func OtatoolsPackageFactory() Module {
	module := &OtatoolsPackage{}
	InitAndroidModule(module)
	return module
}

var (
	otatoolsPackageCertRule = pctx.AndroidStaticRule("otatools_package_cert_files", blueprint.RuleParams{
		Command:     "echo '$out : ' $$(cat $in) > ${out}.d && ${SoongZipCmd} -o $out -l $in",
		CommandDeps: []string{"${SoongZipCmd}"},
		Depfile:     "${out}.d",
		Description: "Zip otatools-package cert files",
	})
)

func (fg *OtatoolsPackage) GenerateAndroidBuildActions(ctx ModuleContext) {
	if ctx.ModuleDir() != "build/make/tools/otatools_package" {
		ctx.ModuleErrorf("There can only be one otatools_package_cert_files module in build/make/tools/otatools_package")
		return
	}
	fileListFile := PathForArbitraryOutput(ctx, ".module_paths", "OtaToolsCertFiles.list")
	otatoolsPackageCertZip := PathForModuleOut(ctx, "otatools_package_cert_files.zip")
	ctx.Build(pctx, BuildParams{
		Rule:   otatoolsPackageCertRule,
		Input:  fileListFile,
		Output: otatoolsPackageCertZip,
	})
	ctx.SetOutputFiles([]Path{otatoolsPackageCertZip}, "")
}
