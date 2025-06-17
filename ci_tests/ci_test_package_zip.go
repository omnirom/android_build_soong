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

package ci_tests

import (
	"fmt"
	"path/filepath"
	"strings"

	"android/soong/android"

	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"
)

func init() {
	pctx.Import("android/soong/android")
	registerTestPackageZipBuildComponents(android.InitRegistrationContext)
}

func registerTestPackageZipBuildComponents(ctx android.RegistrationContext) {
	ctx.RegisterModuleType("test_package", TestPackageZipFactory)
}

type testPackageZip struct {
	android.ModuleBase
	android.DefaultableModuleBase

	properties CITestPackageProperties

	output android.Path
}

type CITestPackageProperties struct {
	// test modules will be added as dependencies using the device os and the common architecture's variant.
	Tests proptools.Configurable[[]string] `android:"arch_variant"`
	// test modules that will be added as dependencies based on the first supported arch variant and the device os variant
	Device_first_tests proptools.Configurable[[]string] `android:"arch_variant"`
	// test modules that will be added as dependencies based on both 32bit and 64bit arch variant and the device os variant
	Device_both_tests proptools.Configurable[[]string] `android:"arch_variant"`
	// test modules that will be added as dependencies based on host
	Host_tests proptools.Configurable[[]string] `android:"arch_variant"`
	// git-main only test modules. Will only be added as dependencies using the device os and the common architecture's variant if exists.
	Tests_if_exist_common proptools.Configurable[[]string] `android:"arch_variant"`
	// git-main only test modules. Will only be added as dependencies based on both 32bit and 64bit arch variant and the device os variant if exists.
	Tests_if_exist_device_both proptools.Configurable[[]string] `android:"arch_variant"`
}

type testPackageZipDepTagType struct {
	blueprint.BaseDependencyTag
}

var testPackageZipDepTag testPackageZipDepTagType

var (
	pctx = android.NewPackageContext("android/soong/ci_tests")
	// test_package module type should only be used for the following modules.
	// TODO: remove "_soong" from the module names inside when eliminating the corresponding make modules
	moduleNamesAllowed = []string{"continuous_instrumentation_tests_soong", "continuous_instrumentation_metric_tests_soong", "continuous_native_tests_soong", "continuous_native_metric_tests_soong", "platform_tests"}
)

func (p *testPackageZip) DepsMutator(ctx android.BottomUpMutatorContext) {
	// adding tests property deps
	for _, t := range p.properties.Tests.GetOrDefault(ctx, nil) {
		ctx.AddVariationDependencies(ctx.Config().AndroidCommonTarget.Variations(), testPackageZipDepTag, t)
	}

	// adding device_first_tests property deps
	for _, t := range p.properties.Device_first_tests.GetOrDefault(ctx, nil) {
		ctx.AddVariationDependencies(ctx.Config().AndroidFirstDeviceTarget.Variations(), testPackageZipDepTag, t)
	}

	// adding device_both_tests property deps
	p.addDeviceBothDeps(ctx, false)

	// adding host_tests property deps
	for _, t := range p.properties.Host_tests.GetOrDefault(ctx, nil) {
		ctx.AddVariationDependencies(ctx.Config().BuildOSTarget.Variations(), testPackageZipDepTag, t)
	}

	// adding Tests_if_exist_* property deps
	for _, t := range p.properties.Tests_if_exist_common.GetOrDefault(ctx, nil) {
		if ctx.OtherModuleExists(t) {
			ctx.AddVariationDependencies(ctx.Config().AndroidCommonTarget.Variations(), testPackageZipDepTag, t)
		}
	}
	p.addDeviceBothDeps(ctx, true)
}

