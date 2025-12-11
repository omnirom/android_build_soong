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

package testsuites

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"

	"android/soong/android"
	"android/soong/java"
	"android/soong/phony"
)

var (
	pctx = android.NewPackageContext("android/soong/test_suites")

	_ = pctx.HostBinToolVariable("buildLicenseMetadataCmd", "build_license_metadata")

	licenseMetadataRule = pctx.AndroidStaticRule("licenseMetadataRule", blueprint.RuleParams{
		Command:        "${buildLicenseMetadataCmd} -o $out @${out}.rsp",
		CommandDeps:    []string{"${buildLicenseMetadataCmd}"},
		Rspfile:        "${out}.rsp",
		RspfileContent: "${args}",
	}, "args")
)

func init() {
	android.RegisterParallelSingletonType("testsuites", testSuiteFilesFactory)
	android.RegisterModuleType("compatibility_test_suite_package", compatibilityTestSuitePackageFactory)
	android.RegisterModuleType("test_suite_package", testSuitePackageFactory)
	pctx.Import("android/soong/android")
}

func testSuiteFilesFactory() android.Singleton {
	return &testSuiteFiles{}
}

type testSuiteFiles struct{}

func (t *testSuiteFiles) GenerateBuildActions(ctx android.SingletonContext) {
	hostOutTestCases := android.PathForHostInstall(ctx, "testcases")
	// regularInstalledFiles maps from test suite name -> all installed files from all modules
	// in the suite. These are the regular "installed" files used prevelantly in soong, not specific
	// to test suites, in contrast to allTestSuiteInstalls, which contains the testcases/ folder
	// installed files.
	regularInstalledFiles := make(map[string]android.InstallPaths)
	// A mapping from suite name to modules in the suite
	testSuiteModules := make(map[string][]android.ModuleProxy)
	sharedLibRoots := make(map[string][]string)
	sharedLibGraph := make(map[string][]string)
	allTestSuiteInstalls := make(map[string]android.Paths)
	allTestSuiteSrcs := make(map[string]android.Paths)
	seenSymlinks := make(map[string]bool)
	var toInstall []android.FilePair
	var oneVariantInstalls []android.FilePair
	var allCompatibilitySuitePackages []compatibilitySuitePackageInfo

	var allTestSuiteConfigs []testSuiteConfig
	allTestSuitesWithHostSharedLibs := []string{
		"general-tests",
		"device-tests",
		"vts",
		"tvts",
		"art-host-tests",
		"host-unit-tests",
		"sdv-host-unit-tests",
		"camera-hal-tests",
		"automotive-tests",
		"automotive-general-tests",
		"automotive-sdv-tests",
	}

	ctx.VisitAllModuleProxies(func(m android.ModuleProxy) {
		commonInfo := android.OtherModuleProviderOrDefault(ctx, m, android.CommonModuleInfoProvider)
		testSuiteSharedLibsInfo := android.OtherModuleProviderOrDefault(ctx, m, android.TestSuiteSharedLibsInfoProvider)
		makeName := android.OtherModuleProviderOrDefault(ctx, m, android.MakeNameInfoProvider).Name
		if makeName != "" && commonInfo.Target.Os == ctx.Config().BuildOS {
			sharedLibGraph[makeName] = append(sharedLibGraph[makeName], testSuiteSharedLibsInfo.MakeNames...)
		}

		if tsm, ok := android.OtherModuleProvider(ctx, m, android.TestSuiteInfoProvider); ok {
			installFilesProvider := android.OtherModuleProviderOrDefault(ctx, m, android.InstallFilesProvider)

			for _, testSuite := range tsm.TestSuites {
				regularInstalledFiles[testSuite] = append(regularInstalledFiles[testSuite], installFilesProvider.InstallFiles...)
				testSuiteModules[testSuite] = append(testSuiteModules[testSuite], m)

				if makeName != "" {
					sharedLibRoots[testSuite] = append(sharedLibRoots[testSuite], makeName)
				}
			}

			if testSuiteInstalls, ok := android.OtherModuleProvider(ctx, m, android.TestSuiteInstallsInfoProvider); ok {
				for _, testSuite := range tsm.TestSuites {
					for _, f := range testSuiteInstalls.Files {
						allTestSuiteInstalls[testSuite] = append(allTestSuiteInstalls[testSuite], f.Dst)
						allTestSuiteSrcs[testSuite] = append(allTestSuiteSrcs[testSuite], f.Src)
					}
					for _, f := range testSuiteInstalls.OneVariantInstalls {
						allTestSuiteInstalls[testSuite] = append(allTestSuiteInstalls[testSuite], f.Dst)
						allTestSuiteSrcs[testSuite] = append(allTestSuiteSrcs[testSuite], f.Src)
					}
				}
				installs := android.OtherModuleProviderOrDefault(ctx, m, android.InstallFilesProvider).InstallFiles
				oneVariantInstalls = append(oneVariantInstalls, testSuiteInstalls.OneVariantInstalls...)
				for _, f := range testSuiteInstalls.Files {
					alreadyInstalled := false
					for _, install := range installs {
						if install.String() == f.Dst.String() {
							alreadyInstalled = true
							break
						}
					}
					if !alreadyInstalled {
						toInstall = append(toInstall, f)
					}
				}
			}
		}

		if info, ok := android.OtherModuleProvider(ctx, m, compatibilitySuitePackageProvider); ok {
			allCompatibilitySuitePackages = append(allCompatibilitySuitePackages, info)
		}
		if info, ok := android.OtherModuleProvider(ctx, m, testSuitePackageProvider); ok {
			allTestSuiteConfigs = append(allTestSuiteConfigs, info)
			if info.buildHostSharedLibsZip || info.includeHostSharedLibsInMainZip {
				allTestSuitesWithHostSharedLibs = append(allTestSuitesWithHostSharedLibs, info.name)
			}
		}
	})

	sort.Strings(allTestSuitesWithHostSharedLibs)
	slices.SortFunc(allTestSuiteConfigs, func(a testSuiteConfig, b testSuiteConfig) int {
		return strings.Compare(a.name, b.name)
	})

	for suite, suiteInstalls := range allTestSuiteInstalls {
		allTestSuiteInstalls[suite] = android.SortedUniquePaths(suiteInstalls)
	}

	hostSharedLibs := gatherHostSharedLibs(ctx, sharedLibRoots, sharedLibGraph)

	for _, testSuite := range android.SortedKeys(testSuiteModules) {
		testSuiteSymbolsZipFile := android.PathForHostInstall(ctx, fmt.Sprintf("%s-symbols.zip", testSuite))
		testSuiteMergedMappingProtoFile := android.PathForHostInstall(ctx, fmt.Sprintf("%s-symbols-mapping.textproto", testSuite))
		android.BuildSymbolsZip(ctx, testSuiteModules[testSuite], nil, testSuiteSymbolsZipFile, testSuiteMergedMappingProtoFile)

		ctx.DistForGoalWithFilenameTag(testSuite, testSuiteSymbolsZipFile, testSuiteSymbolsZipFile.Base())
		ctx.DistForGoalWithFilenameTag(testSuite, testSuiteMergedMappingProtoFile, testSuiteMergedMappingProtoFile.Base())
	}

	// https://source.corp.google.com/h/googleplex-android/platform/superproject/main/+/main:build/make/core/main.mk;l=674;drc=46bd04e115d34fd62b3167128854dfed95290eb0
	testInstalledSharedLibs := make(map[string]android.Paths)
	testInstalledSharedLibsDeduper := make(map[string]bool)
	for _, install := range toInstall {
		testInstalledSharedLibsDeduper[install.Dst.String()] = true
	}
	for _, suite := range allTestSuitesWithHostSharedLibs {
		var myTestCases android.WritablePath = hostOutTestCases
		switch suite {
		case "vts", "tvts":
			suiteInfo := ctx.Config().CompatibilityTestcases()[suite]
			outDir := suiteInfo.OutDir
			if outDir == "" {
				continue
			}
			rel, err := filepath.Rel(ctx.Config().OutDir(), outDir)
			if err != nil || strings.HasPrefix(rel, "..") {
				panic(fmt.Sprintf("Could not make COMPATIBILITY_TESTCASES_OUT_%s (%s) relative to the out dir (%s)", suite, suiteInfo.OutDir, ctx.Config().OutDir()))
			}
			myTestCases = android.PathForArbitraryOutput(ctx, rel)
		}

		for _, f := range hostSharedLibs[suite] {
			dir := filepath.Base(filepath.Dir(f.String()))
			out := android.JoinWriteablePath(ctx, myTestCases, dir, filepath.Base(f.String()))
			if _, ok := testInstalledSharedLibsDeduper[out.String()]; !ok {
				ctx.Build(pctx, android.BuildParams{
					Rule:   android.Cp,
					Input:  f,
					Output: out,
				})
			}
			testInstalledSharedLibsDeduper[out.String()] = true
			testInstalledSharedLibs[suite] = append(testInstalledSharedLibs[suite], out)
		}
	}

	filePairSorter := func(arr []android.FilePair) func(i, j int) bool {
		return func(i, j int) bool {
			c := strings.Compare(arr[i].Dst.String(), arr[j].Dst.String())
			if c < 0 {
				return true
			} else if c > 0 {
				return false
			}
			return arr[i].Src.String() < arr[j].Src.String()
		}
	}

	sort.Slice(toInstall, filePairSorter(toInstall))
	// Dedup, as multiple tests may install the same test data to the same folder
	toInstall = slices.Compact(toInstall)

	// Dedup the oneVariant files by only the dst locations, and ignore installs from other variants
	sort.Slice(oneVariantInstalls, filePairSorter(oneVariantInstalls))
	oneVariantInstalls = slices.CompactFunc(oneVariantInstalls, func(a, b android.FilePair) bool {
		return a.Dst.String() == b.Dst.String()
	})

	for _, install := range toInstall {
		ctx.Build(pctx, android.BuildParams{
			Rule:   android.Cp,
			Input:  install.Src,
			Output: install.Dst,
		})
	}
	for _, install := range oneVariantInstalls {
		ctx.Build(pctx, android.BuildParams{
			Rule:   android.Cp,
			Input:  install.Src,
			Output: install.Dst,
		})
	}

	robolectricZip, robolectrictListZip := buildTestSuite(ctx, "robolectric-tests", regularInstalledFiles["robolectric-tests"])
	ctx.Phony("robolectric-tests", robolectricZip, robolectrictListZip)
	ctx.DistForGoal("robolectric-tests", robolectricZip, robolectrictListZip)

	ravenwoodZip, ravenwoodListZip := buildTestSuite(ctx, "ravenwood-tests", regularInstalledFiles["ravenwood-tests"])
	ctx.Phony("ravenwood-tests", ravenwoodZip, ravenwoodListZip)
	ctx.DistForGoal("ravenwood-tests", ravenwoodZip, ravenwoodListZip)

	for _, testSuiteConfig := range allTestSuiteConfigs {
		files := allTestSuiteInstalls[testSuiteConfig.name]
		sharedLibs := testInstalledSharedLibs[testSuiteConfig.name]
		packageTestSuite(ctx, testSuiteModules[testSuiteConfig.name], files, sharedLibs, testSuiteConfig, hostSharedLibs[testSuiteConfig.name], seenSymlinks)
	}

	for _, suite := range allCompatibilitySuitePackages {
		buildCompatibilitySuitePackage(ctx, suite, slices.Clone(allTestSuiteInstalls[suite.Name]), testSuiteModules[suite.Name], slices.Clone(testInstalledSharedLibs[suite.Name]))
		if suite.Name == "cts" {
			generateCtsCoverageReports(ctx, allTestSuiteSrcs)
		}
	}
}

