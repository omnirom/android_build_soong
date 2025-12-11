package fsgen

import (
	"android/soong/android"
	"android/soong/filesystem"
	"android/soong/genrule"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/blueprint/proptools"
)

// helper function to create boot.img and boot_16k.img
func createBootImageCommon(ctx android.LoadHookContext, kernelPath string, prebuiltBootImagePath string, dtbImg dtbImg, stem *string) bool {
	getPartitionSize := func(partitionVariables android.PartitionVariables) *int64 {
		var partitionSize *int64
		if partitionVariables.BoardBootimagePartitionSize != "" {
			// Base of zero will allow base 10 or base 16 if starting with 0x
			parsed, err := strconv.ParseInt(partitionVariables.BoardBootimagePartitionSize, 0, 64)
			if err != nil {
				panic(fmt.Sprintf("BOARD_BOOTIMAGE_PARTITION_SIZE must be an int, got %s", partitionVariables.BoardBootimagePartitionSize))
			}
			partitionSize = &parsed
		}
		return partitionSize
	}
	partitionVariables := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse
	avbInfo := getAvbInfo(ctx.Config(), "boot")
	bootImageName := generatedModuleNameForPartition(ctx.Config(), strings.TrimSuffix(*stem, ".img"))
	var securityPatch *string
	if partitionVariables.BootSecurityPatch != "" {
		securityPatch = &partitionVariables.BootSecurityPatch
	}

	if prebuiltBootImagePath != "" {
		// prebuilt bootimg
		ctx.CreateModuleInDirectory(
			filesystem.PrebuiltBootimgFactory,
			".",
			&struct {
				Name *string
				Src  *string
			}{
				Name: proptools.StringPtr(bootImageName),
				Src:  proptools.StringPtr(prebuiltBootImagePath),
			},
			&filesystem.CommonBootimgProperties{
				Boot_image_type:             proptools.StringPtr("boot"),
				Partition_name:              proptools.StringPtr("boot"),
				Header_version:              proptools.StringPtr(partitionVariables.BoardBootHeaderVersion),
				Partition_size:              getPartitionSize(partitionVariables),
				Use_avb:                     avbInfo.avbEnable,
				Avb_mode:                    avbInfo.avbMode,
				Avb_private_key:             avbInfo.avbkeyFilegroup,
				Avb_rollback_index:          avbInfo.avbRollbackIndex,
				Avb_rollback_index_location: avbInfo.avbRollbackIndexLocation,
				Avb_algorithm:               avbInfo.avbAlgorithm,
				Security_patch:              securityPatch,
			},
		)

		return true
	}

	if kernelPath == "" {
		// There are potentially code paths that don't use `PRODUCT_COPY_FILES` to copy the kernel to
		// ANDROID_PRODUCT_OUT
		return false
	}

	kernelDir := filepath.Dir(kernelPath)
	kernelBase := filepath.Base(kernelPath)
	kernelFilegroupName := generatedModuleName(ctx.Config(), "kernel"+*stem) // to prevent name collisions.

	ctx.CreateModuleInDirectory(
		android.FileGroupFactory,
		kernelDir,
		&struct {
			Name       *string
			Srcs       []string
			Visibility []string
		}{
			Name:       proptools.StringPtr(kernelFilegroupName),
			Srcs:       []string{kernelBase},
			Visibility: []string{"//visibility:public"},
		},
	)

	var dtbPrebuilt *string
	if dtbImg.include && dtbImg.imgType == "boot" {
		dtbPrebuilt = proptools.StringPtr(":" + dtbImg.name)
	}

	var cmdline []string
	if !buildingVendorBootImage(partitionVariables) {
		cmdline = partitionVariables.InternalKernelCmdline
	}

	ctx.CreateModule(
		filesystem.BootimgFactory,
		&filesystem.BootimgProperties{
			Kernel_prebuilt: proptools.NewSimpleConfigurable(":" + kernelFilegroupName),
			Dtb_prebuilt:    dtbPrebuilt,
			Cmdline:         cmdline,
			Stem:            stem,
		},
		&filesystem.CommonBootimgProperties{
			Boot_image_type:             proptools.StringPtr("boot"),
			Partition_name:              proptools.StringPtr("boot"),
			Header_version:              proptools.StringPtr(partitionVariables.BoardBootHeaderVersion),
			Partition_size:              getPartitionSize(partitionVariables),
			Use_avb:                     avbInfo.avbEnable,
			Avb_mode:                    avbInfo.avbMode,
			Avb_private_key:             avbInfo.avbkeyFilegroup,
			Avb_rollback_index:          avbInfo.avbRollbackIndex,
			Avb_rollback_index_location: avbInfo.avbRollbackIndexLocation,
			Avb_algorithm:               avbInfo.avbAlgorithm,
			Security_patch:              securityPatch,
		},
		&struct {
			Name       *string
			Visibility []string
		}{
			Name:       proptools.StringPtr(bootImageName),
			Visibility: []string{"//visibility:public"},
		},
	)
	return true
}

