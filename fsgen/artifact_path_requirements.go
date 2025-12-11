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

package fsgen

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"android/soong/android"
)

var (
	staticAllowedPatterns = []string{
		// Fakes don't get installed
		"fake_packages/%",
		// RROs become REQUIRED by the source module, but are always placed on the vendor partition.
		"%__auto_generated_characteristics_rro.apk",
		"%__auto_generated_rro_product.apk",
		"%__auto_generated_rro_vendor.apk",
		// $(PRODUCT_OUT)/apex is where shared libraries in APEXes get installed.
		// The path can be considered as a fake path, as the shared libraries are installed there
		// just to have symbols files for them under $(PRODUCT_OUT)/symbols/apex for debugging
		// purpose. The /apex directory is never compiled into a filesystem image.
		"apex/%",
		// Allow system_other odex space optimization.
		"system_other/%.odex",
		"system_other/%.vdex",
		"system_other/%.art",
	}
)

func init() {
	android.RegisterParallelSingletonType("artifact_path_requirements_verifier", ArtifactPathRequirementsVerifierFactory)
}

type artifactPathRequirementsVerifierSingleton struct {
}

func ArtifactPathRequirementsVerifierFactory() android.Singleton {
	return &artifactPathRequirementsVerifierSingleton{}
}

// Replace the Make-defined path place-holder. Add a wildcard suffix if paths are prefixes.
func resolveMakeRelativePaths(cfg android.DeviceConfig, paths []string, suffix string) (ret []string) {
	replacer := strings.NewReplacer(
		"||VENDOR-PATH-PH||", cfg.VendorPath(),
		"||PRODUCT-PATH-PH||", cfg.ProductPath(),
		"||SYSTEM_EXT-PATH-PH||", cfg.SystemExtPath(),
		"||ODM-PATH-PH||", cfg.OdmPath(),
		"||VENDOR_DLKM-PATH-PH||", cfg.VendorDlkmPath(),
		"||ODM_DLKM-PATH-PH||", cfg.OdmDlkmPath(),
		"||SYSTEM_DLKM-PATH-PH||", cfg.SystemDlkmPath(),
	)
	for _, path := range paths {
		ret = append(ret, replacer.Replace(path)+suffix)
	}
	return
}

// Return true if any of make patterns match on the path string.
func matchMakePatterns(path string, patterns []string) (bool, string) {
	for _, pattern := range patterns {
		if android.MatchPattern(pattern, path) {
			return true, pattern
		}
	}
	return false, ""
}

// This implements "filter" and "filter-out" make functions.
// It also returns unused patterns to check any unused allowed list for the strict enforcement.
func filterPatterns(patterns []string, entries []string, filterOut bool) ([]string, []string) {
	usedPatterns := make(map[string]bool)
	var ret []string

	for _, entry := range entries {
		if ok, pattern := matchMakePatterns(entry, patterns); ok {
			usedPatterns[pattern] = true
			if !filterOut {
				ret = append(ret, entry)
			}
		} else {
			if filterOut {
				ret = append(ret, entry)
			}
		}
	}

	var unusedPatterns []string
	for _, pattern := range patterns {
		if !usedPatterns[pattern] {
			unusedPatterns = append(unusedPatterns, pattern)
		}
	}
	return ret, unusedPatterns
}

func printListAndError(ctx android.SingletonContext, offending []string, message string) {
	if len(offending) > 0 {
		errStr := fmt.Sprintf("%s\nOffending entries:\n", message)
		for _, entry := range offending {
			errStr += fmt.Sprintf("    %s\n", entry)
		}
		ctx.Errorf("%s", errStr)
	}
}

