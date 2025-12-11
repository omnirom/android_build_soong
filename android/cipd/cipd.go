// Copyright 2025 The Android Open Source Project
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

package cipd

import (
	"fmt"

	"android/soong/android"

	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"
)

func init() {
	RegisterCipdComponents(android.InitRegistrationContext)
}

func RegisterCipdComponents(ctx android.RegistrationContext) {
	ctx.RegisterModuleType("cipd_package", cipdPackageFactory)
}

var (
	pctx = android.NewPackageContext("android/cipd")

	PrebuiltOS = pctx.VariableConfigMethod("PrebuiltOS", android.Config.PrebuiltOS)
	_          = pctx.SourcePathVariable("cipd", "prebuilts/cipd/${PrebuiltOS}/cipd")
	soong_zip  = pctx.HostBinToolVariable("soong_zip", "soong_zip")

	// CIPD can be expensive for network and disk i/o, so limit the number of concurrent
	// fetches.
	cipdPool = pctx.StaticPool("cipdPool", blueprint.PoolParams{
		Depth: 8,
	})
	// cipd will proxy its requests out of the build sandbox using the unix domain socket
	// set up in build/soong/ui/build/cipd.go.
	cipdExportRule = pctx.AndroidStaticRule("cipd_export",
		blueprint.RuleParams{
			Command:     "rm -rf $root && $cipd export -ensure-file $in -root $root",
			CommandDeps: []string{"$cipd"},
			Pool:        cipdPool,
		}, "root",
	)

	soongZipFromDirRule = pctx.AndroidStaticRule("soong_zip_from_dir",
		blueprint.RuleParams{
			Command: "rm -rf $tempZipDir && " +
				"$cipd export -ensure-file $in -root $tempZipDir && " +
				"$soong_zip -write_if_changed -o $out -C $tempZipDir -D $tempZipDir && " +
				"rm -rf $tempZipDir",
			CommandDeps: []string{"$cipd", "$soong_zip"},
			Pool:        cipdPool,
			Restat:      true,
		}, "tempZipDir",
	)
)

type cipdPackageProperties struct {
	// The name of the cipd package, like "android/prebuilts/GmsCorePrebuilt/arm64"
	Package proptools.Configurable[string]

	// The version tag of the package.
	Version proptools.Configurable[string]

	// A file containing pinned cipd instance ids. It must contain the package version
	// specified.
	Resolved_versions_file string `android:"path"`

	// The files expected to exist in the CIPD package.
	Files []string
}

type cipdPackageModule struct {
	android.ModuleBase

	properties cipdPackageProperties
}

func (p *cipdPackageModule) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	ensureFile := android.PathForModuleOut(ctx, "ensure.txt")
	outPath := android.PathForModuleOut(ctx, "package")

	// The resolved versions file should be relative to the ensure file, so
	// copy it to the output directory as well.
	const resolvedVersionsTxt = "resolved_versions.txt"
	resolvedVersionsFile := android.PathForModuleOut(ctx, resolvedVersionsTxt)
	android.CopyFileRule(ctx,
		android.PathForModuleSrc(ctx, p.properties.Resolved_versions_file),
		resolvedVersionsFile.OutputPath)

	ensureContents := fmt.Sprintf("$ResolvedVersions %s\n", resolvedVersionsTxt)
	version := p.properties.Version.Get(ctx)
	packageProp := p.properties.Package.Get(ctx)
	ensureContents += fmt.Sprintf("%s %s\n", packageProp.Get(), version.Get())
	android.WriteFileRule(ctx, ensureFile, ensureContents)

	if len(p.properties.Files) > 0 {
		outFiles := make(android.WritablePaths, len(p.properties.Files))
		for i, f := range p.properties.Files {
			outFiles[i] = outPath.Join(ctx, f)
		}

		ctx.Build(pctx, android.BuildParams{
			Rule:     cipdExportRule,
			Input:    ensureFile,
			Outputs:  outFiles,
			Implicit: resolvedVersionsFile,
			Args: map[string]string{
				"root": outPath.String(),
			},
		})
		ctx.SetOutputFiles(outFiles.Paths(), "")
	}

	outputZipFile := android.PathForModuleOut(ctx, "package.zip")
	tempZipDir := android.PathForModuleOut(ctx, "zip_temp_pkg_dir")
	// This rule runs `cipd export` (potentially again) to ensure the zip is
	// creatabled regardless of whether individual files are also requested.
	ctx.Build(pctx, android.BuildParams{
		Rule:     soongZipFromDirRule,
		Input:    ensureFile,
		Output:   outputZipFile,
		Implicit: resolvedVersionsFile,
		Args: map[string]string{
			"tempZipDir": tempZipDir.String(),
		},
	})
	ctx.SetOutputFiles(android.Paths{outputZipFile}, ".zip")
}

// cipd_package module installs the given CIPD package version.
func cipdPackageFactory() android.Module {
	module := &cipdPackageModule{}
	module.AddProperties(&module.properties)
	android.InitAndroidModule(module)
	return module
}