func createBootImage(ctx android.LoadHookContext, dtbImg dtbImg) bool {
	partitionVariables := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse
	return createBootImageCommon(ctx, getPrebuiltKernelPath(ctx), partitionVariables.BoardPrebuiltBootImage, dtbImg, proptools.StringPtr("boot.img"))
}

func createBootImage16k(ctx android.LoadHookContext) bool {
	partitionVariables := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse
	// TODO: prebuilt boot_16k.img and dtb is currently not supported in fsgen.
	return createBootImageCommon(ctx, partitionVariables.BoardKernelPath16k, "", dtbImg{include: false}, proptools.StringPtr("boot_16k.img"))
}

func createVendorBootImage(ctx android.LoadHookContext, dtbImg dtbImg) bool {
	partitionVariables := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse

	bootImageName := generatedModuleNameForPartition(ctx.Config(), "vendor_boot")

	avbInfo := getAvbInfo(ctx.Config(), "vendor_boot")

	var dtbPrebuilt *string
	if dtbImg.include && dtbImg.imgType == "vendor_boot" {
		dtbPrebuilt = proptools.StringPtr(":" + dtbImg.name)
	}

	cmdline := partitionVariables.InternalKernelCmdline

	var vendorBootConfigImg *string
	if name, ok := createVendorBootConfigImg(ctx); ok {
		vendorBootConfigImg = proptools.StringPtr(":" + name)
	}

	var partitionSize *int64
	if partitionVariables.BoardVendorBootimagePartitionSize != "" {
		// Base of zero will allow base 10 or base 16 if starting with 0x
		parsed, err := strconv.ParseInt(partitionVariables.BoardVendorBootimagePartitionSize, 0, 64)
		if err != nil {
			ctx.ModuleErrorf("BOARD_VENDOR_BOOTIMAGE_PARTITION_SIZE must be an int, got %s", partitionVariables.BoardVendorBootimagePartitionSize)
		}
		partitionSize = &parsed
	}

	var ramdiskFragmentModules []string
	if buildingVendorRamdiskFragmentDlkm(ctx, partitionVariables) {
		ramdiskFragmentModules = append(ramdiskFragmentModules, generatedModuleNameForPartition(ctx.Config(), "vendor_ramdisk_fragment_dlkm"))
	}
	if ramdisk16kModuleName := createRamdisk16k(ctx); ramdisk16kModuleName != "" {
		ramdiskFragmentModules = append(ramdiskFragmentModules, ramdisk16kModuleName)
	}

	ctx.CreateModule(
		filesystem.BootimgFactory,
		&filesystem.BootimgProperties{
			Ramdisk_module:           proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), "vendor_ramdisk")),
			Ramdisk_fragment_modules: ramdiskFragmentModules,
			Dtb_prebuilt:             dtbPrebuilt,
			Cmdline:                  cmdline,
			Bootconfig:               vendorBootConfigImg,
			Stem:                     proptools.StringPtr("vendor_boot.img"),
		},
		&filesystem.CommonBootimgProperties{
			Boot_image_type:             proptools.StringPtr("vendor_boot"),
			Partition_name:              proptools.StringPtr("vendor_boot"),
			Header_version:              proptools.StringPtr(partitionVariables.BoardBootHeaderVersion),
			Partition_size:              partitionSize,
			Use_avb:                     avbInfo.avbEnable,
			Avb_mode:                    avbInfo.avbMode,
			Avb_private_key:             avbInfo.avbkeyFilegroup,
			Avb_rollback_index:          avbInfo.avbRollbackIndex,
			Avb_rollback_index_location: avbInfo.avbRollbackIndexLocation,
		},

		&struct {
			Name       *string
			Visibility []string
		}{
			Name:       proptools.StringPtr(bootImageName),
			Visibility: []string{"//visibility:public"},
		},
	)
	return true
}

