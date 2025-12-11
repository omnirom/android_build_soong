// Copyright (C) 2021 The Android Open Source Project
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

package kernel

import (
	"fmt"
	"path/filepath"
	"strings"

	"android/soong/android"
	_ "android/soong/cc/config"

	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"
)

func init() {
	pctx.Import("android/soong/cc/config")
	registerKernelBuildComponents(android.InitRegistrationContext)
}

func registerKernelBuildComponents(ctx android.RegistrationContext) {
	ctx.RegisterModuleType("prebuilt_kernel_modules", PrebuiltKernelModulesFactory)
}

type prebuiltKernelModules struct {
	android.ModuleBase

	properties prebuiltKernelModulesProperties

	installDir android.InstallPath
}

type prebuiltKernelModulesProperties struct {
	// List or filegroup of prebuilt kernel module files. Should have .ko suffix.
	Srcs []string `android:"path,arch_variant"`

	// List of kernel module filenames that will be loaded via modules.load.
	// Should have .ko suffix.
	// If empty, this will default to filenames of Srcs.
	// If no sources should be loaded, set `Load_by_default` to false instead.
	Src_filenames_to_load []string `android:"path,arch_variant"`

	// List or filegroup of prebuilt kernel module files for 16k. Should have .ko suffix.
	// These files will be installed in lib/modules/16k-mode/
	// These files are ONLY loaded during the Second Boot Stage when the device is in 16k mode.
	Srcs_16k []string `android:"path,arch_variant"`

	// List of system_dlkm kernel modules that the local kernel modules depend on.
	// The deps will be assembled into intermediates directory for running depmod
	// but will not be added to the current module's installed files.
	System_deps []string `android:"path,arch_variant"`

	// If false, then srcs will not be included in modules.load.
	// This feature is used by system_dlkm
	Load_by_default *bool

	Blocklist_file *string `android:"path"`

	// Path to the kernel module options file
	Options_file *string `android:"path"`

	// Kernel version that these modules are for. Kernel modules are installed to
	// /lib/modules/<kernel_version> directory in the corresponding partition. Default is "".
	Kernel_version *string

	// Whether this module is directly installable to one of the partitions. Default is true
	Installable *bool

	// Whether debug symbols should be stripped from the *.ko files.
	// Defaults to true.
	Strip_debug_symbols *bool
}

// prebuilt_kernel_modules installs a set of prebuilt kernel module files to the correct directory.
// In addition, this module builds modules.load, modules.dep, modules.softdep and modules.alias
// using depmod and installs them as well.
func PrebuiltKernelModulesFactory() android.Module {
	module := &prebuiltKernelModules{}
	module.AddProperties(&module.properties)
	android.InitAndroidArchModule(module, android.DeviceSupported, android.MultilibFirst)
	return module
}

func (pkm *prebuiltKernelModules) installable() bool {
	return proptools.BoolDefault(pkm.properties.Installable, true)
}

func (pkm *prebuiltKernelModules) KernelVersion() string {
	return proptools.StringDefault(pkm.properties.Kernel_version, "")
}

func (pkm *prebuiltKernelModules) DepsMutator(ctx android.BottomUpMutatorContext) {
	// do nothing
}

