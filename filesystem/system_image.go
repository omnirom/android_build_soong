// Copyright (C) 2021 The Android Open Source Project
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
	"android/soong/cc"
	"android/soong/linkerconfig"

	"strings"

	"github.com/google/blueprint/proptools"
)

type systemImage struct {
	filesystem
}

var _ filesystemBuilder = (*systemImage)(nil)

// android_system_image is a specialization of android_filesystem for the 'system' partition.
// Currently, the only difference is the inclusion of linker.config.pb file which specifies
// the provided and the required libraries to and from APEXes.
func SystemImageFactory() android.Module {
	module := &systemImage{}
	module.filesystemBuilder = module
	initFilesystemModule(module, &module.filesystem)
	return module
}

func (s systemImage) FsProps() FilesystemProperties {
	return s.filesystem.properties
}

func (s *systemImage) BuildLinkerConfigFile(
	ctx android.ModuleContext,
	builder *android.RuleBuilder,
	rebasedDir android.OutputPath,
	fullInstallPaths *[]FullInstallPathInfo,
	platformGeneratedFiles *[]string,
) {
	if !proptools.Bool(s.filesystem.properties.Linker_config.Gen_linker_config) {
		return
	}

	output := rebasedDir.Join(ctx, "etc", "linker.config.pb")
	if s.filesystem.properties.Linker_config.Linker_config_srcs != nil {
		provideModules, requireModules := s.getLibsForLinkerConfig(ctx)
		intermediateOutput := android.PathForModuleOut(ctx, "linker.config.pb")
		linkerconfig.BuildLinkerConfig(ctx, android.PathsForModuleSrc(ctx, s.filesystem.properties.Linker_config.Linker_config_srcs), provideModules, requireModules, intermediateOutput)
		builder.Command().Text("cp").Input(intermediateOutput).Output(output)

		installPath := android.PathForModuleInPartitionInstall(ctx, s.PartitionType(), "etc", "linker.config.pb")
		*fullInstallPaths = append(*fullInstallPaths, FullInstallPathInfo{
			FullInstallPath: installPath,
			SourcePath:      intermediateOutput,
		})
		*platformGeneratedFiles = append(*platformGeneratedFiles, installPath.String())
	} else {
		// TODO: This branch is the logic that make uses for the linker config file, which is
		// different than linkerconfig.BuildLinkerConfig used above. Keeping both branches for now
		// because microdroid uses the other method and is in theory happy with it. But we should
		// consider deduping them.
		stubLibraries := cc.StubLibrariesFile(ctx)
		llndkMovedToApexLibraries := cc.MovedToApexLlndkLibrariesFile(ctx)
		outputStep1 := android.PathForModuleOut(ctx, "linker.config.pb.step1")
		builder.Command().
			BuiltTool("conv_linker_config").
			Text("proto --force").
			FlagWithInput("-s ", android.PathForSource(ctx, "system/core/rootdir/etc/linker.config.json")).
			FlagWithOutput("-o ", outputStep1)
		builder.Temporary(outputStep1)
		builder.Command().
			BuiltTool("conv_linker_config").
			Text("systemprovide").
			FlagWithInput("--source ", outputStep1).
			FlagWithArg("--output ", output.String()).
			Textf(`--value "$(cat %s)"`, stubLibraries).
			Implicit(stubLibraries).
			FlagWithArg("--system ", rebasedDir.String())
		builder.Command().
			BuiltTool("conv_linker_config").
			Text("append").
			FlagWithArg("--source ", output.String()).
			FlagWithOutput("--output ", output).
			FlagWithArg("--key ", "requireLibs").
			Textf(`--value "$(cat %s)"`, llndkMovedToApexLibraries).
			Implicit(llndkMovedToApexLibraries)
		// TODO: Make also supports adding an extra append command with PRODUCT_EXTRA_STUB_LIBRARIES,
		// but that variable appears to have no usages.

		installPath := android.PathForModuleInPartitionInstall(ctx, s.PartitionType(), "etc", "linker.config.pb")
		*fullInstallPaths = append(*fullInstallPaths, FullInstallPathInfo{
			FullInstallPath: installPath,
			SourcePath:      output,
		})
		*platformGeneratedFiles = append(*platformGeneratedFiles, installPath.String())
	}

	s.appendToEntry(ctx, output)
}

// Filter the result of GatherPackagingSpecs to discard items targeting outside "system" / "root"
// partition.  Note that "apex" module installs its contents to "apex"(fake partition) as well
// for symbol lookup by imitating "activated" paths.
func (s *systemImage) FilterPackagingSpec(ps android.PackagingSpec) bool {
	return !ps.SkipInstall() &&
		(ps.Partition() == "system" || ps.Partition() == "root" ||
			strings.HasPrefix(ps.Partition(), "system/"))
}

func (s *systemImage) ShouldUseVintfFragmentModuleOnly() bool {
	return true
}
