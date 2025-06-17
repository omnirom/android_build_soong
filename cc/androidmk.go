// Copyright 2015 Google Inc. All rights reserved.
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
	"io"
	"path/filepath"
	"strings"

	"android/soong/android"
)

var (
	NativeBridgeSuffix  = ".native_bridge"
	ProductSuffix       = ".product"
	VendorSuffix        = ".vendor"
	RamdiskSuffix       = ".ramdisk"
	VendorRamdiskSuffix = ".vendor_ramdisk"
	RecoverySuffix      = ".recovery"
	sdkSuffix           = ".sdk"
)

type AndroidMkContext interface {
	BaseModuleName() string
	Target() android.Target
	subAndroidMk(android.Config, *android.AndroidMkInfo, interface{})
	Arch() android.Arch
	Os() android.OsType
	Host() bool
	UseVndk() bool
	VndkVersion() string
	static() bool
	InRamdisk() bool
	InVendorRamdisk() bool
	InRecovery() bool
	NotInPlatform() bool
	InVendorOrProduct() bool
	ArchSpecific() bool
}

type subAndroidMkProviderInfoProducer interface {
	prepareAndroidMKProviderInfo(android.Config, AndroidMkContext, *android.AndroidMkInfo)
}

type subAndroidMkFooterInfoProducer interface {
	prepareAndroidMKFooterInfo(android.Config, AndroidMkContext, *android.AndroidMkInfo)
}

func (c *Module) subAndroidMk(config android.Config, entries *android.AndroidMkInfo, obj interface{}) {
	if c.subAndroidMkOnce == nil {
		c.subAndroidMkOnce = make(map[subAndroidMkProviderInfoProducer]bool)
	}
	if androidmk, ok := obj.(subAndroidMkProviderInfoProducer); ok {
		if !c.subAndroidMkOnce[androidmk] {
			c.subAndroidMkOnce[androidmk] = true
			androidmk.prepareAndroidMKProviderInfo(config, c, entries)
		}
	}
}

var _ android.AndroidMkProviderInfoProducer = (*Module)(nil)

