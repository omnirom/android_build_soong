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

package rust

import (
	"android/soong/android"
)

func init() {
	// Rust objects are object files built with --emit=obj.
	android.RegisterModuleType("rust_object", RustObjectFactory)
	android.RegisterModuleType("rust_object_host", RustObjectHostFactory)
}

type ObjectProperties struct {
	// Indicates that this module is a CRT object. CRT objects will be split
	// into a variant per-API level between min_sdk_version and current.
	Crt *bool
}

type objectDecorator struct {
	*baseCompiler
	Properties ObjectProperties
}

type objectInterface interface {
	crt() bool
}

var _ objectInterface = (*objectDecorator)(nil)
var _ compiler = (*objectDecorator)(nil)

// rust_object produces an object file.
func RustObjectFactory() android.Module {
	module, _ := NewRustObject(android.HostAndDeviceSupported)
	return module.Init()
}

// rust_object_host produces an object file for host only.
func RustObjectHostFactory() android.Module {
	module, _ := NewRustObject(android.HostSupported)
	return module.Init()
}

func NewRustObject(hod android.HostOrDeviceSupported) (*Module, *objectDecorator) {
	module := newModule(hod, android.MultilibFirst)

	object := &objectDecorator{
		baseCompiler: NewBaseCompiler("", "", NoInstall),
	}

	module.compiler = object

	return module, object
}

func (object *objectDecorator) begin(ctx BaseModuleContext) {
	object.baseCompiler.begin(ctx)
}

func (object *objectDecorator) compilerFlags(ctx ModuleContext, flags Flags) Flags {
	flags = object.baseCompiler.compilerFlags(ctx, flags)
	return flags
}

func (object *objectDecorator) compilerDeps(ctx DepsContext, deps Deps) Deps {
	deps = object.baseCompiler.compilerDeps(ctx, deps)
	return deps
}

func (object *objectDecorator) crt() bool {
	return Bool(object.Properties.Crt)
}

func (object *objectDecorator) compilerProps() []interface{} {
	return append(object.baseCompiler.compilerProps(),
		&object.Properties)
}

func (object *objectDecorator) nativeCoverage() bool {
	return true
}

func (object *objectDecorator) emitType() string {
	return "obj"
}

func (object *objectDecorator) compile(ctx ModuleContext, flags Flags, deps PathDeps) buildOutput {
	fileName := object.getStem(ctx) + ".o"
	outputFile := android.PathForModuleOut(ctx, fileName)
	ret := buildOutput{outputFile: outputFile}
	crateRootPath := object.crateRootPath(ctx)

	flags.RustFlags = append(flags.RustFlags, deps.depFlags...)
	object.baseCompiler.unstrippedOutputFile = outputFile

	ret.kytheFile = TransformSrcToObject(ctx, crateRootPath, deps, flags, outputFile).kytheFile
	return ret
}

func (object *objectDecorator) autoDep(ctx android.BottomUpMutatorContext) autoDep {
	// rust_objects are not linked so selecting deps for them doesn't make sense.
	panic("rust_objects should not declare rustlibs dependencies")
}

func (object *objectDecorator) install(ctx ModuleContext) {
	// Objects aren't installable, so do nothing.
}

func (object *objectDecorator) everInstallable() bool {
	// Objects aren't installable.
	return false
}
