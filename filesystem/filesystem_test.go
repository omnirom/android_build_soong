// Copyright 2021 Google Inc. All rights reserved.
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
	"os"
	"testing"

	"android/soong/android"
	"android/soong/bpf"
	"android/soong/cc"
	"android/soong/etc"
	"android/soong/java"
	"android/soong/phony"

	"github.com/google/blueprint/proptools"
)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

var fixture = android.GroupFixturePreparers(
	android.PrepareForIntegrationTestWithAndroid,
	android.PrepareForTestWithAndroidBuildComponents,
	bpf.PrepareForTestWithBpf,
	cc.PrepareForIntegrationTestWithCc,
	etc.PrepareForTestWithPrebuiltEtc,
	java.PrepareForTestWithJavaBuildComponents,
	java.PrepareForTestWithJavaDefaultModules,
	phony.PrepareForTestWithPhony,
	PrepareForTestWithFilesystemBuildComponents,
)

func TestFileSystemDeps(t *testing.T) {
	result := fixture.RunTestWithBp(t, `
		android_filesystem {
			name: "myfilesystem",
			multilib: {
				common: {
					deps: [
						"bpf.o",
						"phony",
					],
				},
				lib32: {
					deps: [
						"foo",
						"libbar",
					],
				},
				lib64: {
					deps: [
						"libbar",
					],
				},
			},
			compile_multilib: "both",
		}

		bpf {
			name: "bpf.o",
			srcs: ["bpf.c"],
		}

		cc_binary {
			name: "foo",
			compile_multilib: "prefer32",
		}

		cc_library {
			name: "libbar",
			required: ["libbaz"],
			target: {
				platform: {
					required: ["lib_platform_only"],
				},
			},
		}

		cc_library {
			name: "libbaz",
		}

		cc_library {
			name: "lib_platform_only",
		}

		phony {
			name: "phony",
			required: [
				"libquz",
				"myapp",
			],
		}

		cc_library {
			name: "libquz",
		}

		android_app {
			name: "myapp",
			platform_apis: true,
			installable: true,
		}
	`)

	// produces "myfilesystem.img"
	result.ModuleForTests(t, "myfilesystem", "android_common").Output("myfilesystem.img")

	fs := result.ModuleForTests(t, "myfilesystem", "android_common").Module().(*filesystem)
	expected := []string{
		"app/myapp/myapp.apk",
		"bin/foo",
		"lib/libbar.so",
		"lib64/libbar.so",
		"lib64/libbaz.so",
		"lib64/libquz.so",
		"lib64/lib_platform_only.so",
		"etc/bpf/bpf.o",
	}
	for _, e := range expected {
		android.AssertStringListContains(t, "missing entry", fs.entries, e)
	}
}

func TestFileSystemFillsLinkerConfigWithStubLibs(t *testing.T) {
	result := fixture.RunTestWithBp(t, `
		android_system_image {
			name: "myfilesystem",
			base_dir: "system",
			deps: [
				"libfoo",
				"libbar",
			],
			linker_config: {
				gen_linker_config: true,
				linker_config_srcs: ["linker.config.json"],
			},
		}

		cc_library {
			name: "libfoo",
			stubs: {
				symbol_file: "libfoo.map.txt",
			},
		}

		cc_library {
			name: "libbar",
		}
	`)

	module := result.ModuleForTests(t, "myfilesystem", "android_common")
	output := module.Output("out/soong/.intermediates/myfilesystem/android_common/linker.config.pb")

	linkerConfigCommand := output.RuleParams.Command

	android.AssertStringDoesContain(t, "linker.config.pb should have libfoo",
		linkerConfigCommand, "libfoo.so")
	android.AssertStringDoesNotContain(t, "linker.config.pb should not have libbar",
		linkerConfigCommand, "libbar.so")
}

func registerComponent(ctx android.RegistrationContext) {
	ctx.RegisterModuleType("component", componentFactory)
}

