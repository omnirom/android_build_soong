// Copyright (C) 2019 The Android Open Source Project
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

// This file contains all the foundation components for override modules and their base module
// types. Override modules are a kind of opposite of default modules in that they override certain
// properties of an existing base module whereas default modules provide base module data to be
// overridden. However, unlike default and defaultable module pairs, both override and overridable
// modules generate and output build actions, and it is up to product make vars to decide which one
// to actually build and install in the end. In other words, default modules and defaultable modules
// can be compared to abstract classes and concrete classes in C++ and Java. By the same analogy,
// both override and overridable modules act like concrete classes.
//
// There is one more crucial difference from the logic perspective. Unlike default pairs, most Soong
// actions happen in the base (overridable) module by creating a local variant for each override
// module based on it.

import (
	"fmt"
	"reflect"
	"slices"

	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"
)

// Interface for override module types, e.g. override_android_app, override_apex
type OverrideModule interface {
	Module

	GetOverriddenModuleName() string

	getOverridingProperties() []interface{}
	setOverridingProperties(properties []interface{})

	getOverrideModuleProperties() *OverrideModuleProperties

	// Internal funcs to handle interoperability between override modules and prebuilts.
	// i.e. cases where an overriding module, too, is overridden by a prebuilt module.
	setOverriddenByPrebuilt(prebuilt Module)
	getOverriddenByPrebuilt() Module
}

// Base module struct for override module types
type OverrideModuleBase struct {
	moduleProperties OverrideModuleProperties

	overridingProperties []interface{}

	overriddenByPrebuilt Module
}

type OverrideModuleProperties struct {
	// Name of the base module to be overridden
	Base *string

	// TODO(jungjw): Add an optional override_name bool flag.
}

func (o *OverrideModuleBase) getOverridingProperties() []interface{} {
	return o.overridingProperties
}

func (o *OverrideModuleBase) setOverridingProperties(properties []interface{}) {
	o.overridingProperties = properties
}

func (o *OverrideModuleBase) getOverrideModuleProperties() *OverrideModuleProperties {
	return &o.moduleProperties
}

func (o *OverrideModuleBase) GetOverriddenModuleName() string {
	return proptools.String(o.moduleProperties.Base)
}

func (o *OverrideModuleBase) setOverriddenByPrebuilt(prebuilt Module) {
	o.overriddenByPrebuilt = prebuilt
}

func (o *OverrideModuleBase) getOverriddenByPrebuilt() Module {
	return o.overriddenByPrebuilt
}

func InitOverrideModule(m OverrideModule) {
	m.setOverridingProperties(m.GetProperties())

	m.AddProperties(m.getOverrideModuleProperties())
}

// Interface for overridable module types, e.g. android_app, apex
type OverridableModule interface {
	Module
	moduleBase() *OverridableModuleBase

	setOverridableProperties(prop []interface{})

	override(ctx BaseModuleContext, bm OverridableModule, info overrideTransitionMutatorInfo)
	GetOverriddenBy() string

	setOverridesProperty(overridesProperties *[]string)

	// Due to complications with incoming dependencies, overrides are processed after DepsMutator.
	// So, overridable properties need to be handled in a separate, dedicated deps mutator.
	OverridablePropertiesDepsMutator(ctx BottomUpMutatorContext)
}

type overridableModuleProperties struct {
	OverriddenBy string `blueprint:"mutated"`
}

// Base module struct for overridable module types
type OverridableModuleBase struct {
	overridableProperties []interface{}

	// If an overridable module has a property to list other modules that itself overrides, it should
	// set this to a pointer to the property through the InitOverridableModule function, so that
	// override information is propagated and aggregated correctly.
	overridesProperty *[]string

	overridableModuleProperties overridableModuleProperties
}

func InitOverridableModule(m OverridableModule, overridesProperty *[]string) {
	m.setOverridableProperties(m.(Module).GetProperties())
	m.setOverridesProperty(overridesProperty)
	m.AddProperties(&m.moduleBase().overridableModuleProperties)
}

func (o *OverridableModuleBase) moduleBase() *OverridableModuleBase {
	return o
}

func (b *OverridableModuleBase) setOverridableProperties(prop []interface{}) {
	b.overridableProperties = prop
}

func (b *OverridableModuleBase) setOverridesProperty(overridesProperty *[]string) {
	b.overridesProperty = overridesProperty
}

