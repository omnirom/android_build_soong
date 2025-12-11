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

type bootloaderInfo struct {
	bootloaderImg android.Path
}

var bootloaderInfoProvider = blueprint.NewProvider[bootloaderInfo]()

type prebuiltBootloader struct {
	android.ModuleBase
	properties       PrebuiltBootloaderProperties
	vbmetaPartitions vbmetaPartitionInfos
}

type PrebuiltBootloaderProperties struct {
	Src *string `android:"path"`
	// List of OTA updatable partitions of bootloader.img.
	// These will be unpacked from bootloader.img and added to the list
	// of partitions to be updated.
	Ab_ota_partitions []string

	// Tool for unpacking bootloader.img
	Unpack_tool *string `android:"path"`
}

// TODO(soong-team): This module should be registered with the name
// `prebuilt_bootloader`. If not, update the property error in
// [androidDevice.checkRadioVersion]. Remove this TODO once the module type is
// registered.
func PrebuiltBootloaderFactory() android.Module {
	module := &prebuiltBootloader{}
	android.InitAndroidArchModule(module, android.DeviceSupported, android.MultilibFirst)
	module.AddProperties(&module.properties)
	return module
}

func (p *prebuiltBootloader) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	if p.properties.Src == nil {
		ctx.PropertyErrorf("src", "Source cannot be empty")
	}
	bootloader := android.PathForModuleSrc(ctx, proptools.String(p.properties.Src))
	bootloaderFiles := append(android.Paths{}, bootloader)
	bootloaderFiles = append(bootloaderFiles, p.partitionFilesBootloader(ctx)...)

	ctx.SetOutputFiles(bootloaderFiles, "")
	android.SetProvider(ctx, vbmetaPartitionsProvider, p.vbmetaPartitions)
	android.SetProvider(ctx, bootloaderInfoProvider, bootloaderInfo{
		bootloaderImg: bootloader,
	})
}

// Unpack a partition from a bootloader.img and add them to
// the list of partitions to be updated.
func (p *prebuiltBootloader) partitionFilesBootloader(ctx android.ModuleContext) android.Paths {
	bootloader := android.PathForModuleSrc(ctx, proptools.String(p.properties.Src))
	if len(p.properties.Ab_ota_partitions) == 0 {
		return nil
	}
	var files android.Paths
	unpackedDir := android.PathForModuleOut(ctx, "unpack_bootloader")
	builder := android.NewRuleBuilder(pctx, ctx).Sbox(
		unpackedDir,
		android.PathForModuleOut(ctx, "unpack_bootloader.textproto"),
	)
	for _, partition := range p.properties.Ab_ota_partitions {
		unpackedImg := unpackedDir.Join(ctx, partition+".img")
		cmd := builder.Command()
		cmd.Input(android.PathForModuleSrc(ctx, proptools.String(p.properties.Unpack_tool))).
			Flag(" unpack ").
			Flag("-o ").
			Textf("%s ", cmd.PathForOutput(unpackedDir)).
			Input(bootloader).
			Text(" " + partition).
			ImplicitOutput(unpackedImg)

		files = append(files, unpackedImg)
		// avb signed
		signed := p.avbAddHash(ctx, builder, partition, unpackedImg)
		files = append(files, signed)
		p.vbmetaPartitions = append(p.vbmetaPartitions, vbmetaPartitionInfo{
			Name:                     partition,
			Output:                   signed,
			AbOtaBootloaderPartition: true,
		})
	}
	builder.Build("unpack_bootloader", "unpack_bootloader")
	return files
}

func (p *prebuiltBootloader) avbAddHash(ctx android.ModuleContext, builder *android.RuleBuilder, partitionName string, unpackedPartition android.OutputPath) android.Path {
	output := unpackedPartition.InSameDir(ctx, partitionName+"_vbfooted.img")
	builder.Command().Text("cp").Input(unpackedPartition).Output(output)

	cmd := builder.Command()
	cmd.BuiltTool("avbtool").
		Text("add_hash_footer").
		FlagWithOutput("--image ", output).
		FlagWithArg("--partition_name ", partitionName).
		Textf(`--salt $(sha256sum "%s" "%s" | cut -d " " -f 1 | tr -d '\n')`, cmd.PathForInput(ctx.Config().BuildNumberFile(ctx)), cmd.PathForInput(ctx.Config().BuildDateFile(ctx))).
		OrderOnly(ctx.Config().BuildNumberFile(ctx)).OrderOnly(ctx.Config().BuildDateFile(ctx))

	vbmetaPaddingSize := 64*1024 + 4096
	cmd.Textf("--partition_size $(( %d + (( $(stat -c %%s %s) - 1) / 4096 + 1) * 4096 ))", vbmetaPaddingSize, cmd.PathForOutput(unpackedPartition))

	return output
}
