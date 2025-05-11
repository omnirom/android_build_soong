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

type filesystemCreatorProps struct {
	Generated_partition_types   []string `blueprint:"mutated"`
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

func generatedPartitions(ctx android.LoadHookContext) []string {
	partitionVars := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse
	generatedPartitions := []string{"system"}
	if ctx.DeviceConfig().SystemExtPath() == "system_ext" {
		generatedPartitions = append(generatedPartitions, "system_ext")
	}
	if ctx.DeviceConfig().BuildingVendorImage() && ctx.DeviceConfig().VendorPath() == "vendor" {
		generatedPartitions = append(generatedPartitions, "vendor")
	}
	if ctx.DeviceConfig().BuildingProductImage() && ctx.DeviceConfig().ProductPath() == "product" {
		generatedPartitions = append(generatedPartitions, "product")
	}
	if ctx.DeviceConfig().BuildingOdmImage() && ctx.DeviceConfig().OdmPath() == "odm" {
		generatedPartitions = append(generatedPartitions, "odm")
	}
	if ctx.DeviceConfig().BuildingUserdataImage() && ctx.DeviceConfig().UserdataPath() == "data" {
		generatedPartitions = append(generatedPartitions, "userdata")
	}
	if partitionVars.BuildingSystemDlkmImage {
		generatedPartitions = append(generatedPartitions, "system_dlkm")
	}
	if partitionVars.BuildingVendorDlkmImage {
		generatedPartitions = append(generatedPartitions, "vendor_dlkm")
	}
	if partitionVars.BuildingOdmDlkmImage {
		generatedPartitions = append(generatedPartitions, "odm_dlkm")
	}
	if partitionVars.BuildingRamdiskImage {
		generatedPartitions = append(generatedPartitions, "ramdisk")
	}
	if buildingVendorBootImage(partitionVars) {
		generatedPartitions = append(generatedPartitions, "vendor_ramdisk")
	}
	if ctx.DeviceConfig().BuildingRecoveryImage() && ctx.DeviceConfig().RecoveryPath() == "recovery" {
		generatedPartitions = append(generatedPartitions, "recovery")
	}
	return generatedPartitions
}

func (f *filesystemCreator) createInternalModules(ctx android.LoadHookContext) {
	soongGeneratedPartitions := generatedPartitions(ctx)
	finalSoongGeneratedPartitions := make([]string, 0, len(soongGeneratedPartitions))
	for _, partitionType := range soongGeneratedPartitions {
		if f.createPartition(ctx, partitionType) {
			f.properties.Generated_partition_types = append(f.properties.Generated_partition_types, partitionType)
			finalSoongGeneratedPartitions = append(finalSoongGeneratedPartitions, partitionType)
		} else {
			f.properties.Unsupported_partition_types = append(f.properties.Unsupported_partition_types, partitionType)
		}
	}

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

	for _, x := range createVbmetaPartitions(ctx, finalSoongGeneratedPartitions) {
		f.properties.Vbmeta_module_names = append(f.properties.Vbmeta_module_names, x.moduleName)
		f.properties.Vbmeta_partition_names = append(f.properties.Vbmeta_partition_names, x.partitionName)
	}

	if buildingSuperImage(partitionVars) {
		createSuperImage(ctx, finalSoongGeneratedPartitions, partitionVars)
		f.properties.Super_image = ":" + generatedModuleName(ctx.Config(), "super")
	}

	ctx.Config().Get(fsGenStateOnceKey).(*FsGenState).soongGeneratedPartitions = finalSoongGeneratedPartitions
	f.createDeviceModule(ctx, finalSoongGeneratedPartitions, f.properties.Vbmeta_module_names)
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

func (f *filesystemCreator) createDeviceModule(
	ctx android.LoadHookContext,
	generatedPartitionTypes []string,
	vbmetaPartitions []string,
) {
	baseProps := &struct {
		Name *string
	}{
		Name: proptools.StringPtr(generatedModuleName(ctx.Config(), "device")),
	}

	// Currently, only the system and system_ext partition module is created.
	partitionProps := &filesystem.PartitionNameProperties{}
	if android.InList("system", generatedPartitionTypes) {
		partitionProps.System_partition_name = proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), "system"))
	}
	if android.InList("system_ext", generatedPartitionTypes) {
		partitionProps.System_ext_partition_name = proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), "system_ext"))
	}
	if android.InList("vendor", generatedPartitionTypes) {
		partitionProps.Vendor_partition_name = proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), "vendor"))
	}
	if android.InList("product", generatedPartitionTypes) {
		partitionProps.Product_partition_name = proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), "product"))
	}
	if android.InList("odm", generatedPartitionTypes) {
		partitionProps.Odm_partition_name = proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), "odm"))
	}
	if android.InList("userdata", f.properties.Generated_partition_types) {
		partitionProps.Userdata_partition_name = proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), "userdata"))
	}
	partitionProps.Vbmeta_partitions = vbmetaPartitions

	ctx.CreateModule(filesystem.AndroidDeviceFactory, baseProps, partitionProps)
}

