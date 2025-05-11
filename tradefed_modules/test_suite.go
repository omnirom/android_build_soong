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

package tradefed_modules

import (
	"encoding/json"
	"path"
	"path/filepath"

	"android/soong/android"
	"android/soong/tradefed"
	"github.com/google/blueprint"
)

const testSuiteModuleType = "test_suite"

type testSuiteTag struct{
	blueprint.BaseDependencyTag
}

type testSuiteManifest struct {
	Name  string `json:"name"`
	Files []string `json:"files"`
}

func init() {
	RegisterTestSuiteBuildComponents(android.InitRegistrationContext)
}

func RegisterTestSuiteBuildComponents(ctx android.RegistrationContext) {
	ctx.RegisterModuleType(testSuiteModuleType, TestSuiteFactory)
}

var PrepareForTestWithTestSuiteBuildComponents = android.GroupFixturePreparers(
	android.FixtureRegisterWithContext(RegisterTestSuiteBuildComponents),
)

type testSuiteProperties struct {
	Description string
	Tests []string `android:"path,arch_variant"`
}

type testSuiteModule struct {
	android.ModuleBase
	android.DefaultableModuleBase
	testSuiteProperties
}

func (t *testSuiteModule) DepsMutator(ctx android.BottomUpMutatorContext) {
	for _, test := range t.Tests {
		if ctx.OtherModuleDependencyVariantExists(ctx.Config().BuildOSCommonTarget.Variations(), test) {
			// Host tests.
			ctx.AddVariationDependencies(ctx.Config().BuildOSCommonTarget.Variations(), testSuiteTag{}, test)
		} else {
			// Target tests.
			ctx.AddDependency(ctx.Module(), testSuiteTag{}, test)
		}
	}
}

func (t *testSuiteModule) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	suiteName := ctx.ModuleName()
	modulesByName := make(map[string]android.Module)
	ctx.WalkDeps(func(child, parent android.Module) bool {
		// Recurse into test_suite dependencies.
		if ctx.OtherModuleType(child) == testSuiteModuleType {
			ctx.Phony(suiteName, android.PathForPhony(ctx, child.Name()))
			return true
		}

		// Only write out top level test suite dependencies here.
		if _, ok := ctx.OtherModuleDependencyTag(child).(testSuiteTag); !ok {
			return false
		}

		if !child.InstallInTestcases() {
			ctx.ModuleErrorf("test_suite only supports modules installed in testcases. %q is not installed in testcases.", child.Name())
			return false
		}

		modulesByName[child.Name()] = child
		return false
	})

	var files []string
	for name, module := range modulesByName {
		// Get the test provider data from the child.
		tp, ok := android.OtherModuleProvider(ctx, module, tradefed.BaseTestProviderKey)
		if !ok {
			// TODO: Consider printing out a list of all module types.
			ctx.ModuleErrorf("%q is not a test module.", name)
			continue
		}

		files = append(files, packageModuleFiles(ctx, suiteName, module, tp)...)
		ctx.Phony(suiteName, android.PathForPhony(ctx, name))
	}

	manifestPath := android.PathForSuiteInstall(ctx, suiteName, suiteName+".json")
	b, err := json.Marshal(testSuiteManifest{Name: suiteName, Files: files})
	if err != nil {
		ctx.ModuleErrorf("Failed to marshal manifest: %v", err)
		return
	}
	android.WriteFileRule(ctx, manifestPath, string(b))

	ctx.Phony(suiteName, manifestPath)
}

func TestSuiteFactory() android.Module {
	module := &testSuiteModule{}
	module.AddProperties(&module.testSuiteProperties)

	android.InitAndroidArchModule(module, android.HostAndDeviceSupported, android.MultilibCommon)
	android.InitDefaultableModule(module)

	return module
}

func packageModuleFiles(ctx android.ModuleContext, suiteName string, module android.Module, tp tradefed.BaseTestProviderData) []string {

	hostOrTarget := "target"
	if tp.IsHost {
		hostOrTarget = "host"
	}

	// suiteRoot at out/soong/packaging/<suiteName>.
	suiteRoot := android.PathForSuiteInstall(ctx, suiteName)

	var installed android.InstallPaths
	// Install links to installed files from the module.
	if installFilesInfo, ok := android.OtherModuleProvider(ctx, module, android.InstallFilesProvider); ok {
		for _, f := range installFilesInfo.InstallFiles {
			// rel is anything under .../<partition>, normally under .../testcases.
			rel := android.Rel(ctx, f.PartitionDir(), f.String())

			// Install the file under <suiteRoot>/<host|target>/<partition>.
			installDir := suiteRoot.Join(ctx, hostOrTarget, f.Partition(), path.Dir(rel))
			linkTo, err := filepath.Rel(installDir.String(), f.String())
			if err != nil {
				ctx.ModuleErrorf("Failed to get relative path from %s to %s: %v", installDir.String(), f.String(), err)
				continue
			}
			installed = append(installed, ctx.InstallAbsoluteSymlink(installDir, path.Base(rel), linkTo))
		}
	}

	// Install config file.
	if tp.TestConfig != nil {
		moduleRoot := suiteRoot.Join(ctx, hostOrTarget, "testcases", module.Name())
		installed = append(installed, ctx.InstallFile(moduleRoot, module.Name() + ".config", tp.TestConfig))
	}

	// Add to phony and manifest, manifestpaths are relative to suiteRoot.
	var manifestEntries []string
	for _, f := range installed {
		manifestEntries = append(manifestEntries, android.Rel(ctx, suiteRoot.String(), f.String()))
		ctx.Phony(suiteName, f)
	}
	return manifestEntries
}