func componentFactory() android.Module {
	m := &component{}
	m.AddProperties(&m.properties)
	android.InitAndroidArchModule(m, android.DeviceSupported, android.MultilibCommon)
	return m
}

type component struct {
	android.ModuleBase
	properties struct {
		Install_copy_in_data []string
	}
}

func (c *component) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	output := android.PathForModuleOut(ctx, c.Name())
	dir := android.PathForModuleInstall(ctx, "components")
	ctx.InstallFile(dir, c.Name(), output)

	dataDir := android.PathForModuleInPartitionInstall(ctx, "data", "components")
	for _, d := range c.properties.Install_copy_in_data {
		ctx.InstallFile(dataDir, d, output)
	}
}

func TestFileSystemGathersItemsOnlyInSystemPartition(t *testing.T) {
	f := android.GroupFixturePreparers(fixture, android.FixtureRegisterWithContext(registerComponent))
	result := f.RunTestWithBp(t, `
		android_system_image {
			name: "myfilesystem",
			multilib: {
				common: {
					deps: ["foo"],
				},
			},
			linker_config: {
				gen_linker_config: true,
				linker_config_srcs: ["linker.config.json"],
			},
		}
		component {
			name: "foo",
			install_copy_in_data: ["bar"],
		}
	`)

	module := result.ModuleForTests(t, "myfilesystem", "android_common").Module().(*systemImage)
	android.AssertDeepEquals(t, "entries should have foo and not bar", []string{"components/foo", "etc/linker.config.pb"}, module.entries)
}

func TestAvbGenVbmetaImage(t *testing.T) {
	result := fixture.RunTestWithBp(t, `
		avb_gen_vbmeta_image {
			name: "input_hashdesc",
			src: "input.img",
			partition_name: "input_partition_name",
			salt: "2222",
		}`)
	cmd := result.ModuleForTests(t, "input_hashdesc", "android_arm64_armv8-a").Rule("avbGenVbmetaImage").RuleParams.Command
	android.AssertStringDoesContain(t, "Can't find correct --partition_name argument",
		cmd, "--partition_name input_partition_name")
	android.AssertStringDoesContain(t, "Can't find --do_not_append_vbmeta_image",
		cmd, "--do_not_append_vbmeta_image")
	android.AssertStringDoesContain(t, "Can't find --output_vbmeta_image",
		cmd, "--output_vbmeta_image ")
	android.AssertStringDoesContain(t, "Can't find --salt argument",
		cmd, "--salt 2222")
}

func TestAvbAddHashFooter(t *testing.T) {
	result := fixture.RunTestWithBp(t, `
		avb_gen_vbmeta_image {
			name: "input_hashdesc",
			src: "input.img",
			partition_name: "input",
			salt: "2222",
		}

		avb_add_hash_footer {
			name: "myfooter",
			src: "input.img",
			filename: "output.img",
			partition_name: "mypartition",
			private_key: "mykey",
			salt: "1111",
			props: [
				{
					name: "prop1",
					value: "value1",
				},
				{
					name: "prop2",
					file: "value_file",
				},
			],
			include_descriptors_from_images: ["input_hashdesc"],
		}
	`)
	cmd := result.ModuleForTests(t, "myfooter", "android_arm64_armv8-a").Rule("avbAddHashFooter").RuleParams.Command
	android.AssertStringDoesContain(t, "Can't find correct --partition_name argument",
		cmd, "--partition_name mypartition")
	android.AssertStringDoesContain(t, "Can't find correct --key argument",
		cmd, "--key mykey")
	android.AssertStringDoesContain(t, "Can't find --salt argument",
		cmd, "--salt 1111")
	android.AssertStringDoesContain(t, "Can't find --prop argument",
		cmd, "--prop 'prop1:value1'")
	android.AssertStringDoesContain(t, "Can't find --prop_from_file argument",
		cmd, "--prop_from_file 'prop2:value_file'")
	android.AssertStringDoesContain(t, "Can't find --include_descriptors_from_image",
		cmd, "--include_descriptors_from_image ")
}