func (c *Module) PrepareAndroidMKProviderInfo(config android.Config) *android.AndroidMkProviderInfo {
	if c.hideApexVariantFromMake || c.Properties.HideFromMake {
		return &android.AndroidMkProviderInfo{
			PrimaryInfo: android.AndroidMkInfo{
				Disabled: true,
			},
		}
	}

	providerData := android.AndroidMkProviderInfo{
		PrimaryInfo: android.AndroidMkInfo{
			OutputFile:   c.outputFile,
			Required:     c.Properties.AndroidMkRuntimeLibs,
			OverrideName: c.BaseModuleName(),
			Include:      "$(BUILD_SYSTEM)/soong_cc_rust_prebuilt.mk",
			EntryMap:     make(map[string][]string),
		},
	}

	entries := &providerData.PrimaryInfo
	if len(c.Properties.Logtags) > 0 {
		entries.AddStrings("LOCAL_SOONG_LOGTAGS_FILES", c.logtagsPaths.Strings()...)
	}
	// Note: Pass the exact value of AndroidMkSystemSharedLibs to the Make
	// world, even if it is an empty list. In the Make world,
	// LOCAL_SYSTEM_SHARED_LIBRARIES defaults to "none", which is expanded
	// to the default list of system shared libs by the build system.
	// Soong computes the exact list of system shared libs, so we have to
	// override the default value when the list of libs is actually empty.
	entries.SetString("LOCAL_SYSTEM_SHARED_LIBRARIES", strings.Join(c.Properties.AndroidMkSystemSharedLibs, " "))
	if len(c.Properties.AndroidMkSharedLibs) > 0 {
		entries.AddStrings("LOCAL_SHARED_LIBRARIES", c.Properties.AndroidMkSharedLibs...)
	}
	if len(c.Properties.AndroidMkRuntimeLibs) > 0 {
		entries.AddStrings("LOCAL_RUNTIME_LIBRARIES", c.Properties.AndroidMkRuntimeLibs...)
	}
	entries.SetString("LOCAL_SOONG_LINK_TYPE", c.makeLinkType)
	if c.InVendor() {
		entries.SetBool("LOCAL_IN_VENDOR", true)
	} else if c.InProduct() {
		entries.SetBool("LOCAL_IN_PRODUCT", true)
	}
	if c.Properties.SdkAndPlatformVariantVisibleToMake {
		// Add the unsuffixed name to SOONG_SDK_VARIANT_MODULES so that Make can rewrite
		// dependencies to the .sdk suffix when building a module that uses the SDK.
		entries.SetString("SOONG_SDK_VARIANT_MODULES",
			"$(SOONG_SDK_VARIANT_MODULES) $(patsubst %.sdk,%,$(LOCAL_MODULE))")
	}
	entries.SetBoolIfTrue("LOCAL_UNINSTALLABLE_MODULE", c.IsSkipInstall())

	for _, feature := range c.features {
		c.subAndroidMk(config, entries, feature)
	}

	c.subAndroidMk(config, entries, c.compiler)
	c.subAndroidMk(config, entries, c.linker)
	if c.sanitize != nil {
		c.subAndroidMk(config, entries, c.sanitize)
	}
	c.subAndroidMk(config, entries, c.installer)

	entries.SubName += c.Properties.SubName

	// The footer info comes at the last step, previously it was achieved by
	// calling some extra footer function that were added earlier. Because we no
	// longer use these extra footer functions, we need to put this step at the
	// last one.
	if c.Properties.IsSdkVariant && c.Properties.SdkAndPlatformVariantVisibleToMake &&
		c.CcLibraryInterface() && c.Shared() {
		// Using the SDK variant as a JNI library needs a copy of the .so that
		// is not named .sdk.so so that it can be packaged into the APK with
		// the right name.
		entries.FooterStrings = []string{
			fmt.Sprintf("%s %s %s", "$(eval $(call copy-one-file,",
				"$(LOCAL_BUILT_MODULE),",
				"$(patsubst %.sdk.so,%.so,$(LOCAL_BUILT_MODULE))))")}
	}

	for _, obj := range []interface{}{c.compiler, c.linker, c.sanitize, c.installer} {
		if obj == nil {
			continue
		}
		if p, ok := obj.(subAndroidMkFooterInfoProducer); ok {
			p.prepareAndroidMKFooterInfo(config, c, entries)
		}
	}

	return &providerData
}

func androidMkWriteExtraTestConfigs(extraTestConfigs android.Paths, entries *android.AndroidMkInfo) {
	if len(extraTestConfigs) > 0 {
		entries.AddStrings("LOCAL_EXTRA_FULL_TEST_CONFIGS", extraTestConfigs.Strings()...)
	}
}

func makeOverrideModuleNames(ctx AndroidMkContext, overrides []string) []string {
	if ctx.Target().NativeBridge == android.NativeBridgeEnabled {
		var result []string
		for _, override := range overrides {
			result = append(result, override+NativeBridgeSuffix)
		}
		return result
	}

	return overrides
}

