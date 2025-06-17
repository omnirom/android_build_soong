// Copyright 2017 Google Inc. All rights reserved.
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

package python

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"android/soong/android"
	"android/soong/cc"

	"github.com/google/blueprint"
)

type pyModule struct {
	name          string
	actualVersion string
	pyRunfiles    []string
	srcsZip       string
	depsSrcsZips  []string
}

var (
	buildNamePrefix          = "soong_python_test"
	moduleVariantErrTemplate = `%s: module %q variant "[a-zA-Z0-9_]*": `
	pkgPathErrTemplate       = moduleVariantErrTemplate +
		"pkg_path: %q must be a relative path contained in par file."
	badIdentifierErrTemplate = moduleVariantErrTemplate +
		"srcs: the path %q contains invalid subpath %q."
	dupRunfileErrTemplate = moduleVariantErrTemplate +
		"found two files to be placed at the same location within zip %q." +
		" First file: in module %s at path %q." +
		" Second file: in module %s at path %q."
	badSrcFileExtErr  = moduleVariantErrTemplate + `srcs: found non \(.py\|.proto\) file: %q!`
	badDataFileExtErr = moduleVariantErrTemplate + `data: found \(.py\) file: %q!`
	bpFile            = "Android.bp"

	data = []struct {
		desc      string
		mockFiles android.MockFS

		errors           []string
		expectedBinaries []pyModule
	}{
		{
			desc: "module with bad src file ext",
			mockFiles: map[string][]byte{
				filepath.Join("dir", bpFile): []byte(
					`python_library_host {
						name: "lib1",
						srcs: [
							"file1.exe",
						],
					}`,
				),
				"dir/file1.exe": nil,
			},
			errors: []string{
				fmt.Sprintf(badSrcFileExtErr,
					"dir/Android.bp:3:11", "lib1", "dir/file1.exe"),
			},
		},
		{
			desc: "module with bad data file ext",
			mockFiles: map[string][]byte{
				filepath.Join("dir", bpFile): []byte(
					`python_library_host {
						name: "lib1",
						srcs: [
							"file1.py",
						],
						data: [
							"file2.py",
						],
					}`,
				),
				"dir/file1.py": nil,
				"dir/file2.py": nil,
			},
			errors: []string{
				fmt.Sprintf(badDataFileExtErr,
					"dir/Android.bp:6:11", "lib1", "dir/file2.py"),
			},
		},
		{
			desc: "module with bad pkg_path format",
			mockFiles: map[string][]byte{
				filepath.Join("dir", bpFile): []byte(
					`python_library_host {
						name: "lib1",
						pkg_path: "a/c/../../",
						srcs: [
							"file1.py",
						],
					}

					python_library_host {
						name: "lib2",
						pkg_path: "a/c/../../../",
						srcs: [
							"file1.py",
						],
					}

					python_library_host {
						name: "lib3",
						pkg_path: "/a/c/../../",
						srcs: [
							"file1.py",
						],
					}`,
				),
				"dir/file1.py": nil,
			},
			errors: []string{
				fmt.Sprintf(pkgPathErrTemplate,
					"dir/Android.bp:11:15", "lib2", "a/c/../../../"),
				fmt.Sprintf(pkgPathErrTemplate,
					"dir/Android.bp:19:15", "lib3", "/a/c/../../"),
			},
		},
		{
			desc: "module with bad runfile src path format",
			mockFiles: map[string][]byte{
				filepath.Join("dir", bpFile): []byte(
					`python_library_host {
						name: "lib1",
						pkg_path: "a/b/c/",
						srcs: [
							".file1.py",
							"123/file1.py",
							"-e/f/file1.py",
						],
					}`,
				),
				"dir/.file1.py":     nil,
				"dir/123/file1.py":  nil,
				"dir/-e/f/file1.py": nil,
			},
			errors: []string{
				fmt.Sprintf(badIdentifierErrTemplate, "dir/Android.bp:4:11",
					"lib1", "a/b/c/-e/f/file1.py", "-e"),
				fmt.Sprintf(badIdentifierErrTemplate, "dir/Android.bp:4:11",
					"lib1", "a/b/c/.file1.py", ".file1"),
				fmt.Sprintf(badIdentifierErrTemplate, "dir/Android.bp:4:11",
					"lib1", "a/b/c/123/file1.py", "123"),
			},
		},
		{
			desc: "module with duplicate runfile path",
			mockFiles: map[string][]byte{
				filepath.Join("dir", bpFile): []byte(
					`python_library_host {
						name: "lib1",
						pkg_path: "a/b/",
						srcs: [
							"c/file1.py",
						],
					}

					python_library_host {
						name: "lib2",
						pkg_path: "a/b/c/",
						srcs: [
							"file1.py",
						],
						libs: [
							"lib1",
						],
					}

					python_binary_host {
						name: "bin",
						pkg_path: "e/",
						srcs: [
							"bin.py",
						],
						libs: [
							"lib2",
						],
					}
					`,
				),
				"dir/c/file1.py": nil,
				"dir/file1.py":   nil,
				"dir/bin.py":     nil,
			},
			errors: []string{
				fmt.Sprintf(dupRunfileErrTemplate, "dir/Android.bp:20:6",
					"bin", "a/b/c/file1.py", "bin", "dir/file1.py",
					"lib1", "dir/c/file1.py"),
			},
		},
	}
)