func TestFileSystemWithCoverageVariants(t *testing.T) {
	context := android.GroupFixturePreparers(
		fixture,
		android.FixtureModifyProductVariables(func(variables android.FixtureProductVariables) {
			variables.GcovCoverage = proptools.BoolPtr(true)
			variables.Native_coverage = proptools.BoolPtr(true)
		}),
	)

	result := context.RunTestWithBp(t, `
		prebuilt_etc {
			name: "prebuilt",
			src: ":myfilesystem",
		}

		android_system_image {
			name: "myfilesystem",
			deps: [
				"libfoo",
			],
			linker_config: {
				gen_linker_config: true,
				linker_config_srcs: ["linker.config.json"],
			},
		}

		cc_library {
			name: "libfoo",
			shared_libs: [
				"libbar",
			],
			stl: "none",
		}

		cc_library {
			name: "libbar",
			stl: "none",
		}
	`)

	filesystem := result.ModuleForTests(t, "myfilesystem", "android_common_cov")
	inputs := filesystem.Output("staging_dir.timestamp").Implicits
	android.AssertStringListContains(t, "filesystem should have libfoo(cov)",
		inputs.Strings(),
		"out/soong/.intermediates/libfoo/android_arm64_armv8-a_shared_cov/libfoo.so")
	android.AssertStringListContains(t, "filesystem should have libbar(cov)",
		inputs.Strings(),
		"out/soong/.intermediates/libbar/android_arm64_armv8-a_shared_cov/libbar.so")

	filesystemOutput := filesystem.OutputFiles(result.TestContext, t, "")[0]
	prebuiltInput := result.ModuleForTests(t, "prebuilt", "android_arm64_armv8-a").Rule("Cp").Input
	if filesystemOutput != prebuiltInput {
		t.Error("prebuilt should use cov variant of filesystem")
	}
}

func TestSystemImageDefaults(t *testing.T) {
	result := fixture.RunTestWithBp(t, `
		android_filesystem_defaults {
			name: "defaults",
			multilib: {
				common: {
					deps: [
						"phony",
					],
				},
				lib64: {
					deps: [
						"libbar",
					],
				},
			},
			compile_multilib: "both",
		}

		android_system_image {
			name: "system",
			defaults: ["defaults"],
			multilib: {
				lib32: {
					deps: [
						"foo",
						"libbar",
					],
				},
			},
		}

		cc_binary {
			name: "foo",
			compile_multilib: "prefer32",
		}

		cc_library {
			name: "libbar",
			required: ["libbaz"],
		}

		cc_library {
			name: "libbaz",
		}

		phony {
			name: "phony",
			required: ["libquz"],
		}

		cc_library {
			name: "libquz",
		}
	`)

	fs := result.ModuleForTests(t, "system", "android_common").Module().(*systemImage)
	expected := []string{
		"bin/foo",
		"lib/libbar.so",
		"lib64/libbar.so",
		"lib64/libbaz.so",
		"lib64/libquz.so",
	}
	for _, e := range expected {
		android.AssertStringListContains(t, "missing entry", fs.entries, e)
	}
}

func TestInconsistentPartitionTypesInDefaults(t *testing.T) {
	fixture.ExtendWithErrorHandler(android.FixtureExpectsOneErrorPattern(
		"doesn't match with the partition type")).
		RunTestWithBp(t, `
		android_filesystem_defaults {
			name: "system_ext_def",
			partition_type: "system_ext",
		}

		android_filesystem_defaults {
			name: "system_def",
			partition_type: "system",
			defaults: ["system_ext_def"],
		}

		android_system_image {
			name: "system",
			defaults: ["system_def"],
		}
	`)
}

func TestPreventDuplicatedEntries(t *testing.T) {
	fixture.ExtendWithErrorHandler(android.FixtureExpectsOneErrorPattern(
		"packaging conflict at")).
		RunTestWithBp(t, `
		android_filesystem {
			name: "fs",
			deps: [
				"foo",
				"foo_dup",
			],
		}

		cc_binary {
			name: "foo",
		}

		cc_binary {
			name: "foo_dup",
			stem: "foo",
		}
	`)
}

