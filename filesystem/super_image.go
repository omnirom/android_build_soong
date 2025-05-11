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

package filesystem

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"android/soong/android"
	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"
)

func init() {
	android.RegisterModuleType("super_image", SuperImageFactory)
}

type superImage struct {
	android.ModuleBase

	properties     SuperImageProperties
	partitionProps SuperImagePartitionNameProperties

	installDir android.InstallPath
}

type SuperImageProperties struct {
	// the size of the super partition
	Size *int64
	// the block device where metadata for dynamic partitions is stored
	Metadata_device *string
	// the super partition block device list
	Block_devices []string
	// whether A/B updater is used
	Ab_update *bool
	// whether dynamic partitions is enabled on devices that were launched without this support
	Retrofit *bool
	// whether virtual A/B seamless update is enabled
	Virtual_ab *bool
	// whether retrofitting virtual A/B seamless update is enabled
	Virtual_ab_retrofit *bool
	// whether the output is a sparse image
	Sparse *bool
	// information about how partitions within the super partition are grouped together
	Partition_groups []PartitionGroupsInfo
	// whether dynamic partitions is used
	Use_dynamic_partitions *bool
}

type PartitionGroupsInfo struct {
	Name          string
	GroupSize     string
	PartitionList []string
}

type SuperImagePartitionNameProperties struct {
	// Name of the System partition filesystem module
	System_partition *string
	// Name of the System_ext partition filesystem module
	System_ext_partition *string
	// Name of the System_dlkm partition filesystem module
	System_dlkm_partition *string
	// Name of the System_other partition filesystem module
	System_other_partition *string
	// Name of the Product partition filesystem module
	Product_partition *string
	// Name of the Vendor partition filesystem module
	Vendor_partition *string
	// Name of the Vendor_dlkm partition filesystem module
	Vendor_dlkm_partition *string
	// Name of the Odm partition filesystem module
	Odm_partition *string
	// Name of the Odm_dlkm partition filesystem module
	Odm_dlkm_partition *string
}

func SuperImageFactory() android.Module {
	module := &superImage{}
	module.AddProperties(&module.properties, &module.partitionProps)
	android.InitAndroidArchModule(module, android.DeviceSupported, android.MultilibCommon)
	return module
}

type superImageDepTagType struct {
	blueprint.BaseDependencyTag
}

var superImageDepTag superImageDepTagType

func (s *superImage) DepsMutator(ctx android.BottomUpMutatorContext) {
	addDependencyIfDefined := func(dep *string) {
		if dep != nil {
			ctx.AddDependency(ctx.Module(), superImageDepTag, proptools.String(dep))
		}
	}

	addDependencyIfDefined(s.partitionProps.System_partition)
	addDependencyIfDefined(s.partitionProps.System_ext_partition)
	addDependencyIfDefined(s.partitionProps.System_dlkm_partition)
	addDependencyIfDefined(s.partitionProps.System_other_partition)
	addDependencyIfDefined(s.partitionProps.Product_partition)
	addDependencyIfDefined(s.partitionProps.Vendor_partition)
	addDependencyIfDefined(s.partitionProps.Vendor_dlkm_partition)
	addDependencyIfDefined(s.partitionProps.Odm_partition)
	addDependencyIfDefined(s.partitionProps.Odm_dlkm_partition)
}

func (s *superImage) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	miscInfo, deps := s.buildMiscInfo(ctx)
	builder := android.NewRuleBuilder(pctx, ctx)
	output := android.PathForModuleOut(ctx, s.installFileName())
	lpMake := ctx.Config().HostToolPath(ctx, "lpmake")
	lpMakeDir := filepath.Dir(lpMake.String())
	deps = append(deps, lpMake)
	builder.Command().Textf("PATH=%s:\\$PATH", lpMakeDir).
		BuiltTool("build_super_image").
		Text("-v").
		Input(miscInfo).
		Implicits(deps).
		Output(output)
	builder.Build("build_super_image", fmt.Sprintf("Creating super image %s", s.BaseModuleName()))
	ctx.SetOutputFiles([]android.Path{output}, "")
}

func (s *superImage) installFileName() string {
	return s.BaseModuleName() + ".img"
}

