// Copyright 2018 Google Inc. All rights reserved.
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
	"reflect"
	"testing"

	"android/soong/android"
)

func TestJarGenruleCombinerSingle(t *testing.T) {
	t.Parallel()
	t.Helper()
	ctx := prepareForJavaTest.RunTestWithBp(t, `
		java_library {
			name: "foo",
			srcs: ["a.java"],
		}

		java_genrule {
			name: "gen",
			tool_files: ["b.java"],
			cmd: "$(location b.java) $(in) $(out)",
			out: ["gen.jar"],
			srcs: [":foo"],
		}

		java_genrule_combiner {
			name: "jarcomb",
			static_libs: ["gen"],
			headers: ["foo"],
		}

		java_library {
			name: "bar",
			static_libs: ["jarcomb"],
			srcs: ["c.java"],
		}

		java_library {
			name: "baz",
			libs: ["jarcomb"],
			srcs: ["c.java"],
		}
	`).TestContext

	fooMod := ctx.ModuleForTests(t, "foo", "android_common")
	fooCombined := fooMod.Output("turbine-combined/foo.jar")
	fooOutputFiles, _ := android.OtherModuleProvider(ctx.OtherModuleProviderAdaptor(), fooMod.Module(), android.OutputFilesProvider)
	fooHeaderJars := fooOutputFiles.TaggedOutputFiles[".hjar"]

	genMod := ctx.ModuleForTests(t, "gen", "android_common")
	gen := genMod.Output("gen.jar")

	jarcombMod := ctx.ModuleForTests(t, "jarcomb", "android_common")
	jarcombInfo, _ := android.OtherModuleProvider(ctx.OtherModuleProviderAdaptor(), jarcombMod.Module(), JavaInfoProvider)
	jarcombOutputFiles, _ := android.OtherModuleProvider(ctx.OtherModuleProviderAdaptor(), jarcombMod.Module(), android.OutputFilesProvider)

	// Confirm that jarcomb simply forwards the jarcomb implementation and the foo headers.
	if len(jarcombOutputFiles.DefaultOutputFiles) != 1 ||
		android.PathRelativeToTop(jarcombOutputFiles.DefaultOutputFiles[0]) != android.PathRelativeToTop(gen.Output) {
		t.Errorf("jarcomb Implementation %v is not [%q]",
			android.PathsRelativeToTop(jarcombOutputFiles.DefaultOutputFiles), android.PathRelativeToTop(gen.Output))
	}
	jarcombHeaderJars := jarcombOutputFiles.TaggedOutputFiles[".hjar"]
	if !reflect.DeepEqual(jarcombHeaderJars, fooHeaderJars) {
		t.Errorf("jarcomb Header jar %v is not %q",
			jarcombHeaderJars, fooHeaderJars)
	}

	// Confirm that JavaInfoProvider agrees.
	if len(jarcombInfo.ImplementationJars) != 1 ||
		android.PathRelativeToTop(jarcombInfo.ImplementationJars[0]) != android.PathRelativeToTop(gen.Output) {
		t.Errorf("jarcomb ImplementationJars %v is not [%q]",
			android.PathsRelativeToTop(jarcombInfo.ImplementationJars), android.PathRelativeToTop(gen.Output))
	}
	if len(jarcombInfo.HeaderJars) != 1 ||
		android.PathRelativeToTop(jarcombInfo.HeaderJars[0]) != android.PathRelativeToTop(fooCombined.Output) {
		t.Errorf("jarcomb HeaderJars %v is not [%q]",
			android.PathsRelativeToTop(jarcombInfo.HeaderJars), android.PathRelativeToTop(fooCombined.Output))
	}

	barMod := ctx.ModuleForTests(t, "bar", "android_common")
	bar := barMod.Output("javac/bar.jar")
	barCombined := barMod.Output("combined/bar.jar")

	// Confirm that bar uses the Implementation from gen and headerJars from foo.
	if len(barCombined.Inputs) != 2 ||
		barCombined.Inputs[0].String() != bar.Output.String() ||
		barCombined.Inputs[1].String() != gen.Output.String() {
		t.Errorf("bar combined jar inputs %v is not [%q, %q]",
			barCombined.Inputs.Strings(), bar.Output.String(), gen.Output.String())
	}

	bazMod := ctx.ModuleForTests(t, "baz", "android_common")
	baz := bazMod.Output("javac/baz.jar")

	string_in_list := func(s string, l []string) bool {
		for _, v := range l {
			if s == v {
				return true
			}
		}
		return false
	}

	// Confirm that baz uses the headerJars from foo.
	bazImplicitsRel := android.PathsRelativeToTop(baz.Implicits)
	for _, v := range android.PathsRelativeToTop(fooHeaderJars) {
		if !string_in_list(v, bazImplicitsRel) {
			t.Errorf("baz Implicits %v does not contain %q", bazImplicitsRel, v)
		}
	}
}