type apiReportType int

const (
	apiMapReportType apiReportType = iota
	apiInheritReportType
)

func (a apiReportType) String() string {
	switch a {
	case apiMapReportType:
		return "api-map"
	case apiInheritReportType:
		return "api-inherit"
	default:
		return "unknown"
	}
}

// The cts test suite will generate reports with "cts-api-map". It needs test suite dependencies
// and api.xml files to analyze jar files. Finally dist the report as build goal "cts-api-coverage".
func generateCtsCoverageReports(ctx android.SingletonContext, allTestSuiteSrcs map[string]android.Paths) {
	hostOutApiMap := android.PathForHostInstall(ctx, "cts-api-map")
	allApiMapFile := make(map[string]android.InstallPath)
	ctsReportModules := []string{"cts", "cts-v-host"}
	ctsVerifierAppListModule := "android-cts-verifier-app-list"
	apiXmlModules := []string{"android_stubs_current", "android_system_stubs_current", "android_module_lib_stubs_current", "android_system_server_stubs_current"}
	foundApiXmlFiles := make(map[string]android.Path)
	testSuiteOutput := make(map[string]android.Path)
	var apiXmlFiles android.Paths
	var ctsVerifierApiMapFile android.Path

	// Collect apk and jar paths in {suite}_api_map_files.txt as input for coverage report.
	for _, suite := range android.SortedKeys(allTestSuiteSrcs) {
		if slices.Contains(ctsReportModules, suite) {
			allTestSuiteSrcs[suite] = android.SortedUniquePaths(allTestSuiteSrcs[suite])
			var apkJarSrcs android.Paths
			for _, srcPath := range allTestSuiteSrcs[suite] {
				if srcPath.Ext() == ".apk" || srcPath.Ext() == ".jar" {
					apkJarSrcs = append(apkJarSrcs, srcPath)
				}
			}
			allApiMapFile[suite] = hostOutApiMap.Join(ctx, fmt.Sprintf("%s_api_map_files.txt", suite))
			android.WriteFileRule(ctx, allApiMapFile[suite], strings.Join(apkJarSrcs.Strings(), " "))
		}
	}

	ctx.VisitAllModuleProxies(func(m android.ModuleProxy) {
		if slices.Contains(apiXmlModules, m.Name()) {
			if _, exists := foundApiXmlFiles[m.Name()]; exists {
				ctx.Errorf("Found multiple variants for module %q while looking for .api.xml", m.Name())
				return
			}
			foundApiXmlFiles[m.Name()] = android.OutputFileForModule(ctx, m, ".api.xml")
		}
		if m.Name() == ctsVerifierAppListModule {
			if ctsVerifierApiMapFile != nil {
				ctx.Errorf("Found multiple variants for module %q", ctsVerifierAppListModule)
				return
			}
			ctsVerifierApiMapFile = android.OutputFileForModule(ctx, m, "")
		}
		if slices.Contains(ctsReportModules, m.Name()) {
			if _, exists := testSuiteOutput[m.Name()]; exists {
				ctx.Errorf("Found multiple variants for module %q while looking for suite zip", m.Name())
				return
			}
			testSuiteOutput[m.Name()] = android.OutputFileForModule(ctx, m, "")
		}
	})
	for _, moduleName := range apiXmlModules {
		apiXmlFiles = append(apiXmlFiles, foundApiXmlFiles[moduleName])
	}
	generateApiMapReport(ctx, pctx, "cts-v-host", apiMapReportType, apiXmlFiles, android.Paths{ctsVerifierApiMapFile, allApiMapFile["cts-v-host"]}, android.Paths{testSuiteOutput["cts-v-host"]})
	generateApiMapReport(ctx, pctx, "cts", apiMapReportType, apiXmlFiles, android.Paths{allApiMapFile["cts"]}, android.Paths{testSuiteOutput["cts"]})
	// Suite "cts-combined": "cts" and "cts-v-host"
	generateApiMapReport(ctx, pctx, "cts-combined", apiMapReportType, apiXmlFiles, android.Paths{ctsVerifierApiMapFile, allApiMapFile["cts"], allApiMapFile["cts-v-host"]}, android.Paths{testSuiteOutput["cts"], testSuiteOutput["cts-v-host"]})
	ctx.DistForGoalWithFilename("cts-api-coverage", hostOutApiMap.Join(ctx, "cts-combined-api-map.xml"), "cts-api-map-report.xml")
	generateApiMapReport(ctx, pctx, "cts-combined", apiInheritReportType, apiXmlFiles, android.Paths{ctsVerifierApiMapFile, allApiMapFile["cts"], allApiMapFile["cts-v-host"]}, android.Paths{testSuiteOutput["cts"], testSuiteOutput["cts-v-host"]})
	ctx.DistForGoalWithFilename("cts-api-coverage", hostOutApiMap.Join(ctx, "cts-combined-api-inherit.xml"), "cts-api-inherit-report.xml")
}

