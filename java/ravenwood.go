// Copyright 2023 Google Inc. All rights reserved.
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
package java

import (
	"strconv"

	"android/soong/aconfig"
	"android/soong/android"
	"android/soong/tradefed"

	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"
)

func init() {
	RegisterRavenwoodBuildComponents(android.InitRegistrationContext)
}

func RegisterRavenwoodBuildComponents(ctx android.RegistrationContext) {
	ctx.RegisterModuleType("android_ravenwood_test", ravenwoodTestFactory)
	ctx.RegisterModuleType("android_ravenwood_libgroup", ravenwoodLibgroupFactory)
}

var ravenwoodLibContentTag = dependencyTag{name: "ravenwoodlibcontent"}
var ravenwoodUtilsTag = dependencyTag{name: "ravenwoodutils"}
var ravenwoodRuntimeTag = dependencyTag{name: "ravenwoodruntime"}
var ravenwoodTargetResourceApkTag = dependencyTag{name: "ravenwood-target-res-apk"}
var allAconfigModuleTag = dependencyTag{name: "all_aconfig"}

var genManifestProperties = pctx.AndroidStaticRule("genManifestProperties",
	blueprint.RuleParams{
		Command: "echo targetSdkVersionInt=$targetSdkVersionInt > $out && " +
			"echo targetSdkVersionRaw=$targetSdkVersionRaw >> $out && " +
			"echo packageName=$packageName >> $out && " +
			"echo targetPackageName=$targetPackageName >> $out && " +
			"echo instrumentationClass=$instrumentationClass >> $out && " +
			"echo moduleName=$moduleName >> $out && " +
			"echo resourceApk=$resourceApk >> $out && " +
			"echo targetResourceApk=$targetResourceApk >> $out",
	},
	"targetSdkVersionInt", "targetSdkVersionRaw", "packageName", "targetPackageName",
	"instrumentationClass", "moduleName", "resourceApk", "targetResourceApk",
)

const ravenwoodUtilsName = "ravenwood-utils"
const ravenwoodRuntimeName = "ravenwood-runtime"

type ravenwoodLibgroupJniDepProviderInfo struct {
	// All the jni_libs module names with transient dependencies.
	names map[string]bool
}

var ravenwoodLibgroupJniDepProvider = blueprint.NewProvider[ravenwoodLibgroupJniDepProviderInfo]()

func getLibPath(archType android.ArchType) string {
	if archType.Multilib == "lib64" {
		return "lib64"
	}
	return "lib"
}

type ravenwoodTestProperties struct {
	// Specify the name of the Instrumentation subclass to use.
	// (e.g. "androidx.test.runner.AndroidJUnitRunner")
	Instrumentation_class *string

	// Specify the package name of the test target apk.
	// This will be set to the target Context's package name.
	// (i.e. Instrumentation.getTargetContext().getPackageName())
	// If this is omitted, Package_name will be used.
	Target_package_name *string

	// Specify another android_app module here to copy it to the test directory, so that
	// the ravenwood test can access it. This APK will be loaded as resources of the test
	// target app.
	Target_resource_apk *string

	// Specify whether to build resources.
	Build_resources *bool
}

type ravenwoodTest struct {
	Library
	aapt

	ravenwoodTestProperties ravenwoodTestProperties

	testProperties testProperties

	testConfig android.Path
	data       android.Paths

	forceOSType   android.OsType
	forceArchType android.ArchType
}

func ravenwoodTestFactory() android.Module {
	module := &ravenwoodTest{}

	module.addHostAndDeviceProperties()
	module.AddProperties(&module.aaptProperties, &module.testProperties, &module.ravenwoodTestProperties)

	module.Module.dexpreopter.isTest = true
	module.Module.linter.properties.Lint.Test_module_type = proptools.BoolPtr(true)

	module.testProperties.Test_suites = []string{
		"general-tests",
		"ravenwood-tests",
	}
	module.testProperties.Test_options.Unit_test = proptools.BoolPtr(false)
	module.Module.sourceProperties.Test_only = proptools.BoolPtr(true)
	module.Module.sourceProperties.Top_level_test_target = true

	InitJavaModule(module, android.DeviceSupported)
	android.InitDefaultableModule(module)

	return module
}

func (r *ravenwoodTest) InstallInTestcases() bool { return true }
func (r *ravenwoodTest) InstallForceOS() (*android.OsType, *android.ArchType) {
	return &r.forceOSType, &r.forceArchType
}
func (r *ravenwoodTest) TestSuites() []string {
	return r.testProperties.Test_suites
}

