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

import "github.com/google/blueprint"

// TransitionMutator implements a top-down mechanism where a module tells its
// direct dependencies what variation they should be built in but the dependency
// has the final say.
//
// When implementing a transition mutator, one needs to implement four methods:
//   - Split() that tells what variations a module has by itself
//   - OutgoingTransition() where a module tells what it wants from its
//     dependency
//   - IncomingTransition() where a module has the final say about its own
//     variation
//   - Mutate() that changes the state of a module depending on its variation
//
// That the effective variation of module B when depended on by module A is the
// composition the outgoing transition of module A and the incoming transition
// of module B.
//
// the outgoing transition should not take the properties of the dependency into
// account, only those of the module that depends on it. For this reason, the
// dependency is not even passed into it as an argument. Likewise, the incoming
// transition should not take the properties of the depending module into
// account and is thus not informed about it. This makes for a nice
// decomposition of the decision logic.
//
// A given transition mutator only affects its own variation; other variations
// stay unchanged along the dependency edges.
//
// Soong makes sure that all modules are created in the desired variations and
// that dependency edges are set up correctly. This ensures that "missing
// variation" errors do not happen and allows for more flexible changes in the
// value of the variation among dependency edges (as oppposed to bottom-up
// mutators where if module A in variation X depends on module B and module B
// has that variation X, A must depend on variation X of B)
//
// The limited power of the context objects passed to individual mutators
// methods also makes it more difficult to shoot oneself in the foot. Complete
// safety is not guaranteed because no one prevents individual transition
// mutators from mutating modules in illegal ways and for e.g. Split() or
// Mutate() to run their own visitations of the transitive dependency of the
// module and both of these are bad ideas, but it's better than no guardrails at
// all.
//
// This model is pretty close to Bazel's configuration transitions. The mapping
// between concepts in Soong and Bazel is as follows:
//   - Module == configured target
//   - Variant == configuration
//   - Variation name == configuration flag
//   - Variation == configuration flag value
//   - Outgoing transition == attribute transition
//   - Incoming transition == rule transition
//
// The Split() method does not have a Bazel equivalent and Bazel split
// transitions do not have a Soong equivalent.
//
// Mutate() does not make sense in Bazel due to the different models of the
// two systems: when creating new variations, Soong clones the old module and
// thus some way is needed to change it state whereas Bazel creates each
// configuration of a given configured target anew.
type TransitionMutator[T blueprint.TransitionInfo] interface {
	// Split returns the set of variations that should be created for a module no
	// matter who depends on it. Used when Make depends on a particular variation
	// or when the module knows its variations just based on information given to
	// it in the Blueprint file. This method should not mutate the module it is
	// called on.
	Split(ctx BaseModuleContext) []T

	// OutgoingTransition is called on a module to determine which variation it wants
	// from its direct dependencies. The dependency itself can override this decision.
	// This method should not mutate the module itself.
	OutgoingTransition(ctx OutgoingTransitionContext, sourceTransitionInfo T) T

	// IncomingTransition is called on a module to determine which variation it should
	// be in based on the variation modules that depend on it want. This gives the module
	// a final say about its own variations. This method should not mutate the module
	// itself.
	IncomingTransition(ctx IncomingTransitionContext, incomingTransitionInfo T) T

	// Mutate is called after a module was split into multiple variations on each variation.
	// It should not split the module any further but adding new dependencies is
	// fine. Unlike all the other methods on TransitionMutator, this method is
	// allowed to mutate the module.
	Mutate(ctx BottomUpMutatorContext, transitionInfo T)

	// TransitionInfoFromVariation is called when adding dependencies with an explicit variation after the
	// TransitionMutator has already run.  It takes a variation name and returns a TransitionInfo for that
	// variation.  It may not be possible for some TransitionMutators to generate an appropriate TransitionInfo
	// if the variation does not contain all the information from the TransitionInfo, in which case the
	// TransitionMutator can panic in TransitionInfoFromVariation, and adding dependencies with explicit variations
	// for this TransitionMutator is not supported.
	TransitionInfoFromVariation(variation string) T
}

