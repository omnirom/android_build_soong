// Copyright 2019 The Android Open Source Project
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
	"strings"
	"testing"

	"android/soong/android"
)

// Test that variants are being generated correctly, and that crate-types are correct.
func TestLibraryVariants(t *testing.T) {

	ctx := testRust(t, `
		rust_library_host {
			name: "libfoo",
			srcs: ["foo.rs"],
			crate_name: "foo",
		}
		rust_ffi_host {
			name: "libfoo.ffi",
			srcs: ["foo.rs"],
			crate_name: "foo"
		}
		rust_ffi_host_static {
			name: "libfoo.ffi_static",
			srcs: ["foo.rs"],
			crate_name: "foo"
		}`)

	// Test all variants are being built.
	libfooRlib := ctx.ModuleForTests(t, "libfoo", "linux_glibc_x86_64_rlib_rlib-std").Rule("rustc")
	libfooDylib := ctx.ModuleForTests(t, "libfoo", "linux_glibc_x86_64_dylib").Rule("rustc")
	libfooFFIRlib := ctx.ModuleForTests(t, "libfoo.ffi", "linux_glibc_x86_64_rlib_rlib-std").Rule("rustc")
	libfooShared := ctx.ModuleForTests(t, "libfoo.ffi", "linux_glibc_x86_64_shared").Rule("rustc")

	rlibCrateType := "rlib"
	dylibCrateType := "dylib"
	sharedCrateType := "cdylib"

	// Test crate type for rlib is correct.
	if !strings.Contains(libfooRlib.Args["rustcFlags"], "crate-type="+rlibCrateType) {
		t.Errorf("missing crate-type for static variant, expecting %#v, rustcFlags: %#v", rlibCrateType, libfooRlib.Args["rustcFlags"])
	}

	// Test crate type for dylib is correct.
	if !strings.Contains(libfooDylib.Args["rustcFlags"], "crate-type="+dylibCrateType) {
		t.Errorf("missing crate-type for static variant, expecting %#v, rustcFlags: %#v", dylibCrateType, libfooDylib.Args["rustcFlags"])
	}

	// Test crate type for FFI rlibs is correct
	if !strings.Contains(libfooFFIRlib.Args["rustcFlags"], "crate-type="+rlibCrateType) {
		t.Errorf("missing crate-type for static variant, expecting %#v, rustcFlags: %#v", rlibCrateType, libfooFFIRlib.Args["rustcFlags"])
	}

	// Test crate type for C shared libraries is correct.
	if !strings.Contains(libfooShared.Args["rustcFlags"], "crate-type="+sharedCrateType) {
		t.Errorf("missing crate-type for shared variant, expecting %#v, got rustcFlags: %#v", sharedCrateType, libfooShared.Args["rustcFlags"])
	}

}

// Test that dylibs are not statically linking the standard library.
func TestDylibPreferDynamic(t *testing.T) {
	ctx := testRust(t, `
		rust_library_host_dylib {
			name: "libfoo",
			srcs: ["foo.rs"],
			crate_name: "foo",
		}`)

	libfooDylib := ctx.ModuleForTests(t, "libfoo", "linux_glibc_x86_64_dylib").Rule("rustc")

	if !strings.Contains(libfooDylib.Args["rustcFlags"], "prefer-dynamic") {
		t.Errorf("missing prefer-dynamic flag for libfoo dylib, rustcFlags: %#v", libfooDylib.Args["rustcFlags"])
	}
}

func TestValidateLibraryStem(t *testing.T) {
	testRustError(t, "crate_name must be defined.", `
			rust_library_host {
				name: "libfoo",
				srcs: ["foo.rs"],
			}`)

	testRustError(t, "library crate_names must be alphanumeric with underscores allowed", `
			rust_library_host {
				name: "libfoo-bar",
				srcs: ["foo.rs"],
				crate_name: "foo-bar"
			}`)

	testRustError(t, "Invalid name or stem property; library filenames must start with lib<crate_name>", `
			rust_library_host {
				name: "foobar",
				srcs: ["foo.rs"],
				crate_name: "foo_bar"
			}`)
	testRustError(t, "Invalid name or stem property; library filenames must start with lib<crate_name>", `
			rust_library_host {
				name: "foobar",
				stem: "libfoo",
				srcs: ["foo.rs"],
				crate_name: "foo_bar"
			}`)
	testRustError(t, "Invalid name or stem property; library filenames must start with lib<crate_name>", `
			rust_library_host {
				name: "foobar",
				stem: "foo_bar",
				srcs: ["foo.rs"],
				crate_name: "foo_bar"
			}`)

}

