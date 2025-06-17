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

package java

import (
	"reflect"
	"testing"

	"android/soong/android"
)

func TestCollectJavaLibraryPropertiesAddLibsDeps(t *testing.T) {
	t.Parallel()
	ctx, _ := testJava(t,
		`
		java_library {name: "Foo"}
		java_library {name: "Bar"}
		java_library {
			name: "javalib",
			libs: ["Foo", "Bar"],
		}
	`)
	module := ctx.ModuleForTests(t, "javalib", "android_common").Module().(*Library)
	dpInfo, _ := android.OtherModuleProvider(ctx, module, android.IdeInfoProviderKey)

	for _, expected := range []string{"Foo", "Bar"} {
		if !android.InList(expected, dpInfo.Deps) {
			t.Errorf("Library.IDEInfo() Deps = %v, %v not found", dpInfo.Deps, expected)
		}
	}
}

func TestCollectJavaLibraryPropertiesAddStaticLibsDeps(t *testing.T) {
	t.Parallel()
	ctx, _ := testJava(t,
		`
		java_library {name: "Foo"}
		java_library {name: "Bar"}
		java_library {
			name: "javalib",
			static_libs: ["Foo", "Bar"],
		}
	`)
	module := ctx.ModuleForTests(t, "javalib", "android_common").Module().(*Library)
	dpInfo, _ := android.OtherModuleProvider(ctx, module, android.IdeInfoProviderKey)

	for _, expected := range []string{"Foo", "Bar"} {
		if !android.InList(expected, dpInfo.Deps) {
			t.Errorf("Library.IDEInfo() Deps = %v, %v not found", dpInfo.Deps, expected)
		}
	}
}

func TestCollectJavaLibraryPropertiesAddScrs(t *testing.T) {
	t.Parallel()
	ctx, _ := testJava(t,
		`
		java_library {
			name: "javalib",
			srcs: ["Foo.java", "Bar.java"],
		}
	`)
	module := ctx.ModuleForTests(t, "javalib", "android_common").Module().(*Library)
	dpInfo, _ := android.OtherModuleProvider(ctx, module, android.IdeInfoProviderKey)

	expected := []string{"Foo.java", "Bar.java"}
	if !reflect.DeepEqual(dpInfo.Srcs, expected) {
		t.Errorf("Library.IDEInfo() Srcs = %v, want %v", dpInfo.Srcs, expected)
	}
}

func TestCollectJavaLibraryPropertiesAddAidlIncludeDirs(t *testing.T) {
	t.Parallel()
	ctx, _ := testJava(t,
		`
		java_library {
			name: "javalib",
			aidl: {
				include_dirs: ["Foo", "Bar"],
			},
		}
	`)
	module := ctx.ModuleForTests(t, "javalib", "android_common").Module().(*Library)
	dpInfo, _ := android.OtherModuleProvider(ctx, module, android.IdeInfoProviderKey)

	expected := []string{"Foo", "Bar"}
	if !reflect.DeepEqual(dpInfo.Aidl_include_dirs, expected) {
		t.Errorf("Library.IDEInfo() Aidl_include_dirs = %v, want %v", dpInfo.Aidl_include_dirs, expected)
	}
}

func TestCollectJavaLibraryWithJarJarRules(t *testing.T) {
	t.Parallel()
	ctx, _ := testJava(t,
		`
		java_library {
			name: "javalib",
			srcs: ["foo.java"],
			jarjar_rules: "jarjar_rules.txt",
		}
	`)
	module := ctx.ModuleForTests(t, "javalib", "android_common").Module().(*Library)
	dpInfo, _ := android.OtherModuleProvider(ctx, module, android.IdeInfoProviderKey)

	android.AssertStringEquals(t, "IdeInfo.Srcs of repackaged library should not be empty", "foo.java", dpInfo.Srcs[0])
	android.AssertStringEquals(t, "IdeInfo.Jar_rules of repackaged library should not be empty", "jarjar_rules.txt", dpInfo.Jarjar_rules[0])
	if !android.SubstringInList(dpInfo.Jars, "soong/.intermediates/javalib/android_common/jarjar/turbine/javalib.jar") {
		t.Errorf("IdeInfo.Jars of repackaged library should contain the output of jarjar-ing. All outputs: %v\n", dpInfo.Jars)
	}
}

func TestCollectJavaLibraryLinkingAgainstVersionedSdk(t *testing.T) {
	t.Parallel()
	ctx := android.GroupFixturePreparers(
		prepareForJavaTest,
		FixtureWithPrebuiltApis(map[string][]string{
			"29": {},
		})).RunTestWithBp(t,
		`
		java_library {
			name: "javalib",
			srcs: ["foo.java"],
			sdk_version: "29",
		}
	`)
	module := ctx.ModuleForTests(t, "javalib", "android_common").Module().(*Library)
	dpInfo, _ := android.OtherModuleProvider(ctx, module, android.IdeInfoProviderKey)

	android.AssertStringListContains(t, "IdeInfo.Deps should contain versioned sdk module", dpInfo.Deps, "sdk_public_29_android")
}

func TestDoNotAddNoneSystemModulesToDeps(t *testing.T) {
	ctx := android.GroupFixturePreparers(
		prepareForJavaTest,
		android.FixtureMergeEnv(
			map[string]string{
				"DISABLE_STUB_VALIDATION": "true",
			},
		),
	).RunTestWithBp(t,
		`
		java_library {
			name: "javalib",
			srcs: ["foo.java"],
			sdk_version: "none",
			system_modules: "none",
		}

		java_api_library {
			name: "javalib.stubs",
			stubs_type: "everything",
			api_contributions: ["javalib-current.txt"],
			api_surface: "public",
			system_modules: "none",
		}
		java_api_contribution {
			name: "javalib-current.txt",
			api_file: "javalib-current.txt",
			api_surface: "public",
		}
	`)
	javalib := ctx.ModuleForTests(t, "javalib", "android_common").Module().(*Library)
	dpInfo, _ := android.OtherModuleProvider(ctx, javalib, android.IdeInfoProviderKey)
	android.AssertStringListDoesNotContain(t, "IdeInfo.Deps should contain not contain `none`", dpInfo.Deps, "none")

	javalib_stubs := ctx.ModuleForTests(t, "javalib.stubs", "android_common").Module().(*ApiLibrary)
	dpInfo, _ = android.OtherModuleProvider(ctx, javalib_stubs, android.IdeInfoProviderKey)
	android.AssertStringListDoesNotContain(t, "IdeInfo.Deps should contain not contain `none`", dpInfo.Deps, "none")
}
