// Copyright 2025 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package unbundled

import (
	"fmt"
	"slices"

	"github.com/google/blueprint"

	"android/soong/android"
	"android/soong/cc"
	"android/soong/filesystem"
	"android/soong/java"
)

var pctx = android.NewPackageContext("android/soong/unbundled")

func init() {
	pctx.Import("android/soong/android")
	registerUnbundledBuilder(android.InitRegistrationContext)
}

func registerUnbundledBuilder(ctx android.RegistrationContext) {
	ctx.RegisterModuleType("unbundled_builder", unbundledBuilderFactory)
}

func unbundledBuilderFactory() android.Module {
	m := &unbundledBuilder{}
	android.InitAndroidModule(m)
	return m
}

// unbundledBuilder handles building and disting certain artifacts for "unbundled builds".
// unbundled builds are builds that aren't for a specific device, such as when you just want to
// build an app or an apex.
type unbundledBuilder struct {
	android.ModuleBase
}

type unbundledDepTagType struct {
	blueprint.BaseDependencyTag
}

func (unbundledDepTagType) ExcludeFromVisibilityEnforcement() {}

var _ android.ExcludeFromVisibilityEnforcementTag = unbundledDepTagType{}

func (unbundledDepTagType) UsesUnbundledVariant() {}

var _ android.UsesUnbundledVariantDepTag = unbundledDepTagType{}

var unbundledDepTag = unbundledDepTagType{}

// We need to implement IsNativeCoverageNeeded so that in coverage builds we depend on the coverage
// variants of the unbundled apps. Only the coverage variants export symbols info.
func (p *unbundledBuilder) IsNativeCoverageNeeded(ctx cc.IsNativeCoverageNeededContext) bool {
	return ctx.DeviceConfig().NativeCoverageEnabled()
}

// Return "false" for UseGenericConfig() to read the DeviceProduct().
// Even though unbundledBuilder is not for a specific device, do we need "targetProductPrefix"?
func (p *unbundledBuilder) UseGenericConfig() bool {
	return false
}

var _ cc.UseCoverage = (*unbundledBuilder)(nil)

func (*unbundledBuilder) DepsMutator(ctx android.BottomUpMutatorContext) {
	apps := ctx.Config().UnbundledBuildApps()
	apps = slices.Clone(apps)
	slices.Sort(apps)

	for _, app := range apps {
		// Add a dependency on the app so we can get its providers later.
		// unbundledDepTag implements android.UsesUnbundledVariantDepTag, which causes the
		// os, arch, and sdk mutators to pick the most appropriate variants to use for unbundled
		// builds. unbundledBuilder itself also implements cc.UseCoverage, which forces coverage
		// variants of deps.
		ctx.AddDependency(ctx.Module(), unbundledDepTag, app)
	}
}

