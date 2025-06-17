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

package cc

import (
	"android/soong/android"
	"strings"
)

func init() {
	android.InitRegistrationContext.RegisterSingletonType("cc_misc_disted_files", ccMiscDistedFilesSingletonFactory)
}

func ccMiscDistedFilesSingletonFactory() android.Singleton {
	return &ccMiscDistedFilesSingleton{}
}

type ccMiscDistedFilesSingleton struct {
	warningsAllowed []string
	usingWnoErrors  []string
}

func (s *ccMiscDistedFilesSingleton) GenerateBuildActions(ctx android.SingletonContext) {
	var warningsAllowed []string
	var usingWnoErrors []string
	var missingProfiles []string
	ctx.VisitAllModules(func(module android.Module) {
		if v, ok := android.OtherModuleProvider(ctx, module, CcMakeVarsInfoProvider); ok {
			warningsAllowed = android.AppendIfNotZero(warningsAllowed, v.WarningsAllowed)
			usingWnoErrors = android.AppendIfNotZero(usingWnoErrors, v.UsingWnoError)
			missingProfiles = android.AppendIfNotZero(missingProfiles, v.MissingProfile)
		}
	})

	warningsAllowed = android.SortedUniqueStrings(warningsAllowed)
	usingWnoErrors = android.SortedUniqueStrings(usingWnoErrors)
	missingProfiles = android.SortedUniqueStrings(missingProfiles)

	s.warningsAllowed = warningsAllowed
	s.usingWnoErrors = usingWnoErrors

	var sb strings.Builder
	sb.WriteString("# Modules using -Wno-error\n")
	for _, nwe := range usingWnoErrors {
		sb.WriteString(nwe)
		sb.WriteString("\n")
	}
	sb.WriteString("# Modules that allow warnings\n")
	for _, wa := range warningsAllowed {
		sb.WriteString(wa)
		sb.WriteString("\n")
	}
	wallWerrFile := android.PathForOutput(ctx, "wall_werror.txt")
	android.WriteFileRuleVerbatim(ctx, wallWerrFile, sb.String())

	// Only dist this file in soong-only builds. In soong+make builds, it contains information
	// from make modules, so we'll still rely on make to build and dist it.
	if !ctx.Config().KatiEnabled() {
		ctx.DistForGoal("droidcore-unbundled", wallWerrFile)
	}

	var sb2 strings.Builder
	sb2.WriteString("# Modules missing PGO profile files\n")
	for _, mp := range missingProfiles {
		sb2.WriteString(mp)
		sb2.WriteString("\n")
	}
	profileMissingFile := android.PathForOutput(ctx, "pgo_profile_file_missing.txt")
	android.WriteFileRuleVerbatim(ctx, profileMissingFile, sb2.String())

	ctx.DistForGoal("droidcore-unbundled", profileMissingFile)
}

func (s *ccMiscDistedFilesSingleton) MakeVars(ctx android.MakeVarsContext) {
	ctx.Strict("SOONG_MODULES_WARNINGS_ALLOWED", strings.Join(s.warningsAllowed, " "))
	ctx.Strict("SOONG_MODULES_USING_WNO_ERROR", strings.Join(s.usingWnoErrors, " "))
}