// Same as vendor_boot, with the exception of the ramdisk module.
func createVendorBootDebugImage(ctx android.LoadHookContext, dtbImg dtbImg) bool {
	partitionVariables := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse

	bootImageName := generatedModuleNameForPartition(ctx.Config(), "vendor_boot-debug")

	avbInfo := getAvbInfo(ctx.Config(), "vendor_boot")

	var dtbPrebuilt *string
	if dtbImg.include && dtbImg.imgType == "vendor_boot" {
		dtbPrebuilt = proptools.StringPtr(":" + dtbImg.name)
	}

	cmdline := partitionVariables.InternalKernelCmdline

	var vendorBootConfigImg *string
	if name := getVendorBootConfigImgName(ctx); name != "" {
		vendorBootConfigImg = proptools.StringPtr(":" + name)
	}

	var partitionSize *int64
	if partitionVariables.BoardVendorBootimagePartitionSize != "" {
		// Base of zero will allow base 10 or base 16 if starting with 0x
		parsed, err := strconv.ParseInt(partitionVariables.BoardVendorBootimagePartitionSize, 0, 64)
		if err != nil {
			ctx.ModuleErrorf("BOARD_VENDOR_BOOTIMAGE_PARTITION_SIZE must be an int, got %s", partitionVariables.BoardVendorBootimagePartitionSize)
		}
		partitionSize = &parsed
	}

	var ramdiskFragmentModules []string
	if buildingVendorRamdiskFragmentDlkm(ctx, partitionVariables) {
		ramdiskFragmentModules = append(ramdiskFragmentModules, generatedModuleNameForPartition(ctx.Config(), "vendor_ramdisk_fragment_dlkm"))
	}
	ctx.CreateModule(
		filesystem.BootimgFactory,
		&filesystem.BootimgProperties{
			Ramdisk_module:           proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), "vendor_ramdisk-debug")),
			Ramdisk_fragment_modules: ramdiskFragmentModules,
			Dtb_prebuilt:             dtbPrebuilt,
			Cmdline:                  cmdline,
			Bootconfig:               vendorBootConfigImg,
			Stem:                     proptools.StringPtr("vendor_boot-debug.img"),
		},
		&filesystem.CommonBootimgProperties{
			Boot_image_type:             proptools.StringPtr("vendor_boot"),
			Partition_name:              proptools.StringPtr("vendor_boot"),
			Header_version:              proptools.StringPtr(partitionVariables.BoardBootHeaderVersion),
			Partition_size:              partitionSize,
			Use_avb:                     avbInfo.avbEnable,
			Avb_mode:                    avbInfo.avbMode,
			Avb_private_key:             avbInfo.avbkeyFilegroup,
			Avb_rollback_index:          avbInfo.avbRollbackIndex,
			Avb_rollback_index_location: avbInfo.avbRollbackIndexLocation,
		},

		&struct {
			Name       *string
			Visibility []string
		}{
			Name:       proptools.StringPtr(bootImageName),
			Visibility: []string{"//visibility:public"},
		},
	)
	return true
}