// Overrides a base module with the given OverrideModule.
func (b *OverridableModuleBase) override(ctx BaseModuleContext, bm OverridableModule, info overrideTransitionMutatorInfo) {
	for _, p := range b.overridableProperties {
		for _, op := range info.overridingProperties {
			if proptools.TypeEqual(p, op) {
				err := proptools.ExtendProperties(p, op, nil, proptools.OrderReplace)
				if err != nil {
					if propertyErr, ok := err.(*proptools.ExtendPropertyError); ok {
						ctx.OtherModulePropertyErrorf(bm, propertyErr.Property, "%s", propertyErr.Err.Error())
					} else {
						panic(err)
					}
				}
			}
		}
	}
	// Adds the base module to the overrides property, if exists, of the overriding module. See the
	// comment on OverridableModuleBase.overridesProperty for details.
	if b.overridesProperty != nil {
		*b.overridesProperty = append(*b.overridesProperty, ctx.OtherModuleName(bm))
	}
	b.overridableModuleProperties.OverriddenBy = info.name
}

// GetOverriddenBy returns the name of the override module that has overridden this module.
// For example, if an override module foo has its 'base' property set to bar, then another local variant
// of bar is created and its properties are overriden by foo. This method returns bar when called from
// the new local variant. It returns "" when called from the original variant of bar.
func (b *OverridableModuleBase) GetOverriddenBy() string {
	return b.overridableModuleProperties.OverriddenBy
}

func (b *OverridableModuleBase) OverridablePropertiesDepsMutator(ctx BottomUpMutatorContext) {
}

// Mutators for override/overridable modules. All the fun happens in these functions. It is critical
// to keep them in this order and not put any order mutators between them.
func RegisterOverridePostDepsMutators(ctx RegisterMutatorsContext) {
	ctx.BottomUp("override_deps", overrideModuleDepsMutator)
	ctx.InfoBasedTransition("override", NewGenericTransitionMutatorAdapter(&overrideTransitionMutator{}))
	// overridableModuleDepsMutator calls OverridablePropertiesDepsMutator so that overridable modules can
	// add deps from overridable properties.
	ctx.BottomUp("overridable_deps", overridableModuleDepsMutator)
	// Because overridableModuleDepsMutator is run after PrebuiltPostDepsMutator,
	// prebuilt's ReplaceDependencies doesn't affect to those deps added by overridable properties.
	// By running PrebuiltPostDepsMutator again after overridableModuleDepsMutator, deps via overridable properties
	// can be replaced with prebuilts.
	ctx.BottomUp("replace_deps_on_prebuilts_for_overridable_deps_again", PrebuiltPostDepsMutator).UsesReplaceDependencies()
	ctx.BottomUp("replace_deps_on_override", replaceDepsOnOverridingModuleMutator).UsesReplaceDependencies()
}

type overrideBaseDependencyTag struct {
	blueprint.BaseDependencyTag
}

var overrideBaseDepTag overrideBaseDependencyTag

// Override module should always override the source module.
// Overrides are implemented as a variant of the overridden module, and the build actions are created in the
// module context of the overridden module.
// If we replace override module with the prebuilt of the overridden module, `GenerateAndroidBuildActions` for
// the override module will have a very different meaning.
func (tag overrideBaseDependencyTag) ReplaceSourceWithPrebuilt() bool {
	return false
}

// Adds dependency on the base module to the overriding module so that they can be visited in the
// next phase.
func overrideModuleDepsMutator(ctx BottomUpMutatorContext) {
	if module, ok := ctx.Module().(OverrideModule); ok {
		base := String(module.getOverrideModuleProperties().Base)
		if !ctx.OtherModuleExists(base) {
			ctx.PropertyErrorf("base", "%q is not a valid module name", base)
			return
		}

		ctx.AddDependency(ctx.Module(), overrideBaseDepTag, *module.getOverrideModuleProperties().Base)

		// Make a copy of module.getOverridingProperties so that it won't be modified by any future
		// mutators, which would violate the immutable providers requirement.
		overridingProps := slices.Clone(module.getOverridingProperties())
		for i, props := range overridingProps {
			overridingProps[i] = proptools.CloneProperties(reflect.ValueOf(props)).Interface()
		}

		info := overrideTransitionMutatorInfo{
			name:                 module.Name(),
			overridingProperties: overridingProps,
		}

		prebuiltDeps := ctx.GetDirectDepsProxyWithTag(PrebuiltDepTag)
		for _, prebuiltDep := range prebuiltDeps {
			depInfo, ok := OtherModuleProvider(ctx, prebuiltDep, PrebuiltInfoProvider)
			if !ok {
				panic("PrebuiltDepTag leads to a non-prebuilt module " + prebuiltDep.Name())
			}
			info.overrideModuleUsePrebuilt = info.overrideModuleUsePrebuilt || depInfo.UsePrebuilt
			info.overrideModulePrebuiltPartitions = append(info.overrideModulePrebuiltPartitions,
				depInfo.PartitionTag)
			info.overrideModulePrebuiltNames = append(info.overrideModulePrebuiltNames,
				ctx.OtherModuleName(prebuiltDep))
		}
		SetProvider(ctx, overrideModuleDefaultInfoProvider, info)
	}
}