func TestTrackPhonyAsRequiredDep(t *testing.T) {
	result := fixture.RunTestWithBp(t, `
		android_filesystem {
			name: "fs",
			deps: ["foo"],
		}

		cc_binary {
			name: "foo",
			required: ["phony"],
		}

		phony {
			name: "phony",
			required: ["libbar"],
		}

		cc_library {
			name: "libbar",
		}
	`)

	fs := result.ModuleForTests(t, "fs", "android_common").Module().(*filesystem)
	expected := []string{
		"bin/foo",
		"lib64/libbar.so",
	}
	for _, e := range expected {
		android.AssertStringListContains(t, "missing entry", fs.entries, e)
	}
}

func TestFilterOutUnsupportedArches(t *testing.T) {
	result := fixture.RunTestWithBp(t, `
		android_filesystem {
			name: "fs_64_only",
			deps: ["foo"],
		}

		android_filesystem {
			name: "fs_64_32",
			compile_multilib: "both",
			deps: ["foo"],
		}

		cc_binary {
			name: "foo",
			required: ["phony"],
		}

		phony {
			name: "phony",
			required: [
				"libbar",
				"app",
			],
		}

		cc_library {
			name: "libbar",
		}

		android_app {
			name: "app",
			srcs: ["a.java"],
			platform_apis: true,
		}
	`)
	testcases := []struct {
		fsName     string
		expected   []string
		unexpected []string
	}{
		{
			fsName:     "fs_64_only",
			expected:   []string{"app/app/app.apk", "bin/foo", "lib64/libbar.so"},
			unexpected: []string{"lib/libbar.so"},
		},
		{
			fsName:     "fs_64_32",
			expected:   []string{"app/app/app.apk", "bin/foo", "lib64/libbar.so", "lib/libbar.so"},
			unexpected: []string{},
		},
	}
	for _, c := range testcases {
		fs := result.ModuleForTests(t, c.fsName, "android_common").Module().(*filesystem)
		for _, e := range c.expected {
			android.AssertStringListContains(t, "missing entry", fs.entries, e)
		}
		for _, e := range c.unexpected {
			android.AssertStringListDoesNotContain(t, "unexpected entry", fs.entries, e)
		}
	}
}

func TestErofsPartition(t *testing.T) {
	result := fixture.RunTestWithBp(t, `
		android_filesystem {
			name: "erofs_partition",
			type: "erofs",
			erofs: {
				compressor: "lz4hc,9",
				compress_hints: "compress_hints.txt",
			},
			deps: ["binfoo"],
		}

		cc_binary {
			name: "binfoo",
		}
	`)

	partition := result.ModuleForTests(t, "erofs_partition", "android_common")
	buildImageConfig := android.ContentFromFileRuleForTests(t, result.TestContext, partition.Output("prop_pre_processing"))
	android.AssertStringDoesContain(t, "erofs fs type", buildImageConfig, "fs_type=erofs")
	android.AssertStringDoesContain(t, "erofs fs type compress algorithm", buildImageConfig, "erofs_default_compressor=lz4hc,9")
	android.AssertStringDoesContain(t, "erofs fs type compress hint", buildImageConfig, "erofs_default_compress_hints=compress_hints.txt")
	android.AssertStringDoesContain(t, "erofs fs type sparse", buildImageConfig, "erofs_sparse_flag=-s")
}

func TestF2fsPartition(t *testing.T) {
	result := fixture.RunTestWithBp(t, `
		android_filesystem {
			name: "f2fs_partition",
			type: "f2fs",
		}
	`)

	partition := result.ModuleForTests(t, "f2fs_partition", "android_common")
	buildImageConfig := android.ContentFromFileRuleForTests(t, result.TestContext, partition.Output("prop_pre_processing"))
	android.AssertStringDoesContain(t, "f2fs fs type", buildImageConfig, "fs_type=f2fs")
	android.AssertStringDoesContain(t, "f2fs fs type sparse", buildImageConfig, "f2fs_sparse_flag=-S")
}

