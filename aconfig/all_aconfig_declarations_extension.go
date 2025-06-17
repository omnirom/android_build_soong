// Copyright 2025 Google Inc. All rights reserved.
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
	"android/soong/android"
	"path"

	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"
)

func AllAconfigDeclarationsExtensionFactory() android.Module {
	module := &allAconfigDeclarationsExtension{}
	module.AddProperties(&module.properties)
	android.InitAndroidArchModule(module, android.DeviceSupported, android.MultilibCommon)
	return module
}

type allAconfigDeclarationsExtensionProperties struct {
	// all_aconfig_declarations module that this module extends. Defaults to
	// all_aconfig_declarations.
	Base *string

	// Directory where the dist artifact should be placed in.
	Dist_dir *string

	ApiSurfaceContributorProperties
}

type allAconfigDeclarationsExtension struct {
	android.ModuleBase

	properties allAconfigDeclarationsExtensionProperties

	finalizedFlags android.ModuleOutPath
}

type allAconfigDeclarationsDependencyTagStruct struct {
	blueprint.BaseDependencyTag
}

var allAconfigDeclarationsDependencyTag allAconfigDeclarationsDependencyTagStruct

func (ext *allAconfigDeclarationsExtension) DepsMutator(ctx android.BottomUpMutatorContext) {
	ctx.AddDependency(ctx.Module(), allAconfigDeclarationsDependencyTag, proptools.StringDefault(ext.properties.Base, "all_aconfig_declarations"))
}

func (ext *allAconfigDeclarationsExtension) GenerateAndroidBuildActions(ctx android.ModuleContext) {

	var parsedFlagsFile android.Path
	ctx.VisitDirectDepsProxyWithTag(allAconfigDeclarationsDependencyTag, func(proxy android.ModuleProxy) {
		if info, ok := android.OtherModuleProvider(ctx, proxy, allAconfigDeclarationsInfoProvider); ok {
			parsedFlagsFile = info.parsedFlagsFile
		} else {
			ctx.PropertyErrorf("base", "base must provide allAconfigDeclarationsInfo")
		}
	})

	ext.finalizedFlags = android.PathForModuleOut(ctx, "finalized-flags.txt")

	GenerateFinalizedFlagsForApiSurface(ctx,
		ext.finalizedFlags,
		parsedFlagsFile,
		ext.properties.ApiSurfaceContributorProperties,
	)

	ctx.Phony(ctx.ModuleName(), ext.finalizedFlags)

	ctx.DistForGoalWithFilename("sdk", ext.finalizedFlags, path.Join(proptools.String(ext.properties.Dist_dir), "finalized-flags.txt"))

	// This module must not set any provider or call `ctx.SetOutputFiles`!
	// This module is only used to depend on the singleton module all_aconfig_declarations and
	// generate the custom finalized-flags.txt file in dist builds, and should not be depended
	// by other modules.
}
