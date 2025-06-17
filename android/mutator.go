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
	"github.com/google/blueprint"
	"github.com/google/blueprint/pool"
)

// Phases:
//   run Pre-arch mutators
//   run archMutator
//   run Pre-deps mutators
//   run depsMutator
//   run PostDeps mutators
//   run FinalDeps mutators (TransitionMutators disallowed in this phase)
//   continue on to GenerateAndroidBuildActions

// collateGloballyRegisteredMutators constructs the list of mutators that have been registered
// with the InitRegistrationContext and will be used at runtime.
func collateGloballyRegisteredMutators() sortableComponents {
	return collateRegisteredMutators(preArch, preDeps, postDeps, postApex, finalDeps)
}

// collateRegisteredMutators constructs a single list of mutators from the separate lists.
func collateRegisteredMutators(preArch, preDeps, postDeps, postApex, finalDeps []RegisterMutatorFunc) sortableComponents {
	mctx := &registerMutatorsContext{}

	register := func(funcs []RegisterMutatorFunc) {
		for _, f := range funcs {
			f(mctx)
		}
	}

	register(preArch)

	register(preDeps)

	register([]RegisterMutatorFunc{registerDepsMutator})

	register(postDeps)

	register(postApex)

	mctx.finalPhase = true
	register(finalDeps)

	return mctx.mutators
}

type registerMutatorsContext struct {
	mutators   sortableComponents
	finalPhase bool
}

type RegisterMutatorsContext interface {
	BottomUp(name string, m BottomUpMutator) MutatorHandle
	BottomUpBlueprint(name string, m blueprint.BottomUpMutator) MutatorHandle
	Transition(name string, m VariationTransitionMutator) TransitionMutatorHandle
	InfoBasedTransition(name string, m androidTransitionMutator) TransitionMutatorHandle
}

type RegisterMutatorFunc func(RegisterMutatorsContext)

var preArch = []RegisterMutatorFunc{
	RegisterNamespaceMutator,

	// Check the visibility rules are valid.
	//
	// This must run after the package renamer mutators so that any issues found during
	// validation of the package's default_visibility property are reported using the
	// correct package name and not the synthetic name.
	//
	// This must also be run before defaults mutators as the rules for validation are
	// different before checking the rules than they are afterwards. e.g.
	//    visibility: ["//visibility:private", "//visibility:public"]
	// would be invalid if specified in a module definition but is valid if it results
	// from something like this:
	//
	//    defaults {
	//        name: "defaults",
	//        // Be inaccessible outside a package by default.
	//        visibility: ["//visibility:private"]
	//    }
	//
	//    defaultable_module {
	//        name: "defaultable_module",
	//        defaults: ["defaults"],
	//        // Override the default.
	//        visibility: ["//visibility:public"]
	//    }
	//
	RegisterVisibilityRuleChecker,

	// Record the default_applicable_licenses for each package.
	//
	// This must run before the defaults so that defaults modules can pick up the package default.
	RegisterLicensesPackageMapper,

	// Apply properties from defaults modules to the referencing modules.
	//
	// Any mutators that are added before this will not see any modules created by
	// a DefaultableHook.
	RegisterDefaultsPreArchMutators,

	// Add dependencies on any components so that any component references can be
	// resolved within the deps mutator.
	//
	// Must be run after defaults so it can be used to create dependencies on the
	// component modules that are creating in a DefaultableHook.
	//
	// Must be run before RegisterPrebuiltsPreArchMutators, i.e. before prebuilts are
	// renamed. That is so that if a module creates components using a prebuilt module
	// type that any dependencies (which must use prebuilt_ prefixes) are resolved to
	// the prebuilt module and not the source module.
	RegisterComponentsMutator,

	// Create an association between prebuilt modules and their corresponding source
	// modules (if any).
	//
	// Must be run after defaults mutators to ensure that any modules created by
	// a DefaultableHook can be either a prebuilt or a source module with a matching
	// prebuilt.
	RegisterPrebuiltsPreArchMutators,

	// Gather the licenses properties for all modules for use during expansion and enforcement.
	//
	// This must come after the defaults mutators to ensure that any licenses supplied
	// in a defaults module has been successfully applied before the rules are gathered.
	RegisterLicensesPropertyGatherer,

	// Gather the visibility rules for all modules for us during visibility enforcement.
	//
	// This must come after the defaults mutators to ensure that any visibility supplied
	// in a defaults module has been successfully applied before the rules are gathered.
	RegisterVisibilityRuleGatherer,
}