func TestFsTypesPropertyError(t *testing.T) {
	fixture.ExtendWithErrorHandler(android.FixtureExpectsOneErrorPattern(
		"erofs: erofs is non-empty, but FS type is f2fs\n. Please delete erofs properties if this partition should use f2fs\n")).
		RunTestWithBp(t, `
		android_filesystem {
			name: "f2fs_partition",
			type: "f2fs",
			erofs: {
				compressor: "lz4hc,9",
				compress_hints: "compress_hints.txt",
			},
		}
	`)
}

// If a system_ext/ module depends on system/ module, the dependency should *not*
// be installed in system_ext/
func TestDoNotPackageCrossPartitionDependencies(t *testing.T) {
	t.Skip() // TODO (spandandas): Re-enable this
	result := fixture.RunTestWithBp(t, `
		android_filesystem {
			name: "myfilesystem",
			deps: ["binfoo"],
			partition_type: "system_ext",
		}

		cc_binary {
			name: "binfoo",
			shared_libs: ["libfoo"],
			system_ext_specific: true,
		}
		cc_library_shared {
			name: "libfoo", // installed in system/
		}
	`)

	partition := result.ModuleForTests(t, "myfilesystem", "android_common")
	fileList := android.ContentFromFileRuleForTests(t, result.TestContext, partition.Output("fileList"))
	android.AssertDeepEquals(t, "filesystem with dependencies on different partition", "bin/binfoo\n", fileList)
}

// If a cc_library is listed in `deps`, and it has a shared and static variant, then the shared variant
// should be installed.
func TestUseSharedVariationOfNativeLib(t *testing.T) {
	result := fixture.RunTestWithBp(t, `
		android_filesystem {
			name: "myfilesystem",
			deps: ["libfoo"],
		}
		// cc_library will create a static and shared variant.
		cc_library {
			name: "libfoo",
		}
	`)

	partition := result.ModuleForTests(t, "myfilesystem", "android_common")
	fileList := android.ContentFromFileRuleForTests(t, result.TestContext, partition.Output("fileList"))
	android.AssertDeepEquals(t, "cc_library listed in deps",
		"lib64/bootstrap/libc.so\nlib64/bootstrap/libdl.so\nlib64/bootstrap/libm.so\nlib64/libc++.so\nlib64/libc.so\nlib64/libdl.so\nlib64/libfoo.so\nlib64/libm.so\n",
		fileList)
}

// binfoo1 overrides binbar. transitive deps of binbar should not be installed.
func TestDoNotInstallTransitiveDepOfOverriddenModule(t *testing.T) {
	result := fixture.RunTestWithBp(t, `
android_filesystem {
    name: "myfilesystem",
    deps: ["binfoo1", "libfoo2", "binbar"],
}
cc_binary {
    name: "binfoo1",
    shared_libs: ["libfoo"],
    overrides: ["binbar"],
}
cc_library {
    name: "libfoo",
}
cc_library {
    name: "libfoo2",
    overrides: ["libfoo"],
}
// binbar gets overridden by binfoo1
// therefore, libbar should not be installed
cc_binary {
    name: "binbar",
    shared_libs: ["libbar"]
}
cc_library {
    name: "libbar",
}
	`)

	partition := result.ModuleForTests(t, "myfilesystem", "android_common")
	fileList := android.ContentFromFileRuleForTests(t, result.TestContext, partition.Output("fileList"))
	android.AssertDeepEquals(t, "Shared library dep of overridden binary should not be installed",
		"bin/binfoo1\nlib64/bootstrap/libc.so\nlib64/bootstrap/libdl.so\nlib64/bootstrap/libm.so\nlib64/libc++.so\nlib64/libc.so\nlib64/libdl.so\nlib64/libfoo2.so\nlib64/libm.so\n",
		fileList)
}

