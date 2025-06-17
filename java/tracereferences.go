// Copyright 2025 Google Inc. All rights reserved.
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
	"android/soong/android"

	"github.com/google/blueprint"
)

var traceReferences = pctx.AndroidStaticRule("traceReferences",
	blueprint.RuleParams{
		Command: `${config.TraceReferencesCmd} ` +
			// Note that we suppress missing def errors, as we're only interested
			// in the direct deps between the sources and target.
			`--map-diagnostics:MissingDefinitionsDiagnostic error none ` +
			`--keep-rules ` +
			`--output ${out} ` +
			`--target ${in} ` +
			// `--source` and `--lib` are already prepended to each
			// jar reference in the sources and libs joined string args.
			`${sources} ` +
			`${libs}`,
		CommandDeps: []string{"${config.TraceReferencesCmd}"},
	}, "sources", "libs")

// Generates keep rules in output corresponding to any references from sources
// (a list of jars) onto target (the referenced jar) that are not included in
// libs (a list of external jars).
func TraceReferences(ctx android.ModuleContext, sources android.Paths, target android.Path, libs android.Paths,
	output android.WritablePath) {
	ctx.Build(pctx, android.BuildParams{
		Rule:      traceReferences,
		Input:     target,
		Output:    output,
		Implicits: append(sources, libs...),
		Args: map[string]string{
			"sources": android.JoinWithPrefix(sources.Strings(), "--source "),
			"libs":    android.JoinWithPrefix(libs.Strings(), "--lib "),
		},
	})
}
