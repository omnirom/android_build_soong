// Copyright 2025 Google Inc. All rights reserved.
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

package config

import (
	"strings"

	"android/soong/android"
)

var (
	// Common Windows Rust flags
	windowsRustFlags = []string{
		"-C dlltool=${cc_config.ClangBin}/llvm-dlltool",
		"-C split-debuginfo=packed",
		"-C link-self-contained=no",
		"-C panic=abort",
	}
	// Common Windows linker flags for Rust
	windowsRustLinkFlags = []string{
		"--sysroot ${cc_config.WindowsGccRoot}/${cc_config.WindowsGccTriple}",
		"-fuse-ld=lld",
	}

	// x86 specific flags
	windowsX86RustFlags   = []string{}
	windowsX8664RustFlags = []string{}

	// x86_64 specific flags
	windowsX86RustLinkFlags   = []string{}
	windowsX8664RustLinkFlags = []string{}
)

func init() {
	registerToolchainFactory(android.Windows, android.X86, windowsX86ToolchainFactory)
	registerToolchainFactory(android.Windows, android.X86_64, windowsX8664ToolchainFactory)

	pctx.StaticVariable("WindowsToolchainRustFlags", strings.Join(windowsRustFlags, " "))
	pctx.StaticVariable("WindowsToolchainRustLinkFlags", strings.Join(windowsRustLinkFlags, " "))

	pctx.StaticVariable("WindowsX86ToolchainRustFlags", strings.Join(windowsX86RustFlags, " "))
	pctx.StaticVariable("WindowsX86ToolchainRustLinkFlags", strings.Join(windowsX86RustLinkFlags, " "))
	pctx.StaticVariable("WindowsX8664ToolchainRustFlags", strings.Join(windowsX8664RustFlags, " "))
	pctx.StaticVariable("WindowsX8664ToolchainRustLinkFlags", strings.Join(windowsX8664RustLinkFlags, " "))
}

type toolchainWindowsX86 struct {
	toolchainBase
}

func (t *toolchainWindowsX86) Is64Bit() bool {
	return false
}

func (t *toolchainWindowsX86) LibclangRuntimeLibraryArch() string {
	return "i686"
}

func (t *toolchainWindowsX86) RlibSuffix() string {
	return ".rlib"
}

// Windows x86
func (t *toolchainWindowsX86) Name() string {
	return "x86"
}

func (t *toolchainWindowsX86) ToolchainLinkFlags() string {
	// Prepend the lld flags from cc_config so we stay in sync with cc
	return "${cc_config.WindowsLdflags} ${cc_config.WindowsX86Ldflags} ${cc_config.WindowsAvailableLibraries} " +
		"${config.WindowsToolchainRustLinkFlags} ${config.WindowsX86ToolchainRustLinkFlags}"
}

func (t *toolchainWindowsX86) ToolchainRustFlags() string {
	return "${config.WindowsToolchainRustFlags} ${config.WindowsX86ToolchainRustFlags}"
}

func (t *toolchainWindowsX86) RustTriple() string {
	return "i686-pc-windows-gnu"
}

func windowsX86ToolchainFactory(arch android.Arch) Toolchain {
	return toolchainWindowsX86Singleton
}

func (toolchainWindowsX86) Supported() bool {
	return true
}

func (toolchainWindowsX86) Bionic() bool {
	return false
}

func (toolchainWindowsX86) StaticLibSuffix() string {
	return ".a"
}

func (toolchainWindowsX86) SharedLibSuffix() string {
	return ".dll"
}

func (toolchainWindowsX86) ExecutableSuffix() string {
	return ".exe"
}

func (toolchainWindowsX86) DylibSuffix() string {
	return ".dylib.dll"
}

func (toolchainWindowsX86) ProcMacroSuffix() string {
	return ".dylib.dll"
}

// Windows x86_64
type toolchainWindowsX8664 struct {
	toolchainBase
}

func (t *toolchainWindowsX8664) Is64Bit() bool {
	return true
}

func (t *toolchainWindowsX8664) LibclangRuntimeLibraryArch() string {
	return "x86_64"
}

func (t *toolchainWindowsX8664) RlibSuffix() string {
	return ".rlib"
}

func (t *toolchainWindowsX8664) Name() string {
	return "x86_64"
}

func (t *toolchainWindowsX8664) ToolchainLinkFlags() string {
	// Prepend the lld flags from cc_config so we stay in sync with cc
	return "${cc_config.WindowsLdflags} ${cc_config.WindowsX8664Ldflags} ${cc_config.WindowsAvailableLibraries} " +
		"${config.WindowsToolchainRustLinkFlags} ${config.WindowsX8664ToolchainRustLinkFlags}"
}

func (t *toolchainWindowsX8664) ToolchainRustFlags() string {
	return "${config.WindowsToolchainRustFlags} ${config.WindowsX8664ToolchainRustFlags}"
}

func (t *toolchainWindowsX8664) RustTriple() string {
	return "x86_64-pc-windows-gnu"
}
func windowsX8664ToolchainFactory(arch android.Arch) Toolchain {
	return toolchainWindowsX8664Singleton
}

func (toolchainWindowsX8664) Supported() bool {
	return true
}

func (toolchainWindowsX8664) Bionic() bool {
	return false
}

func (toolchainWindowsX8664) StaticLibSuffix() string {
	return ".a"
}

func (toolchainWindowsX8664) SharedLibSuffix() string {
	return ".dll"
}

func (toolchainWindowsX8664) ExecutableSuffix() string {
	return ".exe"
}

func (toolchainWindowsX8664) DylibSuffix() string {
	return ".dylib.dll"
}

func (toolchainWindowsX8664) ProcMacroSuffix() string {
	return ".dylib.dll"
}

var toolchainWindowsX8664Singleton Toolchain = &toolchainWindowsX8664{}
var toolchainWindowsX86Singleton Toolchain = &toolchainWindowsX86{}
