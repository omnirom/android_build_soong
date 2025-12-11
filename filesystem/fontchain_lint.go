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

// fontchainLint is part of checkbuild under build/make/core/tasks/fontchain_lint.mk
func (a *androidDevice) fontchainLint(ctx android.ModuleContext) {
	checkEmoji := "true"
	if proptools.Bool(a.deviceProps.Minimal_font_footprint) {
		checkEmoji = "false"
	}

	fsInfoMap := a.getFsInfos(ctx)
	systemPartitionStagingDir, partitionOutput, ok := partitionStagingPath(ctx, fsInfoMap, "system")
	if !ok {
		return
	}

	outputFile := android.PathForModuleOut(ctx, "fontchain_lint.timestamp")

	rule := android.NewRuleBuilder(pctx, ctx)
	rule.Command().Text("rm -f").Output(outputFile)
	rule.Command().BuiltTool("fontchain_linter").Text(systemPartitionStagingDir.String()).Text(checkEmoji).Input(android.PathForSource(ctx, "external/unicode")).Implicit(partitionOutput)
	rule.Command().Text("touch").Output(outputFile)

	rule.Build("running_fontchain_lint", "running fontchain lint")

	if !ctx.Config().KatiEnabled() && proptools.Bool(a.deviceProps.Main_device) {
		// Only create the phony and dist target for Soong-only builds.
		ctx.Phony("fontchain_lint", outputFile)
		ctx.CheckbuildFile(outputFile)
	}
}
