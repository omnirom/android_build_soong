// Copyright (C) 2025 The Android Open Source Project
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
)

//
// cc_device_for_host serves as an intermediate module, making the output of a device module
// available to host modules. This can only be used for the ART simulator.
//

type deviceForHost struct {
	properties deviceForHostProperties
}

type deviceForHostProperties struct {
	// List of modules whose contents will be visible to modules that depend on this module.
	Srcs []string
}

type deviceForHostLinker struct {
	*baseLinker
}

func init() {
	android.RegisterModuleType("cc_device_for_host", deviceForHostFactory)
}

func deviceForHostFactory() android.Module {
	// This module only supports host builds so it is visible to other host modules.
	module := newBaseModule(android.HostSupported, android.MultilibBoth)
	module.sanitize = &sanitize{}
	module.linker = &deviceForHostLinker{
		baseLinker: NewBaseLinker(module.sanitize),
	}
	module.converter = &deviceForHost{}

	return module.Init()
}

var deviceForHostDepTag = dependencyTag{name: "device_for_host"}

func (deviceForHost *deviceForHost) converterProps() []interface{} {
	return []interface{}{&deviceForHost.properties}
}

func (deviceForHost *deviceForHost) getSrcs() []string {
	return deviceForHost.properties.Srcs
}

func (linker *deviceForHostLinker) link(ctx ModuleContext, flags Flags, deps PathDeps,
	objs Objects) android.Path {
	return deps.deviceFileForHost
}

func (linker *deviceForHostLinker) unstrippedOutputFilePath() android.Path {
	return nil
}

func (linker *deviceForHostLinker) strippedAllOutputFilePath() android.Path {
	return nil
}

func (linker *deviceForHostLinker) nativeCoverage() bool {
	return true
}

func (linker *deviceForHostLinker) coverageOutputFilePath() android.OptionalPath {
	return android.OptionalPath{}
}

func (linker *deviceForHostLinker) defaultDistFiles() []android.Path {
	return nil
}
