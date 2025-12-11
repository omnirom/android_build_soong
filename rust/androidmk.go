// Copyright 2019 The Android Open Source Project
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

package rust

import (
	"fmt"
	"path/filepath"

	"android/soong/android"
)

type AndroidMkContext interface {
	Name() string
	Target() android.Target
	SubAndroidMk(*android.AndroidMkInfo, any)
}

type SubAndroidMkProvider interface {
	AndroidMk(AndroidMkContext, *android.AndroidMkInfo)
}

func (mod *Module) SubAndroidMk(data *android.AndroidMkInfo, obj any) {
	if mod.subAndroidMkOnce == nil {
		mod.subAndroidMkOnce = make(map[SubAndroidMkProvider]bool)
	}
	if androidmk, ok := obj.(SubAndroidMkProvider); ok {
		if !mod.subAndroidMkOnce[androidmk] {
			mod.subAndroidMkOnce[androidmk] = true
			androidmk.AndroidMk(mod, data)
		}
	}
}

func (mod *Module) AndroidMkSuffix() string {
	return mod.Properties.RustSubName + mod.Properties.SubName
}

func (mod *Module) PrepareAndroidMKProviderInfo(config android.Config) *android.AndroidMkProviderInfo {
	info := &android.AndroidMkProviderInfo{}
	info.PrimaryInfo = android.AndroidMkInfo{
		OutputFile: android.OptionalPathForPath(mod.UnstrippedOutputFile()),
		Include:    "$(BUILD_SYSTEM)/soong_cc_rust_prebuilt.mk",
	}
	info.PrimaryInfo.AddStrings("LOCAL_RLIB_LIBRARIES", mod.Properties.AndroidMkRlibs...)
	info.PrimaryInfo.AddStrings("LOCAL_DYLIB_LIBRARIES", mod.Properties.AndroidMkDylibs...)
	info.PrimaryInfo.AddStrings("LOCAL_PROC_MACRO_LIBRARIES", mod.Properties.AndroidMkProcMacroLibs...)
	info.PrimaryInfo.AddStrings("LOCAL_SHARED_LIBRARIES", mod.transitiveAndroidMkSharedLibs.ToList()...)
	info.PrimaryInfo.AddStrings("LOCAL_STATIC_LIBRARIES", mod.Properties.AndroidMkStaticLibs...)
	info.PrimaryInfo.AddStrings("LOCAL_HEADER_LIBRARIES", mod.Properties.AndroidMkHeaderLibs...)
	info.PrimaryInfo.AddStrings("LOCAL_SOONG_LINK_TYPE", mod.makeLinkType)
	if mod.InVendor() {
		info.PrimaryInfo.SetBool("LOCAL_IN_VENDOR", true)
	} else if mod.InProduct() {
		info.PrimaryInfo.SetBool("LOCAL_IN_PRODUCT", true)
	}
	if mod.Properties.SdkAndPlatformVariantVisibleToMake {
		// Add the unsuffixed name to SOONG_SDK_VARIANT_MODULES so that Make can rewrite
		// dependencies to the .sdk suffix when building a module that uses the SDK.
		info.PrimaryInfo.SetString("SOONG_SDK_VARIANT_MODULES",
			"$(SOONG_SDK_VARIANT_MODULES) $(patsubst %.sdk,%,$(LOCAL_MODULE))")
	}
	info.PrimaryInfo.SetBoolIfTrue("LOCAL_UNINSTALLABLE_MODULE", mod.IsSkipInstall())

	// The footer info comes at the last step, previously it was achieved by
	// calling some extra footer function that were added earlier. Because we no
	// longer use these extra footer functions, we need to put this step at the
	// last one.
	if mod.Properties.IsSdkVariant && mod.Properties.SdkAndPlatformVariantVisibleToMake &&
		mod.Shared() {
		// Using the SDK variant as a JNI library needs a copy of the .so that
		// is not named .sdk.so so that it can be packaged into the APK with
		// the right name.
		info.PrimaryInfo.FooterStrings = append(info.PrimaryInfo.FooterStrings,
			fmt.Sprintf("%s %s %s", "$(eval $(call copy-one-file,",
				"$(LOCAL_BUILT_MODULE),",
				"$(patsubst %.sdk.so,%.so,$(LOCAL_BUILT_MODULE))))"))
	}

	if mod.compiler != nil && !mod.compiler.Disabled() {
		mod.SubAndroidMk(&info.PrimaryInfo, mod.compiler)
	} else if mod.sourceProvider != nil {
		// If the compiler is disabled, this is a SourceProvider.
		mod.SubAndroidMk(&info.PrimaryInfo, mod.sourceProvider)
	}

	if mod.sanitize != nil {
		mod.SubAndroidMk(&info.PrimaryInfo, mod.sanitize)
	}

	info.PrimaryInfo.SubName += mod.AndroidMkSuffix()

	return info
}

func (binary *binaryDecorator) AndroidMk(ctx AndroidMkContext, ret *android.AndroidMkInfo) {
	ctx.SubAndroidMk(ret, binary.baseCompiler)

	ret.Class = "EXECUTABLES"
}