func (library *libraryDecorator) androidMkWriteExportedFlags(entries *android.AndroidMkInfo) {
	var exportedFlags []string
	var includeDirs android.Paths
	var systemIncludeDirs android.Paths
	var exportedDeps android.Paths

	if library.flagExporterInfo != nil {
		exportedFlags = library.flagExporterInfo.Flags
		includeDirs = library.flagExporterInfo.IncludeDirs
		systemIncludeDirs = library.flagExporterInfo.SystemIncludeDirs
		exportedDeps = library.flagExporterInfo.Deps
	} else {
		exportedFlags = library.flagExporter.flags
		includeDirs = library.flagExporter.dirs
		systemIncludeDirs = library.flagExporter.systemDirs
		exportedDeps = library.flagExporter.deps
	}
	for _, dir := range includeDirs {
		exportedFlags = append(exportedFlags, "-I"+dir.String())
	}
	for _, dir := range systemIncludeDirs {
		exportedFlags = append(exportedFlags, "-isystem "+dir.String())
	}
	if len(exportedFlags) > 0 {
		entries.AddStrings("LOCAL_EXPORT_CFLAGS", exportedFlags...)
	}
	if len(exportedDeps) > 0 {
		entries.AddStrings("LOCAL_EXPORT_C_INCLUDE_DEPS", exportedDeps.Strings()...)
	}
}

func (library *libraryDecorator) androidMkEntriesWriteAdditionalDependenciesForSourceAbiDiff(entries *android.AndroidMkInfo) {
	if !library.static() {
		entries.AddPaths("LOCAL_ADDITIONAL_DEPENDENCIES", library.sAbiDiff)
	}
}

// TODO(ccross): remove this once apex/androidmk.go is converted to AndroidMkEntries
func (library *libraryDecorator) androidMkWriteAdditionalDependenciesForSourceAbiDiff(w io.Writer) {
	if !library.static() {
		fmt.Fprintln(w, "LOCAL_ADDITIONAL_DEPENDENCIES +=", strings.Join(library.sAbiDiff.Strings(), " "))
	}
}

func (library *libraryDecorator) prepareAndroidMKProviderInfo(config android.Config, ctx AndroidMkContext, entries *android.AndroidMkInfo) {
	if library.static() {
		entries.Class = "STATIC_LIBRARIES"
	} else if library.shared() {
		entries.Class = "SHARED_LIBRARIES"
		entries.SetString("LOCAL_SOONG_TOC", library.toc().String())
		if !library.BuildStubs() && library.unstrippedOutputFile != nil {
			entries.SetString("LOCAL_SOONG_UNSTRIPPED_BINARY", library.unstrippedOutputFile.String())
		}
		if len(library.Properties.Overrides) > 0 {
			entries.SetString("LOCAL_OVERRIDES_MODULES", strings.Join(makeOverrideModuleNames(ctx, library.Properties.Overrides), " "))
		}
		if len(library.postInstallCmds) > 0 {
			entries.SetString("LOCAL_POST_INSTALL_CMD", strings.Join(library.postInstallCmds, "&& "))
		}
	} else if library.header() {
		entries.Class = "HEADER_LIBRARIES"
	}

	library.androidMkWriteExportedFlags(entries)
	library.androidMkEntriesWriteAdditionalDependenciesForSourceAbiDiff(entries)

	if entries.OutputFile.Valid() {
		_, _, ext := android.SplitFileExt(entries.OutputFile.Path().Base())
		entries.SetString("LOCAL_BUILT_MODULE_STEM", "$(LOCAL_MODULE)"+ext)
	}

	if library.coverageOutputFile.Valid() {
		entries.SetString("LOCAL_PREBUILT_COVERAGE_ARCHIVE", library.coverageOutputFile.String())
	}

	if library.shared() && !library.BuildStubs() {
		ctx.subAndroidMk(config, entries, library.baseInstaller)
	} else {
		if library.BuildStubs() && library.StubsVersion() != "" {
			entries.SubName = "." + library.StubsVersion()
		}
		// library.makeUninstallable() depends on this to bypass HideFromMake() for
		// static libraries.
		entries.SetBool("LOCAL_UNINSTALLABLE_MODULE", true)
		if library.BuildStubs() {
			entries.SetBool("LOCAL_NO_NOTICE_FILE", true)
		}
	}
	// If a library providing a stub is included in an APEX, the private APIs of the library
	// is accessible only inside the APEX. From outside of the APEX, clients can only use the
	// public APIs via the stub. To enforce this, the (latest version of the) stub gets the
	// name of the library. The impl library instead gets the `.bootstrap` suffix to so that
	// they can be exceptionally used directly when APEXes are not available (e.g. during the
	// very early stage in the boot process).
	if len(library.Properties.Stubs.Versions) > 0 && !ctx.Host() && ctx.NotInPlatform() &&
		!ctx.InRamdisk() && !ctx.InVendorRamdisk() && !ctx.InRecovery() && !ctx.InVendorOrProduct() && !ctx.static() {
		if library.BuildStubs() && library.isLatestStubVersion() {
			entries.SubName = ""
		}
		if !library.BuildStubs() {
			entries.SubName = ".bootstrap"
		}
	}
}

