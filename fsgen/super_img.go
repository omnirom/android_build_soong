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
	"strconv"

	"android/soong/android"
	"android/soong/filesystem"

	"github.com/google/blueprint/proptools"
)

func buildingSuperImage(partitionVars android.PartitionVariables) bool {
	return partitionVars.ProductBuildSuperPartition
}

func createSuperImage(
	ctx android.LoadHookContext,
	partitions allGeneratedPartitionData,
	partitionVars android.PartitionVariables,
	systemOtherImageName string,
) []string {
	baseProps := &struct {
		Name *string
	}{
		Name: proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), "super")),
	}

	superImageProps := &filesystem.SuperImageProperties{
		Metadata_device:               proptools.StringPtr(partitionVars.BoardSuperPartitionMetadataDevice),
		Block_devices:                 partitionVars.BoardSuperPartitionBlockDevices,
		Ab_update:                     proptools.BoolPtr(partitionVars.AbOtaUpdater),
		Retrofit:                      proptools.BoolPtr(partitionVars.ProductRetrofitDynamicPartitions),
		Use_dynamic_partitions:        proptools.BoolPtr(partitionVars.ProductUseDynamicPartitions),
		Super_image_in_update_package: proptools.BoolPtr(partitionVars.BoardSuperImageInUpdatePackage),
		Create_super_empty:            proptools.BoolPtr(partitionVars.BuildingSuperEmptyImage),
	}
	if partitionVars.ProductVirtualAbOta {
		superImageProps.Virtual_ab.Enable = proptools.BoolPtr(true)
		superImageProps.Virtual_ab.Retrofit = proptools.BoolPtr(partitionVars.ProductVirtualAbOtaRetrofit)
		superImageProps.Virtual_ab.Compression = proptools.BoolPtr(partitionVars.ProductVirtualAbCompression)
		if partitionVars.ProductVirtualAbCompressionMethod != "" {
			superImageProps.Virtual_ab.Compression_method = proptools.StringPtr(partitionVars.ProductVirtualAbCompressionMethod)
		}
		if partitionVars.ProductVirtualAbCompressionFactor != "" {
			factor, err := strconv.ParseInt(partitionVars.ProductVirtualAbCompressionFactor, 10, 32)
			if err != nil {
				ctx.ModuleErrorf("Compression factor must be an int, got %q", partitionVars.ProductVirtualAbCompressionFactor)
			}
			superImageProps.Virtual_ab.Compression_factor = proptools.Int64Ptr(factor)
		}
		if partitionVars.ProductVirtualAbCowVersion != "" {
			version, err := strconv.ParseInt(partitionVars.ProductVirtualAbCowVersion, 10, 32)
			if err != nil {
				ctx.ModuleErrorf("COW version must be an int, got %q", partitionVars.ProductVirtualAbCowVersion)
			}
			superImageProps.Virtual_ab.Cow_version = proptools.Int64Ptr(version)
		}
	}
	size, _ := strconv.ParseInt(partitionVars.BoardSuperPartitionSize, 10, 64)
	superImageProps.Size = proptools.Int64Ptr(size)
	sparse := !partitionVars.TargetUserimagesSparseExtDisabled && !partitionVars.TargetUserimagesSparseF2fsDisabled
	superImageProps.Sparse = proptools.BoolPtr(sparse)

	var partitionGroupsInfo []filesystem.PartitionGroupsInfo
	for _, groupName := range android.SortedKeys(partitionVars.BoardSuperPartitionGroups) {
		info := filesystem.PartitionGroupsInfo{
			Name:          groupName,
			GroupSize:     partitionVars.BoardSuperPartitionGroups[groupName].GroupSize,
			PartitionList: partitionVars.BoardSuperPartitionGroups[groupName].PartitionList,
		}
		partitionGroupsInfo = append(partitionGroupsInfo, info)
	}
	superImageProps.Partition_groups = partitionGroupsInfo

	if systemOtherImageName != "" {
		superImageProps.System_other_partition = proptools.StringPtr(systemOtherImageName)
	}

	var superImageSubpartitions []string
	partitionNameProps := &filesystem.SuperImagePartitionNameProperties{}
	if modName := partitions.nameForType("system"); modName != "" {
		partitionNameProps.System_partition = proptools.StringPtr(modName)
		superImageSubpartitions = append(superImageSubpartitions, "system")
	}
	if modName := partitions.nameForType("system_ext"); modName != "" {
		partitionNameProps.System_ext_partition = proptools.StringPtr(modName)
		superImageSubpartitions = append(superImageSubpartitions, "system_ext")
	}
	if modName := partitions.nameForType("system_dlkm"); modName != "" {
		partitionNameProps.System_dlkm_partition = proptools.StringPtr(modName)
		superImageSubpartitions = append(superImageSubpartitions, "system_dlkm")
	}
	if modName := partitions.nameForType("system_other"); modName != "" {
		partitionNameProps.System_other_partition = proptools.StringPtr(modName)
		superImageSubpartitions = append(superImageSubpartitions, "system_other")
	}
	if modName := partitions.nameForType("product"); modName != "" {
		partitionNameProps.Product_partition = proptools.StringPtr(modName)
		superImageSubpartitions = append(superImageSubpartitions, "product")
	}
	if modName := partitions.nameForType("vendor"); modName != "" {
		partitionNameProps.Vendor_partition = proptools.StringPtr(modName)
		superImageSubpartitions = append(superImageSubpartitions, "vendor")
	}
	if modName := partitions.nameForType("vendor_dlkm"); modName != "" {
		partitionNameProps.Vendor_dlkm_partition = proptools.StringPtr(modName)
		superImageSubpartitions = append(superImageSubpartitions, "vendor_dlkm")
	}
	if modName := partitions.nameForType("odm"); modName != "" {
		partitionNameProps.Odm_partition = proptools.StringPtr(modName)
		superImageSubpartitions = append(superImageSubpartitions, "odm")
	}
	if modName := partitions.nameForType("odm_dlkm"); modName != "" {
		partitionNameProps.Odm_dlkm_partition = proptools.StringPtr(modName)
		superImageSubpartitions = append(superImageSubpartitions, "odm_dlkm")
	}

	ctx.CreateModule(filesystem.SuperImageFactory, baseProps, superImageProps, partitionNameProps)
	return superImageSubpartitions
}