func partitionSpecificFsProps(ctx android.EarlyModuleContext, fsProps *filesystem.FilesystemProperties, partitionVars android.PartitionVariables, partitionType string) {
	switch partitionType {
	case "system":
		fsProps.Build_logtags = proptools.BoolPtr(true)
		// https://source.corp.google.com/h/googleplex-android/platform/build//639d79f5012a6542ab1f733b0697db45761ab0f3:core/packaging/flags.mk;l=21;drc=5ba8a8b77507f93aa48cc61c5ba3f31a4d0cbf37;bpv=1;bpt=0
		fsProps.Gen_aconfig_flags_pb = proptools.BoolPtr(true)
		// Identical to that of the aosp_shared_system_image
		if partitionVars.ProductFsverityGenerateMetadata {
			fsProps.Fsverity.Inputs = []string{
				"etc/boot-image.prof",
				"etc/dirty-image-objects",
				"etc/preloaded-classes",
				"etc/classpaths/*.pb",
				"framework/*",
				"framework/*/*",     // framework/{arch}
				"framework/oat/*/*", // framework/oat/{arch}
			}
			fsProps.Fsverity.Libs = []string{":framework-res{.export-package.apk}"}
		}
		// Most of the symlinks and directories listed here originate from create_root_structure.mk,
		// but the handwritten generic system image also recreates them:
		// https://cs.android.com/android/platform/superproject/main/+/main:build/make/target/product/generic/Android.bp;l=33;drc=db08311f1b6ef6cb0a4fbcc6263b89849360ce04
		// TODO(b/377734331): only generate the symlinks if the relevant partitions exist
		fsProps.Symlinks = []filesystem.SymlinkDefinition{
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/system/bin/init"),
				Name:   proptools.StringPtr("init"),
			},
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/system/etc"),
				Name:   proptools.StringPtr("etc"),
			},
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/system/bin"),
				Name:   proptools.StringPtr("bin"),
			},
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/data/user_de/0/com.android.shell/files/bugreports"),
				Name:   proptools.StringPtr("bugreports"),
			},
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/sys/kernel/debug"),
				Name:   proptools.StringPtr("d"),
			},
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/storage/self/primary"),
				Name:   proptools.StringPtr("sdcard"),
			},
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/product/etc/security/adb_keys"),
				Name:   proptools.StringPtr("adb_keys"),
			},
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/vendor/odm/app"),
				Name:   proptools.StringPtr("odm/app"),
			},
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/vendor/odm/bin"),
				Name:   proptools.StringPtr("odm/bin"),
			},
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/vendor/odm/etc"),
				Name:   proptools.StringPtr("odm/etc"),
			},
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/vendor/odm/firmware"),
				Name:   proptools.StringPtr("odm/firmware"),
			},
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/vendor/odm/framework"),
				Name:   proptools.StringPtr("odm/framework"),
			},
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/vendor/odm/lib"),
				Name:   proptools.StringPtr("odm/lib"),
			},
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/vendor/odm/lib64"),
				Name:   proptools.StringPtr("odm/lib64"),
			},
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/vendor/odm/overlay"),
				Name:   proptools.StringPtr("odm/overlay"),
			},
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/vendor/odm/priv-app"),
				Name:   proptools.StringPtr("odm/priv-app"),
			},
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/vendor/odm/usr"),
				Name:   proptools.StringPtr("odm/usr"),
			},
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/product"),
				Name:   proptools.StringPtr("system/product"),
			},
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/system_ext"),
				Name:   proptools.StringPtr("system/system_ext"),
			},
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/vendor"),
				Name:   proptools.StringPtr("system/vendor"),
			},
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/system_dlkm/lib/modules"),
				Name:   proptools.StringPtr("system/lib/modules"),
			},
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/data/cache"),
				Name:   proptools.StringPtr("cache"),
			},
			// For Treble Generic System Image (GSI), system-as-root GSI needs to work on
			// both devices with and without /odm_dlkm partition. Those symlinks are for
			// devices without /odm_dlkm partition. For devices with /odm_dlkm
			// partition, mount odm_dlkm.img under /odm_dlkm will hide those symlinks.
			// Note that /odm_dlkm/lib is omitted because odm DLKMs should be accessed
			// via /odm/lib/modules directly. All of this also applies to the vendor_dlkm symlink
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/odm/odm_dlkm/etc"),
				Name:   proptools.StringPtr("odm_dlkm/etc"),
			},
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/vendor/vendor_dlkm/etc"),
				Name:   proptools.StringPtr("vendor_dlkm/etc"),
			},
		}
		fsProps.Base_dir = proptools.StringPtr("system")
		fsProps.Dirs = proptools.NewSimpleConfigurable([]string{
			// From generic_rootdirs in build/make/target/product/generic/Android.bp
			"acct",
			"apex",
			"bootstrap-apex",
			"config",
			"data",
			"data_mirror",
			"debug_ramdisk",
			"dev",
			"linkerconfig",
			"metadata",
			"mnt",
			"odm",
			"odm_dlkm",
			"oem",
			"postinstall",
			"proc",
			"second_stage_resources",
			"storage",
			"sys",
			"system",
			"system_dlkm",
			"tmp",
			"vendor",
			"vendor_dlkm",

			// from android_rootdirs in build/make/target/product/generic/Android.bp
			"system_ext",
			"product",
		})
	case "system_ext":
		if partitionVars.ProductFsverityGenerateMetadata {
			fsProps.Fsverity.Inputs = []string{
				"framework/*",
				"framework/*/*",     // framework/{arch}
				"framework/oat/*/*", // framework/oat/{arch}
			}
			fsProps.Fsverity.Libs = []string{":framework-res{.export-package.apk}"}
		}
	case "product":
		fsProps.Gen_aconfig_flags_pb = proptools.BoolPtr(true)
		fsProps.Android_filesystem_deps.System = proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), "system"))
		if ctx.DeviceConfig().SystemExtPath() == "system_ext" {
			fsProps.Android_filesystem_deps.System_ext = proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), "system_ext"))
		}
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
		fsProps.Android_filesystem_deps.System = proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), "system"))
		if ctx.DeviceConfig().SystemExtPath() == "system_ext" {
			fsProps.Android_filesystem_deps.System_ext = proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), "system_ext"))
		}
	case "odm":
		fsProps.Symlinks = []filesystem.SymlinkDefinition{
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/odm_dlkm/lib/modules"),
				Name:   proptools.StringPtr("lib/modules"),
			},
		}
	case "userdata":
		fsProps.Base_dir = proptools.StringPtr("data")
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
	case "recovery":
		// Following https://cs.android.com/android/platform/superproject/main/+/main:build/make/core/Makefile;l=2826;drc=ad7cfb56010cb22c3aa0e70cf71c804352553526
		fsProps.Dirs = android.NewSimpleConfigurable([]string{
			"sdcard",
			"tmp",
		})
		fsProps.Symlinks = []filesystem.SymlinkDefinition{
			{
				Target: proptools.StringPtr("/system/bin/init"),
				Name:   proptools.StringPtr("init"),
			},
			{
				Target: proptools.StringPtr("prop.default"),
				Name:   proptools.StringPtr("default.prop"),
			},
		}
		fsProps.Base_dir = proptools.StringPtr("recovery")
	}
}

