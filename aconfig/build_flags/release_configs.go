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
	"fmt"
	"path/filepath"

	"android/soong/android"

	"github.com/google/blueprint"
)

type ReleaseConfigContributionsProviderData struct {
	ContributionDir   android.SourcePath
	ContributionPaths android.Paths
}

var ReleaseConfigContributionsProviderKey = blueprint.NewProvider[ReleaseConfigContributionsProviderData]()

// Soong uses `release_config_contributions` modules to produce the
// `build_flags/all_release_config_contributions.*` artifacts, listing *all* of
// the directories in the source tree that contribute to each release config,
// whether or not they are actually used for the lunch product.
//
// This artifact helps flagging automation determine in which directory a flag
// should be placed by default.
type ReleaseConfigContributionsModule struct {
	android.ModuleBase
	android.DefaultableModuleBase

	// Properties for "release_config_contributions"
	properties struct {
		// The `release_configs/*.textproto` files provided by this
		// directory, relative to this Android.bp file
		Srcs []string `android:"path"`
	}
}

func ReleaseConfigContributionsFactory() android.Module {
	module := &ReleaseConfigContributionsModule{}

	android.InitAndroidModule(module)
	android.InitDefaultableModule(module)
	module.AddProperties(&module.properties)

	return module
}

func (module *ReleaseConfigContributionsModule) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	srcs := android.PathsForModuleSrc(ctx, module.properties.Srcs)
	if len(srcs) == 0 {
		return
	}
	contributionDir := filepath.Dir(filepath.Dir(srcs[0].String()))
	for _, file := range srcs {
		if filepath.Dir(filepath.Dir(file.String())) != contributionDir {
			ctx.ModuleErrorf("Cannot include %s with %s contributions", file, contributionDir)
		}
		if filepath.Base(filepath.Dir(file.String())) != "release_configs" || file.Ext() != ".textproto" {
			ctx.ModuleErrorf("Invalid contribution file %s", file)
		}
	}
	android.SetProvider(ctx, ReleaseConfigContributionsProviderKey, ReleaseConfigContributionsProviderData{
		ContributionDir:   android.PathForSource(ctx, contributionDir),
		ContributionPaths: srcs,
	})

}

// Soong provides release config information for the active release config via
// a `release_config` module.
//
// This module can be used by test modules that need to inspect release configs.
type ReleaseConfigModule struct {
	android.ModuleBase
	android.DefaultableModuleBase

	outputPath android.Path
}

func ReleaseConfigFactory() android.Module {
	module := &ReleaseConfigModule{}
	android.InitAndroidArchModule(module, android.HostAndDeviceSupported, android.MultilibCommon)
	return module
}

type ReleaseConfigProviderData struct {
	BuildFlagsProductJson   android.Path
	BuildFlagsSystemJson    android.Path
	BuildFlagsSystemExtJson android.Path
	BuildFlagsVendorJson    android.Path
}

var ReleaseConfigProviderKey = blueprint.NewProvider[ReleaseConfigProviderData]()

func (*ReleaseConfigModule) UseGenericConfig() bool {
	return false
}

type cpData struct {
	inFiles  []android.WritablePath
	outFiles android.Paths
}

func (c *cpData) AddCopy(ctx android.ModuleContext, product, prefix, suffix string) android.ModuleOutPath {
	i := android.PathForModuleOut(ctx, prefix+"-"+product+suffix)
	o := android.PathForModuleOut(ctx, prefix+suffix)
	c.inFiles = append(c.inFiles, i)
	c.outFiles = append(c.outFiles, o)
	ctx.Build(pctx, android.BuildParams{
		Rule:   android.CpIfChanged,
		Input:  i,
		Output: o,
	})
	return o
}

func (module *ReleaseConfigModule) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	if !ctx.Config().HasDeviceProduct() {
		return
	}

	product := ctx.Config().DeviceProduct()

	fileInfo := &cpData{}

	// The `release-config` command generates files which have ${TARGET_PRODUCT} in the name.
	// We rename them here to make life a little easier for consuming modules.
	providerData := ReleaseConfigProviderData{
		BuildFlagsProductJson:   fileInfo.AddCopy(ctx, product, "build_flags", "-product.json"),
		BuildFlagsSystemJson:    fileInfo.AddCopy(ctx, product, "build_flags", "-system.json"),
		BuildFlagsSystemExtJson: fileInfo.AddCopy(ctx, product, "build_flags", "-system_ext.json"),
		BuildFlagsVendorJson:    fileInfo.AddCopy(ctx, product, "build_flags", "-vendor.json"),
	}
	addCommonRules(ctx, releaseConfigRule, fileInfo, product)
	ctx.Phony("release_config_metadata", fileInfo.outFiles...)
	android.SetProvider(ctx, ReleaseConfigProviderKey, providerData)
}

