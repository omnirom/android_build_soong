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
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"android/soong/android"
	"android/soong/filesystem"
	"android/soong/kernel"

	"github.com/google/blueprint"
	"github.com/google/blueprint/parser"
	"github.com/google/blueprint/proptools"
)

var pctx = android.NewPackageContext("android/soong/fsgen")

func init() {
	registerBuildComponents(android.InitRegistrationContext)
}

func registerBuildComponents(ctx android.RegistrationContext) {
	ctx.RegisterModuleType("soong_filesystem_creator", filesystemCreatorFactory)
	ctx.PreDepsMutators(RegisterCollectFileSystemDepsMutators)
}

type generatedPartitionData struct {
	partitionType string
	moduleName    string
	// supported is true if the module was created successfully, false if there was some problem
	// and the module couldn't be created.
	supported   bool
	handwritten bool
}

type allGeneratedPartitionData []generatedPartitionData

func (d allGeneratedPartitionData) moduleNames() []string {
	var result []string
	for _, data := range d {
		if data.supported {
			result = append(result, data.moduleName)
		}
	}
	return result
}

func (d allGeneratedPartitionData) types() []string {
	var result []string
	for _, data := range d {
		if data.supported {
			result = append(result, data.partitionType)
		}
	}
	return result
}

func (d allGeneratedPartitionData) unsupportedTypes() []string {
	var result []string
	for _, data := range d {
		if !data.supported {
			result = append(result, data.partitionType)
		}
	}
	return result
}

func (d allGeneratedPartitionData) names() []string {
	var result []string
	for _, data := range d {
		if data.supported {
			result = append(result, data.moduleName)
		}
	}
	return result
}

func (d allGeneratedPartitionData) nameForType(ty string) string {
	for _, data := range d {
		if data.supported && data.partitionType == ty {
			return data.moduleName
		}
	}
	return ""
}

func (d allGeneratedPartitionData) typeForName(name string) string {
	for _, data := range d {
		if data.supported && data.moduleName == name {
			return data.partitionType
		}
	}
	return ""
}

func (d allGeneratedPartitionData) isHandwritten(name string) bool {
	for _, data := range d {
		if data.supported && data.moduleName == name {
			return data.handwritten
		}
	}
	return false
}

type filesystemCreatorProps struct {
	Unsupported_partition_types []string `blueprint:"mutated"`

	Vbmeta_module_names    []string `blueprint:"mutated"`
	Vbmeta_partition_names []string `blueprint:"mutated"`

	Boot_image        string `blueprint:"mutated" android:"path_device_first"`
	Vendor_boot_image string `blueprint:"mutated" android:"path_device_first"`
	Init_boot_image   string `blueprint:"mutated" android:"path_device_first"`
	Super_image       string `blueprint:"mutated" android:"path_device_first"`
}

type filesystemCreator struct {
	android.ModuleBase

	properties filesystemCreatorProps
}

func filesystemCreatorFactory() android.Module {
	module := &filesystemCreator{}

	android.InitAndroidArchModule(module, android.DeviceSupported, android.MultilibCommon)
	module.AddProperties(&module.properties)
	android.AddLoadHook(module, func(ctx android.LoadHookContext) {
		generatedPrebuiltEtcModuleNames := createPrebuiltEtcModules(ctx)
		avbpubkeyGenerated := createAvbpubkeyModule(ctx)
		createFsGenState(ctx, generatedPrebuiltEtcModuleNames, avbpubkeyGenerated)
		module.createAvbKeyFilegroups(ctx)
		module.createMiscFilegroups(ctx)
		module.createInternalModules(ctx)
	})

	return module
}

func generatedPartitions(ctx android.EarlyModuleContext) allGeneratedPartitionData {
	partitionVars := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse

	var result allGeneratedPartitionData
	addGenerated := func(ty string) {
		result = append(result, generatedPartitionData{
			partitionType: ty,
			moduleName:    generatedModuleNameForPartition(ctx.Config(), ty),
			supported:     true,
		})
	}

	if ctx.Config().UseSoongSystemImage() {
		if ctx.Config().SoongDefinedSystemImage() == "" {
			panic("PRODUCT_SOONG_DEFINED_SYSTEM_IMAGE must be set if USE_SOONG_DEFINED_SYSTEM_IMAGE is true")
		}
		result = append(result, generatedPartitionData{
			partitionType: "system",
			moduleName:    ctx.Config().SoongDefinedSystemImage(),
			supported:     true,
			handwritten:   true,
		})
	} else {
		addGenerated("system")
	}
	if ctx.DeviceConfig().SystemExtPath() == "system_ext" {
		addGenerated("system_ext")
	}
	if ctx.DeviceConfig().BuildingVendorImage() && ctx.DeviceConfig().VendorPath() == "vendor" {
		addGenerated("vendor")
	}
	if ctx.DeviceConfig().BuildingProductImage() && ctx.DeviceConfig().ProductPath() == "product" {
		addGenerated("product")
	}
	if ctx.DeviceConfig().BuildingOdmImage() && ctx.DeviceConfig().OdmPath() == "odm" {
		addGenerated("odm")
	}
	if ctx.DeviceConfig().BuildingUserdataImage() && ctx.DeviceConfig().UserdataPath() == "data" {
		addGenerated("userdata")
	}
	if partitionVars.BuildingSystemDlkmImage {
		addGenerated("system_dlkm")
	}
	if partitionVars.BuildingVendorDlkmImage {
		addGenerated("vendor_dlkm")
	}
	if partitionVars.BuildingOdmDlkmImage {
		addGenerated("odm_dlkm")
	}
	if partitionVars.BuildingRamdiskImage {
		addGenerated("ramdisk")
	}
	if buildingVendorBootImage(partitionVars) {
		addGenerated("vendor_ramdisk")
	}
	if ctx.DeviceConfig().BuildingRecoveryImage() && ctx.DeviceConfig().RecoveryPath() == "recovery" {
		addGenerated("recovery")
	}
	return result
}

