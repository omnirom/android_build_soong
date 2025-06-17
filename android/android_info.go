// Copyright 2024 The Android Open Source Project
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
	"github.com/google/blueprint/proptools"
)

var (
	mergeAndRemoveComments = pctx.AndroidStaticRule("merge_and_remove_comments",
		blueprint.RuleParams{
			Command: "cat $in | grep -v '#' > $out",
		},
	)
	androidInfoTxtToProp = pctx.AndroidStaticRule("android_info_txt_to_prop",
		blueprint.RuleParams{
			Command: "grep 'require version-' $in | sed -e 's/require version-/ro.build.expect./g' > $out",
		},
	)
)

type androidInfoProperties struct {
	// Name of output file. Defaults to module name
	Stem *string

	// Paths of board-info.txt files.
	Board_info_files []string `android:"path"`

	// Name of bootloader board. If board_info_files is empty, `board={bootloader_board_name}` will
	// be printed to output. Ignored if board_info_files is not empty.
	Bootloader_board_name *string
}

type androidInfoModule struct {
	ModuleBase

	properties androidInfoProperties
}

func (p *androidInfoModule) GenerateAndroidBuildActions(ctx ModuleContext) {
	if len(p.properties.Board_info_files) > 0 && p.properties.Bootloader_board_name != nil {
		ctx.ModuleErrorf("Either Board_info_files or Bootloader_board_name should be set. Please remove one of them\n")
		return
	}
	androidInfoTxtName := proptools.StringDefault(p.properties.Stem, ctx.ModuleName()+".txt")
	androidInfoTxt := PathForModuleOut(ctx, androidInfoTxtName)
	androidInfoProp := androidInfoTxt.ReplaceExtension(ctx, "prop")

	if boardInfoFiles := PathsForModuleSrc(ctx, p.properties.Board_info_files); len(boardInfoFiles) > 0 {
		ctx.Build(pctx, BuildParams{
			Rule:   mergeAndRemoveComments,
			Inputs: boardInfoFiles,
			Output: androidInfoTxt,
		})
	} else if bootloaderBoardName := proptools.String(p.properties.Bootloader_board_name); bootloaderBoardName != "" {
		WriteFileRule(ctx, androidInfoTxt, "board="+bootloaderBoardName)
	} else {
		WriteFileRule(ctx, androidInfoTxt, "")
	}

	// Create android_info.prop
	ctx.Build(pctx, BuildParams{
		Rule:   androidInfoTxtToProp,
		Input:  androidInfoTxt,
		Output: androidInfoProp,
	})

	ctx.SetOutputFiles(Paths{androidInfoProp}, "")
	ctx.SetOutputFiles(Paths{androidInfoTxt}, ".txt")
}

// android_info module generate a file named android-info.txt that contains various information
// about the device we're building for.  This file is typically packaged up with everything else.
func AndroidInfoFactory() Module {
	module := &androidInfoModule{}
	module.AddProperties(&module.properties)
	InitAndroidArchModule(module, DeviceSupported, MultilibCommon)
	return module
}
