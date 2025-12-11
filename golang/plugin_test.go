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

package golang

import (
	"testing"

	"android/soong/android"
	"github.com/google/blueprint/bootstrap"
)

func TestPluginAllowList(t *testing.T) {
	bp := `
		bootstrap_go_package {
			name: "bad_plugin",
			pkgPath: "test/bad",
			pluginFor: ["soong_build"],
		}

		bootstrap_go_package {
			name: "soong-llvm",
			pkgPath: "test/llvm",
			pluginFor: ["soong_build"],
		}

		blueprint_go_binary {
			name: "soong_build",
		}
	`

	android.GroupFixturePreparers(
		android.PrepareForTestWithArchMutator,
		android.FixtureRegisterWithContext(func(ctx android.RegistrationContext) {
			RegisterGoModuleTypes(ctx)
			ctx.PreDepsMutators(func(ctx android.RegisterMutatorsContext) {
				ctx.BottomUpBlueprint("bootstrap_deps", bootstrap.BootstrapDeps).UsesReverseDependencies()
			})
			android.RegisterPluginSingletonBuildComponents(ctx)
		}),
	).ExtendWithErrorHandler(android.FixtureExpectsOneErrorPattern(`New plugins are not supported; however \["bad_plugin"\] were found.`)).
		RunTestWithBp(t, bp)
}
