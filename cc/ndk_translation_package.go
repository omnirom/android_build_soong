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

package cc

import (
	"fmt"

	"android/soong/android"
	"path/filepath"
	"strings"

	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"
)

func init() {
	android.RegisterModuleType("ndk_translation_package", NdkTranslationPackageFactory)
}

func NdkTranslationPackageFactory() android.Module {
	module := &ndkTranslationPackage{}
	module.AddProperties(&module.properties)
	android.InitAndroidMultiTargetsArchModule(module, android.DeviceSupported, android.MultilibCommon)
	return module
}

type ndkTranslationPackage struct {
	android.ModuleBase
	properties ndkTranslationPackageProperties

	output android.Path
}

type ndkTranslationPackageProperties struct {
	// Dependencies with native bridge variants that should be packaged.
	// (e.g. arm and arm64 on an x86_64 device)
	Native_bridge_deps proptools.Configurable[[]string]
	// Non-native bridge variants that should be packaged.
	// (e.g. x86 and x86_64 on an x86_64 device)
	Device_both_deps []string
	// Non-native bridge variants with lib64 that should be packaged.
	// (e.g. x86_64 on an x86_64 device)
	Device_64_deps []string
	// Non-native bridge variants with lib32 that should be packaged.
	// (e.g. x86 on an x86_64 device)
	Device_32_deps []string
	// Non-native bridge variants whose first variant should be packaged.
	Device_first_deps []string
	// Non-native bridge variants whose first variant should be packaged,
	// but always into lib/, bin/ directories.
	Device_first_to_32_deps []string
	// Non-native bridge variants that should _not_ be packaged, but
	// used as inputs to generate Android.mk and product.mk
	Device_both_extra_allowed_deps []string
	Device_32_extra_allowed_deps   []string

	// Version to use in generating the new sysprops
	Version *string

	// Path to Android.bp generator
	Android_bp_gen_path *string

	// Path to product.mk generator
	Product_mk_gen_path *string

	// Whether generate build files for the ndk_translation_packages, default is true.
	Generate_build_files *bool
}

type ndkTranslationPackageDepTag struct {
	blueprint.DependencyTag
	name string
}

func (_ ndkTranslationPackageDepTag) ExcludeFromVisibilityEnforcement() {}

// Some dependencies do not support native bridge variants for riscv
func (_ ndkTranslationPackageDepTag) AllowDisabledModuleDependency(target android.Module) bool {
	return target.Target().NativeBridge == android.NativeBridgeEnabled &&
		target.Target().Arch.ArchType == android.Riscv64
}

func (_ ndkTranslationPackageDepTag) AllowDisabledModuleDependencyProxy(ctx android.OtherModuleProviderContext, mod android.ModuleProxy) bool {
	commonInfo := android.OtherModulePointerProviderOrDefault(ctx, mod, android.CommonModuleInfoProvider)
	return commonInfo.Target.NativeBridge == android.NativeBridgeEnabled &&
		commonInfo.Target.Arch.ArchType == android.Riscv64

}

var (
	ndkTranslationPackageTag              = ndkTranslationPackageDepTag{name: "dep"}
	ndkTranslationPackageFirstTo32SrcsTag = ndkTranslationPackageDepTag{name: "first_to_32"}
	ndkTranslationExtraAllowedDepsTag     = ndkTranslationPackageDepTag{name: "extra_allowed_deps"}
)