func (f *filesystemCreator) createInternalModules(ctx android.LoadHookContext) {
	partitions := generatedPartitions(ctx)
	for i := range partitions {
		f.createPartition(ctx, partitions, &partitions[i])
	}
	// Create android_info.prop
	f.createAndroidInfo(ctx)

	partitionVars := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse
	dtbImg := createDtbImgFilegroup(ctx)

	if buildingBootImage(partitionVars) {
		if createBootImage(ctx, dtbImg) {
			f.properties.Boot_image = ":" + generatedModuleNameForPartition(ctx.Config(), "boot")
		} else {
			f.properties.Unsupported_partition_types = append(f.properties.Unsupported_partition_types, "boot")
		}
	}
	if buildingVendorBootImage(partitionVars) {
		if createVendorBootImage(ctx, dtbImg) {
			f.properties.Vendor_boot_image = ":" + generatedModuleNameForPartition(ctx.Config(), "vendor_boot")
		} else {
			f.properties.Unsupported_partition_types = append(f.properties.Unsupported_partition_types, "vendor_boot")
		}
	}
	if buildingInitBootImage(partitionVars) {
		if createInitBootImage(ctx) {
			f.properties.Init_boot_image = ":" + generatedModuleNameForPartition(ctx.Config(), "init_boot")
		} else {
			f.properties.Unsupported_partition_types = append(f.properties.Unsupported_partition_types, "init_boot")
		}
	}

	var systemOtherImageName string
	if buildingSystemOtherImage(partitionVars) {
		systemModule := partitions.nameForType("system")
		systemOtherImageName = generatedModuleNameForPartition(ctx.Config(), "system_other")
		ctx.CreateModule(
			filesystem.SystemOtherImageFactory,
			&filesystem.SystemOtherImageProperties{
				System_image:                    &systemModule,
				Preinstall_dexpreopt_files_from: partitions.moduleNames(),
			},
			&struct {
				Name *string
			}{
				Name: proptools.StringPtr(systemOtherImageName),
			},
		)
	}

	for _, x := range f.createVbmetaPartitions(ctx, partitions) {
		f.properties.Vbmeta_module_names = append(f.properties.Vbmeta_module_names, x.moduleName)
		f.properties.Vbmeta_partition_names = append(f.properties.Vbmeta_partition_names, x.partitionName)
	}

	var superImageSubpartitions []string
	if buildingSuperImage(partitionVars) {
		superImageSubpartitions = createSuperImage(ctx, partitions, partitionVars, systemOtherImageName)
		f.properties.Super_image = ":" + generatedModuleNameForPartition(ctx.Config(), "super")
	}

	ctx.Config().Get(fsGenStateOnceKey).(*FsGenState).soongGeneratedPartitions = partitions
	f.createDeviceModule(ctx, partitions, f.properties.Vbmeta_module_names, superImageSubpartitions)
}

func generatedModuleName(cfg android.Config, suffix string) string {
	prefix := "soong"
	if cfg.HasDeviceProduct() {
		prefix = cfg.DeviceProduct()
	}
	return fmt.Sprintf("%s_generated_%s", prefix, suffix)
}

func generatedModuleNameForPartition(cfg android.Config, partitionType string) string {
	return generatedModuleName(cfg, fmt.Sprintf("%s_image", partitionType))
}

func buildingSystemOtherImage(partitionVars android.PartitionVariables) bool {
	// TODO: Recreate this logic from make instead of just depending on the final result variable:
	// https://cs.android.com/android/platform/superproject/main/+/main:build/make/core/board_config.mk;l=429;drc=15a0df840e7093f65518003ab80cf24a3d9e8e6a
	return partitionVars.BuildingSystemOtherImage
}

func (f *filesystemCreator) createBootloaderFilegroup(ctx android.LoadHookContext) (string, bool) {
	bootloaderPath := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse.PrebuiltBootloader
	if len(bootloaderPath) == 0 {
		return "", false
	}

	bootloaderFilegroupName := generatedModuleName(ctx.Config(), "bootloader")
	filegroupProps := &struct {
		Name       *string
		Srcs       []string
		Visibility []string
	}{
		Name:       proptools.StringPtr(bootloaderFilegroupName),
		Srcs:       []string{bootloaderPath},
		Visibility: []string{"//visibility:public"},
	}
	ctx.CreateModuleInDirectory(android.FileGroupFactory, ".", filegroupProps)
	return bootloaderFilegroupName, true
}

func (f *filesystemCreator) createReleaseToolsFilegroup(ctx android.LoadHookContext) (string, bool) {
	releaseToolsDir := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse.ReleaseToolsExtensionDir
	if releaseToolsDir == "" {
		return "", false
	}

	releaseToolsFilegroupName := generatedModuleName(ctx.Config(), "releasetools")
	filegroupProps := &struct {
		Name       *string
		Srcs       []string
		Visibility []string
	}{
		Name:       proptools.StringPtr(releaseToolsFilegroupName),
		Srcs:       []string{"releasetools.py"},
		Visibility: []string{"//visibility:public"},
	}
	ctx.CreateModuleInDirectory(android.FileGroupFactory, releaseToolsDir, filegroupProps)
	return releaseToolsFilegroupName, true
}

func (f *filesystemCreator) createFastbootInfoFilegroup(ctx android.LoadHookContext) (string, bool) {
	fastbootInfoFile := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse.BoardFastbootInfoFile
	if fastbootInfoFile == "" {
		return "", false
	}

	fastbootInfoFilegroupName := generatedModuleName(ctx.Config(), "fastboot")
	filegroupProps := &struct {
		Name       *string
		Srcs       []string
		Visibility []string
	}{
		Name:       proptools.StringPtr(fastbootInfoFilegroupName),
		Srcs:       []string{fastbootInfoFile},
		Visibility: []string{"//visibility:public"},
	}
	ctx.CreateModuleInDirectory(android.FileGroupFactory, ".", filegroupProps)
	return fastbootInfoFilegroupName, true
}

func (f *filesystemCreator) createDeviceModule(
	ctx android.LoadHookContext,
	partitions allGeneratedPartitionData,
	vbmetaPartitions []string,
	superImageSubPartitions []string,
) {
	baseProps := &struct {
		Name *string
	}{
		Name: proptools.StringPtr(generatedModuleName(ctx.Config(), "device")),
	}

	// Currently, only the system and system_ext partition module is created.
	partitionProps := &filesystem.PartitionNameProperties{}
	if f.properties.Super_image != "" {
		partitionProps.Super_partition_name = proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), "super"))
	}
	if modName := partitions.nameForType("system"); modName != "" && !android.InList("system", superImageSubPartitions) {
		partitionProps.System_partition_name = proptools.StringPtr(modName)
	}
	if modName := partitions.nameForType("system_ext"); modName != "" && !android.InList("system_ext", superImageSubPartitions) {
		partitionProps.System_ext_partition_name = proptools.StringPtr(modName)
	}
	if modName := partitions.nameForType("vendor"); modName != "" && !android.InList("vendor", superImageSubPartitions) {
		partitionProps.Vendor_partition_name = proptools.StringPtr(modName)
	}
	if modName := partitions.nameForType("product"); modName != "" && !android.InList("product", superImageSubPartitions) {
		partitionProps.Product_partition_name = proptools.StringPtr(modName)
	}
	if modName := partitions.nameForType("odm"); modName != "" && !android.InList("odm", superImageSubPartitions) {
		partitionProps.Odm_partition_name = proptools.StringPtr(modName)
	}
	if modName := partitions.nameForType("userdata"); modName != "" {
		partitionProps.Userdata_partition_name = proptools.StringPtr(modName)
	}
	if modName := partitions.nameForType("recovery"); modName != "" && !ctx.DeviceConfig().BoardMoveRecoveryResourcesToVendorBoot() {
		partitionProps.Recovery_partition_name = proptools.StringPtr(modName)
	}
	if modName := partitions.nameForType("system_dlkm"); modName != "" && !android.InList("system_dlkm", superImageSubPartitions) {
		partitionProps.System_dlkm_partition_name = proptools.StringPtr(modName)
	}
	if modName := partitions.nameForType("vendor_dlkm"); modName != "" && !android.InList("vendor_dlkm", superImageSubPartitions) {
		partitionProps.Vendor_dlkm_partition_name = proptools.StringPtr(modName)
	}
	if modName := partitions.nameForType("odm_dlkm"); modName != "" && !android.InList("odm_dlkm", superImageSubPartitions) {
		partitionProps.Odm_dlkm_partition_name = proptools.StringPtr(modName)
	}
	if f.properties.Boot_image != "" {
		partitionProps.Boot_partition_name = proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), "boot"))
	}
	if f.properties.Vendor_boot_image != "" {
		partitionProps.Vendor_boot_partition_name = proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), "vendor_boot"))
	}
	if f.properties.Init_boot_image != "" {
		partitionProps.Init_boot_partition_name = proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), "init_boot"))
	}
	partitionProps.Vbmeta_partitions = vbmetaPartitions

	deviceProps := &filesystem.DeviceProperties{
		Main_device:                   proptools.BoolPtr(true),
		Ab_ota_updater:                proptools.BoolPtr(ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse.AbOtaUpdater),
		Ab_ota_partitions:             ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse.AbOtaPartitions,
		Ab_ota_postinstall_config:     ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse.AbOtaPostInstallConfig,
		Ramdisk_node_list:             proptools.StringPtr(":ramdisk_node_list"),
		Android_info:                  proptools.StringPtr(":" + generatedModuleName(ctx.Config(), "android_info.prop{.txt}")),
		Kernel_version:                ctx.Config().ProductVariables().BoardKernelVersion,
		Partial_ota_update_partitions: ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse.BoardPartialOtaUpdatePartitionsList,
		Flash_block_size:              proptools.StringPtr(ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse.BoardFlashBlockSize),
		Bootloader_in_update_package:  proptools.BoolPtr(ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse.BootloaderInUpdatePackage),
	}

	if bootloader, ok := f.createBootloaderFilegroup(ctx); ok {
		deviceProps.Bootloader = proptools.StringPtr(":" + bootloader)
	}
	if releaseTools, ok := f.createReleaseToolsFilegroup(ctx); ok {
		deviceProps.Releasetools_extension = proptools.StringPtr(":" + releaseTools)
	}
	if fastbootInfo, ok := f.createFastbootInfoFilegroup(ctx); ok {
		deviceProps.FastbootInfo = proptools.StringPtr(":" + fastbootInfo)
	}

	ctx.CreateModule(filesystem.AndroidDeviceFactory, baseProps, partitionProps, deviceProps)
}

