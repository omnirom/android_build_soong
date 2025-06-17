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
	"regexp"
	"slices"
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
	// whether the output is a sparse image
	Sparse *bool
	// information about how partitions within the super partition are grouped together
	Partition_groups []PartitionGroupsInfo
	// Name of the system_other partition filesystem module. This module will be installed to
	// the "b" slot of the system partition in a/b partition builds.
	System_other_partition *string
	// whether dynamic partitions is used
	Use_dynamic_partitions *bool
	Virtual_ab             struct {
		// whether virtual A/B seamless update is enabled
		Enable *bool
		// whether retrofitting virtual A/B seamless update is enabled
		Retrofit *bool
		// If set, device uses virtual A/B Compression
		Compression *bool
		// This value controls the compression algorithm used for VABC.
		// Valid options are defined in system/core/fs_mgr/libsnapshot/cow_writer.cpp
		// e.g. "none", "gz", "brotli"
		Compression_method *string
		// Specifies maximum bytes to be compressed at once during ota. Options: 4096, 8192, 16384, 32768, 65536, 131072, 262144.
		Compression_factor *int64
		// Specifies COW version to be used by update_engine and libsnapshot. If this value is not
		// specified we default to COW version 2 in update_engine for backwards compatibility
		Cow_version *int64
	}
	// Whether the super image will be disted in the update package
	Super_image_in_update_package *bool
	// Whether a super_empty.img should be created
	Create_super_empty *bool
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

type SuperImageInfo struct {
	// The built super.img file, which contains the sub-partitions
	SuperImage android.Path

	// Mapping from the sub-partition type to its re-exported FileSystemInfo providers from the
	// sub-partitions.
	SubImageInfo map[string]FilesystemInfo

	DynamicPartitionsInfo android.Path

	SuperEmptyImage android.Path

	AbUpdate bool
}

var SuperImageProvider = blueprint.NewProvider[SuperImageInfo]()

func SuperImageFactory() android.Module {
	module := &superImage{}
	module.AddProperties(&module.properties, &module.partitionProps)
	android.InitAndroidArchModule(module, android.DeviceSupported, android.MultilibCommon)
	return module
}

type superImageDepTagType struct {
	blueprint.BaseDependencyTag
}

var subImageDepTag superImageDepTagType

type systemOtherDepTagType struct {
	blueprint.BaseDependencyTag
}

var systemOtherDepTag systemOtherDepTagType

