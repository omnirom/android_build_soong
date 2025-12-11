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

package build_flags

import (
	"android/soong/android"
)

// A singleton module that collects all of the build flags declared in the
// tree into a single combined file for export to the external flag setting
// server (inside Google it's Gantry).
//
// Note that this is ALL build_declarations modules present in the tree, not just
// ones that are relevant to the product currently being built, so that that infra
// doesn't need to pull from multiple builds and merge them.
func AllBuildFlagDeclarationsFactory() android.Singleton {
	return &allBuildFlagDeclarationsSingleton{}
}

type allBuildFlagDeclarationsSingleton struct {
	flagsBinaryProtoPath   android.OutputPath
	flagsTextProtoPath     android.OutputPath
	configsBinaryProtoPath android.OutputPath
	configsTextProtoPath   android.OutputPath
}

var buildFlagArtifactsDistGoals = []string{
	"docs", "droid", "sdk", "release_config_metadata", "gms",
}

func (this *allBuildFlagDeclarationsSingleton) GenerateBuildActions(ctx android.SingletonContext) {
	// Find all of the build_flag_declarations modules
	var flagsFiles android.Paths
	// Find all of the release_config_contribution modules
	var contributionDirs android.Paths
	var contributionPaths android.Paths
	ctx.VisitAllModuleProxies(func(module android.ModuleProxy) {
		decl, ok := android.OtherModuleProvider(ctx, module, BuildFlagDeclarationsProviderKey)
		if ok {
			flagsFiles = append(flagsFiles, decl.IntermediateCacheOutputPath)
		}

		contrib, ok := android.OtherModuleProvider(ctx, module, ReleaseConfigContributionsProviderKey)
		if ok {
			contributionDirs = append(contributionDirs, contrib.ContributionDir)
			contributionPaths = append(contributionPaths, contrib.ContributionPaths...)
		}
	})

	basePath := android.PathForIntermediates(ctx, "release_configs")

	// Generate build action for build_flag (binary proto output)
	this.flagsBinaryProtoPath = basePath.Join(ctx, "all_build_flag_declarations.pb")
	ctx.Build(pctx, android.BuildParams{
		Rule:        allDeclarationsRule,
		Inputs:      flagsFiles,
		Output:      this.flagsBinaryProtoPath,
		Description: "all_build_flag_declarations",
		Args: map[string]string{
			"intermediates": android.JoinPathsWithPrefix(flagsFiles, "--intermediate "),
		},
	})
	ctx.Phony("all_build_flag_declarations", this.flagsBinaryProtoPath)

	// Generate build action for build_flag (text proto output)
	this.flagsTextProtoPath = basePath.Join(ctx, "all_build_flag_declarations.textproto")
	ctx.Build(pctx, android.BuildParams{
		Rule:        allDeclarationsRuleTextProto,
		Input:       this.flagsBinaryProtoPath,
		Output:      this.flagsTextProtoPath,
		Description: "all_build_flag_declarations_textproto",
	})
	ctx.Phony("all_build_flag_declarations_textproto", this.flagsTextProtoPath)

	// Generate build action for release_configs (binary proto output)
	this.configsBinaryProtoPath = basePath.Join(ctx, "all_release_config_contributions.pb")
	ctx.Build(pctx, android.BuildParams{
		Rule:        allReleaseConfigContributionsRule,
		Inputs:      contributionPaths,
		Output:      this.configsBinaryProtoPath,
		Description: "all_release_config_contributions",
		Args: map[string]string{
			"dirs":   android.JoinPathsWithPrefix(contributionDirs, "--dir "),
			"format": "pb",
		},
	})
	ctx.Phony("all_release_config_contributions", this.configsBinaryProtoPath)

	this.configsTextProtoPath = basePath.Join(ctx, "all_release_config_contributions.textproto")
	ctx.Build(pctx, android.BuildParams{
		Rule:        allReleaseConfigContributionsRule,
		Inputs:      contributionPaths,
		Output:      this.configsTextProtoPath,
		Description: "all_release_config_contributions_textproto",
		Args: map[string]string{
			"dirs":   android.JoinPathsWithPrefix(contributionDirs, "--dir "),
			"format": "textproto",
		},
	})
	ctx.Phony("all_release_config_contributions_textproto", this.configsTextProtoPath)

	// Validator to ensure that there are no duplicate flag declarations.
	// The Validator timestamp file.
	mapsListPath := basePath.Join(ctx, "release_config_map.list")
	duplicatesPath := basePath.Join(ctx, "duplicate_allowlist.list")
	validatorPath := basePath.Join(ctx, "release_config_map.timestamp")

	// The file containing the list of all `release_config_map.textproto` files in the source tree.
	ctx.Build(pctx, android.BuildParams{
		Rule:       android.CpIfChanged,
		Input:      android.PathForArbitraryOutput(ctx, ".module_paths", "release_config_map.list"),
		Output:     mapsListPath,
		Validation: validatorPath,
	})
	ctx.Build(pctx, android.BuildParams{
		Rule:       android.CpIfChanged,
		Input:      android.PathForArbitraryOutput(ctx, ".module_paths", "duplicate_allowlist.list"),
		Output:     duplicatesPath,
		Validation: validatorPath,
	})
	ctx.Build(pctx, android.BuildParams{
		Rule:      flagDeclarationsValidationRule,
		Input:     mapsListPath,
		Implicits: append(android.Paths{duplicatesPath}, flagsFiles...),
		Output:    validatorPath,
	})
	// Make sure that this is at least built on CI machines.
	ctx.Phony("droid", mapsListPath)

	// Add a simple target for ci/build_metadata to use.
	ctx.Phony("release_config_metadata",
		this.flagsBinaryProtoPath,
		this.flagsTextProtoPath,
		this.configsBinaryProtoPath,
		this.configsTextProtoPath,
		mapsListPath,
	)

	ctx.DistForGoal("droid", this.flagsBinaryProtoPath)

	ctx.DistForGoalsWithFilename(buildFlagArtifactsDistGoals, this.flagsBinaryProtoPath, "build_flags/all_flags.pb")
	ctx.DistForGoalsWithFilename(buildFlagArtifactsDistGoals, this.flagsTextProtoPath, "build_flags/all_flags.textproto")
	ctx.DistForGoalsWithFilename(buildFlagArtifactsDistGoals, this.configsBinaryProtoPath, "build_flags/all_release_config_contributions.pb")
	ctx.DistForGoalsWithFilename(buildFlagArtifactsDistGoals, this.configsTextProtoPath, "build_flags/all_release_config_contributions.textproto")

}