func registerArchMutator(ctx RegisterMutatorsContext) {
	ctx.Transition("os", &osTransitionMutator{})
	ctx.BottomUp("image_begin", imageMutatorBeginMutator)
	ctx.Transition("image", &imageTransitionMutator{})
	ctx.Transition("arch", &archTransitionMutator{})
}

var preDeps = []RegisterMutatorFunc{
	registerArchMutator,
	RegisterPrebuiltsPreDepsMutators,
}

var postDeps = []RegisterMutatorFunc{
	registerPathDepsMutator,
	RegisterPrebuiltsPostDepsMutators,
	RegisterVisibilityRuleEnforcer,
	RegisterLicensesDependencyChecker,
	registerNeverallowMutator,
	RegisterOverridePostDepsMutators,
}

var postApex = []RegisterMutatorFunc{}

var finalDeps = []RegisterMutatorFunc{}

func PreArchMutators(f RegisterMutatorFunc) {
	preArch = append(preArch, f)
}

func PreDepsMutators(f RegisterMutatorFunc) {
	preDeps = append(preDeps, f)
}

func PostDepsMutators(f RegisterMutatorFunc) {
	postDeps = append(postDeps, f)
}

func PostApexMutators(f RegisterMutatorFunc) {
	postApex = append(postApex, f)
}

func FinalDepsMutators(f RegisterMutatorFunc) {
	finalDeps = append(finalDeps, f)
}

type BottomUpMutator func(BottomUpMutatorContext)

type BottomUpMutatorContext interface {
	BaseModuleContext

	// AddDependency adds a dependency to the given module.  It returns a slice of modules for each
	// dependency (some entries may be nil).
	//
	// This method will pause until the new dependencies have had the current mutator called on them.
	AddDependency(module blueprint.Module, tag blueprint.DependencyTag, name ...string) []Module

	// AddReverseDependency adds a dependency from the destination to the given module.
	// Does not affect the ordering of the current mutator pass, but will be ordered
	// correctly for all future mutator passes.  All reverse dependencies for a destination module are
	// collected until the end of the mutator pass, sorted by name, and then appended to the destination
	// module's dependency list.  May only  be called by mutators that were marked with
	// UsesReverseDependencies during registration.
	AddReverseDependency(module blueprint.Module, tag blueprint.DependencyTag, name string)

	// AddVariationDependencies adds deps as dependencies of the current module, but uses the variations
	// argument to select which variant of the dependency to use.  It returns a slice of modules for
	// each dependency (some entries may be nil).  A variant of the dependency must exist that matches
	// all the non-local variations of the current module, plus the variations argument.
	//
	// This method will pause until the new dependencies have had the current mutator called on them.
	AddVariationDependencies(variations []blueprint.Variation, tag blueprint.DependencyTag, names ...string) []Module

	// AddReverseVariationDependency adds a dependency from the named module to the current
	// module. The given variations will be added to the current module's varations, and then the
	// result will be used to find the correct variation of the depending module, which must exist.
	//
	// Does not affect the ordering of the current mutator pass, but will be ordered
	// correctly for all future mutator passes.  All reverse dependencies for a destination module are
	// collected until the end of the mutator pass, sorted by name, and then appended to the destination
	// module's dependency list.  May only  be called by mutators that were marked with
	// UsesReverseDependencies during registration.
	AddReverseVariationDependency([]blueprint.Variation, blueprint.DependencyTag, string)

	// AddFarVariationDependencies adds deps as dependencies of the current module, but uses the
	// variations argument to select which variant of the dependency to use.  It returns a slice of
	// modules for each dependency (some entries may be nil).  A variant of the dependency must
	// exist that matches the variations argument, but may also have other variations.
	// For any unspecified variation the first variant will be used.
	//
	// Unlike AddVariationDependencies, the variations of the current module are ignored - the
	// dependency only needs to match the supplied variations.
	//
	// This method will pause until the new dependencies have had the current mutator called on them.
	AddFarVariationDependencies([]blueprint.Variation, blueprint.DependencyTag, ...string) []Module

	// ReplaceDependencies finds all the variants of the module with the specified name, then
	// replaces all dependencies onto those variants with the current variant of this module.
	// Replacements don't take effect until after the mutator pass is finished.  May only
	// be called by mutators that were marked with UsesReplaceDependencies during registration.
	ReplaceDependencies(string)

	// ReplaceDependenciesIf finds all the variants of the module with the specified name, then
	// replaces all dependencies onto those variants with the current variant of this module
	// as long as the supplied predicate returns true.
	// Replacements don't take effect until after the mutator pass is finished.  May only
	// be called by mutators that were marked with UsesReplaceDependencies during registration.
	ReplaceDependenciesIf(string, blueprint.ReplaceDependencyPredicate)

	// Rename all variants of a module.  The new name is not visible to calls to ModuleName,
	// AddDependency or OtherModuleName until after this mutator pass is complete.  May only be called
	// by mutators that were marked with UsesRename during registration.
	Rename(name string)

	// CreateModule creates a new module by calling the factory method for the specified moduleType, and applies
	// the specified property structs to it as if the properties were set in a blueprint file.  May only
	// be called by mutators that were marked with UsesCreateModule during registration.
	CreateModule(ModuleFactory, ...interface{}) Module
}