func generateApiMapReport(ctx android.SingletonContext, pctx android.PackageContext, suite string, reportType apiReportType, apiXmlFiles android.Paths, jarFileLists android.Paths, dependencies android.Paths) {
	hostOutApiMap := android.PathForHostInstall(ctx, "cts-api-map")
	moduleName := fmt.Sprintf("%s-%s-xml", suite, reportType.String())
	sboxOut := android.PathForOutput(ctx, "api_map_report_temps", moduleName, "gen")
	sboxManifest := android.PathForOutput(ctx, "api_map_report_temps", moduleName, fmt.Sprintf("%s_genrule.sbox.textproto", moduleName))
	outputFileName := fmt.Sprintf("%s-%s.xml", suite, reportType.String())
	jarFilesList := hostOutApiMap.Join(ctx, fmt.Sprintf("%s_jar_files_%s.txt", suite, reportType.String()))
	jarFiles_rule := android.NewRuleBuilder(pctx, ctx)
	jarFiles_rule.Command().
		Text("cat").
		Inputs(jarFileLists).
		Text(">").
		Output(jarFilesList)
	jarFiles_rule.Build(jarFilesList.Base(), fmt.Sprintf("Jar files list for %s", suite))

	rule := android.NewRuleBuilder(pctx, ctx).Sbox(sboxOut, sboxManifest)
	switch reportType {
	case apiMapReportType:
		rule.Command().BuiltTool("cts-api-map").
			Flag("-j 4").
			Flag("-m api_map").
			Flag("-m xts_annotation").
			FlagWithArg("-a ", strings.Join(apiXmlFiles.Strings(), ",")).
			FlagWithArg("-i ", jarFilesList.String()).
			Flag("-f xml").
			FlagWithOutput("-o ", sboxOut.Join(ctx, outputFileName)).
			Implicit(jarFilesList).
			Implicits(apiXmlFiles).
			Implicits(dependencies)
	case apiInheritReportType:
		rule.Command().BuiltTool("cts-api-map").
			Flag("-j 4").
			Flag("-m xts_api_inherit").
			FlagWithArg("-a ", strings.Join(apiXmlFiles.Strings(), ",")).
			FlagWithArg("-i ", jarFilesList.String()).
			Flag("-f xml").
			FlagWithOutput("-o ", sboxOut.Join(ctx, outputFileName)).
			Implicit(jarFilesList).
			Implicits(apiXmlFiles).
			Implicits(dependencies)
	}
	reportDesc := fmt.Sprintf("%s report for %s", reportType.String(), suite)
	rule.Command().Text("echo").Text(reportDesc + ":").Text(hostOutApiMap.Join(ctx, outputFileName).String())
	rule.Build(fmt.Sprintf("generate_%s_report_%s", reportType.String(), suite), reportDesc)
	ctx.Phony(moduleName, hostOutApiMap.Join(ctx, outputFileName))
	ctx.Build(pctx, android.BuildParams{
		Rule:   android.Cp,
		Input:  sboxOut.Join(ctx, outputFileName),
		Output: hostOutApiMap.Join(ctx, outputFileName),
	})
}

// Get a mapping from testSuite -> list of host shared libraries, given:
// - sharedLibRoots: Mapping from testSuite -> androidMk name of all test modules in the suite
// - sharedLibGraph: Mapping from androidMk name of module -> androidMk names of its shared libs
//
// This mimics how make did it historically, which is filled with inaccuracies. Make didn't
// track variants and treated all variants as if they were merged into one big module. This means
// you can have a test that's only included in the "vts" test suite on the device variant, and
// only has a shared library on the host variant, and that shared library will still be included
// into the vts test suite.
func gatherHostSharedLibs(ctx android.SingletonContext, sharedLibRoots, sharedLibGraph map[string][]string) map[string]android.Paths {
	hostOutTestCases := android.PathForHostInstall(ctx, "testcases")
	hostOut := filepath.Dir(hostOutTestCases.String())

	for k, v := range sharedLibGraph {
		sharedLibGraph[k] = android.SortedUniqueStrings(v)
	}

	suiteToSharedLibModules := make(map[string]map[string]bool)
	for suite, modules := range sharedLibRoots {
		suiteToSharedLibModules[suite] = make(map[string]bool)
		var queue []string
		for _, root := range android.SortedUniqueStrings(modules) {
			queue = append(queue, sharedLibGraph[root]...)
		}
		for len(queue) > 0 {
			mod := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			if suiteToSharedLibModules[suite][mod] {
				continue
			}
			suiteToSharedLibModules[suite][mod] = true
			queue = append(queue, sharedLibGraph[mod]...)
		}
	}

	hostSharedLibs := make(map[string]android.Paths)

	ctx.VisitAllModuleProxies(func(m android.ModuleProxy) {
		if makeName, ok := android.OtherModuleProvider(ctx, m, android.MakeNameInfoProvider); ok {
			commonInfo := android.OtherModuleProviderOrDefault(ctx, m, android.CommonModuleInfoProvider)
			if commonInfo.SkipInstall {
				return
			}
			installFilesProvider := android.OtherModuleProviderOrDefault(ctx, m, android.InstallFilesProvider)
			for suite, sharedLibModulesInSuite := range suiteToSharedLibModules {
				if sharedLibModulesInSuite[makeName.Name] {
					for _, f := range installFilesProvider.InstallFiles {
						if strings.HasSuffix(f.String(), ".so") && strings.HasPrefix(f.String(), hostOut) {
							hostSharedLibs[suite] = append(hostSharedLibs[suite], f)
						}
					}
				}
			}
		}
	})
	for suite, files := range hostSharedLibs {
		hostSharedLibs[suite] = android.SortedUniquePaths(files)
	}

	return hostSharedLibs
}

// Gather shared library dependencies of host tests that are not installed algonside the test.
// These common dependencies are installed in testcases/lib[64]/ (to reduce duplication).
func gatherCommonHostSharedLibsForSymlinks(ctx android.SingletonContext, suite string) map[string]android.Paths {
	hostOut32And64 := android.PathForHostInstall(ctx, "lib").String()
	moduleNameToCommonHostSharedLibs := make(map[string]android.Paths)
	ctx.VisitAllModuleProxies(func(m android.ModuleProxy) {
		commonInfo := android.OtherModuleProviderOrDefault(ctx, m, android.CommonModuleInfoProvider)
		testInfo := android.OtherModuleProviderOrDefault(ctx, m, android.TestSuiteInfoProvider)
		if commonInfo.SkipInstall || !commonInfo.Host || !android.InList(suite, testInfo.TestSuites) {
			return
		}
		installFilesProvider := android.OtherModuleProviderOrDefault(ctx, m, android.InstallFilesProvider)
		for _, transitive := range installFilesProvider.TransitiveInstallFiles.ToList() {
			transitivePathString := transitive.String()
			if strings.HasPrefix(transitivePathString, hostOut32And64) &&
				strings.HasSuffix(transitivePathString, ".so") {
				moduleNameToCommonHostSharedLibs[m.Name()] = append(moduleNameToCommonHostSharedLibs[m.Name()], transitive)
			}
		}
	})
	return moduleNameToCommonHostSharedLibs
}

type testSuiteConfig struct {
	name                                         string
	buildHostSharedLibsZip                       bool
	includeHostSharedLibsInMainZip               bool
	includeCommonHostSharedLibsSymlinksInMainZip bool
	hostJavaToolFiles                            android.Paths
	Art_data_zips                                android.Paths
}