func (p *testPackageZip) addDeviceBothDeps(ctx android.BottomUpMutatorContext, checkIfExist bool) {
	android32TargetList := android.FirstTarget(ctx.Config().Targets[android.Android], "lib32")
	android64TargetList := android.FirstTarget(ctx.Config().Targets[android.Android], "lib64")
	if len(android32TargetList) > 0 {
		maybeAndroid32Target := &android32TargetList[0]
		if checkIfExist {
			for _, t := range p.properties.Tests_if_exist_device_both.GetOrDefault(ctx, nil) {
				if ctx.OtherModuleExists(t) {
					ctx.AddFarVariationDependencies(maybeAndroid32Target.Variations(), testPackageZipDepTag, t)
				}
			}
		} else {
			ctx.AddFarVariationDependencies(maybeAndroid32Target.Variations(), testPackageZipDepTag, p.properties.Device_both_tests.GetOrDefault(ctx, nil)...)
		}
	}
	if len(android64TargetList) > 0 {
		maybeAndroid64Target := &android64TargetList[0]
		if checkIfExist {
			for _, t := range p.properties.Tests_if_exist_device_both.GetOrDefault(ctx, nil) {
				if ctx.OtherModuleExists(t) {
					ctx.AddFarVariationDependencies(maybeAndroid64Target.Variations(), testPackageZipDepTag, t)
				}
			}
		} else {
			ctx.AddFarVariationDependencies(maybeAndroid64Target.Variations(), testPackageZipDepTag, p.properties.Device_both_tests.GetOrDefault(ctx, nil)...)
		}
	}
}

func TestPackageZipFactory() android.Module {
	module := &testPackageZip{}

	module.AddProperties(&module.properties)

	android.InitAndroidArchModule(module, android.DeviceSupported, android.MultilibCommon)
	android.InitDefaultableModule(module)

	return module
}

func (p *testPackageZip) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	// Never install this test package, it's for disting only
	p.SkipInstall()

	if !android.InList(ctx.ModuleName(), moduleNamesAllowed) {
		ctx.ModuleErrorf("%s is not allowed to use module type test_package")
	}

	p.output = createOutput(ctx, pctx)

	ctx.SetOutputFiles(android.Paths{p.output}, "")
}

func createOutput(ctx android.ModuleContext, pctx android.PackageContext) android.ModuleOutPath {
	productOut := filepath.Join(ctx.Config().OutDir(), "target", "product", ctx.Config().DeviceName())
	stagingDir := android.PathForModuleOut(ctx, "STAGING")
	productVariables := ctx.Config().ProductVariables()
	arch := proptools.String(productVariables.DeviceArch)
	secondArch := proptools.String(productVariables.DeviceSecondaryArch)

	builder := android.NewRuleBuilder(pctx, ctx)
	builder.Command().Text("rm").Flag("-rf").Text(stagingDir.String())
	builder.Command().Text("mkdir").Flag("-p").Output(stagingDir)
	builder.Temporary(stagingDir)
	ctx.WalkDeps(func(child, parent android.Module) bool {
		if !child.Enabled(ctx) {
			return false
		}
		if android.EqualModules(parent, ctx.Module()) && ctx.OtherModuleDependencyTag(child) == testPackageZipDepTag {
			// handle direct deps
			extendBuilderCommand(ctx, child, builder, stagingDir, productOut, arch, secondArch)
			return true
		} else if !android.EqualModules(parent, ctx.Module()) && ctx.OtherModuleDependencyTag(child) == android.RequiredDepTag {
			// handle the "required" from deps
			extendBuilderCommand(ctx, child, builder, stagingDir, productOut, arch, secondArch)
			return true
		} else {
			return false
		}
	})

	output := android.PathForModuleOut(ctx, ctx.ModuleName()+".zip")
	builder.Command().
		BuiltTool("soong_zip").
		Flag("-o").Output(output).
		Flag("-C").Text(stagingDir.String()).
		Flag("-D").Text(stagingDir.String())
	builder.Command().Text("rm").Flag("-rf").Text(stagingDir.String())
	builder.Build("test_package", fmt.Sprintf("build test_package for %s", ctx.ModuleName()))
	return output
}