func addCommonRules(ctx android.ModuleContext, rule blueprint.Rule, fileInfo *cpData, product string) {
	// The file `${OUT_DIR}/soong/release-config/maps_list-${TARGET_PRODUCT}.txt` has the list of
	// release_config_map.textproto files to use.
	argsPath := android.PathForOutput(ctx, "release-config", fmt.Sprintf("args-%s.txt", product))
	hashFile := android.PathForOutput(ctx, "release-config", fmt.Sprintf("files_used-%s.hash", product))
	outputDir := android.PathForModuleOut(ctx)

	ctx.Build(pctx, android.BuildParams{
		Rule:    rule,
		Inputs:  android.Paths{argsPath, hashFile},
		Outputs: fileInfo.inFiles,
		Args: map[string]string{
			"argsFile":  argsPath.String(),
			"product":   product,
			"moduleOut": outputDir.String(),
		},
	})

	ctx.Phony("droid", fileInfo.outFiles...)
	ctx.SetOutputFiles(fileInfo.outFiles, "")
}

// Soong provides release config information for all release configs via an
// `all_release_configs` module.
//
// This module can be used by test modules that need to inspect release configs.
type AllReleaseConfigsModule struct {
	android.ModuleBase
	android.DefaultableModuleBase

	// There are no extra properties for "all_release_configs".
	properties struct{}
}

func AllReleaseConfigsFactory() android.Module {
	module := &AllReleaseConfigsModule{}

	android.InitAndroidModule(module)
	android.InitDefaultableModule(module)
	module.AddProperties(&module.properties)

	return module
}

type AllReleaseConfigsProviderData struct {
	AllReleaseConfigsDb        android.Path
	AllReleaseConfigsTextproto android.Path
	AllReleaseConfigsJson      android.Path
	InheritanceGraphDot        android.Path
}

var AllReleaseConfigsProviderKey = blueprint.NewProvider[AllReleaseConfigsProviderData]()

func (*AllReleaseConfigsModule) UseGenericConfig() bool {
	return false
}

func (module *AllReleaseConfigsModule) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	if !ctx.Config().HasDeviceProduct() {
		return
	}

	product := ctx.Config().DeviceProduct()

	fileInfo := &cpData{}

	providerData := AllReleaseConfigsProviderData{
		AllReleaseConfigsDb:        fileInfo.AddCopy(ctx, product, "all_release_configs", ".pb"),
		AllReleaseConfigsTextproto: fileInfo.AddCopy(ctx, product, "all_release_configs", ".textproto"),
		AllReleaseConfigsJson:      fileInfo.AddCopy(ctx, product, "all_release_configs", ".json"),
		InheritanceGraphDot:        fileInfo.AddCopy(ctx, product, "inheritance_graph", ".dot"),
	}
	addCommonRules(ctx, allReleaseConfigsRule, fileInfo, product)
	ctx.Phony("all_release_configs", fileInfo.outFiles...)
	android.SetProvider(ctx, AllReleaseConfigsProviderKey, providerData)

	ctx.DistForGoalsWithFilename(buildFlagArtifactsDistGoals, providerData.AllReleaseConfigsDb, "build_flags/all_release_configs.pb")
	ctx.DistForGoalsWithFilename(buildFlagArtifactsDistGoals, providerData.AllReleaseConfigsTextproto, "build_flags/all_release_configs.textproto")
	ctx.DistForGoalsWithFilename(buildFlagArtifactsDistGoals, providerData.AllReleaseConfigsJson, "build_flags/all_release_configs.json")
	ctx.DistForGoalsWithFilename(buildFlagArtifactsDistGoals, providerData.InheritanceGraphDot,
		fmt.Sprintf("build_flags/inheritance_graph-%s.dot", ctx.Config().DeviceProduct()))
}