// Same as vendor_boot-debug, with the exception of some additional properties in the installed adb_debug.prop file.
func createVendorBootTestHarnessImage(ctx android.LoadHookContext, dtbImg dtbImg) bool {
	partitionVariables := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse

	bootImageName := generatedModuleNameForPartition(ctx.Config(), "vendor_boot-test-harness")

	avbInfo := getAvbInfo(ctx.Config(), "vendor_boot")

	var dtbPrebuilt *string
	if dtbImg.include && dtbImg.imgType == "vendor_boot" {
		dtbPrebuilt = proptools.StringPtr(":" + dtbImg.name)
	}

	cmdline := partitionVariables.InternalKernelCmdline

	var vendorBootConfigImg *string
	if name := getVendorBootConfigImgName(ctx); name != "" {
		vendorBootConfigImg = proptools.StringPtr(":" + name)
	}

	var partitionSize *int64
	if partitionVariables.BoardVendorBootimagePartitionSize != "" {
		// Base of zero will allow base 10 or base 16 if starting with 0x
		parsed, err := strconv.ParseInt(partitionVariables.BoardVendorBootimagePartitionSize, 0, 64)
		if err != nil {
			ctx.ModuleErrorf("BOARD_VENDOR_BOOTIMAGE_PARTITION_SIZE must be an int, got %s", partitionVariables.BoardVendorBootimagePartitionSize)
		}
		partitionSize = &parsed
	}

	var ramdiskFragmentModules []string
	if buildingVendorRamdiskFragmentDlkm(ctx, partitionVariables) {
		ramdiskFragmentModules = append(ramdiskFragmentModules, generatedModuleNameForPartition(ctx.Config(), "vendor_ramdisk_fragment_dlkm"))
	}
	ctx.CreateModule(
		filesystem.BootimgFactory,
		&filesystem.BootimgProperties{
			Ramdisk_module:           proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), "vendor_ramdisk-test-harness")),
			Ramdisk_fragment_modules: ramdiskFragmentModules,
			Dtb_prebuilt:             dtbPrebuilt,
			Cmdline:                  cmdline,
			Bootconfig:               vendorBootConfigImg,
			Stem:                     proptools.StringPtr("vendor_boot-test-harness.img"),
		},
		&filesystem.CommonBootimgProperties{
			Boot_image_type:             proptools.StringPtr("vendor_boot"),
			Partition_name:              proptools.StringPtr("vendor_boot"),
			Header_version:              proptools.StringPtr(partitionVariables.BoardBootHeaderVersion),
			Partition_size:              partitionSize,
			Use_avb:                     avbInfo.avbEnable,
			Avb_mode:                    avbInfo.avbMode,
			Avb_private_key:             avbInfo.avbkeyFilegroup,
			Avb_rollback_index:          avbInfo.avbRollbackIndex,
			Avb_rollback_index_location: avbInfo.avbRollbackIndexLocation,
		},

		&struct {
			Name       *string
			Visibility []string
		}{
			Name:       proptools.StringPtr(bootImageName),
			Visibility: []string{"//visibility:public"},
		},
	)
	return true
}