func buildTestSuite(ctx android.SingletonContext, suiteName string, files android.InstallPaths) (android.Path, android.Path) {
	installedPaths := android.SortedUniquePaths(files.Paths())

	outputFile := pathForPackaging(ctx, suiteName+".zip")
	rule := android.NewRuleBuilder(pctx, ctx)
	rule.Command().BuiltTool("soong_zip").
		FlagWithOutput("-o ", outputFile).
		FlagWithArg("-P ", "host/testcases").
		FlagWithArg("-C ", pathForTestCases(ctx).String()).
		FlagWithRspFileInputList("-r ", outputFile.ReplaceExtension(ctx, "rsp"), installedPaths).
		Flag("-sha256") // necessary to save cas_uploader's time

	testList := buildTestList(ctx, suiteName+"_list", installedPaths)
	testListZipOutputFile := pathForPackaging(ctx, suiteName+"_list.zip")

	rule.Command().BuiltTool("soong_zip").
		FlagWithOutput("-o ", testListZipOutputFile).
		FlagWithArg("-C ", pathForPackaging(ctx).String()).
		FlagWithInput("-f ", testList).
		Flag("-sha256")

	rule.Build(strings.ReplaceAll(suiteName, "-", "_")+"_zip", suiteName+".zip")

	return outputFile, testListZipOutputFile
}

func buildTestList(ctx android.SingletonContext, listFile string, installedPaths android.Paths) android.Path {
	buf := &strings.Builder{}
	for _, p := range installedPaths {
		if p.Ext() != ".config" {
			continue
		}
		pc, err := toTestListPath(p.String(), pathForTestCases(ctx).String(), "host/testcases")
		if err != nil {
			ctx.Errorf("Failed to convert path: %s, %v", p.String(), err)
			continue
		}
		buf.WriteString(pc)
		buf.WriteString("\n")
	}
	outputFile := pathForPackaging(ctx, listFile)
	android.WriteFileRuleVerbatim(ctx, outputFile, buf.String())
	return outputFile
}

func toTestListPath(path, relativeRoot, prefix string) (string, error) {
	dest, err := filepath.Rel(relativeRoot, path)
	if err != nil {
		return "", err
	}
	return filepath.Join(prefix, dest), nil
}

func pathForPackaging(ctx android.PathContext, pathComponents ...string) android.OutputPath {
	pathComponents = append([]string{"packaging"}, pathComponents...)
	return android.PathForOutput(ctx, pathComponents...)
}

func pathForTestCases(ctx android.PathContext) android.InstallPath {
	return android.PathForHostInstall(ctx, "testcases")
}

