// Copyright (C) 2025 The Android Open Source Project
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
	"android/soong/android"

	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"
)

var (
	copyStagingDirRule = pctx.AndroidStaticRule("copy_staging_dir", blueprint.RuleParams{
		Command: "rsync -a --checksum $dir/ $dest && touch $out",
	}, "dir", "dest")
)

func (a *androidDevice) copyToProductOut(ctx android.ModuleContext, builder *android.RuleBuilder, src android.Path, dest string) {
	destPath := android.PathForModuleInPartitionInstall(ctx, "").Join(ctx, dest)
	builder.Command().Text("rsync").Flag("-a").Flag("--checksum").Input(src).Text(destPath.String())
}

func (a *androidDevice) copyFilesToProductOutForSoongOnly(ctx android.ModuleContext) android.Path {
	filesystemInfos := a.getFsInfos(ctx)

	var deps android.Paths
	var depsNoImg android.Paths // subset of deps without any img files. used for sbom creation.
	installedFilesMap := make(map[android.Path]bool)
	for _, partition := range android.SortedKeys(filesystemInfos) {
		info := filesystemInfos[partition]
		imgInstallPath := android.PathForModuleInPartitionInstall(ctx, "", partition+".img")
		ctx.Build(pctx, android.BuildParams{
			Rule:   android.Cp,
			Input:  info.Output,
			Output: imgInstallPath,
		})

		// Make it so doing `m <moduleName>` or `m <partitionType>image` will copy the files to
		// PRODUCT_OUT
		if partition == "system_ext" {
			partition = "systemext"
		}
		partition = partition + "image"
		ctx.Phony(info.ModuleName, imgInstallPath)
		ctx.Phony(partition, imgInstallPath)
		for _, fip := range info.FullInstallPaths {
			// TODO: Directories. But maybe they're not necessary? Adevice doesn't care
			// about empty directories, still need to check if adb sync does.
			if !fip.IsDir {
				if !fip.RequiresFullInstall {
					// Some modules set requires_full_install: false, which causes their staging
					// directory file to not be installed. This is usually because the file appears
					// in both PRODUCT_COPY_FILES and a soong module for the handwritten soong system
					// image. In this case, that module's installed files would conflict with the
					// PRODUCT_COPY_FILES. However, in soong-only builds, we don't automatically
					// create rules for PRODUCT_COPY_FILES unless they're needed in the partition.
					// So in that case, nothing is creating the installed path. Create them now
					// if that's the case.
					if fip.SymlinkTarget == "" {
						ctx.Build(pctx, android.BuildParams{
							Rule:   android.CpWithBash,
							Input:  fip.SourcePath,
							Output: fip.FullInstallPath,
							Args: map[string]string{
								// Preserve timestamps for adb sync, so that this installed file's
								// timestamp matches the timestamp in the filesystem's intermediate
								// staging dir
								"cpFlags": "-p",
							},
						})
					} else {
						ctx.Build(pctx, android.BuildParams{
							Rule:   android.SymlinkWithBash,
							Output: fip.FullInstallPath,
							Args: map[string]string{
								"fromPath": fip.SymlinkTarget,
							},
						})
					}
				}
				ctx.Phony(info.ModuleName, fip.FullInstallPath)
				ctx.Phony(partition, fip.FullInstallPath)
				ctx.Phony("sync_"+partition, fip.FullInstallPath)
				ctx.Phony("sync", fip.FullInstallPath)

				if info.Prebuilt {
					continue
				}

				deps = append(deps, fip.FullInstallPath)
				depsNoImg = append(depsNoImg, fip.FullInstallPath)
			}
		}

		deps = append(deps, imgInstallPath)

		// Copy installed-files(.txt|.json) to staging dir for makepush
		for _, installedFiles := range info.InstalledFilesDepSet.ToList() {
			if _, exists := installedFilesMap[installedFiles.Json]; !exists && installedFiles.Json != nil {
				installPath := android.PathForModuleInPartitionInstall(ctx, "", installedFiles.Json.Base())
				ctx.Build(pctx, android.BuildParams{
					Rule:   android.Cp,
					Input:  installedFiles.Json,
					Output: installPath,
				})
				deps = append(deps, installPath)
				installedFilesMap[installedFiles.Json] = true
			}
			if _, exists := installedFilesMap[installedFiles.Txt]; !exists && installedFiles.Txt != nil {
				installPath := android.PathForModuleInPartitionInstall(ctx, "", installedFiles.Txt.Base())
				ctx.Build(pctx, android.BuildParams{
					Rule:   android.Cp,
					Input:  installedFiles.Txt,
					Output: installPath,
				})
				deps = append(deps, installPath)
				installedFilesMap[installedFiles.Txt] = true
			}
		}
	}

	a.createComplianceMetadataTimestamp(ctx, depsNoImg)

	// List all individual files to be copied to PRODUCT_OUT here
	bootloaderDepTags := []blueprint.DependencyTag{bootloaderDepTag, tzswDepTag}
	ctx.VisitDirectDepsProxy(func(child android.ModuleProxy) {
		tag := ctx.OtherModuleDependencyTag(child)
		if !android.InList(tag, bootloaderDepTags) {
			return
		}
		files := android.OutputFilesForModule(ctx, child, "")
		for _, file := range files {
			installPath := android.PathForModuleInPartitionInstall(ctx, "", file.Base())
			ctx.Build(pctx, android.BuildParams{
				Rule:   android.Cp,
				Input:  file,
				Output: installPath,
			})
			deps = append(deps, installPath)
		}
	})

	copyBootImg := func(prop *string, type_ string) {
		if proptools.String(prop) != "" {
			partition := ctx.GetDirectDepProxyWithTag(*prop, filesystemDepTag)
			if info, ok := android.OtherModuleProvider(ctx, partition, BootimgInfoProvider); ok {
				installPath := android.PathForModuleInPartitionInstall(ctx, "", type_+".img")
				ctx.Build(pctx, android.BuildParams{
					Rule:   android.Cp,
					Input:  info.Output,
					Output: installPath,
				})
				deps = append(deps, installPath)
			} else {
				ctx.ModuleErrorf("%s does not set BootimgInfo\n", *prop)
			}
		}
	}

	copyBootImg(a.partitionProps.Init_boot_partition_name, "init_boot")
	copyBootImg(a.partitionProps.Boot_partition_name, "boot")
	copyBootImg(a.partitionProps.Vendor_boot_partition_name, "vendor_boot")
	copyBootImg(a.partitionProps.Vendor_kernel_boot_partition_name, "vendor_kernel_boot")

	// pvmfw
	if a.deviceProps.Pvmfw.Image != nil {
		pvmfwImg := android.PathForModuleSrc(ctx, proptools.String(a.deviceProps.Pvmfw.Image))
		installPath := android.PathForModuleInPartitionInstall(ctx, "", "pvmfw.img")
		ctx.Build(pctx, android.BuildParams{
			Rule:   android.Cp,
			Input:  pvmfwImg,
			Output: installPath,
		})
		deps = append(deps, installPath)
	}

	// radio
	if a.deviceProps.Radio_partition_name != nil {
		radio := ctx.GetDirectDepProxyWithTag(*a.deviceProps.Radio_partition_name, radioDepTag)
		files := android.OutputFilesForModule(ctx, radio, "")
		for _, file := range files {
			installPath := android.PathForModuleInPartitionInstall(ctx, "", file.Base())
			ctx.Build(pctx, android.BuildParams{
				Rule:   android.Cp,
				Input:  file,
				Output: installPath,
			})
			deps = append(deps, installPath)
		}
	}

	// dtbo
	for _, dtbo := range []*string{a.deviceProps.Dtbo_image, a.deviceProps.Dtbo_image_16k} {
		if dtbo != nil {
			dtboModule := ctx.GetDirectDepProxyWithTag(*dtbo, dtboDepTag)
			img := android.OutputFilesForModule(ctx, dtboModule, "")[0]
			installPath := android.PathForModuleInPartitionInstall(ctx, "", img.Base())
			ctx.Build(pctx, android.BuildParams{
				Rule:   android.Cp,
				Input:  img,
				Output: installPath,
			})
			deps = append(deps, installPath)
		}
	}

	// Fastboot-info.txt
	fastbootInstallpath := android.PathForModuleInPartitionInstall(ctx, "", "fastboot-info.txt")
	ctx.Build(pctx, android.BuildParams{
		Rule:   android.Cp,
		Input:  a.fastbootInfoFile,
		Output: fastbootInstallpath,
	})
	deps = append(deps, fastbootInstallpath)

	for _, vbmetaModName := range a.partitionProps.Vbmeta_partitions {
		partition := ctx.GetDirectDepProxyWithTag(vbmetaModName, filesystemDepTag)
		if info, ok := android.OtherModuleProvider(ctx, partition, vbmetaPartitionProvider); ok {
			installPath := android.PathForModuleInPartitionInstall(ctx, "", info.Name+".img")
			ctx.Build(pctx, android.BuildParams{
				Rule:   android.Cp,
				Input:  info.Output,
				Output: installPath,
			})
			deps = append(deps, installPath)
		} else {
			ctx.ModuleErrorf("%s does not set vbmetaPartitionProvider\n", vbmetaModName)
		}
	}

	if proptools.String(a.partitionProps.Super_partition_name) != "" {
		partition := ctx.GetDirectDepProxyWithTag(*a.partitionProps.Super_partition_name, superPartitionDepTag)
		if info, ok := android.OtherModuleProvider(ctx, partition, SuperImageProvider); ok {
			installPath := android.PathForModuleInPartitionInstall(ctx, "", "super.img")
			ctx.Build(pctx, android.BuildParams{
				Rule:   android.Cp,
				Input:  info.SuperImage,
				Output: installPath,
			})
			deps = append(deps, installPath)
			if info.SuperEmptyImage != nil {
				installPath := android.PathForModuleInPartitionInstall(ctx, "", "super_empty.img")
				ctx.Build(pctx, android.BuildParams{
					Rule:   android.Cp,
					Input:  info.SuperEmptyImage,
					Output: installPath,
				})
				deps = append(deps, installPath)
			}

		} else {
			ctx.ModuleErrorf("%s does not set SuperImageProvider\n", *a.partitionProps.Super_partition_name)
		}
	}

	if a.androidInfoTxt != nil {
		installPath := android.PathForModuleInPartitionInstall(ctx, "", "android-info.txt")
		var validations android.Paths
		if a.boardInfoTxt != nil && a.deviceProps.Bootloader != nil {
			validations = append(validations, a.checkRadioVersion(ctx))
		}
		ctx.Build(pctx, android.BuildParams{
			Rule:        android.Cp,
			Input:       a.androidInfoTxt,
			Output:      installPath,
			Validations: validations,
		})
		deps = append(deps, installPath)
	}

	copyToProductOutTimestamp := android.PathForModuleOut(ctx, "product_out_copy_timestamp")
	ctx.Build(pctx, android.BuildParams{
		Rule:      android.Touch,
		Output:    copyToProductOutTimestamp,
		Implicits: deps,
	})

	return copyToProductOutTimestamp
}