func TestPythonModule(t *testing.T) {
	for _, d := range data {
		d.mockFiles[filepath.Join("common", bpFile)] = []byte(`
python_library {
  name: "py3-stdlib",
  host_supported: true,
}
cc_binary {
  name: "py3-launcher",
  host_supported: true,
}
`)

		t.Run(d.desc, func(t *testing.T) {
			result := android.GroupFixturePreparers(
				android.PrepareForTestWithDefaults,
				android.PrepareForTestWithArchMutator,
				android.PrepareForTestWithAllowMissingDependencies,
				cc.PrepareForTestWithCcDefaultModules,
				PrepareForTestWithPythonBuildComponents,
				d.mockFiles.AddToFixture(),
			).ExtendWithErrorHandler(android.FixtureExpectsAllErrorsToMatchAPattern(d.errors)).
				RunTest(t)

			if len(result.Errs) > 0 {
				return
			}

			for _, e := range d.expectedBinaries {
				t.Run(e.name, func(t *testing.T) {
					expectModule(t, result.TestContext, e.name, e.actualVersion, e.srcsZip, e.pyRunfiles)
				})
			}
		})
	}
}

func TestTestOnlyProvider(t *testing.T) {
	t.Parallel()
	ctx := android.GroupFixturePreparers(
		PrepareForTestWithPythonBuildComponents,
		android.PrepareForTestWithAllowMissingDependencies,
	).RunTestWithBp(t, `
                // These should be test-only
                python_library { name: "py-lib-test", test_only: true }
                python_library { name: "py-lib-test-host", test_only: true, host_supported: true }
                python_test {    name: "py-test", srcs: ["py-test.py"] }
                python_test_host { name: "py-test-host", srcs: ["py-test-host.py"] }
                python_binary_host { name: "py-bin-test", srcs: ["py-bin-test.py"] }

                // These should not be.
                python_library { name: "py-lib" }
                python_binary_host { name: "py-bin", srcs: ["py-bin.py"] }
	`)

	// Visit all modules and ensure only the ones that should
	// marked as test-only are marked as test-only.

	actualTestOnly := []string{}
	ctx.VisitAllModules(func(m blueprint.Module) {
		if provider, ok := android.OtherModuleProvider(ctx.TestContext.OtherModuleProviderAdaptor(), m, android.TestOnlyProviderKey); ok {
			if provider.TestOnly {
				actualTestOnly = append(actualTestOnly, m.Name())
			}
		}
	})
	expectedTestOnlyModules := []string{
		"py-lib-test",
		"py-lib-test-host",
		"py-test",
		"py-test-host",
	}

	notEqual, left, right := android.ListSetDifference(expectedTestOnlyModules, actualTestOnly)
	if notEqual {
		t.Errorf("test-only: Expected but not found: %v, Found but not expected: %v", left, right)
	}
}

// Don't allow setting test-only on things that are always tests or never tests.
func TestInvalidTestOnlyTargets(t *testing.T) {
	testCases := []string{
		` python_test { name: "py-test", test_only: true, srcs: ["py-test.py"] } `,
		` python_test_host { name: "py-test-host", test_only: true, srcs: ["py-test-host.py"] } `,
		` python_defaults { name: "py-defaults", test_only: true, srcs: ["foo.py"] } `,
	}

	for i, bp := range testCases {
		ctx := android.GroupFixturePreparers(
			PrepareForTestWithPythonBuildComponents,
			android.PrepareForTestWithAllowMissingDependencies).
			ExtendWithErrorHandler(android.FixtureIgnoreErrors).
			RunTestWithBp(t, bp)
		if len(ctx.Errs) != 1 {
			t.Errorf("Expected err setting test_only in testcase #%d: %d errs", i, len(ctx.Errs))
			continue
		}
		if !strings.Contains(ctx.Errs[0].Error(), "unrecognized property \"test_only\"") {
			t.Errorf("ERR: %s bad bp: %s", ctx.Errs[0], bp)
		}
	}
}

func expectModule(t *testing.T, ctx *android.TestContext, name, variant, expectedSrcsZip string, expectedPyRunfiles []string) {
	module := ctx.ModuleForTests(t, name, variant)

	base, baseOk := module.Module().(*PythonLibraryModule)
	if !baseOk {
		t.Fatalf("%s is not Python module!", name)
	}

	actualPyRunfiles := []string{}
	for _, path := range base.srcsPathMappings {
		actualPyRunfiles = append(actualPyRunfiles, path.dest)
	}

	android.AssertDeepEquals(t, "pyRunfiles", expectedPyRunfiles, actualPyRunfiles)

	android.AssertPathRelativeToTopEquals(t, "srcsZip", expectedSrcsZip, base.srcsZip)
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