func packageTestSuite(
	ctx android.SingletonContext,
	modules []android.ModuleProxy,
	files android.Paths,
	sharedLibs android.Paths,
	suiteConfig testSuiteConfig,
	hostSharedLibs android.Paths,
	seenSymlinks map[string]bool) {
	hostOutTestCases := android.PathForHostInstall(ctx, "testcases")
	targetOutTestCases := android.PathForDeviceFirstInstall(ctx, "testcases")
	hostOut := filepath.Dir(hostOutTestCases.String())
	targetOut := filepath.Dir(targetOutTestCases.String())

	testsZip := pathForPackaging(ctx, suiteConfig.name+".zip")
	generalTestsFilesListText := android.PathForDeviceFirstInstall(ctx, "general-tests_files")
	generalTestsHostFilesListText := android.PathForDeviceFirstInstall(ctx, "general-tests_host_files")
	generalTestsTargetFilesListText := android.PathForDeviceFirstInstall(ctx, "general-tests_target_files")
	testsListTxt := pathForPackaging(ctx, suiteConfig.name+"_list.txt")
	testsListZip := pathForPackaging(ctx, suiteConfig.name+"_list.zip")
	testsConfigsZip := pathForPackaging(ctx, suiteConfig.name+"_configs.zip")
	testsHostSharedLibsZip := pathForPackaging(ctx, suiteConfig.name+"_host-shared-libs.zip")
	var listLines, filesListLines, hostFilesListLines, targetFilesListLines []string

	// use intermediate files to hold the file inputs, to prevent argument list from being too long
	testsZipCmdHostFileInput := android.PathForIntermediates(ctx, suiteConfig.name+"_host_list.txt")
	testsZipCmdTargetFileInput := android.PathForIntermediates(ctx, suiteConfig.name+"_target_list.txt")
	var testsZipCmdHostFileInputContent, testsZipCmdTargetFileInputContent []string

	testsZipBuilder := android.NewRuleBuilder(pctx, ctx)
	testsZipCmd := testsZipBuilder.Command().
		BuiltTool("soong_zip").
		Flag("-sha256").
		Flag("-d").
		FlagWithArg("-P ", "host").
		FlagWithArg("-C ", hostOut)

	testsConfigsZipBuilder := android.NewRuleBuilder(pctx, ctx)
	testsConfigsZipCmd := testsConfigsZipBuilder.Command().
		BuiltTool("soong_zip").
		Flag("-d").
		FlagWithOutput("-o ", testsConfigsZip).
		FlagWithArg("-P ", "host").
		FlagWithArg("-C ", hostOut)

	for _, f := range files {
		if strings.HasPrefix(f.String(), hostOutTestCases.String()) {
			testsZipCmdHostFileInputContent = append(testsZipCmdHostFileInputContent, f.String())
			testsZipCmd.Implicit(f)

			if strings.HasSuffix(f.String(), ".config") {
				testsConfigsZipCmd.FlagWithInput("-f ", f)
				listLines = append(listLines, strings.Replace(f.String(), hostOut, "host", 1))
			}
			// Adding files installed in out/host to general-tests-files-list, e.g.,
			// out/host/linux-x86/testcases/hello_world_test/hello_world_test.config
			filesListLines = append(filesListLines, f.String())
			hostFilesListLines = append(hostFilesListLines, f.String())
		}
	}

	if suiteConfig.includeHostSharedLibsInMainZip {
		for _, f := range sharedLibs {
			// Adding host shared libs to general-tests-files-list, e.g.,
			// out/host/linux-x86/testcases/lib64/libc++.so
			filesListLines = append(filesListLines, f.String())
			hostFilesListLines = append(hostFilesListLines, f.String())
			if strings.HasPrefix(f.String(), hostOutTestCases.String()) {
				testsZipCmdHostFileInputContent = append(testsZipCmdHostFileInputContent, f.String())
				testsZipCmd.Implicit(f)
			}
		}
	}

	if suiteConfig.includeCommonHostSharedLibsSymlinksInMainZip {
		commonHostSharedLibsForSymlinks := gatherCommonHostSharedLibsForSymlinks(ctx, suiteConfig.name)
		for _, moduleName := range android.SortedKeys(commonHostSharedLibsForSymlinks) {
			var symlinksForModuleTarget []android.Path
			for _, common := range commonHostSharedLibsForSymlinks[moduleName] {
				if !android.InList(common, hostSharedLibs) {
					continue
				}
				var symlink, libInTestCase android.WritablePath
				var symlinkTargetPrefix string
				if strings.Contains(common.String(), "/lib64/") {
					symlink = android.PathForHostInstall(ctx, "testcases", moduleName, "x86_64", "shared_libs", common.Base())
					symlinkTargetPrefix = "../../../lib64"
					libInTestCase = android.PathForHostInstall(ctx, "testcases", "lib64", common.Base())
				} else {
					symlink = android.PathForHostInstall(ctx, "testcases", moduleName, "x86", "shared_libs", common.Base())
					symlinkTargetPrefix = "../../../lib"
					libInTestCase = android.PathForHostInstall(ctx, "testcases", "lib", common.Base())
				}

				testsZipCmdHostFileInputContent = append(testsZipCmdHostFileInputContent, symlink.String())
				testsZipCmd.Implicit(symlink)

				symlinksForModuleTarget = append(symlinksForModuleTarget, symlink, libInTestCase)

				// Adding host shared libs symbolic links to general-tests-files-list, e.g.,
				// out/host/linux-x86/testcases/hello_world_test/x86_64/shared_libs/libc++.so
				filesListLines = append(filesListLines, symlink.String())
				hostFilesListLines = append(hostFilesListLines, symlink.String())

				if _, exists := seenSymlinks[symlink.String()]; exists {
					continue
				}
				seenSymlinks[symlink.String()] = true
				ctx.Build(pctx, android.BuildParams{
					Rule:   android.Symlink,
					Output: symlink,
					Args: map[string]string{
						"fromPath": fmt.Sprintf("%s/%s", symlinkTargetPrefix, common.Base()),
					},
				})
			}
			if len(symlinksForModuleTarget) != 0 {
				ctx.Phony(moduleName, android.SortedUniquePaths(symlinksForModuleTarget)...)
			}
		}
	}

	android.WriteFileRule(ctx, testsZipCmdHostFileInput, strings.Join(testsZipCmdHostFileInputContent, "\n"))

	testsZipCmd.
		FlagWithArg("-P ", "host").
		FlagWithArg("-C ", hostOut).
		FlagWithInput("-l ", testsZipCmdHostFileInput).
		FlagWithArg("-P ", "target").
		FlagWithArg("-C ", targetOut)
	testsConfigsZipCmd.
		FlagWithArg("-P ", "target").
		FlagWithArg("-C ", targetOut)
	for _, f := range files {
		if strings.HasPrefix(f.String(), targetOutTestCases.String()) {
			testsZipCmdTargetFileInputContent = append(testsZipCmdTargetFileInputContent, f.String())
			testsZipCmd.Implicit(f)

			if strings.HasSuffix(f.String(), ".config") {
				testsConfigsZipCmd.FlagWithInput("-f ", f)
				listLines = append(listLines, strings.Replace(f.String(), targetOut, "target", 1))
			}

			// Adding files installed in out/target to general-tests-files-list, e.g.,
			// out/target/product/vsoc_x86_64_only/testcases/hello_world_test/hello_world_test.config
			filesListLines = append(filesListLines, f.String())
			targetFilesListLines = append(targetFilesListLines, f.String())
		}
	}

	android.WriteFileRule(ctx, testsZipCmdTargetFileInput, strings.Join(testsZipCmdTargetFileInputContent, "\n"))
	testsZipCmd.FlagWithInput("-l ", testsZipCmdTargetFileInput)

	if len(suiteConfig.hostJavaToolFiles) > 0 {
		testsZipCmd.FlagWithArg("-P ", "host/tools")
		testsZipCmd.Flag("-j")

		for _, hostJavaTool := range suiteConfig.hostJavaToolFiles {
			testsZipCmd.FlagWithInput("-f ", hostJavaTool)
		}
	}

	// Special handling for art_data_zips. This is a custom property for ART's needs.
	// It points to a filegroup that generates the art_common directory, which
	// must be placed under host/testcases in the final zip.
	if len(suiteConfig.Art_data_zips) > 0 {
		unmergedTestsZip := pathForPackaging(ctx, suiteConfig.name+"-unmerged.zip")
		testsZipCmd.FlagWithOutput("-o ", unmergedTestsZip)
		testsZipBuilder.Build(suiteConfig.name+"-unmerged", "building "+suiteConfig.name+" unmerged zip")

		mergeBuilder := android.NewRuleBuilder(pctx, ctx)
		mergeBuilder.Command().
			BuiltTool("merge_zips").
			Output(testsZip).
			Inputs(android.Paths{unmergedTestsZip}).
			Inputs(suiteConfig.Art_data_zips)
		mergeBuilder.Build(suiteConfig.name+"-merge", "merging "+suiteConfig.name+" zip")
	} else {
		testsZipCmd.FlagWithOutput("-o ", testsZip)
		testsZipBuilder.Build(suiteConfig.name, "building "+suiteConfig.name+" zip")
	}

	testsConfigsZipBuilder.Build(suiteConfig.name+"-configs", "building "+suiteConfig.name+" configs zip")

	if suiteConfig.buildHostSharedLibsZip {
		testsHostSharedLibsZipBuilder := android.NewRuleBuilder(pctx, ctx)
		testsHostSharedLibsZipCmd := testsHostSharedLibsZipBuilder.Command().
			BuiltTool("soong_zip").
			Flag("-d").
			FlagWithOutput("-o ", testsHostSharedLibsZip).
			FlagWithArg("-P ", "host").
			FlagWithArg("-C ", hostOut)

		for _, f := range sharedLibs {
			if strings.HasPrefix(f.String(), hostOutTestCases.String()) && strings.HasSuffix(f.String(), ".so") {
				testsHostSharedLibsZipCmd.FlagWithInput("-f ", f)
			}
		}

		testsHostSharedLibsZipBuilder.Build(suiteConfig.name+"-host-shared-libs", "building "+suiteConfig.name+"host shared libs")
	}

	android.WriteFileRule(ctx, testsListTxt, strings.Join(listLines, "\n"))
	// https://source.corp.google.com/h/googleplex-android/platform/build/+/c34c3738ba6be7ef1fc3acb7be5122bede415789
	if suiteConfig.name == "general-tests" {
		android.WriteFileRule(ctx, generalTestsFilesListText, strings.Join(filesListLines, "\n"))
		android.WriteFileRule(ctx, generalTestsHostFilesListText, strings.Join(hostFilesListLines, "\n"))
		android.WriteFileRule(ctx, generalTestsTargetFilesListText, strings.Join(targetFilesListLines, "\n"))
	}
	testsListZipBuilder := android.NewRuleBuilder(pctx, ctx)
	testsListZipBuilder.Command().
		BuiltTool("soong_zip").
		Flag("-d").
		FlagWithOutput("-o ", testsListZip).
		FlagWithArg("-e ", suiteConfig.name+"_list").
		FlagWithInput("-f ", testsListTxt)
	testsListZipBuilder.Build(suiteConfig.name+"_list_zip", "building "+suiteConfig.name+" list zip")

	if ctx.Config().JavaCoverageEnabled() {
		jacocoJar := pathForPackaging(ctx, suiteConfig.name+"_jacoco_report_classes.jar")
		java.BuildJacocoZip(ctx, modules, jacocoJar)
	}

	ctx.Phony(suiteConfig.name, testsZip, testsListZip, testsConfigsZip)
	ctx.Phony("general-tests-files-list", generalTestsFilesListText, generalTestsHostFilesListText, generalTestsTargetFilesListText)
	ctx.DistForGoal(suiteConfig.name, testsZip, testsListZip, testsConfigsZip)
	if suiteConfig.buildHostSharedLibsZip {
		ctx.DistForGoal(suiteConfig.name, testsHostSharedLibsZip)
	}
	ctx.Phony("tests", android.PathForPhony(ctx, suiteConfig.name))
}

