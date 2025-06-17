// Copyright 2019 Google Inc. All rights reserved.
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

package java

import (
	"fmt"

	"android/soong/android"
	"android/soong/java/config"
	"android/soong/tradefed"

	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"
)

func init() {
	RegisterRobolectricBuildComponents(android.InitRegistrationContext)
}

type RobolectricRuntimesInfo struct {
	Runtimes []android.InstallPath
}

var RobolectricRuntimesInfoProvider = blueprint.NewProvider[RobolectricRuntimesInfo]()

type roboRuntimeOnlyDependencyTag struct {
	blueprint.BaseDependencyTag
}

var roboRuntimeOnlyDepTag roboRuntimeOnlyDependencyTag

// Mark this tag so dependencies that use it are excluded from visibility enforcement.
func (t roboRuntimeOnlyDependencyTag) ExcludeFromVisibilityEnforcement() {}

var _ android.ExcludeFromVisibilityEnforcementTag = roboRuntimeOnlyDepTag

func RegisterRobolectricBuildComponents(ctx android.RegistrationContext) {
	ctx.RegisterModuleType("android_robolectric_test", RobolectricTestFactory)
	ctx.RegisterModuleType("android_robolectric_runtimes", robolectricRuntimesFactory)
}

var robolectricDefaultLibs = []string{
	"mockito-robolectric-prebuilt",
	"truth",
	// TODO(ccross): this is not needed at link time
	"junitxml",
}

const robolectricCurrentLib = "Robolectric_all-target"
const clearcutJunitLib = "ClearcutJunitListenerAar"
const robolectricPrebuiltLibPattern = "platform-robolectric-%s-prebuilt"

var (
	roboCoverageLibsTag = dependencyTag{name: "roboCoverageLibs"}
	roboRuntimesTag     = dependencyTag{name: "roboRuntimes"}
)

type robolectricProperties struct {
	// The name of the android_app module that the tests will run against.
	Instrumentation_for *string

	// Additional libraries for which coverage data should be generated
	Coverage_libs []string

	Test_options struct {
		// Timeout in seconds when running the tests.
		Timeout *int64

		// Number of shards to use when running the tests.
		Shards *int64
	}

	// Use /external/robolectric rather than /external/robolectric-shadows as the version of robolectric
	// to use.  /external/robolectric closely tracks github's master, and will fully replace /external/robolectric-shadows
	Upstream *bool

	// Use strict mode to limit access of Robolectric API directly. See go/roboStrictMode
	Strict_mode *bool

	Jni_libs proptools.Configurable[[]string]
}

type robolectricTest struct {
	Library

	robolectricProperties robolectricProperties
	testProperties        testProperties

	testConfig android.Path
	data       android.Paths

	forceOSType   android.OsType
	forceArchType android.ArchType
}

func (r *robolectricTest) TestSuites() []string {
	return r.testProperties.Test_suites
}

var _ android.TestSuiteModule = (*robolectricTest)(nil)

func (r *robolectricTest) DepsMutator(ctx android.BottomUpMutatorContext) {
	r.Library.DepsMutator(ctx)

	if r.robolectricProperties.Instrumentation_for != nil {
		ctx.AddVariationDependencies(nil, instrumentationForTag, String(r.robolectricProperties.Instrumentation_for))
	} else {
		ctx.PropertyErrorf("instrumentation_for", "missing required instrumented module")
	}

	ctx.AddVariationDependencies(nil, roboRuntimeOnlyDepTag, clearcutJunitLib)

	if proptools.BoolDefault(r.robolectricProperties.Strict_mode, true) {
		ctx.AddVariationDependencies(nil, roboRuntimeOnlyDepTag, robolectricCurrentLib)
	} else {
		ctx.AddVariationDependencies(nil, staticLibTag, robolectricCurrentLib)
	}

	ctx.AddVariationDependencies(nil, staticLibTag, robolectricDefaultLibs...)

	ctx.AddVariationDependencies(nil, roboCoverageLibsTag, r.robolectricProperties.Coverage_libs...)

	ctx.AddFarVariationDependencies(ctx.Config().BuildOSCommonTarget.Variations(),
		roboRuntimesTag, "robolectric-android-all-prebuilts")

	for _, lib := range r.robolectricProperties.Jni_libs.GetOrDefault(ctx, nil) {
		ctx.AddVariationDependencies(ctx.Config().BuildOSTarget.Variations(), jniLibTag, lib)
	}
}