func (object *objectLinker) prepareAndroidMKProviderInfo(config android.Config, ctx AndroidMkContext, entries *android.AndroidMkInfo) {
	entries.Class = "STATIC_LIBRARIES"
}

func (object *objectLinker) prepareAndroidMKFooterInfo(config android.Config, ctx AndroidMkContext, entries *android.AndroidMkInfo) {
	out := entries.OutputFile.Path()
	name := ctx.BaseModuleName()
	if entries.OverrideName != "" {
		name = entries.OverrideName
	}

	prefix := ""
	if ctx.ArchSpecific() {
		switch ctx.Os().Class {
		case android.Host:
			if ctx.Target().HostCross {
				prefix = "HOST_CROSS_"
			} else {
				prefix = "HOST_"
			}
		case android.Device:
			prefix = "TARGET_"

		}

		if ctx.Arch().ArchType != config.Targets[ctx.Os()][0].Arch.ArchType {
			prefix = "2ND_" + prefix
		}
	}

	varname := fmt.Sprintf("SOONG_%sOBJECT_%s%s", prefix, name, entries.SubName)

	entries.FooterStrings = append(entries.FooterStrings,
		fmt.Sprintf("\n%s := %s\n.KATI_READONLY: %s", varname, out.String(), varname))
}

func (test *testDecorator) prepareAndroidMKProviderInfo(config android.Config, ctx AndroidMkContext, entries *android.AndroidMkInfo) {
	if len(test.InstallerProperties.Test_suites) > 0 {
		entries.AddCompatibilityTestSuites(test.InstallerProperties.Test_suites...)
	}
}

func (binary *binaryDecorator) prepareAndroidMKProviderInfo(config android.Config, ctx AndroidMkContext, entries *android.AndroidMkInfo) {
	ctx.subAndroidMk(config, entries, binary.baseInstaller)

	entries.Class = "EXECUTABLES"
	entries.SetString("LOCAL_SOONG_UNSTRIPPED_BINARY", binary.unstrippedOutputFile.String())
	if len(binary.symlinks) > 0 {
		entries.AddStrings("LOCAL_MODULE_SYMLINKS", binary.symlinks...)
	}

	if binary.coverageOutputFile.Valid() {
		entries.SetString("LOCAL_PREBUILT_COVERAGE_ARCHIVE", binary.coverageOutputFile.String())
	}

	if len(binary.Properties.Overrides) > 0 {
		entries.SetString("LOCAL_OVERRIDES_MODULES", strings.Join(makeOverrideModuleNames(ctx, binary.Properties.Overrides), " "))
	}
	if len(binary.postInstallCmds) > 0 {
		entries.SetString("LOCAL_POST_INSTALL_CMD", strings.Join(binary.postInstallCmds, "&& "))
	}
}

