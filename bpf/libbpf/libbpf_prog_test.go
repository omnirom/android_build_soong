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

package libbpf_prog

import (
	"os"
	"strings"
	"testing"

	"android/soong/android"
	"android/soong/cc"
)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

var prepareForLibbpfProgTest = android.GroupFixturePreparers(
	cc.PrepareForTestWithCcDefaultModules,
	android.FixtureMergeMockFs(
		map[string][]byte{
			"bpf.c":              nil,
			"bpf_invalid_name.c": nil,
			"BpfTest.cpp":        nil,
		},
	),
	PrepareForTestWithLibbpfProg,
)

func TestLibbpfProgDataDependency(t *testing.T) {
	bp := `
		libbpf_prog {
			name: "bpf.bpf",
			srcs: ["bpf.c"],
		}

		cc_test {
			name: "vts_test_binary_bpf_module",
			compile_multilib: "first",
			srcs: ["BpfTest.cpp"],
			data: [":bpf.bpf"],
			gtest: false,
		}
	`

	prepareForLibbpfProgTest.RunTestWithBp(t, bp)
}

func TestLibbpfProgSourceName(t *testing.T) {
	bp := `
		libbpf_prog {
			name: "bpf_invalid_name.bpf",
			srcs: ["bpf_invalid_name.c"],
		}
	`
	prepareForLibbpfProgTest.ExtendWithErrorHandler(android.FixtureExpectsOneErrorPattern(
		`invalid character '_' in source name`)).
		RunTestWithBp(t, bp)
}

func TestLibbpfProgVendor(t *testing.T) {
	bp := `
		libbpf_prog {
			name: "bpf.bpf",
			srcs: ["bpf.c"],
			vendor: true,
			relative_install_path: "prefix",
		}
	`

	result := prepareForLibbpfProgTest.RunTestWithBp(t, bp)
	module := result.ModuleForTests(t, "bpf.bpf", "android_vendor_arm64_armv8-a").Module().(*libbpfProg)
	data := android.AndroidMkDataForTest(t, result.TestContext, module)
	name := module.BaseModuleName()
	var builder strings.Builder
	data.Custom(&builder, name, "", "", data)
	androidMk := android.StringRelativeToTop(result.Config, builder.String())

	expected := "LOCAL_MODULE_PATH := $(TARGET_OUT_VENDOR_ETC)/bpf/prefix"
	if !strings.Contains(androidMk, expected) {
		t.Errorf("%q is not found in %q", expected, androidMk)
	}
}

func TestLibbpfProgGeneratedHeaderLib(t *testing.T) {
	bp := `
		libbpf_prog {
			name: "bpf.bpf",
			srcs: ["bpf.c"],
			header_libs: ["foo_headers"],
		}

		cc_library_headers {
			name: "foo_headers",
			generated_headers: ["gen_headers"],
			export_generated_headers: ["gen_headers"],
		}

		genrule {
			name: "gen_headers",
			out: ["gen.h"],
			cmd: "touch $(out)",
		}
	`

	result := prepareForLibbpfProgTest.RunTestWithBp(t, bp)

	bpfCc := result.ModuleForTests(t, "bpf.bpf", "android_arm64_armv8-a").Rule("libbpfProgCcRule")
	android.AssertPathsRelativeToTopEquals(t, "expected implicit deps", []string{
		"out/soong/.intermediates/libbpf_headers/libbpf_headers/gen/foo.h",
		"out/soong/.intermediates/gen_headers/gen/gen.h",
	}, bpfCc.Implicits)
}
