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

package etc

import (
	"android/soong/android"

	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"
)

func init() {
	android.RegisterModuleType("avbpubkey", AvbpubkeyModuleFactory)
	pctx.HostBinToolVariable("avbtool", "avbtool")
}

type avbpubkeyProperty struct {
	Private_key *string `android:"path"`
}

type AvbpubkeyModule struct {
	android.ModuleBase

	properties avbpubkeyProperty

	outputPath  android.WritablePath
	installPath android.InstallPath
}

func AvbpubkeyModuleFactory() android.Module {
	module := &AvbpubkeyModule{}
	module.AddProperties(&module.properties)
	android.InitAndroidArchModule(module, android.DeviceSupported, android.MultilibFirst)
	return module
}

var avbPubKeyRule = pctx.AndroidStaticRule("avbpubkey",
	blueprint.RuleParams{
		Command: `${avbtool} extract_public_key --key ${in} --output ${out}.tmp` +
			` && ( if cmp -s ${out}.tmp ${out} ; then rm ${out}.tmp ; else mv ${out}.tmp ${out} ; fi )`,
		CommandDeps: []string{"${avbtool}"},
		Restat:      true,
		Description: "Extracting system_other avb key",
	})

func (m *AvbpubkeyModule) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	if !m.ProductSpecific() {
		ctx.ModuleErrorf("avbpubkey module type must set product_specific to true")
	}

	m.outputPath = android.PathForModuleOut(ctx, ctx.ModuleName(), "system_other.avbpubkey")

	ctx.Build(pctx, android.BuildParams{
		Rule:   avbPubKeyRule,
		Input:  android.PathForModuleSrc(ctx, proptools.String(m.properties.Private_key)),
		Output: m.outputPath,
	})

	m.installPath = android.PathForModuleInstall(ctx, "etc/security/avb")
	ctx.InstallFile(m.installPath, "system_other.avbpubkey", m.outputPath)
}

func (m *AvbpubkeyModule) AndroidMkEntries() []android.AndroidMkEntries {
	if m.IsSkipInstall() {
		return []android.AndroidMkEntries{}
	}

	return []android.AndroidMkEntries{
		{
			Class:      "ETC",
			OutputFile: android.OptionalPathForPath(m.outputPath),
		}}
}
