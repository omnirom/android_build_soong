// Copyright 2020 Google Inc. All rights reserved.
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

package rust

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/blueprint/proptools"

	"android/soong/android"
	"android/soong/rust/config"
)

// This singleton collects Rust crate definitions and generates a JSON file
// (${OUT_DIR}/soong/rust-project.json) which can be use by external tools,
// such as rust-analyzer. It does so when either make, mm, mma, mmm or mmma is
// called.  This singleton is enabled only if SOONG_GEN_RUST_PROJECT is set.
// For example,
//
//   $ SOONG_GEN_RUST_PROJECT=1 SOONG_LINK_RUST_PROJECT_TO=${ANDROID_BUILD_TOP} m nothing

const (
	// Environment variables used to control the behavior of this singleton.
	envVariableCollectRustDeps = "SOONG_GEN_RUST_PROJECT"
	envVariableUseKythe        = "XREF_CORPUS"
	rustProjectJsonFileName    = "rust-project.json"
	rustTargetMappingFileName  = "rust-target-mapping.json"
)

// The format of rust-project.json is not yet finalized. A current description is available at:
// https://github.com/rust-analyzer/rust-analyzer/blob/master/docs/user/manual.adoc#non-cargo-based-projects
type rustProjectDep struct {
	// The Crate attribute is the index of the dependency in the Crates array in rustProjectJson.
	Crate int    `json:"crate"`
	Name  string `json:"name"`
}

type rustProjectCrate struct {
	DisplayName    string                 `json:"display_name"`
	RootModule     string                 `json:"root_module"`
	Edition        string                 `json:"edition,omitempty"`
	Deps           []rustProjectDep       `json:"deps"`
	Cfg            []string               `json:"cfg"`
	Env            map[string]string      `json:"env"`
	ProcMacro      bool                   `json:"is_proc_macro"`
	ProcMacroDylib *string                `json:"proc_macro_dylib_path"`
	Source         rustProjectIncludeDirs `json:"source"`
}

type rustProjectJson struct {
	Sysroot string             `json:"sysroot"`
	Crates  []rustProjectCrate `json:"crates"`
}

type rustTargetMappingsJson []rustTargetMappingJson

type rustTargetMappingJson struct {
	Name               string `json:"name"`
	BuildTarget        string `json:"build_target"`
	CheckTarget        string `json:"check_target"`
	SourceDir          string `json:"source_dir"`
	TargetProduct      string `json:"TARGET_PRODUCT"`
	TargetRelease      string `json:"TARGET_RELEASE"`
	TargetBuildVariant string `json:"TARGET_BUILD_VARIANT"`
}

type rustProjectIncludeDirs struct {
	Include_dirs []string `json:"include_dirs"`
	Exclude_dirs []string `json:"exclude_dirs"`
}

// crateInfo is used during the processing to keep track of the known crates.
type crateInfo struct {
	Idx    int            // Index of the crate in rustProjectJson.Crates slice.
	Deps   map[string]int // The keys are the module names and not the crate names.
	Device bool           // True if the crate at idx was a device crate
}

type projectGeneratorSingleton struct {
	project        rustProjectJson
	targetMappings rustTargetMappingsJson
	knownCrates    map[string]crateInfo // Keys are module names.
}

func rustProjectGeneratorSingleton() android.Singleton {
	return &projectGeneratorSingleton{}
}

func init() {
	android.RegisterParallelSingletonType("rust_project_generator", rustProjectGeneratorSingleton)
}

// mergeDependencies visits all the dependencies for module and updates crate and deps
// with any new dependency.
func (singleton *projectGeneratorSingleton) mergeDependencies(ctx android.SingletonContext,
	module android.ModuleProxy, crate *rustProjectCrate, deps map[string]int) {

	ctx.VisitDirectDepsProxies(module, func(child android.ModuleProxy) {
		// Skip intra-module dependencies (i.e., generated-source library depending on the source variant).
		if module.Name() == child.Name() {
			return
		}
		// Skip unsupported modules.
		rustInfo, commonInfo, ok := isModuleSupported(ctx, child)
		if !ok {
			return
		}
		// For unknown dependency, add it first.
		var childId int
		cInfo, known := singleton.knownCrates[child.Name()]
		if !known {
			childId, ok = singleton.addCrate(ctx, child, rustInfo, commonInfo)
			if !ok {
				return
			}
		} else {
			childId = cInfo.Idx
		}
		// Is this dependency known already?
		if _, ok = deps[child.Name()]; ok {
			return
		}
		crate.Deps = append(crate.Deps, rustProjectDep{Crate: childId, Name: rustInfo.CompilerInfo.CrateName})
		deps[child.Name()] = childId
	})
}