// androidTransitionMutator is a copy of blueprint.TransitionMutator with the context argument types changed
// from blueprint.BaseModuleContext to BaseModuleContext, etc.
type androidTransitionMutator interface {
	Split(ctx BaseModuleContext) []blueprint.TransitionInfo
	OutgoingTransition(ctx OutgoingTransitionContext, sourceTransitionInfo blueprint.TransitionInfo) blueprint.TransitionInfo
	IncomingTransition(ctx IncomingTransitionContext, incomingTransitionInfo blueprint.TransitionInfo) blueprint.TransitionInfo
	Mutate(ctx BottomUpMutatorContext, transitionInfo blueprint.TransitionInfo)
	TransitionInfoFromVariation(variation string) blueprint.TransitionInfo
}

// VariationTransitionMutator is a simpler version of androidTransitionMutator that passes variation strings instead
// of a blueprint.TransitionInfo object.
type VariationTransitionMutator interface {
	Split(ctx BaseModuleContext) []string
	OutgoingTransition(ctx OutgoingTransitionContext, sourceVariation string) string
	IncomingTransition(ctx IncomingTransitionContext, incomingVariation string) string
	Mutate(ctx BottomUpMutatorContext, variation string)
}

type IncomingTransitionContext interface {
	ArchModuleContext
	ModuleProviderContext
	ModuleErrorContext

	// Module returns the target of the dependency edge for which the transition
	// is being computed
	Module() Module

	// ModuleName returns the name of the module.  This is generally the value that was returned by Module.Name() when
	// the module was created, but may have been modified by calls to BottomUpMutatorContext.Rename.
	ModuleName() string

	// DepTag() Returns the dependency tag through which this dependency is
	// reached
	DepTag() blueprint.DependencyTag

	// Config returns the configuration for the build.
	Config() Config

	DeviceConfig() DeviceConfig

	// IsAddingDependency returns true if the transition is being called while adding a dependency
	// after the transition mutator has already run, or false if it is being called when the transition
	// mutator is running.  This should be used sparingly, all uses will have to be removed in order
	// to support creating variants on demand.
	IsAddingDependency() bool
}

type OutgoingTransitionContext interface {
	ArchModuleContext
	ModuleProviderContext

	// Module returns the target of the dependency edge for which the transition
	// is being computed
	Module() Module

	// ModuleName returns the name of the module.  This is generally the value that was returned by Module.Name() when
	// the module was created, but may have been modified by calls to BottomUpMutatorContext.Rename.
	ModuleName() string

	// DepTag() Returns the dependency tag through which this dependency is
	// reached
	DepTag() blueprint.DependencyTag

	// Config returns the configuration for the build.
	Config() Config

	DeviceConfig() DeviceConfig
}

// androidTransitionMutatorAdapter wraps an androidTransitionMutator to convert it to a blueprint.TransitionInfo
// by converting the blueprint.*Context objects into android.*Context objects.
type androidTransitionMutatorAdapter struct {
	finalPhase bool
	mutator    androidTransitionMutator
	name       string
}

func (a *androidTransitionMutatorAdapter) Split(ctx blueprint.BaseModuleContext) []blueprint.TransitionInfo {
	if a.finalPhase {
		panic("TransitionMutator not allowed in FinalDepsMutators")
	}
	m := ctx.Module().(Module)
	moduleContext := m.base().baseModuleContextFactory(ctx)
	return a.mutator.Split(&moduleContext)
}

func (a *androidTransitionMutatorAdapter) OutgoingTransition(bpctx blueprint.OutgoingTransitionContext,
	sourceTransitionInfo blueprint.TransitionInfo) blueprint.TransitionInfo {
	m := bpctx.Module().(Module)
	ctx := outgoingTransitionContextPool.Get()
	defer outgoingTransitionContextPool.Put(ctx)
	*ctx = outgoingTransitionContextImpl{
		archModuleContext: m.base().archModuleContextFactory(bpctx),
		bp:                bpctx,
	}
	return a.mutator.OutgoingTransition(ctx, sourceTransitionInfo)
}

func (a *androidTransitionMutatorAdapter) IncomingTransition(bpctx blueprint.IncomingTransitionContext,
	incomingTransitionInfo blueprint.TransitionInfo) blueprint.TransitionInfo {
	m := bpctx.Module().(Module)
	ctx := incomingTransitionContextPool.Get()
	defer incomingTransitionContextPool.Put(ctx)
	*ctx = incomingTransitionContextImpl{
		archModuleContext: m.base().archModuleContextFactory(bpctx),
		bp:                bpctx,
	}
	return a.mutator.IncomingTransition(ctx, incomingTransitionInfo)
}

