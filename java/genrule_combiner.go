// Copyright 2019 Google Inc. All rights reserved.
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

package java

import (
	"fmt"
	"io"

	"android/soong/android"
	"android/soong/dexpreopt"

	"github.com/google/blueprint/depset"
	"github.com/google/blueprint/proptools"
)

type GenruleCombiner struct {
	android.ModuleBase
	android.DefaultableModuleBase

	genruleCombinerProperties GenruleCombinerProperties

	headerJars                    android.Paths
	implementationJars            android.Paths
	implementationAndResourceJars android.Paths
	resourceJars                  android.Paths
	aconfigProtoFiles             android.Paths

	srcJarArgs []string
	srcJarDeps android.Paths

	headerDirs android.Paths

	combinedHeaderJar         android.Path
	combinedImplementationJar android.Path
}

type GenruleCombinerProperties struct {
	// List of modules whose implementation (and resources) jars will be visible to modules
	// that depend on this module.
	Static_libs proptools.Configurable[[]string] `android:"arch_variant"`

	// List of modules whose header jars will be visible to modules that depend on this module.
	Headers proptools.Configurable[[]string] `android:"arch_variant"`
}

// java_genrule_combiner provides the implementation and resource jars from `static_libs`, with
// the header jars from `headers`.
//
// This is useful when a java_genrule is used to change the implementation of a java library
// without requiring a change in the header jars.
func GenruleCombinerFactory() android.Module {
	module := &GenruleCombiner{}

	module.AddProperties(&module.genruleCombinerProperties)
	InitJavaModule(module, android.HostAndDeviceSupported)
	return module
}

var genruleCombinerHeaderDepTag = dependencyTag{name: "genrule_combiner_header"}

func (j *GenruleCombiner) DepsMutator(ctx android.BottomUpMutatorContext) {
	ctx.AddVariationDependencies(nil, staticLibTag,
		j.genruleCombinerProperties.Static_libs.GetOrDefault(ctx, nil)...)
	ctx.AddVariationDependencies(nil, genruleCombinerHeaderDepTag,
		j.genruleCombinerProperties.Headers.GetOrDefault(ctx, nil)...)
}

