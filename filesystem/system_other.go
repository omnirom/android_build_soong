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
	"sort"
	"strings"
	"time"

	"android/soong/android"

	"github.com/google/blueprint"
	"github.com/google/blueprint/depset"
	"github.com/google/blueprint/proptools"
)

var (
	systemOtherPropFileTweaks = pctx.AndroidStaticRule("system_other_prop_file_tweaks", blueprint.RuleParams{
		Command: `rm -rf $out && sed -e 's@^mount_point=/$$@mount_point=system_other@g' -e 's@^partition_name=system$$@partition_name=system_other@g' $in > $out`,
	})
)

type SystemOtherImageProperties struct {
	// The system_other image always requires a reference to the system image. The system_other
	// partition gets built into the system partition's "b" slot in a/b partition builds. Thus, it
	// copies most of its configuration from the system image, such as filesystem type, avb signing
	// info, etc. Including it here does not automatically mean that it will pick up the system
	// image's dexpropt files, it must also be listed in Preinstall_dexpreopt_files_from for that.
	System_image *string

	// This system_other partition will include all the dexpreopt files from the apps on these
	// partitions.
	Preinstall_dexpreopt_files_from []string
}

type systemOtherImage struct {
	android.ModuleBase
	android.DefaultableModuleBase
	properties SystemOtherImageProperties
}

// The system_other image is the default contents of the "b" slot of the system image.
// It contains the dexpreopt files of all the apps on the device, for a faster first boot.
// Afterwards, at runtime, it will be used as a regular b slot for OTA updates, and the initial
// dexpreopt files will be deleted.
func SystemOtherImageFactory() android.Module {
	module := &systemOtherImage{}
	module.AddProperties(&module.properties)
	android.InitAndroidArchModule(module, android.DeviceSupported, android.MultilibCommon)
	android.InitDefaultableModule(module)
	return module
}

type systemImageDeptag struct {
	blueprint.BaseDependencyTag
}

var systemImageDependencyTag = systemImageDeptag{}

type dexpreoptDeptag struct {
	blueprint.BaseDependencyTag
}

var dexpreoptDependencyTag = dexpreoptDeptag{}

func (m *systemOtherImage) DepsMutator(ctx android.BottomUpMutatorContext) {
	if proptools.String(m.properties.System_image) == "" {
		ctx.ModuleErrorf("system_image property must be set")
		return
	}
	ctx.AddDependency(ctx.Module(), systemImageDependencyTag, *m.properties.System_image)
	ctx.AddDependency(ctx.Module(), dexpreoptDependencyTag, m.properties.Preinstall_dexpreopt_files_from...)
}

func (m *systemOtherImage) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	systemImage := ctx.GetDirectDepProxyWithTag(*m.properties.System_image, systemImageDependencyTag)
	systemInfo, ok := android.OtherModuleProvider(ctx, systemImage, FilesystemProvider)
	if !ok {
		ctx.PropertyErrorf("system_image", "Expected system_image module to provide FilesystemProvider")
		return
	}

	output := android.PathForModuleOut(ctx, "system_other.img")
	stagingDir := android.PathForModuleOut(ctx, "system_other").OutputPath
	stagingDirTimestamp := android.PathForModuleOut(ctx, "staging_dir.timestamp")

	builder := android.NewRuleBuilder(pctx, ctx)
	builder.Command().Textf("rm -rf %s && mkdir -p %s", stagingDir, stagingDir)

	specs := make(map[string]android.PackagingSpec)
	for _, otherPartition := range m.properties.Preinstall_dexpreopt_files_from {
		dexModule := ctx.GetDirectDepProxyWithTag(otherPartition, dexpreoptDependencyTag)
		fsInfo, ok := android.OtherModuleProvider(ctx, dexModule, FilesystemProvider)
		if !ok {
			ctx.PropertyErrorf("preinstall_dexpreopt_files_from", "Expected module %q to provide FilesystemProvider", otherPartition)
			return
		}
		// Merge all the packaging specs into 1 map
		for k := range fsInfo.SpecsForSystemOther {
			if _, ok := specs[k]; ok {
				ctx.ModuleErrorf("Packaging spec %s given by two different partitions", k)
				continue
			}
			specs[k] = fsInfo.SpecsForSystemOther[k]
		}
	}

	// TOOD: CopySpecsToDir only exists on PackagingBase, but doesn't use any fields from it. Clean this up.
	(&android.PackagingBase{}).CopySpecsToDir(ctx, builder, specs, stagingDir)

	fullInstallPaths := []FullInstallPathInfo{}
	for _, mod := range android.SortedKeys(specs) {
		spec := specs[mod]
		fullInstallPaths = append(fullInstallPaths, FullInstallPathInfo{
			FullInstallPath:     spec.FullInstallPath(),
			RequiresFullInstall: spec.RequiresFullInstall(),
			SourcePath:          spec.SrcPath(),
			SymlinkTarget:       spec.SymlinkTarget(),
		})
	}

	platformGenerated := []string{}
	if len(m.properties.Preinstall_dexpreopt_files_from) > 0 {
		builtPath := android.PathForModuleOut(ctx, "system_other", "system-other-odex-marker")
		builder.Command().Textf("touch").Output(builtPath)
		installPath := android.PathForModuleInPartitionInstall(ctx, "system_other", "system-other-odex-marker")
		fullInstallPaths = append(fullInstallPaths, FullInstallPathInfo{
			FullInstallPath: installPath,
			SourcePath:      builtPath,
		})
		platformGenerated = append(platformGenerated, installPath.String())
	}
	builder.Command().Textf("touch").Output(stagingDirTimestamp)
	builder.Build("assemble_filesystem_staging_dir", "Assemble filesystem staging dir")

	// Most of the time, if build_image were to call a host tool, it accepts the path to the
	// host tool in a field in the prop file. However, it doesn't have that option for fec, which
	// it expects to just be on the PATH. Add fec to the PATH.
	fec := ctx.Config().HostToolPath(ctx, "fec")
	pathToolDirs := []string{filepath.Dir(fec.String())}

	// In make, the exact same prop file is used for both system and system_other. However, I
	// believe make goes through a different build_image code path that is based on the name of
	// the output file. So it sees the output file is named system_other.img and makes some changes.
	// We don't use that codepath, so make the changes manually to the prop file.
	propFile := android.PathForModuleOut(ctx, "prop")
	ctx.Build(pctx, android.BuildParams{
		Rule:   systemOtherPropFileTweaks,
		Input:  systemInfo.BuildImagePropFile,
		Output: propFile,
	})

	builder = android.NewRuleBuilder(pctx, ctx)
	builder.Command().
		Textf("PATH=%s:$PATH", strings.Join(pathToolDirs, ":")).
		BuiltTool("build_image").
		Text(stagingDir.String()). // input directory
		Input(propFile).
		Implicits(systemInfo.BuildImagePropFileDeps).
		Implicit(fec).
		Implicit(stagingDirTimestamp).
		Output(output).
		Text(stagingDir.String())

	builder.Build("build_system_other", "build system other")

	fsInfo := FilesystemInfo{
		Output:              output,
		RootDir:             stagingDir,
		ModuleName:          ctx.ModuleName(),
		FilesystemConfig:    m.generateFilesystemConfig(ctx, stagingDir, stagingDirTimestamp),
		PropFileForMiscInfo: m.buildPropFileForMiscInfo(ctx),
		InstalledFilesDepSet: depset.New(
			depset.POSTORDER,
			[]InstalledFilesStruct{buildInstalledFiles(ctx, "system-other", stagingDir, output)},
			nil,
		),
		FullInstallPaths: fullInstallPaths,
	}

	android.SetProvider(ctx, FilesystemProvider, fsInfo)

	ctx.SetOutputFiles(android.Paths{output}, "")
	ctx.CheckbuildFile(output)

	// Dump compliance metadata
	complianceMetadataInfo := ctx.ComplianceMetadataInfo()
	filesContained := make([]string, 0, len(fullInstallPaths))
	for _, file := range fullInstallPaths {
		filesContained = append(filesContained, file.FullInstallPath.String())
	}
	complianceMetadataInfo.SetFilesContained(filesContained)
	complianceMetadataInfo.SetPlatformGeneratedFiles(platformGenerated)
}

