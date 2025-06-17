// Copyright (C) 2021 The Android Open Source Project
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

package apex

import (
	"strings"
	"testing"

	"android/soong/android"
	"android/soong/dexpreopt"
	"android/soong/java"
)

var prepareForTestWithSystemserverclasspathFragment = android.GroupFixturePreparers(
	java.PrepareForTestWithDexpreopt,
	PrepareForTestWithApexBuildComponents,
)

func TestSystemserverclasspathFragmentContents(t *testing.T) {
	t.Parallel()
	result := android.GroupFixturePreparers(
		prepareForTestWithSystemserverclasspathFragment,
		prepareForTestWithMyapex,
		dexpreopt.FixtureSetApexSystemServerJars("myapex:foo", "myapex:bar", "myapex:baz"),
	).RunTestWithBp(t, `
		apex {
			name: "myapex",
			key: "myapex.key",
			systemserverclasspath_fragments: [
				"mysystemserverclasspathfragment",
			],
			updatable: false,
		}

		apex_key {
			name: "myapex.key",
			public_key: "testkey.avbpubkey",
			private_key: "testkey.pem",
		}

		java_library {
			name: "foo",
			srcs: ["b.java"],
			installable: true,
			apex_available: [
				"myapex",
			],
		}

		java_library {
			name: "bar",
			srcs: ["c.java"],
			installable: true,
			dex_preopt: {
				profile: "bar-art-profile",
			},
			apex_available: [
				"myapex",
			],
		}

		java_library {
			name: "baz",
			srcs: ["d.java"],
			installable: true,
			dex_preopt: {
				profile_guided: true, // ignored
			},
			apex_available: [
				"myapex",
			],
			sdk_version: "core_current",
		}

		systemserverclasspath_fragment {
			name: "mysystemserverclasspathfragment",
			contents: [
				"foo",
				"bar",
				"baz",
			],
			apex_available: [
				"myapex",
			],
		}
	`)

	ctx := result.TestContext

	ensureExactContents(t, ctx, "myapex", "android_common_myapex", []string{
		"etc/classpaths/systemserverclasspath.pb",
		"javalib/foo.jar",
		"javalib/bar.jar",
		"javalib/bar.jar.prof",
		"javalib/baz.jar",
	})

	java.CheckModuleDependencies(t, ctx, "myapex", "android_common_myapex", []string{
		`all_apex_contributions`,
		`dex2oatd`,
		`myapex.key`,
		`mysystemserverclasspathfragment`,
	})

	assertProfileGuided(t, ctx, "foo", "android_common_apex10000", false)
	assertProfileGuided(t, ctx, "bar", "android_common_apex10000", true)
	assertProfileGuided(t, ctx, "baz", "android_common_apex10000", false)
}

func TestSystemserverclasspathFragmentNoGeneratedProto(t *testing.T) {
	t.Parallel()
	result := android.GroupFixturePreparers(
		prepareForTestWithSystemserverclasspathFragment,
		prepareForTestWithMyapex,
		dexpreopt.FixtureSetApexSystemServerJars("myapex:foo"),
	).RunTestWithBp(t, `
		apex {
			name: "myapex",
			key: "myapex.key",
			systemserverclasspath_fragments: [
				"mysystemserverclasspathfragment",
			],
			updatable: false,
		}

		apex_key {
			name: "myapex.key",
			public_key: "testkey.avbpubkey",
			private_key: "testkey.pem",
		}

		java_library {
			name: "foo",
			srcs: ["b.java"],
			installable: true,
			apex_available: [
				"myapex",
			],
		}

		systemserverclasspath_fragment {
			name: "mysystemserverclasspathfragment",
			generate_classpaths_proto: false,
			contents: [
				"foo",
			],
			apex_available: [
				"myapex",
			],
		}
	`)

	ensureExactContents(t, result.TestContext, "myapex", "android_common_myapex", []string{
		"javalib/foo.jar",
	})

	java.CheckModuleDependencies(t, result.TestContext, "myapex", "android_common_myapex", []string{
		`all_apex_contributions`,
		`dex2oatd`,
		`myapex.key`,
		`mysystemserverclasspathfragment`,
	})
}

