// Copyright (C) 2024 The Android Open Source Project
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
	"android/soong/android"
	"android/soong/etc"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/google/blueprint/proptools"
)

type srcBaseFileInstallBaseFileTuple struct {
	srcBaseFile     string
	installBaseFile string
}

// prebuilt src files grouped by the install partitions.
// Each groups are a mapping of the relative install path to the name of the files
type prebuiltSrcGroupByInstallPartition struct {
	system         map[string][]srcBaseFileInstallBaseFileTuple
	system_ext     map[string][]srcBaseFileInstallBaseFileTuple
	product        map[string][]srcBaseFileInstallBaseFileTuple
	vendor         map[string][]srcBaseFileInstallBaseFileTuple
	recovery       map[string][]srcBaseFileInstallBaseFileTuple
	vendor_dlkm    map[string][]srcBaseFileInstallBaseFileTuple
	vendor_ramdisk map[string][]srcBaseFileInstallBaseFileTuple
}

func newPrebuiltSrcGroupByInstallPartition() *prebuiltSrcGroupByInstallPartition {
	return &prebuiltSrcGroupByInstallPartition{
		system:         map[string][]srcBaseFileInstallBaseFileTuple{},
		system_ext:     map[string][]srcBaseFileInstallBaseFileTuple{},
		product:        map[string][]srcBaseFileInstallBaseFileTuple{},
		vendor:         map[string][]srcBaseFileInstallBaseFileTuple{},
		recovery:       map[string][]srcBaseFileInstallBaseFileTuple{},
		vendor_dlkm:    map[string][]srcBaseFileInstallBaseFileTuple{},
		vendor_ramdisk: map[string][]srcBaseFileInstallBaseFileTuple{},
	}
}

func isSubdirectory(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}

func appendIfCorrectInstallPartition(partitionToInstallPathList []partitionToInstallPath, destPath, srcPath string, srcGroup *prebuiltSrcGroupByInstallPartition) {
	for _, part := range partitionToInstallPathList {
		partition := part.name
		installPath := part.installPath

		if isSubdirectory(installPath, destPath) {
			installDir := filepath.Dir(destPath)
			var srcMap map[string][]srcBaseFileInstallBaseFileTuple
			switch partition {
			case "system":
				srcMap = srcGroup.system
			case "system_ext":
				srcMap = srcGroup.system_ext
			case "product":
				srcMap = srcGroup.product
			case "vendor":
				srcMap = srcGroup.vendor
			case "recovery":
				srcMap = srcGroup.recovery
			case "vendor_dlkm":
				srcMap = srcGroup.vendor_dlkm
			case "vendor_ramdisk":
				srcMap = srcGroup.vendor_ramdisk
			}
			if srcMap != nil {
				srcMap[installDir] = append(srcMap[installDir], srcBaseFileInstallBaseFileTuple{
					srcBaseFile:     filepath.Base(srcPath),
					installBaseFile: filepath.Base(destPath),
				})
			}
			return
		}
	}
}

// Create a map of source files to the list of destination files from PRODUCT_COPY_FILES entries.
// Note that the value of the map is a list of string, given that a single source file can be
// copied to multiple files.
// This function also checks the existence of the source files, and validates that there is no
// multiple source files copying to the same dest file.
func uniqueExistingProductCopyFileMap(ctx android.LoadHookContext) map[string][]string {
	seen := make(map[string]bool)
	filtered := make(map[string][]string)

	for _, copyFilePair := range ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse.ProductCopyFiles {
		srcDestList := strings.Split(copyFilePair, ":")
		if len(srcDestList) < 2 {
			ctx.ModuleErrorf("PRODUCT_COPY_FILES must follow the format \"src:dest\", got: %s", copyFilePair)
		}
		src, dest := srcDestList[0], srcDestList[1]

		// Some downstream branches use absolute path as entries in PRODUCT_COPY_FILES.
		// Convert them to relative path from top and check if they do not escape the tree root.
		relSrc := android.ToRelativeSourcePath(ctx, src)

		if _, ok := seen[dest]; !ok {
			if optionalPath := android.ExistentPathForSource(ctx, relSrc); optionalPath.Valid() {
				seen[dest] = true
				filtered[relSrc] = append(filtered[relSrc], dest)
			}
		}
	}

	return filtered
}

type partitionToInstallPath struct {
	name        string
	installPath string
}