func (pkm *prebuiltKernelModules) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	if !pkm.installable() {
		pkm.SkipInstall()
	}

	modules := android.PathsForModuleSrc(ctx, pkm.properties.Srcs)
	systemModules := android.PathsForModuleSrc(ctx, pkm.properties.System_deps)

	depmodOut := pkm.runDepmod(ctx, modules, systemModules)
	if proptools.BoolDefault(pkm.properties.Strip_debug_symbols, true) {
		modules = stripDebugSymbols(ctx, modules)
	}

	installDir := android.PathForModuleInstall(ctx, "lib", "modules")
	// Kernel module is installed to vendor_ramdisk/lib/modules regardless of product
	// configuration. This matches the behavior in make and prevents the files from being
	// installed in `vendor_ramdisk/first_stage_ramdisk`.
	if pkm.InstallInVendorRamdisk() {
		installDir = android.PathForModuleInPartitionInstall(ctx, "vendor_ramdisk", "lib", "modules")
	} else if pkm.InstallInVendorKernelRamdisk() {
		installDir = android.PathForModuleInPartitionInstall(ctx, "vendor_kernel_ramdisk", "lib", "modules")
	}

	if pkm.KernelVersion() != "" {
		installDir = installDir.Join(ctx, pkm.KernelVersion())
	}

	dests := []string{}
	for _, m := range modules {
		installPath := ctx.InstallFile(installDir, filepath.Base(m.String()), m)
		dests = append(dests, installPath.String())
	}
	installDir16k := installDir.Join(ctx, "16k-mode")
	for _, m := range android.PathsForModuleSrc(ctx, pkm.properties.Srcs_16k) {
		installPath := ctx.InstallFile(installDir16k, filepath.Base(m.String()), m)
		dests = append(dests, installPath.String())
	}
	srcs := android.PathsForModuleSrc(ctx, pkm.properties.Srcs).Strings()
	srcs = append(srcs, android.PathsForModuleSrc(ctx, pkm.properties.Srcs_16k).Strings()...)
	// Use ANDROID-GEN to identify the source of module.* files which are generated in the build process.
	// See the use of ANDROID-GEN in build/make/core/Makefile
	androidGen := "ANDROID-GEN"
	// Add ANDROID-GEN four time to match the number of "modules.*" files installed below.
	srcs = append(srcs, androidGen, androidGen, androidGen, androidGen)
	installPath := ctx.InstallFile(installDir, "modules.load", depmodOut.modulesLoad)
	dests = append(dests, installPath.String())
	installPath = ctx.InstallFile(installDir, "modules.dep", depmodOut.modulesDep)
	dests = append(dests, installPath.String())
	installPath = ctx.InstallFile(installDir, "modules.softdep", depmodOut.modulesSoftdep)
	dests = append(dests, installPath.String())
	installPath = ctx.InstallFile(installDir, "modules.alias", depmodOut.modulesAlias)
	dests = append(dests, installPath.String())

	pkm.installBlocklistFile(ctx, installDir, &srcs, &dests)
	pkm.installOptionsFile(ctx, installDir, &srcs, &dests)

	ctx.SetOutputFiles(modules, ".modules")

	android.SetProvider(ctx, android.PrebuiltKernelModulesComplianceMetadataProvider,
		android.PrebuiltKernelModulesComplianceMetadata{
			Srcs:  srcs,
			Dests: dests,
		})
}

func (pkm *prebuiltKernelModules) installBlocklistFile(ctx android.ModuleContext, installDir android.InstallPath, srcs *[]string, dests *[]string) {
	if pkm.properties.Blocklist_file == nil {
		return
	}
	blocklistOut := android.PathForModuleOut(ctx, "modules.blocklist")

	src := android.PathForModuleSrc(ctx, proptools.String(pkm.properties.Blocklist_file))
	*srcs = append(*srcs, src.String())
	ctx.Build(pctx, android.BuildParams{
		Rule:   processBlocklistFile,
		Input:  src,
		Output: blocklistOut,
	})
	installPath := ctx.InstallFile(installDir, "modules.blocklist", blocklistOut)
	*dests = append(*dests, installPath.String())
}

func (pkm *prebuiltKernelModules) installOptionsFile(ctx android.ModuleContext, installDir android.InstallPath, srcs *[]string, dests *[]string) {
	if pkm.properties.Options_file == nil {
		return
	}
	optionsOut := android.PathForModuleOut(ctx, "modules.options")

	src := android.PathForModuleSrc(ctx, proptools.String(pkm.properties.Options_file))
	*srcs = append(*srcs, src.String())
	ctx.Build(pctx, android.BuildParams{
		Rule:   processOptionsFile,
		Input:  src,
		Output: optionsOut,
	})
	installPath := ctx.InstallFile(installDir, "modules.options", optionsOut)
	*dests = append(*dests, installPath.String())
}

var (
	pctx = android.NewPackageContext("android/soong/kernel")

	StripRule = pctx.AndroidStaticRule("strip",
		blueprint.RuleParams{
			Command:     "$stripCmd -o $out --strip-debug $in",
			CommandDeps: []string{"$stripCmd"},
		}, "stripCmd")
)