// An outgoingTransitionContextImpl and incomingTransitionContextImpl is created for every dependency of every module
// for each transition mutator.  bottomUpMutatorContext is created once for every module for every BottomUp mutator.
// Use a global pool for each to avoid reallocating every time.
var (
	outgoingTransitionContextPool = pool.New[outgoingTransitionContextImpl]()
	incomingTransitionContextPool = pool.New[incomingTransitionContextImpl]()
	bottomUpMutatorContextPool    = pool.New[bottomUpMutatorContext]()
)

type bottomUpMutatorContext struct {
	bp blueprint.BottomUpMutatorContext
	baseModuleContext
	finalPhase bool
}

// callers must immediately follow the call to this function with defer bottomUpMutatorContextPool.Put(mctx).
func bottomUpMutatorContextFactory(ctx blueprint.BottomUpMutatorContext, a Module,
	finalPhase bool) *bottomUpMutatorContext {

	moduleContext := a.base().baseModuleContextFactory(ctx)
	mctx := bottomUpMutatorContextPool.Get()
	*mctx = bottomUpMutatorContext{
		bp:                ctx,
		baseModuleContext: moduleContext,
		finalPhase:        finalPhase,
	}
	return mctx
}

func (x *registerMutatorsContext) BottomUp(name string, m BottomUpMutator) MutatorHandle {
	finalPhase := x.finalPhase
	f := func(ctx blueprint.BottomUpMutatorContext) {
		if a, ok := ctx.Module().(Module); ok {
			mctx := bottomUpMutatorContextFactory(ctx, a, finalPhase)
			defer bottomUpMutatorContextPool.Put(mctx)
			m(mctx)
		}
	}
	mutator := &mutator{name: x.mutatorName(name), bottomUpMutator: f}
	x.mutators = append(x.mutators, mutator)
	return mutator
}

func (x *registerMutatorsContext) BottomUpBlueprint(name string, m blueprint.BottomUpMutator) MutatorHandle {
	mutator := &mutator{name: name, bottomUpMutator: m}
	x.mutators = append(x.mutators, mutator)
	return mutator
}

func (x *registerMutatorsContext) Transition(name string, m VariationTransitionMutator) TransitionMutatorHandle {
	atm := &androidTransitionMutatorAdapter{
		finalPhase: x.finalPhase,
		mutator:    variationTransitionMutatorAdapter{m},
		name:       name,
	}
	mutator := &mutator{
		name:              name,
		transitionMutator: atm,
	}
	x.mutators = append(x.mutators, mutator)
	return mutator
}

func (x *registerMutatorsContext) InfoBasedTransition(name string, m androidTransitionMutator) TransitionMutatorHandle {
	atm := &androidTransitionMutatorAdapter{
		finalPhase: x.finalPhase,
		mutator:    m,
		name:       name,
	}
	mutator := &mutator{
		name:              name,
		transitionMutator: atm,
	}
	x.mutators = append(x.mutators, mutator)
	return mutator
}

func (x *registerMutatorsContext) mutatorName(name string) string {
	return name
}

func (mutator *mutator) componentName() string {
	return mutator.name
}

