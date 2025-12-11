// Copyright (C) 2025 The Android Open Source Project
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
	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"

	"android/soong/android"
)

type prebuiltRadioImg struct {
	android.ModuleBase
	properties PrebuiltRadioImgProperties
}

type PrebuiltRadioImgProperties struct {
	Src *string `android:"path"`
	// List of OTA updatable partitions of radio.img.
	// These will be unpacked from radio.img and added to the list
	// of partitions to be updated.
	Ab_ota_partitions []string

	// Tool for unpacking radio.img
	Unpack_tool *string `android:"path"`
}

type radioInfo struct {
	image android.Path
}

var radioInfoProvider = blueprint.NewProvider[radioInfo]()

// TODO(soong-team): This module should be registered with the name
// `prebuilt_radio_image`. If not, update the property error in
// [androidDevice.checkRadioVersion]. Remove this TODO once the module type is
// registered.
func PrebuiltRadioImgFactory() android.Module {
	module := &prebuiltRadioImg{}
	android.InitAndroidArchModule(module, android.DeviceSupported, android.MultilibFirst)
	module.AddProperties(&module.properties)
	return module
}

func (p *prebuiltRadioImg) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	var radioFiles android.Paths
	input := android.PathForModuleSrc(ctx, proptools.String(p.properties.Src))
	radioFiles = append(radioFiles, input)
	radioFiles = append(radioFiles, p.partitionFiles(ctx)...)

	ctx.SetOutputFiles(radioFiles, "")

	android.SetProvider(ctx, radioInfoProvider, radioInfo{
		image: input,
	})
}

// Unpack a partition from a radio.img image and add them to
// the list of partitions to be updated.
func (p *prebuiltRadioImg) partitionFiles(ctx android.ModuleContext) android.Paths {
	if len(p.properties.Ab_ota_partitions) == 0 {
		return nil
	}
	var radioFiles android.Paths
	unpackedDir := android.PathForModuleOut(ctx, "unpack_radio")
	builder := android.NewRuleBuilder(pctx, ctx).Sbox(
		unpackedDir,
		android.PathForModuleOut(ctx, "unpack_radio.textproto"),
	)
	input := android.PathForModuleSrc(ctx, proptools.String(p.properties.Src))
	for _, partition := range p.properties.Ab_ota_partitions {
		unpackedImg := unpackedDir.Join(ctx, partition+".img")
		cmd := builder.Command()
		cmd.
			Input(android.PathForModuleSrc(ctx, proptools.String(p.properties.Unpack_tool))).
			Textf(" --output-dir=%s ", cmd.PathForOutput(unpackedDir)).
			Input(input).
			Text(" " + partition).
			ImplicitOutput(unpackedImg)

		radioFiles = append(radioFiles, unpackedImg)
	}
	builder.Build("unpack_radio", "unpack_radio")
	return radioFiles
}