func TestSharedLibrary(t *testing.T) {
	ctx := testRust(t, `
		rust_ffi_shared {
			name: "libfoo",
			srcs: ["foo.rs"],
			crate_name: "foo",
		}`)

	libfoo := ctx.ModuleForTests(t, "libfoo", "android_arm64_armv8-a_shared")

	libfooOutput := libfoo.Rule("rustc")
	if !strings.Contains(libfooOutput.Args["linkFlags"], "-Wl,-soname=libfoo.so") {
		t.Errorf("missing expected -Wl,-soname linker flag for libfoo shared lib, linkFlags: %#v",
			libfooOutput.Args["linkFlags"])
	}

	if !android.InList("libstd", libfoo.Module().(*Module).Properties.AndroidMkDylibs) {
		t.Errorf("Non-static libstd dylib expected to be a dependency of Rust shared libraries. Dylib deps are: %#v",
			libfoo.Module().(*Module).Properties.AndroidMkDylibs)
	}
}

func TestSharedLibraryToc(t *testing.T) {
	ctx := testRust(t, `
		rust_ffi_shared {
			name: "libfoo",
			srcs: ["foo.rs"],
			crate_name: "foo",
		}
		cc_binary {
			name: "fizzbuzz",
			shared_libs: ["libfoo"],
		}`)

	fizzbuzz := ctx.ModuleForTests(t, "fizzbuzz", "android_arm64_armv8-a").Rule("ld")

	if !android.SuffixInList(fizzbuzz.Implicits.Strings(), "libfoo.so.toc") {
		t.Errorf("missing expected libfoo.so.toc implicit dependency, instead found: %#v",
			fizzbuzz.Implicits.Strings())
	}
}

func TestStaticLibraryLinkage(t *testing.T) {
	ctx := testRust(t, `
		rust_ffi_static {
			name: "libfoo",
			srcs: ["foo.rs"],
			crate_name: "foo",
		}`)

	libfoo := ctx.ModuleForTests(t, "libfoo", "android_arm64_armv8-a_rlib_rlib-std")

	if !android.InList("libstd", libfoo.Module().(*Module).Properties.AndroidMkRlibs) {
		t.Errorf("Static libstd rlib expected to be a dependency of Rust rlib libraries. Rlib deps are: %#v",
			libfoo.Module().(*Module).Properties.AndroidMkDylibs)
	}
}

func TestNativeDependencyOfRlib(t *testing.T) {
	ctx := testRust(t, `
		rust_ffi_static {
			name: "libffi_static",
			crate_name: "ffi_static",
			rlibs: ["librust_rlib"],
			srcs: ["foo.rs"],
		}
		rust_library_rlib {
			name: "librust_rlib",
			crate_name: "rust_rlib",
			srcs: ["foo.rs"],
			shared_libs: ["libshared_cc_dep"],
			static_libs: ["libstatic_cc_dep"],
		}
		cc_library_shared {
			name: "libshared_cc_dep",
			srcs: ["foo.cpp"],
		}
		cc_library_static {
			name: "libstatic_cc_dep",
			srcs: ["foo.cpp"],
		}
		`)

	rustRlibRlibStd := ctx.ModuleForTests(t, "librust_rlib", "android_arm64_armv8-a_rlib_rlib-std")
	rustRlibDylibStd := ctx.ModuleForTests(t, "librust_rlib", "android_arm64_armv8-a_rlib_dylib-std")
	ffiRlib := ctx.ModuleForTests(t, "libffi_static", "android_arm64_armv8-a_rlib_rlib-std")

	modules := []android.TestingModule{
		rustRlibRlibStd,
		rustRlibDylibStd,
		ffiRlib,
	}

	// librust_rlib specifies -L flag to cc deps output directory on rustc command
	// and re-export the cc deps to rdep libffi_static
	// When building rlib crate, rustc doesn't link the native libraries
	// The build system assumes the  cc deps will be at the final linkage (either a shared library or binary)
	// Hence, these flags are no-op
	// TODO: We could consider removing these flags
	for _, module := range modules {
		if !strings.Contains(module.Rule("rustc").Args["libFlags"],
			"-L out/soong/.intermediates/libshared_cc_dep/android_arm64_armv8-a_shared/") {
			t.Errorf(
				"missing -L flag for libshared_cc_dep of %s, rustcFlags: %#v",
				module.Module().Name(), rustRlibRlibStd.Rule("rustc").Args["libFlags"],
			)
		}
		if !strings.Contains(module.Rule("rustc").Args["libFlags"],
			"-L out/soong/.intermediates/libstatic_cc_dep/android_arm64_armv8-a_static/") {
			t.Errorf(
				"missing -L flag for libstatic_cc_dep of %s, rustcFlags: %#v",
				module.Module().Name(), rustRlibRlibStd.Rule("rustc").Args["libFlags"],
			)
		}
	}
}

