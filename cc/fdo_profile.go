// Copyright 2023 Google Inc. All rights reserved.
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

package cc

import (
	"android/soong/android"
	"github.com/google/blueprint"

	"github.com/google/blueprint/proptools"
)

func init() {
	RegisterFdoProfileBuildComponents(android.InitRegistrationContext)
}

func RegisterFdoProfileBuildComponents(ctx android.RegistrationContext) {
	ctx.RegisterModuleType("fdo_profile", FdoProfileFactory)
}

type fdoProfile struct {
	android.ModuleBase

	properties fdoProfileProperties
}

type fdoProfileProperties struct {
	Profile proptools.Configurable[string] `android:"arch_variant,replace_instead_of_append"`
}

// FdoProfileInfo is provided by FdoProfileProvider
type FdoProfileInfo struct {
	Path android.Path
}

// FdoProfileProvider is used to provide path to an fdo profile
var FdoProfileProvider = blueprint.NewProvider[FdoProfileInfo]()

// GenerateAndroidBuildActions of fdo_profile does not have any build actions
func (fp *fdoProfile) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	profile := fp.properties.Profile.GetOrDefault(ctx, "")
	if profile != "" {
		path := android.PathForModuleSrc(ctx, profile)
		android.SetProvider(ctx, FdoProfileProvider, FdoProfileInfo{
			Path: path,
		})
	}
}

func FdoProfileFactory() android.Module {
	m := &fdoProfile{}
	m.AddProperties(&m.properties)
	android.InitAndroidMultiTargetsArchModule(m, android.DeviceSupported, android.MultilibBoth)
	return m
}