func createVendorKernelBootImage(ctx android.LoadHookContext, dtbImg dtbImg) bool {
	vendorKernelBootVariables, exists := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse.PartitionQualifiedVariables["vendor_kernel_boot"]
	if !exists {
		return false
	}

	bootImageName := generatedModuleNameForPartition(ctx.Config(), "vendor_kernel_boot")

	avbInfo := getAvbInfo(ctx.Config(), "vendor_kernel_boot")

	var dtbPrebuilt *string
	if dtbImg.include && dtbImg.imgType == "vendor_kernel_boot" {
		dtbPrebuilt = proptools.StringPtr(":" + dtbImg.name)
	}

	var partitionSize *int64
	if vendorKernelBootVariables.BoardPartitionSize != "" {
		// Base of zero will allow base 10 or base 16 if starting with 0x
		parsed, err := strconv.ParseInt(vendorKernelBootVariables.BoardPartitionSize, 0, 64)
		if err != nil {
			ctx.ModuleErrorf("BOARD_VENDOR_KERNEL_BOOTIMAGE_PARTITION_SIZE must be an int, got %s", vendorKernelBootVariables.BoardPartitionSize)
		}
		partitionSize = &parsed
	}

	ctx.CreateModule(
		filesystem.BootimgFactory,
		&filesystem.BootimgProperties{
			Ramdisk_module: proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), "vendor_kernel_ramdisk")),
			Dtb_prebuilt:   dtbPrebuilt,
			Stem:           proptools.StringPtr("vendor_kernel_boot.img"),
		},
		&filesystem.CommonBootimgProperties{
			Boot_image_type:             proptools.StringPtr("vendor_kernel_boot"),
			Partition_name:              proptools.StringPtr("vendor_kernel_boot"),
			Header_version:              proptools.StringPtr("4"),
			Partition_size:              partitionSize,
			Use_avb:                     avbInfo.avbEnable,
			Avb_mode:                    avbInfo.avbMode,
			Avb_private_key:             avbInfo.avbkeyFilegroup,
			Avb_rollback_index:          avbInfo.avbRollbackIndex,
			Avb_rollback_index_location: avbInfo.avbRollbackIndexLocation,
		},

		&struct {
			Name       *string
			Visibility []string
		}{
			Name:       proptools.StringPtr(bootImageName),
			Visibility: []string{"//visibility:public"},
		},
	)
	return true
}

func createInitBootImage(ctx android.LoadHookContext) bool {
	partitionVariables := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse

	bootImageName := generatedModuleNameForPartition(ctx.Config(), "init_boot")

	var securityPatch *string
	if partitionVariables.InitBootSecurityPatch != "" {
		securityPatch = &partitionVariables.InitBootSecurityPatch
	} else if partitionVariables.BootSecurityPatch != "" {
		securityPatch = &partitionVariables.BootSecurityPatch
	}

	var partitionSize *int64
	if partitionVariables.BoardInitBootimagePartitionSize != "" {
		// Base of zero will allow base 10 or base 16 if starting with 0x
		parsed, err := strconv.ParseInt(partitionVariables.BoardInitBootimagePartitionSize, 0, 64)
		if err != nil {
			panic(fmt.Sprintf("BOARD_INIT_BOOT_IMAGE_PARTITION_SIZE must be an int, got %s", partitionVariables.BoardInitBootimagePartitionSize))
		}
		partitionSize = &parsed
	}

	avbInfo := getAvbInfo(ctx.Config(), "init_boot")

	ctx.CreateModule(
		filesystem.BootimgFactory,
		&filesystem.BootimgProperties{
			Ramdisk_module: proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), "ramdisk")),
			Stem:           proptools.StringPtr("init_boot.img"),
		},
		&filesystem.CommonBootimgProperties{
			Boot_image_type:             proptools.StringPtr("init_boot"),
			Partition_name:              proptools.StringPtr("init_boot"),
			Header_version:              proptools.StringPtr(partitionVariables.BoardInitBootHeaderVersion),
			Partition_size:              partitionSize,
			Use_avb:                     avbInfo.avbEnable,
			Avb_mode:                    avbInfo.avbMode,
			Avb_private_key:             avbInfo.avbkeyFilegroup,
			Avb_rollback_index:          avbInfo.avbRollbackIndex,
			Avb_rollback_index_location: avbInfo.avbRollbackIndexLocation,
			Avb_algorithm:               avbInfo.avbAlgorithm,
			Security_patch:              securityPatch,
		},
		&struct {
			Name       *string
			Visibility []string
		}{
			Name:       proptools.StringPtr(bootImageName),
			Visibility: []string{"//visibility:public"},
		},
	)
	return true
}

