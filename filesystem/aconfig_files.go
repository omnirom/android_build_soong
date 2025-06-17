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
	"android/soong/android"
	"strconv"
	"strings"

	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"
)

func init() {
	pctx.HostBinToolVariable("aconfig", "aconfig")
}

var (
	aconfigCreateStorage = pctx.AndroidStaticRule("aconfig_create_storage", blueprint.RuleParams{
		Command:     `$aconfig create-storage --container $container --file $fileType --out $out --cache $in --version $version`,
		CommandDeps: []string{"$aconfig"},
	}, "container", "fileType", "version")

	subPartitionsInPartition = map[string][]string{
		"system": {"system_ext", "product", "vendor"},
		"vendor": {"odm"},
	}
)

func (f *filesystem) buildAconfigFlagsFiles(
	ctx android.ModuleContext,
	builder *android.RuleBuilder,
	specs map[string]android.PackagingSpec,
	dir android.OutputPath,
	fullInstallPaths *[]FullInstallPathInfo,
) {
	if !proptools.Bool(f.properties.Gen_aconfig_flags_pb) {
		return
	}

	partition := f.PartitionType()
	subPartitionsFound := map[string]bool{}
	fullInstallPath := android.PathForModuleInPartitionInstall(ctx, partition)

	for _, subPartition := range subPartitionsInPartition[partition] {
		subPartitionsFound[subPartition] = false
	}

	var caches []android.Path
	for _, ps := range specs {
		caches = append(caches, ps.GetAconfigPaths()...)
		for subPartition, found := range subPartitionsFound {
			if !found && strings.HasPrefix(ps.RelPathInPackage(), subPartition+"/") {
				subPartitionsFound[subPartition] = true
				break
			}
		}
	}
	caches = android.SortedUniquePaths(caches)

	buildAconfigFlagsFiles := func(container string, dir android.OutputPath, fullInstallPath android.InstallPath) {
		aconfigFlagsPb := android.PathForModuleOut(ctx, "aconfig", container, "aconfig_flags.pb")
		aconfigFlagsPbBuilder := android.NewRuleBuilder(pctx, ctx)
		cmd := aconfigFlagsPbBuilder.Command().
			BuiltTool("aconfig").
			Text(" dump-cache --dedup --format protobuf --out").
			Output(aconfigFlagsPb).
			Textf("--filter container:%s+state:ENABLED", container).
			Textf("--filter container:%s+permission:READ_WRITE", container)
		for _, cache := range caches {
			cmd.FlagWithInput("--cache ", cache)
		}
		aconfigFlagsPbBuilder.Build(container+"_aconfig_flags_pb", "build aconfig_flags.pb")

		installEtcDir := dir.Join(ctx, "etc")
		installAconfigFlagsPath := installEtcDir.Join(ctx, "aconfig_flags.pb")
		builder.Command().Text("mkdir -p ").Text(installEtcDir.String())
		builder.Command().Text("cp").Input(aconfigFlagsPb).Text(installAconfigFlagsPath.String())
		*fullInstallPaths = append(*fullInstallPaths, FullInstallPathInfo{
			FullInstallPath: fullInstallPath.Join(ctx, "etc/aconfig_flags.pb"),
			SourcePath:      aconfigFlagsPb,
		})
		f.appendToEntry(ctx, installAconfigFlagsPath)

		// To enable fingerprint, we need to have v2 storage files. The default version is 1.
		storageFilesVersion := 1
		if ctx.Config().ReleaseFingerprintAconfigPackages() {
			storageFilesVersion = 2
		}

		installAconfigStorageDir := installEtcDir.Join(ctx, "aconfig")
		builder.Command().Text("mkdir -p").Text(installAconfigStorageDir.String())

		generatePartitionAconfigStorageFile := func(fileType, fileName string) {
			outPath := android.PathForModuleOut(ctx, "aconfig", container, fileName)
			installPath := installAconfigStorageDir.Join(ctx, fileName)
			ctx.Build(pctx, android.BuildParams{
				Rule:   aconfigCreateStorage,
				Input:  aconfigFlagsPb,
				Output: outPath,
				Args: map[string]string{
					"container": container,
					"fileType":  fileType,
					"version":   strconv.Itoa(storageFilesVersion),
				},
			})
			builder.Command().
				Text("cp").Input(outPath).Text(installPath.String())
			*fullInstallPaths = append(*fullInstallPaths, FullInstallPathInfo{
				SourcePath:      outPath,
				FullInstallPath: fullInstallPath.Join(ctx, "etc/aconfig", fileName),
			})
			f.appendToEntry(ctx, installPath)
		}

		if ctx.Config().ReleaseCreateAconfigStorageFile() {
			generatePartitionAconfigStorageFile("package_map", "package.map")
			generatePartitionAconfigStorageFile("flag_map", "flag.map")
			generatePartitionAconfigStorageFile("flag_val", "flag.val")
			generatePartitionAconfigStorageFile("flag_info", "flag.info")
		}
	}

	buildAconfigFlagsFiles(partition, dir, fullInstallPath)
	for _, subPartition := range android.SortedKeys(subPartitionsFound) {
		if subPartitionsFound[subPartition] {
			buildAconfigFlagsFiles(subPartition, dir.Join(ctx, subPartition), fullInstallPath.Join(ctx, subPartition))
		}
	}
}