func TestSystemServerClasspathFragmentWithContentNotInMake(t *testing.T) {
	t.Parallel()
	android.GroupFixturePreparers(
		prepareForTestWithSystemserverclasspathFragment,
		prepareForTestWithMyapex,
		dexpreopt.FixtureSetApexSystemServerJars("myapex:foo"),
	).
		ExtendWithErrorHandler(android.FixtureExpectsAtLeastOneErrorMatchingPattern(
			`in contents must also be declared in PRODUCT_APEX_SYSTEM_SERVER_JARS`)).
		RunTestWithBp(t, `
			apex {
				name: "myapex",
				key: "myapex.key",
				systemserverclasspath_fragments: [
					"mysystemserverclasspathfragment",
				],
				updatable: false,
			}

			apex_key {
				name: "myapex.key",
				public_key: "testkey.avbpubkey",
				private_key: "testkey.pem",
			}

			java_library {
				name: "foo",
				srcs: ["b.java"],
				installable: true,
				apex_available: ["myapex"],
			}

			java_library {
				name: "bar",
				srcs: ["b.java"],
				installable: true,
				apex_available: ["myapex"],
			}

			systemserverclasspath_fragment {
				name: "mysystemserverclasspathfragment",
				contents: [
					"foo",
					"bar",
				],
				apex_available: [
					"myapex",
				],
			}
		`)
}

func TestPrebuiltSystemserverclasspathFragmentContents(t *testing.T) {
	t.Parallel()
	result := android.GroupFixturePreparers(
		prepareForTestWithSystemserverclasspathFragment,
		prepareForTestWithMyapex,
		dexpreopt.FixtureSetApexSystemServerJars("myapex:foo", "myapex:bar"),
	).RunTestWithBp(t, `
		prebuilt_apex {
			name: "myapex",
			arch: {
				arm64: {
					src: "myapex-arm64.apex",
				},
				arm: {
					src: "myapex-arm.apex",
				},
			},
			exported_systemserverclasspath_fragments: ["mysystemserverclasspathfragment"],
		}

		java_import {
			name: "foo",
			jars: ["foo.jar"],
			apex_available: [
				"myapex",
			],
		}

		java_import {
			name: "bar",
			jars: ["bar.jar"],
			dex_preopt: {
				profile_guided: true,
			},
			apex_available: [
				"myapex",
			],
		}

		prebuilt_systemserverclasspath_fragment {
			name: "mysystemserverclasspathfragment",
			prefer: true,
			contents: [
				"foo",
				"bar",
			],
			apex_available: [
				"myapex",
			],
		}
	`)

	ctx := result.TestContext

	java.CheckModuleDependencies(t, ctx, "myapex", "android_common_prebuilt_myapex", []string{
		`all_apex_contributions`,
		`dex2oatd`,
		`prebuilt_mysystemserverclasspathfragment`,
	})

	java.CheckModuleDependencies(t, ctx, "mysystemserverclasspathfragment", "android_common_prebuilt_myapex", []string{
		`all_apex_contributions`,
		`prebuilt_bar`,
		`prebuilt_foo`,
	})

	ensureExactDeapexedContents(t, ctx, "myapex", "android_common_prebuilt_myapex", []string{
		"javalib/foo.jar",
		"javalib/bar.jar",
		"javalib/bar.jar.prof",
	})

	assertProfileGuidedPrebuilt(t, ctx, "myapex", "foo", false)
	assertProfileGuidedPrebuilt(t, ctx, "myapex", "bar", true)
}