// Returns the equivalent of the BUILDING_BOOT_IMAGE variable in make. Derived from this logic:
// https://cs.android.com/android/platform/superproject/main/+/main:build/make/core/board_config.mk;l=458;drc=5b55f926830963c02ab1d2d91e46442f04ba3af0
func buildingBootImage(partitionVars android.PartitionVariables) bool {
	if partitionVars.BoardUsesRecoveryAsBoot {
		return false
	}

	if partitionVars.ProductBuildBootImage {
		return true
	}

	if len(partitionVars.BoardPrebuiltBootimage) > 0 {
		return false
	}

	if len(partitionVars.BoardBootimagePartitionSize) > 0 {
		return true
	}

	// TODO: return true if BOARD_KERNEL_BINARIES is set and has a *_BOOTIMAGE_PARTITION_SIZE
	// variable. However, I don't think BOARD_KERNEL_BINARIES is ever set in practice.

	return false
}

// Returns the equivalent of the BUILDING_VENDOR_BOOT_IMAGE variable in make. Derived from this logic:
// https://cs.android.com/android/platform/superproject/main/+/main:build/make/core/board_config.mk;l=518;drc=5b55f926830963c02ab1d2d91e46442f04ba3af0
func buildingVendorBootImage(partitionVars android.PartitionVariables) bool {
	if v, exists := boardBootHeaderVersion(partitionVars); exists && v >= 3 {
		x := partitionVars.ProductBuildVendorBootImage
		if x == "" || x == "true" {
			return true
		}
	}

	return false
}

func buildingDebugVendorBootImage(partitionVars android.PartitionVariables) bool {
	return buildingVendorBootImage(partitionVars) && partitionVars.BuildingDebugVendorBootImage
}

func buildingDebugRamdiskImage(partitionVars android.PartitionVariables) bool {
	return partitionVars.BuildingDebugBootImage || partitionVars.BuildingDebugVendorBootImage
}

func buildingVendorKernelBootImage(partitionVars android.PartitionVariables) bool {
	vendorKernelBootVariables, exists := partitionVars.PartitionQualifiedVariables["vendor_kernel_boot"]
	return exists && vendorKernelBootVariables.BuildingImage
}

func buildingVendorRamdiskFragmentDlkm(ctx android.EarlyModuleContext, partitionVars android.PartitionVariables) bool {
	if len(partitionVars.BoardVendorRamdiskFragments) == 0 {
		return false
	}
	if len(partitionVars.BoardVendorRamdiskFragments) > 1 || partitionVars.BoardVendorRamdiskFragments[0] != "dlkm" {
		ctx.ModuleErrorf("Multiple vendor ramdisk fragments is currently not supported in soong only builds")
	}
	return true
}

// Derived from: https://cs.android.com/android/platform/superproject/main/+/main:build/make/core/board_config.mk;l=480;drc=5b55f926830963c02ab1d2d91e46442f04ba3af0
func buildingInitBootImage(partitionVars android.PartitionVariables) bool {
	if !partitionVars.ProductBuildInitBootImage {
		if partitionVars.BoardUsesRecoveryAsBoot || len(partitionVars.BoardPrebuiltInitBootimage) > 0 {
			return false
		} else if len(partitionVars.BoardInitBootimagePartitionSize) > 0 {
			return true
		}
	} else {
		if partitionVars.BoardUsesRecoveryAsBoot {
			panic("PRODUCT_BUILD_INIT_BOOT_IMAGE is true, but so is BOARD_USES_RECOVERY_AS_BOOT. Use only one option.")
		}
		return true
	}
	return false
}

func boardBootHeaderVersion(partitionVars android.PartitionVariables) (int, bool) {
	if len(partitionVars.BoardBootHeaderVersion) == 0 {
		return 0, false
	}
	v, err := strconv.ParseInt(partitionVars.BoardBootHeaderVersion, 10, 32)
	if err != nil {
		panic(fmt.Sprintf("BOARD_BOOT_HEADER_VERSION must be an int, got: %q", partitionVars.BoardBootHeaderVersion))
	}
	return int(v), true
}