var overrideModuleDefaultInfoProvider = blueprint.NewMutatorProvider[overrideTransitionMutatorInfo]("override_deps")

var OverrideInfoProvider = blueprint.NewMutatorProvider[OverrideInfo]("override_mutate")

type OverrideInfo struct {
	OverriddenBy string
}

// Now, goes through all overridable modules, finds all modules overriding them, creates a local
// variant for each of them, and performs the actual overriding operation by calling override().
type overrideTransitionMutator struct{}

var _ TransitionMutator[overrideTransitionMutatorInfo] = (*overrideTransitionMutator)(nil)

type overrideTransitionMutatorInfo struct {
	name                 string
	overridingProperties []interface{} `blueprint:"allow_configurable_in_provider"`

	overrideModuleUsePrebuilt        bool
	overrideModulePrebuiltPartitions []string
	overrideModulePrebuiltNames      []string
}

var overrideTransitionMutatorEmptyVariation = overrideTransitionMutatorInfo{name: ""}

func (info overrideTransitionMutatorInfo) Variation() string {
	return info.name
}

func (overrideTransitionMutator) Split(ctx BaseModuleContext) []overrideTransitionMutatorInfo {
	if _, ok := ctx.Module().(OverrideModule); ok {
		// Create a variant of the overriding module with its own name. This matches the above local
		// variant name rule for overridden modules, and thus allows ReplaceDependencies to match the
		// two.
		info, _ := ModuleProvider(ctx, overrideModuleDefaultInfoProvider)
		return []overrideTransitionMutatorInfo{info}
	}

	return []overrideTransitionMutatorInfo{overrideTransitionMutatorEmptyVariation}
}

func (overrideTransitionMutator) OutgoingTransition(ctx OutgoingTransitionContext, sourceInfo overrideTransitionMutatorInfo) overrideTransitionMutatorInfo {
	if _, ok := ctx.Module().(OverrideModule); ok {
		if ctx.DepTag() == overrideBaseDepTag {
			return sourceInfo
		}
	}

	// Variations are always local and shouldn't affect the variant used for dependencies
	return overrideTransitionMutatorEmptyVariation
}

func (overrideTransitionMutator) IncomingTransition(ctx IncomingTransitionContext, incomingInfo overrideTransitionMutatorInfo) overrideTransitionMutatorInfo {
	if _, ok := ctx.Module().(OverridableModule); ok {
		return incomingInfo
	} else if _, ok := ctx.Module().(OverrideModule); ok {
		// To allow dependencies to be added without having to know the variation.
		info, _ := ModuleProvider(ctx, overrideModuleDefaultInfoProvider)
		return info
	}

	return overrideTransitionMutatorEmptyVariation
}

func (overrideTransitionMutator) Mutate(ctx BottomUpMutatorContext, info overrideTransitionMutatorInfo) {
	if info.name != "" {
		if b, ok := ctx.Module().(OverridableModule); ok {
			b.override(ctx, b, info)

			checkPrebuiltReplacesOverride(ctx, b, info)
		}
		SetProvider(ctx, OverrideInfoProvider, OverrideInfo{
			OverriddenBy: info.name,
		})
	}
}

func (overrideTransitionMutator) TransitionInfoFromVariation(variation string) overrideTransitionMutatorInfo {
	panic(fmt.Errorf("not implemented"))
}

func checkPrebuiltReplacesOverride(ctx BottomUpMutatorContext, bm OverridableModule, info overrideTransitionMutatorInfo) {
	// See if there's a prebuilt module that overrides this override module with prefer flag,
	// in which case we call HideFromMake on the corresponding variant later.
	if info.overrideModuleUsePrebuilt {
		// The overriding module itself, too, is overridden by a prebuilt.
		// Perform the same check for replacement
		checkInvariantsForSourceAndPrebuilt(ctx, bm.PartitionTag(ctx.DeviceConfig()), info.overrideModulePrebuiltPartitions, info.overrideModulePrebuiltNames)
		// Copy the flag and hide it in make
		bm.ReplacedByPrebuilt()
	}
}

func overridableModuleDepsMutator(ctx BottomUpMutatorContext) {
	ctx.Module().base().baseOverridablePropertiesDepsMutator(ctx)
	if b, ok := ctx.Module().(OverridableModule); ok && b.Enabled(ctx) {
		b.OverridablePropertiesDepsMutator(ctx)
	}
}

func replaceDepsOnOverridingModuleMutator(ctx BottomUpMutatorContext) {
	if b, ok := ctx.Module().(OverridableModule); ok {
		if o := b.GetOverriddenBy(); o != "" {
			// Redirect dependencies on the overriding module to this overridden module. Overriding
			// modules are basically pseudo modules, and all build actions are associated to overridden
			// modules. Therefore, dependencies on overriding modules need to be forwarded there as well.
			ctx.ReplaceDependencies(o)
		}
	}
}