func partitionSpecificFsProps(ctx android.EarlyModuleContext, partitions allGeneratedPartitionData, fsProps *filesystem.FilesystemProperties, partitionType string) {
	partitionVars := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse
	switch partitionType {
	case "system":
		fsProps.Build_logtags = proptools.BoolPtr(true)
		// https://source.corp.google.com/h/googleplex-android/platform/build//639d79f5012a6542ab1f733b0697db45761ab0f3:core/packaging/flags.mk;l=21;drc=5ba8a8b77507f93aa48cc61c5ba3f31a4d0cbf37;bpv=1;bpt=0
		fsProps.Gen_aconfig_flags_pb = proptools.BoolPtr(true)
		// Identical to that of the aosp_shared_system_image
		if partitionVars.ProductFsverityGenerateMetadata {
			fsProps.Fsverity.Inputs = proptools.NewSimpleConfigurable([]string{
				"etc/boot-image.prof",
				"etc/dirty-image-objects",
				"etc/preloaded-classes",
				"etc/classpaths/*.pb",
				"framework/*",
				"framework/*/*",     // framework/{arch}
				"framework/oat/*/*", // framework/oat/{arch}
			})
			fsProps.Fsverity.Libs = proptools.NewSimpleConfigurable([]string{":framework-res{.export-package.apk}"})
		}
		fsProps.Symlinks = commonSymlinksFromRoot
		fsProps.Symlinks = append(fsProps.Symlinks,
			[]filesystem.SymlinkDefinition{
				{
					Target: proptools.StringPtr("/data/cache"),
					Name:   proptools.StringPtr("cache"),
				},
				{
					Target: proptools.StringPtr("/storage/self/primary"),
					Name:   proptools.StringPtr("sdcard"),
				},
				{
					Target: proptools.StringPtr("/system_dlkm/lib/modules"),
					Name:   proptools.StringPtr("system/lib/modules"),
				},
				{
					Target: proptools.StringPtr("/product"),
					Name:   proptools.StringPtr("system/product"),
				},
				{
					Target: proptools.StringPtr("/system_ext"),
					Name:   proptools.StringPtr("system/system_ext"),
				},
				{
					Target: proptools.StringPtr("/vendor"),
					Name:   proptools.StringPtr("system/vendor"),
				},
			}...,
		)
		fsProps.Base_dir = proptools.StringPtr("system")
		fsProps.Dirs = proptools.NewSimpleConfigurable(commonPartitionDirs)
		fsProps.Security_patch = proptools.StringPtr(ctx.Config().PlatformSecurityPatch())
		fsProps.Stem = proptools.StringPtr("system.img")
	case "system_ext":
		if partitionVars.ProductFsverityGenerateMetadata {
			fsProps.Fsverity.Inputs = proptools.NewSimpleConfigurable([]string{
				"framework/*",
				"framework/*/*",     // framework/{arch}
				"framework/oat/*/*", // framework/oat/{arch}
			})
			fsProps.Fsverity.Libs = proptools.NewSimpleConfigurable([]string{":framework-res{.export-package.apk}"})
		}
		fsProps.Security_patch = proptools.StringPtr(ctx.Config().PlatformSecurityPatch())
		fsProps.Stem = proptools.StringPtr("system_ext.img")
		fsProps.Gen_aconfig_flags_pb = proptools.BoolPtr(true)
	case "product":
		fsProps.Gen_aconfig_flags_pb = proptools.BoolPtr(true)
		fsProps.Android_filesystem_deps.System = proptools.StringPtr(partitions.nameForType("system"))
		if systemExtName := partitions.nameForType("system_ext"); systemExtName != "" {
			fsProps.Android_filesystem_deps.System_ext = proptools.StringPtr(systemExtName)
		}
		fsProps.Security_patch = proptools.StringPtr(ctx.Config().PlatformSecurityPatch())
		fsProps.Stem = proptools.StringPtr("product.img")
	case "vendor":
		fsProps.Gen_aconfig_flags_pb = proptools.BoolPtr(true)
		fsProps.Symlinks = []filesystem.SymlinkDefinition{
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/odm"),
				Name:   proptools.StringPtr("odm"),
			},
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/vendor_dlkm/lib/modules"),
				Name:   proptools.StringPtr("lib/modules"),
			},
		}
		fsProps.Android_filesystem_deps.System = proptools.StringPtr(partitions.nameForType("system"))
		if systemExtName := partitions.nameForType("system_ext"); systemExtName != "" {
			fsProps.Android_filesystem_deps.System_ext = proptools.StringPtr(systemExtName)
		}
		fsProps.Security_patch = proptools.StringPtr(partitionVars.VendorSecurityPatch)
		fsProps.Stem = proptools.StringPtr("vendor.img")
	case "odm":
		fsProps.Symlinks = []filesystem.SymlinkDefinition{
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/odm_dlkm/lib/modules"),
				Name:   proptools.StringPtr("lib/modules"),
			},
		}
		fsProps.Security_patch = proptools.StringPtr(partitionVars.OdmSecurityPatch)
		fsProps.Stem = proptools.StringPtr("odm.img")
	case "userdata":
		fsProps.Stem = proptools.StringPtr("userdata.img")
		if vars, ok := partitionVars.PartitionQualifiedVariables["userdata"]; ok {
			parsed, err := strconv.ParseInt(vars.BoardPartitionSize, 10, 64)
			if err != nil {
				panic(fmt.Sprintf("Partition size must be an int, got %s", vars.BoardPartitionSize))
			}
			fsProps.Partition_size = &parsed
			// Disable avb for userdata partition
			fsProps.Use_avb = nil
		}
		// https://cs.android.com/android/platform/superproject/main/+/main:build/make/core/Makefile;l=2265;drc=7f50a123045520f2c5e18e9eb4e83f92244a1459
		if s, err := strconv.ParseBool(partitionVars.ProductFsCasefold); err == nil {
			fsProps.Support_casefolding = proptools.BoolPtr(s)
		} else if len(partitionVars.ProductFsCasefold) > 0 {
			ctx.ModuleErrorf("Unrecognized PRODUCT_FS_CASEFOLD value %s", partitionVars.ProductFsCasefold)
		}
		if s, err := strconv.ParseBool(partitionVars.ProductQuotaProjid); err == nil {
			fsProps.Support_project_quota = proptools.BoolPtr(s)
		} else if len(partitionVars.ProductQuotaProjid) > 0 {
			ctx.ModuleErrorf("Unrecognized PRODUCT_QUOTA_PROJID value %s", partitionVars.ProductQuotaProjid)
		}
		if s, err := strconv.ParseBool(partitionVars.ProductFsCompression); err == nil {
			fsProps.Enable_compression = proptools.BoolPtr(s)
		} else if len(partitionVars.ProductFsCompression) > 0 {
			ctx.ModuleErrorf("Unrecognized PRODUCT_FS_COMPRESSION value %s", partitionVars.ProductFsCompression)
		}

	case "ramdisk":
		// Following the logic in https://cs.android.com/android/platform/superproject/main/+/c3c5063df32748a8806ce5da5dd0db158eab9ad9:build/make/core/Makefile;l=1307
		fsProps.Dirs = android.NewSimpleConfigurable([]string{
			"debug_ramdisk",
			"dev",
			"metadata",
			"mnt",
			"proc",
			"second_stage_resources",
			"sys",
		})
		if partitionVars.BoardUsesGenericKernelImage {
			fsProps.Dirs.AppendSimpleValue([]string{
				"first_stage_ramdisk/debug_ramdisk",
				"first_stage_ramdisk/dev",
				"first_stage_ramdisk/metadata",
				"first_stage_ramdisk/mnt",
				"first_stage_ramdisk/proc",
				"first_stage_ramdisk/second_stage_resources",
				"first_stage_ramdisk/sys",
			})
		}
		fsProps.Stem = proptools.StringPtr("ramdisk.img")
	case "recovery":
		dirs := append(commonPartitionDirs, []string{
			"sdcard",
		}...)

		dirsWithRoot := make([]string, len(dirs))
		for i, dir := range dirs {
			dirsWithRoot[i] = filepath.Join("root", dir)
		}

		fsProps.Dirs = proptools.NewSimpleConfigurable(dirsWithRoot)
		fsProps.Symlinks = symlinksWithNamePrefix(append(commonSymlinksFromRoot, filesystem.SymlinkDefinition{
			Target: proptools.StringPtr("prop.default"),
			Name:   proptools.StringPtr("default.prop"),
		}), "root")
		fsProps.Stem = proptools.StringPtr("recovery.img")
	case "system_dlkm":
		fsProps.Security_patch = proptools.StringPtr(partitionVars.SystemDlkmSecurityPatch)
		fsProps.Stem = proptools.StringPtr("system_dlkm.img")
	case "vendor_dlkm":
		fsProps.Security_patch = proptools.StringPtr(partitionVars.VendorDlkmSecurityPatch)
		fsProps.Stem = proptools.StringPtr("vendor_dlkm.img")
	case "odm_dlkm":
		fsProps.Security_patch = proptools.StringPtr(partitionVars.OdmDlkmSecurityPatch)
		fsProps.Stem = proptools.StringPtr("odm_dlkm.img")
	case "vendor_ramdisk":
		if recoveryName := partitions.nameForType("recovery"); recoveryName != "" {
			fsProps.Include_files_of = []string{recoveryName}
		}
		fsProps.Stem = proptools.StringPtr("vendor_ramdisk.img")
	}
}