func (r *robolectricTest) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	r.forceOSType = ctx.Config().BuildOS
	r.forceArchType = ctx.Config().BuildArch

	var extraTestRunnerOptions []tradefed.Option
	extraTestRunnerOptions = append(extraTestRunnerOptions, tradefed.Option{Name: "java-flags", Value: "-Drobolectric=true"})
	if proptools.BoolDefault(r.robolectricProperties.Strict_mode, true) {
		extraTestRunnerOptions = append(extraTestRunnerOptions, tradefed.Option{Name: "java-flags", Value: "-Drobolectric.strict.mode=true"})
	}

	var extraOptions []tradefed.Option
	var javaHome = ctx.Config().Getenv("ANDROID_JAVA_HOME")
	extraOptions = append(extraOptions, tradefed.Option{Name: "java-folder", Value: javaHome})

	r.testConfig = tradefed.AutoGenTestConfig(ctx, tradefed.AutoGenTestConfigOptions{
		TestConfigProp:          r.testProperties.Test_config,
		TestConfigTemplateProp:  r.testProperties.Test_config_template,
		TestSuites:              r.testProperties.Test_suites,
		OptionsForAutogenerated: extraOptions,
		TestRunnerOptions:       extraTestRunnerOptions,
		AutoGenConfig:           r.testProperties.Auto_gen_config,
		DeviceTemplate:          "${RobolectricTestConfigTemplate}",
		HostTemplate:            "${RobolectricTestConfigTemplate}",
	})
	r.data = android.PathsForModuleSrc(ctx, r.testProperties.Data)
	r.data = append(r.data, android.PathsForModuleSrc(ctx, r.testProperties.Device_common_data)...)
	r.data = append(r.data, android.PathsForModuleSrc(ctx, r.testProperties.Device_first_data)...)
	r.data = append(r.data, android.PathsForModuleSrc(ctx, r.testProperties.Device_first_prefer32_data)...)
	r.data = append(r.data, android.PathsForModuleSrc(ctx, r.testProperties.Host_common_data)...)

	var ok bool
	var instrumentedApp *JavaInfo
	var appInfo *AppInfo

	// TODO: this inserts paths to built files into the test, it should really be inserting the contents.
	instrumented := ctx.GetDirectDepsProxyWithTag(instrumentationForTag)

	if len(instrumented) == 1 {
		appInfo, ok = android.OtherModuleProvider(ctx, instrumented[0], AppInfoProvider)
		if !ok {
			ctx.PropertyErrorf("instrumentation_for", "dependency must be an android_app")
		}
		instrumentedApp = android.OtherModuleProviderOrDefault(ctx, instrumented[0], JavaInfoProvider)
	} else if !ctx.Config().AllowMissingDependencies() {
		panic(fmt.Errorf("expected exactly 1 instrumented dependency, got %d", len(instrumented)))
	}

	var resourceApk android.Path
	var manifest android.Path
	if appInfo != nil {
		manifest = appInfo.MergedManifestFile
		resourceApk = instrumentedApp.OutputFile
	}

	roboTestConfigJar := android.PathForModuleOut(ctx, "robolectric_samedir", "samedir_config.jar")
	generateSameDirRoboTestConfigJar(ctx, roboTestConfigJar)

	extraCombinedJars := android.Paths{roboTestConfigJar}

	handleLibDeps := func(dep android.ModuleProxy) {
		if !android.InList(ctx.OtherModuleName(dep), config.FrameworkLibraries) {
			if m, ok := android.OtherModuleProvider(ctx, dep, JavaInfoProvider); ok {
				extraCombinedJars = append(extraCombinedJars, m.ImplementationAndResourcesJars...)
			}
		}
	}

	for _, dep := range ctx.GetDirectDepsProxyWithTag(libTag) {
		handleLibDeps(dep)
	}
	for _, dep := range ctx.GetDirectDepsProxyWithTag(sdkLibTag) {
		handleLibDeps(dep)
	}
	// handle the runtimeOnly tag for strict_mode
	for _, dep := range ctx.GetDirectDepsProxyWithTag(roboRuntimeOnlyDepTag) {
		handleLibDeps(dep)
	}

	if appInfo != nil {
		extraCombinedJars = append(extraCombinedJars, instrumentedApp.ImplementationAndResourcesJars...)
	}

	r.stem = proptools.StringDefault(r.overridableProperties.Stem, ctx.ModuleName())
	r.classLoaderContexts = r.usesLibrary.classLoaderContextForUsesLibDeps(ctx)
	r.dexpreopter.disableDexpreopt()
	javaInfo := r.compile(ctx, nil, nil, nil, extraCombinedJars)

	installPath := android.PathForModuleInstall(ctx, r.BaseModuleName())
	var installDeps android.InstallPaths

	for _, data := range r.data {
		installedData := ctx.InstallFile(installPath, data.Rel(), data)
		installDeps = append(installDeps, installedData)
	}

	if manifest != nil {
		r.data = append(r.data, manifest)
		installedManifest := ctx.InstallFile(installPath, ctx.ModuleName()+"-AndroidManifest.xml", manifest)
		installDeps = append(installDeps, installedManifest)
	}

	if resourceApk != nil {
		r.data = append(r.data, resourceApk)
		installedResourceApk := ctx.InstallFile(installPath, ctx.ModuleName()+".apk", resourceApk)
		installDeps = append(installDeps, installedResourceApk)
	}

	runtimes := ctx.GetDirectDepProxyWithTag("robolectric-android-all-prebuilts", roboRuntimesTag)
	for _, runtime := range android.OtherModuleProviderOrDefault(ctx, runtimes, RobolectricRuntimesInfoProvider).Runtimes {
		installDeps = append(installDeps, runtime)
	}

	installedConfig := ctx.InstallFile(installPath, ctx.ModuleName()+".config", r.testConfig)
	installDeps = append(installDeps, installedConfig)

	soInstallPath := installPath.Join(ctx, getLibPath(r.forceArchType))
	for _, jniLib := range collectTransitiveJniDeps(ctx) {
		installJni := ctx.InstallFile(soInstallPath, jniLib.path.Base(), jniLib.path)
		installDeps = append(installDeps, installJni)
	}

	r.installFile = ctx.InstallFile(installPath, ctx.ModuleName()+".jar", r.outputFile, installDeps...)

	if javaInfo != nil {
		setExtraJavaInfo(ctx, r, javaInfo)
		android.SetProvider(ctx, JavaInfoProvider, javaInfo)
	}

	moduleInfoJSON := r.javaLibraryModuleInfoJSON(ctx)
	if _, ok := r.testConfig.(android.WritablePath); ok {
		moduleInfoJSON.AutoTestConfig = []string{"true"}
	}
	if r.testConfig != nil {
		moduleInfoJSON.TestConfig = append(moduleInfoJSON.TestConfig, r.testConfig.String())
	}
	if len(r.testProperties.Test_suites) > 0 {
		moduleInfoJSON.CompatibilitySuites = append(moduleInfoJSON.CompatibilitySuites, r.testProperties.Test_suites...)
	} else {
		moduleInfoJSON.CompatibilitySuites = append(moduleInfoJSON.CompatibilitySuites, "null-suite")
	}

	android.SetProvider(ctx, android.TestSuiteInfoProvider, android.TestSuiteInfo{
		TestSuites: r.TestSuites(),
	})

	android.SetProvider(ctx, android.TestOnlyProviderKey, android.TestModuleInformation{
		TestOnly:       Bool(r.sourceProperties.Test_only),
		TopLevelTarget: r.sourceProperties.Top_level_test_target,
	})
}