func (s *superImage) buildMiscInfo(ctx android.ModuleContext) (android.Path, android.Paths) {
	var miscInfoString strings.Builder
	addStr := func(name string, value string) {
		miscInfoString.WriteString(name)
		miscInfoString.WriteRune('=')
		miscInfoString.WriteString(value)
		miscInfoString.WriteRune('\n')
	}

	addStr("use_dynamic_partitions", strconv.FormatBool(proptools.Bool(s.properties.Use_dynamic_partitions)))
	addStr("dynamic_partition_retrofit", strconv.FormatBool(proptools.Bool(s.properties.Retrofit)))
	addStr("lpmake", "lpmake")
	addStr("super_metadata_device", proptools.String(s.properties.Metadata_device))
	if len(s.properties.Block_devices) > 0 {
		addStr("super_block_devices", strings.Join(s.properties.Block_devices, " "))
	}
	addStr("super_super_device_size", strconv.Itoa(proptools.Int(s.properties.Size)))
	var groups, partitionList []string
	for _, groupInfo := range s.properties.Partition_groups {
		groups = append(groups, groupInfo.Name)
		partitionList = append(partitionList, groupInfo.PartitionList...)
		addStr("super_"+groupInfo.Name+"_group_size", groupInfo.GroupSize)
		addStr("super_"+groupInfo.Name+"_partition_list", strings.Join(groupInfo.PartitionList, " "))
	}
	addStr("super_partition_groups", strings.Join(groups, " "))
	addStr("dynamic_partition_list", strings.Join(partitionList, " "))

	addStr("virtual_ab", strconv.FormatBool(proptools.Bool(s.properties.Virtual_ab)))
	addStr("virtual_ab_retrofit", strconv.FormatBool(proptools.Bool(s.properties.Virtual_ab_retrofit)))
	addStr("ab_update", strconv.FormatBool(proptools.Bool(s.properties.Ab_update)))
	addStr("build_non_sparse_super_partition", strconv.FormatBool(!proptools.Bool(s.properties.Sparse)))

	partitionToImagePath := make(map[string]string)
	nameToPartition := make(map[string]string)
	var systemOtherPartitionNameNeeded string
	addEntryToPartitionToName := func(p string, s *string) {
		if proptools.String(s) != "" {
			nameToPartition[*s] = p
		}
	}

	// Build partitionToImagePath, because system partition may need system_other
	// partition image path
	for _, p := range partitionList {
		if _, ok := nameToPartition[p]; ok {
			continue
		}
		switch p {
		case "system":
			addEntryToPartitionToName(p, s.partitionProps.System_partition)
			systemOtherPartitionNameNeeded = proptools.String(s.partitionProps.System_other_partition)
		case "system_dlkm":
			addEntryToPartitionToName(p, s.partitionProps.System_dlkm_partition)
		case "system_ext":
			addEntryToPartitionToName(p, s.partitionProps.System_ext_partition)
		case "product":
			addEntryToPartitionToName(p, s.partitionProps.Product_partition)
		case "vendor":
			addEntryToPartitionToName(p, s.partitionProps.Vendor_partition)
		case "vendor_dlkm":
			addEntryToPartitionToName(p, s.partitionProps.Vendor_dlkm_partition)
		case "odm":
			addEntryToPartitionToName(p, s.partitionProps.Odm_partition)
		case "odm_dlkm":
			addEntryToPartitionToName(p, s.partitionProps.Odm_dlkm_partition)
		default:
			ctx.ModuleErrorf("current partition %s not a super image supported partition", p)
		}
	}

	var deps android.Paths
	ctx.VisitDirectDeps(func(m android.Module) {
		if p, ok := nameToPartition[m.Name()]; ok {
			if output, ok := android.OtherModuleProvider(ctx, m, android.OutputFilesProvider); ok {
				partitionToImagePath[p] = output.DefaultOutputFiles[0].String()
				deps = append(deps, output.DefaultOutputFiles[0])
			}
		} else if systemOtherPartitionNameNeeded != "" && m.Name() == systemOtherPartitionNameNeeded {
			if output, ok := android.OtherModuleProvider(ctx, m, android.OutputFilesProvider); ok {
				partitionToImagePath["system_other"] = output.DefaultOutputFiles[0].String()
				// TODO: add system_other to deps after it can be generated
				// deps = append(deps, output.DefaultOutputFiles[0])
			}
		}
	})

	for _, p := range android.SortedKeys(partitionToImagePath) {
		addStr(p+"_image", partitionToImagePath[p])
	}

	miscInfo := android.PathForModuleOut(ctx, "misc_info.txt")
	android.WriteFileRule(ctx, miscInfo, miscInfoString.String())
	return miscInfo, deps
}
