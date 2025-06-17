package fsgen

import (
	"android/soong/android"
	"android/soong/filesystem"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/blueprint/proptools"
)

func createBootImage(ctx android.LoadHookContext, dtbImg dtbImg) bool {
	partitionVariables := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse

	if partitionVariables.TargetKernelPath == "" {
		// There are potentially code paths that don't set TARGET_KERNEL_PATH
		return false
	}

	kernelDir := filepath.Dir(partitionVariables.TargetKernelPath)
	kernelBase := filepath.Base(partitionVariables.TargetKernelPath)
	kernelFilegroupName := generatedModuleName(ctx.Config(), "kernel")

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

	var partitionSize *int64
	if partitionVariables.BoardBootimagePartitionSize != "" {
		// Base of zero will allow base 10 or base 16 if starting with 0x
		parsed, err := strconv.ParseInt(partitionVariables.BoardBootimagePartitionSize, 0, 64)
		if err != nil {
			panic(fmt.Sprintf("BOARD_BOOTIMAGE_PARTITION_SIZE must be an int, got %s", partitionVariables.BoardBootimagePartitionSize))
		}
		partitionSize = &parsed
	}

	var securityPatch *string
	if partitionVariables.BootSecurityPatch != "" {
		securityPatch = &partitionVariables.BootSecurityPatch
	}

	avbInfo := getAvbInfo(ctx.Config(), "boot")

	bootImageName := generatedModuleNameForPartition(ctx.Config(), "boot")

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
			Boot_image_type:             proptools.StringPtr("boot"),
			Kernel_prebuilt:             proptools.StringPtr(":" + kernelFilegroupName),
			Header_version:              proptools.StringPtr(partitionVariables.BoardBootHeaderVersion),
			Partition_size:              partitionSize,
			Use_avb:                     avbInfo.avbEnable,
			Avb_mode:                    avbInfo.avbMode,
			Avb_private_key:             avbInfo.avbkeyFilegroup,
			Avb_rollback_index:          avbInfo.avbRollbackIndex,
			Avb_rollback_index_location: avbInfo.avbRollbackIndexLocation,
			Avb_algorithm:               avbInfo.avbAlgorithm,
			Security_patch:              securityPatch,
			Dtb_prebuilt:                dtbPrebuilt,
			Cmdline:                     cmdline,
			Stem:                        proptools.StringPtr("boot.img"),
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

	ctx.CreateModule(
		filesystem.BootimgFactory,
		&filesystem.BootimgProperties{
			Boot_image_type:             proptools.StringPtr("vendor_boot"),
			Ramdisk_module:              proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), "vendor_ramdisk")),
			Header_version:              proptools.StringPtr(partitionVariables.BoardBootHeaderVersion),
			Partition_size:              partitionSize,
			Use_avb:                     avbInfo.avbEnable,
			Avb_mode:                    avbInfo.avbMode,
			Avb_private_key:             avbInfo.avbkeyFilegroup,
			Avb_rollback_index:          avbInfo.avbRollbackIndex,
			Avb_rollback_index_location: avbInfo.avbRollbackIndexLocation,
			Dtb_prebuilt:                dtbPrebuilt,
			Cmdline:                     cmdline,
			Bootconfig:                  vendorBootConfigImg,
			Stem:                        proptools.StringPtr("vendor_boot.img"),
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
			Boot_image_type:             proptools.StringPtr("init_boot"),
			Ramdisk_module:              proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), "ramdisk")),
			Header_version:              proptools.StringPtr(partitionVariables.BoardBootHeaderVersion),
			Security_patch:              securityPatch,
			Partition_size:              partitionSize,
			Use_avb:                     avbInfo.avbEnable,
			Avb_mode:                    avbInfo.avbMode,
			Avb_private_key:             avbInfo.avbkeyFilegroup,
			Avb_rollback_index:          avbInfo.avbRollbackIndex,
			Avb_rollback_index_location: avbInfo.avbRollbackIndexLocation,
			Avb_algorithm:               avbInfo.avbAlgorithm,
			Stem:                        proptools.StringPtr("init_boot.img"),
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
	for _, copyFilePair := range partitionVars.ProductCopyFiles {
		srcDestList := strings.Split(copyFilePair, ":")
		if len(srcDestList) < 2 {
			ctx.ModuleErrorf("PRODUCT_COPY_FILES must follow the format \"src:dest\", got: %s", copyFilePair)
		}
		if srcDestList[1] == "dtb.img" {
			moduleName := generatedModuleName(ctx.Config(), "dtb_img_filegroup")
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
			imgType := "vendor_boot"
			if !buildingVendorBootImage(partitionVars) {
				imgType = "boot"
			}
			return dtbImg{include: true, name: moduleName, imgType: imgType}
		}
	}
	return dtbImg{include: false}
}

func createVendorBootConfigImg(ctx android.LoadHookContext) (string, bool) {
	partitionVars := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse
	bootconfig := partitionVars.InternalBootconfig
	bootconfigFile := partitionVars.InternalBootconfigFile
	if len(bootconfig) == 0 && len(bootconfigFile) == 0 {
		return "", false
	}

	vendorBootconfigImgModuleName := generatedModuleName(ctx.Config(), "vendor_bootconfig_image")

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