var (
	dlkmPartitions = []string{
		"system_dlkm",
		"vendor_dlkm",
		"odm_dlkm",
	}
)

// Creates a soong module to build the given partition.
func (f *filesystemCreator) createPartition(ctx android.LoadHookContext, partitions allGeneratedPartitionData, partition *generatedPartitionData) {
	// Nextgen team's handwritten soong system image, don't need to create anything ourselves
	if partition.partitionType == "system" && ctx.Config().UseSoongSystemImage() {
		return
	}

	baseProps := generateBaseProps(proptools.StringPtr(partition.moduleName))

	fsProps, supported := generateFsProps(ctx, partitions, partition.partitionType)
	if !supported {
		partition.supported = false
		return
	}

	partitionType := partition.partitionType
	if partitionType == "vendor" || partitionType == "product" || partitionType == "system" {
		fsProps.Linker_config.Gen_linker_config = proptools.BoolPtr(true)
		if partitionType != "system" {
			fsProps.Linker_config.Linker_config_srcs = f.createLinkerConfigSourceFilegroups(ctx, partitionType)
		}
	}

	if android.InList(partitionType, append(dlkmPartitions, "vendor_ramdisk")) {
		f.createPrebuiltKernelModules(ctx, partitionType)
	}

	var module android.Module
	if partitionType == "system" {
		module = ctx.CreateModule(filesystem.SystemImageFactory, baseProps, fsProps)
	} else {
		// Explicitly set the partition.
		fsProps.Partition_type = proptools.StringPtr(partitionType)
		module = ctx.CreateModule(filesystem.FilesystemFactory, baseProps, fsProps)
	}
	module.HideFromMake()
	if partitionType == "vendor" {
		f.createVendorBuildProp(ctx)
	}
}

// Creates filegroups for the files specified in BOARD_(partition_)AVB_KEY_PATH
func (f *filesystemCreator) createAvbKeyFilegroups(ctx android.LoadHookContext) {
	partitionVars := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse
	var files []string

	if len(partitionVars.BoardAvbKeyPath) > 0 {
		files = append(files, partitionVars.BoardAvbKeyPath)
	}
	for _, partition := range android.SortedKeys(partitionVars.PartitionQualifiedVariables) {
		specificPartitionVars := partitionVars.PartitionQualifiedVariables[partition]
		if len(specificPartitionVars.BoardAvbKeyPath) > 0 {
			files = append(files, specificPartitionVars.BoardAvbKeyPath)
		}
	}

	fsGenState := ctx.Config().Get(fsGenStateOnceKey).(*FsGenState)
	for _, file := range files {
		if _, ok := fsGenState.avbKeyFilegroups[file]; ok {
			continue
		}
		if file == "external/avb/test/data/testkey_rsa4096.pem" {
			// There already exists a checked-in filegroup for this commonly-used key, just use that
			fsGenState.avbKeyFilegroups[file] = "avb_testkey_rsa4096"
			continue
		}
		dir := filepath.Dir(file)
		base := filepath.Base(file)
		name := fmt.Sprintf("avb_key_%x", strings.ReplaceAll(file, "/", "_"))
		ctx.CreateModuleInDirectory(
			android.FileGroupFactory,
			dir,
			&struct {
				Name       *string
				Srcs       []string
				Visibility []string
			}{
				Name:       proptools.StringPtr(name),
				Srcs:       []string{base},
				Visibility: []string{"//visibility:public"},
			},
		)
		fsGenState.avbKeyFilegroups[file] = name
	}
}

