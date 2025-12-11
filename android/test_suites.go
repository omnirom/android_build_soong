// Copyright 2020 Google Inc. All rights reserved.
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

package android

import (
	"github.com/google/blueprint"
)

//go:generate go run ../../blueprint/gobtools/codegen/gob_gen.go

type TestSuiteModule interface {
	Module
	TestSuites() []string
}

// @auto-generate: gob
type TestSuiteInfo struct {
	// A suffix to append to the name of the test.
	// Useful because historically different variants of soong modules became differently-named
	// make modules, like "my_test.vendor" for the vendor variant.
	NameSuffix string

	TestSuites []string

	NeedsArchFolder bool

	MainFile Path

	MainFileStem string

	MainFileExt string

	ConfigFile Path

	ConfigFileSuffix string

	ExtraConfigs Paths

	PerTestcaseDirectory bool

	Data []DataPath

	NonArchData []DataPath

	CompatibilitySupportFiles []Path

	// Eqivalent of LOCAL_DISABLE_TEST_CONFIG in make
	DisableTestConfig bool

	// Eqivalent of LOCAL_IS_UNIT_TEST in make
	IsUnitTest bool
}

var TestSuiteInfoProvider = blueprint.NewProvider[TestSuiteInfo]()

// TestSuiteSharedLibsInfo is a provider of AndroidMk names of shared lib modules, for packaging
// shared libs into test suites. It's not intended as a general-purpose shared lib tracking
// mechanism. It's added to both test modules (to track their shared libs) and also shared lib
// modules (to track their transitive shared libs).
// @auto-generate: gob
type TestSuiteSharedLibsInfo struct {
	MakeNames []string
}

var TestSuiteSharedLibsInfoProvider = blueprint.NewProvider[TestSuiteSharedLibsInfo]()

// MakeNameInfoProvider records the AndroidMk name for the module. This will match the names
// referenced in TestSuiteSharedLibsInfo
// @auto-generate: gob
type MakeNameInfo struct {
	Name string
}

var MakeNameInfoProvider = blueprint.NewProvider[MakeNameInfo]()

// @auto-generate: gob
type FilePair struct {
	Src Path
	Dst WritablePath
}

// @auto-generate: gob
type TestSuiteInstallsInfo struct {
	Files              []FilePair
	OneVariantInstalls []FilePair
}

var TestSuiteInstallsInfoProvider = blueprint.NewProvider[TestSuiteInstallsInfo]()