// Test that variants pull in the right type of rustlib autodep
func TestAutoDeps(t *testing.T) {

	ctx := testRust(t, `
		rust_library_host {
			name: "libbar",
			srcs: ["bar.rs"],
			crate_name: "bar",
		}
		rust_library_host_rlib {
			name: "librlib_only",
			srcs: ["bar.rs"],
			crate_name: "rlib_only",
		}
		rust_library_host {
			name: "libfoo",
			srcs: ["foo.rs"],
			crate_name: "foo",
			rustlibs: [
				"libbar",
				"librlib_only",
			],
		}
		rust_ffi_host {
			name: "libfoo.ffi",
			srcs: ["foo.rs"],
			crate_name: "foo",
			rustlibs: [
				"libbar",
				"librlib_only",
			],
		}
		rust_ffi_host_static {
			name: "libfoo.ffi.static",
			srcs: ["foo.rs"],
			crate_name: "foo",
			rustlibs: [
				"libbar",
				"librlib_only",
			],
		}`)

	libfooRlib := ctx.ModuleForTests(t, "libfoo", "linux_glibc_x86_64_rlib_rlib-std")
	libfooDylib := ctx.ModuleForTests(t, "libfoo", "linux_glibc_x86_64_dylib")
	libfooFFIRlib := ctx.ModuleForTests(t, "libfoo.ffi", "linux_glibc_x86_64_rlib_rlib-std")
	libfooShared := ctx.ModuleForTests(t, "libfoo.ffi", "linux_glibc_x86_64_shared")

	for _, static := range []android.TestingModule{libfooRlib, libfooFFIRlib} {
		if !android.InList("libbar.rlib-std", static.Module().(*Module).Properties.AndroidMkRlibs) {
			t.Errorf("libbar not present as rlib dependency in static lib: %s", static.Module().Name())
		}
		if android.InList("libbar", static.Module().(*Module).Properties.AndroidMkDylibs) {
			t.Errorf("libbar present as dynamic dependency in static lib: %s", static.Module().Name())
		}
	}

	for _, dyn := range []android.TestingModule{libfooDylib, libfooShared} {
		if !android.InList("libbar", dyn.Module().(*Module).Properties.AndroidMkDylibs) {
			t.Errorf("libbar not present as dynamic dependency in dynamic lib: %s", dyn.Module().Name())
		}
		if android.InList("libbar", dyn.Module().(*Module).Properties.AndroidMkRlibs) {
			t.Errorf("libbar present as rlib dependency in dynamic lib: %s", dyn.Module().Name())
		}
		if !android.InList("librlib_only", dyn.Module().(*Module).Properties.AndroidMkRlibs) {
			t.Errorf("librlib_only should be selected by rustlibs as an rlib: %s.", dyn.Module().Name())
		}
	}
}

// Test that stripped versions are correctly generated and used.
func TestStrippedLibrary(t *testing.T) {
	ctx := testRust(t, `
		rust_library_dylib {
			name: "libfoo",
			crate_name: "foo",
			srcs: ["foo.rs"],
		}
		rust_library_dylib {
			name: "libbar",
			crate_name: "bar",
			srcs: ["foo.rs"],
			strip: {
				none: true
			}
		}
	`)

	foo := ctx.ModuleForTests(t, "libfoo", "android_arm64_armv8-a_dylib")
	foo.Output("libfoo.dylib.so")
	foo.Output("unstripped/libfoo.dylib.so")
	// Check that the `cp` rule is using the stripped version as input.
	cp := foo.Rule("android.Cp")
	if strings.HasSuffix(cp.Input.String(), "unstripped/libfoo.dylib.so") {
		t.Errorf("installed library not based on stripped version: %v", cp.Input)
	}

	fizzBar := ctx.ModuleForTests(t, "libbar", "android_arm64_armv8-a_dylib").MaybeOutput("unstripped/libbar.dylib.so")
	if fizzBar.Rule != nil {
		t.Errorf("unstripped library exists, so stripped library has incorrectly been generated")
	}
}