// Creates filegroups for miscellaneous other files
func (f *filesystemCreator) createMiscFilegroups(ctx android.LoadHookContext) {
	partitionVars := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse

	if partitionVars.BoardErofsCompressorHints != "" {
		dir := filepath.Dir(partitionVars.BoardErofsCompressorHints)
		base := filepath.Base(partitionVars.BoardErofsCompressorHints)
		ctx.CreateModuleInDirectory(
			android.FileGroupFactory,
			dir,
			&struct {
				Name       *string
				Srcs       []string
				Visibility []string
			}{
				Name:       proptools.StringPtr("soong_generated_board_erofs_compress_hints_filegroup"),
				Srcs:       []string{base},
				Visibility: []string{"//visibility:public"},
			},
		)
	}
}

// createPrebuiltKernelModules creates `prebuilt_kernel_modules`. These modules will be added to deps of the
// autogenerated *_dlkm filsystem modules. Each _dlkm partition should have a single prebuilt_kernel_modules dependency.
// This ensures that the depmod artifacts (modules.* installed in /lib/modules/) are generated with a complete view.
func (f *filesystemCreator) createPrebuiltKernelModules(ctx android.LoadHookContext, partitionType string) {
	fsGenState := ctx.Config().Get(fsGenStateOnceKey).(*FsGenState)
	name := generatedModuleName(ctx.Config(), fmt.Sprintf("%s-kernel-modules", partitionType))
	props := &struct {
		Name                 *string
		Srcs                 []string
		System_deps          []string
		System_dlkm_specific *bool
		Vendor_dlkm_specific *bool
		Odm_dlkm_specific    *bool
		Vendor_ramdisk       *bool
		Load_by_default      *bool
		Blocklist_file       *string
		Options_file         *string
		Strip_debug_symbols  *bool
	}{
		Name:                proptools.StringPtr(name),
		Strip_debug_symbols: proptools.BoolPtr(false),
	}
	switch partitionType {
	case "system_dlkm":
		props.Srcs = android.ExistentPathsForSources(ctx, ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse.SystemKernelModules).Strings()
		props.System_dlkm_specific = proptools.BoolPtr(true)
		if len(ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse.SystemKernelLoadModules) == 0 {
			// Create empty modules.load file for system
			// https://source.corp.google.com/h/googleplex-android/platform/build/+/ef55daac9954896161b26db4f3ef1781b5a5694c:core/Makefile;l=695-700;drc=549fe2a5162548bd8b47867d35f907eb22332023;bpv=1;bpt=0
			props.Load_by_default = proptools.BoolPtr(false)
		}
		if blocklistFile := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse.SystemKernelBlocklistFile; blocklistFile != "" {
			props.Blocklist_file = proptools.StringPtr(blocklistFile)
		}
	case "vendor_dlkm":
		props.Srcs = android.ExistentPathsForSources(ctx, ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse.VendorKernelModules).Strings()
		if len(ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse.SystemKernelModules) > 0 {
			props.System_deps = []string{":" + generatedModuleName(ctx.Config(), "system_dlkm-kernel-modules") + "{.modules}"}
		}
		props.Vendor_dlkm_specific = proptools.BoolPtr(true)
		if blocklistFile := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse.VendorKernelBlocklistFile; blocklistFile != "" {
			props.Blocklist_file = proptools.StringPtr(blocklistFile)
		}
	case "odm_dlkm":
		props.Srcs = android.ExistentPathsForSources(ctx, ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse.OdmKernelModules).Strings()
		props.Odm_dlkm_specific = proptools.BoolPtr(true)
		if blocklistFile := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse.OdmKernelBlocklistFile; blocklistFile != "" {
			props.Blocklist_file = proptools.StringPtr(blocklistFile)
		}
	case "vendor_ramdisk":
		props.Srcs = android.ExistentPathsForSources(ctx, ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse.VendorRamdiskKernelModules).Strings()
		props.Vendor_ramdisk = proptools.BoolPtr(true)
		if blocklistFile := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse.VendorRamdiskKernelBlocklistFile; blocklistFile != "" {
			props.Blocklist_file = proptools.StringPtr(blocklistFile)
		}
		if optionsFile := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse.VendorRamdiskKernelOptionsFile; optionsFile != "" {
			props.Options_file = proptools.StringPtr(optionsFile)
		}

	default:
		ctx.ModuleErrorf("DLKM is not supported for %s\n", partitionType)
	}

	if len(props.Srcs) == 0 {
		return // do not generate `prebuilt_kernel_modules` if there are no sources
	}

	kernelModule := ctx.CreateModuleInDirectory(
		kernel.PrebuiltKernelModulesFactory,
		".", // create in root directory for now
		props,
	)
	kernelModule.HideFromMake()
	// Add to deps
	(*fsGenState.fsDeps[partitionType])[name] = defaultDepCandidateProps(ctx.Config())
}

// Create an android_info module. This will be used to create /vendor/build.prop
func (f *filesystemCreator) createAndroidInfo(ctx android.LoadHookContext) {
	// Create a android_info for vendor
	// The board info files might be in a directory outside the root soong namespace, so create
	// the module in "."
	partitionVars := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse
	androidInfoProps := &struct {
		Name                  *string
		Board_info_files      []string
		Bootloader_board_name *string
		Stem                  *string
	}{
		Name:             proptools.StringPtr(generatedModuleName(ctx.Config(), "android_info.prop")),
		Board_info_files: partitionVars.BoardInfoFiles,
		Stem:             proptools.StringPtr("android-info.txt"),
	}
	if len(androidInfoProps.Board_info_files) == 0 {
		androidInfoProps.Bootloader_board_name = proptools.StringPtr(partitionVars.BootLoaderBoardName)
	}
	androidInfoProp := ctx.CreateModuleInDirectory(
		android.AndroidInfoFactory,
		".",
		androidInfoProps,
	)
	androidInfoProp.HideFromMake()
}

func (f *filesystemCreator) createVendorBuildProp(ctx android.LoadHookContext) {
	vendorBuildProps := &struct {
		Name           *string
		Vendor         *bool
		Stem           *string
		Product_config *string
		Android_info   *string
		Licenses       []string
		Dist           android.Dist
	}{
		Name:           proptools.StringPtr(generatedModuleName(ctx.Config(), "vendor-build.prop")),
		Vendor:         proptools.BoolPtr(true),
		Stem:           proptools.StringPtr("build.prop"),
		Product_config: proptools.StringPtr(":product_config"),
		Android_info:   proptools.StringPtr(":" + generatedModuleName(ctx.Config(), "android_info.prop")),
		Dist: android.Dist{
			Targets: []string{"droidcore-unbundled"},
			Dest:    proptools.StringPtr("build.prop-vendor"),
		},
		Licenses: []string{"Android-Apache-2.0"},
	}
	vendorBuildProp := ctx.CreateModule(
		android.BuildPropFactory,
		vendorBuildProps,
	)
	// We don't want this to conflict with the make-built vendor build.prop, but unfortunately
	// calling HideFromMake() prevents disting files, even in soong-only mode. So only call
	// HideFromMake() on soong+make builds.
	if ctx.Config().KatiEnabled() {
		vendorBuildProp.HideFromMake()
	}
}