func getPartitionToInstallPathList(ctx android.LoadHookContext) []partitionToInstallPath {
	// System is intentionally added at the last to consider the scenarios where
	// non-system partitions are installed as part of the system partition
	partitionToInstallPathList := []partitionToInstallPath{
		{name: "recovery", installPath: "recovery/root"},
		{name: "vendor", installPath: ctx.DeviceConfig().VendorPath()},
		{name: "vendor_dlkm", installPath: ctx.DeviceConfig().VendorDlkmPath()},
		{name: "vendor_ramdisk", installPath: "vendor_ramdisk"},
		{name: "product", installPath: ctx.DeviceConfig().ProductPath()},
		{name: "system_ext", installPath: ctx.DeviceConfig().SystemExtPath()},
		{name: "system", installPath: "system"},
		{name: "system", installPath: "root"},
	}

	return partitionToInstallPathList
}

func processProductCopyFiles(ctx android.LoadHookContext) map[string]*prebuiltSrcGroupByInstallPartition {
	// Filter out duplicate dest entries and non existing src entries
	productCopyFileMap := uniqueExistingProductCopyFileMap(ctx)

	groupedSources := map[string]*prebuiltSrcGroupByInstallPartition{}
	for _, src := range android.SortedKeys(productCopyFileMap) {
		destFiles := productCopyFileMap[src]
		srcFileDir := filepath.Dir(src)
		if _, ok := groupedSources[srcFileDir]; !ok {
			groupedSources[srcFileDir] = newPrebuiltSrcGroupByInstallPartition()
		}
		for _, dest := range destFiles {
			appendIfCorrectInstallPartition(getPartitionToInstallPathList(ctx), dest, filepath.Base(src), groupedSources[srcFileDir])
		}
	}

	return groupedSources
}

type prebuiltModuleProperties struct {
	Name *string

	From_product_copy_files *bool

	Soc_specific                              *bool
	Product_specific                          *bool
	System_ext_specific                       *bool
	Vendor_dlkm_specific                      *bool
	Recovery                                  *bool
	Ramdisk                                   *bool
	Vendor_ramdisk                            *bool
	Install_in_root                           *bool
	Install_path_skip_first_stage_ramdisk_dir *bool

	Srcs []string

	No_full_install *bool

	NamespaceExportedToMake bool

	Visibility []string
}

// Split relative_install_path to a separate struct, because it is not supported for every
// modules listed in [etcInstallPathToFactoryMap]
type prebuiltSubdirProperties struct {
	// If the base file name of the src and dst all match, dsts property does not need to be
	// set, and only relative_install_path can be set.
	Relative_install_path *string
}

// Split install_in_root to a separate struct as it is part of rootProperties instead of
// properties
type prebuiltInstallInRootProperties struct {
	Install_in_root *bool
}

var (
	etcInstallPathToFactoryList = map[string]android.ModuleFactory{
		"":                    etc.PrebuiltRootFactory,
		"avb":                 etc.PrebuiltAvbFactory,
		"bin":                 etc.PrebuiltBinaryFactory,
		"bt_firmware":         etc.PrebuiltBtFirmwareFactory,
		"cacerts":             etc.PrebuiltEtcCaCertsFactory,
		"dsp":                 etc.PrebuiltDSPFactory,
		"etc":                 etc.PrebuiltEtcFactory,
		"etc/dsp":             etc.PrebuiltDSPFactory,
		"etc/firmware":        etc.PrebuiltFirmwareFactory,
		"etc/firmware_8475":        etc.PrebuiltFirmwareFactory,
		"firmware":            etc.PrebuiltFirmwareFactory,
		"gpu":                 etc.PrebuiltGPUFactory,
		"first_stage_ramdisk": etc.PrebuiltFirstStageRamdiskFactory,
		"fonts":               etc.PrebuiltFontFactory,
		"framework":           etc.PrebuiltFrameworkFactory,
		"lib":                 etc.PrebuiltLibFactory,
		"lib64":               etc.PrebuiltRenderScriptBitcodeFactory,
		"lib/rfsa":            etc.PrebuiltRFSAFactory,
		"media":               etc.PrebuiltMediaFactory,
		"odm":                 etc.PrebuiltOdmFactory,
		"optee":               etc.PrebuiltOpteeFactory,
		"overlay":             etc.PrebuiltOverlayFactory,
		"priv-app":            etc.PrebuiltPrivAppFactory,
		"radio":               etc.PrebuiltRadioFactory,
		"sbin":                etc.PrebuiltSbinFactory,
		"system":              etc.PrebuiltSystemFactory,
		"res":                 etc.PrebuiltResFactory,
		"rfs":                 etc.PrebuiltRfsFactory,
		"tee":                 etc.PrebuiltTeeFactory,
		"tts":                 etc.PrebuiltVoicepackFactory,
		"tvconfig":            etc.PrebuiltTvConfigFactory,
		"tvservice":           etc.PrebuiltTvServiceFactory,
		"usr/share":           etc.PrebuiltUserShareFactory,
		"usr/hyphen-data":     etc.PrebuiltUserHyphenDataFactory,
		"usr/keylayout":       etc.PrebuiltUserKeyLayoutFactory,
		"usr/keychars":        etc.PrebuiltUserKeyCharsFactory,
		"usr/srec":            etc.PrebuiltUserSrecFactory,
		"usr/idc":             etc.PrebuiltUserIdcFactory,
		"vendor":              etc.PrebuiltVendorFactory,
		"vendor_dlkm":         etc.PrebuiltVendorDlkmFactory,
		"vendor_overlay":      etc.PrebuiltVendorOverlayFactory,
		"wallpaper":           etc.PrebuiltWallpaperFactory,
		"wlc_upt":             etc.PrebuiltWlcUptFactory,
	}
)