func (benchmark *benchmarkDecorator) prepareAndroidMKProviderInfo(config android.Config, ctx AndroidMkContext, entries *android.AndroidMkInfo) {
	ctx.subAndroidMk(config, entries, benchmark.binaryDecorator)
	entries.Class = "NATIVE_TESTS"
	if len(benchmark.Properties.Test_suites) > 0 {
		entries.AddCompatibilityTestSuites(benchmark.Properties.Test_suites...)
	}
	if benchmark.testConfig != nil {
		entries.SetString("LOCAL_FULL_TEST_CONFIG", benchmark.testConfig.String())
	}
	entries.SetBool("LOCAL_NATIVE_BENCHMARK", true)
	if !BoolDefault(benchmark.Properties.Auto_gen_config, true) {
		entries.SetBool("LOCAL_DISABLE_AUTO_GENERATE_TEST_CONFIG", true)
	}
}

func (test *testBinary) prepareAndroidMKProviderInfo(config android.Config, ctx AndroidMkContext, entries *android.AndroidMkInfo) {
	ctx.subAndroidMk(config, entries, test.binaryDecorator)
	ctx.subAndroidMk(config, entries, test.testDecorator)

	entries.Class = "NATIVE_TESTS"
	if test.testConfig != nil {
		entries.SetString("LOCAL_FULL_TEST_CONFIG", test.testConfig.String())
	}
	if !BoolDefault(test.Properties.Auto_gen_config, true) {
		entries.SetBool("LOCAL_DISABLE_AUTO_GENERATE_TEST_CONFIG", true)
	}
	entries.AddStrings("LOCAL_TEST_MAINLINE_MODULES", test.Properties.Test_mainline_modules...)

	entries.SetBoolIfTrue("LOCAL_COMPATIBILITY_PER_TESTCASE_DIRECTORY", Bool(test.Properties.Per_testcase_directory))
	if len(test.Properties.Data_bins) > 0 {
		entries.AddStrings("LOCAL_TEST_DATA_BINS", test.Properties.Data_bins...)
	}

	test.Properties.Test_options.CommonTestOptions.SetAndroidMkInfoEntries(entries)

	androidMkWriteExtraTestConfigs(test.extraTestConfigs, entries)
}

func (fuzz *fuzzBinary) prepareAndroidMKProviderInfo(config android.Config, ctx AndroidMkContext, entries *android.AndroidMkInfo) {
	ctx.subAndroidMk(config, entries, fuzz.binaryDecorator)

	entries.SetBool("LOCAL_IS_FUZZ_TARGET", true)
	if fuzz.installedSharedDeps != nil {
		// TOOD: move to install dep
		entries.AddStrings("LOCAL_FUZZ_INSTALLED_SHARED_DEPS", fuzz.installedSharedDeps...)
	}
}

func (test *testLibrary) prepareAndroidMKProviderInfo(config android.Config, ctx AndroidMkContext, entries *android.AndroidMkInfo) {
	ctx.subAndroidMk(config, entries, test.libraryDecorator)
	ctx.subAndroidMk(config, entries, test.testDecorator)
}

func (installer *baseInstaller) prepareAndroidMKProviderInfo(config android.Config, ctx AndroidMkContext, entries *android.AndroidMkInfo) {
	if installer.path == (android.InstallPath{}) {
		return
	}

	path, file := filepath.Split(installer.path.String())
	stem, suffix, _ := android.SplitFileExt(file)
	entries.SetString("LOCAL_MODULE_SUFFIX", suffix)
	entries.SetString("LOCAL_MODULE_PATH", path)
	entries.SetString("LOCAL_MODULE_STEM", stem)
}

func (c *stubDecorator) prepareAndroidMKProviderInfo(config android.Config, ctx AndroidMkContext, entries *android.AndroidMkInfo) {
	entries.SubName = ndkLibrarySuffix + "." + c.apiLevel.String()
	entries.Class = "SHARED_LIBRARIES"

	if !c.BuildStubs() {
		entries.Disabled = true
		return
	}

	path, file := filepath.Split(c.installPath.String())
	stem, suffix, _ := android.SplitFileExt(file)
	entries.SetString("LOCAL_MODULE_SUFFIX", suffix)
	entries.SetString("LOCAL_MODULE_PATH", path)
	entries.SetString("LOCAL_MODULE_STEM", stem)
	entries.SetBool("LOCAL_NO_NOTICE_FILE", true)
	if c.parsedCoverageXmlPath.String() != "" {
		entries.SetString("SOONG_NDK_API_XML", "$(SOONG_NDK_API_XML) "+c.parsedCoverageXmlPath.String())
	}
	entries.SetBool("LOCAL_UNINSTALLABLE_MODULE", true) // Stubs should not be installed
}

