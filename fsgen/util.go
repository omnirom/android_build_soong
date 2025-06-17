// Copyright (C) 2024 The Android Open Source Project
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

package fsgen

import (
	"android/soong/android"
	"android/soong/filesystem"
	"fmt"
	"strconv"
	"strings"
)

// Returns the appropriate dpi for recovery common resources selection. Replicates the logic in
// https://cs.android.com/android/platform/superproject/main/+/main:build/make/core/Makefile;l=2536;drc=a6af369e71ded123734523ea640b97b70a557cb9
func getDpi(ctx android.LoadHookContext) string {
	recoveryDensity := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse.TargetScreenDensity
	if len(recoveryDensity) == 0 {
		aaptPreferredConfig := ctx.Config().ProductAAPTPreferredConfig()
		if len(aaptPreferredConfig) > 0 {
			recoveryDensity = aaptPreferredConfig
		} else {
			recoveryDensity = "mdpi"
		}
	}
	if !android.InList(recoveryDensity, []string{"xxxhdpi", "xxhdpi", "xhdpi", "hdpi", "mdpi"}) {
		recoveryDensity = strings.TrimSuffix(recoveryDensity, "dpi")
		dpiInt, err := strconv.ParseInt(recoveryDensity, 10, 64)
		if err != nil {
			panic(fmt.Sprintf("Error in parsing recoveryDensity: %s", err.Error()))
		}
		if dpiInt >= 560 {
			recoveryDensity = "xxxhdpi"
		} else if dpiInt >= 400 {
			recoveryDensity = "xxhdpi"
		} else if dpiInt >= 280 {
			recoveryDensity = "xhdpi"
		} else if dpiInt >= 200 {
			recoveryDensity = "hdpi"
		} else {
			recoveryDensity = "mdpi"
		}
	}

	if p := android.ExistentPathForSource(ctx, fmt.Sprintf("bootable/recovery/res-%s", recoveryDensity)); !p.Valid() {
		recoveryDensity = "xhdpi"
	}

	return recoveryDensity
}

// Returns the name of the appropriate prebuilt module for installing font.png file.
// https://cs.android.com/android/platform/superproject/main/+/main:build/make/core/Makefile;l=2536;drc=a6af369e71ded123734523ea640b97b70a557cb9
func getRecoveryFontModuleName(ctx android.LoadHookContext) string {
	if android.InList(getDpi(ctx), []string{"xxxhdpi", "xxhdpi", "xhdpi"}) {
		return "recovery-fonts-18"
	}
	return "recovery-fonts-12"
}

// Returns a new list of symlinks with prefix added to the dest directory for all symlinks
func symlinksWithNamePrefix(symlinks []filesystem.SymlinkDefinition, prefix string) []filesystem.SymlinkDefinition {
	ret := make([]filesystem.SymlinkDefinition, len(symlinks))
	for i, symlink := range symlinks {
		ret[i] = symlink.CopyWithNamePrefix(prefix)
	}
	return ret
}