func TestInstallLinkerConfigFile(t *testing.T) {
	result := fixture.RunTestWithBp(t, `
android_filesystem {
    name: "myfilesystem",
    deps: ["libfoo_has_no_stubs", "libfoo_has_stubs"],
    linker_config: {
        gen_linker_config: true,
        linker_config_srcs: ["linker.config.json"],
    },
    partition_type: "vendor",
}
cc_library {
    name: "libfoo_has_no_stubs",
    vendor: true,
}
cc_library {
    name: "libfoo_has_stubs",
    stubs: {symbol_file: "libfoo.map.txt"},
    vendor: true,
}
	`)

	linkerConfigCmd := result.ModuleForTests(t, "myfilesystem", "android_common").Output("out/soong/.intermediates/myfilesystem/android_common/linker.config.pb").RuleParams.Command
	android.AssertStringDoesContain(t, "Could not find linker.config.json file in cmd", linkerConfigCmd, "conv_linker_config proto --force -s linker.config.json")
	android.AssertStringDoesContain(t, "Could not find stub in `provideLibs`", linkerConfigCmd, "--key provideLibs --value libfoo_has_stubs.so")
}

// override_android_* modules implicitly override their base module.
// If both of these are listed in `deps`, the base module should not be installed.
// Also, required deps should be updated too.
func TestOverrideModulesInDeps(t *testing.T) {
	result := fixture.RunTestWithBp(t, `
		cc_library_shared {
			name: "libfoo",
			stl: "none",
			system_shared_libs: [],
		}
		cc_library_shared {
			name: "libbar",
			stl: "none",
			system_shared_libs: [],
		}
		phony {
			name: "myapp_phony",
			required: ["myapp"],
		}
		phony {
			name: "myoverrideapp_phony",
			required: ["myoverrideapp"],
		}
		android_app {
			name: "myapp",
			platform_apis: true,
			required: ["libfoo"],
		}
		override_android_app {
			name: "myoverrideapp",
			base: "myapp",
			required: ["libbar"],
		}
		android_filesystem {
			name: "myfilesystem",
			deps: ["myapp"],
		}
		android_filesystem {
			name: "myfilesystem_overridden",
			deps: ["myapp", "myoverrideapp"],
		}
		android_filesystem {
			name: "myfilesystem_overridden_indirect",
			deps: ["myapp_phony", "myoverrideapp_phony"],
		}
	`)

	partition := result.ModuleForTests(t, "myfilesystem", "android_common")
	fileList := android.ContentFromFileRuleForTests(t, result.TestContext, partition.Output("fileList"))
	android.AssertStringEquals(t, "filesystem without override app", "app/myapp/myapp.apk\nlib64/libfoo.so\n", fileList)

	for _, overridden := range []string{"myfilesystem_overridden", "myfilesystem_overridden_indirect"} {
		overriddenPartition := result.ModuleForTests(t, overridden, "android_common")
		overriddenFileList := android.ContentFromFileRuleForTests(t, result.TestContext, overriddenPartition.Output("fileList"))
		android.AssertStringEquals(t, "filesystem with "+overridden, "app/myoverrideapp/myoverrideapp.apk\nlib64/libbar.so\n", overriddenFileList)
	}
}

func TestRamdiskPartitionSetsDevNodes(t *testing.T) {
	result := android.GroupFixturePreparers(
		fixture,
		android.FixtureMergeMockFs(android.MockFS{
			"ramdisk_node_list": nil,
		}),
	).RunTestWithBp(t, `
		android_filesystem {
			name: "ramdisk_filesystem",
			partition_name: "ramdisk",
		}
		filegroup {
			name: "ramdisk_node_list",
			srcs: ["ramdisk_node_list"],
		}
	`)

	android.AssertBoolEquals(
		t,
		"Generated ramdisk image expected to depend on \"ramdisk_node_list\" module",
		true,
		java.CheckModuleHasDependency(t, result.TestContext, "ramdisk_filesystem", "android_common", "ramdisk_node_list"),
	)
}
