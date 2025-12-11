//
// Copyright (C) 2025 The Android Open Source Project
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
	"android/soong/android"
)

// This module is used to compile the rust toolchain objects
// When RUST_PREBUILTS_VERSION is set, the object will generated
// from the given Rust version.
func init() {
	android.RegisterModuleType("rust_toolchain_object",
		rustToolchainObjectFactory)
	android.RegisterModuleType("rust_toolchain_object_host",
		rustToolchainObjectHostFactory)
}

type toolchainObjectProperties struct {
	// path to the toolchain object crate root, relative to the top of the toolchain source
	Toolchain_crate_root *string `android:"arch_variant"`
	// path to the rest of the toolchain srcs, relative to the top of the toolchain source
	Toolchain_srcs []string `android:"arch_variant"`
}

type toolchainObjectDecorator struct {
	*objectDecorator
	Properties toolchainObjectProperties
}

// toolchainCrateRoot implements toolchainCompiler.
func (t *toolchainObjectDecorator) toolchainCrateRoot() *string {
	return t.Properties.Toolchain_crate_root
}

// toolchainSrcs implements toolchainCompiler.
func (t *toolchainObjectDecorator) toolchainSrcs() []string {
	return t.Properties.Toolchain_srcs
}

// rust_toolchain_library produces all rust variants.
func rustToolchainObjectFactory() android.Module {
	module, object := NewRustObject(android.HostAndDeviceSupported)
	return initToolchainObject(module, object)
}

// rust_toolchain_library produces all rust variants.
func rustToolchainObjectHostFactory() android.Module {
	module, object := NewRustObject(android.HostSupported)
	return initToolchainObject(module, object)
}

func initToolchainObject(module *Module, object *objectDecorator) android.Module {
	toolchainObject := &toolchainObjectDecorator{
		objectDecorator: object,
	}
	module.compiler = toolchainObject
	module.AddProperties(&toolchainObject.Properties)

	android.AddLoadHook(module, rustSetToolchainSource)

	return module.Init()
}

func (t *toolchainObjectDecorator) compilerProps() []any {
	return append(t.objectDecorator.compilerProps(), &t.Properties)
}

func (t *toolchainObjectDecorator) crt() bool {
	return t.objectDecorator.crt()
}

func (t *toolchainObjectDecorator) install(ctx ModuleContext) {
	// Objects aren't installable, so do nothing.
}

func (t *toolchainObjectDecorator) everInstallable() bool {
	// Objects aren't installable.
	return false
}

var _ toolchainCompiler = (*toolchainObjectDecorator)(nil)
