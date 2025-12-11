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
	"maps"

	"github.com/google/blueprint/proptools"

	"android/soong/android"
)

func (a *androidDevice) checkPartitionSizes(ctx android.ModuleContext) {
	if proptools.String(a.partitionProps.Super_partition_name) == "" {
		return
	}

	var partitionsToCheck map[string]FilesystemInfo
	superPartition := ctx.GetDirectDepProxyWithTag(*a.partitionProps.Super_partition_name, superPartitionDepTag)
	if info, ok := android.OtherModuleProvider(ctx, superPartition, SuperImageProvider); ok {
		partitionsToCheck = maps.Clone(info.SubImageInfo)
	} else {
		ctx.ModuleErrorf("Super partition %s does not set SuperImageProvider\n", superPartition.Name())
		return
	}

	miscInfo := android.PathForModuleOut(ctx, "check_partition_sizes_misc_info.txt")
	outputFile := android.PathForModuleOut(ctx, "check_all_partition_sizes.log")

	rule := android.NewRuleBuilder(pctx, ctx)
	rule.Command().Text("cp -f").Input(a.miscInfo).Output(miscInfo)
	for _, partition := range android.SortedKeys(partitionsToCheck) {
		fsInfo := partitionsToCheck[partition]
		rule.Command().Textf("echo %s_image=%s >> %s", partition, fsInfo.Output, miscInfo).
			Implicit(fsInfo.Output)
	}

	rule.Command().
		BuiltTool("check_partition_sizes").
		FlagWithOutput("--logfile ", outputFile).
		Flag("-v").
		Input(miscInfo)

	rule.Build("check_all_partition_sizes", "Check partition sizes")

	if !ctx.Config().KatiEnabled() && proptools.Bool(a.deviceProps.Main_device) {
		ctx.Phony("check-all-partition-sizes", outputFile)
		ctx.Phony("droid_targets", outputFile)
		ctx.DistForGoal("droid_targets", outputFile)
	}
}
