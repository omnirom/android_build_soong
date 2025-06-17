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

import "github.com/google/blueprint"

type vintfFragmentProperties struct {
	// Vintf fragment XML file.
	Src string `android:"path"`
}

type VintfFragmentModule struct {
	ModuleBase
	ApexModuleBase

	properties vintfFragmentProperties

	installDirPath InstallPath
	outputFilePath Path
}

func init() {
	registerVintfFragmentComponents(InitRegistrationContext)
}

func registerVintfFragmentComponents(ctx RegistrationContext) {
	ctx.RegisterModuleType("vintf_fragment", vintfLibraryFactory)
}

type VintfFragmentInfo struct {
	OutputFile Path
}

var VintfFragmentInfoProvider = blueprint.NewProvider[VintfFragmentInfo]()

// vintf_fragment module processes vintf fragment file and installs under etc/vintf/manifest.
// Vintf fragment files formerly listed in vintf_fragment property would be transformed into
// this module type.
func vintfLibraryFactory() Module {
	m := &VintfFragmentModule{}
	m.AddProperties(
		&m.properties,
	)
	InitAndroidArchModule(m, DeviceSupported, MultilibCommon)

	return m
}

func (m *VintfFragmentModule) GenerateAndroidBuildActions(ctx ModuleContext) {
	builder := NewRuleBuilder(pctx, ctx)
	srcVintfFragment := PathForModuleSrc(ctx, m.properties.Src)
	processedVintfFragment := PathForModuleOut(ctx, srcVintfFragment.Base())

	// Process vintf fragment source file with assemble_vintf tool
	builder.Command().
		Flag("VINTF_IGNORE_TARGET_FCM_VERSION=true").
		BuiltTool("assemble_vintf").
		FlagWithInput("-i ", srcVintfFragment).
		FlagWithOutput("-o ", processedVintfFragment)

	builder.Build("assemble_vintf", "Process vintf fragment "+processedVintfFragment.String())

	m.installDirPath = PathForModuleInstall(ctx, "etc", "vintf", "manifest")
	m.outputFilePath = processedVintfFragment

	ctx.InstallFile(m.installDirPath, processedVintfFragment.Base(), processedVintfFragment)

	SetProvider(ctx, VintfFragmentInfoProvider, VintfFragmentInfo{
		OutputFile: m.OutputFile(),
	})
}

func (m *VintfFragmentModule) OutputFile() Path {
	return m.outputFilePath
}

// Make this module visible to AndroidMK so it can be referenced from modules defined from Android.mk files
func (m *VintfFragmentModule) AndroidMkEntries() []AndroidMkEntries {
	return []AndroidMkEntries{{
		Class:      "ETC",
		OutputFile: OptionalPathForPath(m.outputFilePath),
		ExtraEntries: []AndroidMkExtraEntriesFunc{
			func(ctx AndroidMkExtraEntriesContext, entries *AndroidMkEntries) {
				entries.SetString("LOCAL_MODULE_PATH", m.installDirPath.String())
				entries.SetString("LOCAL_INSTALLED_MODULE_STEM", m.outputFilePath.Base())
			},
		},
	}}
}

var _ ApexModule = (*VintfFragmentModule)(nil)

// Implements android.ApexModule
func (m *VintfFragmentModule) MinSdkVersionSupported(ctx BaseModuleContext) ApiLevel {
	return MinApiLevel
}