func extendBuilderCommand(ctx android.ModuleContext, m android.Module, builder *android.RuleBuilder, stagingDir android.ModuleOutPath, productOut, arch, secondArch string) {
	info, ok := android.OtherModuleProvider(ctx, m, android.ModuleInfoJSONProvider)
	if !ok {
		ctx.OtherModuleErrorf(m, "doesn't set ModuleInfoJSON provider")
	} else if len(info) != 1 {
		ctx.OtherModuleErrorf(m, "doesn't provide exactly one ModuleInfoJSON")
	}

	classes := info[0].GetClass()
	if len(info[0].Class) != 1 {
		ctx.OtherModuleErrorf(m, "doesn't have exactly one class in its ModuleInfoJSON")
	}
	class := strings.ToLower(classes[0])
	if class == "apps" {
		class = "app"
	} else if class == "java_libraries" {
		class = "framework"
	}

	installedFilesInfo, ok := android.OtherModuleProvider(ctx, m, android.InstallFilesProvider)
	if !ok {
		ctx.ModuleErrorf("Module %s doesn't set InstallFilesProvider", m.Name())
	}

	for _, spec := range installedFilesInfo.PackagingSpecs {
		if spec.SrcPath() == nil {
			// Probably a symlink
			continue
		}
		installedFile := spec.FullInstallPath()

		ext := installedFile.Ext()
		// there are additional installed files for some app-class modules, we only need the .apk, .odex and .vdex files in the test package
		excludeInstalledFile := ext != ".apk" && ext != ".odex" && ext != ".vdex"
		if class == "app" && excludeInstalledFile {
			continue
		}
		// only .jar files should be included for a framework dep
		if class == "framework" && ext != ".jar" {
			continue
		}
		name := removeFileExtension(installedFile.Base())
		// some apks have other apk as installed files, these additional files shouldn't be included
		isAppOrFramework := class == "app" || class == "framework"
		if isAppOrFramework && name != ctx.OtherModuleName(m) {
			continue
		}

		f := strings.TrimPrefix(installedFile.String(), productOut+"/")
		if strings.HasPrefix(f, "out") {
			continue
		}
		if strings.HasPrefix(f, "system/") {
			f = strings.Replace(f, "system/", "DATA/", 1)
		}
		f = strings.ReplaceAll(f, filepath.Join("testcases", name, arch), filepath.Join("DATA", class, name))
		f = strings.ReplaceAll(f, filepath.Join("testcases", name, secondArch), filepath.Join("DATA", class, name))
		if strings.HasPrefix(f, "testcases") {
			f = strings.Replace(f, "testcases", filepath.Join("DATA", class), 1)
		}
		if strings.HasPrefix(f, "data/") {
			f = strings.Replace(f, "data/", "DATA/", 1)
		}
		f = strings.ReplaceAll(f, "DATA_other", "system_other")
		f = strings.ReplaceAll(f, "system_other/DATA", "system_other/system")
		dir := filepath.Dir(f)

		tempOut := android.PathForModuleOut(ctx, "STAGING", f)
		builder.Command().Text("mkdir").Flag("-p").Text(filepath.Join(stagingDir.String(), dir))
		// Copy srcPath instead of installedFile because some rules like target-files.zip
		// are non-hermetic and would be affected if we built the installed files.
		builder.Command().Text("cp").Flag("-Rf").Input(spec.SrcPath()).Output(tempOut)
		builder.Temporary(tempOut)
	}
}

func removeFileExtension(filename string) string {
	return strings.TrimSuffix(filename, filepath.Ext(filename))
}

// The only purpose of this method is to make sure we can build the module directly
// without adding suffix "-soong"
func (p *testPackageZip) AndroidMkEntries() []android.AndroidMkEntries {
	return []android.AndroidMkEntries{
		android.AndroidMkEntries{
			Class:      "ETC",
			OutputFile: android.OptionalPathForPath(p.output),
			ExtraEntries: []android.AndroidMkExtraEntriesFunc{
				func(ctx android.AndroidMkExtraEntriesContext, entries *android.AndroidMkEntries) {
					entries.SetBool("LOCAL_UNINSTALLABLE_MODULE", true)
				}},
		},
	}
}