func (a *androidTransitionMutatorAdapter) Mutate(ctx blueprint.BottomUpMutatorContext, transitionInfo blueprint.TransitionInfo) {
	am := ctx.Module().(Module)
	variation := transitionInfo.Variation()
	if variation != "" {
		// TODO: this should really be checking whether the TransitionMutator affected this module, not
		//  the empty variant, but TransitionMutator has no concept of skipping a module.
		base := am.base()
		base.commonProperties.DebugMutators = append(base.commonProperties.DebugMutators, a.name)
		base.commonProperties.DebugVariations = append(base.commonProperties.DebugVariations, variation)
	}

	mctx := bottomUpMutatorContextFactory(ctx, am, a.finalPhase)
	defer bottomUpMutatorContextPool.Put(mctx)
	a.mutator.Mutate(mctx, transitionInfo)
}

func (a *androidTransitionMutatorAdapter) TransitionInfoFromVariation(variation string) blueprint.TransitionInfo {
	return a.mutator.TransitionInfoFromVariation(variation)
}

// variationTransitionMutatorAdapter wraps a VariationTransitionMutator to convert it to an androidTransitionMutator
// by wrapping the string info object used by VariationTransitionMutator with variationTransitionInfo to convert it into
// blueprint.TransitionInfo.
type variationTransitionMutatorAdapter struct {
	m VariationTransitionMutator
}

func (v variationTransitionMutatorAdapter) Split(ctx BaseModuleContext) []blueprint.TransitionInfo {
	variations := v.m.Split(ctx)
	transitionInfos := make([]blueprint.TransitionInfo, 0, len(variations))
	for _, variation := range variations {
		transitionInfos = append(transitionInfos, variationTransitionInfo{variation})
	}
	return transitionInfos
}

func (v variationTransitionMutatorAdapter) OutgoingTransition(ctx OutgoingTransitionContext,
	sourceTransitionInfo blueprint.TransitionInfo) blueprint.TransitionInfo {

	sourceVariationTransitionInfo, _ := sourceTransitionInfo.(variationTransitionInfo)
	outgoingVariation := v.m.OutgoingTransition(ctx, sourceVariationTransitionInfo.variation)
	return variationTransitionInfo{outgoingVariation}
}

func (v variationTransitionMutatorAdapter) IncomingTransition(ctx IncomingTransitionContext,
	incomingTransitionInfo blueprint.TransitionInfo) blueprint.TransitionInfo {

	incomingVariationTransitionInfo, _ := incomingTransitionInfo.(variationTransitionInfo)
	variation := v.m.IncomingTransition(ctx, incomingVariationTransitionInfo.variation)
	return variationTransitionInfo{variation}
}

func (v variationTransitionMutatorAdapter) Mutate(ctx BottomUpMutatorContext, transitionInfo blueprint.TransitionInfo) {
	variationTransitionInfo, _ := transitionInfo.(variationTransitionInfo)
	v.m.Mutate(ctx, variationTransitionInfo.variation)
}

func (v variationTransitionMutatorAdapter) TransitionInfoFromVariation(variation string) blueprint.TransitionInfo {
	return variationTransitionInfo{variation}
}

// variationTransitionInfo is a blueprint.TransitionInfo that contains a single variation string.
type variationTransitionInfo struct {
	variation string
}

func (v variationTransitionInfo) Variation() string {
	return v.variation
}

// genericTransitionMutatorAdapter wraps a TransitionMutator to convert it to an androidTransitionMutator
type genericTransitionMutatorAdapter[T blueprint.TransitionInfo] struct {
	m TransitionMutator[T]
}

// NewGenericTransitionMutatorAdapter is used to convert a generic TransitionMutator[T] into an androidTransitionMutator
// that can be passed to RegisterMutatorsContext.InfoBasedTransition.
func NewGenericTransitionMutatorAdapter[T blueprint.TransitionInfo](m TransitionMutator[T]) androidTransitionMutator {
	return &genericTransitionMutatorAdapter[T]{m}
}

func (g *genericTransitionMutatorAdapter[T]) convertTransitionInfoToT(transitionInfo blueprint.TransitionInfo) T {
	if transitionInfo == nil {
		var zero T
		return zero
	}
	return transitionInfo.(T)
}

