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
	}

	if pkm.KernelVersion() != "" {
		installDir = installDir.Join(ctx, pkm.KernelVersion())
	}

	for _, m := range modules {
		ctx.InstallFile(installDir, filepath.Base(m.String()), m)
	}
	ctx.InstallFile(installDir, "modules.load", depmodOut.modulesLoad)
	ctx.InstallFile(installDir, "modules.dep", depmodOut.modulesDep)
	ctx.InstallFile(installDir, "modules.softdep", depmodOut.modulesSoftdep)
	ctx.InstallFile(installDir, "modules.alias", depmodOut.modulesAlias)
	pkm.installBlocklistFile(ctx, installDir)
	pkm.installOptionsFile(ctx, installDir)

	ctx.SetOutputFiles(modules, ".modules")
}

func (pkm *prebuiltKernelModules) installBlocklistFile(ctx android.ModuleContext, installDir android.InstallPath) {
	if pkm.properties.Blocklist_file == nil {
		return
	}
	blocklistOut := android.PathForModuleOut(ctx, "modules.blocklist")

	ctx.Build(pctx, android.BuildParams{
		Rule:   processBlocklistFile,
		Input:  android.PathForModuleSrc(ctx, proptools.String(pkm.properties.Blocklist_file)),
		Output: blocklistOut,
	})
	ctx.InstallFile(installDir, "modules.blocklist", blocklistOut)
}

func (pkm *prebuiltKernelModules) installOptionsFile(ctx android.ModuleContext, installDir android.InstallPath) {
	if pkm.properties.Options_file == nil {
		return
	}
	optionsOut := android.PathForModuleOut(ctx, "modules.options")

	ctx.Build(pctx, android.BuildParams{
		Rule:   processOptionsFile,
		Input:  android.PathForModuleSrc(ctx, proptools.String(pkm.properties.Options_file)),
		Output: optionsOut,
	})
	ctx.InstallFile(installDir, "modules.options", optionsOut)
}

var (
	pctx = android.NewPackageContext("android/soong/kernel")

	stripRule = pctx.AndroidStaticRule("strip",
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
			Rule:   stripRule,
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
	} else if ctx.InstallInVendorRamdisk() {
		return modulesDir.Join(ctx, "lib", "modules")
	} else {
		// not an android dlkm module.
		return modulesDir
	}
}

func (pkm *prebuiltKernelModules) runDepmod(ctx android.ModuleContext, modules android.Paths, systemModules android.Paths) depmodOutputs {
	baseDir := android.PathForModuleOut(ctx, "depmod").OutputPath
	fakeVer := "0.0" // depmod demands this anyway
	modulesDir := baseDir.Join(ctx, "lib", "modules", fakeVer)
	modulesCpDir := modulesDirForAndroidDlkm(ctx, modulesDir, false)

	builder := android.NewRuleBuilder(pctx, ctx)

	// Copy the module files to a temporary dir
	builder.Command().Text("rm").Flag("-rf").Text(modulesCpDir.String())
	builder.Command().Text("mkdir").Flag("-p").Text(modulesCpDir.String())
	for _, m := range modules {
		builder.Command().Text("cp").Input(m).Text(modulesCpDir.String())
	}

	modulesDirForSystemDlkm := modulesDirForAndroidDlkm(ctx, modulesDir, true)
	if len(systemModules) > 0 {
		builder.Command().Text("mkdir").Flag("-p").Text(modulesDirForSystemDlkm.String())
	}
	for _, m := range systemModules {
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
		for _, m := range modules {
			basenames = append(basenames, filepath.Base(m.String()))
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
	if ctx.InstallInSystemDlkm() || ctx.InstallInVendorDlkm() || ctx.InstallInOdmDlkm() || ctx.InstallInVendorRamdisk() {
		finalModulesDep = modulesDep.ReplaceExtension(ctx, "intermediates")
		ctx.Build(pctx, android.BuildParams{
			Rule:   addLeadingSlashToPaths,
			Input:  modulesDep,
			Output: finalModulesDep,
		})
	}

	return depmodOutputs{modulesLoad, finalModulesDep, modulesSoftdep, modulesAlias}
}