func generatedPrebuiltEtcModuleName(partition, srcDir, destDir string, count int) string {
	// generated module name follows the pattern:
	// <install partition>-<src file path>-<relative install path from partition root>-<number>
	// Note that all path separators are replaced with "_" in the name
	moduleName := partition
	if !android.InList(srcDir, []string{"", "."}) {
		moduleName += fmt.Sprintf("-%s", strings.ReplaceAll(srcDir, string(filepath.Separator), "_"))
	}
	if !android.InList(destDir, []string{"", "."}) {
		moduleName += fmt.Sprintf("-%s", strings.ReplaceAll(destDir, string(filepath.Separator), "_"))
	}
	moduleName += fmt.Sprintf("-%d", count)

	return moduleName
}

func groupDestFilesBySrc(destFiles []srcBaseFileInstallBaseFileTuple) (ret map[string][]srcBaseFileInstallBaseFileTuple, maxLen int) {
	ret = map[string][]srcBaseFileInstallBaseFileTuple{}
	maxLen = 0
	for _, tuple := range destFiles {
		if _, ok := ret[tuple.srcBaseFile]; !ok {
			ret[tuple.srcBaseFile] = []srcBaseFileInstallBaseFileTuple{}
		}
		ret[tuple.srcBaseFile] = append(ret[tuple.srcBaseFile], tuple)
		maxLen = max(maxLen, len(ret[tuple.srcBaseFile]))
	}
	return ret, maxLen
}

func prebuiltEtcModuleProps(ctx android.LoadHookContext, moduleName, partition, destDir string) prebuiltModuleProperties {
	moduleProps := prebuiltModuleProperties{}
	moduleProps.Name = proptools.StringPtr(moduleName)

	// Set partition specific properties
	switch partition {
	case "system_ext":
		moduleProps.System_ext_specific = proptools.BoolPtr(true)
	case "product":
		moduleProps.Product_specific = proptools.BoolPtr(true)
	case "vendor":
		moduleProps.Soc_specific = proptools.BoolPtr(true)
	case "vendor_dlkm":
		moduleProps.Vendor_dlkm_specific = proptools.BoolPtr(true)
	case "vendor_ramdisk":
		moduleProps.Vendor_ramdisk = proptools.BoolPtr(true)
		// Enforce partition path to be "TARGET_COPY_OUT_VENDOR_RAMDISK" by skipping "first_stage_ramdisk".
		moduleProps.Install_path_skip_first_stage_ramdisk_dir = proptools.BoolPtr(true)
		moduleProps.Install_in_root = proptools.BoolPtr(true)
	case "recovery":
		// To match the logic in modulePartition() in android/paths.go
		if ctx.DeviceConfig().BoardUsesRecoveryAsBoot() && strings.HasPrefix(destDir, "first_stage_ramdisk") {
			moduleProps.Ramdisk = proptools.BoolPtr(true)
		} else {
			moduleProps.Recovery = proptools.BoolPtr(true)
		}
	}

	moduleProps.From_product_copy_files = proptools.BoolPtr(true)
	moduleProps.No_full_install = proptools.BoolPtr(true)
	moduleProps.NamespaceExportedToMake = true
	moduleProps.Visibility = []string{"//visibility:public"}

	return moduleProps
}

