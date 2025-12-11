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

package android

import (
	"github.com/google/blueprint"
)

//go:generate go run ../../blueprint/gobtools/codegen/gob_gen.go

func init() {
	pctx.HostBinToolVariable("symbols_map", "symbols_map")
}

var zipFiles = pctx.AndroidStaticRule("SnapshotZipFiles", blueprint.RuleParams{
	Command:        `${SoongZipCmd}  -r $out.rsp -o $out`,
	CommandDeps:    []string{"${SoongZipCmd}"},
	Rspfile:        "$out.rsp",
	RspfileContent: "$in",
})

var mergeSymbolsMapProtos = pctx.AndroidStaticRule("merge_symbol_map_protos", blueprint.RuleParams{
	Command:        `${symbols_map} -merge $out @$out.rsp`,
	CommandDeps:    []string{"${symbols_map}"},
	Rspfile:        "$out.rsp",
	RspfileContent: "$in",
})

// Provider for generating symbols.zip
// @auto-generate: gob
type SymbolicOutputInfo struct {
	UnstrippedOutputFile Path
	SymbolicOutputPath   InstallPath
	ElfMappingProtoPath  InstallPath
}

// @auto-generate: gob
type SymbolicOutputInfos []*SymbolicOutputInfo

// SymbolInfosProvider provides necessary information to generate the symbols.zip
var SymbolInfosProvider = blueprint.NewProvider[SymbolicOutputInfos]()

func (s *SymbolicOutputInfos) SortedUniqueSymbolicOutputPaths() Paths {
	ret := make(Paths, len(*s))
	for i, info := range *s {
		ret[i] = info.SymbolicOutputPath
	}
	return SortedUniquePaths(ret)
}

func (s *SymbolicOutputInfos) SortedUniqueElfMappingProtoPaths() Paths {
	ret := make(Paths, len(*s))
	for i, info := range *s {
		ret[i] = info.ElfMappingProtoPath
	}
	return SortedUniquePaths(ret)
}

// symbolsContext allows calling [BuildSymbolsZip] in both modules and singletons
type symbolsContext interface {
	OtherModuleProviderContext
	BuilderContext
}

// Defines the build rules to generate the symbols.zip file and the merged elf mapping textproto
// file. Modules in depModules that provide [SymbolInfosProvider] and are exported to make
// will be listed in the symbols.zip and the merged proto file.
func BuildSymbolsZip(ctx symbolsContext, depModules []ModuleProxy, extraSymbols *SymbolicOutputInfos, symbolsZipFile, mergedMappingProtoFile WritablePath) (Paths, Paths) {
	var allSymbolicOutputPaths, allElfMappingProtoPaths Paths
	for _, mod := range depModules {
		if commonInfo, _ := OtherModuleProvider(ctx, mod, CommonModuleInfoProvider); commonInfo.SkipAndroidMkProcessing {
			continue
		}
		if symbolInfos, ok := OtherModuleProvider(ctx, mod, SymbolInfosProvider); ok {
			allSymbolicOutputPaths = append(allSymbolicOutputPaths, symbolInfos.SortedUniqueSymbolicOutputPaths()...)
			allElfMappingProtoPaths = append(allElfMappingProtoPaths, symbolInfos.SortedUniqueElfMappingProtoPaths()...)
		}
	}
	if extraSymbols != nil {
		allSymbolicOutputPaths = append(allSymbolicOutputPaths, extraSymbols.SortedUniqueSymbolicOutputPaths()...)
		allElfMappingProtoPaths = append(allElfMappingProtoPaths, extraSymbols.SortedUniqueElfMappingProtoPaths()...)
	}
	allSymbolicOutputPaths = SortedUniquePaths(allSymbolicOutputPaths)
	allElfMappingProtoPaths = SortedUniquePaths(allElfMappingProtoPaths)

	ctx.Build(pctx, BuildParams{
		Rule:   zipFiles,
		Inputs: allSymbolicOutputPaths,
		Output: symbolsZipFile,
	})

	ctx.Build(pctx, BuildParams{
		Rule:   mergeSymbolsMapProtos,
		Output: mergedMappingProtoFile,
		Inputs: allElfMappingProtoPaths,
	})

	return allSymbolicOutputPaths, allElfMappingProtoPaths
}