func (r *ravenwoodTest) DepsMutator(ctx android.BottomUpMutatorContext) {
	r.Library.DepsMutator(ctx)

	// Generically depend on the runtime so that it's installed together with us
	ctx.AddVariationDependencies(nil, ravenwoodRuntimeTag, ravenwoodRuntimeName)

	// Directly depend on any utils so that we link against them
	utils := ctx.AddVariationDependencies(nil, ravenwoodUtilsTag, ravenwoodUtilsName)[0]
	if utils != nil {
		for _, lib := range utils.(*ravenwoodLibgroup).ravenwoodLibgroupProperties.Libs {
			ctx.AddVariationDependencies(nil, libTag, lib)
		}
	}

	// Add jni libs
	for _, lib := range r.testProperties.Jni_libs.GetOrDefault(ctx, nil) {
		ctx.AddVariationDependencies(ctx.Config().BuildOSTarget.Variations(), jniLibTag, lib)
	}

	// Resources APK
	if resourceApk := proptools.String(r.ravenwoodTestProperties.Target_resource_apk); resourceApk != "" {
		ctx.AddVariationDependencies(nil, ravenwoodTargetResourceApkTag, resourceApk)
	}

	sdkDep := decodeSdkDep(ctx, android.SdkContext(r))
	if sdkDep.hasFrameworkLibs() {
		r.aapt.deps(ctx, sdkDep)
	}

	for _, aconfig_declaration := range r.aaptProperties.Flags_packages {
		ctx.AddDependency(ctx.Module(), aconfigDeclarationTag, aconfig_declaration)
	}
}