func TestSystemserverclasspathFragmentStandaloneContents(t *testing.T) {
	t.Parallel()
	result := android.GroupFixturePreparers(
		prepareForTestWithSystemserverclasspathFragment,
		prepareForTestWithMyapex,
		dexpreopt.FixtureSetApexStandaloneSystemServerJars("myapex:foo", "myapex:bar", "myapex:baz"),
	).RunTestWithBp(t, `
		apex {
			name: "myapex",
			key: "myapex.key",
			systemserverclasspath_fragments: [
				"mysystemserverclasspathfragment",
			],
			updatable: false,
		}

		apex_key {
			name: "myapex.key",
			public_key: "testkey.avbpubkey",
			private_key: "testkey.pem",
		}

		java_library {
			name: "foo",
			srcs: ["b.java"],
			installable: true,
			apex_available: [
				"myapex",
			],
		}

		java_library {
			name: "bar",
			srcs: ["c.java"],
			dex_preopt: {
				profile: "bar-art-profile",
			},
			installable: true,
			apex_available: [
				"myapex",
			],
		}

		java_library {
			name: "baz",
			srcs: ["d.java"],
			dex_preopt: {
				profile_guided: true, // ignored
			},
			installable: true,
			apex_available: [
				"myapex",
			],
			sdk_version: "core_current",
		}

		systemserverclasspath_fragment {
			name: "mysystemserverclasspathfragment",
			standalone_contents: [
				"foo",
				"bar",
				"baz",
			],
			apex_available: [
				"myapex",
			],
		}
	`)

	ctx := result.TestContext

	ensureExactContents(t, ctx, "myapex", "android_common_myapex", []string{
		"etc/classpaths/systemserverclasspath.pb",
		"javalib/foo.jar",
		"javalib/bar.jar",
		"javalib/bar.jar.prof",
		"javalib/baz.jar",
	})

	assertProfileGuided(t, ctx, "foo", "android_common_apex10000", false)
	assertProfileGuided(t, ctx, "bar", "android_common_apex10000", true)
	assertProfileGuided(t, ctx, "baz", "android_common_apex10000", false)
}

func TestPrebuiltStandaloneSystemserverclasspathFragmentContents(t *testing.T) {
	t.Parallel()
	result := android.GroupFixturePreparers(
		prepareForTestWithSystemserverclasspathFragment,
		prepareForTestWithMyapex,
		dexpreopt.FixtureSetApexStandaloneSystemServerJars("myapex:foo", "myapex:bar"),
	).RunTestWithBp(t, `
		prebuilt_apex {
			name: "myapex",
			arch: {
				arm64: {
					src: "myapex-arm64.apex",
				},
				arm: {
					src: "myapex-arm.apex",
				},
			},
			exported_systemserverclasspath_fragments: ["mysystemserverclasspathfragment"],
		}

		java_import {
			name: "foo",
			jars: ["foo.jar"],
			apex_available: [
				"myapex",
			],
		}

		java_import {
			name: "bar",
			jars: ["bar.jar"],
			dex_preopt: {
				profile_guided: true,
			},
			apex_available: [
				"myapex",
			],
		}

		prebuilt_systemserverclasspath_fragment {
			name: "mysystemserverclasspathfragment",
			prefer: true,
			standalone_contents: [
				"foo",
				"bar",
			],
			apex_available: [
				"myapex",
			],
		}
	`)

	ctx := result.TestContext

	java.CheckModuleDependencies(t, ctx, "mysystemserverclasspathfragment", "android_common_prebuilt_myapex", []string{
		`all_apex_contributions`,
		`prebuilt_bar`,
		`prebuilt_foo`,
	})

	ensureExactDeapexedContents(t, ctx, "myapex", "android_common_prebuilt_myapex", []string{
		"javalib/foo.jar",
		"javalib/bar.jar",
		"javalib/bar.jar.prof",
	})

	assertProfileGuidedPrebuilt(t, ctx, "myapex", "foo", false)
	assertProfileGuidedPrebuilt(t, ctx, "myapex", "bar", true)
}

func assertProfileGuided(t *testing.T, ctx *android.TestContext, moduleName string, variant string, expected bool) {
	dexpreopt := ctx.ModuleForTests(t, moduleName, variant).Rule("dexpreopt")
	actual := strings.Contains(dexpreopt.RuleParams.Command, "--profile-file=")
	if expected != actual {
		t.Fatalf("Expected profile-guided to be %v, got %v", expected, actual)
	}
}

