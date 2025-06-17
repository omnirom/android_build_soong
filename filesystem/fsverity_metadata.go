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
	"strings"

	"android/soong/android"

	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"
)

func init() {
	pctx.HostBinToolVariable("fsverity_metadata_generator", "fsverity_metadata_generator")
	pctx.HostBinToolVariable("fsverity_manifest_generator", "fsverity_manifest_generator")
	pctx.HostBinToolVariable("fsverity", "fsverity")
}

var (
	buildFsverityMeta = pctx.AndroidStaticRule("build_fsverity_meta", blueprint.RuleParams{
		Command:     `$fsverity_metadata_generator --fsverity-path $fsverity --signature none --hash-alg sha256 --output $out $in`,
		CommandDeps: []string{"$fsverity_metadata_generator", "$fsverity"},
	})
	buildFsverityManifest = pctx.AndroidStaticRule("build_fsverity_manifest", blueprint.RuleParams{
		Command:     `$fsverity_manifest_generator --fsverity-path $fsverity --output $out @$in`,
		CommandDeps: []string{"$fsverity_manifest_generator", "$fsverity"},
	})
)

type fsverityProperties struct {
	// Patterns of files for fsverity metadata generation.  For each matched file, a .fsv_meta file
	// will be generated and included to the filesystem image.
	// etc/security/fsverity/BuildManifest.apk will also be generated which contains information
	// about generated .fsv_meta files.
	Inputs proptools.Configurable[[]string]

	// APK libraries to link against, for etc/security/fsverity/BuildManifest.apk
	Libs proptools.Configurable[[]string] `android:"path"`
}

// Mapping of a given fsverity file, which may be a real file or a symlink, and the on-device
// path it should have relative to the filesystem root.
type fsveritySrcDest struct {
	src  android.Path
	dest string
}

func (f *filesystem) writeManifestGeneratorListFile(
	ctx android.ModuleContext,
	outputPath android.WritablePath,
	matchedFiles []fsveritySrcDest,
	rootDir android.OutputPath,
	rebasedDir android.OutputPath,
) []android.Path {
	prefix, err := filepath.Rel(rootDir.String(), rebasedDir.String())
	if err != nil {
		panic("rebasedDir should be relative to rootDir")
	}
	if prefix == "." {
		prefix = ""
	}
	if f.PartitionType() == "system_ext" {
		// Use the equivalent of $PRODUCT_OUT as the base dir.
		// This ensures that the paths in build_manifest.pb contain on-device paths
		// e.g. system_ext/framework/javalib.jar
		// and not framework/javalib.jar.
		//
		// Although base-dir is outside the rootdir provided for packaging, this action
		// is hermetic since it uses `manifestGeneratorListPath` to filter the files to be written to build_manifest.pb
		prefix = "system_ext"
	}

	var deps []android.Path
	var buf strings.Builder
	for _, spec := range matchedFiles {
		src := spec.src.String()
		dst := filepath.Join(prefix, spec.dest)
		if strings.Contains(src, ",") {
			ctx.ModuleErrorf("Path cannot contain a comma: %s", src)
		}
		if strings.Contains(dst, ",") {
			ctx.ModuleErrorf("Path cannot contain a comma: %s", dst)
		}
		buf.WriteString(src)
		buf.WriteString(",")
		buf.WriteString(dst)
		buf.WriteString("\n")
		deps = append(deps, spec.src)
	}
	android.WriteFileRuleVerbatim(ctx, outputPath, buf.String())
	return deps
}