func buildCompatibilitySuitePackage(
	ctx android.SingletonContext,
	suite compatibilitySuitePackageInfo,
	testSuiteFiles android.Paths,
	testSuiteModules []android.ModuleProxy,
	testSuiteLibs android.Paths,
) {
	testSuiteName := suite.Name
	subdir := fmt.Sprintf("android-%s", testSuiteName)
	if suite.TestSuiteSubdir != "" {
		subdir = suite.TestSuiteSubdir
	}

	hostOutSuite := android.PathForHostInstall(ctx, testSuiteName)
	hostOutTestCases := hostOutSuite.Join(ctx, subdir, "testcases")
	hostOutTools := hostOutSuite.Join(ctx, subdir, "tools")
	testSuiteFiles = slices.DeleteFunc(testSuiteFiles, func(f android.Path) bool {
		return !strings.HasPrefix(f.String(), hostOutTestCases.String()+"/")
	})

	// Some make rules still rely on the zip being at this location
	out := hostOutSuite.Join(ctx, fmt.Sprintf("android-%s.zip", testSuiteName))
	builder := android.NewRuleBuilder(pctx, ctx)
	cmd := builder.Command().BuiltTool("soong_zip").
		FlagWithOutput("-o ", out).
		FlagWithArg("-e ", subdir+"/tools/version.txt").
		FlagWithInput("-f ", ctx.Config().BuildNumberFile(ctx))

	// Tools need to be copied to the test suite folder for other tools to use, like
	// <suite>-tradefed run commandAndExit
	copyTool := func(tool android.Path) {
		builder.Command().Text("cp").Input(tool).Output(hostOutTools.Join(ctx, tool.Base()))
	}

	hostTools := suite.ToolFiles
	for _, hostTool := range hostTools {
		cmd.
			FlagWithArg("-e ", subdir+"/tools/"+hostTool.Base()).
			FlagWithInput("-f ", hostTool)
		copyTool(hostTool)
	}

	if suite.Readme != nil {
		cmd.
			FlagWithArg("-e ", subdir+"/tools/"+suite.Readme.Base()).
			FlagWithInput("-f ", suite.Readme)
		copyTool(suite.Readme)
	}

	if suite.Aliases != nil {
		cmd.
			FlagWithArg("-e ", subdir+"/tools/aliases").
			FlagWithInput("-f ", suite.Aliases)
		builder.Command().Text("cp").Input(suite.Aliases).Output(hostOutTools.Join(ctx, "aliases"))
	}

	if suite.DynamicConfig != nil {
		cmd.
			FlagWithArg("-e ", subdir+"/testcases/"+testSuiteName+".dynamic").
			FlagWithInput("-f ", suite.DynamicConfig)
		builder.Command().Text("cp").Input(suite.DynamicConfig).Output(hostOutTestCases.Join(ctx, testSuiteName+".dynamic"))
	}

	hostSharedLibs := suite.HostSharedLibs
	for _, hostSharedLib := range hostSharedLibs {
		cmd.
			FlagWithArg("-e ", subdir+"/lib64/"+hostSharedLib.Base()).
			FlagWithInput("-f ", hostSharedLib)
		builder.Command().Text("cp").Input(hostSharedLib).Output(hostOutSuite.Join(ctx, subdir, "lib64", hostSharedLib.Base()))
	}

	licenceInfos := slices.Concat(android.GetNoticeModuleInfos(ctx, testSuiteModules), suite.ToolNoticeInfo)
	if len(licenceInfos) > 0 {
		notice := android.PathForOutput(ctx, "compatibility_test_suites", testSuiteName, "NOTICE.txt")
		android.BuildNoticeTextOutputFromNoticeModuleInfos(ctx, notice, "compatibility_"+testSuiteName, "Test suites",
			android.BuildNoticeFromLicenseDataArgs{
				Title:       "Notices for files contained in the test suites filesystem image:",
				StripPrefix: []string{hostOutSuite.String()},
				Filter:      slices.Concat(testSuiteFiles.Strings(), hostTools.Strings()),
				Replace: []android.NoticeReplace{
					{
						From: ctx.Config().HostJavaToolPath(ctx, "").String(),
						To:   "/" + subdir + "/tools",
					},
					{
						From: ctx.Config().HostToolPath(ctx, "").String(),
						To:   "/" + subdir + "/tools",
					},
				},
			},
			licenceInfos)

		cmd.
			FlagWithArg("-e ", subdir+"/NOTICE.txt").
			FlagWithInput("-f ", notice)
	}

	cmd.FlagWithArg("-C ", hostOutSuite.String())
	for _, f := range testSuiteFiles {
		cmd.FlagWithInput("-f ", f)
	}

	cmd.FlagWithArg("-C ", hostOutSuite.String())
	for _, f := range testSuiteLibs {
		cmd.FlagWithInput("-f ", f)
	}

	addJdkToZip(ctx, cmd, subdir)

	builder.Build("compatibility_zip_"+testSuiteName, fmt.Sprintf("Compatibility test suite zip %q", testSuiteName))

	ctx.Phony(testSuiteName, out)
	// TODO: Enable NoDist by default for all compatibility test suites.
	if !suite.NoDist {
		ctx.DistForGoal(testSuiteName, out)
	}

	if suite.BuildSharedReport {
		buildSharedReport(ctx, suite, testSuiteFiles, testSuiteModules, testSuiteLibs, subdir, out)
	}

	if suite.BuildTestList == true {
		// Original compatibility_tests_list_zip in build/make/core/tasks/tools/compatibility.mk
		// output is $(HOST_OUT)/$(test_suite_name)/android-$(test_suite_name)-tests_list.zip
		testsListTxt := hostOutSuite.Join(ctx, fmt.Sprintf("android-%s-tests_list", testSuiteName))
		testsListZip := hostOutSuite.Join(ctx, fmt.Sprintf("android-%s-tests_list.zip", testSuiteName))
		var listLines []string
		for _, f := range testSuiteFiles {
			if f.Ext() == ".config" {
				listLines = append(listLines, strings.TrimPrefix(f.String(), hostOutTestCases.String()+"/"))
			}
		}
		sort.Strings(listLines)
		android.WriteFileRule(ctx, testsListTxt, strings.Join(listLines, "\n"))

		testsListZipBuilder := android.NewRuleBuilder(pctx, ctx)
		testsListZipBuilder.Command().
			BuiltTool("soong_zip").
			FlagWithOutput("-o ", testsListZip).
			FlagWithArg("-C ", hostOutSuite.String()).
			FlagWithInput("-f ", testsListTxt)
		testsListZipBuilder.Build("tests_list_"+testSuiteName, fmt.Sprintf("Compatibility test suite test list %q", testSuiteName))

		ctx.Phony(testSuiteName, testsListZip)
		ctx.DistForGoal(testSuiteName, testsListZip)
	}

	if suite.BuildMetadata {
		metadata_rule := android.NewRuleBuilder(pctx, ctx)
		compatibility_files_metadata := hostOutSuite.Join(ctx, fmt.Sprintf("%s_files_metadata.textproto", testSuiteName))

		metadata_rule.Command().BuiltTool("file_metadata_generation").
			FlagWithArg("--testcases_dir ", hostOutTestCases.String()).
			FlagWithInput("--aapt2 ", ctx.Config().HostToolPath(ctx, "aapt2")).
			FlagWithArg("--sdk_version ", ctx.Config().PlatformSdkVersion().String()).
			FlagWithOutput("--output ", compatibility_files_metadata).
			Implicit(out)
		metadata_rule.Build("compatibility_metadata_"+testSuiteName, fmt.Sprintf("Compatibility test suite metadata file %q", testSuiteName))

		ctx.Phony(testSuiteName, compatibility_files_metadata)
		if !suite.NoDist {
			ctx.DistForGoal(testSuiteName, compatibility_files_metadata)
		}
	}
}