func (s *artifactPathRequirementsVerifierSingleton) GenerateBuildActions(ctx android.SingletonContext) {
	partitionVars := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse
	enforcement := partitionVars.EnforceArtifactPathRequirements
	if !android.InList(enforcement, []string{"", "true", "false", "relaxed", "strict"}) {
		ctx.Errorf("PRODUCT_ENFORCE_ARTIFACT_PATH_REQUIREMENTS must be one of [true, false, relaxed, strict], found: %s", enforcement)
	}

	allInstalledModules := productInstalledModules(ctx, "all")
	allInstalledFiles := make(map[string]bool)
	allOffendingFiles := make(map[string]bool)
	installedModulesOfMakefile := make(map[string][]string)
	installedFilesOfMakefile := make(map[string]map[string]bool)
	internalOffendingFiles := make(map[string][]string)
	externalOffendingFiles := make(map[string][]string)
	externalOffendingProps := make(map[string][]string)
	externalOffendingFcms := make(map[string][]string)
	internalUnusedAllowedList := make(map[string][]string)
	externalUnusedAllowedList := make(map[string][]string)
	for _, makefile := range partitionVars.ArtifactPathRequirementProducts {
		installedModulesOfMakefile[makefile] = productInstalledModules(ctx, makefile)
		installedFilesOfMakefile[makefile] = make(map[string]bool)
	}

	ctx.VisitAllModulesOrProxies(func(m android.ModuleOrProxy) {
		info, ok := android.OtherModuleProvider(ctx, m, android.CommonModuleInfoProvider)
		// The module names listed in the PRODUCT_PACKAGES are the primary variants in soong, that
		// we want to vefify here. Skip non-primary variants.
		if !ok || !info.Enabled || info.SkipInstall || info.NoFullInstall || info.Host || info.IsNonPrimaryImageVariation {
			return
		}

		// m.Name() returns soong-modified names, for example, prebuilt modules have 'prebuilt_'
		// prefix. Use BaseModuleName for the names in PRODUCT_PACKAGES.
		name := info.BaseModuleName
		if !android.InList(name, allInstalledModules) {
			return
		}

		if installInfo, ok := android.OtherModuleProvider(ctx, m, android.InstallFilesProvider); ok {
			for _, ps := range installInfo.TransitivePackagingSpecs.ToList() {
				if ps.SkipInstall() || ps.Partition() == "" || ps.InstallInSanitizerDir() {
					continue
				}
				installedFile := filepath.Join(ps.Partition(), ps.RelPathInPackage())
				allInstalledFiles[installedFile] = true
				for _, makefile := range partitionVars.ArtifactPathRequirementProducts {
					if android.InList(name, installedModulesOfMakefile[makefile]) {
						installedFilesOfMakefile[makefile][installedFile] = true
					}
				}
			}
		}
	})

	for _, makefile := range partitionVars.ArtifactPathRequirementProducts {
		pathPatterns := resolveMakeRelativePaths(ctx.DeviceConfig(), partitionVars.ArtifactPathRequirementsOfMakefile[makefile], "%")

		// Verify that the product only produces files inside its path requirements.
		allowedPatterns := resolveMakeRelativePaths(ctx.DeviceConfig(), partitionVars.ArtifactPathAllowedListOfMakefile[makefile], "")
		offendingFiles, _ := filterPatterns(append(pathPatterns, staticAllowedPatterns...), android.SortedKeys(installedFilesOfMakefile[makefile]), true)
		internalOffendingFiles[makefile], internalUnusedAllowedList[makefile] = filterPatterns(allowedPatterns, offendingFiles, true)

		// Optionally verify that nothing else produces files inside this artifact path requirement.
		if enforcement == "" || enforcement == "false" {
			continue
		}
		extraFiles, _ := filterPatterns(android.SortedKeys(installedFilesOfMakefile[makefile]), android.SortedKeys(allInstalledFiles), true)
		allowedPatterns = resolveMakeRelativePaths(ctx.DeviceConfig(), partitionVars.ArtifactPathRequirementAllowedList, "")
		offendingFiles, _ = filterPatterns(pathPatterns, extraFiles, false)
		for _, f := range offendingFiles {
			allOffendingFiles[f] = true
		}
		externalOffendingFiles[makefile], externalUnusedAllowedList[makefile] = filterPatterns(allowedPatterns, offendingFiles, true)

		// For the artifact path enforced devices, verify that no external makefiles add contents inside the 'system' artifact path.
		if android.InList("system/%", pathPatterns) {
			var allowedSysprops []string
			for _, prop := range partitionVars.ArtifactPathRequirementSyspropAllowedList {
				allowedSysprops = append(allowedSysprops, prop+"=%")
			}
			// Check for PRODUCT_SYSTEM_PROPERTIES
			externalOffendingProps[makefile], _ = filterPatterns(append(partitionVars.SystemPropertiesOfMakefile[makefile], allowedSysprops...), partitionVars.ProductSystemProperties, true)
			// Check for PRODUCT_SYSTEM_DEFAULT_PROPERTIES
			moreOffendingEntries, _ := filterPatterns(append(partitionVars.SystemDefaultPropertiesOfMakefile[makefile], allowedSysprops...), partitionVars.ProductSystemDefaultProperties, true)
			externalOffendingProps[makefile] = append(externalOffendingProps[makefile], moreOffendingEntries...)

			// Check for DEVICE_FRAMEWORK_COMPATIBILITY_MATRIX_FILE
			externalOffendingFcms[makefile], _ = filterPatterns(partitionVars.DeviceFcmFileOfMakefile[makefile], ctx.Config().DeviceFrameworkCompatibilityMatrixFile(), true)
		}
	}

	// Show offending files
	for _, makefile := range android.SortedKeys(internalOffendingFiles) {
		sort.Strings(internalOffendingFiles[makefile])
		errMsg := fmt.Sprintf("%s produces files outside its artifact path requirement.\n", makefile)
		errMsg += fmt.Sprintf("Allowed paths are %s\n", strings.Join(resolveMakeRelativePaths(ctx.DeviceConfig(), partitionVars.ArtifactPathRequirementsOfMakefile[makefile], "*"), ", "))
		printListAndError(ctx, internalOffendingFiles[makefile], errMsg)
	}
	for _, makefile := range android.SortedKeys(internalUnusedAllowedList) {
		sort.Strings(internalUnusedAllowedList[makefile])
		if !partitionVars.ArtifactPathRequirementsIsRelaxedOfMakefile[makefile] {
			errMsg := fmt.Sprintf("%s includes redundant allowed entries in its artifact path requirement.\n", makefile)
			errMsg += "If the modules are defined in Android.mk, They might be missing from the verification. Define the modules in Android.bp instead.\n"
			errMsg += "Otherwise, remove the redundant allowed entries.\n"
			printListAndError(ctx, internalUnusedAllowedList[makefile], errMsg)
		}
	}
	if enforcement != "" && enforcement != "false" {
		for _, makefile := range android.SortedKeys(externalOffendingFiles) {
			sort.Strings(externalOffendingFiles[makefile])
			errMsg := fmt.Sprintf("Device makefile produces files inside %s's artifact path requirement.\n", makefile)
			errMsg += "Consider adding these files to outside of the artifact path requirement instead.\n"
			printListAndError(ctx, externalOffendingFiles[makefile], errMsg)
		}
		for _, makefile := range android.SortedKeys(externalOffendingProps) {
			sort.Strings(externalOffendingProps[makefile])
			errMsg := fmt.Sprintf("Device makefile has PRODUCT_SYSTEM_PROPERTIES or PRODUCT_SYSTEM_DEFAULT_PROPERTIES that add properties to the 'system' partition, which is against %s's artifact path requirement.\n", makefile)
			errMsg += "Please use PRODUCT_PRODUCT_PROPERTIES or PRODUCT_SYSTEM_EXT_PROPERTIES to add them to a different partition instead."
			printListAndError(ctx, externalOffendingProps[makefile], errMsg)
		}
		for _, makefile := range android.SortedKeys(externalOffendingFcms) {
			sort.Strings(externalOffendingFcms[makefile])
			errMsg := fmt.Sprintf("Device makefile has DEVICE_FRAMEWORK_COMPATIBILITY_MATRIX_FILE that adds the FCM files to the 'system' partition, which is against %s's artifact path requirement.\n", makefile)
			errMsg += "Please define a vintf_compatibility_matrix module in Android.bp for each FCM file with 'system_ext_specific: true' or the other partition that implements the feature. Then add each of these vintf_compatibility_matrix modules to PRODUCT_PACKAGES."
			printListAndError(ctx, externalOffendingFcms[makefile], errMsg)
		}
		if enforcement != "relaxed" {
			for _, makefile := range android.SortedKeys(externalUnusedAllowedList) {
				sort.Strings(externalUnusedAllowedList[makefile])
				errMsg := fmt.Sprintf("Device makefile includes redundant artifact path requirement allowed list entries in %s.\n", makefile)
				errMsg += "If the modules are defined in Android.mk, They might be missing from the verification. Define the modules in Android.bp instead.\n"
				errMsg += "Otherwise, remove the redundant allowed entries.\n"
				printListAndError(ctx, externalUnusedAllowedList[makefile], errMsg)
			}
		}
	}

	// This is also defined in artifact_path_requirements.mk which is not used in Soong-only build.
	// HasDeviceProduct() can also be used to see if DeviceName() is present or not.
	if ctx.Config().HasDeviceProduct() && !ctx.Config().KatiEnabled() {
		output := android.PathForArbitraryOutput(ctx, "target", "product", ctx.Config().DeviceName(), "offending_artifacts.txt")
		android.WriteFileRule(ctx, output, strings.Join(android.SortedKeys(allOffendingFiles), "\n"))
	}
}