func (n *ndkTranslationPackage) DepsMutator(ctx android.BottomUpMutatorContext) {
	for index, t := range ctx.MultiTargets() {
		if t.NativeBridge == android.NativeBridgeEnabled {
			ctx.AddFarVariationDependencies(t.Variations(), ndkTranslationPackageTag, n.properties.Native_bridge_deps.GetOrDefault(ctx, nil)...)
		} else if t.Arch.ArchType == android.X86_64 {
			ctx.AddFarVariationDependencies(t.Variations(), ndkTranslationPackageTag, n.properties.Device_64_deps...)
			ctx.AddFarVariationDependencies(t.Variations(), ndkTranslationPackageTag, n.properties.Device_both_deps...)
			ctx.AddFarVariationDependencies(t.Variations(), ndkTranslationExtraAllowedDepsTag, n.properties.Device_both_extra_allowed_deps...)
		} else if t.Arch.ArchType == android.X86 {
			ctx.AddFarVariationDependencies(t.Variations(), ndkTranslationPackageTag, n.properties.Device_32_deps...)
			ctx.AddFarVariationDependencies(t.Variations(), ndkTranslationPackageTag, n.properties.Device_both_deps...)
			ctx.AddFarVariationDependencies(t.Variations(), ndkTranslationExtraAllowedDepsTag, n.properties.Device_both_extra_allowed_deps...)
			ctx.AddFarVariationDependencies(t.Variations(), ndkTranslationExtraAllowedDepsTag, n.properties.Device_32_extra_allowed_deps...)
		}
		if index == 0 { // Primary arch
			ctx.AddFarVariationDependencies(t.Variations(), ndkTranslationPackageTag, n.properties.Device_first_deps...)
			ctx.AddFarVariationDependencies(t.Variations(), ndkTranslationPackageFirstTo32SrcsTag, n.properties.Device_first_to_32_deps...)
		}
	}
}

func (n *ndkTranslationPackage) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	var files []android.PackagingSpec   // both arches
	var files64 []android.PackagingSpec // 64 only
	var extraFiles []android.PackagingSpec
	var extraFiles64 []android.PackagingSpec

	ctx.VisitDirectDepsProxy(func(child android.ModuleProxy) {
		tag := ctx.OtherModuleDependencyTag(child)
		info := android.OtherModuleProviderOrDefault(ctx, child, android.InstallFilesProvider)
		commonInfo := android.OtherModulePointerProviderOrDefault(ctx, child, android.CommonModuleInfoProvider)
		if tag == ndkTranslationExtraAllowedDepsTag {
			extraFiles = append(extraFiles, info.PackagingSpecs...)
			if commonInfo.Target.Arch.ArchType == android.X86_64 || commonInfo.Target.Arch.ArchType == android.Arm64 {
				extraFiles64 = append(extraFiles64, info.PackagingSpecs...)
			}
			return
		}
		files = append(files, info.PackagingSpecs...)
		if (commonInfo.Target.Arch.ArchType == android.X86_64 || commonInfo.Target.Arch.ArchType == android.Arm64) && tag != ndkTranslationPackageFirstTo32SrcsTag {
			files64 = append(files64, info.PackagingSpecs...)
		}
	})

	outZip := android.PathForModuleOut(ctx, ctx.ModuleName()+".zip")
	builder := android.NewRuleBuilder(pctx, ctx)
	cmd := builder.Command().
		BuiltTool("soong_zip").
		FlagWithOutput("-o ", outZip)

	if proptools.BoolDefault(n.properties.Generate_build_files, true) {
		outBp := n.genAndroidBp(ctx, files)
		outArm64ArmMk, outArm64Mk := n.genProductMk(ctx, files, files64, extraFiles, extraFiles64)
		for _, buildFile := range []android.Path{outBp, outArm64ArmMk, outArm64Mk} {
			cmd.
				FlagWithArg("-C ", filepath.Dir(buildFile.String())).
				FlagWithInput("-f ", buildFile)
		}
	}

	for _, file := range files {
		// Copy to relative path inside the zip
		cmd.
			FlagWithArg("-e ", "system/"+file.RelPathInPackage()).
			FlagWithInput("-f ", file.SrcPath())
	}

	builder.Build("ndk_translation_package.zip", fmt.Sprintf("Build ndk_translation_package for %s", ctx.ModuleName()))

	ctx.CheckbuildFile(outZip)
	n.output = outZip

	ctx.DistForGoal(ctx.ModuleName(), outZip)
}

