// Copyright 2024 Google Inc. All rights reserved.
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

import "testing"

type testTransitionMutator struct {
	split              func(ctx BaseModuleContext) []string
	outgoingTransition func(ctx OutgoingTransitionContext, sourceVariation string) string
	incomingTransition func(ctx IncomingTransitionContext, incomingVariation string) string
	mutate             func(ctx BottomUpMutatorContext, variation string)
}

func (t *testTransitionMutator) Split(ctx BaseModuleContext) []string {
	if t.split != nil {
		return t.split(ctx)
	}
	return []string{""}
}

func (t *testTransitionMutator) OutgoingTransition(ctx OutgoingTransitionContext, sourceVariation string) string {
	if t.outgoingTransition != nil {
		return t.outgoingTransition(ctx, sourceVariation)
	}
	return sourceVariation
}

func (t *testTransitionMutator) IncomingTransition(ctx IncomingTransitionContext, incomingVariation string) string {
	if t.incomingTransition != nil {
		return t.incomingTransition(ctx, incomingVariation)
	}
	return incomingVariation
}

func (t *testTransitionMutator) Mutate(ctx BottomUpMutatorContext, variation string) {
	if t.mutate != nil {
		t.mutate(ctx, variation)
	}
}

func TestModuleString(t *testing.T) {
	bp := `
		test {
			name: "foo",
		}
	`

	var moduleStrings []string

	GroupFixturePreparers(
		FixtureRegisterWithContext(func(ctx RegistrationContext) {

			ctx.PreArchMutators(func(ctx RegisterMutatorsContext) {
				ctx.Transition("pre_arch", &testTransitionMutator{
					split: func(ctx BaseModuleContext) []string {
						moduleStrings = append(moduleStrings, ctx.Module().String())
						return []string{"a", "b"}
					},
				})
			})

			ctx.PreDepsMutators(func(ctx RegisterMutatorsContext) {
				ctx.Transition("pre_deps", &testTransitionMutator{
					split: func(ctx BaseModuleContext) []string {
						moduleStrings = append(moduleStrings, ctx.Module().String())
						return []string{"c", "d"}
					},
				})
			})

			ctx.PostDepsMutators(func(ctx RegisterMutatorsContext) {
				ctx.Transition("post_deps", &testTransitionMutator{
					split: func(ctx BaseModuleContext) []string {
						moduleStrings = append(moduleStrings, ctx.Module().String())
						return []string{"e", "f"}
					},
					outgoingTransition: func(ctx OutgoingTransitionContext, sourceVariation string) string {
						return ""
					},
				})
				ctx.BottomUp("rename_bottom_up", func(ctx BottomUpMutatorContext) {
					moduleStrings = append(moduleStrings, ctx.Module().String())
					ctx.Rename(ctx.Module().base().Name() + "_renamed1")
				}).UsesRename()
				ctx.BottomUp("final", func(ctx BottomUpMutatorContext) {
					moduleStrings = append(moduleStrings, ctx.Module().String())
				})
			})

			ctx.RegisterModuleType("test", mutatorTestModuleFactory)
		}),
		FixtureWithRootAndroidBp(bp),
	).RunTest(t)

	want := []string{
		// Initial name.
		"foo{}",

		// After pre_arch (reversed because rename_top_down is TopDown so it visits in reverse order).
		"foo{pre_arch:b}",
		"foo{pre_arch:a}",

		// After pre_deps (reversed because post_deps TransitionMutator.Split is TopDown).
		"foo{pre_arch:b,pre_deps:d}",
		"foo{pre_arch:b,pre_deps:c}",
		"foo{pre_arch:a,pre_deps:d}",
		"foo{pre_arch:a,pre_deps:c}",

		// After post_deps.
		"foo{pre_arch:a,pre_deps:c,post_deps:e}",
		"foo{pre_arch:a,pre_deps:c,post_deps:f}",
		"foo{pre_arch:a,pre_deps:d,post_deps:e}",
		"foo{pre_arch:a,pre_deps:d,post_deps:f}",
		"foo{pre_arch:b,pre_deps:c,post_deps:e}",
		"foo{pre_arch:b,pre_deps:c,post_deps:f}",
		"foo{pre_arch:b,pre_deps:d,post_deps:e}",
		"foo{pre_arch:b,pre_deps:d,post_deps:f}",

		// After rename_bottom_up.
		"foo_renamed1{pre_arch:a,pre_deps:c,post_deps:e}",
		"foo_renamed1{pre_arch:a,pre_deps:c,post_deps:f}",
		"foo_renamed1{pre_arch:a,pre_deps:d,post_deps:e}",
		"foo_renamed1{pre_arch:a,pre_deps:d,post_deps:f}",
		"foo_renamed1{pre_arch:b,pre_deps:c,post_deps:e}",
		"foo_renamed1{pre_arch:b,pre_deps:c,post_deps:f}",
		"foo_renamed1{pre_arch:b,pre_deps:d,post_deps:e}",
		"foo_renamed1{pre_arch:b,pre_deps:d,post_deps:f}",
	}

	AssertDeepEquals(t, "module String() values", want, moduleStrings)
}

func TestTransitionMutatorInFinalDeps(t *testing.T) {
	GroupFixturePreparers(
		FixtureRegisterWithContext(func(ctx RegistrationContext) {
			ctx.FinalDepsMutators(func(ctx RegisterMutatorsContext) {
				ctx.Transition("vars", &testTransitionMutator{
					split: func(ctx BaseModuleContext) []string {
						return []string{"a", "b"}
					},
				})
			})

			ctx.RegisterModuleType("test", mutatorTestModuleFactory)
		}),
		FixtureWithRootAndroidBp(`test {name: "foo"}`),
	).
		ExtendWithErrorHandler(FixtureExpectsOneErrorPattern("not allowed in FinalDepsMutators")).
		RunTest(t)
}
