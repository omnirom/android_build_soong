// Copyright 2023 Google Inc. All rights reserved.
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

package aconfig

import (
	"strconv"

	"android/soong/android"
)

func ExportedJavaDeclarationsLibraryFactory() android.Singleton {
	return &exportedJavaDeclarationsLibrarySingleton{}
}

type exportedJavaDeclarationsLibrarySingleton struct {
	intermediatePath android.OutputPath
}

func (this *exportedJavaDeclarationsLibrarySingleton) GenerateBuildActions(ctx android.SingletonContext) {
	// Find all of the aconfig_declarations modules
	var cacheFiles android.Paths
	ctx.VisitAllModuleProxies(func(module android.ModuleProxy) {
		decl, ok := android.OtherModuleProvider(ctx, module, android.AconfigDeclarationsProviderKey)
		if !ok {
			return
		}
		cacheFiles = append(cacheFiles, decl.IntermediateCacheOutputPath)
	})

	var newExported bool
	if useNewExported, ok := ctx.Config().GetBuildFlag("RELEASE_ACONFIG_NEW_EXPORTED"); ok {
		newExported = useNewExported == "true"
	}

	var newStorage bool
	if useNewStorage, ok := ctx.Config().GetBuildFlag("RELEASE_READ_FROM_NEW_STORAGE"); ok {
		newStorage = useNewStorage == "true"
	}

	// Generate build action for aconfig
	this.intermediatePath = android.PathForIntermediates(ctx, "exported_java_aconfig_library.jar")
	ctx.Build(pctx, android.BuildParams{
		Rule:        exportedJavaRule,
		Inputs:      cacheFiles,
		Output:      this.intermediatePath,
		Description: "exported_java_aconfig_library",
		Args: map[string]string{
			"cache_files":      android.JoinPathsWithPrefix(cacheFiles, " "),
			"use_new_storage":  strconv.FormatBool(newStorage),
			"use_new_exported": strconv.FormatBool(newExported),
			"check_api_level":  strconv.FormatBool(ctx.Config().ReleaseAconfigCheckApiLevel()),
		},
	})
	ctx.Phony("exported_java_aconfig_library", this.intermediatePath)
	ctx.DistForGoalWithFilename("sdk", this.intermediatePath, "android-flags.jar")
}