func (f *filesystem) buildFsverityMetadataFiles(
	ctx android.ModuleContext,
	builder *android.RuleBuilder,
	specs map[string]android.PackagingSpec,
	rootDir android.OutputPath,
	rebasedDir android.OutputPath,
	fullInstallPaths *[]FullInstallPathInfo,
) {
	match := func(path string) bool {
		for _, pattern := range f.properties.Fsverity.Inputs.GetOrDefault(ctx, nil) {
			if matched, err := filepath.Match(pattern, path); matched {
				return true
			} else if err != nil {
				ctx.PropertyErrorf("fsverity.inputs", "bad pattern %q", pattern)
				return false
			}
		}
		return false
	}

	var matchedFiles []android.PackagingSpec
	var matchedSymlinks []android.PackagingSpec
	for _, relPath := range android.SortedKeys(specs) {
		if match(relPath) {
			spec := specs[relPath]
			if spec.SrcPath() != nil {
				matchedFiles = append(matchedFiles, spec)
			} else if spec.SymlinkTarget() != "" {
				matchedSymlinks = append(matchedSymlinks, spec)
			} else {
				ctx.ModuleErrorf("Expected a file or symlink for fsverity packaging spec")
			}
		}
	}

	if len(matchedFiles) == 0 && len(matchedSymlinks) == 0 {
		return
	}

	// STEP 1: generate .fsv_meta
	var fsverityFileSpecs []fsveritySrcDest
	for _, spec := range matchedFiles {
		rel := spec.RelPathInPackage() + ".fsv_meta"
		outPath := android.PathForModuleOut(ctx, "fsverity/meta_files", rel)
		destPath := rebasedDir.Join(ctx, rel)
		// srcPath is copied by CopySpecsToDir()
		ctx.Build(pctx, android.BuildParams{
			Rule:   buildFsverityMeta,
			Input:  spec.SrcPath(),
			Output: outPath,
		})
		builder.Command().Textf("cp").Input(outPath).Output(destPath)
		f.appendToEntry(ctx, destPath)
		*fullInstallPaths = append(*fullInstallPaths, FullInstallPathInfo{
			SourcePath:      destPath,
			FullInstallPath: android.PathForModuleInPartitionInstall(ctx, f.PartitionType(), rel),
		})
		fsverityFileSpecs = append(fsverityFileSpecs, fsveritySrcDest{
			src:  spec.SrcPath(),
			dest: spec.RelPathInPackage(),
		})
	}
	for _, spec := range matchedSymlinks {
		rel := spec.RelPathInPackage() + ".fsv_meta"
		outPath := android.PathForModuleOut(ctx, "fsverity/meta_files", rel)
		destPath := rebasedDir.Join(ctx, rel)
		target := spec.SymlinkTarget() + ".fsv_meta"
		ctx.Build(pctx, android.BuildParams{
			Rule:   android.Symlink,
			Output: outPath,
			Args: map[string]string{
				"fromPath": target,
			},
		})
		builder.Command().
			Textf("cp").
			Flag(ctx.Config().CpPreserveSymlinksFlags()).
			Input(outPath).
			Output(destPath)
		f.appendToEntry(ctx, destPath)
		*fullInstallPaths = append(*fullInstallPaths, FullInstallPathInfo{
			SymlinkTarget:   target,
			FullInstallPath: android.PathForModuleInPartitionInstall(ctx, f.PartitionType(), rel),
		})
		// The fsverity manifest tool needs to actually look at the symlink. But symlink
		// packagingSpecs are not actually created on disk, at least until the staging dir is
		// built for the partition. Create a fake one now so the tool can see it.
		realizedSymlink := android.PathForModuleOut(ctx, "fsverity/realized_symlinks", spec.RelPathInPackage())
		ctx.Build(pctx, android.BuildParams{
			Rule:   android.Symlink,
			Output: realizedSymlink,
			Args: map[string]string{
				"fromPath": spec.SymlinkTarget(),
			},
		})
		fsverityFileSpecs = append(fsverityFileSpecs, fsveritySrcDest{
			src:  realizedSymlink,
			dest: spec.RelPathInPackage(),
		})
	}

	// STEP 2: generate signed BuildManifest.apk
	// STEP 2-1: generate build_manifest.pb
	manifestGeneratorListPath := android.PathForModuleOut(ctx, "fsverity/fsverity_manifest.list")
	manifestDeps := f.writeManifestGeneratorListFile(ctx, manifestGeneratorListPath, fsverityFileSpecs, rootDir, rebasedDir)
	manifestPbPath := android.PathForModuleOut(ctx, "fsverity/build_manifest.pb")
	ctx.Build(pctx, android.BuildParams{
		Rule:      buildFsverityManifest,
		Input:     manifestGeneratorListPath,
		Implicits: manifestDeps,
		Output:    manifestPbPath,
	})

	// STEP 2-2: generate BuildManifest.apk (unsigned)
	apkNameSuffix := ""
	if f.PartitionType() == "system_ext" {
		//https://source.corp.google.com/h/googleplex-android/platform/build/+/e392d2b486c2d4187b20a72b1c67cc737ecbcca5:core/Makefile;l=3410;drc=ea8f34bc1d6e63656b4ec32f2391e9d54b3ebb6b;bpv=1;bpt=0
		apkNameSuffix = "SystemExt"
	}
	apkPath := android.PathForModuleOut(ctx, "fsverity", fmt.Sprintf("BuildManifest%s.apk", apkNameSuffix))
	idsigPath := android.PathForModuleOut(ctx, "fsverity", fmt.Sprintf("BuildManifest%s.apk.idsig", apkNameSuffix))
	manifestTemplatePath := android.PathForSource(ctx, "system/security/fsverity/AndroidManifest.xml")
	libs := android.PathsForModuleSrc(ctx, f.properties.Fsverity.Libs.GetOrDefault(ctx, nil))

	minSdkVersion := ctx.Config().PlatformSdkCodename()
	if minSdkVersion == "REL" {
		minSdkVersion = ctx.Config().PlatformSdkVersion().String()
	}

	apkBuilder := android.NewRuleBuilder(pctx, ctx)

	// aapt2 doesn't support adding individual asset files. Create a temp directory to hold asset
	// files and pass it to aapt2.
	tmpAssetDir := android.PathForModuleOut(ctx, "fsverity/tmp_asset_dir")
	stagedManifestPbPath := tmpAssetDir.Join(ctx, "build_manifest.pb")
	apkBuilder.Command().
		Text("rm -rf").Text(tmpAssetDir.String()).
		Text("&&").
		Text("mkdir -p").Text(tmpAssetDir.String())
	apkBuilder.Command().Text("cp").Input(manifestPbPath).Output(stagedManifestPbPath)

	unsignedApkCommand := apkBuilder.Command().
		BuiltTool("aapt2").
		Text("link").
		FlagWithOutput("-o ", apkPath).
		FlagWithArg("-A ", tmpAssetDir.String()).Implicit(stagedManifestPbPath)
	for _, lib := range libs {
		unsignedApkCommand.FlagWithInput("-I ", lib)
	}
	unsignedApkCommand.
		FlagWithArg("--min-sdk-version ", minSdkVersion).
		FlagWithArg("--version-code ", ctx.Config().PlatformSdkVersion().String()).
		FlagWithArg("--version-name ", ctx.Config().AppsDefaultVersionName()).
		FlagWithInput("--manifest ", manifestTemplatePath).
		Text(" --rename-manifest-package com.android.security.fsverity_metadata." + f.partitionName())

	// STEP 2-3: sign BuildManifest.apk
	pemPath, keyPath := ctx.Config().DefaultAppCertificate(ctx)
	apkBuilder.Command().
		BuiltTool("apksigner").
		Text("sign").
		FlagWithArg("--in ", apkPath.String()).
		FlagWithInput("--cert ", pemPath).
		FlagWithInput("--key ", keyPath).
		ImplicitOutput(idsigPath)
	apkBuilder.Build(fmt.Sprintf("%s_fsverity_apk", ctx.ModuleName()), "build fsverity apk")

	// STEP 2-4: Install the apk into the staging directory
	installedApkPath := rebasedDir.Join(ctx, "etc", "security", "fsverity", fmt.Sprintf("BuildManifest%s.apk", apkNameSuffix))
	installedIdsigPath := rebasedDir.Join(ctx, "etc", "security", "fsverity", fmt.Sprintf("BuildManifest%s.apk.idsig", apkNameSuffix))
	builder.Command().Text("mkdir -p").Text(filepath.Dir(installedApkPath.String()))
	builder.Command().Text("cp").Input(apkPath).Text(installedApkPath.String())
	builder.Command().Text("cp").Input(idsigPath).Text(installedIdsigPath.String())

	*fullInstallPaths = append(*fullInstallPaths, FullInstallPathInfo{
		SourcePath:      apkPath,
		FullInstallPath: android.PathForModuleInPartitionInstall(ctx, f.PartitionType(), fmt.Sprintf("etc/security/fsverity/BuildManifest%s.apk", apkNameSuffix)),
	})

	f.appendToEntry(ctx, installedApkPath)

	*fullInstallPaths = append(*fullInstallPaths, FullInstallPathInfo{
		SourcePath:      idsigPath,
		FullInstallPath: android.PathForModuleInPartitionInstall(ctx, f.PartitionType(), fmt.Sprintf("etc/security/fsverity/BuildManifest%s.apk.idsig", apkNameSuffix)),
	})

	f.appendToEntry(ctx, installedIdsigPath)
}