var (
	dlkmPartitions = []string{
		"system_dlkm",
		"vendor_dlkm",
		"odm_dlkm",
	}
)

// Creates a soong module to build the given partition. Returns false if we can't support building
// it.
func (f *filesystemCreator) createPartition(ctx android.LoadHookContext, partitionType string) bool {
	baseProps := generateBaseProps(proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), partitionType)))

	fsProps, supported := generateFsProps(ctx, partitionType)
	if !supported {
		return false
	}

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
	return true
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
	}{
		Name: proptools.StringPtr(name),
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

// Create a build_prop and android_info module. This will be used to create /vendor/build.prop
func (f *filesystemCreator) createVendorBuildProp(ctx android.LoadHookContext) {
	// Create a android_info for vendor
	// The board info files might be in a directory outside the root soong namespace, so create
	// the module in "."
	partitionVars := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse
	androidInfoProps := &struct {
		Name                  *string
		Board_info_files      []string
		Bootloader_board_name *string
	}{
		Name:             proptools.StringPtr(generatedModuleName(ctx.Config(), "android-info.prop")),
		Board_info_files: partitionVars.BoardInfoFiles,
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
	// Create a build prop for vendor
	vendorBuildProps := &struct {
		Name           *string
		Vendor         *bool
		Stem           *string
		Product_config *string
		Android_info   *string
	}{
		Name:           proptools.StringPtr(generatedModuleName(ctx.Config(), "vendor-build.prop")),
		Vendor:         proptools.BoolPtr(true),
		Stem:           proptools.StringPtr("build.prop"),
		Product_config: proptools.StringPtr(":product_config"),
		Android_info:   proptools.StringPtr(":" + androidInfoProp.Name()),
	}
	vendorBuildProp := ctx.CreateModule(
		android.BuildPropFactory,
		vendorBuildProps,
	)
	vendorBuildProp.HideFromMake()
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

func generateFsProps(ctx android.EarlyModuleContext, partitionType string) (*filesystem.FilesystemProperties, bool) {
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
	fsProps.Avb_hash_algorithm = avbInfo.avbHashAlgorithm

	fsProps.Partition_name = proptools.StringPtr(partitionType)

	switch partitionType {
	// The partitions that support file_contexts came from here:
	// https://cs.android.com/android/platform/superproject/main/+/main:build/make/core/Makefile;l=2270;drc=ad7cfb56010cb22c3aa0e70cf71c804352553526
	case "system", "userdata", "cache", "vendor", "product", "system_ext", "odm", "vendor_dlkm", "odm_dlkm", "system_dlkm", "oem":
		fsProps.Precompiled_file_contexts = proptools.StringPtr(":file_contexts_bin_gen")
	}

	fsProps.Is_auto_generated = proptools.BoolPtr(true)

	partitionSpecificFsProps(ctx, fsProps, partitionVars, partitionType)

	return fsProps, true
}

type avbInfo struct {
	avbEnable        *bool
	avbKeyPath       *string
	avbkeyFilegroup  *string
	avbAlgorithm     *string
	avbRollbackIndex *int64
	avbMode          *string
	avbHashAlgorithm *string
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
		if specificPartitionVars.BoardAvbRollbackIndex != "" {
			parsed, err := strconv.ParseInt(specificPartitionVars.BoardAvbRollbackIndex, 10, 64)
			if err != nil {
				panic(fmt.Sprintf("Rollback index must be an int, got %s", specificPartitionVars.BoardAvbRollbackIndex))
			}
			result.avbRollbackIndex = &parsed
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

func (f *filesystemCreator) createFileListDiffTest(ctx android.ModuleContext, partitionType string) android.Path {
	partitionModuleName := generatedModuleNameForPartition(ctx.Config(), partitionType)
	systemImage := ctx.GetDirectDepWithTag(partitionModuleName, generatedFilesystemDepTag)
	filesystemInfo, ok := android.OtherModuleProvider(ctx, systemImage, filesystem.FilesystemProvider)
	if !ok {
		ctx.ModuleErrorf("Expected module %s to provide FileysystemInfo", partitionModuleName)
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

type systemImageDepTagType struct {
	blueprint.BaseDependencyTag
}

var generatedFilesystemDepTag systemImageDepTagType
var generatedVbmetaPartitionDepTag systemImageDepTagType

func (f *filesystemCreator) DepsMutator(ctx android.BottomUpMutatorContext) {
	for _, partitionType := range f.properties.Generated_partition_types {
		ctx.AddDependency(ctx.Module(), generatedFilesystemDepTag, generatedModuleNameForPartition(ctx.Config(), partitionType))
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

	var content strings.Builder
	generatedBp := android.PathForModuleOut(ctx, "soong_generated_product_config.bp")
	for _, partition := range ctx.Config().Get(fsGenStateOnceKey).(*FsGenState).soongGeneratedPartitions {
		content.WriteString(generateBpContent(ctx, partition))
		content.WriteString("\n")
	}
	android.WriteFileRule(ctx, generatedBp, content.String())

	ctx.Phony("product_config_to_bp", generatedBp)

	var diffTestFiles []android.Path
	for _, partitionType := range f.properties.Generated_partition_types {
		diffTestFile := f.createFileListDiffTest(ctx, partitionType)
		diffTestFiles = append(diffTestFiles, diffTestFile)
		ctx.Phony(fmt.Sprintf("soong_generated_%s_filesystem_test", partitionType), diffTestFile)
	}
	for _, partitionType := range f.properties.Unsupported_partition_types {
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
	fsProps, fsTypeSupported := generateFsProps(ctx, partitionType)
	if !fsTypeSupported {
		return ""
	}

	baseProps := generateBaseProps(proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), partitionType)))
	fsGenState := ctx.Config().Get(fsGenStateOnceKey).(*FsGenState)
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