func createRecoveryBuildProp(ctx android.LoadHookContext) string {
	moduleName := generatedModuleName(ctx.Config(), "recovery-prop.default")

	var vendorBuildProp *string
	if ctx.DeviceConfig().BuildingVendorImage() && ctx.DeviceConfig().VendorPath() == "vendor" {
		vendorBuildProp = proptools.StringPtr(":" + generatedModuleName(ctx.Config(), "vendor-build.prop"))
	}

	recoveryBuildProps := &struct {
		Name                  *string
		System_build_prop     *string
		Vendor_build_prop     *string
		Odm_build_prop        *string
		Product_build_prop    *string
		System_ext_build_prop *string

		Recovery        *bool
		No_full_install *bool
		Visibility      []string
	}{
		Name:                  proptools.StringPtr(moduleName),
		System_build_prop:     proptools.StringPtr(":system-build.prop"),
		Vendor_build_prop:     vendorBuildProp,
		Odm_build_prop:        proptools.StringPtr(":odm-build.prop"),
		Product_build_prop:    proptools.StringPtr(":product-build.prop"),
		System_ext_build_prop: proptools.StringPtr(":system_ext-build.prop"),

		Recovery:        proptools.BoolPtr(true),
		No_full_install: proptools.BoolPtr(true),
		Visibility:      []string{"//visibility:public"},
	}

	ctx.CreateModule(android.RecoveryBuildPropModuleFactory, recoveryBuildProps)

	return moduleName
}

// createLinkerConfigSourceFilegroups creates filegroup modules to generate linker.config.pb for the following partitions
// 1. vendor: Using PRODUCT_VENDOR_LINKER_CONFIG_FRAGMENTS (space separated file list)
// 1. product: Using PRODUCT_PRODUCT_LINKER_CONFIG_FRAGMENTS (space separated file list)
// It creates a filegroup for each file in the fragment list
// The filegroup modules are then added to `linker_config_srcs` of the autogenerated vendor `android_filesystem`.
func (f *filesystemCreator) createLinkerConfigSourceFilegroups(ctx android.LoadHookContext, partitionType string) []string {
	ret := []string{}
	partitionVars := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse
	var linkerConfigSrcs []string
	if partitionType == "vendor" {
		linkerConfigSrcs = android.FirstUniqueStrings(partitionVars.VendorLinkerConfigSrcs)
	} else if partitionType == "product" {
		linkerConfigSrcs = android.FirstUniqueStrings(partitionVars.ProductLinkerConfigSrcs)
	} else {
		ctx.ModuleErrorf("linker.config.pb is only supported for vendor and product partitions. For system partition, use `android_system_image`")
	}

	if len(linkerConfigSrcs) > 0 {
		// Create a filegroup, and add `:<filegroup_name>` to ret.
		for index, linkerConfigSrc := range linkerConfigSrcs {
			dir := filepath.Dir(linkerConfigSrc)
			base := filepath.Base(linkerConfigSrc)
			fgName := generatedModuleName(ctx.Config(), fmt.Sprintf("%s-linker-config-src%s", partitionType, strconv.Itoa(index)))
			srcs := []string{base}
			fgProps := &struct {
				Name *string
				Srcs proptools.Configurable[[]string]
			}{
				Name: proptools.StringPtr(fgName),
				Srcs: proptools.NewSimpleConfigurable(srcs),
			}
			ctx.CreateModuleInDirectory(
				android.FileGroupFactory,
				dir,
				fgProps,
			)
			ret = append(ret, ":"+fgName)
		}
	}
	return ret
}

type filesystemBaseProperty struct {
	Name             *string
	Compile_multilib *string
	Visibility       []string
}

func generateBaseProps(namePtr *string) *filesystemBaseProperty {
	return &filesystemBaseProperty{
		Name:             namePtr,
		Compile_multilib: proptools.StringPtr("both"),
		// The vbmeta modules are currently in the root directory and depend on the partitions
		Visibility: []string{"//.", "//build/soong:__subpackages__"},
	}
}

func generateFsProps(ctx android.EarlyModuleContext, partitions allGeneratedPartitionData, partitionType string) (*filesystem.FilesystemProperties, bool) {
	fsProps := &filesystem.FilesystemProperties{}

	partitionVars := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse
	var avbInfo avbInfo
	var fsType string
	if strings.Contains(partitionType, "ramdisk") {
		fsType = "compressed_cpio"
	} else {
		specificPartitionVars := partitionVars.PartitionQualifiedVariables[partitionType]
		fsType = specificPartitionVars.BoardFileSystemType
		avbInfo = getAvbInfo(ctx.Config(), partitionType)
		if fsType == "" {
			fsType = "ext4" //default
		}
	}

	fsProps.Type = proptools.StringPtr(fsType)
	if filesystem.GetFsTypeFromString(ctx, *fsProps.Type).IsUnknown() {
		// Currently the android_filesystem module type only supports a handful of FS types like ext4, erofs
		return nil, false
	}

	if *fsProps.Type == "erofs" {
		if partitionVars.BoardErofsCompressor != "" {
			fsProps.Erofs.Compressor = proptools.StringPtr(partitionVars.BoardErofsCompressor)
		}
		if partitionVars.BoardErofsCompressorHints != "" {
			fsProps.Erofs.Compress_hints = proptools.StringPtr(":soong_generated_board_erofs_compress_hints_filegroup")
		}
	}

	// Don't build this module on checkbuilds, the soong-built partitions are still in-progress
	// and sometimes don't build.
	fsProps.Unchecked_module = proptools.BoolPtr(true)

	// BOARD_AVB_ENABLE
	fsProps.Use_avb = avbInfo.avbEnable
	// BOARD_AVB_KEY_PATH
	fsProps.Avb_private_key = avbInfo.avbkeyFilegroup
	// BOARD_AVB_ALGORITHM
	fsProps.Avb_algorithm = avbInfo.avbAlgorithm
	// BOARD_AVB_SYSTEM_ROLLBACK_INDEX
	fsProps.Rollback_index = avbInfo.avbRollbackIndex
	// BOARD_AVB_SYSTEM_ROLLBACK_INDEX_LOCATION
	fsProps.Rollback_index_location = avbInfo.avbRollbackIndexLocation
	fsProps.Avb_hash_algorithm = avbInfo.avbHashAlgorithm

	fsProps.Partition_name = proptools.StringPtr(partitionType)

	switch partitionType {
	// The partitions that support file_contexts came from here:
	// https://cs.android.com/android/platform/superproject/main/+/main:build/make/core/Makefile;l=2270;drc=ad7cfb56010cb22c3aa0e70cf71c804352553526
	case "system", "userdata", "cache", "vendor", "product", "system_ext", "odm", "vendor_dlkm", "odm_dlkm", "system_dlkm", "oem":
		fsProps.Precompiled_file_contexts = proptools.StringPtr(":file_contexts_bin_gen")
	}

	fsProps.Is_auto_generated = proptools.BoolPtr(true)
	if partitionType != "system" {
		mountPoint := proptools.StringPtr(partitionType)
		// https://cs.android.com/android/platform/superproject/main/+/main:build/make/tools/releasetools/build_image.py;l=1012;drc=3f576a753594bad3fc838ccb8b1b72f7efac1d50
		if partitionType == "userdata" {
			mountPoint = proptools.StringPtr("data")
		}
		fsProps.Mount_point = mountPoint

	}

	partitionSpecificFsProps(ctx, partitions, fsProps, partitionType)

	return fsProps, true
}