func assertProfileGuidedPrebuilt(t *testing.T, ctx *android.TestContext, apexName string, moduleName string, expected bool) {
	dexpreopt := ctx.ModuleForTests(t, apexName, "android_common_prebuilt_"+apexName).Rule("dexpreopt." + moduleName)
	actual := strings.Contains(dexpreopt.RuleParams.Command, "--profile-file=")
	if expected != actual {
		t.Fatalf("Expected profile-guided to be %v, got %v", expected, actual)
	}
}

func TestCheckSystemServerOrderWithArtApex(t *testing.T) {
	preparers := android.GroupFixturePreparers(
		java.PrepareForTestWithDexpreopt,
		java.PrepareForTestWithJavaSdkLibraryFiles,
		PrepareForTestWithApexBuildComponents,
		prepareForTestWithArtApex,
		java.FixtureConfigureBootJars("com.android.art:framework-art"),
		dexpreopt.FixtureSetApexSystemServerJars("com.android.apex1:service-apex1", "com.android.art:service-art"),
		java.FixtureWithLastReleaseApis("baz"),
	)

	// Creates a com.android.art apex with a bootclasspath fragment and a systemserverclasspath fragment, and a
	// com.android.apex1 prebuilt whose bootclasspath fragment depends on the com.android.art bootclasspath fragment.
	// Verifies that the checkSystemServerOrder doesn't get confused by the bootclasspath dependencies and report
	// that service-apex1 depends on service-art.
	result := preparers.RunTestWithBp(t, `
		apex {
			name: "com.android.art",
			key: "com.android.art.key",
			bootclasspath_fragments: ["art-bootclasspath-fragment"],
			systemserverclasspath_fragments: ["art-systemserverclasspath-fragment"],
			updatable: false,
		}

		apex_key {
			name: "com.android.art.key",
			public_key: "com.android.art.avbpubkey",
			private_key: "com.android.art.pem",
		}

		bootclasspath_fragment {
			name: "art-bootclasspath-fragment",
			image_name: "art",
			contents: ["framework-art"],
			apex_available: [
				"com.android.art",
			],
			hidden_api: {
				split_packages: ["*"],
			},
		}

		java_library {
			name: "framework-art",
			apex_available: ["com.android.art"],
			srcs: ["a.java"],
			compile_dex: true,
		}

		systemserverclasspath_fragment {
			name: "art-systemserverclasspath-fragment",
			apex_available: ["com.android.art"],
			contents: ["service-art"],
		}

		java_library {
			name: "service-art",
			srcs: ["a.java"],
			apex_available: ["com.android.art"],
			compile_dex: true,
		}

		prebuilt_apex {
			name: "com.android.apex1",
			arch: {
				arm64: {
					src: "myapex-arm64.apex",
				},
				arm: {
					src: "myapex-arm.apex",
				},
			},
			exported_bootclasspath_fragments: ["com.android.apex1-bootclasspath-fragment"],
			exported_systemserverclasspath_fragments: ["com.android.apex1-systemserverclasspath-fragment"],
		}

		prebuilt_bootclasspath_fragment {
			name: "com.android.apex1-bootclasspath-fragment",
			visibility: ["//visibility:public"],
			apex_available: ["com.android.apex1"],
			contents: ["framework-apex1"],
			fragments: [
				{
					apex: "com.android.art",
					module: "art-bootclasspath-fragment",
				},
			],
			hidden_api: {
				annotation_flags: "hiddenapi/annotation-flags.csv",
				metadata: "hiddenapi/metadata.csv",
				index: "hiddenapi/index.csv",
				stub_flags: "hiddenapi/stub-flags.csv",
				all_flags: "hiddenapi/all-flags.csv",
			},
		}

		java_import {
			name: "framework-apex1",
			apex_available: ["com.android.apex1"],
		}

		prebuilt_systemserverclasspath_fragment {
			name: "com.android.apex1-systemserverclasspath-fragment",
			apex_available: ["com.android.apex1"],
			contents: ["service-apex1"],
		}

		java_import {
			name: "service-apex1",
			installable: true,
			apex_available: ["com.android.apex1"],
			sdk_version: "current",
		}`)

	_ = result
}