func (mutator *mutator) register(ctx *Context) {
	blueprintCtx := ctx.Context
	var handle blueprint.MutatorHandle
	if mutator.bottomUpMutator != nil {
		handle = blueprintCtx.RegisterBottomUpMutator(mutator.name, mutator.bottomUpMutator)
	} else if mutator.transitionMutator != nil {
		handle := blueprintCtx.RegisterTransitionMutator(mutator.name, mutator.transitionMutator)
		if mutator.neverFar {
			handle.NeverFar()
		}
	}

	// Forward booleans set on the MutatorHandle to the blueprint.MutatorHandle.
	if mutator.usesRename {
		handle.UsesRename()
	}
	if mutator.usesReverseDependencies {
		handle.UsesReverseDependencies()
	}
	if mutator.usesReplaceDependencies {
		handle.UsesReplaceDependencies()
	}
	if mutator.usesCreateModule {
		handle.UsesCreateModule()
	}
	if mutator.mutatesDependencies {
		handle.MutatesDependencies()
	}
	if mutator.mutatesGlobalState {
		handle.MutatesGlobalState()
	}
}

type MutatorHandle interface {
	// Parallel sets the mutator to visit modules in parallel while maintaining ordering.  Calling any
	// method on the mutator context is thread-safe, but the mutator must handle synchronization
	// for any modifications to global state or any modules outside the one it was invoked on.
	// Deprecated: all Mutators are parallel by default.
	Parallel() MutatorHandle

	// UsesRename marks the mutator as using the BottomUpMutatorContext.Rename method, which prevents
	// coalescing adjacent mutators into a single mutator pass.
	UsesRename() MutatorHandle

	// UsesReverseDependencies marks the mutator as using the BottomUpMutatorContext.AddReverseDependency
	// method, which prevents coalescing adjacent mutators into a single mutator pass.
	UsesReverseDependencies() MutatorHandle

	// UsesReplaceDependencies marks the mutator as using the BottomUpMutatorContext.ReplaceDependencies
	// method, which prevents coalescing adjacent mutators into a single mutator pass.
	UsesReplaceDependencies() MutatorHandle

	// UsesCreateModule marks the mutator as using the BottomUpMutatorContext.CreateModule method,
	// which prevents coalescing adjacent mutators into a single mutator pass.
	UsesCreateModule() MutatorHandle

	// MutatesDependencies marks the mutator as modifying properties in dependencies, which prevents
	// coalescing adjacent mutators into a single mutator pass.
	MutatesDependencies() MutatorHandle

	// MutatesGlobalState marks the mutator as modifying global state, which prevents coalescing
	// adjacent mutators into a single mutator pass.
	MutatesGlobalState() MutatorHandle
}

type TransitionMutatorHandle interface {
	// NeverFar causes the variations created by this mutator to never be ignored when adding
	// far variation dependencies. Normally, far variation dependencies ignore all the variants
	// of the source module, and only use the variants explicitly requested by the
	// AddFarVariationDependencies call.
	NeverFar() MutatorHandle
}

func (mutator *mutator) Parallel() MutatorHandle {
	return mutator
}

func (mutator *mutator) UsesRename() MutatorHandle {
	mutator.usesRename = true
	return mutator
}

func (mutator *mutator) UsesReverseDependencies() MutatorHandle {
	mutator.usesReverseDependencies = true
	return mutator
}

func (mutator *mutator) UsesReplaceDependencies() MutatorHandle {
	mutator.usesReplaceDependencies = true
	return mutator
}

func (mutator *mutator) UsesCreateModule() MutatorHandle {
	mutator.usesCreateModule = true
	return mutator
}

func (mutator *mutator) MutatesDependencies() MutatorHandle {
	mutator.mutatesDependencies = true
	return mutator
}

func (mutator *mutator) MutatesGlobalState() MutatorHandle {
	mutator.mutatesGlobalState = true
	return mutator
}

func (mutator *mutator) NeverFar() MutatorHandle {
	mutator.neverFar = true
	return mutator
}

func RegisterComponentsMutator(ctx RegisterMutatorsContext) {
	ctx.BottomUp("component-deps", componentDepsMutator)
}

// A special mutator that runs just prior to the deps mutator to allow the dependencies
// on component modules to be added so that they can depend directly on a prebuilt
// module.
func componentDepsMutator(ctx BottomUpMutatorContext) {
	ctx.Module().ComponentDepsMutator(ctx)
}