func (j *GenruleCombiner) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	if len(j.genruleCombinerProperties.Static_libs.GetOrDefault(ctx, nil)) < 1 {
		ctx.PropertyErrorf("static_libs", "at least one dependency is required")
	}

	if len(j.genruleCombinerProperties.Headers.GetOrDefault(ctx, nil)) < 1 {
		ctx.PropertyErrorf("headers", "at least one dependency is required")
	}

	var transitiveHeaderJars []depset.DepSet[android.Path]
	var transitiveImplementationJars []depset.DepSet[android.Path]
	var transitiveResourceJars []depset.DepSet[android.Path]
	var sdkVersion android.SdkSpec
	var stubsLinkType StubsLinkType
	moduleWithSdkDepInfo := &ModuleWithSdkDepInfo{}

	// Collect the headers first, so that aconfig flag values for the libraries will override
	// values from the headers (if they are different).
	ctx.VisitDirectDepsWithTag(genruleCombinerHeaderDepTag, func(m android.Module) {
		if dep, ok := android.OtherModuleProvider(ctx, m, JavaInfoProvider); ok {
			j.headerJars = append(j.headerJars, dep.HeaderJars...)

			j.srcJarArgs = append(j.srcJarArgs, dep.SrcJarArgs...)
			j.srcJarDeps = append(j.srcJarDeps, dep.SrcJarDeps...)
			j.aconfigProtoFiles = append(j.aconfigProtoFiles, dep.AconfigIntermediateCacheOutputPaths...)
			sdkVersion = dep.SdkVersion
			stubsLinkType = dep.StubsLinkType
			*moduleWithSdkDepInfo = *dep.ModuleWithSdkDepInfo

			transitiveHeaderJars = append(transitiveHeaderJars, dep.TransitiveStaticLibsHeaderJars)
		} else if dep, ok := android.OtherModuleProvider(ctx, m, android.CodegenInfoProvider); ok {
			j.aconfigProtoFiles = append(j.aconfigProtoFiles, dep.IntermediateCacheOutputPaths...)
		} else {
			ctx.PropertyErrorf("headers", "module %q cannot be used as a dependency", ctx.OtherModuleName(m))
		}
	})
	ctx.VisitDirectDepsWithTag(staticLibTag, func(m android.Module) {
		if dep, ok := android.OtherModuleProvider(ctx, m, JavaInfoProvider); ok {
			j.implementationJars = append(j.implementationJars, dep.ImplementationJars...)
			j.implementationAndResourceJars = append(j.implementationAndResourceJars, dep.ImplementationAndResourcesJars...)
			j.resourceJars = append(j.resourceJars, dep.ResourceJars...)

			transitiveImplementationJars = append(transitiveImplementationJars, dep.TransitiveStaticLibsImplementationJars)
			transitiveResourceJars = append(transitiveResourceJars, dep.TransitiveStaticLibsResourceJars)
			j.aconfigProtoFiles = append(j.aconfigProtoFiles, dep.AconfigIntermediateCacheOutputPaths...)
		} else if dep, ok := android.OtherModuleProvider(ctx, m, android.OutputFilesProvider); ok {
			// This is provided by `java_genrule` modules.
			j.implementationJars = append(j.implementationJars, dep.DefaultOutputFiles...)
			j.implementationAndResourceJars = append(j.implementationAndResourceJars, dep.DefaultOutputFiles...)
			stubsLinkType = Implementation
		} else {
			ctx.PropertyErrorf("static_libs", "module %q cannot be used as a dependency", ctx.OtherModuleName(m))
		}
	})

	jarName := ctx.ModuleName() + ".jar"

	if len(j.implementationAndResourceJars) > 1 {
		outputFile := android.PathForModuleOut(ctx, "combined", jarName)
		TransformJarsToJar(ctx, outputFile, "combine", j.implementationAndResourceJars,
			android.OptionalPath{}, false, nil, nil)
		j.combinedImplementationJar = outputFile
	} else if len(j.implementationAndResourceJars) == 1 {
		j.combinedImplementationJar = j.implementationAndResourceJars[0]
	}

	if len(j.headerJars) > 1 {
		outputFile := android.PathForModuleOut(ctx, "turbine-combined", jarName)
		TransformJarsToJar(ctx, outputFile, "turbine combine", j.headerJars,
			android.OptionalPath{}, false, nil, []string{"META-INF/TRANSITIVE"})
		j.combinedHeaderJar = outputFile
		j.headerDirs = append(j.headerDirs, android.PathForModuleOut(ctx, "turbine-combined"))
	} else if len(j.headerJars) == 1 {
		j.combinedHeaderJar = j.headerJars[0]
	}

	javaInfo := &JavaInfo{
		HeaderJars:                             android.Paths{j.combinedHeaderJar},
		LocalHeaderJars:                        android.Paths{j.combinedHeaderJar},
		TransitiveStaticLibsHeaderJars:         depset.New(depset.PREORDER, android.Paths{j.combinedHeaderJar}, transitiveHeaderJars),
		TransitiveStaticLibsImplementationJars: depset.New(depset.PREORDER, android.Paths{j.combinedImplementationJar}, transitiveImplementationJars),
		TransitiveStaticLibsResourceJars:       depset.New(depset.PREORDER, nil, transitiveResourceJars),
		GeneratedSrcjars:                       android.Paths{j.combinedImplementationJar},
		ImplementationAndResourcesJars:         android.Paths{j.combinedImplementationJar},
		ImplementationJars:                     android.Paths{j.combinedImplementationJar},
		ModuleWithSdkDepInfo:                   moduleWithSdkDepInfo,
		ResourceJars:                           j.resourceJars,
		OutputFile:                             j.combinedImplementationJar,
		SdkVersion:                             sdkVersion,
		SrcJarArgs:                             j.srcJarArgs,
		SrcJarDeps:                             j.srcJarDeps,
		StubsLinkType:                          stubsLinkType,
		AconfigIntermediateCacheOutputPaths:    j.aconfigProtoFiles,
	}
	setExtraJavaInfo(ctx, j, javaInfo)
	ctx.SetOutputFiles(android.Paths{javaInfo.OutputFile}, "")
	ctx.SetOutputFiles(android.Paths{javaInfo.OutputFile}, android.DefaultDistTag)
	ctx.SetOutputFiles(javaInfo.ImplementationAndResourcesJars, ".jar")
	ctx.SetOutputFiles(javaInfo.HeaderJars, ".hjar")
	android.SetProvider(ctx, JavaInfoProvider, javaInfo)

}

