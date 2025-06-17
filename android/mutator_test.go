// Copyright 2015 Google Inc. All rights reserved.
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

package android

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/blueprint"
)

type mutatorTestModule struct {
	ModuleBase
	props struct {
		Deps_missing_deps    []string
		Mutator_missing_deps []string
	}

	missingDeps []string
}

func mutatorTestModuleFactory() Module {
	module := &mutatorTestModule{}
	module.AddProperties(&module.props)
	InitAndroidModule(module)
	return module
}

func (m *mutatorTestModule) GenerateAndroidBuildActions(ctx ModuleContext) {
	ctx.Build(pctx, BuildParams{
		Rule:   Touch,
		Output: PathForModuleOut(ctx, "output"),
	})

	m.missingDeps = ctx.GetMissingDependencies()
}

func (m *mutatorTestModule) DepsMutator(ctx BottomUpMutatorContext) {
	ctx.AddDependency(ctx.Module(), nil, m.props.Deps_missing_deps...)
}

func addMissingDependenciesMutator(ctx BottomUpMutatorContext) {
	ctx.AddMissingDependencies(ctx.Module().(*mutatorTestModule).props.Mutator_missing_deps)
}

func TestMutatorAddMissingDependencies(t *testing.T) {
	bp := `
		test {
			name: "foo",
			deps_missing_deps: ["regular_missing_dep"],
			mutator_missing_deps: ["added_missing_dep"],
		}
	`

	result := GroupFixturePreparers(
		PrepareForTestWithAllowMissingDependencies,
		FixtureRegisterWithContext(func(ctx RegistrationContext) {
			ctx.RegisterModuleType("test", mutatorTestModuleFactory)
			ctx.PreDepsMutators(func(ctx RegisterMutatorsContext) {
				ctx.BottomUp("add_missing_dependencies", addMissingDependenciesMutator)
			})
		}),
		FixtureWithRootAndroidBp(bp),
	).RunTest(t)

	foo := result.ModuleForTests(t, "foo", "").Module().(*mutatorTestModule)

	AssertDeepEquals(t, "foo missing deps", []string{"added_missing_dep", "regular_missing_dep"}, foo.missingDeps)
}

func TestFinalDepsPhase(t *testing.T) {
	bp := `
		test {
			name: "common_dep_1",
		}
		test {
			name: "common_dep_2",
		}
		test {
			name: "foo",
		}
	`

	finalGot := sync.Map{}

	GroupFixturePreparers(
		FixtureRegisterWithContext(func(ctx RegistrationContext) {
			dep1Tag := struct {
				blueprint.BaseDependencyTag
			}{}
			dep2Tag := struct {
				blueprint.BaseDependencyTag
			}{}

			ctx.PostDepsMutators(func(ctx RegisterMutatorsContext) {
				ctx.BottomUp("far_deps_1", func(ctx BottomUpMutatorContext) {
					if !strings.HasPrefix(ctx.ModuleName(), "common_dep") {
						ctx.AddFarVariationDependencies([]blueprint.Variation{}, dep1Tag, "common_dep_1")
					}
				})
				ctx.Transition("variant", &testTransitionMutator{
					split: func(ctx BaseModuleContext) []string {
						return []string{"a", "b"}
					},
				})
			})

			ctx.FinalDepsMutators(func(ctx RegisterMutatorsContext) {
				ctx.BottomUp("far_deps_2", func(ctx BottomUpMutatorContext) {
					if !strings.HasPrefix(ctx.ModuleName(), "common_dep") {
						ctx.AddFarVariationDependencies([]blueprint.Variation{}, dep2Tag, "common_dep_2")
					}
				})
				ctx.BottomUp("final", func(ctx BottomUpMutatorContext) {
					counter, _ := finalGot.LoadOrStore(ctx.Module().String(), &atomic.Int64{})
					counter.(*atomic.Int64).Add(1)
					ctx.VisitDirectDeps(func(mod Module) {
						counter, _ := finalGot.LoadOrStore(fmt.Sprintf("%s -> %s", ctx.Module().String(), mod), &atomic.Int64{})
						counter.(*atomic.Int64).Add(1)
					})
				})
			})

			ctx.RegisterModuleType("test", mutatorTestModuleFactory)
		}),
		FixtureWithRootAndroidBp(bp),
	).RunTest(t)

	finalWant := map[string]int{
		"common_dep_1{variant:a}":                   1,
		"common_dep_1{variant:b}":                   1,
		"common_dep_2{variant:a}":                   1,
		"common_dep_2{variant:b}":                   1,
		"foo{variant:a}":                            1,
		"foo{variant:a} -> common_dep_1{variant:a}": 1,
		"foo{variant:a} -> common_dep_2{variant:a}": 1,
		"foo{variant:b}":                            1,
		"foo{variant:b} -> common_dep_1{variant:b}": 1,
		"foo{variant:b} -> common_dep_2{variant:a}": 1,
	}

	finalGotMap := make(map[string]int)
	finalGot.Range(func(k, v any) bool {
		finalGotMap[k.(string)] = int(v.(*atomic.Int64).Load())
		return true
	})

	AssertDeepEquals(t, "final", finalWant, finalGotMap)
}