func buildSharedReport(
	ctx android.SingletonContext,
	suite compatibilitySuitePackageInfo,
	testSuiteFiles android.Paths,
	testSuiteModules []android.ModuleProxy,
	testSuiteLibs android.Paths,
	subdir string,
	out android.InstallPath,
) {
	testSuiteName := suite.Name
	hostOutSuite := android.PathForHostInstall(ctx, testSuiteName)
	hostOutSubDir := hostOutSuite.String() + "/" + subdir + "/"

	compatibilityOutput := android.PathForOutput(ctx, "compatibility_test_suites", testSuiteName)
	metaLicOutput := compatibilityOutput.Join(ctx, "meta_lic")
	// Aggregate license metadata from all component modules into a single file.
	var componentMetadataFiles android.Paths
	meta_builder := android.NewRuleBuilder(pctx, ctx)
	for _, mod := range testSuiteModules {
		if provider, ok := android.OtherModuleProvider(ctx, mod, android.LicenseMetadataProvider); ok && provider.LicenseMetadataPath != nil {
			if testSuiteInstalls, ok := android.OtherModuleProvider(ctx, mod, android.TestSuiteInstallsInfoProvider); ok {
				for _, f := range testSuiteInstalls.Files {
					if strings.Contains(f.Dst.String(), mod.Name()) && strings.HasPrefix(f.Dst.String(), hostOutSubDir) {
						if f.Dst.Ext() == ".jar" || f.Dst.Ext() == ".apk" {
							subdir_meta_file := strings.TrimPrefix(f.Dst.String(), hostOutSubDir) + ".meta_lic"
							target_meta_file := metaLicOutput.Join(ctx, subdir_meta_file)
							meta_builder.Command().Text("cp").
								Input(provider.LicenseMetadataPath).
								Output(target_meta_file)
							componentMetadataFiles = append(componentMetadataFiles, target_meta_file)
							break
						}
					}
				}
			}
		}
	}

	var toolFilenames []string
	for _, f := range suite.ToolFiles {
		toolFilenames = append(toolFilenames, f.Base())
	}
	for _, info := range suite.ToolNoticeInfo {
		if info.LicenseMetadataFile != nil {
			var installed_files []string
			for _, filename := range toolFilenames {
				fname := strings.SplitN(filename, ".", 2)
				if fname[0] == info.Name {
					installed_files = append(installed_files, filename)
				}
			}
			for _, f := range installed_files {
				target_tool_file := metaLicOutput.Join(ctx, "tools", f+".meta_lic")
				meta_builder.Command().Text("cp").
					Input(info.LicenseMetadataFile).
					Output(target_tool_file)
				componentMetadataFiles = append(componentMetadataFiles, target_tool_file)
			}
		}
	}
	meta_builder.Build("cp_meta_lic_"+testSuiteName, fmt.Sprintf("cp meta lic %q", testSuiteName))
	componentMetadataFiles = android.SortedUniquePaths(componentMetadataFiles)

	// Also include all the files that are directly included in the zip as sources.
	allSources := slices.Concat(testSuiteFiles, testSuiteLibs)
	allSources = android.SortedUniquePaths(allSources)
	metaLic := android.PathForOutput(ctx, out.Base()+".meta_lic")
	var args []string
	args = append(args, "-mn ", out.Base())
	args = append(args, "-mt compatibility_test_suite_package")
	args = append(args, "--is_container")
	args = append(args, android.JoinWithPrefix(proptools.NinjaAndShellEscapeListIncludingSpaces(componentMetadataFiles.Strings()), "-d "))
	args = append(args, android.JoinWithPrefix(proptools.NinjaAndShellEscapeListIncludingSpaces(allSources.Strings()), "-s "))
	ctx.Build(pctx, android.BuildParams{
		Rule:        licenseMetadataRule,
		Output:      metaLic,
		Implicits:   append(componentMetadataFiles, allSources...),
		Description: fmt.Sprintf("aggregate %q license metadata", testSuiteName),
		Args: map[string]string{
			"args": strings.Join(args, " "),
		},
	})

	// Only gts build shared report. The output is gts-shared-report.txt.
	gtsReportBuilder := android.NewRuleBuilder(pctx, ctx)
	reportCmd := gtsReportBuilder.Command().BuiltTool("generate_gts_shared_report")
	reportCmd.FlagWithInput("--checkshare ", ctx.Config().HostToolPath(ctx, "compliance_checkshare"))
	shared_report := hostOutSuite.Join(ctx, "gts-shared-report.txt")
	reportCmd.FlagWithOutput("-o ", shared_report)
	reportCmd.FlagWithInput("--gts-test-metalic ", metaLic)
	reportCmd.FlagWithArg("--gts-test-dir ", "compatibility_test_suites/"+testSuiteName+"/meta_lic")
	reportCmd.Implicit(out)
	gtsReportBuilder.Build("gen_"+testSuiteName+"_shared_report", "generate gts_shared_report.txt")

	ctx.Phony(testSuiteName, shared_report)
	ctx.DistForGoal(testSuiteName, shared_report)
}

func addJdkToZip(ctx android.SingletonContext, command *android.RuleBuilderCommand, subdir string) {
	jdkHome := filepath.Dir(ctx.Config().Getenv("ANDROID_JAVA_HOME")) + "/linux-x86"
	glob := jdkHome + "/**/*"
	files, err := ctx.GlobWithDeps(glob, nil)
	if err != nil {
		ctx.Errorf("Could not glob %s: %s", glob, err)
		return
	}
	paths := android.PathsForSource(ctx, files)

	command.
		FlagWithArg("-P ", subdir+"/jdk").
		FlagWithArg("-C ", jdkHome).
		FlagWithArg("-D ", jdkHome).
		Flag("-sha256").Implicits(paths)
}

// compatibility_test_suite_package builds a zip file of all the tests tagged with this suite's
// name. It's the equivalent of compatibility.mk from the make build system.
//
// In soong, the module does nothing. But a singleton finds all the compatibility_test_suite_package
// modules in the tree and emits build rules for them. This is because they need to search
// all the modules in the tree for ones tagged with the appropriate test suite, but this could
// maybe be replaced with reverse dependencies.
func compatibilityTestSuitePackageFactory() android.Module {
	m := &compatibilityTestSuitePackage{}
	android.InitAndroidModule(m)
	m.AddProperties(&m.properties)
	return m
}

type compatibilityTestSuitePackageProperties struct {
	Tradefed            *string
	Readme              *string `android:"path"`
	Tools               []string
	Dynamic_config      *string `android:"path"`
	Host_shared_libs    []string
	Build_test_list     *bool
	Build_metadata      *bool
	Build_shared_report *bool
	// If true, the test suite will not be included in the dist-for-goal method.
	No_dist *bool
	// If set, this will override the name property used in the zip file. This is useful when the test suite
	// requires post-processing, so the module name does not conflict with the original test suite name.
	Test_suite_name   *string `json:"test_suite_name"`
	Test_suite_subdir *string
	// Path to the config defining command aliases for the test suite console.
	Aliases *string `android:"path"`
	phony.PhonyProperties
}

type compatibilityTestSuitePackage struct {
	android.ModuleBase
	properties compatibilityTestSuitePackageProperties
}

type compatibilitySuitePackageInfo struct {
	Name              string
	Readme            android.Path
	DynamicConfig     android.Path
	ToolFiles         android.Paths
	ToolNoticeInfo    android.NoticeModuleInfos
	HostSharedLibs    android.Paths
	BuildTestList     bool
	BuildMetadata     bool
	BuildSharedReport bool
	NoDist            bool
	TestSuiteSubdir   string
	Aliases           android.Path
}

var compatibilitySuitePackageProvider = blueprint.NewProvider[compatibilitySuitePackageInfo]()

type ctspTradefedDeptagType struct {
	blueprint.BaseDependencyTag
}

var ctspTradefedDeptag = ctspTradefedDeptagType{}

type ctspHostJavaToolDeptagType struct {
	blueprint.BaseDependencyTag
}

var ctspHostJavaToolDeptag = ctspHostJavaToolDeptagType{}

type ctspHostToolDeptagType struct {
	blueprint.BaseDependencyTag
}

var ctspHostToolDeptag = ctspHostToolDeptagType{}

type ctspHostSharedLibDeptagType struct {
	blueprint.BaseDependencyTag
}

var ctspHostSharedLibDeptag = ctspHostSharedLibDeptagType{}

func (m *compatibilityTestSuitePackage) DepsMutator(ctx android.BottomUpMutatorContext) {
	variations := ctx.Config().BuildOSTarget.Variations()
	ctx.AddVariationDependencies(variations, ctspTradefedDeptag, proptools.String(m.properties.Tradefed))
	ctx.AddVariationDependencies(variations, ctspHostJavaToolDeptag, proptools.String(m.properties.Tradefed)+"-tests")
	ctx.AddVariationDependencies(variations, ctspHostJavaToolDeptag, "tradefed")
	ctx.AddVariationDependencies(variations, ctspHostJavaToolDeptag, "loganalysis")
	ctx.AddVariationDependencies(variations, ctspHostJavaToolDeptag, "compatibility-host-util")
	ctx.AddVariationDependencies(variations, ctspHostJavaToolDeptag, "compatibility-tradefed")
	ctx.AddVariationDependencies(variations, ctspHostToolDeptag, "test-utils-script")
	for _, tool := range m.properties.Tools {
		ctx.AddVariationDependencies(variations, ctspHostJavaToolDeptag, tool)
	}
	for _, host_shared_lib := range m.properties.Host_shared_libs {
		ctx.AddVariationDependencies(variations, ctspHostSharedLibDeptag, host_shared_lib)
	}
}