// isModuleSupported returns the RustModule if the module
// should be considered for inclusion in rust-project.json.
func isModuleSupported(ctx android.SingletonContext, module android.ModuleProxy) (*RustInfo, *android.CommonModuleInfo, bool) {
	info, ok := android.OtherModuleProvider(ctx, module, RustInfoProvider)
	if !ok {
		return nil, nil, false
	}
	commonInfo := android.OtherModuleProviderOrDefault(ctx, module, android.CommonModuleInfoProvider)
	if !commonInfo.Enabled {
		return nil, nil, false
	}
	return info, commonInfo, true
}

// addCrate adds a crate to singleton.project.Crates ensuring that required
// dependencies are also added. It returns the index of the new crate in
// singleton.project.Crates
func (singleton *projectGeneratorSingleton) addCrate(ctx android.SingletonContext, module android.ModuleProxy, rustInfo *RustInfo,
	commonInfo *android.CommonModuleInfo) (int, bool) {

	deps := make(map[string]int)

	var procMacroDylib *string = nil

	if procMacro := rustInfo.ProcMacroInfo; procMacro != nil {
		procMacroDylib = proptools.StringPtr(procMacro.Dylib.String())
	}

	crate := rustProjectCrate{
		DisplayName:    module.Name(),
		RootModule:     rustInfo.CompilerInfo.CrateRootPath.String(),
		Edition:        rustInfo.CompilerInfo.Edition,
		Deps:           make([]rustProjectDep, 0),
		Cfg:            make([]string, 0),
		Env:            make(map[string]string),
		ProcMacro:      procMacroDylib != nil,
		ProcMacroDylib: procMacroDylib,
		Source: rustProjectIncludeDirs{
			Include_dirs: []string{ctx.ModuleDir(module)},
			Exclude_dirs: []string{},
		}, // TODO: What should this value be?
	}

	if cargoOutDir := rustInfo.CompilerInfo.CargoOutDir; cargoOutDir.Valid() {
		crate.Env["OUT_DIR"] = cargoOutDir.String()
	}

	for _, feature := range rustInfo.CompilerInfo.Features {
		crate.Cfg = append(crate.Cfg, "feature=\""+feature+"\"")
	}

	singleton.mergeDependencies(ctx, module, &crate, deps)

	var idx int
	if cInfo, ok := singleton.knownCrates[module.Name()]; ok {
		idx = cInfo.Idx
		singleton.project.Crates[idx] = crate
	} else {
		idx = len(singleton.project.Crates)
		singleton.project.Crates = append(singleton.project.Crates, crate)
	}
	singleton.knownCrates[module.Name()] = crateInfo{Idx: idx, Deps: deps, Device: commonInfo.Target.Os.Class == android.Device}
	outputfiles := android.OutputFilesForModule(ctx, module, "")
	// Count the number of output files that should be used for mapping
	numberValidOutputFiles := 0
	validOutputIndex := 0
	for ix, item := range outputfiles {
		if !(strings.HasSuffix(item.String(), ".rs") || strings.HasSuffix(item.String(), ".c") || strings.HasSuffix(item.String(), ".d")) {
			validOutputIndex = ix
			numberValidOutputFiles += 1
		}
	}

	buildVariant := "user"
	if ctx.Config().Eng() {
		buildVariant = "eng"
	} else if ctx.Config().Debuggable() {
		buildVariant = "userdebug"
	}
	if numberValidOutputFiles == 1 {
		outputFileName := outputfiles[validOutputIndex].String()
		mapping := rustTargetMappingJson{
			Name:               module.Name(),
			BuildTarget:        outputFileName,
			CheckTarget:        outputFileName + ".checkJson",
			SourceDir:          ctx.ModuleDir(module),
			TargetProduct:      ctx.Config().DeviceProduct(),
			TargetRelease:      "trunk_staging",
			TargetBuildVariant: buildVariant,
		}
		singleton.targetMappings = append(singleton.targetMappings, mapping)
	} else if numberValidOutputFiles > 1 {
		ctx.ModuleErrorf(module, "%s", "More than one file for an output in the module")
	}
	return idx, true
}