func TestLibstdLinkage(t *testing.T) {
	ctx := testRust(t, `
		rust_library {
			name: "libfoo",
			srcs: ["foo.rs"],
			crate_name: "foo",
		}
		rust_ffi {
			name: "libbar",
			srcs: ["foo.rs"],
			crate_name: "bar",
			rustlibs: ["libfoo"],
		}
		rust_ffi_static {
			name: "libbar_static",
			srcs: ["foo.rs"],
			crate_name: "bar",
			rustlibs: ["libfoo"],
		}
		rust_ffi {
			name: "libbar.prefer_rlib",
			srcs: ["foo.rs"],
			crate_name: "bar",
			rustlibs: ["libfoo"],
			prefer_rlib: true,
		}`)

	libfooDylib := ctx.ModuleForTests(t, "libfoo", "android_arm64_armv8-a_dylib").Module().(*Module)
	libfooRlibStatic := ctx.ModuleForTests(t, "libfoo", "android_arm64_armv8-a_rlib_rlib-std").Module().(*Module)
	libfooRlibDynamic := ctx.ModuleForTests(t, "libfoo", "android_arm64_armv8-a_rlib_dylib-std").Module().(*Module)

	libbarShared := ctx.ModuleForTests(t, "libbar", "android_arm64_armv8-a_shared").Module().(*Module)
	libbarFFIRlib := ctx.ModuleForTests(t, "libbar", "android_arm64_armv8-a_rlib_rlib-std").Module().(*Module)

	// prefer_rlib works the same for both rust_library and rust_ffi, so a single check is sufficient here.
	libbarRlibStd := ctx.ModuleForTests(t, "libbar.prefer_rlib", "android_arm64_armv8-a_shared").Module().(*Module)

	if !android.InList("libstd", libfooRlibStatic.Properties.AndroidMkRlibs) {
		t.Errorf("rlib-std variant for device rust_library_rlib does not link libstd as an rlib")
	}
	if !android.InList("libstd", libfooRlibDynamic.Properties.AndroidMkDylibs) {
		t.Errorf("dylib-std variant for device rust_library_rlib does not link libstd as an dylib")
	}
	if !android.InList("libstd", libfooDylib.Properties.AndroidMkDylibs) {
		t.Errorf("Device rust_library_dylib does not link libstd as an dylib")
	}

	if !android.InList("libstd", libbarShared.Properties.AndroidMkDylibs) {
		t.Errorf("Device rust_ffi_shared does not link libstd as an dylib")
	}
	if !android.InList("libstd", libbarFFIRlib.Properties.AndroidMkRlibs) {
		t.Errorf("Device rust_ffi_static does not link libstd as an rlib")
	}
	if !android.InList("libfoo.rlib-std", libbarFFIRlib.Properties.AndroidMkRlibs) {
		t.Errorf("Device rust_ffi_static does not link dependent rustlib rlib-std variant")
	}
	if !android.InList("libstd", libbarRlibStd.Properties.AndroidMkRlibs) {
		t.Errorf("rust_ffi with prefer_rlib does not link libstd as an rlib")
	}

}

func TestRustFFIExportedIncludes(t *testing.T) {
	ctx := testRust(t, `
		rust_ffi {
			name: "libbar",
			srcs: ["foo.rs"],
			crate_name: "bar",
			export_include_dirs: ["rust_includes"],
			host_supported: true,
		}
		cc_library_static {
			name: "libfoo",
			srcs: ["foo.cpp"],
			shared_libs: ["libbar"],
			host_supported: true,
		}`)
	libfooStatic := ctx.ModuleForTests(t, "libfoo", "linux_glibc_x86_64_static").Rule("cc")
	android.AssertStringDoesContain(t, "cFlags for lib module", libfooStatic.Args["cFlags"], " -Irust_includes ")
}