func stripDebugSymbols(ctx android.ModuleContext, modules android.Paths) android.Paths {
	dir := android.PathForModuleOut(ctx, "stripped").OutputPath
	var outputs android.Paths

	for _, m := range modules {
		stripped := dir.Join(ctx, filepath.Base(m.String()))
		ctx.Build(pctx, android.BuildParams{
			Rule:   StripRule,
			Input:  m,
			Output: stripped,
			Args: map[string]string{
				"stripCmd": "${config.ClangBin}/llvm-strip",
			},
		})
		outputs = append(outputs, stripped)
	}

	return outputs
}

type depmodOutputs struct {
	modulesLoad    android.OutputPath
	modulesDep     android.OutputPath
	modulesSoftdep android.OutputPath
	modulesAlias   android.OutputPath
}

var (
	// system/lib/modules/foo.ko: system/lib/modules/bar.ko
	// will be converted to
	// /system/lib/modules/foo.ko: /system/lib/modules/bar.ko
	addLeadingSlashToPaths = pctx.AndroidStaticRule("add_leading_slash",
		blueprint.RuleParams{
			Command: `sed -e 's|\([^: ]*lib/modules/[^: ]*\)|/\1|g' $in > $out`,
		},
	)
	// Remove empty lines. Raise an exception if line is _not_ formatted as `blocklist $name.ko`
	processBlocklistFile = pctx.AndroidStaticRule("process_blocklist_file",
		blueprint.RuleParams{
			Command: `rm -rf $out && awk <$in > $out` +
				` '/^#/ { print; next }` +
				` NF == 0 { next }` +
				` NF != 2 || $$1 != "blocklist"` +
				` { print "Invalid blocklist line " FNR ": " $$0 >"/dev/stderr";` +
				` exit_status = 1; next }` +
				` { $$1 = $$1; print }` +
				` END { exit exit_status }'`,
		},
	)
	// Remove empty lines. Raise an exception if line is _not_ formatted as `options $name.ko`
	processOptionsFile = pctx.AndroidStaticRule("process_options_file",
		blueprint.RuleParams{
			Command: `rm -rf $out && awk <$in > $out` +
				` '/^#/ { print; next }` +
				` NF == 0 { next }` +
				` NF < 2 || $$1 != "options"` +
				` { print "Invalid options line " FNR ": " $$0 >"/dev/stderr";` +
				` exit_status = 1; next }` +
				` { $$1 = $$1; print }` +
				` END { exit exit_status }'`,
		},
	)
)

// This is the path in soong intermediates where the .ko files will be copied.
// The layout should match the layout on device so that depmod can create meaningful modules.* files.
func modulesDirForAndroidDlkm(ctx android.ModuleContext, modulesDir android.OutputPath, system bool) android.OutputPath {
	if ctx.InstallInSystemDlkm() || system {
		// The first component can be either system or system_dlkm
		// system works because /system/lib/modules is a symlink to /system_dlkm/lib/modules.
		// system was chosen to match the contents of the kati built modules.dep
		return modulesDir.Join(ctx, "system", "lib", "modules")
	} else if ctx.InstallInVendorDlkm() {
		return modulesDir.Join(ctx, "vendor", "lib", "modules")
	} else if ctx.InstallInOdmDlkm() {
		return modulesDir.Join(ctx, "odm", "lib", "modules")
	} else if ctx.InstallInVendorRamdisk() || ctx.InstallInVendorKernelRamdisk() {
		return modulesDir.Join(ctx, "lib", "modules")
	} else {
		// not an android dlkm module.
		return modulesDir
	}
}

// Validates that each entry in Src_filenames_to_load is present in Srcs
func (pkm *prebuiltKernelModules) validateSrcFilenamesToLoad(ctx android.ModuleContext) {
	if len(pkm.properties.Src_filenames_to_load) == 0 {
		return
	}
	filenames := make(map[string]bool)
	for _, module := range android.PathsForModuleSrc(ctx, pkm.properties.Srcs) {
		filenames[module.Base()] = true
	}
	for _, filenameToLoad := range pkm.properties.Src_filenames_to_load {
		if _, exists := filenames[filenameToLoad]; !exists {
			ctx.PropertyErrorf("Src_filenames_to_load", "%s in Src_filenames_to_load not present in Srcs", filenameToLoad)
		}
	}
}

