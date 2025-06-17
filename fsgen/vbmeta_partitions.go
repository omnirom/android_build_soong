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
	"android/soong/filesystem"
	"strconv"

	"github.com/google/blueprint/proptools"
)

type vbmetaModuleInfo struct {
	// The name of the generated vbmeta module
	moduleName string
	// The name of the module that avb understands. This is the name passed to --chain_partition,
	// and also the basename of the output file. (the output file is called partitionName + ".img")
	partitionName string
}

// https://cs.android.com/android/platform/superproject/main/+/main:build/make/core/Makefile;l=4849;drc=62e20f0d218f60bae563b4ee742d88cca1fc1901
var avbPartitions = []string{
	"boot",
	"init_boot",
	"vendor_boot",
	"vendor_kernel_boot",
	"system",
	"vendor",
	"product",
	"system_ext",
	"odm",
	"vendor_dlkm",
	"odm_dlkm",
	"system_dlkm",
	"dtbo",
	"pvmfw",
	"recovery",
	"vbmeta_system",
	"vbmeta_vendor",
}

// Creates the vbmeta partition and the chained vbmeta partitions. Returns the list of module names
// that the function created. May return nil if the product isn't using avb.
//
// AVB is Android Verified Boot: https://source.android.com/docs/security/features/verifiedboot
// It works by signing all the partitions, but then also including an extra metadata paritition
// called vbmeta that depends on all the other signed partitions. This creates a requirement
// that you update all those partitions and the vbmeta partition together, so in order to relax
// that requirement products can set up "chained" vbmeta partitions, where a chained partition
// like vbmeta_system might contain the avb metadata for just a few products. In cuttlefish
// vbmeta_system contains metadata about product, system, and system_ext. Using chained partitions,
// that group of partitions can be updated independently from the other signed partitions.
func (f *filesystemCreator) createVbmetaPartitions(ctx android.LoadHookContext, partitions allGeneratedPartitionData) []vbmetaModuleInfo {
	partitionVars := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse
	// Some products seem to have BuildingVbmetaImage as true even when BoardAvbEnable is false
	if !partitionVars.BuildingVbmetaImage || !partitionVars.BoardAvbEnable {
		return nil
	}

	var result []vbmetaModuleInfo

	// https://cs.android.com/android/platform/superproject/main/+/main:build/make/core/Makefile;l=4593;drc=62e20f0d218f60bae563b4ee742d88cca1fc1901
	var internalAvbPartitionsInChainedVbmetaImages []string
	var chainedPartitionTypes []string
	for _, chainedName := range android.SortedKeys(partitionVars.ChainedVbmetaPartitions) {
		props := partitionVars.ChainedVbmetaPartitions[chainedName]
		filesystemPartitionType := chainedName
		chainedName = "vbmeta_" + chainedName
		if len(props.Partitions) == 0 {
			continue
		}
		internalAvbPartitionsInChainedVbmetaImages = append(internalAvbPartitionsInChainedVbmetaImages, props.Partitions...)
		if len(props.Key) == 0 {
			ctx.ModuleErrorf("No key found for chained avb partition %q", chainedName)
			continue
		}
		if len(props.Algorithm) == 0 {
			ctx.ModuleErrorf("No algorithm found for chained avb partition %q", chainedName)
			continue
		}
		if len(props.RollbackIndex) == 0 {
			ctx.ModuleErrorf("No rollback index found for chained avb partition %q", chainedName)
			continue
		}
		ril, err := strconv.ParseInt(props.RollbackIndexLocation, 10, 32)
		if err != nil {
			ctx.ModuleErrorf("Rollback index location must be an int, got %q", props.RollbackIndexLocation)
			continue
		}
		// The default is to use the PlatformSecurityPatch, and a lot of product config files
		// just set it to the platform security patch, so detect that and don't set the property
		// in soong.
		var rollbackIndex *int64
		if props.RollbackIndex != ctx.Config().PlatformSecurityPatch() {
			i, err := strconv.ParseInt(props.RollbackIndex, 10, 32)
			if err != nil {
				ctx.ModuleErrorf("Rollback index must be an int, got %q", props.RollbackIndex)
				continue
			}
			rollbackIndex = &i
		}

		var partitionModules []string
		for _, partition := range props.Partitions {
			if modName := partitions.nameForType(partition); modName != "" {
				partitionModules = append(partitionModules, modName)
			}
		}

		name := generatedModuleNameForPartition(ctx.Config(), chainedName)
		ctx.CreateModuleInDirectory(
			filesystem.VbmetaFactory,
			".", // Create in the root directory for now so its easy to get the key
			&filesystem.VbmetaProperties{
				Partition_name:            proptools.StringPtr(chainedName),
				Filesystem_partition_type: proptools.StringPtr(filesystemPartitionType),
				Stem:                      proptools.StringPtr(chainedName + ".img"),
				Private_key:               proptools.StringPtr(props.Key),
				Algorithm:                 &props.Algorithm,
				Rollback_index:            rollbackIndex,
				Rollback_index_location:   &ril,
				Partitions:                proptools.NewSimpleConfigurable(partitionModules),
			}, &struct {
				Name *string
			}{
				Name: &name,
			},
		).HideFromMake()

		result = append(result, vbmetaModuleInfo{
			moduleName:    name,
			partitionName: chainedName,
		})

		chainedPartitionTypes = append(chainedPartitionTypes, chainedName)
	}

	vbmetaModuleName := generatedModuleNameForPartition(ctx.Config(), "vbmeta")

	var algorithm *string
	var ri *int64
	var key *string
	if len(partitionVars.BoardAvbKeyPath) == 0 {
		// Match make's defaults: https://cs.android.com/android/platform/superproject/main/+/main:build/make/core/Makefile;l=4568;drc=5b55f926830963c02ab1d2d91e46442f04ba3af0
		key = proptools.StringPtr("external/avb/test/data/testkey_rsa4096.pem")
		algorithm = proptools.StringPtr("SHA256_RSA4096")
	} else {
		key = proptools.StringPtr(partitionVars.BoardAvbKeyPath)
		algorithm = proptools.StringPtr(partitionVars.BoardAvbAlgorithm)
	}
	if len(partitionVars.BoardAvbRollbackIndex) > 0 {
		parsedRi, err := strconv.ParseInt(partitionVars.BoardAvbRollbackIndex, 10, 32)
		if err != nil {
			ctx.ModuleErrorf("Rollback index location must be an int, got %q", partitionVars.BoardAvbRollbackIndex)
		}
		ri = &parsedRi
	}

	// --chain_partition argument is only set for partitions that set
	// `BOARD_AVB_<partition name>_KEY_PATH` value and is not "recovery"
	// https://cs.android.com/android/platform/superproject/main/+/main:build/make/core/Makefile;l=4823;drc=62e20f0d218f60bae563b4ee742d88cca1fc1901
	includeAsChainedPartitionInVbmeta := func(partition string) bool {
		val, ok := partitionVars.PartitionQualifiedVariables[partition]
		return ok && len(val.BoardAvbKeyPath) > 0 && partition != "recovery"
	}

	// --include_descriptors_from_image is passed if both conditions are met:
	// - `BOARD_AVB_<partition name>_KEY_PATH` value is not set
	// - not included in INTERNAL_AVB_PARTITIONS_IN_CHAINED_VBMETA_IMAGES
	// for partitions that set INSTALLED_<partition name>IMAGE_TARGET
	// https://cs.android.com/android/platform/superproject/main/+/main:build/make/core/Makefile;l=4827;drc=62e20f0d218f60bae563b4ee742d88cca1fc1901
	includeAsIncludedPartitionInVbmeta := func(partition string) bool {
		if android.InList(partition, internalAvbPartitionsInChainedVbmetaImages) {
			// Already handled by chained vbmeta partitions
			return false
		}
		partitionQualifiedVars := partitionVars.PartitionQualifiedVariables[partition]

		// The return logic in the switch cases below are identical to
		// ifdef INSTALLED_<partition name>IMAGE_TARGET
		switch partition {
		case "boot":
			return partitionQualifiedVars.BuildingImage || partitionQualifiedVars.PrebuiltImage || partitionVars.BoardUsesRecoveryAsBoot
		case "vendor_kernel_boot", "dtbo":
			return partitionQualifiedVars.PrebuiltImage
		case "system":
			return partitionQualifiedVars.BuildingImage
		case "init_boot", "vendor_boot", "vendor", "product", "system_ext", "odm", "vendor_dlkm", "odm_dlkm", "system_dlkm":
			return partitionQualifiedVars.BuildingImage || partitionQualifiedVars.PrebuiltImage
		// TODO: Import BOARD_USES_PVMFWIMAGE
		// ifeq ($(BOARD_USES_PVMFWIMAGE),true)
		// case "pvmfw":
		case "recovery":
			// ifdef INSTALLED_RECOVERYIMAGE_TARGET
			return !ctx.DeviceConfig().BoardUsesRecoveryAsBoot() && !ctx.DeviceConfig().BoardMoveRecoveryResourcesToVendorBoot()
		// Technically these partitions are determined based on len(BOARD_AVB_VBMETA_SYSTEM) and
		// len(BOARD_AVB_VBMETA_VENDOR) but if these are non empty these partitions are
		// already included in the chained partitions.
		case "vbmeta_system", "vbmeta_vendor":
			return false
		default:
			return false
		}
	}

	var chainedPartitionModules []string
	var includePartitionModules []string
	allGeneratedPartitionTypes := append(partitions.types(),
		chainedPartitionTypes...,
	)
	if len(f.properties.Boot_image) > 0 {
		allGeneratedPartitionTypes = append(allGeneratedPartitionTypes, "boot")
	}
	if len(f.properties.Init_boot_image) > 0 {
		allGeneratedPartitionTypes = append(allGeneratedPartitionTypes, "init_boot")
	}
	if len(f.properties.Vendor_boot_image) > 0 {
		allGeneratedPartitionTypes = append(allGeneratedPartitionTypes, "vendor_boot")
	}

	// https://cs.android.com/android/platform/superproject/main/+/main:build/make/core/Makefile;l=4919;drc=62e20f0d218f60bae563b4ee742d88cca1fc1901
	for _, partitionType := range android.SortedUniqueStrings(append(avbPartitions, chainedPartitionTypes...)) {
		if !android.InList(partitionType, allGeneratedPartitionTypes) {
			// Skip if the partition is not auto generated
			continue
		}
		name := partitions.nameForType(partitionType)
		if name == "" {
			name = generatedModuleNameForPartition(ctx.Config(), partitionType)
		}
		if includeAsChainedPartitionInVbmeta(partitionType) {
			chainedPartitionModules = append(chainedPartitionModules, name)
		} else if includeAsIncludedPartitionInVbmeta(partitionType) {
			includePartitionModules = append(includePartitionModules, name)
		}
	}

	ctx.CreateModuleInDirectory(
		filesystem.VbmetaFactory,
		".", // Create in the root directory for now so its easy to get the key
		&filesystem.VbmetaProperties{
			Stem:               proptools.StringPtr("vbmeta.img"),
			Algorithm:          algorithm,
			Private_key:        key,
			Rollback_index:     ri,
			Chained_partitions: chainedPartitionModules,
			Partitions:         proptools.NewSimpleConfigurable(includePartitionModules),
			Partition_name:     proptools.StringPtr("vbmeta"),
		}, &struct {
			Name *string
		}{
			Name: &vbmetaModuleName,
		},
	).HideFromMake()

	result = append(result, vbmetaModuleInfo{
		moduleName:    vbmetaModuleName,
		partitionName: "vbmeta",
	})
	return result
}