func (m *compatibilityTestSuitePackage) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	suiteName := m.Name()
	if m.properties.Test_suite_name != nil && *m.properties.Test_suite_name != "" {
		suiteName = *m.properties.Test_suite_name
	}

	if matched, err := regexp.MatchString("[a-zA-Z0-9_-]+", suiteName); err != nil || !matched {
		ctx.ModuleErrorf("Invalid test suite name, must match [a-zA-Z0-9_-]+, got %q", suiteName)
		return
	}

	tradefedName := proptools.String(m.properties.Tradefed)
	tradefed := ctx.GetDirectDepProxyWithTag(tradefedName, ctspTradefedDeptag)

	tradefedFiles := android.OtherModuleProviderOrDefault(ctx, tradefed, android.InstallFilesProvider).InstallFiles

	if len(tradefedFiles) != 2 || tradefedFiles[0].Base() != tradefedName || tradefedFiles[1].Base() != tradefedName+".jar" {
		ctx.PropertyErrorf("tradefed", "Dependency %q did not provide expected files, produced: %s", tradefedName, tradefedFiles.Strings())
	}

	var toolFiles android.Paths
	for _, f := range tradefedFiles {
		toolFiles = append(toolFiles, f)
	}
	toolNoticeinfo := android.NoticeModuleInfos{android.GetNoticeModuleInfo(ctx, tradefed)}

	ctx.VisitDirectDepsProxyWithTag(ctspHostJavaToolDeptag, func(dep android.ModuleProxy) {
		file := android.OtherModuleProviderOrDefault(ctx, dep, java.JavaInfoProvider).InstallFile
		if file == nil {
			ctx.PropertyErrorf("tools", "Dependency %q did not provide java installfile, is it a java module?", ctx.OtherModuleName(dep))
		}
		toolFiles = append(toolFiles, file)
		toolNoticeinfo = append(toolNoticeinfo, android.GetNoticeModuleInfo(ctx, dep))
	})

	ctx.VisitDirectDepsProxyWithTag(ctspHostToolDeptag, func(dep android.ModuleProxy) {
		files := android.OtherModuleProviderOrDefault(ctx, dep, android.InstallFilesProvider).InstallFiles
		if len(files) != 1 {
			ctx.ModuleErrorf("Dependency %q did not provide expected single file", ctx.OtherModuleName(dep))
		}
		for _, f := range files {
			toolFiles = append(toolFiles, f)
		}
		toolNoticeinfo = append(toolNoticeinfo, android.GetNoticeModuleInfo(ctx, dep))
	})

	var hostSharedLibs android.Paths
	ctx.VisitDirectDepsProxyWithTag(ctspHostSharedLibDeptag, func(dep android.ModuleProxy) {
		libs := android.OtherModuleProviderOrDefault(ctx, dep, android.InstallFilesProvider).InstallFiles
		for _, lib := range libs {
			hostSharedLibs = append(hostSharedLibs, lib)
		}
		toolNoticeinfo = append(toolNoticeinfo, android.GetNoticeModuleInfo(ctx, dep))
	})

	var readme android.Path
	if m.properties.Readme != nil {
		readme = android.PathForModuleSrc(ctx, *m.properties.Readme)
	}

	var dynamicConfig android.Path
	if m.properties.Dynamic_config != nil {
		dynamicConfig = android.PathForModuleSrc(ctx, *m.properties.Dynamic_config)
	}

	var aliases android.Path
	if m.properties.Aliases != nil {
		aliases = android.PathForModuleSrc(ctx, *m.properties.Aliases)
	}

	android.SetProvider(ctx, compatibilitySuitePackageProvider, compatibilitySuitePackageInfo{
		Name:              suiteName,
		Readme:            readme,
		DynamicConfig:     dynamicConfig,
		ToolFiles:         toolFiles,
		ToolNoticeInfo:    toolNoticeinfo,
		HostSharedLibs:    hostSharedLibs,
		BuildTestList:     proptools.BoolDefault(m.properties.Build_test_list, true),
		BuildMetadata:     proptools.Bool(m.properties.Build_metadata),
		BuildSharedReport: proptools.Bool(m.properties.Build_shared_report),
		NoDist:            proptools.Bool(m.properties.No_dist),
		TestSuiteSubdir:   proptools.String(m.properties.Test_suite_subdir),
		Aliases:           aliases,
	})

	// Make compatibility_test_suite_package a SourceFileProducer so that it can be used by other modules.
	ctx.SetOutputFiles(android.Paths{android.PathForHostInstall(ctx, suiteName, fmt.Sprintf("android-%s.zip", suiteName))}, "")
	for _, dep := range m.properties.Phony_deps.GetOrDefault(ctx, nil) {
		ctx.Phony(m.Name(), android.PathForPhony(ctx, dep))
	}
}

func testSuitePackageFactory() android.Module {
	m := &testSuitePackage{}
	android.InitAndroidModule(m)
	m.AddProperties(&m.properties)
	return m
}

type testSuitePackageProperties struct {
	Build_host_shared_libs_zip           *bool
	Include_host_shared_libs_in_main_zip *bool
	// When true, a symlink will be created per test for any
	// shared library dependencies that are not installed alongside the test.
	//
	// e.g. host/testcases/$test/x86_64/shared_libs/libfoo.so --> ../../../lib64/libfoo.so
	// The target of the symlink will be the host/testcases/lib64/libfoo.so
	Include_common_host_shared_libs_symlinks_in_main_zip *bool
	Host_java_tools                                      []string
	// Custom property for ART's needs. This points to a zip file (or a list of zip files)
	// that provides extra data files for ART host tests. The content of the zip(s) will be
	// merged into the final test suite zip. For example, it is used to package the
	// art_common_host_data_files.zip file.
	Art_data_zips []string `android:"path"`
}

type testSuitePackage struct {
	android.ModuleBase
	properties testSuitePackageProperties
	outputFile android.Path
}

var testSuitePackageProvider = blueprint.NewProvider[testSuiteConfig]()

type tspHostJavaToolDeptagType struct {
	blueprint.BaseDependencyTag
}

var tspHostJavaToolDeptag = tspHostJavaToolDeptagType{}

func (t *testSuitePackage) DepsMutator(ctx android.BottomUpMutatorContext) {
	variations := ctx.Config().BuildOSTarget.Variations()
	ctx.AddVariationDependencies(variations, tspHostJavaToolDeptag, t.properties.Host_java_tools...)
}

// testSuitePackage does not have build actions of its own.
// It sets a provider with info about its packaging config.
// A singleton will create the build rules to create the .zip files.
func (t *testSuitePackage) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	var toolFiles android.Paths
	ctx.VisitDirectDepsProxyWithTag(tspHostJavaToolDeptag, func(dep android.ModuleProxy) {
		file := android.OtherModuleProviderOrDefault(ctx, dep, java.JavaInfoProvider).InstallFile
		if file == nil {
			ctx.PropertyErrorf("host_java_tools", "Dependency %q did not provide java installfile, is it a java module?", ctx.OtherModuleName(dep))
		}
		toolFiles = append(toolFiles, file)
	})

	android.SetProvider(ctx, testSuitePackageProvider, testSuiteConfig{
		name:                           t.Name(),
		buildHostSharedLibsZip:         proptools.Bool(t.properties.Build_host_shared_libs_zip),
		includeHostSharedLibsInMainZip: proptools.Bool(t.properties.Include_host_shared_libs_in_main_zip),
		includeCommonHostSharedLibsSymlinksInMainZip: proptools.Bool(t.properties.Include_common_host_shared_libs_symlinks_in_main_zip),
		hostJavaToolFiles: toolFiles,
		Art_data_zips:     android.PathsForModuleSrc(ctx, t.properties.Art_data_zips),
	})

	t.outputFile = pathForPackaging(ctx, t.Name()+".zip")

	if ctx.Config().JavaCoverageEnabled() {
		jacocoJar := pathForPackaging(ctx, t.Name()+"_jacoco_report_classes.jar")
		ctx.SetOutputFiles(android.Paths{jacocoJar}, ".jacoco")

		// This phony is for BWYN, as it will try to "optimize" the test zip file but doesn't
		// have logic to optimize the jacoco zip. So it just builds the jacoco zip separately.
		ctx.Phony(ctx.ModuleName()+"-jacoco", jacocoJar)
	}
}

// Some things (like the module phony and disting) still need an AndroidMkEntries() function
// despite this module not being used from make.
func (p *testSuitePackage) AndroidMkEntries() []android.AndroidMkEntries {
	return []android.AndroidMkEntries{
		android.AndroidMkEntries{
			Class:      "ETC",
			OutputFile: android.OptionalPathForPath(p.outputFile),
			ExtraEntries: []android.AndroidMkExtraEntriesFunc{
				func(ctx android.AndroidMkExtraEntriesContext, entries *android.AndroidMkEntries) {
					entries.SetBool("LOCAL_UNINSTALLABLE_MODULE", true)
				}},
		},
	}
}