type avbInfo struct {
	avbEnable                *bool
	avbKeyPath               *string
	avbkeyFilegroup          *string
	avbAlgorithm             *string
	avbRollbackIndex         *int64
	avbRollbackIndexLocation *int64
	avbMode                  *string
	avbHashAlgorithm         *string
}

func getAvbInfo(config android.Config, partitionType string) avbInfo {
	partitionVars := config.ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse
	specificPartitionVars := partitionVars.PartitionQualifiedVariables[partitionType]
	var result avbInfo
	boardAvbEnable := partitionVars.BoardAvbEnable
	if boardAvbEnable {
		result.avbEnable = proptools.BoolPtr(true)
		// There are "global" and "specific" copies of a lot of these variables. Sometimes they
		// choose the specific and then fall back to the global one if it's not set, other times
		// the global one actually only applies to the vbmeta partition.
		if partitionType == "vbmeta" {
			if partitionVars.BoardAvbKeyPath != "" {
				result.avbKeyPath = proptools.StringPtr(partitionVars.BoardAvbKeyPath)
			}
			if partitionVars.BoardAvbRollbackIndex != "" {
				parsed, err := strconv.ParseInt(partitionVars.BoardAvbRollbackIndex, 10, 64)
				if err != nil {
					panic(fmt.Sprintf("Rollback index must be an int, got %s", partitionVars.BoardAvbRollbackIndex))
				}
				result.avbRollbackIndex = &parsed
			}
		}
		if specificPartitionVars.BoardAvbKeyPath != "" {
			result.avbKeyPath = proptools.StringPtr(specificPartitionVars.BoardAvbKeyPath)
		}
		if specificPartitionVars.BoardAvbAlgorithm != "" {
			result.avbAlgorithm = proptools.StringPtr(specificPartitionVars.BoardAvbAlgorithm)
		} else if partitionVars.BoardAvbAlgorithm != "" {
			result.avbAlgorithm = proptools.StringPtr(partitionVars.BoardAvbAlgorithm)
		}
		if specificPartitionVars.BoardAvbRollbackIndex != "" {
			parsed, err := strconv.ParseInt(specificPartitionVars.BoardAvbRollbackIndex, 10, 64)
			if err != nil {
				panic(fmt.Sprintf("Rollback index must be an int, got %s", specificPartitionVars.BoardAvbRollbackIndex))
			}
			result.avbRollbackIndex = &parsed
		}
		if specificPartitionVars.BoardAvbRollbackIndexLocation != "" {
			parsed, err := strconv.ParseInt(specificPartitionVars.BoardAvbRollbackIndexLocation, 10, 64)
			if err != nil {
				panic(fmt.Sprintf("Rollback index location must be an int, got %s", specificPartitionVars.BoardAvbRollbackIndexLocation))
			}
			result.avbRollbackIndexLocation = &parsed
		}

		// Make allows you to pass arbitrary arguments to avbtool via this variable, but in practice
		// it's only used for --hash_algorithm. The soong module has a dedicated property for the
		// hashtree algorithm, and doesn't allow custom arguments, so just extract the hashtree
		// algorithm out of the arbitrary arguments.
		addHashtreeFooterArgs := strings.Split(specificPartitionVars.BoardAvbAddHashtreeFooterArgs, " ")
		if i := slices.Index(addHashtreeFooterArgs, "--hash_algorithm"); i >= 0 {
			result.avbHashAlgorithm = &addHashtreeFooterArgs[i+1]
		}

		result.avbMode = proptools.StringPtr("make_legacy")
	}
	if result.avbKeyPath != nil {
		fsGenState := config.Get(fsGenStateOnceKey).(*FsGenState)
		filegroup := fsGenState.avbKeyFilegroups[*result.avbKeyPath]
		result.avbkeyFilegroup = proptools.StringPtr(":" + filegroup)
	}
	return result
}

func (f *filesystemCreator) createFileListDiffTest(ctx android.ModuleContext, partitionType string, partitionModuleName string) android.Path {
	partitionImage := ctx.GetDirectDepWithTag(partitionModuleName, generatedFilesystemDepTag)
	filesystemInfo, ok := android.OtherModuleProvider(ctx, partitionImage, filesystem.FilesystemProvider)
	if !ok {
		ctx.ModuleErrorf("Expected module %s to provide FileysystemInfo", partitionModuleName)
		return nil
	}
	makeFileList := android.PathForArbitraryOutput(ctx, fmt.Sprintf("target/product/%s/obj/PACKAGING/%s_intermediates/file_list.txt", ctx.Config().DeviceName(), partitionType))
	diffTestResultFile := android.PathForModuleOut(ctx, fmt.Sprintf("diff_test_%s.txt", partitionModuleName))

	builder := android.NewRuleBuilder(pctx, ctx)
	builder.Command().BuiltTool("file_list_diff").
		Input(makeFileList).
		Input(filesystemInfo.FileListFile).
		Text(partitionModuleName)
	builder.Command().Text("touch").Output(diffTestResultFile)
	builder.Build(partitionModuleName+" diff test", partitionModuleName+" diff test")
	return diffTestResultFile
}

func createFailingCommand(ctx android.ModuleContext, message string) android.Path {
	hasher := sha256.New()
	hasher.Write([]byte(message))
	filename := fmt.Sprintf("failing_command_%x.txt", hasher.Sum(nil))
	file := android.PathForModuleOut(ctx, filename)
	builder := android.NewRuleBuilder(pctx, ctx)
	builder.Command().Textf("echo %s", proptools.NinjaAndShellEscape(message))
	builder.Command().Text("exit 1 #").Output(file)
	builder.Build("failing command "+filename, "failing command "+filename)
	return file
}

func createVbmetaDiff(ctx android.ModuleContext, vbmetaModuleName string, vbmetaPartitionName string) android.Path {
	vbmetaModule := ctx.GetDirectDepWithTag(vbmetaModuleName, generatedVbmetaPartitionDepTag)
	outputFilesProvider, ok := android.OtherModuleProvider(ctx, vbmetaModule, android.OutputFilesProvider)
	if !ok {
		ctx.ModuleErrorf("Expected module %s to provide OutputFiles", vbmetaModule)
	}
	if len(outputFilesProvider.DefaultOutputFiles) != 1 {
		ctx.ModuleErrorf("Expected 1 output file from module %s", vbmetaModule)
	}
	soongVbMetaFile := outputFilesProvider.DefaultOutputFiles[0]
	makeVbmetaFile := android.PathForArbitraryOutput(ctx, fmt.Sprintf("target/product/%s/%s.img", ctx.Config().DeviceName(), vbmetaPartitionName))

	diffTestResultFile := android.PathForModuleOut(ctx, fmt.Sprintf("diff_test_%s.txt", vbmetaModuleName))
	createDiffTest(ctx, diffTestResultFile, soongVbMetaFile, makeVbmetaFile)
	return diffTestResultFile
}