func (pkm *prebuiltKernelModules) runDepmod(ctx android.ModuleContext, modules android.Paths, systemModules android.Paths) depmodOutputs {
	baseDir := android.PathForModuleOut(ctx, "depmod").OutputPath
	fakeVer := "0.0" // depmod demands this anyway
	modulesDir := baseDir.Join(ctx, "lib", "modules", fakeVer)
	modulesCpDir := modulesDirForAndroidDlkm(ctx, modulesDir, false)

	builder := android.NewRuleBuilder(pctx, ctx)

	// Copy the module files to a temporary dir
	moduleNames := map[string]bool{}
	builder.Command().Text("rm").Flag("-rf").Text(modulesCpDir.String())
	builder.Command().Text("mkdir").Flag("-p").Text(modulesCpDir.String())
	for _, m := range modules {
		builder.Command().Text("cp").Input(m).Text(modulesCpDir.String())
		moduleNames[m.Base()] = true
	}

	modulesDirForSystemDlkm := modulesDirForAndroidDlkm(ctx, modulesDir, true)
	if len(systemModules) > 0 {
		builder.Command().Text("rm").Flag("-rf").Text(modulesDirForSystemDlkm.String())
		builder.Command().Text("mkdir").Flag("-p").Text(modulesDirForSystemDlkm.String())
	}
	for _, m := range systemModules {
		// https://source.corp.google.com/h/googleplex-android/platform/build/+/71d79d0a58e112f76ee2c2dfdebd331971145b4c:core/Makefile;l=480-487;bpv=1;bpt=0;drc=567ee7be9833ea96a65e36fb21d4bd783ff74f1c
		// When there is a duplicate module present in both directories, we want modules in PRIVATE_MODULES to take
		// precedence. Since depmod does not provide any guarantee about ordering of
		// dependency resolution, we achieve this by maually removing any duplicate
		// modules with lower priority.
		if _, exists := moduleNames[m.Base()]; exists {
			continue
		}
		builder.Command().Text("cp").Input(m).Text(modulesDirForSystemDlkm.String())
	}

	// Enumerate modules to load
	modulesLoad := modulesDir.Join(ctx, "modules.load")
	// If Load_by_default is set to false explicitly, create an empty modules.load
	if pkm.properties.Load_by_default != nil && !*pkm.properties.Load_by_default {
		builder.Command().Text("rm").Flag("-rf").Text(modulesLoad.String())
		builder.Command().Text("touch").Output(modulesLoad)
	} else {
		var basenames []string
		if len(pkm.properties.Src_filenames_to_load) > 0 {
			pkm.validateSrcFilenamesToLoad(ctx)
			basenames = append(basenames, pkm.properties.Src_filenames_to_load...)
		} else {
			for _, m := range modules {
				basenames = append(basenames, filepath.Base(m.String()))
			}
		}
		builder.Command().
			Text("echo").Flag("\"" + strings.Join(basenames, " ") + "\"").
			Text("|").Text("tr").Flag("\" \"").Flag("\"\\n\"").
			Text(">").Output(modulesLoad)
	}

	// Run depmod to build modules.dep/softdep/alias files
	modulesDep := modulesDir.Join(ctx, "modules.dep")
	modulesSoftdep := modulesDir.Join(ctx, "modules.softdep")
	modulesAlias := modulesDir.Join(ctx, "modules.alias")
	builder.Command().Text("mkdir").Flag("-p").Text(modulesDir.String())
	builder.Command().
		BuiltTool("depmod").
		FlagWithArg("-b ", baseDir.String()).
		Text(fakeVer).
		ImplicitOutput(modulesDep).
		ImplicitOutput(modulesSoftdep).
		ImplicitOutput(modulesAlias)

	builder.Build("depmod", fmt.Sprintf("depmod %s", ctx.ModuleName()))

	finalModulesDep := modulesDep
	// Add a leading slash to paths in modules.dep of android dlkm and vendor ramdisk
	if ctx.InstallInSystemDlkm() || ctx.InstallInVendorDlkm() || ctx.InstallInOdmDlkm() || ctx.InstallInVendorRamdisk() || ctx.InstallInVendorKernelRamdisk() {
		finalModulesDep = modulesDep.ReplaceExtension(ctx, "intermediates")
		ctx.Build(pctx, android.BuildParams{
			Rule:   addLeadingSlashToPaths,
			Input:  modulesDep,
			Output: finalModulesDep,
		})
	}

	return depmodOutputs{modulesLoad, finalModulesDep, modulesSoftdep, modulesAlias}
}