func createPrebuiltEtcModulesInDirectory(ctx android.LoadHookContext, partition, srcDir, destDir string, destFiles []srcBaseFileInstallBaseFileTuple) (moduleNames []string) {
	groupedDestFiles, maxLen := groupDestFilesBySrc(destFiles)

	for fileIndex := range maxLen {
		srcTuple := []srcBaseFileInstallBaseFileTuple{}
		for _, srcFile := range android.SortedKeys(groupedDestFiles) {
			groupedDestFile := groupedDestFiles[srcFile]
			if len(groupedDestFile) > fileIndex {
				srcTuple = append(srcTuple, groupedDestFile[fileIndex])
			}
		}

		moduleName := generatedPrebuiltEtcModuleName(partition, srcDir, destDir, fileIndex)

		var firstInstallPath string
		for _, pi := range getPartitionToInstallPathList(ctx) {
			if isSubdirectory(pi.installPath, destDir) {
				destDir, _ = filepath.Rel(pi.installPath, destDir)
				firstInstallPath = pi.installPath
				break
			}
		}

		// Find out the most appropriate module type to generate
		var etcInstallPathKey string
		for _, etcInstallPath := range android.SortedKeys(etcInstallPathToFactoryList) {
			// Do not break when found but iterate until the end to find a module with more
			// specific install path
			if strings.HasPrefix(destDir, etcInstallPath) {
				etcInstallPathKey = etcInstallPath
			}
		}
		moduleFactory := etcInstallPathToFactoryList[etcInstallPathKey]
		relDestDirFromInstallDirBase, _ := filepath.Rel(etcInstallPathKey, destDir)

		moduleProps := prebuiltEtcModuleProps(ctx, moduleName, partition, destDir)
		modulePropsPtr := &moduleProps
		propsList := []interface{}{modulePropsPtr}

		allCopyFileNamesUnchanged := true
		var srcBaseFiles, installBaseFiles []string
		for _, tuple := range srcTuple {
			if tuple.srcBaseFile != tuple.installBaseFile {
				allCopyFileNamesUnchanged = false
			}
			srcBaseFiles = append(srcBaseFiles, tuple.srcBaseFile)
			installBaseFiles = append(installBaseFiles, tuple.installBaseFile)
		}

		// Recovery partition-installed modules are installed to `recovery/root/system` by
		// default (See modulePartition() in android/paths.go). If the destination file
		// directory is not `recovery/root/system/...`, it should set install_in_root to true
		// to prevent being installed in `recovery/root/system`.
		if (partition == "recovery" && !strings.HasPrefix(destDir, "system")) ||
			(partition == "system" && firstInstallPath == "root" && destDir == ".") {
			propsList = append(propsList, &prebuiltInstallInRootProperties{
				Install_in_root: proptools.BoolPtr(true),
			})
			// Discard any previously picked module and force it to prebuilt_{root,any} as
			// they are the only modules allowed to specify the `install_in_root` property.
			etcInstallPathKey = ""
			relDestDirFromInstallDirBase = destDir
		}

		// Set appropriate srcs, dsts, and releative_install_path based on
		// the source and install file names
		modulePropsPtr.Srcs = srcBaseFiles

		// prebuilt_root should only be used in very limited cases in prebuilt_etc moddule gen, where:
		// - all source file names are identical to the installed file names, and
		// - all source files are installed in root, not the subdirectories of root
		// prebuilt_root currently does not have a good way to specify the names of the multiple
		// installed files, and prebuilt_root does not allow installing files at a subdirectory
		// of the root.
		// Use prebuilt_any instead of prebuilt_root if either of the conditions are not met as
		// a fallback behavior.
		if etcInstallPathKey == "" {
			if !(allCopyFileNamesUnchanged && android.InList(relDestDirFromInstallDirBase, []string{"", "."})) {
				moduleFactory = etc.PrebuiltAnyFactory
			}
		}

		if allCopyFileNamesUnchanged {
			// Specify relative_install_path if it is not installed in the base directory of the module.
			// In case of prebuilt_{root,any} this is equivalent to the root of the partition.
			if !android.InList(relDestDirFromInstallDirBase, []string{"", "."}) {
				propsList = append(propsList, &prebuiltSubdirProperties{
					Relative_install_path: proptools.StringPtr(relDestDirFromInstallDirBase),
				})
			}
		} else {
			dsts := proptools.NewConfigurable[[]string](nil, nil)
			for _, installBaseFile := range installBaseFiles {
				dsts.AppendSimpleValue([]string{filepath.Join(relDestDirFromInstallDirBase, installBaseFile)})
			}
			propsList = append(propsList, &etc.PrebuiltDstsProperties{
				Dsts: dsts,
			})
		}

		ctx.CreateModuleInDirectory(moduleFactory, srcDir, propsList...)
		moduleNames = append(moduleNames, moduleName)
	}

	return moduleNames
}

