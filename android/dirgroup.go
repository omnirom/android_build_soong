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

package android

import (
	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"
)

func init() {
	RegisterDirgroupBuildComponents(InitRegistrationContext)
}

func RegisterDirgroupBuildComponents(ctx RegistrationContext) {
	ctx.RegisterModuleType("dirgroup", DirGroupFactory)
}

type dirGroupProperties struct {
	// dirs lists directories that will be included in this dirgroup
	Dirs proptools.Configurable[[]string] `android:"path"`
}

type dirGroup struct {
	ModuleBase
	DefaultableModuleBase
	properties dirGroupProperties
}

type DirInfo struct {
	Dirs DirectoryPaths
}

var DirProvider = blueprint.NewProvider[DirInfo]()

// dirgroup contains a list of dirs that are referenced by other modules
// properties using the syntax ":<name>". dirgroup are also be used to export
// dirs across package boundaries. Currently the only allowed usage is genrule's
// dir_srcs property.
func DirGroupFactory() Module {
	module := &dirGroup{}
	module.AddProperties(&module.properties)
	InitAndroidModule(module)
	InitDefaultableModule(module)
	return module
}

func (fg *dirGroup) GenerateAndroidBuildActions(ctx ModuleContext) {
	dirs := DirectoryPathsForModuleSrc(ctx, fg.properties.Dirs.GetOrDefault(ctx, nil))
	SetProvider(ctx, DirProvider, DirInfo{Dirs: dirs})
}
