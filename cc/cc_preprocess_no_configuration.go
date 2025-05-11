// Copyright 2024 Google Inc. All rights reserved.
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
	"android/soong/android"
	"slices"
	"strings"
)

func init() {
	RegisterCCPreprocessNoConfiguration(android.InitRegistrationContext)
}

func RegisterCCPreprocessNoConfiguration(ctx android.RegistrationContext) {
	ctx.RegisterModuleType("cc_preprocess_no_configuration", ccPreprocessNoConfigurationFactory)
}

// cc_preprocess_no_configuration modules run the c preprocessor on a single input source file.
// They also have "no configuration", meaning they don't have an arch or os associated with them,
// they should be thought of as pure textual transformations of the input file. In some cases this
// is good, in others you might want to do different transformations depending on what arch the
// result will be compiled in, in which case you can use cc_object instead of this module.
func ccPreprocessNoConfigurationFactory() android.Module {
	m := &ccPreprocessNoConfiguration{}
	m.AddProperties(&m.properties)
	android.InitAndroidModule(m)
	return m
}

type ccPreprocessNoConfigurationProps struct {
	// Called Srcs for consistency with the other cc module types, but only accepts 1 input source
	// file.
	Srcs []string `android:"path"`
	// The flags to pass to the c compiler. Must include -E in order to enable preprocessing-only
	// mode.
	Cflags []string `android:"path"`
}

type ccPreprocessNoConfiguration struct {
	android.ModuleBase
	properties ccPreprocessNoConfigurationProps
}

func (m *ccPreprocessNoConfiguration) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	srcs := android.PathsForModuleSrc(ctx, m.properties.Srcs)
	if len(srcs) != 1 {
		ctx.PropertyErrorf("Srcs", "cc_preprocess_no_configuration only accepts 1 source file, found: %v", srcs.Strings())
		return
	}
	src := srcs[0]

	hasE := false
	for _, cflag := range m.properties.Cflags {
		if cflag == "-E" {
			hasE = true
			break
		} else if cflag == "-P" || strings.HasPrefix(cflag, "-D") {
			// do nothing, allow it
		} else {
			ctx.PropertyErrorf("Cflags", "cc_preprocess_no_configuration only allows -D and -P flags, found: %q", cflag)
			return
		}
	}
	if !hasE {
		ctx.PropertyErrorf("Cflags", "cc_preprocess_no_configuration must have a -E cflag")
		return
	}

	cflags := slices.Clone(m.properties.Cflags)

	// Match behavior of other cc modules:
	// https://cs.android.com/android/platform/superproject/main/+/main:build/soong/cc/compiler.go;l=422;drc=7297f05ee8cda422ccb32c4af4d9d715d6bac10e
	cflags = append(cflags, "-I"+ctx.ModuleDir())

	var ccCmd string
	switch src.Ext() {
	case ".c":
		ccCmd = "clang"
	case ".cpp", ".cc", ".cxx", ".mm":
		ccCmd = "clang++"
	default:
		ctx.PropertyErrorf("srcs", "File %s has unknown extension. Supported extensions: .c, .cpp, .cc, .cxx, .mm", src)
		return
	}

	ccCmd = "${config.ClangBin}/" + ccCmd

	outFile := android.PathForModuleOut(ctx, src.Base())

	ctx.Build(pctx, android.BuildParams{
		Rule:        cc,
		Description: ccCmd + " " + src.Rel(),
		Output:      outFile,
		Input:       src,
		Args: map[string]string{
			"cFlags": strings.Join(cflags, " "),
			"ccCmd":  ccCmd,
		},
	})

	ctx.SetOutputFiles([]android.Path{outFile}, "")
}