type dtbImg struct {
	// whether to include the dtb image in boot image
	include bool

	// name of the generated dtb image filegroup name
	name string

	// type of the boot image that the dtb image argument should be specified
	imgType string
}

func createDtbImgFilegroup(ctx android.LoadHookContext) dtbImg {
	partitionVars := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse
	if !partitionVars.BoardIncludeDtbInBootimg {
		return dtbImg{include: false}
	}
	moduleName := generatedModuleName(ctx.Config(), "dtb_img_filegroup")
	imgType := "vendor_boot"
	if !buildingVendorBootImage(partitionVars) {
		imgType = "boot"
	}
	if buildingVendorKernelBootImage(partitionVars) {
		// https://cs.android.com/android/platform/superproject/main/+/main:build/make/core/Makefile;l=1655-1658?q=INTERNAL_VENDOR_BOOTIMAGE_ARGS&ss=android%2Fplatform%2Fsuperproject%2Fmain
		// If we have vendor_kernel_boot partition, we migrate dtb image to that image
		// and allow dtb in vendor_boot to be empty.
		imgType = "vendor_kernel_boot"
	}
	if partitionVars.BoardPrebuiltDtbDir != "" {
		// https://cs.android.com/android/platform/superproject/main/+/main:build/make/core/Makefile;l=1019-1022?q=BOARD_PREBUILT_DTBIMAGE_DIR&ss=android%2Fplatform%2Fsuperproject%2Fmaini
		ctx.CreateModuleInDirectory(
			genrule.GenRuleFactory,
			".",
			&struct {
				Name *string
				Srcs []string
				Out  []string
				Cmd  *string
			}{
				Name: proptools.StringPtr(moduleName),
				Srcs: []string{fmt.Sprintf("%s/*.dtb", partitionVars.BoardPrebuiltDtbDir)},
				Out:  []string{"dtb.img"},
				Cmd:  proptools.StringPtr("cat $(in) > $(out)"),
			},
		)
		return dtbImg{include: true, name: moduleName, imgType: imgType}
	}
	for _, copyFilePair := range partitionVars.ProductCopyFiles {
		srcDestList := strings.Split(copyFilePair, ":")
		if len(srcDestList) < 2 {
			ctx.ModuleErrorf("PRODUCT_COPY_FILES must follow the format \"src:dest\", got: %s", copyFilePair)
		}
		if srcDestList[1] == "dtb.img" {
			ctx.CreateModuleInDirectory(
				android.FileGroupFactory,
				filepath.Dir(srcDestList[0]),
				&struct {
					Name *string
					Srcs []string
				}{
					Name: proptools.StringPtr(moduleName),
					Srcs: []string{filepath.Base(srcDestList[1])},
				},
			)
			return dtbImg{include: true, name: moduleName, imgType: imgType}
		}
	}
	return dtbImg{include: false}
}

func getVendorBootConfigImgName(ctx android.LoadHookContext) string {
	partitionVars := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse
	bootconfig := partitionVars.InternalBootconfig
	bootconfigFile := partitionVars.InternalBootconfigFile
	if len(bootconfig) == 0 && len(bootconfigFile) == 0 {
		return ""
	}

	return generatedModuleName(ctx.Config(), "vendor_bootconfig_image")
}

func createVendorBootConfigImg(ctx android.LoadHookContext) (string, bool) {
	vendorBootconfigImgModuleName := getVendorBootConfigImgName(ctx)
	if vendorBootconfigImgModuleName == "" {
		return "", false
	}
	partitionVars := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse
	bootconfig := partitionVars.InternalBootconfig
	bootconfigFile := partitionVars.InternalBootconfigFile
	ctx.CreateModule(
		filesystem.BootconfigModuleFactory,
		&struct {
			Name             *string
			Boot_config      []string
			Boot_config_file *string
		}{
			Name:             proptools.StringPtr(vendorBootconfigImgModuleName),
			Boot_config:      bootconfig,
			Boot_config_file: proptools.StringPtr(bootconfigFile),
		},
	)

	return vendorBootconfigImgModuleName, true
}