func (s *superImage) DepsMutator(ctx android.BottomUpMutatorContext) {
	addDependencyIfDefined := func(dep *string) {
		if dep != nil {
			ctx.AddDependency(ctx.Module(), subImageDepTag, proptools.String(dep))
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
	if s.properties.System_other_partition != nil {
		ctx.AddDependency(ctx.Module(), systemOtherDepTag, *s.properties.System_other_partition)
	}
}

func (s *superImage) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	miscInfo, deps, subImageInfos := s.buildMiscInfo(ctx, false)
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
	var superEmptyImage android.WritablePath
	if proptools.Bool(s.properties.Create_super_empty) {
		superEmptyImageBuilder := android.NewRuleBuilder(pctx, ctx)
		superEmptyImage = android.PathForModuleOut(ctx, "super_empty.img")
		superEmptyMiscInfo, superEmptyDeps, _ := s.buildMiscInfo(ctx, true)
		if superEmptyDeps != nil {
			ctx.ModuleErrorf("TODO: Handle additional deps when building super_empty.img")
		}
		superEmptyImageBuilder.Command().Textf("PATH=%s:\\$PATH", lpMakeDir).
			BuiltTool("build_super_image").
			Text("-v").
			Input(superEmptyMiscInfo).
			Implicit(lpMake).
			Output(superEmptyImage)
		superEmptyImageBuilder.Build("build_super_empty_image", fmt.Sprintf("Creating super empty image %s", s.BaseModuleName()))
	}
	android.SetProvider(ctx, SuperImageProvider, SuperImageInfo{
		SuperImage:            output,
		SubImageInfo:          subImageInfos,
		DynamicPartitionsInfo: s.generateDynamicPartitionsInfo(ctx),
		SuperEmptyImage:       superEmptyImage,
		AbUpdate:              proptools.Bool(s.properties.Ab_update),
	})
	ctx.SetOutputFiles([]android.Path{output}, "")
	ctx.CheckbuildFile(output)

	buildComplianceMetadata(ctx, subImageDepTag)
}

func (s *superImage) installFileName() string {
	return "super.img"
}

func (s *superImage) buildMiscInfo(ctx android.ModuleContext, superEmpty bool) (android.Path, android.Paths, map[string]FilesystemInfo) {
	var miscInfoString strings.Builder
	partitionList := s.dumpDynamicPartitionInfo(ctx, &miscInfoString)
	addStr := func(name string, value string) {
		miscInfoString.WriteString(name)
		miscInfoString.WriteRune('=')
		miscInfoString.WriteString(value)
		miscInfoString.WriteRune('\n')
	}
	addStr("ab_update", strconv.FormatBool(proptools.Bool(s.properties.Ab_update)))
	if superEmpty {
		miscInfo := android.PathForModuleOut(ctx, "misc_info_super_empty.txt")
		android.WriteFileRule(ctx, miscInfo, miscInfoString.String())
		return miscInfo, nil, nil
	}

	subImageInfo := make(map[string]FilesystemInfo)
	var deps android.Paths

	missingPartitionErrorMessage := ""
	handleSubPartition := func(partitionType string, name *string) {
		if proptools.String(name) == "" {
			missingPartitionErrorMessage += fmt.Sprintf("%s image listed in partition groups, but its module was not specified. ", partitionType)
			return
		}
		mod := ctx.GetDirectDepWithTag(*name, subImageDepTag)
		if mod == nil {
			ctx.ModuleErrorf("Could not get dep %q", *name)
			return
		}
		info, ok := android.OtherModuleProvider(ctx, mod, FilesystemProvider)
		if !ok {
			ctx.ModuleErrorf("Expected dep %q to provide FilesystemInfo", *name)
			return
		}
		addStr(partitionType+"_image", info.Output.String())
		deps = append(deps, info.Output)
		if _, ok := subImageInfo[partitionType]; ok {
			ctx.ModuleErrorf("Already set subimageInfo for %q", partitionType)
		}
		subImageInfo[partitionType] = info
	}

	// Build partitionToImagePath, because system partition may need system_other
	// partition image path
	for _, p := range partitionList {
		switch p {
		case "system":
			handleSubPartition("system", s.partitionProps.System_partition)
		case "system_dlkm":
			handleSubPartition("system_dlkm", s.partitionProps.System_dlkm_partition)
		case "system_ext":
			handleSubPartition("system_ext", s.partitionProps.System_ext_partition)
		case "product":
			handleSubPartition("product", s.partitionProps.Product_partition)
		case "vendor":
			handleSubPartition("vendor", s.partitionProps.Vendor_partition)
		case "vendor_dlkm":
			handleSubPartition("vendor_dlkm", s.partitionProps.Vendor_dlkm_partition)
		case "odm":
			handleSubPartition("odm", s.partitionProps.Odm_partition)
		case "odm_dlkm":
			handleSubPartition("odm_dlkm", s.partitionProps.Odm_dlkm_partition)
		default:
			ctx.ModuleErrorf("partition %q is not a super image supported partition", p)
		}
	}

	if s.properties.System_other_partition != nil {
		if !slices.Contains(partitionList, "system") {
			ctx.PropertyErrorf("system_other_partition", "Must have a system partition to use a system_other partition")
		}
		systemOther := ctx.GetDirectDepProxyWithTag(*s.properties.System_other_partition, systemOtherDepTag)
		systemOtherFiles := android.OutputFilesForModule(ctx, systemOther, "")
		if len(systemOtherFiles) != 1 {
			ctx.PropertyErrorf("system_other_partition", "Expected 1 output file from module %q", *&s.properties.System_other_partition)
		} else {
			handleSubPartition("system_other", s.partitionProps.System_other_partition)
		}
	}

	// Delay the error message until execution time because on aosp-main-future-without-vendor,
	// BUILDING_VENDOR_IMAGE is false so we don't get the vendor image, but it's still listed in
	// BOARD_GOOGLE_DYNAMIC_PARTITIONS_PARTITION_LIST.
	missingPartitionErrorMessageFile := android.PathForModuleOut(ctx, "missing_partition_error.txt")
	if missingPartitionErrorMessage != "" {
		ctx.Build(pctx, android.BuildParams{
			Rule:   android.ErrorRule,
			Output: missingPartitionErrorMessageFile,
			Args: map[string]string{
				"error": missingPartitionErrorMessage,
			},
		})
	} else {
		ctx.Build(pctx, android.BuildParams{
			Rule:   android.Touch,
			Output: missingPartitionErrorMessageFile,
		})
	}

	miscInfo := android.PathForModuleOut(ctx, "misc_info.txt")
	android.WriteFileRule(ctx, miscInfo, miscInfoString.String(), missingPartitionErrorMessageFile)
	return miscInfo, deps, subImageInfo
}

func (s *superImage) dumpDynamicPartitionInfo(ctx android.ModuleContext, sb *strings.Builder) []string {
	addStr := func(name string, value string) {
		sb.WriteString(name)
		sb.WriteRune('=')
		sb.WriteString(value)
		sb.WriteRune('\n')
	}

	addStr("use_dynamic_partitions", strconv.FormatBool(proptools.Bool(s.properties.Use_dynamic_partitions)))
	if proptools.Bool(s.properties.Retrofit) {
		addStr("dynamic_partition_retrofit", "true")
	}
	addStr("lpmake", "lpmake")
	addStr("build_super_partition", "true")
	if proptools.Bool(s.properties.Create_super_empty) {
		addStr("build_super_empty_partition", "true")
	}
	addStr("super_metadata_device", proptools.String(s.properties.Metadata_device))
	if len(s.properties.Block_devices) > 0 {
		addStr("super_block_devices", strings.Join(s.properties.Block_devices, " "))
	}
	// TODO: In make, there's more complicated logic than just this surrounding super_*_device_size
	addStr("super_super_device_size", strconv.Itoa(proptools.Int(s.properties.Size)))
	var groups, partitionList []string
	for _, groupInfo := range s.properties.Partition_groups {
		groups = append(groups, groupInfo.Name)
		partitionList = append(partitionList, groupInfo.PartitionList...)
	}
	addStr("dynamic_partition_list", strings.Join(android.SortedUniqueStrings(partitionList), " "))
	addStr("super_partition_groups", strings.Join(groups, " "))
	initialPartitionListLen := len(partitionList)
	partitionList = android.SortedUniqueStrings(partitionList)
	if len(partitionList) != initialPartitionListLen {
		ctx.ModuleErrorf("Duplicate partitions found in the partition_groups property")
	}
	// Add Partition group info after adding `super_partition_groups` and `dynamic_partition_list`
	for _, groupInfo := range s.properties.Partition_groups {
		addStr("super_"+groupInfo.Name+"_group_size", groupInfo.GroupSize)
		addStr("super_"+groupInfo.Name+"_partition_list", strings.Join(groupInfo.PartitionList, " "))
	}

	if proptools.Bool(s.properties.Super_image_in_update_package) {
		addStr("super_image_in_update_package", "true")
	}
	addStr("super_partition_size", strconv.Itoa(proptools.Int(s.properties.Size)))

	if proptools.Bool(s.properties.Virtual_ab.Enable) {
		addStr("virtual_ab", "true")
		if proptools.Bool(s.properties.Virtual_ab.Retrofit) {
			addStr("virtual_ab_retrofit", "true")
		}
		addStr("virtual_ab_compression", strconv.FormatBool(proptools.Bool(s.properties.Virtual_ab.Compression)))
		if s.properties.Virtual_ab.Compression_method != nil {
			matched, _ := regexp.MatchString("^[a-zA-Z0-9_-]+$", *s.properties.Virtual_ab.Compression_method)
			if !matched {
				ctx.PropertyErrorf("virtual_ab.compression_method", "compression_method cannot have special characters")
			}
			addStr("virtual_ab_compression_method", *s.properties.Virtual_ab.Compression_method)
		}
		if s.properties.Virtual_ab.Cow_version != nil {
			addStr("virtual_ab_cow_version", strconv.FormatInt(*s.properties.Virtual_ab.Cow_version, 10))
		}
		if s.properties.Virtual_ab.Compression_factor != nil {
			addStr("virtual_ab_compression_factor", strconv.FormatInt(*s.properties.Virtual_ab.Compression_factor, 10))
		}

	} else {
		if s.properties.Virtual_ab.Retrofit != nil {
			ctx.PropertyErrorf("virtual_ab.retrofit", "This property cannot be set when virtual_ab is disabled")
		}
		if s.properties.Virtual_ab.Compression != nil {
			ctx.PropertyErrorf("virtual_ab.compression", "This property cannot be set when virtual_ab is disabled")
		}
		if s.properties.Virtual_ab.Compression_method != nil {
			ctx.PropertyErrorf("virtual_ab.compression_method", "This property cannot be set when virtual_ab is disabled")
		}
		if s.properties.Virtual_ab.Compression_factor != nil {
			ctx.PropertyErrorf("virtual_ab.compression_factor", "This property cannot be set when virtual_ab is disabled")
		}
	}

	return partitionList
}

func (s *superImage) generateDynamicPartitionsInfo(ctx android.ModuleContext) android.Path {
	var contents strings.Builder
	s.dumpDynamicPartitionInfo(ctx, &contents)
	dynamicPartitionsInfo := android.PathForModuleOut(ctx, "dynamic_partitions_info.txt")
	android.WriteFileRuleVerbatim(ctx, dynamicPartitionsInfo, contents.String())
	return dynamicPartitionsInfo
}