func generateSameDirRoboTestConfigJar(ctx android.ModuleContext, outputFile android.ModuleOutPath) {
	rule := android.NewRuleBuilder(pctx, ctx)

	outputDir := outputFile.InSameDir(ctx)
	configFile := outputDir.Join(ctx, "com/android/tools/test_config.properties")
	rule.Temporary(configFile)
	rule.Command().Text("rm -f").Output(outputFile).Output(configFile)
	rule.Command().Textf("mkdir -p $(dirname %s)", configFile.String())
	rule.Command().
		Text("(").
		Textf(`echo "android_merged_manifest=%s-AndroidManifest.xml" &&`, ctx.ModuleName()).
		Textf(`echo "android_resource_apk=%s.apk"`, ctx.ModuleName()).
		Text(") >>").Output(configFile)
	rule.Command().
		BuiltTool("soong_zip").
		FlagWithArg("-C ", outputDir.String()).
		FlagWithInput("-f ", configFile).
		FlagWithOutput("-o ", outputFile)

	rule.Build("generate_test_config_samedir", "generate test_config.properties")
}

func (r *robolectricTest) AndroidMkEntries() []android.AndroidMkEntries {
	entriesList := r.Library.AndroidMkEntries()
	entries := &entriesList[0]
	entries.ExtraEntries = append(entries.ExtraEntries,
		func(ctx android.AndroidMkExtraEntriesContext, entries *android.AndroidMkEntries) {
			entries.SetBool("LOCAL_UNINSTALLABLE_MODULE", true)
			entries.AddStrings("LOCAL_COMPATIBILITY_SUITE", "robolectric-tests")
			if r.testConfig != nil {
				entries.SetPath("LOCAL_FULL_TEST_CONFIG", r.testConfig)
			}
		})
	return entriesList
}

