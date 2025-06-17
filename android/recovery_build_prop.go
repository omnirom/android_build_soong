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

package android

import "github.com/google/blueprint/proptools"

func init() {
	RegisterModuleType("recovery_build_prop", RecoveryBuildPropModuleFactory)
}

type recoveryBuildPropProperties struct {
	// Path to the system build.prop file
	System_build_prop *string `android:"path"`

	// Path to the vendor build.prop file
	Vendor_build_prop *string `android:"path"`

	// Path to the odm build.prop file
	Odm_build_prop *string `android:"path"`

	// Path to the product build.prop file
	Product_build_prop *string `android:"path"`

	// Path to the system_ext build.prop file
	System_ext_build_prop *string `android:"path"`
}

type recoveryBuildPropModule struct {
	ModuleBase
	properties recoveryBuildPropProperties

	outputFilePath ModuleOutPath

	installPath InstallPath
}

func RecoveryBuildPropModuleFactory() Module {
	module := &recoveryBuildPropModule{}
	module.AddProperties(&module.properties)
	InitAndroidArchModule(module, DeviceSupported, MultilibCommon)
	return module
}

// Overrides ctx.Module().InstallInRoot().
// recovery_build_prop module always installs in root so that the prop.default
// file is installed in recovery/root instead of recovery/root/system
func (r *recoveryBuildPropModule) InstallInRoot() bool {
	return true
}

func (r *recoveryBuildPropModule) appendRecoveryUIProperties(ctx ModuleContext, rule *RuleBuilder) {
	rule.Command().Text("echo '#' >>").Output(r.outputFilePath)
	rule.Command().Text("echo '# RECOVERY UI BUILD PROPERTIES' >>").Output(r.outputFilePath)
	rule.Command().Text("echo '#' >>").Output(r.outputFilePath)

	for propName, val := range ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse.PrivateRecoveryUiProperties {
		if len(val) > 0 {
			rule.Command().
				Textf("echo ro.recovery.ui.%s=%s >>", propName, val).
				Output(r.outputFilePath)
		}
	}
}

func (r *recoveryBuildPropModule) getBuildProps(ctx ModuleContext) Paths {
	var buildProps Paths
	for _, buildProp := range []*string{
		r.properties.System_build_prop,
		r.properties.Vendor_build_prop,
		r.properties.Odm_build_prop,
		r.properties.Product_build_prop,
		r.properties.System_ext_build_prop,
	} {
		if buildProp != nil {
			if buildPropPath := PathForModuleSrc(ctx, proptools.String(buildProp)); buildPropPath != nil {
				buildProps = append(buildProps, buildPropPath)
			}
		}
	}
	return buildProps
}

func (r *recoveryBuildPropModule) GenerateAndroidBuildActions(ctx ModuleContext) {
	if !r.InstallInRecovery() {
		ctx.ModuleErrorf("recovery_build_prop module must set `recovery` property to true")
	}
	r.outputFilePath = PathForModuleOut(ctx, ctx.ModuleName(), "prop.default")

	// Replicates the logic in https://cs.android.com/android/platform/superproject/main/+/main:build/make/core/Makefile;l=2733;drc=0585bb1bcf4c89065adaf709f48acc8b869fd3ce
	rule := NewRuleBuilder(pctx, ctx)
	rule.Command().Text("rm").FlagWithOutput("-f ", r.outputFilePath)
	rule.Command().Text("cat").
		Inputs(r.getBuildProps(ctx)).
		Text(">>").
		Output(r.outputFilePath)
	r.appendRecoveryUIProperties(ctx, rule)

	rule.Build(ctx.ModuleName(), "generating recovery prop.default")
	r.installPath = PathForModuleInstall(ctx)
	ctx.InstallFile(r.installPath, "prop.default", r.outputFilePath)
}