// Make sure cc_rustlibs_for_make has the expected behavior, and that
// cc_library_static does as well.
// This is here instead of cc/library_test.go because the test needs to
// define a rust_ffi module which can't be done in soong-cc to avoid the
// circular dependency.
func TestCCRustlibsForMake(t *testing.T) {
	t.Parallel()
	result := testRust(t, `
		rust_ffi_static {
			name: "libbar",
			srcs: ["foo.rs"],
			crate_name: "bar",
			export_include_dirs: ["rust_includes"],
			host_supported: true,
		}

		cc_rustlibs_for_make {
			name: "libmakerustlibs",
			whole_static_libs: ["libbar"],
		}

		cc_library_static {
			name: "libccstatic",
			whole_static_libs: ["libbar"],
		}
	`)

	libmakerustlibs := result.ModuleForTests(t, "libmakerustlibs", "android_arm64_armv8-a_static").MaybeRule("rustc")
	libccstatic := result.ModuleForTests(t, "libccstatic", "android_arm64_armv8-a_static").MaybeRule("rustc")

	if libmakerustlibs.Output == nil {
		t.Errorf("cc_rustlibs_for_make is not generating a  Rust staticlib when it should")
	}

	if libccstatic.Output != nil {
		t.Errorf("cc_library_static is generating a Rust staticlib when it should not")
	}
}

func TestRustVersionScript(t *testing.T) {
	ctx := testRust(t, `
	rust_library {
		name: "librs",
		srcs: ["bar.rs"],
		crate_name: "rs",
		extra_exported_symbols: "librs.map.txt",
	}
	rust_ffi {
		name: "libffi",
		srcs: ["foo.rs"],
		crate_name: "ffi",
		version_script: "libffi.map.txt",
	}
	`)

	//linkFlags
	librs := ctx.ModuleForTests(t, "librs", "android_arm64_armv8-a_dylib").Rule("rustc")
	libffi := ctx.ModuleForTests(t, "libffi", "android_arm64_armv8-a_shared").Rule("rustc")

	if !strings.Contains(librs.Args["linkFlags"], "-Wl,--version-script=librs.map.txt") {
		t.Errorf("missing expected -Wl,--version-script= linker flag for libextended shared lib, linkFlags: %#v",
			librs.Args["linkFlags"])
	}
	if strings.Contains(librs.Args["linkFlags"], "-Wl,--android-version-script=librs.map.txt") {
		t.Errorf("unexpected -Wl,--android-version-script= linker flag for libextended shared lib, linkFlags: %#v",
			librs.Args["linkFlags"])
	}

	if !strings.Contains(libffi.Args["linkFlags"], "-Wl,--android-version-script=libffi.map.txt") {
		t.Errorf("missing -Wl,--android-version-script= linker flag for libreplaced shared lib, linkFlags: %#v",
			libffi.Args["linkFlags"])
	}
	if strings.Contains(libffi.Args["linkFlags"], "-Wl,--version-script=libffi.map.txt") {
		t.Errorf("unexpected -Wl,--version-script= linker flag for libextended shared lib, linkFlags: %#v",
			libffi.Args["linkFlags"])
	}
}

func TestRustVersionScriptPropertyErrors(t *testing.T) {
	testRustError(t, "version_script: can only be set for rust_ffi modules", `
		rust_library {
			name: "librs",
			srcs: ["bar.rs"],
			crate_name: "rs",
			version_script: "libbar.map.txt",
		}`)
	testRustError(t, "version_script and extra_exported_symbols", `
		rust_ffi {
			name: "librs",
			srcs: ["bar.rs"],
			crate_name: "rs",
			version_script: "libbar.map.txt",
			extra_exported_symbols: "libbar.map.txt",
		}`)
}

func TestStubsVersions(t *testing.T) {
	t.Parallel()
	bp := `
		rust_ffi {
			name: "libfoo",
			crate_name: "foo",
			srcs: ["foo.rs"],
			stubs: {
				versions: ["29", "R", "current"],
			},
		}
	`
	ctx := android.GroupFixturePreparers(
		prepareForRustTest,
		android.PrepareForTestWithVisibility,
		rustMockedFiles.AddToFixture(),
		android.FixtureModifyConfigAndContext(func(config android.Config, ctx *android.TestContext) {
			config.TestProductVariables.Platform_version_active_codenames = []string{"R"}
		})).RunTestWithBp(t, bp)

	variants := ctx.ModuleVariantsForTests("libfoo")
	for _, expectedVer := range []string{"29", "R", "current"} {
		expectedVariant := "android_arm_armv7-a-neon_shared_" + expectedVer
		if !android.InList(expectedVariant, variants) {
			t.Errorf("missing expected variant: %q", expectedVariant)
		}
	}
}