// An android_robolectric_test module compiles tests against the Robolectric framework that can run on the local host
// instead of on a device.
func RobolectricTestFactory() android.Module {
	module := &robolectricTest{}

	module.addHostProperties()
	module.AddProperties(
		&module.Module.deviceProperties,
		&module.robolectricProperties,
		&module.testProperties)

	module.Module.dexpreopter.isTest = true
	module.Module.linter.properties.Lint.Test_module_type = proptools.BoolPtr(true)
	module.Module.sourceProperties.Test_only = proptools.BoolPtr(true)
	module.Module.sourceProperties.Top_level_test_target = true
	module.testProperties.Test_suites = []string{"robolectric-tests"}

	InitJavaModule(module, android.DeviceSupported)
	return module
}

func (r *robolectricTest) InstallInTestcases() bool { return true }
func (r *robolectricTest) InstallForceOS() (*android.OsType, *android.ArchType) {
	return &r.forceOSType, &r.forceArchType
}

func robolectricRuntimesFactory() android.Module {
	module := &robolectricRuntimes{}
	module.AddProperties(&module.props)
	android.InitAndroidArchModule(module, android.HostSupportedNoCross, android.MultilibCommon)
	return module
}

type robolectricRuntimesProperties struct {
	Jars []string `android:"path"`
	Lib  *string
}

type robolectricRuntimes struct {
	android.ModuleBase

	props robolectricRuntimesProperties

	runtimes []android.InstallPath

	forceOSType   android.OsType
	forceArchType android.ArchType
}

func (r *robolectricRuntimes) TestSuites() []string {
	return []string{"robolectric-tests"}
}

var _ android.TestSuiteModule = (*robolectricRuntimes)(nil)

func (r *robolectricRuntimes) DepsMutator(ctx android.BottomUpMutatorContext) {
	if !ctx.Config().AlwaysUsePrebuiltSdks() && r.props.Lib != nil {
		ctx.AddVariationDependencies(nil, libTag, String(r.props.Lib))
	}
}

func (r *robolectricRuntimes) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	if ctx.Target().Os != ctx.Config().BuildOSCommonTarget.Os {
		return
	}

	r.forceOSType = ctx.Config().BuildOS
	r.forceArchType = ctx.Config().BuildArch

	files := android.PathsForModuleSrc(ctx, r.props.Jars)

	androidAllDir := android.PathForModuleInstall(ctx, "android-all")
	for _, from := range files {
		installedRuntime := ctx.InstallFile(androidAllDir, from.Base(), from)
		r.runtimes = append(r.runtimes, installedRuntime)
	}

	if !ctx.Config().AlwaysUsePrebuiltSdks() && r.props.Lib != nil {
		runtimeFromSourceModule := ctx.GetDirectDepProxyWithTag(String(r.props.Lib), libTag)
		if runtimeFromSourceModule == nil {
			if ctx.Config().AllowMissingDependencies() {
				ctx.AddMissingDependencies([]string{String(r.props.Lib)})
			} else {
				ctx.PropertyErrorf("lib", "missing dependency %q", String(r.props.Lib))
			}
			return
		}
		runtimeFromSourceJar := android.OutputFileForModule(ctx, runtimeFromSourceModule, "")

		// "TREE" name is essential here because it hooks into the "TREE" name in
		// Robolectric's SdkConfig.java that will always correspond to the NEWEST_SDK
		// in Robolectric configs.
		runtimeName := "android-all-current-robolectric-r0.jar"
		installedRuntime := ctx.InstallFile(androidAllDir, runtimeName, runtimeFromSourceJar)
		r.runtimes = append(r.runtimes, installedRuntime)
	}

	android.SetProvider(ctx, RobolectricRuntimesInfoProvider, RobolectricRuntimesInfo{
		Runtimes: r.runtimes,
	})

	android.SetProvider(ctx, android.TestSuiteInfoProvider, android.TestSuiteInfo{
		TestSuites: r.TestSuites(),
	})
}

func (r *robolectricRuntimes) InstallInTestcases() bool { return true }
func (r *robolectricRuntimes) InstallForceOS() (*android.OsType, *android.ArchType) {
	return &r.forceOSType, &r.forceArchType
}