func (r *ravenwoodTest) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	r.forceOSType = ctx.Config().BuildOS
	r.forceArchType = ctx.Config().BuildArch

	r.testConfig = tradefed.AutoGenTestConfig(ctx, tradefed.AutoGenTestConfigOptions{
		TestConfigProp:          r.testProperties.Test_config,
		TestConfigTemplateProp:  r.testProperties.Test_config_template,
		OptionsForAutogenerated: r.testProperties.Test_options.Tradefed_options,
		TestRunnerOptions:       r.testProperties.Test_options.Test_runner_options,
		TestSuites:              r.testProperties.Test_suites,
		AutoGenConfig:           r.testProperties.Auto_gen_config,
		DeviceTemplate:          "${RavenwoodTestConfigTemplate}",
		HostTemplate:            "${RavenwoodTestConfigTemplate}",
	})
	r.data = android.PathsForModuleSrc(ctx, r.testProperties.Data)
	r.data = append(r.data, android.PathsForModuleSrc(ctx, r.testProperties.Device_common_data)...)
	r.data = append(r.data, android.PathsForModuleSrc(ctx, r.testProperties.Device_first_data)...)
	r.data = append(r.data, android.PathsForModuleSrc(ctx, r.testProperties.Device_first_prefer32_data)...)
	r.data = append(r.data, android.PathsForModuleSrc(ctx, r.testProperties.Host_common_data)...)
	r.data = append(r.data, android.PathsForModuleSrc(ctx, r.testProperties.Host_first_data)...)

	r.data = android.SortedUniquePaths(r.data)

	var testData []android.DataPath
	for _, data := range r.data {
		dataPath := android.DataPath{SrcPath: data}
		testData = append(testData, dataPath)
	}
	for _, d := range r.extraOutputFiles {
		testData = append(testData, android.DataPath{SrcPath: d})
	}

	// When setting the manifest property, we only want to set it for aaptProperties.
	// Explicitly remove it from the Module properties to prevent it from using
	// AndroidManifest.xml as JAR manifest, creating a malformed JAR file.
	r.Module.properties.Manifest = nil

	// Build resources before Java sources.
	var resourceApk android.Path
	if proptools.Bool(r.ravenwoodTestProperties.Build_resources) {
		r.aaptBuildActions(ctx)
		resourceApk = r.aapt.exportPackage
	}

	// Always enable Ravenizer for ravenwood tests.
	r.Library.ravenizer.enabled = true

	r.Library.GenerateAndroidBuildActions(ctx)

	// Start by depending on all files installed by dependencies
	var installDeps android.InstallPaths

	// All JNI libraries included in the runtime
	var runtimeJniModuleNames map[string]bool

	utils := ctx.GetDirectDepsProxyWithTag(ravenwoodUtilsTag)[0]
	for _, installFile := range android.OtherModuleProviderOrDefault(
		ctx, utils, android.InstallFilesProvider).InstallFiles {
		installDeps = append(installDeps, installFile)
	}
	jniDeps, ok := android.OtherModuleProvider(ctx, utils, ravenwoodLibgroupJniDepProvider)
	if ok {
		runtimeJniModuleNames = jniDeps.names
	}

	runtime := ctx.GetDirectDepsProxyWithTag(ravenwoodRuntimeTag)[0]
	for _, installFile := range android.OtherModuleProviderOrDefault(
		ctx, runtime, android.InstallFilesProvider).InstallFiles {
		installDeps = append(installDeps, installFile)
	}
	jniDeps, ok = android.OtherModuleProvider(ctx, runtime, ravenwoodLibgroupJniDepProvider)
	if ok {
		runtimeJniModuleNames = jniDeps.names
	}

	// Also remember what JNI libs are in the runtime.

	// Also depend on our config
	installPath := android.PathForModuleInstall(ctx, r.BaseModuleName())
	installConfig := ctx.InstallFile(installPath, ctx.ModuleName()+".config", r.testConfig)
	installDeps = append(installDeps, installConfig)

	// Depend on the JNI libraries, but don't install the ones that the runtime already
	// contains.
	soInstallPath := installPath.Join(ctx, getLibPath(r.forceArchType))
	for _, jniLib := range collectTransitiveJniDeps(ctx) {
		if _, ok := runtimeJniModuleNames[jniLib.name]; ok {
			continue // Runtime already includes it.
		}
		installJni := ctx.InstallFile(soInstallPath, jniLib.path.Base(), jniLib.path)
		installDeps = append(installDeps, installJni)
	}

	resApkInstallPath := installPath.Join(ctx, "ravenwood-res-apks")

	var resApkName string
	var targetResApkName string

	if resourceApk != nil {
		installResApk := ctx.InstallFile(resApkInstallPath, "ravenwood-res.apk", resourceApk)
		installDeps = append(installDeps, installResApk)
		resApkName = "ravenwood-res.apk"
	}

	if resApk := ctx.GetDirectDepsProxyWithTag(ravenwoodTargetResourceApkTag); len(resApk) > 0 {
		installFile := android.OutputFileForModule(ctx, resApk[0], "")
		installResApk := ctx.InstallFile(resApkInstallPath, "ravenwood-target-res.apk", installFile)
		installDeps = append(installDeps, installResApk)
		targetResApkName = "ravenwood-target-res.apk"
	}

	// Generate manifest properties
	propertiesOutputPath := android.PathForModuleGen(ctx, "ravenwood.properties")

	targetSdkVersion := proptools.StringDefault(r.deviceProperties.Target_sdk_version, "")
	targetSdkVersionInt := r.TargetSdkVersion(ctx).FinalOrFutureInt() // FinalOrFutureInt may be 10000.
	packageName := r.aapt.aaptProperties.Package_name.GetOrDefault(ctx, "")
	targetPackageName := proptools.StringDefault(r.ravenwoodTestProperties.Target_package_name, "")
	instClassName := proptools.StringDefault(r.ravenwoodTestProperties.Instrumentation_class, "")
	ctx.Build(pctx, android.BuildParams{
		Rule:        genManifestProperties,
		Description: "genManifestProperties",
		Output:      propertiesOutputPath,
		Args: map[string]string{
			"targetSdkVersionInt":  strconv.Itoa(targetSdkVersionInt),
			"targetSdkVersionRaw":  targetSdkVersion,
			"packageName":          packageName,
			"targetPackageName":    targetPackageName,
			"instrumentationClass": instClassName,
			"moduleName":           ctx.ModuleName(),
			"resourceApk":          resApkName,
			"targetResourceApk":    targetResApkName,
		},
	})
	installProps := ctx.InstallFile(installPath, "ravenwood.properties", propertiesOutputPath)
	installDeps = append(installDeps, installProps)

	// Copy data files
	for _, data := range r.data {
		installedData := ctx.InstallFile(installPath, data.Rel(), data)
		installDeps = append(installDeps, installedData)
	}

	// Install our JAR with all dependencies
	ctx.InstallFile(installPath, ctx.ModuleName()+".jar", r.outputFile, installDeps...)

	moduleInfoJSON := ctx.ModuleInfoJSON()
	if _, ok := r.testConfig.(android.WritablePath); ok {
		moduleInfoJSON.AutoTestConfig = []string{"true"}
	}
	if r.testConfig != nil {
		moduleInfoJSON.TestConfig = append(moduleInfoJSON.TestConfig, r.testConfig.String())
	}
	moduleInfoJSON.CompatibilitySuites = []string{"general-tests", "ravenwood-tests"}

	android.SetProvider(ctx, android.TestSuiteInfoProvider, android.TestSuiteInfo{
		TestSuites: r.TestSuites(),
	})
}