func createPrebuiltDtboImages(ctx android.LoadHookContext) (string, string) {
	partitionVars := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse

	dtboModuleName := getDtboModuleName(ctx)
	dtbo16kModuleName := getDtbo16kModuleName(ctx)

	if dtboModuleName != "" {
		size, _ := strconv.ParseInt(partitionVars.BoardDtboPartitionSize, 0, 64)
		ctx.CreateModuleInDirectory(
			filesystem.PrebuiltDtboImgFactory,
			".",
			&struct {
				Name           *string
				Src            *string
				Partition_size *int64
			}{
				Name:           proptools.StringPtr(dtboModuleName),
				Src:            proptools.StringPtr(partitionVars.BoardPrebuiltDtboImage),
				Partition_size: proptools.Int64Ptr(size),
			},
		)
	}

	if dtbo16kModuleName != "" {
		size, _ := strconv.ParseInt(partitionVars.BoardDtboPartitionSize, 0, 64)
		ctx.CreateModuleInDirectory(
			filesystem.PrebuiltDtboImgFactory,
			".",
			&struct {
				Name           *string
				Src            *string
				Partition_size *int64
				Stem           *string
			}{
				Name:           proptools.StringPtr(dtbo16kModuleName),
				Src:            proptools.StringPtr(partitionVars.BoardPrebuiltDtboImage16kb),
				Partition_size: proptools.Int64Ptr(size),
				Stem:           proptools.StringPtr("dtbo_16k.img"),
			},
		)
	}

	return dtboModuleName, dtbo16kModuleName
}

func getDtboModuleName(ctx android.LoadHookContext) string {
	partitionVars := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse
	if partitionVars.BoardPrebuiltDtboImage != "" {
		file := android.ExistentPathForSource(ctx, partitionVars.BoardPrebuiltDtboImage)
		if file.Valid() {
			return generatedModuleNameForPartition(ctx.Config(), "dtbo")
		}
	}
	return ""
}

func getDtbo16kModuleName(ctx android.LoadHookContext) string {
	partitionVars := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse
	if partitionVars.BoardPrebuiltDtboImage16kb != "" {
		return generatedModuleNameForPartition(ctx.Config(), "dtbo_16k")
	}
	return ""
}

func createBootOtas16kModules(ctx android.LoadHookContext, dtboModuleName, dtbo16kModuleName string) string {
	partitionVars := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse
	if partitionVars.BoardKernelPath16k == "" {
		return ""
	}

	name := generatedModuleName(ctx.Config(), "boot_otas_16k")
	props := filesystem.BootOtas16kProperties{
		Boot_image:          proptools.StringPtr(":" + generatedModuleName(ctx.Config(), "boot_image")),
		Boot_image_16k:      proptools.StringPtr(":" + generatedModuleName(ctx.Config(), "boot_16k_image")),
		Use_ota_incremental: proptools.BoolPtr(partitionVars.Board16kOtaUseIncremental),
	}
	if dtboModuleName != "" {
		props.Dtbo_image = proptools.StringPtr(":" + dtboModuleName)
	}
	if dtbo16kModuleName != "" {
		props.Dtbo_image_16k = proptools.StringPtr(":" + dtbo16kModuleName)

	}
	ctx.CreateModuleInDirectory(
		filesystem.BootOtas16kFactory,
		".",
		&struct {
			Name   *string
			Vendor *bool
		}{
			Name:   proptools.StringPtr(name),
			Vendor: proptools.BoolPtr(true),
		},
		&props,
	)
	return name
}