func depsMutator(ctx BottomUpMutatorContext) {
	if m := ctx.Module(); m.Enabled(ctx) {
		m.base().baseDepsMutator(ctx)
		m.DepsMutator(ctx)
	}
}

func registerDepsMutator(ctx RegisterMutatorsContext) {
	ctx.BottomUp("deps", depsMutator).UsesReverseDependencies()
}

// android.bottomUpMutatorContext either has to embed blueprint.BottomUpMutatorContext, in which case every method that
// has an overridden version in android.BaseModuleContext has to be manually forwarded to BaseModuleContext to avoid
// ambiguous method errors, or it has to store a blueprint.BottomUpMutatorContext non-embedded, in which case every
// non-overridden method has to be forwarded.  There are fewer non-overridden methods, so use the latter.  The following
// methods forward to the identical blueprint versions for bottomUpMutatorContext.

func (b *bottomUpMutatorContext) Rename(name string) {
	b.bp.Rename(name)
	b.Module().base().commonProperties.DebugName = name
}

func (b *bottomUpMutatorContext) createModule(factory blueprint.ModuleFactory, name string, props ...interface{}) Module {
	return bpModuleToModule(b.bp.CreateModule(factory, name, props...))
}

func (b *bottomUpMutatorContext) createModuleInDirectory(factory blueprint.ModuleFactory, name string, _ string, props ...interface{}) Module {
	panic("createModuleInDirectory is not implemented for bottomUpMutatorContext")
}

func (b *bottomUpMutatorContext) CreateModule(factory ModuleFactory, props ...interface{}) Module {
	return createModule(b, factory, "_bottomUpMutatorModule", doesNotSpecifyDirectory(), props...)
}

func (b *bottomUpMutatorContext) AddDependency(module blueprint.Module, tag blueprint.DependencyTag, name ...string) []Module {
	if b.baseModuleContext.checkedMissingDeps() {
		panic("Adding deps not allowed after checking for missing deps")
	}
	return bpModulesToModules(b.bp.AddDependency(module, tag, name...))
}

func (b *bottomUpMutatorContext) AddReverseDependency(module blueprint.Module, tag blueprint.DependencyTag, name string) {
	if b.baseModuleContext.checkedMissingDeps() {
		panic("Adding deps not allowed after checking for missing deps")
	}
	b.bp.AddReverseDependency(module, tag, name)
}

func (b *bottomUpMutatorContext) AddReverseVariationDependency(variations []blueprint.Variation, tag blueprint.DependencyTag, name string) {
	if b.baseModuleContext.checkedMissingDeps() {
		panic("Adding deps not allowed after checking for missing deps")
	}
	b.bp.AddReverseVariationDependency(variations, tag, name)
}

func (b *bottomUpMutatorContext) AddVariationDependencies(variations []blueprint.Variation, tag blueprint.DependencyTag,
	names ...string) []Module {
	if b.baseModuleContext.checkedMissingDeps() {
		panic("Adding deps not allowed after checking for missing deps")
	}
	return bpModulesToModules(b.bp.AddVariationDependencies(variations, tag, names...))
}

func (b *bottomUpMutatorContext) AddFarVariationDependencies(variations []blueprint.Variation,
	tag blueprint.DependencyTag, names ...string) []Module {
	if b.baseModuleContext.checkedMissingDeps() {
		panic("Adding deps not allowed after checking for missing deps")
	}

	return bpModulesToModules(b.bp.AddFarVariationDependencies(variations, tag, names...))
}

func (b *bottomUpMutatorContext) ReplaceDependencies(name string) {
	if b.baseModuleContext.checkedMissingDeps() {
		panic("Adding deps not allowed after checking for missing deps")
	}
	b.bp.ReplaceDependencies(name)
}

func (b *bottomUpMutatorContext) ReplaceDependenciesIf(name string, predicate blueprint.ReplaceDependencyPredicate) {
	if b.baseModuleContext.checkedMissingDeps() {
		panic("Adding deps not allowed after checking for missing deps")
	}
	b.bp.ReplaceDependenciesIf(name, predicate)
}

func bpModulesToModules(bpModules []blueprint.Module) []Module {
	modules := make([]Module, len(bpModules))
	for i, bpModule := range bpModules {
		modules[i] = bpModuleToModule(bpModule)
	}
	return modules
}

func bpModuleToModule(bpModule blueprint.Module) Module {
	if bpModule != nil {
		return bpModule.(Module)
	}
	return nil
}