// createComplianceMetadataTimestampForSoongOnly creates a timestamp file in m --soong-only
// this timestamp file depends on installed files of the main `android_device`.
// Any changes to installed files of the main `android_device` will retrigger SBOM generation
func (a *androidDevice) createComplianceMetadataTimestamp(ctx android.ModuleContext, installedFiles android.Paths) {
	ctx.Build(pctx, android.BuildParams{
		Rule:      android.Touch,
		Implicits: installedFiles,
		Output:    android.PathForOutput(ctx, "compliance-metadata", ctx.Config().DeviceProduct(), "installed_files.stamp"),
	})
}

// Returns a mapping from partition type -> FilesystemInfo. This includes filesystems that are
// nested inside of other partitions, such as the partitions inside super.img, or ramdisk inside
// of boot.
func (a *androidDevice) getFsInfos(ctx android.ModuleContext) map[string]FilesystemInfo {
	type propToType struct {
		prop *string
		ty   string
	}

	filesystemInfos := make(map[string]FilesystemInfo)

	partitionDefinitions := []propToType{
		propToType{a.partitionProps.System_partition_name, "system"},
		propToType{a.partitionProps.System_ext_partition_name, "system_ext"},
		propToType{a.partitionProps.Product_partition_name, "product"},
		propToType{a.partitionProps.Vendor_partition_name, "vendor"},
		propToType{a.partitionProps.Odm_partition_name, "odm"},
		propToType{a.partitionProps.Recovery_partition_name, "recovery"},
		propToType{a.partitionProps.System_dlkm_partition_name, "system_dlkm"},
		propToType{a.partitionProps.Vendor_dlkm_partition_name, "vendor_dlkm"},
		propToType{a.partitionProps.Odm_dlkm_partition_name, "odm_dlkm"},
		propToType{a.partitionProps.Userdata_partition_name, "userdata"},
		// filesystemInfo from init_boot and vendor_boot actually are re-exports of the ramdisk
		// images inside of them
		propToType{a.partitionProps.Init_boot_partition_name, "ramdisk"},
		propToType{a.partitionProps.Vendor_boot_partition_name, "vendor_ramdisk"},
	}
	for _, partitionDefinition := range partitionDefinitions {
		if proptools.String(partitionDefinition.prop) != "" {
			partition := ctx.GetDirectDepProxyWithTag(*partitionDefinition.prop, filesystemDepTag)
			if info, ok := android.OtherModuleProvider(ctx, partition, FilesystemProvider); ok {
				filesystemInfos[partitionDefinition.ty] = info
			} else {
				ctx.ModuleErrorf("Super partition %s does not set FilesystemProvider\n", partition.Name())
			}
		}
	}
	if a.partitionProps.Super_partition_name != nil {
		superPartition := ctx.GetDirectDepProxyWithTag(*a.partitionProps.Super_partition_name, superPartitionDepTag)
		if info, ok := android.OtherModuleProvider(ctx, superPartition, SuperImageProvider); ok {
			for partition := range info.SubImageInfo {
				filesystemInfos[partition] = info.SubImageInfo[partition]
			}
		} else {
			ctx.ModuleErrorf("Super partition %s does not set SuperImageProvider\n", superPartition.Name())
		}
	}

	return filesystemInfos
}

func (a *androidDevice) getInfoOnlyFsInfos(ctx android.ModuleContext) map[string]FilesystemInfo {
	filesystemInfos := make(map[string]FilesystemInfo)

	if a.deviceProps.InfoPartitionProps.System_partition_name != nil {
		systemPartition := ctx.GetDirectDepProxyWithTag(*a.deviceProps.InfoPartitionProps.System_partition_name, filesystemDepTag)
		if info, ok := android.OtherModuleProvider(ctx, systemPartition, FilesystemProvider); ok {
			filesystemInfos["system"] = info
		}
	}
	if a.deviceProps.InfoPartitionProps.System_ext_partition_name != nil {
		systemExtPartition := ctx.GetDirectDepProxyWithTag(*a.deviceProps.InfoPartitionProps.System_ext_partition_name, filesystemDepTag)
		if info, ok := android.OtherModuleProvider(ctx, systemExtPartition, FilesystemProvider); ok {
			filesystemInfos["system_ext"] = info
		}
	}

	return filesystemInfos
}
