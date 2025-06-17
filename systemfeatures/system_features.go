// Copyright 2024 Google Inc. All rights reserved.
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

package systemfeatures

import (
	"fmt"
	"sort"
	"strings"

	"android/soong/android"
	"android/soong/genrule"

	"github.com/google/blueprint/proptools"
)

var (
	pctx = android.NewPackageContext("android/soong/systemfeatures")
)

func init() {
	registerSystemFeaturesComponents(android.InitRegistrationContext)
}

func registerSystemFeaturesComponents(ctx android.RegistrationContext) {
	ctx.RegisterModuleType("java_system_features_srcs", JavaSystemFeaturesSrcsFactory)
}

type javaSystemFeaturesSrcs struct {
	android.ModuleBase
	properties struct {
		// The fully qualified class name for the generated code, e.g., com.android.Foo
		Full_class_name string
		// Whether to generate only a simple metadata class with details about the full API surface.
		// This is useful for tools that rely on the mapping from feature names to their generated
		// method names, but don't want the fully generated API class (e.g., for linting).

		Metadata_only *bool
	}
	outputFiles android.WritablePaths
}

var _ genrule.SourceFileGenerator = (*javaSystemFeaturesSrcs)(nil)
var _ android.SourceFileProducer = (*javaSystemFeaturesSrcs)(nil)

func (m *javaSystemFeaturesSrcs) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	// Create a file name appropriate for the given fully qualified (w/ package) class name.
	classNameParts := strings.Split(m.properties.Full_class_name, ".")
	outputDir := android.PathForModuleGen(ctx)
	outputFileName := classNameParts[len(classNameParts)-1] + ".java"
	outputFile := android.PathForModuleGen(ctx, outputFileName).OutputPath

	// Collect all RELEASE_SYSTEM_FEATURE_$K:$V build flags into a list of "$K:$V" pairs.
	var features []string
	for k, v := range ctx.Config().ProductVariables().BuildFlags {
		if strings.HasPrefix(k, "RELEASE_SYSTEM_FEATURE_") {
			shortFeatureName := strings.TrimPrefix(k, "RELEASE_SYSTEM_FEATURE_")
			features = append(features, fmt.Sprintf("%s:%s", shortFeatureName, v))
		}
	}
	// Ensure sorted outputs for consistency of flag ordering in ninja outputs.
	sort.Strings(features)

	rule := android.NewRuleBuilder(pctx, ctx)
	rule.Command().Text("rm -rf").Text(outputDir.String())
	rule.Command().Text("mkdir -p").Text(outputDir.String())
	rule.Command().
		BuiltTool("systemfeatures-gen-tool").
		Flag(m.properties.Full_class_name).
		FlagForEachArg("--feature=", features).
		FlagWithArg("--readonly=", fmt.Sprint(ctx.Config().ReleaseUseSystemFeatureBuildFlags())).
		FlagWithArg("--metadata-only=", fmt.Sprint(proptools.Bool(m.properties.Metadata_only))).
		FlagWithOutput(" > ", outputFile)
	rule.Build(ctx.ModuleName(), "Generating systemfeatures srcs filegroup")

	m.outputFiles = append(m.outputFiles, outputFile)
}

func (m *javaSystemFeaturesSrcs) Srcs() android.Paths {
	return m.outputFiles.Paths()
}

func (m *javaSystemFeaturesSrcs) GeneratedSourceFiles() android.Paths {
	return m.outputFiles.Paths()
}

func (m *javaSystemFeaturesSrcs) GeneratedDeps() android.Paths {
	return m.outputFiles.Paths()
}

func (m *javaSystemFeaturesSrcs) GeneratedHeaderDirs() android.Paths {
	return nil
}

func JavaSystemFeaturesSrcsFactory() android.Module {
	module := &javaSystemFeaturesSrcs{}
	module.AddProperties(&module.properties)
	module.properties.Metadata_only = proptools.BoolPtr(false)
	android.InitAndroidModule(module)
	return module
}
