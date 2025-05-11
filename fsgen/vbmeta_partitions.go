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
	"slices"
	"strconv"
	"strings"

	"github.com/google/blueprint/proptools"
)

type vbmetaModuleInfo struct {
	// The name of the generated vbmeta module
	moduleName string
	// The name of the module that avb understands. This is the name passed to --chain_partition,
	// and also the basename of the output file. (the output file is called partitionName + ".img")
	partitionName string
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
func createVbmetaPartitions(ctx android.LoadHookContext, generatedPartitionTypes []string) []vbmetaModuleInfo {
	partitionVars := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse
	// Some products seem to have BuildingVbmetaImage as true even when BoardAvbEnable is false
	if !partitionVars.BuildingVbmetaImage || !partitionVars.BoardAvbEnable {
		return nil
	}

	var result []vbmetaModuleInfo

	var chainedPartitions []string
	var partitionTypesHandledByChainedPartitions []string
	for _, chainedName := range android.SortedKeys(partitionVars.ChainedVbmetaPartitions) {
		props := partitionVars.ChainedVbmetaPartitions[chainedName]
		chainedName = "vbmeta_" + chainedName
		if len(props.Partitions) == 0 {
			continue
		}
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
			partitionTypesHandledByChainedPartitions = append(partitionTypesHandledByChainedPartitions, partition)
			if !slices.Contains(generatedPartitionTypes, partition) {
				// The partition is probably unsupported.
				continue
			}
			partitionModules = append(partitionModules, generatedModuleNameForPartition(ctx.Config(), partition))
		}

		name := generatedModuleName(ctx.Config(), chainedName)
		ctx.CreateModuleInDirectory(
			filesystem.VbmetaFactory,
			".", // Create in the root directory for now so its easy to get the key
			&filesystem.VbmetaProperties{
				Partition_name:          proptools.StringPtr(chainedName),
				Stem:                    proptools.StringPtr(chainedName + ".img"),
				Private_key:             proptools.StringPtr(props.Key),
				Algorithm:               &props.Algorithm,
				Rollback_index:          rollbackIndex,
				Rollback_index_location: &ril,
				Partitions:              proptools.NewSimpleConfigurable(partitionModules),
			}, &struct {
				Name *string
			}{
				Name: &name,
			},
		).HideFromMake()

		chainedPartitions = append(chainedPartitions, name)

		result = append(result, vbmetaModuleInfo{
			moduleName:    name,
			partitionName: chainedName,
		})
	}

	vbmetaModuleName := generatedModuleName(ctx.Config(), "vbmeta")

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

	var partitionModules []string
	for _, partitionType := range generatedPartitionTypes {
		if slices.Contains(partitionTypesHandledByChainedPartitions, partitionType) {
			// Already handled by a chained vbmeta partition
			continue
		}
		if strings.Contains(partitionType, "ramdisk") || strings.Contains(partitionType, "boot") {
			// ramdisk is never signed with avb information
			// boot partitions just have the avb footer, and don't have a corresponding vbmeta
			// partition.
			continue
		}
		partitionModules = append(partitionModules, generatedModuleNameForPartition(ctx.Config(), partitionType))
	}

	ctx.CreateModuleInDirectory(
		filesystem.VbmetaFactory,
		".", // Create in the root directory for now so its easy to get the key
		&filesystem.VbmetaProperties{
			Stem:               proptools.StringPtr("vbmeta.img"),
			Algorithm:          algorithm,
			Private_key:        key,
			Rollback_index:     ri,
			Chained_partitions: chainedPartitions,
			Partitions:         proptools.NewSimpleConfigurable(partitionModules),
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
