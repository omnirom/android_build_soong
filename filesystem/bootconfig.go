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

package filesystem

import (
	"android/soong/android"
	"strings"

	"github.com/google/blueprint/proptools"
)

func init() {
	android.RegisterModuleType("bootconfig", BootconfigModuleFactory)
	pctx.Import("android/soong/android")
}

type bootconfigProperty struct {
	// List of bootconfig parameters that will be written as a line separated list in the output
	// file.
	Boot_config []string
	// Path to the file that contains the list of bootconfig parameters. This will be appended
	// to the output file, after the entries in boot_config.
	Boot_config_file *string `android:"path"`
}

type BootconfigModule struct {
	android.ModuleBase

	properties bootconfigProperty
}

// bootconfig module generates the `vendor-bootconfig.img` file, which lists the bootconfig
// parameters and can be passed as a `--vendor_bootconfig` value in mkbootimg invocation.
func BootconfigModuleFactory() android.Module {
	module := &BootconfigModule{}
	module.AddProperties(&module.properties)
	android.InitAndroidArchModule(module, android.DeviceSupported, android.MultilibCommon)
	return module
}

func (m *BootconfigModule) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	bootConfig := m.properties.Boot_config
	bootConfigFileStr := proptools.String(m.properties.Boot_config_file)
	if len(bootConfig) == 0 && len(bootConfigFileStr) == 0 {
		return
	}

	var bootConfigFile android.Path
	if len(bootConfigFileStr) > 0 {
		bootConfigFile = android.PathForModuleSrc(ctx, bootConfigFileStr)
	}

	outputPath := android.PathForModuleOut(ctx, ctx.ModuleName(), "vendor-bootconfig.img")
	bootConfigOutput := android.PathForModuleOut(ctx, ctx.ModuleName(), "bootconfig.txt")
	android.WriteFileRule(ctx, bootConfigOutput, strings.Join(bootConfig, "\n"))

	bcFiles := android.Paths{bootConfigOutput}
	if bootConfigFile != nil {
		bcFiles = append(bcFiles, bootConfigFile)
	}
	ctx.Build(pctx, android.BuildParams{
		Rule:        android.Cat,
		Description: "concatenate bootconfig parameters",
		Inputs:      bcFiles,
		Output:      outputPath,
	})
	ctx.SetOutputFiles(android.Paths{outputPath}, "")
}
