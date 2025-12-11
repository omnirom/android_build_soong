//
// Copyright (C) 2021 The Android Open Source Project
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package rust

import (
	"path"
	"path/filepath"

	"android/soong/android"
	"android/soong/rust/config"

	"github.com/google/blueprint/proptools"
)

// This module is used to compile the rust toolchain libraries
// When RUST_PREBUILTS_VERSION is set, the library will generated
// from the given Rust version.
func init() {
	android.RegisterModuleType("rust_toolchain_library",
		rustToolchainLibraryFactory)
	android.RegisterModuleType("rust_toolchain_library_rlib",
		rustToolchainLibraryRlibFactory)
	android.RegisterModuleType("rust_toolchain_library_dylib",
		rustToolchainLibraryDylibFactory)
	android.RegisterModuleType("rust_toolchain_rustc_prebuilt",
		rustToolchainRustcPrebuiltFactory)
}

type toolchainLibraryProperties struct {
	// path to the toolchain crate root, relative to the top of the toolchain source
	Toolchain_crate_root *string `android:"arch_variant"`
	// path to the rest of the toolchain srcs, relative to the top of the toolchain source
	Toolchain_srcs []string `android:"arch_variant"`
}

type toolchainLibraryDecorator struct {
	*libraryDecorator
	Properties toolchainLibraryProperties
}

// toolchainCrateRoot implements toolchainCompiler.
func (t *toolchainLibraryDecorator) toolchainCrateRoot() *string {
	return t.Properties.Toolchain_crate_root
}

// toolchainSrcs implements toolchainCompiler.
func (t *toolchainLibraryDecorator) toolchainSrcs() []string {
	return t.Properties.Toolchain_srcs
}

// rust_toolchain_library produces all rust variants.
func rustToolchainLibraryFactory() android.Module {
	module, library := NewRustLibrary(android.HostAndDeviceSupported)
	library.BuildOnlyRust()

	return initToolchainLibrary(module, library)
}

// rust_toolchain_library_dylib produces a dylib.
func rustToolchainLibraryDylibFactory() android.Module {
	module, library := NewRustLibrary(android.HostAndDeviceSupported)
	library.BuildOnlyDylib()

	return initToolchainLibrary(module, library)
}

// rust_toolchain_library_rlib produces an rlib.
func rustToolchainLibraryRlibFactory() android.Module {
	module, library := NewRustLibrary(android.HostAndDeviceSupported)
	library.BuildOnlyRlib()

	return initToolchainLibrary(module, library)
}

func initToolchainLibrary(module *Module, library *libraryDecorator) android.Module {
	toolchainLibrary := &toolchainLibraryDecorator{
		libraryDecorator: library,
	}
	module.compiler = toolchainLibrary
	module.AddProperties(&toolchainLibrary.Properties)
	android.AddLoadHook(module, rustSetToolchainSource)

	return module.Init()
}

type toolchainCompiler interface {
	toolchainCrateRoot() *string
	toolchainSrcs() []string
}

var _ toolchainCompiler = (*toolchainLibraryDecorator)(nil)

func rustSetToolchainSource(ctx android.LoadHookContext) {
	if toolchainLib, ok := ctx.Module().(*Module).compiler.(toolchainCompiler); ok {
		prefix := filepath.Join("linux-x86", GetRustPrebuiltVersion(ctx))
		versionedCrateRoot := path.Join(prefix, android.String(toolchainLib.toolchainCrateRoot()))
		versionedSrcs := make([]string, len(toolchainLib.toolchainSrcs()))
		for i, src := range toolchainLib.toolchainSrcs() {
			versionedSrcs[i] = path.Join(prefix, src)
		}

		type props struct {
			Crate_root *string
			Srcs       []string
		}
		p := &props{}
		p.Crate_root = &versionedCrateRoot
		p.Srcs = versionedSrcs
		ctx.AppendProperties(p)
	} else {
		ctx.ModuleErrorf("Called rustSetToolchainSource on a non-Toolchain Module.")
	}
}

// GetRustPrebuiltVersion returns the RUST_PREBUILTS_VERSION env var, or the default version if it is not defined.
func GetRustPrebuiltVersion(ctx android.LoadHookContext) string {
	return ctx.Config().GetenvWithDefault("RUST_PREBUILTS_VERSION", config.RustDefaultVersion)
}

type toolchainRustcPrebuiltProperties struct {
	// path to rustc prebuilt, relative to the top of the toolchain source
	Toolchain_prebuilt_src *string
	// path to deps, relative to the top of the toolchain source
	Toolchain_deps []string
	// path to deps, relative to module directory
	Deps []string
}

func rustToolchainRustcPrebuiltFactory() android.Module {
	module := android.NewPrebuiltBuildTool()
	module.AddProperties(&toolchainRustcPrebuiltProperties{})
	android.AddLoadHook(module, func(ctx android.LoadHookContext) {
		var toolchainProps *toolchainRustcPrebuiltProperties
		for _, p := range ctx.Module().GetProperties() {
			toolchainProperties, ok := p.(*toolchainRustcPrebuiltProperties)
			if ok {
				toolchainProps = toolchainProperties
			}
		}

		if toolchainProps.Toolchain_prebuilt_src == nil {
			ctx.PropertyErrorf("toolchain_prebuilt_src", "must set path to rustc prebuilt")
		}

		prefix := filepath.Join(config.HostPrebuiltTag(ctx.Config()), GetRustPrebuiltVersion(ctx))
		deps := make([]string, 0, len(toolchainProps.Toolchain_deps)+len(toolchainProps.Deps))
		for _, d := range toolchainProps.Toolchain_deps {
			deps = append(deps, path.Join(prefix, d))
		}
		deps = append(deps, toolchainProps.Deps...)

		props := struct {
			Src  *string
			Deps []string
		}{
			Src:  proptools.StringPtr(path.Join(prefix, *toolchainProps.Toolchain_prebuilt_src)),
			Deps: deps,
		}
		ctx.AppendProperties(&props)
	})
	return module
}