// This method is adapted from AndroidApp.aaptBuildActions() in app.go, with changes
// irrelevant to RavenwoodTest removed.
func (r *ravenwoodTest) aaptBuildActions(ctx android.ModuleContext) {
	usePlatformAPI := proptools.Bool(r.Module.deviceProperties.Platform_apis)
	if ctx.Module().(android.SdkContext).SdkVersion(ctx).Kind == android.SdkModule {
		usePlatformAPI = true
	}
	r.aapt.usesNonSdkApis = usePlatformAPI

	aconfigTextFilePaths := getAconfigFilePaths(ctx)

	r.aapt.buildActions(ctx,
		aaptBuildActionOptions{
			sdkContext:                     android.SdkContext(r),
			enforceDefaultTargetSdkVersion: true,
			forceNonFinalResourceIDs:       true,
			aconfigTextFiles:               aconfigTextFilePaths,
			usesLibrary:                    &r.usesLibrary,
		},
	)

	android.SetProvider(ctx, FlagsPackagesProvider, FlagsPackages{
		AconfigTextFiles: aconfigTextFilePaths,
	})

	// Add R classes into classpath
	if r.useResourceProcessorBusyBox(ctx) {
		// When building an app with ResourceProcessorBusyBox enabled ResourceProcessorBusyBox has already
		// created R.class files that provide IDs for resources in busybox/R.jar.  Pass that file in the
		// classpath when compiling everything else, and add it to the final classes jar.
		r.extraClasspathJars = append(r.extraClasspathJars, r.aapt.rJar)
		r.extraCombinedJars = append(r.extraCombinedJars, r.aapt.rJar)
	} else {
		// When building an app without ResourceProcessorBusyBox the aapt2 rule creates R.srcjar containing
		// R.java files for the app's package and the packages from all transitive static android_library
		// dependencies.  Compile the srcjar alongside the rest of the sources.
		r.extraSrcJars = append(r.extraSrcJars, r.aapt.aaptSrcJar)
	}
}

func (r *ravenwoodTest) PrepareAndroidMKProviderInfo(config android.Config) *android.AndroidMkProviderInfo {
	info := r.Library.prepareAndroidMKProviderInfo(config)

	if info.PrimaryInfo.OutputFile.Valid() {
		info.PrimaryInfo.SetBool("LOCAL_UNINSTALLABLE_MODULE", true)
		info.PrimaryInfo.AddStrings("LOCAL_COMPATIBILITY_SUITE",
			"general-tests", "ravenwood-tests")
		if r.testConfig != nil {
			info.PrimaryInfo.SetPath("LOCAL_FULL_TEST_CONFIG", r.testConfig)
		}
	}

	r.addHostDexAndroidMkInfo(info)

	return info
}

type ravenwoodLibgroupProperties struct {
	Libs []string

	Jni_libs proptools.Configurable[[]string]

	// We use this to copy framework-res.apk to the ravenwood runtime directory.
	Data []string `android:"path,arch_variant"`

	// We use this to copy font files to the ravenwood runtime directory.
	Fonts []string `android:"path,arch_variant"`
}

type ravenwoodLibgroup struct {
	android.ModuleBase

	ravenwoodLibgroupProperties ravenwoodLibgroupProperties

	forceOSType   android.OsType
	forceArchType android.ArchType
}

func ravenwoodLibgroupFactory() android.Module {
	module := &ravenwoodLibgroup{}
	module.AddProperties(&module.ravenwoodLibgroupProperties)

	android.InitAndroidArchModule(module, android.DeviceSupported, android.MultilibCommon)
	return module
}

func (r *ravenwoodLibgroup) InstallInTestcases() bool { return true }
func (r *ravenwoodLibgroup) InstallForceOS() (*android.OsType, *android.ArchType) {
	return &r.forceOSType, &r.forceArchType
}
func (r *ravenwoodLibgroup) TestSuites() []string {
	return []string{
		"general-tests",
		"ravenwood-tests",
	}
}

func (r *ravenwoodLibgroup) DepsMutator(ctx android.BottomUpMutatorContext) {
	// Always depends on our underlying libs
	for _, lib := range r.ravenwoodLibgroupProperties.Libs {
		ctx.AddVariationDependencies(nil, ravenwoodLibContentTag, lib)
	}
	for _, lib := range r.ravenwoodLibgroupProperties.Jni_libs.GetOrDefault(ctx, nil) {
		ctx.AddVariationDependencies(ctx.Config().BuildOSTarget.Variations(), jniLibTag, lib)
	}

	if r.Name() == ravenwoodRuntimeName {
		ctx.AddVariationDependencies(nil, allAconfigModuleTag, aconfig.AllAconfigModule)
	}
}