// Creates a build rule to generate Android.bp and returns path of the generated file.
func (n *ndkTranslationPackage) genAndroidBp(ctx android.ModuleContext, files []android.PackagingSpec) android.Path {
	genDir := android.PathForModuleOut(ctx, "android_bp_dir")
	generator := android.PathForModuleSrc(ctx, proptools.String(n.properties.Android_bp_gen_path))
	builder := android.NewRuleBuilder(pctx, ctx).Sbox(
		genDir,
		android.PathForModuleOut(ctx, "Android.bp.sbox.textproto"),
	)
	outBp := genDir.Join(ctx, "Android.bp")
	builder.Command().
		Input(generator).
		Implicits(specsToSrcPaths(files)).
		Flag(strings.Join(filesRelativeToInstallDir(ctx, files), " ")).
		FlagWithOutput("> ", outBp)
	builder.Build("ndk_translation_package.Android.bp", "Build ndk_translation_package Android.bp")

	return outBp
}

// Creates a build rule to generate product.mk and returns path of the generated files
func (n *ndkTranslationPackage) genProductMk(ctx android.ModuleContext, files, files64, extraFiles, extraFiles64 []android.PackagingSpec) (android.Path, android.Path) {
	genDir := android.PathForModuleOut(ctx, "product_arm64_arm_dir")
	generator := android.PathForModuleSrc(ctx, proptools.String(n.properties.Product_mk_gen_path))
	// Both arches
	builder := android.NewRuleBuilder(pctx, ctx).Sbox(
		genDir,
		android.PathForModuleOut(ctx, "product_arm64_arm.mk.textproto"),
	)
	outArm64ArmMk := genDir.Join(ctx, "product_arm64_arm.mk")
	builder.Command().
		Input(generator).
		Implicits(specsToSrcPaths(files)).
		FlagWithArg("--version=", proptools.String(n.properties.Version)).
		Flag("--arm64 --arm").
		FlagForEachArg("--extra_allowed_artifact ", filesRelativeToInstallDir(ctx, extraFiles)).
		Flag(strings.Join(filesRelativeToInstallDir(ctx, files), " ")).
		FlagWithOutput("> ", outArm64ArmMk)
	builder.Build("ndk_translation_package.product_arm64_arm.mk", "Build ndk_translation_package product_arm64_arm.mk")

	// Arm64 only
	genDir = android.PathForModuleOut(ctx, "product_arm64_dir")
	builder = android.NewRuleBuilder(pctx, ctx).Sbox(
		genDir,
		android.PathForModuleOut(ctx, "product_arm64.mk.textproto"),
	)
	outArm64Mk := genDir.Join(ctx, "product_arm64.mk")
	builder.Command().
		Input(generator).
		Implicits(specsToSrcPaths(files64)).
		FlagWithArg("--version=", proptools.String(n.properties.Version)).
		Flag("--arm64").
		FlagForEachArg("--extra_allowed_artifact ", filesRelativeToInstallDir(ctx, extraFiles64)).
		Flag(strings.Join(filesRelativeToInstallDir(ctx, files64), " ")).
		FlagWithOutput("> ", outArm64Mk)
	builder.Build("ndk_translation_package.product_arm_64.mk", "Build ndk_translation_package product_arm64.mk")

	return outArm64ArmMk, outArm64Mk
}

func filesRelativeToInstallDir(ctx android.ModuleContext, files []android.PackagingSpec) []string {
	var ret []string
	for _, file := range files {
		ret = append(ret, "system/"+file.RelPathInPackage())
	}
	return ret
}

func specsToSrcPaths(specs []android.PackagingSpec) android.Paths {
	var ret android.Paths
	for _, spec := range specs {
		ret = append(ret, spec.SrcPath())
	}
	return ret
}

// The only purpose of this method is to make sure we can build the module directly without dist.
func (n *ndkTranslationPackage) AndroidMkEntries() []android.AndroidMkEntries {
	return []android.AndroidMkEntries{
		android.AndroidMkEntries{
			Class:      "ETC",
			OutputFile: android.OptionalPathForPath(n.output),
			ExtraEntries: []android.AndroidMkExtraEntriesFunc{
				func(ctx android.AndroidMkExtraEntriesContext, entries *android.AndroidMkEntries) {
					entries.SetBool("LOCAL_UNINSTALLABLE_MODULE", true)
				}},
		},
	}
}
