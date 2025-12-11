// Copyright 2018 Google Inc. All rights reserved.
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

package cc

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"

	"android/soong/android"
)

// This singleton generates a compile_commands.json file. It does so for each
// blueprint Android.bp resulting in a cc.Module when either make, mm, mma, mmm
// or mmma is called. It will only create a single compile_commands.json file
// at ${OUT_DIR}/soong/development/ide/compdb/compile_commands.json. It will also symlink it
// to ${SOONG_LINK_COMPDB_TO} if set. In general this should be created by running
// make SOONG_GEN_COMPDB=1 nothing to get all targets.

func init() {
	android.RegisterParallelSingletonType("compdb_generator", compDBGeneratorSingleton)
}

func compDBGeneratorSingleton() android.Singleton {
	return &compdbGeneratorSingleton{}
}

type compdbGeneratorSingleton struct{}

const (
	compdbFilename                = "compile_commands.json"
	compdbOutputProjectsDirectory = "development/ide/compdb"

	// Environment variables used to modify behavior of this singleton.
	envVariableGenerateCompdb          = "SOONG_GEN_COMPDB"
	envVariableGenerateCompdbDebugInfo = "SOONG_GEN_COMPDB_DEBUG"
	envVariableCompdbLink              = "SOONG_LINK_COMPDB_TO"
)

// A compdb entry. The compile_commands.json file is a list of these.
type compDbEntry struct {
	Directory string   `json:"directory"`
	Arguments []string `json:"arguments"`
	File      string   `json:"file"`
	Output    string   `json:"output,omitempty"`
}

func (c *compdbGeneratorSingleton) GenerateBuildActions(ctx android.SingletonContext) {
	if !ctx.Config().IsEnvTrue(envVariableGenerateCompdb) {
		return
	}

	// Instruct the generator to indent the json file for easier debugging.
	outputCompdbDebugInfo := ctx.Config().IsEnvTrue(envVariableGenerateCompdbDebugInfo)

	// We only want one entry per file. We don't care what module/isa it's from
	m := make(map[string]compDbEntry)
	ctx.VisitAllModuleProxies(func(module android.ModuleProxy) {
		if ccModule, ok := android.OtherModuleProvider(ctx, module, CcInfoProvider); ok {
			if ccModule.CompilerInfo != nil {
				generateCompdbProject(ctx, module, ccModule, m)
			}
		}
	})

	// Create the output file.
	dir := android.PathForOutput(ctx, compdbOutputProjectsDirectory)
	os.MkdirAll(filepath.Join(android.AbsSrcDirForExistingUseCases(), dir.String()), 0777)
	compDBFile := dir.Join(ctx, compdbFilename)
	f, err := os.Create(filepath.Join(android.AbsSrcDirForExistingUseCases(), compDBFile.String()))
	if err != nil {
		log.Fatalf("Could not create file %s: %s", compDBFile, err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Fatalf("Could not close file %s: %s", compDBFile, err)
		}
	}()

	v := make([]compDbEntry, 0, len(m))
	for _, value := range m {
		v = append(v, value)
	}

	w := json.NewEncoder(f)
	if outputCompdbDebugInfo {
		w.SetIndent("", " ")
	}
	if err := w.Encode(v); err != nil {
		log.Fatalf("Failed to encode: %s", err)
	}

	if finalLinkDir := ctx.Config().Getenv(envVariableCompdbLink); finalLinkDir != "" {
		finalLinkPath := filepath.Join(finalLinkDir, compdbFilename)
		os.Remove(finalLinkPath)
		if err := os.Symlink(compDBFile.String(), finalLinkPath); err != nil {
			log.Fatalf("Unable to symlink %s to %s: %s", compDBFile, finalLinkPath, err)
		}
	}
}

func expandAllVars(ctx android.SingletonContext, args []string) []string {
	var out []string
	for _, arg := range args {
		if arg != "" {
			if val, err := evalAndSplitVariable(ctx, arg); err == nil {
				out = append(out, val...)
			} else {
				out = append(out, arg)
			}
		}
	}
	return out
}

func getArguments(ctx android.SingletonContext, src android.Path, ccModule *CcInfo, ccPath string, cxxPath string) []string {
	var args []string
	isCpp := false
	isAsm := false
	// TODO It would be better to ask soong for the types here.
	var clangPath string
	switch src.Ext() {
	case ".S", ".s", ".asm":
		isAsm = true
		isCpp = false
		clangPath = ccPath
	case ".c":
		isAsm = false
		isCpp = false
		clangPath = ccPath
	case ".cpp", ".cc", ".cxx", ".mm":
		isAsm = false
		isCpp = true
		clangPath = cxxPath
	case ".o":
		return nil
	default:
		log.Print("Unknown file extension " + src.Ext() + " on file " + src.String())
		isAsm = true
		isCpp = false
		clangPath = ccPath
	}
	args = append(args, clangPath)
	args = append(args, expandAllVars(ctx, ccModule.GlobalFlags.CommonFlags)...)
	args = append(args, expandAllVars(ctx, ccModule.LocalFlags.CommonFlags)...)
	args = append(args, expandAllVars(ctx, ccModule.GlobalFlags.CFlags)...)
	args = append(args, expandAllVars(ctx, ccModule.LocalFlags.CFlags)...)
	if isCpp {
		args = append(args, expandAllVars(ctx, ccModule.GlobalFlags.CppFlags)...)
		args = append(args, expandAllVars(ctx, ccModule.LocalFlags.CppFlags)...)
	} else if !isAsm {
		args = append(args, expandAllVars(ctx, ccModule.GlobalFlags.ConlyFlags)...)
		args = append(args, expandAllVars(ctx, ccModule.LocalFlags.ConlyFlags)...)
	}
	args = append(args, expandAllVars(ctx, ccModule.SystemIncludeFlags)...)
	args = append(args, expandAllVars(ctx, ccModule.NoOverrideFlags)...)
	args = append(args, src.String())
	return args
}

func generateCompdbProject(ctx android.SingletonContext, module android.ModuleProxy, ccModule *CcInfo, builds map[string]compDbEntry) {
	srcs := ccModule.CompilerInfo.Srcs
	if len(srcs) == 0 {
		return
	}

	pathToCC, err := ctx.Eval(pctx, "${config.ClangBin}")
	ccPath := "/bin/false"
	cxxPath := "/bin/false"
	if err == nil {
		ccPath = filepath.Join(pathToCC, "clang")
		cxxPath = filepath.Join(pathToCC, "clang++")
	}
	for _, src := range srcs {
		if _, ok := builds[src.String()]; !ok {
			args := getArguments(ctx, src, ccModule, ccPath, cxxPath)
			if args == nil {
				continue
			}
			builds[src.String()] = compDbEntry{
				Directory: android.AbsSrcDirForExistingUseCases(),
				Arguments: args,
				File:      src.String(),
			}
		}
	}
}

func evalAndSplitVariable(ctx android.SingletonContext, str string) ([]string, error) {
	evaluated, err := ctx.Eval(pctx, str)
	if err == nil {
		return strings.Fields(strings.Trim(evaluated, "'")), nil
	}
	return []string{""}, err
}