func createDiffTest(ctx android.ModuleContext, diffTestResultFile android.WritablePath, file1 android.Path, file2 android.Path) {
	builder := android.NewRuleBuilder(pctx, ctx)
	builder.Command().Text("diff").
		Input(file1).
		Input(file2)
	builder.Command().Text("touch").Output(diffTestResultFile)
	builder.Build("diff test "+diffTestResultFile.String(), "diff test")
}

type imageDepTagType struct {
	blueprint.BaseDependencyTag
}

var generatedFilesystemDepTag imageDepTagType
var generatedVbmetaPartitionDepTag imageDepTagType

func (f *filesystemCreator) DepsMutator(ctx android.BottomUpMutatorContext) {
	for _, name := range ctx.Config().Get(fsGenStateOnceKey).(*FsGenState).soongGeneratedPartitions.names() {
		ctx.AddDependency(ctx.Module(), generatedFilesystemDepTag, name)
	}
	for _, vbmetaModule := range f.properties.Vbmeta_module_names {
		ctx.AddDependency(ctx.Module(), generatedVbmetaPartitionDepTag, vbmetaModule)
	}
}

func (f *filesystemCreator) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	if ctx.ModuleDir() != "build/soong/fsgen" {
		ctx.ModuleErrorf("There can only be one soong_filesystem_creator in build/soong/fsgen")
	}
	f.HideFromMake()

	partitions := ctx.Config().Get(fsGenStateOnceKey).(*FsGenState).soongGeneratedPartitions

	var content strings.Builder
	generatedBp := android.PathForModuleOut(ctx, "soong_generated_product_config.bp")
	for _, partition := range partitions.types() {
		content.WriteString(generateBpContent(ctx, partition))
		content.WriteString("\n")
	}
	android.WriteFileRule(ctx, generatedBp, content.String())

	ctx.Phony("product_config_to_bp", generatedBp)

	if !ctx.Config().KatiEnabled() {
		// Cannot diff since the kati packaging rules will not be created.
		return
	}
	var diffTestFiles []android.Path
	for _, partitionType := range partitions.types() {
		diffTestFile := f.createFileListDiffTest(ctx, partitionType, partitions.nameForType(partitionType))
		diffTestFiles = append(diffTestFiles, diffTestFile)
		ctx.Phony(fmt.Sprintf("soong_generated_%s_filesystem_test", partitionType), diffTestFile)
	}
	for _, partitionType := range slices.Concat(partitions.unsupportedTypes(), f.properties.Unsupported_partition_types) {
		diffTestFile := createFailingCommand(ctx, fmt.Sprintf("Couldn't build %s partition", partitionType))
		diffTestFiles = append(diffTestFiles, diffTestFile)
		ctx.Phony(fmt.Sprintf("soong_generated_%s_filesystem_test", partitionType), diffTestFile)
	}
	for i, vbmetaModule := range f.properties.Vbmeta_module_names {
		diffTestFile := createVbmetaDiff(ctx, vbmetaModule, f.properties.Vbmeta_partition_names[i])
		diffTestFiles = append(diffTestFiles, diffTestFile)
		ctx.Phony(fmt.Sprintf("soong_generated_%s_filesystem_test", f.properties.Vbmeta_partition_names[i]), diffTestFile)
	}
	if f.properties.Boot_image != "" {
		diffTestFile := android.PathForModuleOut(ctx, "boot_diff_test.txt")
		soongBootImg := android.PathForModuleSrc(ctx, f.properties.Boot_image)
		makeBootImage := android.PathForArbitraryOutput(ctx, fmt.Sprintf("target/product/%s/boot.img", ctx.Config().DeviceName()))
		createDiffTest(ctx, diffTestFile, soongBootImg, makeBootImage)
		diffTestFiles = append(diffTestFiles, diffTestFile)
		ctx.Phony("soong_generated_boot_filesystem_test", diffTestFile)
	}
	if f.properties.Vendor_boot_image != "" {
		diffTestFile := android.PathForModuleOut(ctx, "vendor_boot_diff_test.txt")
		soongBootImg := android.PathForModuleSrc(ctx, f.properties.Vendor_boot_image)
		makeBootImage := android.PathForArbitraryOutput(ctx, fmt.Sprintf("target/product/%s/vendor_boot.img", ctx.Config().DeviceName()))
		createDiffTest(ctx, diffTestFile, soongBootImg, makeBootImage)
		diffTestFiles = append(diffTestFiles, diffTestFile)
		ctx.Phony("soong_generated_vendor_boot_filesystem_test", diffTestFile)
	}
	if f.properties.Init_boot_image != "" {
		diffTestFile := android.PathForModuleOut(ctx, "init_boot_diff_test.txt")
		soongBootImg := android.PathForModuleSrc(ctx, f.properties.Init_boot_image)
		makeBootImage := android.PathForArbitraryOutput(ctx, fmt.Sprintf("target/product/%s/init_boot.img", ctx.Config().DeviceName()))
		createDiffTest(ctx, diffTestFile, soongBootImg, makeBootImage)
		diffTestFiles = append(diffTestFiles, diffTestFile)
		ctx.Phony("soong_generated_init_boot_filesystem_test", diffTestFile)
	}
	if f.properties.Super_image != "" {
		diffTestFile := android.PathForModuleOut(ctx, "super_diff_test.txt")
		soongSuperImg := android.PathForModuleSrc(ctx, f.properties.Super_image)
		makeSuperImage := android.PathForArbitraryOutput(ctx, fmt.Sprintf("target/product/%s/super.img", ctx.Config().DeviceName()))
		createDiffTest(ctx, diffTestFile, soongSuperImg, makeSuperImage)
		diffTestFiles = append(diffTestFiles, diffTestFile)
		ctx.Phony("soong_generated_super_filesystem_test", diffTestFile)
	}
	ctx.Phony("soong_generated_filesystem_tests", diffTestFiles...)
}

func generateBpContent(ctx android.EarlyModuleContext, partitionType string) string {
	fsGenState := ctx.Config().Get(fsGenStateOnceKey).(*FsGenState)
	fsProps, fsTypeSupported := generateFsProps(ctx, fsGenState.soongGeneratedPartitions, partitionType)
	if !fsTypeSupported {
		return ""
	}

	baseProps := generateBaseProps(proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), partitionType)))
	deps := fsGenState.fsDeps[partitionType]
	highPriorityDeps := fsGenState.generatedPrebuiltEtcModuleNames
	depProps := generateDepStruct(*deps, highPriorityDeps)

	result, err := proptools.RepackProperties([]interface{}{baseProps, fsProps, depProps})
	if err != nil {
		ctx.ModuleErrorf("%s", err.Error())
		return ""
	}

	moduleType := "android_filesystem"
	if partitionType == "system" {
		moduleType = "android_system_image"
	}

	file := &parser.File{
		Defs: []parser.Definition{
			&parser.Module{
				Type: moduleType,
				Map:  *result,
			},
		},
	}
	bytes, err := parser.Print(file)
	if err != nil {
		ctx.ModuleErrorf(err.Error())
	}
	return strings.TrimSpace(string(bytes))
}