func (c *vndkPrebuiltLibraryDecorator) prepareAndroidMKProviderInfo(config android.Config, ctx AndroidMkContext, entries *android.AndroidMkInfo) {
	entries.Class = "SHARED_LIBRARIES"

	entries.SubName = c.androidMkSuffix

	c.libraryDecorator.androidMkWriteExportedFlags(entries)

	// Specifying stem is to pass check_elf_files when vendor modules link against vndk prebuilt.
	// We can't use install path because VNDKs are not installed. Instead, Srcs is directly used.
	_, file := filepath.Split(c.properties.Srcs[0])
	stem, suffix, ext := android.SplitFileExt(file)
	entries.SetString("LOCAL_BUILT_MODULE_STEM", "$(LOCAL_MODULE)"+ext)
	entries.SetString("LOCAL_MODULE_SUFFIX", suffix)
	entries.SetString("LOCAL_MODULE_STEM", stem)

	if c.tocFile.Valid() {
		entries.SetString("LOCAL_SOONG_TOC", c.tocFile.String())
	}

	// VNDK libraries available to vendor are not installed because
	// they are packaged in VNDK APEX and installed by APEX packages (apex/apex.go)
	entries.SetBool("LOCAL_UNINSTALLABLE_MODULE", true)
}

func (p *prebuiltLinker) prepareAndroidMKProviderInfo(config android.Config, ctx AndroidMkContext, entries *android.AndroidMkInfo) {
	if p.properties.Check_elf_files != nil {
		entries.SetBool("LOCAL_CHECK_ELF_FILES", *p.properties.Check_elf_files)
	} else {
		// soong_cc_rust_prebuilt.mk does not include check_elf_file.mk by default
		// because cc_library_shared and cc_binary use soong_cc_rust_prebuilt.mk as well.
		// In order to turn on prebuilt ABI checker, set `LOCAL_CHECK_ELF_FILES` to
		// true if `p.properties.Check_elf_files` is not specified.
		entries.SetBool("LOCAL_CHECK_ELF_FILES", true)
	}
}

func (p *prebuiltLibraryLinker) prepareAndroidMKProviderInfo(config android.Config, ctx AndroidMkContext, entries *android.AndroidMkInfo) {
	ctx.subAndroidMk(config, entries, p.libraryDecorator)
	if p.shared() {
		ctx.subAndroidMk(config, entries, &p.prebuiltLinker)
		androidMkWritePrebuiltOptions(p.baseLinker, entries)
	}
}

func (p *prebuiltBinaryLinker) prepareAndroidMKProviderInfo(config android.Config, ctx AndroidMkContext, entries *android.AndroidMkInfo) {
	ctx.subAndroidMk(config, entries, p.binaryDecorator)
	ctx.subAndroidMk(config, entries, &p.prebuiltLinker)
	androidMkWritePrebuiltOptions(p.baseLinker, entries)
}

func androidMkWritePrebuiltOptions(linker *baseLinker, entries *android.AndroidMkInfo) {
	allow := linker.Properties.Allow_undefined_symbols
	if allow != nil {
		entries.SetBool("LOCAL_ALLOW_UNDEFINED_SYMBOLS", *allow)
	}
	ignore := linker.Properties.Ignore_max_page_size
	if ignore != nil {
		entries.SetBool("LOCAL_IGNORE_MAX_PAGE_SIZE", *ignore)
	}
}