// appendCrateAndDependencies creates a rustProjectCrate for the module argument and appends it to singleton.project.
// It visits the dependencies of the module depth-first so the dependency ID can be added to the current module. If the
// current module is already in singleton.knownCrates, its dependencies are merged.
func (singleton *projectGeneratorSingleton) appendCrateAndDependencies(ctx android.SingletonContext, module android.ModuleProxy) {
	rustInfo, commonInfo, ok := isModuleSupported(ctx, module)
	if !ok {
		return
	}
	// If we have seen this crate already; merge any new dependencies.
	if cInfo, ok := singleton.knownCrates[module.Name()]; ok {
		// If we have a new device variant, override the old one
		if !cInfo.Device && commonInfo.Target.Os.Class == android.Device {
			singleton.addCrate(ctx, module, rustInfo, commonInfo)
			return
		}
		crate := singleton.project.Crates[cInfo.Idx]
		singleton.mergeDependencies(ctx, module, &crate, cInfo.Deps)
		singleton.project.Crates[cInfo.Idx] = crate
		return
	}
	singleton.addCrate(ctx, module, rustInfo, commonInfo)
}

func (singleton *projectGeneratorSingleton) GenerateBuildActions(ctx android.SingletonContext) {
	if !(ctx.Config().IsEnvTrue(envVariableCollectRustDeps) || ctx.Config().Getenv(envVariableUseKythe) != "") {
		return
	}

	singleton.project.Sysroot = config.RustPath(ctx)

	singleton.knownCrates = make(map[string]crateInfo)
	ctx.VisitAllModuleProxies(func(module android.ModuleProxy) {
		singleton.appendCrateAndDependencies(ctx, module)
	})

	path := android.PathForOutput(ctx, rustProjectJsonFileName)
	err := createJsonFile(singleton.project, path)
	if err != nil {
		ctx.Errorf(err.Error())

	}
	if ctx.Config().XrefCorpusName() != "" {
		rule := android.NewRuleBuilder(pctx, ctx)
		jsonPath := android.PathForOutput(ctx, "rust-project.json")
		kzipPath := android.PathForOutput(ctx, "rust-project.kzip")
		vnames := android.PathForSource(ctx, "build/soong/vnames.json")
		rule.Command().PrebuiltBuildTool(ctx, "rust_project_to_kzip").
			FlagWithInput("-project_json ", jsonPath).
			FlagWithOutput("-output ", kzipPath).
			FlagWithArg("-corpus ", ctx.Config().XrefCorpusName()).
			FlagWithArg("-root ", ".").
			FlagWithInput("-vnames_json_path ", vnames)
		ruleName := "rust3-extraction"
		rule.Build(ruleName, "Turning Rust project into kzips")
		ctx.Phony("xref_rust", kzipPath)
	}

	rustTargetMappingPath := android.PathForOutput(ctx, rustTargetMappingFileName)
	if err := createJsonFile(singleton.targetMappings, rustTargetMappingPath); err != nil {
		ctx.Errorf(err.Error())
	}

}

func createJsonFile(project any, rustProjectPath android.WritablePath) error {
	buf, err := json.MarshalIndent(project, "", "  ")
	if err != nil {
		return fmt.Errorf("JSON marshal failed: %s", err)
	}

	if err := android.WriteFileToOutputDir(rustProjectPath, buf, 0666); err != nil {
		return fmt.Errorf("Writing rust-project to %s failed: %s", rustProjectPath.String(), err)
	}
	return nil
}