func (s *systemOtherImage) generateFilesystemConfig(ctx android.ModuleContext, stagingDir, stagingDirTimestamp android.Path) android.Path {
	out := android.PathForModuleOut(ctx, "filesystem_config.txt")
	ctx.Build(pctx, android.BuildParams{
		Rule:   fsConfigRule,
		Input:  stagingDirTimestamp, // assemble the staging directory
		Output: out,
		Args: map[string]string{
			"rootDir": stagingDir.String(),
			"prefix":  "system/",
		},
	})
	return out
}

func (f *systemOtherImage) buildPropFileForMiscInfo(ctx android.ModuleContext) android.Path {
	var lines []string
	addStr := func(name string, value string) {
		lines = append(lines, fmt.Sprintf("%s=%s", name, value))
	}

	addStr("building_system_other_image", "true")

	systemImage := ctx.GetDirectDepProxyWithTag(*f.properties.System_image, systemImageDependencyTag)
	systemInfo, ok := android.OtherModuleProvider(ctx, systemImage, FilesystemProvider)
	if !ok {
		ctx.PropertyErrorf("system_image", "Expected system_image module to provide FilesystemProvider")
		return nil
	}
	if systemInfo.PartitionSize == nil {
		addStr("system_other_disable_sparse", "true")
	}
	if systemInfo.UseAvb {
		addStr("avb_system_other_hashtree_enable", "true")
		addStr("avb_system_other_algorithm", systemInfo.AvbAlgorithm)
		footerArgs := fmt.Sprintf("--hash_algorithm %s", systemInfo.AvbHashAlgorithm)
		if rollbackIndex, err := f.avbRollbackIndex(ctx); err == nil {
			footerArgs += fmt.Sprintf(" --rollback_index %d", rollbackIndex)
		} else {
			ctx.ModuleErrorf("Could not determine rollback_index %s\n", err)
		}
		addStr("avb_system_other_add_hashtree_footer_args", footerArgs)
		if systemInfo.AvbKey != nil {
			addStr("avb_system_other_key_path", systemInfo.AvbKey.String())
		}
	}

	sort.Strings(lines)

	propFile := android.PathForModuleOut(ctx, "prop_file")
	android.WriteFileRule(ctx, propFile, strings.Join(lines, "\n"))
	return propFile
}

// Use the default: PlatformSecurityPatch
// TODO: Get this value from vbmeta_system
func (f *systemOtherImage) avbRollbackIndex(ctx android.ModuleContext) (int64, error) {
	t, err := time.Parse(time.DateOnly, ctx.Config().PlatformSecurityPatch())
	if err != nil {
		return -1, err
	}
	return t.Unix(), err
}
