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
	"github.com/google/blueprint/proptools"

	"android/soong/android"
)

type prebuiltTzsw struct {
	android.ModuleBase
	properties       PrebuiltTzswProperties
	vbmetaPartitions vbmetaPartitionInfos
}

type PrebuiltTzswProperties struct {
	Src *string `android:"path"`
}

func PrebuiltTzswFactory() android.Module {
	module := &prebuiltTzsw{}
	android.InitAndroidArchModule(module, android.DeviceSupported, android.MultilibFirst)
	module.AddProperties(&module.properties)
	return module
}

func (p *prebuiltTzsw) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	if p.properties.Src == nil {
		ctx.PropertyErrorf("src", "Source cannot be empty")
	}
	src := android.PathForModuleSrc(ctx, proptools.String(p.properties.Src))
	srcWithAvb := p.avbAddHash(ctx, src)
	ctx.SetOutputFiles([]android.Path{src, srcWithAvb}, "")
	android.SetProvider(ctx, vbmetaPartitionProvider, vbmetaPartitionInfo{
		Name:   "tzsw",
		Output: srcWithAvb,
	})
}

func (p *prebuiltTzsw) avbAddHash(ctx android.ModuleContext, src android.Path) android.Path {
	vbmetaIntermediates := android.PathForModuleOut(ctx, "vbmeta")
	builder := android.NewRuleBuilder(pctx, ctx).Sbox(
		vbmetaIntermediates,
		android.PathForModuleOut(ctx, "vbmeta.textproto"),
	)
	output := vbmetaIntermediates.Join(ctx, "tzsw_vbfooted.img")
	builder.Command().Text("cp").Input(src).Output(output)

	cmd := builder.Command()
	cmd.BuiltTool("avbtool").
		Text("add_hash_footer").
		FlagWithOutput("--image ", output).
		FlagWithArg("--partition_name ", "tzsw").
		Textf(`--salt $(sha256sum "%s" "%s" | cut -d " " -f 1 | tr -d '\n')`, cmd.PathForInput(ctx.Config().BuildNumberFile(ctx)), cmd.PathForInput(ctx.Config().BuildDateFile(ctx))).
		OrderOnly(ctx.Config().BuildNumberFile(ctx)).OrderOnly(ctx.Config().BuildDateFile(ctx))

	vbmetaPaddingSize := 64*1024 + 4096
	cmd.Textf("--partition_size $(( %d + (( $(stat -c %%s %s) - 1) / 4096 + 1) * 4096 ))", vbmetaPaddingSize, cmd.PathForInput(src))

	builder.Build("vbmeta", "vbmeta")
	return output
}

func (_ *prebuiltTzsw) UseGenericConfig() bool {
	return false
}