func (object *objectDecorator) AndroidMk(ctx AndroidMkContext, ret *android.AndroidMkInfo) {
	ctx.SubAndroidMk(ret, object.baseCompiler)

	ret.Class = "STATIC_LIBRARIES"
}

func (test *testDecorator) AndroidMk(ctx AndroidMkContext, ret *android.AndroidMkInfo) {
	ctx.SubAndroidMk(ret, test.binaryDecorator)

	ret.Class = "NATIVE_TESTS"
	ret.AddCompatibilityTestSuites(test.Properties.Test_suites...)
	if test.testConfig != nil {
		ret.SetString("LOCAL_FULL_TEST_CONFIG", test.testConfig.String())
	}
	ret.SetBoolIfTrue("LOCAL_DISABLE_AUTO_GENERATE_TEST_CONFIG", !BoolDefault(test.Properties.Auto_gen_config, true))
	if test.Properties.Data_bins != nil {
		ret.AddStrings("LOCAL_TEST_DATA_BINS", test.Properties.Data_bins...)
	}

	test.Properties.Test_options.SetAndroidMkInfoEntries(ret)
}

func (benchmark *benchmarkDecorator) AndroidMk(ctx AndroidMkContext, ret *android.AndroidMkInfo) {
	benchmark.binaryDecorator.AndroidMk(ctx, ret)
	ret.Class = "NATIVE_TESTS"
	ret.AddCompatibilityTestSuites(benchmark.Properties.Test_suites...)
	if benchmark.testConfig != nil {
		ret.SetString("LOCAL_FULL_TEST_CONFIG", benchmark.testConfig.String())
	}
	ret.SetBool("LOCAL_NATIVE_BENCHMARK", true)
	ret.SetBoolIfTrue("LOCAL_DISABLE_AUTO_GENERATE_TEST_CONFIG", !BoolDefault(benchmark.Properties.Auto_gen_config, true))
}

func (library *libraryDecorator) AndroidMk(ctx AndroidMkContext, ret *android.AndroidMkInfo) {
	ctx.SubAndroidMk(ret, library.baseCompiler)

	if library.rlib() {
		ret.Class = "RLIB_LIBRARIES"
	} else if library.dylib() {
		ret.Class = "DYLIB_LIBRARIES"
	} else if library.static() {
		ret.Class = "STATIC_LIBRARIES"
	} else if library.shared() {
		ret.Class = "SHARED_LIBRARIES"
	}
	if library.tocFile.Valid() {
		ret.SetString("LOCAL_SOONG_TOC", library.tocFile.String())
	}
	if ret.OutputFile.Valid() {
		_, _, ext := android.SplitFileExt(ret.OutputFile.Path().Base())
		ret.SetString("LOCAL_BUILT_MODULE_STEM", "$(LOCAL_MODULE)"+ext)
	}
}

func (procMacro *procMacroDecorator) AndroidMk(ctx AndroidMkContext, ret *android.AndroidMkInfo) {
	ctx.SubAndroidMk(ret, procMacro.baseCompiler)

	ret.Class = "PROC_MACRO_LIBRARIES"
}

func (sourceProvider *BaseSourceProvider) AndroidMk(ctx AndroidMkContext, ret *android.AndroidMkInfo) {
	outFile := sourceProvider.OutputFiles[0]
	ret.Class = "ETC"
	ret.OutputFile = android.OptionalPathForPath(outFile)
	ret.SubName += sourceProvider.subName
	_, file := filepath.Split(outFile.String())
	stem, suffix, _ := android.SplitFileExt(file)
	ret.SetString("LOCAL_MODULE_SUFFIX", suffix)
	ret.SetString("LOCAL_MODULE_STEM", stem)
	ret.SetBool("LOCAL_UNINSTALLABLE_MODULE", true)
}

func (bindgen *bindgenDecorator) AndroidMk(ctx AndroidMkContext, ret *android.AndroidMkInfo) {
	ctx.SubAndroidMk(ret, bindgen.BaseSourceProvider)
}

func (proto *protobufDecorator) AndroidMk(ctx AndroidMkContext, ret *android.AndroidMkInfo) {
	ctx.SubAndroidMk(ret, proto.BaseSourceProvider)
}

func (compiler *baseCompiler) AndroidMk(ctx AndroidMkContext, ret *android.AndroidMkInfo) {
	if compiler.path == (android.InstallPath{}) {
		return
	}

	if compiler.strippedOutputFile.Valid() {
		ret.OutputFile = compiler.strippedOutputFile
	}

	ret.SetPath("LOCAL_SOONG_UNSTRIPPED_BINARY", compiler.unstrippedOutputFile)
	path, file := filepath.Split(compiler.path.String())
	stem, suffix, _ := android.SplitFileExt(file)
	ret.SetString("LOCAL_MODULE_SUFFIX", suffix)
	ret.SetString("LOCAL_MODULE_PATH", path)
	ret.SetString("LOCAL_MODULE_STEM", stem)
}

func (fuzz *fuzzDecorator) AndroidMk(ctx AndroidMkContext, ret *android.AndroidMkInfo) {
	ctx.SubAndroidMk(ret, fuzz.binaryDecorator)

	ret.SetBool("LOCAL_IS_FUZZ_TARGET", true)
}