func (g *genericTransitionMutatorAdapter[T]) Split(ctx BaseModuleContext) []blueprint.TransitionInfo {
	transitionInfos := g.m.Split(ctx)
	bpTransitionInfos := make([]blueprint.TransitionInfo, 0, len(transitionInfos))
	for _, transitionInfo := range transitionInfos {
		bpTransitionInfos = append(bpTransitionInfos, transitionInfo)
	}
	return bpTransitionInfos
}

func (g *genericTransitionMutatorAdapter[T]) OutgoingTransition(ctx OutgoingTransitionContext, sourceTransitionInfo blueprint.TransitionInfo) blueprint.TransitionInfo {
	sourceTransitionInfoT := g.convertTransitionInfoToT(sourceTransitionInfo)
	return g.m.OutgoingTransition(ctx, sourceTransitionInfoT)
}

func (g *genericTransitionMutatorAdapter[T]) IncomingTransition(ctx IncomingTransitionContext, incomingTransitionInfo blueprint.TransitionInfo) blueprint.TransitionInfo {
	incomingTransitionInfoT := g.convertTransitionInfoToT(incomingTransitionInfo)
	return g.m.IncomingTransition(ctx, incomingTransitionInfoT)
}

func (g *genericTransitionMutatorAdapter[T]) Mutate(ctx BottomUpMutatorContext, transitionInfo blueprint.TransitionInfo) {
	transitionInfoT := g.convertTransitionInfoToT(transitionInfo)
	g.m.Mutate(ctx, transitionInfoT)
}

func (g *genericTransitionMutatorAdapter[T]) TransitionInfoFromVariation(variation string) blueprint.TransitionInfo {
	return g.m.TransitionInfoFromVariation(variation)
}

// incomingTransitionContextImpl wraps a blueprint.IncomingTransitionContext to convert it to an
// IncomingTransitionContext.
type incomingTransitionContextImpl struct {
	archModuleContext
	bp blueprint.IncomingTransitionContext
}

func (c *incomingTransitionContextImpl) Module() Module {
	return c.bp.Module().(Module)
}

func (c *incomingTransitionContextImpl) ModuleName() string {
	return c.bp.ModuleName()
}

func (c *incomingTransitionContextImpl) DepTag() blueprint.DependencyTag {
	return c.bp.DepTag()
}

func (c *incomingTransitionContextImpl) Config() Config {
	return c.bp.Config().(Config)
}

func (c *incomingTransitionContextImpl) DeviceConfig() DeviceConfig {
	return DeviceConfig{c.bp.Config().(Config).deviceConfig}
}

func (c *incomingTransitionContextImpl) IsAddingDependency() bool {
	return c.bp.IsAddingDependency()
}

func (c *incomingTransitionContextImpl) provider(provider blueprint.AnyProviderKey) (any, bool) {
	return c.bp.Provider(provider)
}

func (c *incomingTransitionContextImpl) ModuleErrorf(fmt string, args ...interface{}) {
	c.bp.ModuleErrorf(fmt, args)
}

func (c *incomingTransitionContextImpl) PropertyErrorf(property, fmt string, args ...interface{}) {
	c.bp.PropertyErrorf(property, fmt, args)
}

// outgoingTransitionContextImpl wraps a blueprint.OutgoingTransitionContext to convert it to an
// OutgoingTransitionContext.
type outgoingTransitionContextImpl struct {
	archModuleContext
	bp blueprint.OutgoingTransitionContext
}

func (c *outgoingTransitionContextImpl) Module() Module {
	return c.bp.Module().(Module)
}

func (c *outgoingTransitionContextImpl) ModuleName() string {
	return c.bp.ModuleName()
}

func (c *outgoingTransitionContextImpl) DepTag() blueprint.DependencyTag {
	return c.bp.DepTag()
}

func (c *outgoingTransitionContextImpl) Config() Config {
	return c.bp.Config().(Config)
}

func (c *outgoingTransitionContextImpl) DeviceConfig() DeviceConfig {
	return DeviceConfig{c.bp.Config().(Config).deviceConfig}
}

func (c *outgoingTransitionContextImpl) provider(provider blueprint.AnyProviderKey) (any, bool) {
	return c.bp.Provider(provider)
}