func (j *GenruleCombiner) GeneratedSourceFiles() android.Paths {
	return append(android.Paths{}, j.combinedImplementationJar)
}

func (j *GenruleCombiner) GeneratedHeaderDirs() android.Paths {
	return append(android.Paths{}, j.headerDirs...)
}

func (j *GenruleCombiner) GeneratedDeps() android.Paths {
	return append(android.Paths{}, j.combinedImplementationJar)
}

func (j *GenruleCombiner) Srcs() android.Paths {
	return append(android.Paths{}, j.implementationAndResourceJars...)
}

func (j *GenruleCombiner) HeaderJars() android.Paths {
	return j.headerJars
}

func (j *GenruleCombiner) ImplementationAndResourcesJars() android.Paths {
	return j.implementationAndResourceJars
}

func (j *GenruleCombiner) DexJarBuildPath(ctx android.ModuleErrorfContext) android.Path {
	return nil
}

func (j *GenruleCombiner) DexJarInstallPath() android.Path {
	return nil
}

func (j *GenruleCombiner) AidlIncludeDirs() android.Paths {
	return nil
}

func (j *GenruleCombiner) ClassLoaderContexts() dexpreopt.ClassLoaderContextMap {
	return nil
}

func (j *GenruleCombiner) JacocoReportClassesFile() android.Path {
	return nil
}

func (j *GenruleCombiner) AndroidMk() android.AndroidMkData {
	return android.AndroidMkData{
		Class:      "JAVA_LIBRARIES",
		OutputFile: android.OptionalPathForPath(j.combinedImplementationJar),
		// Make does not support Windows Java modules
		Disabled: j.Os() == android.Windows,
		Include:  "$(BUILD_SYSTEM)/soong_java_prebuilt.mk",
		Extra: []android.AndroidMkExtraFunc{
			func(w io.Writer, outputFile android.Path) {
				fmt.Fprintln(w, "LOCAL_UNINSTALLABLE_MODULE := true")
				fmt.Fprintln(w, "LOCAL_SOONG_HEADER_JAR :=", j.combinedHeaderJar.String())
				fmt.Fprintln(w, "LOCAL_SOONG_CLASSES_JAR :=", j.combinedImplementationJar.String())
			},
		},
	}
}

// implement the following interface for IDE completion.
var _ android.IDEInfo = (*GenruleCombiner)(nil)

func (j *GenruleCombiner) IDEInfo(ctx android.BaseModuleContext, ideInfo *android.IdeInfo) {
	ideInfo.Deps = append(ideInfo.Deps, j.genruleCombinerProperties.Static_libs.GetOrDefault(ctx, nil)...)
	ideInfo.Libs = append(ideInfo.Libs, j.genruleCombinerProperties.Static_libs.GetOrDefault(ctx, nil)...)
	ideInfo.Deps = append(ideInfo.Deps, j.genruleCombinerProperties.Headers.GetOrDefault(ctx, nil)...)
	ideInfo.Libs = append(ideInfo.Libs, j.genruleCombinerProperties.Headers.GetOrDefault(ctx, nil)...)
}
