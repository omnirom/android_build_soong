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

package java

// Rules for instrumenting classes using jacoco

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"

	"android/soong/android"
	"android/soong/java/config"
)

func init() {
	android.InitRegistrationContext.RegisterParallelSingletonType("device_tests_jacoco_zip", deviceTestsJacocoZipSingletonFactory)
}

var (
	jacoco = pctx.AndroidStaticRule("jacoco", blueprint.RuleParams{
		Command: `rm -rf $tmpDir && mkdir -p $tmpDir && ` +
			`${config.Zip2ZipCmd} -i $in -o $strippedJar $stripSpec && ` +
			`${config.JavaCmd} ${config.JavaVmFlags} -jar ${config.JacocoCLIJar} ` +
			`  instrument --quiet --dest $tmpDir $strippedJar && ` +
			`${config.MergeZipsCmd} --ignore-duplicates -j $out $tmpJar $in`,
		CommandDeps: []string{
			"${config.Zip2ZipCmd}",
			"${config.JavaCmd}",
			"${config.JacocoCLIJar}",
			"${config.MergeZipsCmd}",
		},
	},
		"strippedJar", "stripSpec", "tmpDir", "tmpJar")
)

func jacocoDepsMutator(ctx android.BottomUpMutatorContext) {
	type instrumentable interface {
		shouldInstrument(ctx android.BaseModuleContext) bool
		shouldInstrumentInApex(ctx android.BaseModuleContext) bool
		setInstrument(value bool)
	}

	j, ok := ctx.Module().(instrumentable)
	if !ctx.Module().Enabled(ctx) || !ok {
		return
	}

	if j.shouldInstrumentInApex(ctx) {
		j.setInstrument(true)
	}

	if j.shouldInstrument(ctx) && ctx.ModuleName() != "jacocoagent" {
		// We can use AddFarVariationDependencies here because, since this dep
		// is added as libs only (i.e. a compiletime CLASSPATH entry only),
		// the first variant of jacocoagent is sufficient to prevent
		// compile time errors.
		// At this stage in the build, AddVariationDependencies is not always
		// able to procure a variant of jacocoagent that matches the calling
		// module.
		ctx.AddFarVariationDependencies(ctx.Module().Target().Variations(), libTag, "jacocoagent")
	}
}

// Instruments a jar using the Jacoco command line interface.  Uses stripSpec to extract a subset
// of the classes in inputJar into strippedJar, instruments strippedJar into tmpJar, and then
// combines the classes in tmpJar with inputJar (preferring the instrumented classes in tmpJar)
// to produce instrumentedJar.
func jacocoInstrumentJar(ctx android.ModuleContext, instrumentedJar, strippedJar android.WritablePath,
	inputJar android.Path, stripSpec string) {

	// The basename of tmpJar has to be the same as the basename of strippedJar
	tmpJar := android.PathForModuleOut(ctx, "jacoco", "tmp", strippedJar.Base())

	ctx.Build(pctx, android.BuildParams{
		Rule:           jacoco,
		Description:    "jacoco",
		Output:         instrumentedJar,
		ImplicitOutput: strippedJar,
		Input:          inputJar,
		Args: map[string]string{
			"strippedJar": strippedJar.String(),
			"stripSpec":   stripSpec,
			"tmpDir":      filepath.Dir(tmpJar.String()),
			"tmpJar":      tmpJar.String(),
		},
	})
}

func (j *Module) jacocoModuleToZipCommand(ctx android.ModuleContext) string {
	includes, err := jacocoFiltersToSpecs(j.properties.Jacoco.Include_filter)
	if err != nil {
		ctx.PropertyErrorf("jacoco.include_filter", "%s", err.Error())
	}
	// Also include the default list of classes to exclude from instrumentation.
	excludes, err := jacocoFiltersToSpecs(append(j.properties.Jacoco.Exclude_filter, config.DefaultJacocoExcludeFilter...))
	if err != nil {
		ctx.PropertyErrorf("jacoco.exclude_filter", "%s", err.Error())
	}

	return jacocoFiltersToZipCommand(includes, excludes)
}

func jacocoFiltersToZipCommand(includes, excludes []string) string {
	specs := ""
	if len(excludes) > 0 {
		specs += android.JoinWithPrefix(excludes, "-x ") + " "
	}
	if len(includes) > 0 {
		specs += strings.Join(includes, " ")
	} else {
		specs += "'**/*.class'"
	}
	return specs
}

func jacocoFiltersToSpecs(filters []string) ([]string, error) {
	specs := make([]string, len(filters))
	var err error
	for i, f := range filters {
		specs[i], err = jacocoFilterToSpec(f)
		if err != nil {
			return nil, err
		}
	}
	return proptools.NinjaAndShellEscapeList(specs), nil
}