func (*unbundledBuilder) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	ctx.Module().HideFromMake()
	if ctx.ModuleDir() != "build/soong/unbundled" {
		ctx.ModuleErrorf("There can only be 1 unbundled_builder in build/soong/unbundled")
		return
	}
	if !ctx.Config().HasUnbundledBuildApps() {
		return
	}

	var appModules []android.ModuleProxy
	ctx.VisitDirectDepsProxyWithTag(unbundledDepTag, func(m android.ModuleProxy) {
		appModules = append(appModules, m)
	})

	targetProductPrefix := ""
	if ctx.Config().HasDeviceProduct() {
		targetProductPrefix = ctx.Config().DeviceProduct()
		if ctx.Config().BuildType() == "debug" {
			targetProductPrefix += "_debug"
		}
		targetProductPrefix += "-"
	}

	// Dist installed files, bundles, and AARs
	for _, app := range appModules {
		name := android.OtherModuleNameWithPossibleOverride(ctx, app)
		if bundleInfo, ok := android.OtherModuleProvider(ctx, app, java.BundleProvider); ok {
			ctx.DistForGoalWithFilename("apps_only", bundleInfo.Bundle, name+"-base.zip")
		}
		if info, ok := android.OtherModuleProvider(ctx, app, android.InstallFilesProvider); ok {
			for _, file := range info.InstallFiles {
				// The "apex" partition is a fake partition just to create files in
				// out/target/product/<device>/apex. Including it leads to duplicate rule errors as
				// there are multiple apexes with files installed at the same location within them.
				if file.Partition() == "apex" {
					continue
				}

				ctx.DistForGoal("apps_only", file)
			}
			if len(info.InstallFiles) == 0 {
				outputFiles, err := android.OutputFilesForModuleOrErr(ctx, app, "")
				// OutputFilesForModuleOrErr can error out when the module doesn't provide any
				// output files. We don't care about that, just dist files when they're provided.
				if err == nil {
					for _, file := range outputFiles {
						ctx.DistForGoal("apps_only", file)
					}
				}
				if aarInfo, ok := android.OtherModuleProvider(ctx, app, java.AARProvider); ok {
					ctx.DistForGoal("apps_only", aarInfo.Aar)
				}
			}
		}
	}

	// Dist apexkeys.txt
	apexKeysFile := android.PathForModuleOut(ctx, "apexkeys.txt")
	apexKeysRuleBuilder := android.NewRuleBuilder(pctx, ctx)
	apexKeysRuleBuilder.Command().Textf("rm -f %s && touch ", apexKeysFile.String()).Output(apexKeysFile)
	for _, app := range appModules {
		if info, ok := android.OtherModuleProvider(ctx, app, filesystem.ApexKeyPathInfoProvider); ok {
			apexKeysRuleBuilder.Command().Text("cat ").Input(info.ApexKeyPath).Text(" >> ").Output(apexKeysFile)
		}
	}
	apexKeysRuleBuilder.Build("unbundled_apexkeys.txt", "Unbundled apexkeys.txt")
	ctx.DistForGoal("apps_only", apexKeysFile)
	ctx.Phony("apexkeys.txt", apexKeysFile)

	// Dist symbols.zip
	symbolsZip := android.PathForOutput(ctx, "unbundled_singleton", targetProductPrefix+"symbols.zip")
	symbolsMapping := android.PathForOutput(ctx, "unbundled_singleton", targetProductPrefix+"symbols-mapping.textproto")
	android.BuildSymbolsZip(ctx, appModules, nil, symbolsZip, symbolsMapping)
	ctx.DistForGoalWithFilenameTag("apps_only", symbolsZip, symbolsZip.Base())
	ctx.DistForGoalWithFilenameTag("apps_only", symbolsMapping, symbolsMapping.Base())

	// Dist lint reports
	var reportFiles android.Paths
	for _, app := range appModules {
		name := android.OtherModuleNameWithPossibleOverride(ctx, app)
		if info, ok := android.OtherModuleProvider(ctx, app, java.ModuleLintReportZipsProvider); ok {
			reports := info.AllReports()
			for _, report := range reports {
				ctx.DistForGoalWithFilename("lint-check", report, fmt.Sprintf("%s-%s", name, report.Base()))
			}
			reportFiles = append(reportFiles, reports...)
		}
	}
	ctx.Phony("lint-check", reportFiles...)

	// Dist proguard zips
	proguardZips := java.BuildProguardZips(ctx, appModules)
	ctx.DistForGoalWithFilenameTag("apps_only", proguardZips.DictZip, targetProductPrefix+proguardZips.DictZip.Base())
	ctx.DistForGoalWithFilenameTag("apps_only", proguardZips.DictMapping, targetProductPrefix+proguardZips.DictMapping.Base())
	ctx.DistForGoalWithFilenameTag("apps_only", proguardZips.UsageZip, targetProductPrefix+proguardZips.UsageZip.Base())

	// Dist jacoco report jar
	if ctx.Config().IsEnvTrue("EMMA_INSTRUMENT") {
		jacocoZip := android.PathForModuleOut(ctx, "jacoco-report-classes-all.jar")
		java.BuildJacocoZipWithPotentialDeviceTests(ctx, appModules, jacocoZip)
		ctx.DistForGoal("apps_only", jacocoZip)
	}

	// Dist sboms
	for _, app := range appModules {
		android.BuildUnbundledSbom(ctx, app)
	}

	ctx.DistForGoal("apps_only", java.ApkCertsFile(ctx))
}