func TestStubsVersions_NotSorted(t *testing.T) {
	t.Parallel()
	bp := `
	rust_ffi_shared {
		name: "libfoo",
		crate_name: "foo",
		srcs: ["foo.rs"],
		stubs: {
				versions: ["29", "current", "R"],
			},
		}
	`
	fixture := android.GroupFixturePreparers(
		prepareForRustTest,
		android.PrepareForTestWithVisibility,
		rustMockedFiles.AddToFixture(),

		android.FixtureModifyConfigAndContext(func(config android.Config, ctx *android.TestContext) {
			config.TestProductVariables.Platform_version_active_codenames = []string{"R"}
		}))

	fixture.ExtendWithErrorHandler(android.FixtureExpectsOneErrorPattern(`"libfoo" .*: versions: not sorted`)).RunTestWithBp(t, bp)
}

func TestStubsVersions_ParseError(t *testing.T) {
	t.Parallel()
	bp := `
	rust_ffi_shared {
		name: "libfoo",
		crate_name: "foo",
		srcs: ["foo.rs"],
			stubs: {
				versions: ["29", "current", "X"],
			},
		}
	`
	fixture := android.GroupFixturePreparers(
		prepareForRustTest,
		android.PrepareForTestWithVisibility,
		rustMockedFiles.AddToFixture(),

		android.FixtureModifyConfigAndContext(func(config android.Config, ctx *android.TestContext) {
			config.TestProductVariables.Platform_version_active_codenames = []string{"R"}
		}))

	fixture.ExtendWithErrorHandler(android.FixtureExpectsOneErrorPattern(`"libfoo" .*: versions: "X" could not be parsed as an integer and is not a recognized codename`)).RunTestWithBp(t, bp)
}

func TestVersionedStubs(t *testing.T) {
	t.Parallel()
	bp := `
	rust_ffi_shared {
		name: "libFoo",
		crate_name: "Foo",
		srcs: ["foo.rs"],
			stubs: {
				symbol_file: "foo.map.txt",
				versions: ["1", "2", "3"],
			},
		}

	cc_library_shared {
		name: "libBar",
		srcs: ["bar.c"],
		shared_libs: ["libFoo#1"],
	}

	rust_library {
		name: "libbar_rs",
		crate_name: "bar_rs",
		srcs: ["bar.rs"],
		shared_libs: ["libFoo#1"],
	}
	rust_ffi {
		name: "libbar_ffi_rs",
		crate_name: "bar_ffi_rs",
		srcs: ["bar.rs"],
		shared_libs: ["libFoo#1"],
	}
	`

	ctx := android.GroupFixturePreparers(
		prepareForRustTest,
		android.PrepareForTestWithVisibility,
		rustMockedFiles.AddToFixture()).RunTestWithBp(t, bp)

	variants := ctx.ModuleVariantsForTests("libFoo")
	expectedVariants := []string{
		"android_arm64_armv8-a_shared",
		"android_arm64_armv8-a_shared_1",
		"android_arm64_armv8-a_shared_2",
		"android_arm64_armv8-a_shared_3",
		"android_arm64_armv8-a_shared_current",
		"android_arm_armv7-a-neon_shared",
		"android_arm_armv7-a-neon_shared_1",
		"android_arm_armv7-a-neon_shared_2",
		"android_arm_armv7-a-neon_shared_3",
		"android_arm_armv7-a-neon_shared_current",
	}
	variantsMismatch := false
	if len(variants) != len(expectedVariants) {
		variantsMismatch = true
	} else {
		for _, v := range expectedVariants {
			if !android.InList(v, variants) {
				variantsMismatch = false
			}
		}
	}
	if variantsMismatch {
		t.Errorf("variants of libFoo expected:\n")
		for _, v := range expectedVariants {
			t.Errorf("%q\n", v)
		}
		t.Errorf(", but got:\n")
		for _, v := range variants {
			t.Errorf("%q\n", v)
		}
	}

	libBarLinkRule := ctx.ModuleForTests(t, "libBar", "android_arm64_armv8-a_shared").Rule("ld")
	libBarFlags := libBarLinkRule.Args["libFlags"]

	libBarRsRustcRule := ctx.ModuleForTests(t, "libbar_rs", "android_arm64_armv8-a_dylib").Rule("rustc")
	libBarRsFlags := libBarRsRustcRule.Args["linkFlags"]

	libBarFfiRsRustcRule := ctx.ModuleForTests(t, "libbar_ffi_rs", "android_arm64_armv8-a_shared").Rule("rustc")
	libBarFfiRsFlags := libBarFfiRsRustcRule.Args["linkFlags"]

	libFoo1StubPath := "libFoo/android_arm64_armv8-a_shared_1/unstripped/libFoo.so"
	if !strings.Contains(libBarFlags, libFoo1StubPath) {
		t.Errorf("%q is not found in %q", libFoo1StubPath, libBarFlags)
	}
	if !strings.Contains(libBarRsFlags, libFoo1StubPath) {
		t.Errorf("%q is not found in %q", libFoo1StubPath, libBarRsFlags)
	}
	if !strings.Contains(libBarFfiRsFlags, libFoo1StubPath) {
		t.Errorf("%q is not found in %q", libFoo1StubPath, libBarFfiRsFlags)
	}
}