func jacocoFilterToSpec(filter string) (string, error) {
	recursiveWildcard := strings.HasSuffix(filter, "**")
	nonRecursiveWildcard := false
	if !recursiveWildcard {
		nonRecursiveWildcard = strings.HasSuffix(filter, "*")
		filter = strings.TrimSuffix(filter, "*")
	} else {
		filter = strings.TrimSuffix(filter, "**")
	}

	if recursiveWildcard && !(strings.HasSuffix(filter, ".") || filter == "") {
		return "", fmt.Errorf("only '**' or '.**' is supported as recursive wildcard in a filter")
	}

	if strings.ContainsRune(filter, '*') {
		return "", fmt.Errorf("'*' is only supported as the last character in a filter")
	}

	spec := strings.Replace(filter, ".", "/", -1)

	if recursiveWildcard {
		spec += "**/*.class"
	} else if nonRecursiveWildcard {
		spec += "*.class"
	} else {
		spec += ".class"
	}

	return spec, nil
}

type JacocoInfo struct {
	ReportClassesFile android.Path
	Class             string
	ModuleName        string
}

var ApexJacocoInfoProvider = blueprint.NewProvider[[]JacocoInfo]()

type BuildJacocoZipContext interface {
	android.BuilderContext
	android.OtherModuleProviderContext
}

func BuildJacocoZip(ctx BuildJacocoZipContext, modules []android.ModuleProxy, outputFile android.WritablePath) {
	jacocoZipBuilder := android.NewRuleBuilder(pctx, ctx)
	jacocoZipCmd := jacocoZipBuilder.Command().BuiltTool("soong_zip").
		FlagWithOutput("-o ", outputFile).
		Flag("-L 0")
	for _, m := range modules {
		if javaInfo, ok := android.OtherModuleProvider(ctx, m, JavaInfoProvider); ok && javaInfo.JacocoInfo.ReportClassesFile != nil {
			jacoco := javaInfo.JacocoInfo
			jacocoZipCmd.FlagWithArg("-e ", fmt.Sprintf("out/target/common/obj/%s/%s_intermediates/jacoco-report-classes.jar", jacoco.Class, jacoco.ModuleName)).
				FlagWithInput("-f ", jacoco.ReportClassesFile)
		} else if info, ok := android.OtherModuleProvider(ctx, m, ApexJacocoInfoProvider); ok {
			for _, jacoco := range info {
				jacocoZipCmd.FlagWithArg("-e ", fmt.Sprintf("out/target/common/obj/%s/%s_intermediates/jacoco-report-classes.jar", jacoco.Class, jacoco.ModuleName)).
					FlagWithInput("-f ", jacoco.ReportClassesFile)
			}
		}
	}

	jacocoZipBuilder.Build("jacoco_report_classes_zip_"+outputFile.String(), "Building jacoco report zip")
}

func BuildJacocoZipWithPotentialDeviceTests(ctx android.ModuleContext, modules []android.ModuleProxy, outputFile android.WritablePath) {
	if !ctx.Config().IsEnvTrue("JACOCO_PACKAGING_INCLUDE_DEVICE_TESTS") {
		BuildJacocoZip(ctx, modules, outputFile)
		return
	}

	jacocoZipWithoutDeviceTests := android.PathForModuleOut(ctx, "temp-jacoco-report-classes-all-without-device-tests.jar")
	BuildJacocoZip(ctx, modules, jacocoZipWithoutDeviceTests)
	ctx.Build(pctx, android.BuildParams{
		Rule:   android.MergeZips,
		Output: outputFile,
		Inputs: []android.Path{
			jacocoZipWithoutDeviceTests,
			DeviceTestsJacocoReportZip(ctx),
		},
	})
}

func deviceTestsJacocoZipSingletonFactory() android.Singleton {
	return &deviceTestsJacocoZipSingleton{}
}

type deviceTestsJacocoZipSingleton struct{}

// GenerateBuildActions implements android.Singleton.
func (d *deviceTestsJacocoZipSingleton) GenerateBuildActions(ctx android.SingletonContext) {
	var deviceTestModules []android.ModuleProxy
	ctx.VisitAllModuleProxies(func(m android.ModuleProxy) {
		if tsm, ok := android.OtherModuleProvider(ctx, m, android.TestSuiteInfoProvider); ok {
			if slices.Contains(tsm.TestSuites, "device-tests") {
				deviceTestModules = append(deviceTestModules, m)
			}
		}
	})

	jacocoZip := DeviceTestsJacocoReportZip(ctx)
	BuildJacocoZip(ctx, deviceTestModules, jacocoZip)
}

func DeviceTestsJacocoReportZip(ctx android.PathContext) android.WritablePath {
	return android.PathForOutput(ctx, "device_tests_jacoco_report_classes_all.jar")
}