func (r *ravenwoodLibgroup) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	r.forceOSType = ctx.Config().BuildOS
	r.forceArchType = ctx.Config().BuildArch

	// Collect the JNI dependencies, including the transitive deps.
	jniDepNames := make(map[string]bool)
	jniLibs := collectTransitiveJniDeps(ctx)

	for _, jni := range jniLibs {
		jniDepNames[jni.name] = true
	}
	android.SetProvider(ctx, ravenwoodLibgroupJniDepProvider, ravenwoodLibgroupJniDepProviderInfo{
		names: jniDepNames,
	})

	install := func(to android.InstallPath, srcs ...android.Path) {
		for _, s := range srcs {
			ctx.InstallFile(to, s.Base(), s)
		}
	}

	// Install our runtime into expected location for packaging
	installPath := android.PathForModuleInstall(ctx, r.BaseModuleName())
	for _, lib := range r.ravenwoodLibgroupProperties.Libs {
		libModule := ctx.GetDirectDepProxyWithTag(lib, ravenwoodLibContentTag)
		if libModule.IsNil() {
			if ctx.Config().AllowMissingDependencies() {
				ctx.AddMissingDependencies([]string{lib})
			} else {
				ctx.PropertyErrorf("lib", "missing dependency %q", lib)
			}
			continue
		}
		libJar := android.OutputFileForModule(ctx, libModule, "")
		ctx.InstallFile(installPath, libJar.Base(), libJar)
	}
	soInstallPath := android.PathForModuleInstall(ctx, r.BaseModuleName()).Join(ctx, getLibPath(r.forceArchType))

	for _, jniLib := range jniLibs {
		install(soInstallPath, jniLib.path)
	}

	dataInstallPath := installPath.Join(ctx, "ravenwood-data")
	data := android.PathsForModuleSrc(ctx, r.ravenwoodLibgroupProperties.Data)
	for _, file := range data {
		install(dataInstallPath, file)
	}

	fontsInstallPath := installPath.Join(ctx, "fonts")
	fonts := android.PathsForModuleSrc(ctx, r.ravenwoodLibgroupProperties.Fonts)
	for _, file := range fonts {
		install(fontsInstallPath, file)
	}

	// Copy aconfig flag storage files.
	if r.Name() == ravenwoodRuntimeName {
		allAconfigFound := false
		if allAconfig := ctx.GetDirectDepProxyWithTag(aconfig.AllAconfigModule, allAconfigModuleTag); !allAconfig.IsNil() {
			aadi, ok := android.OtherModuleProvider(ctx, allAconfig, aconfig.AllAconfigDeclarationsInfoProvider)
			if ok {
				// Binary proto file and the text proto.
				// We don't really use the text proto file, but having this would make debugging easier.
				install(installPath.Join(ctx, "aconfig/metadata/aconfig/etc"), aadi.ParsedFlagsFile, aadi.TextProtoFlagsFile)

				// The "new" storage files.
				install(installPath.Join(ctx, "aconfig/metadata/aconfig/maps"), aadi.StoragePackageMap, aadi.StorageFlagMap)
				install(installPath.Join(ctx, "aconfig/metadata/aconfig/boot"), aadi.StorageFlagVal, aadi.StorageFlagInfo)

				allAconfigFound = true
			}
		}
		if !allAconfigFound {
			if ctx.Config().AllowMissingDependencies() {
				ctx.AddMissingDependencies([]string{aconfig.AllAconfigModule})
			} else {
				ctx.ModuleErrorf("missing dependency %q", aconfig.AllAconfigModule)
			}
		}
	}

	// Normal build should perform install steps
	ctx.Phony(r.BaseModuleName(), android.PathForPhony(ctx, r.BaseModuleName()+"-install"))

	android.SetProvider(ctx, android.TestSuiteInfoProvider, android.TestSuiteInfo{
		TestSuites: r.TestSuites(),
	})
}

// collectTransitiveJniDeps returns all JNI dependencies, including transitive
// ones, including NDK / stub libs. (Because Ravenwood has no "preinstalled" libraries)
func collectTransitiveJniDeps(ctx android.ModuleContext) []jniLib {
	libs, _ := collectJniDeps(ctx, true, false, nil)
	return libs
}