func TestCheckConflictingExplicitVersions(t *testing.T) {
	t.Parallel()
	bp := `
	cc_library_shared {
		name: "libbar",
		srcs: ["bar.c"],
		shared_libs: ["libfoo", "libfoo#impl"],
	}

	rust_ffi_shared {
		name: "libfoo",
		crate_name: "foo",
		srcs: ["foo.rs"],
		stubs: {
			versions: ["29", "current"],
		},
	}
	`
	fixture := android.GroupFixturePreparers(
		prepareForRustTest,
		android.PrepareForTestWithVisibility,
		rustMockedFiles.AddToFixture())

	fixture.ExtendWithErrorHandler(android.FixtureExpectsOneErrorPattern(`duplicate shared libraries with different explicit versions`)).RunTestWithBp(t, bp)
}

func TestAddnoOverride64GlobalCflags(t *testing.T) {
	t.Parallel()
	bp := `
		cc_library_shared {
			name: "libclient",
			srcs: ["foo.c"],
			shared_libs: ["libfoo#1"],
		}

		rust_ffi_shared {
			name: "libfoo",
			crate_name: "foo",
			srcs: ["foo.c"],
			shared_libs: ["libbar"],
			stubs: {
				symbol_file: "foo.map.txt",
				versions: ["1", "2", "3"],
			},
		}

		cc_library_shared {
			name: "libbar",
			export_include_dirs: ["include/libbar"],
			srcs: ["foo.c"],
		}`
	ctx := android.GroupFixturePreparers(
		prepareForRustTest,
		android.PrepareForTestWithVisibility,
		rustMockedFiles.AddToFixture()).RunTestWithBp(t, bp)

	cFlags := ctx.ModuleForTests(t, "libclient", "android_arm64_armv8-a_shared").Rule("cc").Args["cFlags"]

	if !strings.Contains(cFlags, "${config.NoOverride64GlobalCflags}") {
		t.Errorf("expected %q in cflags, got %q", "${config.NoOverride64GlobalCflags}", cFlags)
	}
}

// Make sure the stubs properties can only be used in modules producing shared libs
func TestRustStubsFFIOnly(t *testing.T) {
	testRustError(t, "stubs properties", `
		rust_library {
			name: "libfoo",
			crate_name: "foo",
			srcs: ["foo.c"],
			shared_libs: ["libbar"],
			stubs: {
				symbol_file: "foo.map.txt",
			},
		}
	`)

	testRustError(t, "stubs properties", `
		rust_library {
			name: "libfoo",
			crate_name: "foo",
			srcs: ["foo.c"],
			shared_libs: ["libbar"],
			stubs: {
				versions: ["1"],
			},
		}
	`)

	testRustError(t, "stubs properties", `
		rust_ffi_static {
			name: "libfoo",
			crate_name: "foo",
			srcs: ["foo.c"],
			shared_libs: ["libbar"],
			stubs: {
				symbol_file: "foo.map.txt",
			},
		}
	`)
	testRustError(t, "stubs properties", `
		rust_ffi_static {
			name: "libfoo",
			crate_name: "foo",
			srcs: ["foo.c"],
			shared_libs: ["libbar"],
			stubs: {
				versions: ["1"],
			},
		}
	`)
}

// TODO: When rust_ffi libraries support export_*_lib_headers,
// add a test similar to cc.TestStubsLibReexportsHeaders
