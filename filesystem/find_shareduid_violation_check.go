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

// findSharedUIDViolationPartitionsToCheck is a hardcoded list of partitions to pass to
// the find_shareduid_violation tool
// TODO: should this include "odm"?
var findSharedUIDViolationPartitionsToCheck = []string{"system", "system_ext", "vendor", "product"}

func (a *androidDevice) findSharedUIDViolation(ctx android.ModuleContext) {
	fsInfoMap := a.getFsInfos(ctx)

	rule := android.NewRuleBuilder(pctx, ctx)
	cmd := rule.Command().BuiltTool("find_shareduid_violation")
	cmd.FlagWithInput("--aapt ", ctx.Config().HostToolPath(ctx, "aapt2"))
	outputFile := android.PathForModuleOut(ctx, "shareduid_violation_modules.json")

	// The find_shareduid_violation tool expects a path to $PRODUCT_OUT, and then relative
	// paths to partition staging directories to search for APKs.  The Soong partition staging
	// directories may not all be under the same directory, so give an empty path for
	// --product_out and then full paths for partition staging directories.
	cmd.Flag("--product_out=")

	for _, typ := range findSharedUIDViolationPartitionsToCheck {
		partitionStagingDir, partitionOutput, ok := partitionStagingPath(ctx, fsInfoMap, typ)
		if ok {
			cmd.FlagWithArg("--copy_out_"+typ+" ", partitionStagingDir.String())
			cmd.Implicit(partitionOutput)
		}
	}

	cmd.Text(" > ").Output(outputFile)

	rule.Build("find_shareduid_violation", "find shareduid violation")

	if !ctx.Config().KatiEnabled() && proptools.Bool(a.deviceProps.Main_device) {
		// Only create the phony and dist target for Soong-only builds.
		ctx.Phony("find_shareduid_violation_check", outputFile)
		ctx.DistForGoal("droidcore", outputFile)
	}
}