func createPrebuiltEtcModulesForPartition(ctx android.LoadHookContext, partition, srcDir string, destDirFilesMap map[string][]srcBaseFileInstallBaseFileTuple) (ret []string) {
	for _, destDir := range android.SortedKeys(destDirFilesMap) {
		ret = append(ret, createPrebuiltEtcModulesInDirectory(ctx, partition, srcDir, destDir, destDirFilesMap[destDir])...)
	}
	return ret
}

// Creates prebuilt_* modules based on the install paths and returns the list of generated
// module names
func createPrebuiltEtcModules(ctx android.LoadHookContext) (ret []string) {
	groupedSources := processProductCopyFiles(ctx)
	for _, srcDir := range android.SortedKeys(groupedSources) {
		groupedSource := groupedSources[srcDir]
		ret = append(ret, createPrebuiltEtcModulesForPartition(ctx, "system", srcDir, groupedSource.system)...)
		ret = append(ret, createPrebuiltEtcModulesForPartition(ctx, "system_ext", srcDir, groupedSource.system_ext)...)
		ret = append(ret, createPrebuiltEtcModulesForPartition(ctx, "product", srcDir, groupedSource.product)...)
		ret = append(ret, createPrebuiltEtcModulesForPartition(ctx, "vendor", srcDir, groupedSource.vendor)...)
		ret = append(ret, createPrebuiltEtcModulesForPartition(ctx, "recovery", srcDir, groupedSource.recovery)...)
		ret = append(ret, createPrebuiltEtcModulesForPartition(ctx, "vendor_dlkm", srcDir, groupedSource.vendor_dlkm)...)
		ret = append(ret, createPrebuiltEtcModulesForPartition(ctx, "vendor_ramdisk", srcDir, groupedSource.vendor_ramdisk)...)
	}

	return ret
}

func createAvbpubkeyModule(ctx android.LoadHookContext) bool {
	partitionVars := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse
	if !(partitionVars.BuildingSystemOtherImage && partitionVars.BoardAvbEnable) {
		// https://cs.android.com/android/platform/superproject/+/android-latest-release:build/make/core/Makefile;l=835;drc=8ab87941b1b9a3fc990cb1986e6245cf0af10a70
		return false
	}

	avbKeyPath := partitionVars.BoardAvbKeyPath
	if avbKeyPath == "" {
		// Use default
		// https://cs.android.com/android/_/android/platform/build/+/045a3d6a3e359633a14853a5a5e1e4f2a11cbdae:core/Makefile;l=4548;bpv=1;bpt=0;drc=a951ebf0198006f7fd38073a05c442d0eb92f97b
		avbKeyPath = "external/avb/test/data/testkey_rsa4096.pem"
	}
	ctx.CreateModuleInDirectory(
		etc.AvbpubkeyModuleFactory,
		".",
		&struct {
			Name             *string
			Product_specific *bool
			Private_key      *string
			No_full_install  *bool
			Visibility       []string
			Licenses         []string
		}{
			Name:             proptools.StringPtr("system_other_avbpubkey"),
			Product_specific: proptools.BoolPtr(true),
			Private_key:      proptools.StringPtr(avbKeyPath),
			No_full_install:  proptools.BoolPtr(true),
			Visibility:       []string{"//visibility:public"},
			Licenses:         []string{"Android-Apache-2.0"},
		},
	)

	return true
}

func getPrebuiltKernelPath(ctx android.LoadHookContext) string {
	processedProductCopyFilesMap := uniqueExistingProductCopyFileMap(ctx)
	for _, srcPath := range android.SortedKeys(processedProductCopyFilesMap) {
		destPaths := processedProductCopyFilesMap[srcPath]
		for _, destPath := range destPaths {
			if destPath == "kernel" {
				return srcPath
			}
		}
	}
	return ""
}
