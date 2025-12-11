// Copyright 2025 The Android Open Source Project
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

package cipd

import (
	"android/soong/android"
	"slices"
	"testing"
)

func TestCipdPackage(t *testing.T) {
	bp := `
	cipd_package {
		name: "cipd_package1",
		package: "android/prebuilts/package1",
		version: "version1",
		files: [
			"package1_file1",
			"package1_file2",
		],
		resolved_versions_file: "cipd.versions",
	}
	`

	result := android.GroupFixturePreparers(
		android.PrepareForTestWithAndroidBuildComponents,
		android.FixtureRegisterWithContext(RegisterCipdComponents),
	).RunTestWithBp(t, bp)

	module := result.ModuleForTests(t, "cipd_package1", "")
	export := module.Rule("cipd_export")

	intermediateDir := "out/soong/.intermediates/cipd_package1"
	wantEnsureFile := intermediateDir + "/ensure.txt"
	if export.Input.String() != wantEnsureFile {
		t.Errorf("export.Input.String() = %v, want %v", export.Input.String(), wantEnsureFile)
	}
	if len(export.Inputs) != 0 {
		t.Errorf("len(export.Inputs) = %v, want 0", len(export.Inputs))
	}

	wantRoot := intermediateDir + "/package"
	wantExportOutputs := []string{
		wantRoot + "/package1_file1",
		wantRoot + "/package1_file2",
	}

	var gotExportOutputs []string
	for _, output := range export.Outputs {
		gotExportOutputs = append(gotExportOutputs, output.String())
	}
	if !slices.Equal(wantExportOutputs, gotExportOutputs) {
		t.Errorf("export.Outputs = %v, want %v", gotExportOutputs, wantExportOutputs)
	}
	if export.Output != nil {
		t.Errorf("export.Output = %v, want nil", export.Output)
	}
	if export.Args["root"] != wantRoot {
		t.Errorf("export.Args{\"root\"] = %v, want %v", export.Args["root"], wantRoot)
	}
	if len(export.Args) != 1 {
		t.Errorf("len(export.Args) = %v, want 1", len(export.Args))
	}

	zipRule := module.Rule("soong_zip_from_dir")
	wantZipFile := intermediateDir + "/package.zip"
	if zipRule.Output.String() != wantZipFile {
		t.Errorf("zipRule.Output = %q, want %q", zipRule.Output.String(), wantZipFile)
	}

	if zipRule.Input.String() != wantEnsureFile {
		t.Errorf("zipRule.Input.String() = %q, want %q", zipRule.Input.String(), wantEnsureFile)
	}
	if len(zipRule.Args) != 1 {
		t.Fatalf("len(zipRule.Args) = %v, want 1 (was %v)", len(zipRule.Args), zipRule.Args)
	}
	wantTempZipDir := intermediateDir + "/zip_temp_pkg_dir"
	if zipRule.Args["tempZipDir"] != wantTempZipDir {
		t.Errorf("zipRule.Args[\"tempZipDir\"] = %q, want %q", zipRule.Args["tempZipDir"], wantTempZipDir)
	}

	zipTaggedOutputs := module.OutputFiles(result.TestContext, t, ".zip")
	if len(zipTaggedOutputs) != 1 {
		t.Errorf("len(module.OutputFiles(..., \".zip\")) = %d, want 1", len(zipTaggedOutputs))
	}
	if val := zipTaggedOutputs[0].String(); val != wantZipFile {
		t.Errorf("module.OutputFiles(..., \".zip\")[0] = %q, want %q", val, wantZipFile)
	}
}