func TestJarGenruleCombinerMulti(t *testing.T) {
	t.Parallel()
	t.Helper()
	ctx := prepareForJavaTest.RunTestWithBp(t, `
		java_library {
			name: "foo1",
			srcs: ["foo1_a.java"],
		}

		java_library {
			name: "foo2",
			srcs: ["foo2_a.java"],
		}

		java_genrule {
			name: "gen1",
			tool_files: ["b.java"],
			cmd: "$(location b.java) $(in) $(out)",
			out: ["gen1.jar"],
			srcs: [":foo1"],
		}

		java_genrule {
			name: "gen2",
			tool_files: ["b.java"],
			cmd: "$(location b.java) $(in) $(out)",
			out: ["gen2.jar"],
			srcs: [":foo2"],
		}

		// Combine multiple java_genrule modules.
		java_genrule_combiner {
			name: "jarcomb",
			static_libs: ["gen1", "gen2"],
			headers: ["foo1", "foo2"],
		}

		java_library {
			name: "bar",
			static_libs: ["jarcomb"],
			srcs: ["c.java"],
		}

		java_library {
			name: "baz",
			libs: ["jarcomb"],
			srcs: ["c.java"],
		}
	`).TestContext

	gen1Mod := ctx.ModuleForTests(t, "gen1", "android_common")
	gen1 := gen1Mod.Output("gen1.jar")
	gen2Mod := ctx.ModuleForTests(t, "gen2", "android_common")
	gen2 := gen2Mod.Output("gen2.jar")

	jarcombMod := ctx.ModuleForTests(t, "jarcomb", "android_common")
	jarcomb := jarcombMod.Output("combined/jarcomb.jar")
	jarcombTurbine := jarcombMod.Output("turbine-combined/jarcomb.jar")
	_ = jarcombTurbine
	jarcombInfo, _ := android.OtherModuleProvider(ctx.OtherModuleProviderAdaptor(), jarcombMod.Module(), JavaInfoProvider)
	_ = jarcombInfo
	jarcombOutputFiles, _ := android.OtherModuleProvider(ctx.OtherModuleProviderAdaptor(), jarcombMod.Module(), android.OutputFilesProvider)
	jarcombHeaderJars := jarcombOutputFiles.TaggedOutputFiles[".hjar"]

	if len(jarcomb.Inputs) != 2 ||
		jarcomb.Inputs[0].String() != gen1.Output.String() ||
		jarcomb.Inputs[1].String() != gen2.Output.String() {
		t.Errorf("jarcomb inputs %v are not [%q, %q]",
			jarcomb.Inputs.Strings(), gen1.Output.String(), gen2.Output.String())
	}

	if len(jarcombHeaderJars) != 1 ||
		android.PathRelativeToTop(jarcombHeaderJars[0]) != android.PathRelativeToTop(jarcombTurbine.Output) {
		t.Errorf("jarcomb Header jars %v is not [%q]",
			android.PathsRelativeToTop(jarcombHeaderJars), android.PathRelativeToTop(jarcombTurbine.Output))
	}

	barMod := ctx.ModuleForTests(t, "bar", "android_common")
	bar := barMod.Output("javac/bar.jar")
	barCombined := barMod.Output("combined/bar.jar")

	// Confirm that bar uses the Implementation and Headers from jarcomb.
	if len(barCombined.Inputs) != 2 ||
		barCombined.Inputs[0].String() != bar.Output.String() ||
		barCombined.Inputs[1].String() != jarcomb.Output.String() {
		t.Errorf("bar combined jar inputs %v is not [%q, %q]",
			barCombined.Inputs.Strings(), bar.Output.String(), jarcomb.Output.String())
	}

	bazMod := ctx.ModuleForTests(t, "baz", "android_common")
	baz := bazMod.Output("javac/baz.jar")

	string_in_list := func(s string, l []string) bool {
		for _, v := range l {
			if s == v {
				return true
			}
		}
		return false
	}

	// Confirm that baz uses the headerJars from foo.
	bazImplicitsRel := android.PathsRelativeToTop(baz.Implicits)
	for _, v := range android.PathsRelativeToTop(jarcombHeaderJars) {
		if !string_in_list(v, bazImplicitsRel) {
			t.Errorf("baz Implicits %v does not contain %q", bazImplicitsRel, v)
		}
	}
}
