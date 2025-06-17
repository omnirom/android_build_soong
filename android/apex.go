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

package android

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/google/blueprint"
)

var (
	// This is the sdk version when APEX was first introduced
	SdkVersion_Android10 = uncheckedFinalApiLevel(29)
)

// ApexInfo describes the metadata about one or more apexBundles that an apex variant of a module is
// part of.  When an apex variant is created, the variant is associated with one apexBundle. But
// when multiple apex variants are merged for deduping (see mergeApexVariations), this holds the
// information about the apexBundles that are merged together.
// Accessible via `ctx.Provider(android.ApexInfoProvider).(android.ApexInfo)`
type ApexInfo struct {
	// Name of the apex variation that this module (i.e. the apex variant of the module) is
	// mutated into, or "" for a platform (i.e. non-APEX) variant.
	//
	// Also note that a module can be included in multiple APEXes, in which case, the module is
	// mutated into one or more variants, each of which is for an APEX. The variants then can
	// later be deduped if they don't need to be compiled differently. This is an optimization
	// done in mergeApexVariations.
	ApexVariationName string

	// ApiLevel that this module has to support at minimum.
	MinSdkVersion ApiLevel

	// True if this module comes from an updatable apexBundle.
	Updatable bool

	// True if this module can use private platform APIs. Only non-updatable APEX can set this
	// to true.
	UsePlatformApis bool

	// True if this is for a prebuilt_apex.
	//
	// If true then this will customize the apex processing to make it suitable for handling
	// prebuilt_apex, e.g. it will prevent ApexInfos from being merged together.
	//
	// Unlike the source apex module type the prebuilt_apex module type cannot share compatible variants
	// across prebuilt_apex modules. That is because there is no way to determine whether two
	// prebuilt_apex modules that export files for the same module are compatible. e.g. they could have
	// been built from different source at different times or they could have been built with different
	// build options that affect the libraries.
	//
	// While it may be possible to provide sufficient information to determine whether two prebuilt_apex
	// modules were compatible it would be a lot of work and would not provide much benefit for a couple
	// of reasons:
	//   - The number of prebuilt_apex modules that will be exporting files for the same module will be
	//     low as the prebuilt_apex only exports files for the direct dependencies that require it and
	//     very few modules are direct dependencies of multiple prebuilt_apex modules, e.g. there are a
	//     few com.android.art* apex files that contain the same contents and could export files for the
	//     same modules but only one of them needs to do so. Contrast that with source apex modules which
	//     need apex specific variants for every module that contributes code to the apex, whether direct
	//     or indirect.
	//   - The build cost of a prebuilt_apex variant is generally low as at worst it will involve some
	//     extra copying of files. Contrast that with source apex modules that has to build each variant
	//     from source.
	ForPrebuiltApex bool

	// Returns the name of the overridden apex (com.android.foo)
	BaseApexName string

	// Returns the value of `apex_available_name`
	ApexAvailableName string
}

func (a ApexInfo) Variation() string {
	return a.ApexVariationName
}

// Minimize is called during a transition from a module with a unique variation per apex to a module that should
// share variations between apexes.  It returns a minimized ApexInfo that removes any apex names and replaces
// the variation name with one computed from the remaining properties.
func (a ApexInfo) Minimize() ApexInfo {
	info := ApexInfo{
		MinSdkVersion:   a.MinSdkVersion,
		UsePlatformApis: a.UsePlatformApis,
	}
	info.ApexVariationName = info.mergedName()
	return info
}

type ApexAvailableInfo struct {
	// Returns the apex names that this module is available for
	ApexAvailableFor []string
}

var ApexInfoProvider = blueprint.NewMutatorProvider[ApexInfo]("apex_mutate")
var ApexAvailableInfoProvider = blueprint.NewMutatorProvider[ApexAvailableInfo]("apex_mutate")

func (i ApexInfo) AddJSONData(d *map[string]interface{}) {
	(*d)["Apex"] = map[string]interface{}{
		"ApexVariationName": i.ApexVariationName,
		"MinSdkVersion":     i.MinSdkVersion,
		"ForPrebuiltApex":   i.ForPrebuiltApex,
	}
}

// mergedName gives the name of the alias variation that will be used when multiple apex variations
// of a module can be deduped into one variation. For example, if libfoo is included in both apex.a
// and apex.b, and if the two APEXes have the same min_sdk_version (say 29), then libfoo doesn't
// have to be built twice, but only once. In that case, the two apex variations apex.a and apex.b
// are configured to have the same alias variation named apex29. Whether platform APIs is allowed
// or not also matters; if two APEXes don't have the same allowance, they get different names and
// thus wouldn't be merged.
func (i ApexInfo) mergedName() string {
	name := "apex" + strconv.Itoa(i.MinSdkVersion.FinalOrFutureInt())
	if i.UsePlatformApis {
		name += "_p"
	}
	return name
}

// IsForPlatform tells whether this module is for the platform or not. If false is returned, it
// means that this apex variant of the module is built for an APEX.
func (i ApexInfo) IsForPlatform() bool {
	return i.ApexVariationName == ""
}

// To satisfy the comparable interface
func (i ApexInfo) Equal(other any) bool {
	otherApexInfo, ok := other.(ApexInfo)
	return ok && i.ApexVariationName == otherApexInfo.ApexVariationName &&
		i.MinSdkVersion == otherApexInfo.MinSdkVersion &&
		i.Updatable == otherApexInfo.Updatable &&
		i.UsePlatformApis == otherApexInfo.UsePlatformApis
}

// ApexBundleInfo contains information about the dependencies of an apex
type ApexBundleInfo struct {
}

var ApexBundleInfoProvider = blueprint.NewMutatorProvider[ApexBundleInfo]("apex_mutate")

// DepInSameApexChecker defines an interface that should be used to determine whether a given dependency
// should be considered as part of the same APEX as the current module or not.
type DepInSameApexChecker interface {
	// OutgoingDepIsInSameApex tests if the module depended on via 'tag' is considered as part of
	// the same APEX as this module. For example, a static lib dependency usually returns true here, while a
	// shared lib dependency to a stub library returns false.
	//
	// This method must not be called directly without first ignoring dependencies whose tags
	// implement ExcludeFromApexContentsTag. Calls from within the func passed to WalkPayloadDeps()
	// are fine as WalkPayloadDeps() will ignore those dependencies automatically. Otherwise, use
	// IsDepInSameApex instead.
	OutgoingDepIsInSameApex(tag blueprint.DependencyTag) bool

	// IncomingDepIsInSameApex tests if this module depended on via 'tag' is considered as part of
	// the same APEX as the depending module module. For example, a static lib dependency usually
	// returns true here, while a shared lib dependency to a stub library returns false.
	//
	// This method must not be called directly without first ignoring dependencies whose tags
	// implement ExcludeFromApexContentsTag. Calls from within the func passed to WalkPayloadDeps()
	// are fine as WalkPayloadDeps() will ignore those dependencies automatically. Otherwise, use
	// IsDepInSameApex instead.
	IncomingDepIsInSameApex(tag blueprint.DependencyTag) bool
}

// DepInSameApexInfo is a provider that wraps around a DepInSameApexChecker that can be
// used to check if a dependency belongs to the same apex as the module when walking
// through the dependencies of a module.
type DepInSameApexInfo struct {
	Checker DepInSameApexChecker
}

var DepInSameApexInfoProvider = blueprint.NewMutatorProvider[DepInSameApexInfo]("apex_unique")

func IsDepInSameApex(ctx BaseModuleContext, module, dep Module) bool {
	depTag := ctx.OtherModuleDependencyTag(dep)
	if _, ok := depTag.(ExcludeFromApexContentsTag); ok {
		// The tag defines a dependency that never requires the child module to be part of the same
		// apex as the parent.
		return false
	}

	if !EqualModules(ctx.Module(), module) {
		if moduleInfo, ok := OtherModuleProvider(ctx, module, DepInSameApexInfoProvider); ok {
			if !moduleInfo.Checker.OutgoingDepIsInSameApex(depTag) {
				return false
			}
		}
	} else {
		if m, ok := ctx.Module().(ApexModule); ok && !m.GetDepInSameApexChecker().OutgoingDepIsInSameApex(depTag) {
			return false
		}
	}
	if depInfo, ok := OtherModuleProvider(ctx, dep, DepInSameApexInfoProvider); ok {
		if !depInfo.Checker.IncomingDepIsInSameApex(depTag) {
			return false
		}
	}

	return true
}

// ApexModule is the interface that a module type is expected to implement if the module has to be
// built differently depending on whether the module is destined for an APEX or not (i.e., installed
// to one of the regular partitions).
//
// Native shared libraries are one such module type; when it is built for an APEX, it should depend
// only on stable interfaces such as NDK, stable AIDL, or C APIs from other APEXes.
//
// A module implementing this interface will be mutated into multiple variations by apex.apexMutator
// if it is directly or indirectly included in one or more APEXes. Specifically, if a module is
// included in apex.foo and apex.bar then three apex variants are created: platform, apex.foo and
// apex.bar. The platform variant is for the regular partitions (e.g., /system or /vendor, etc.)
// while the other two are for the APEXs, respectively. The latter two variations can be merged (see
// mergedName) when the two APEXes have the same min_sdk_version requirement.
type ApexModule interface {
	Module

	apexModuleBase() *ApexModuleBase

	// Marks that this module should be built for the specified APEX. Call this BEFORE
	// apex.apexMutator is run.
	BuildForApex(apex ApexInfo)

	// Returns true if this module is present in any APEX either directly or indirectly. Call
	// this after apex.apexMutator is run.
	InAnyApex() bool

	// NotInPlatform returns true if the module is not available to the platform due to
	// apex_available being set and not containing "//apex_available:platform".
	NotInPlatform() bool

	// Tests if this module could have APEX variants. Even when a module type implements
	// ApexModule interface, APEX variants are created only for the module instances that return
	// true here. This is useful for not creating APEX variants for certain types of shared
	// libraries such as NDK stubs.
	CanHaveApexVariants() bool

	// Tests if this module can be installed to APEX as a file. For example, this would return
	// true for shared libs while return false for static libs because static libs are not
	// installable module (but it can still be mutated for APEX)
	IsInstallableToApex() bool

	// Tests if this module is available for the specified APEX or ":platform". This is from the
	// apex_available property of the module.
	AvailableFor(what string) bool

	// Returns the apexes that are available for this module, valid values include
	// "//apex_available:platform", "//apex_available:anyapex" and specific apexes.
	// There are some differences between this one and the ApexAvailable on
	// ApexModuleBase for cc, java library and sdkLibraryXml.
	ApexAvailableFor() []string

	// AlwaysRequiresPlatformApexVariant allows the implementing module to determine whether an
	// APEX mutator should always be created for it.
	//
	// Returns false by default.
	AlwaysRequiresPlatformApexVariant() bool

	// Returns true if this module is not available to platform (i.e. apex_available property
	// doesn't have "//apex_available:platform"), or shouldn't be available to platform, which
	// is the case when this module depends on other module that isn't available to platform.
	NotAvailableForPlatform() bool

	// Marks that this module is not available to platform. Set by the
	// check-platform-availability mutator in the apex package.
	SetNotAvailableForPlatform()

	// Returns the min sdk version that the module supports, .
	MinSdkVersionSupported(ctx BaseModuleContext) ApiLevel

	// Returns true if this module needs a unique variation per apex, effectively disabling the
	// deduping. This is turned on when, for example if use_apex_name_macro is set so that each
	// apex variant should be built with different macro definitions.
	UniqueApexVariations() bool

	GetDepInSameApexChecker() DepInSameApexChecker
}

// Properties that are common to all module types implementing ApexModule interface.
type ApexProperties struct {
	// Availability of this module in APEXes. Only the listed APEXes can contain this module. If
	// the module has stubs then other APEXes and the platform may access it through them
	// (subject to visibility).
	//
	// "//apex_available:anyapex" is a pseudo APEX name that matches to any APEX.
	// "//apex_available:platform" refers to non-APEX partitions like "system.img".
	// Prefix pattern (com.foo.*) can be used to match with any APEX name with the prefix(com.foo.).
	// Default is ["//apex_available:platform"].
	Apex_available []string

	// See ApexModule.NotAvailableForPlatform()
	NotAvailableForPlatform bool `blueprint:"mutated"`

	// See ApexModule.UniqueApexVariants()
	UniqueApexVariationsForDeps bool `blueprint:"mutated"`
}

// Marker interface that identifies dependencies that are excluded from APEX contents.
//
// At the moment the sdk.sdkRequirementsMutator relies on the fact that the existing tags which
// implement this interface do not define dependencies onto members of an sdk_snapshot. If that
// changes then sdk.sdkRequirementsMutator will need fixing.
type ExcludeFromApexContentsTag interface {
	blueprint.DependencyTag

	// Method that differentiates this interface from others.
	ExcludeFromApexContents()
}

// Interface that identifies dependencies to skip Apex dependency check
type SkipApexAllowedDependenciesCheck interface {
	// Returns true to skip the Apex dependency check, which limits the allowed dependency in build.
	SkipApexAllowedDependenciesCheck() bool
}

// ApexModuleBase provides the default implementation for the ApexModule interface. APEX-aware
// modules are expected to include this struct and call InitApexModule().
type ApexModuleBase struct {
	ApexProperties     ApexProperties
	apexPropertiesLock sync.Mutex // protects ApexProperties during parallel apexDirectlyInAnyMutator

	canHaveApexVariants bool

	apexInfos     []ApexInfo
	apexInfosLock sync.Mutex // protects apexInfos during parallel apexInfoMutator
}

func (m *ApexModuleBase) ApexTransitionMutatorSplit(ctx BaseModuleContext) []ApexInfo {
	return []ApexInfo{{}}
}

func (m *ApexModuleBase) ApexTransitionMutatorOutgoing(ctx OutgoingTransitionContext, info ApexInfo) ApexInfo {
	if !ctx.Module().(ApexModule).GetDepInSameApexChecker().OutgoingDepIsInSameApex(ctx.DepTag()) {
		return ApexInfo{}
	}
	return info
}

func (m *ApexModuleBase) ApexTransitionMutatorIncoming(ctx IncomingTransitionContext, info ApexInfo) ApexInfo {
	module := ctx.Module().(ApexModule)
	if !module.CanHaveApexVariants() {
		return ApexInfo{}
	}

	if !ctx.Module().(ApexModule).GetDepInSameApexChecker().IncomingDepIsInSameApex(ctx.DepTag()) {
		return ApexInfo{}
	}

	if info.ApexVariationName == "" {
		return ApexInfo{}
	}

	if !ctx.Module().(ApexModule).UniqueApexVariations() && !m.ApexProperties.UniqueApexVariationsForDeps && !info.ForPrebuiltApex {
		return info.Minimize()
	}
	return info
}

func (m *ApexModuleBase) ApexTransitionMutatorMutate(ctx BottomUpMutatorContext, info ApexInfo) {
	SetProvider(ctx, ApexInfoProvider, info)

	module := ctx.Module().(ApexModule)
	base := module.apexModuleBase()

	platformVariation := info.ApexVariationName == ""
	if !platformVariation {
		// Do some validity checks.
		// TODO(jiyong): is this the right place?
		base.checkApexAvailableProperty(ctx)

		SetProvider(ctx, ApexAvailableInfoProvider, ApexAvailableInfo{
			ApexAvailableFor: module.ApexAvailableFor(),
		})
	}
	if platformVariation && !ctx.Host() && !module.AvailableFor(AvailableToPlatform) && module.NotAvailableForPlatform() {
		// Do not install the module for platform, but still allow it to output
		// uninstallable AndroidMk entries in certain cases when they have side
		// effects.  TODO(jiyong): move this routine to somewhere else
		module.MakeUninstallable()
	}
}

// Initializes ApexModuleBase struct. Not calling this (even when inheriting from ApexModuleBase)
// prevents the module from being mutated for apexBundle.
func InitApexModule(m ApexModule) {
	base := m.apexModuleBase()
	base.canHaveApexVariants = true

	m.AddProperties(&base.ApexProperties)
}

// Implements ApexModule
func (m *ApexModuleBase) apexModuleBase() *ApexModuleBase {
	return m
}

var (
	availableToPlatformList = []string{AvailableToPlatform}
)

// Implements ApexModule
func (m *ApexModuleBase) ApexAvailable() []string {
	aa := m.ApexProperties.Apex_available
	if len(aa) > 0 {
		return aa
	}
	// Default is availability to platform
	return CopyOf(availableToPlatformList)
}

func (m *ApexModuleBase) ApexAvailableFor() []string {
	return m.ApexAvailable()
}

// Implements ApexModule
func (m *ApexModuleBase) BuildForApex(apex ApexInfo) {
	m.apexInfosLock.Lock()
	defer m.apexInfosLock.Unlock()
	if slices.ContainsFunc(m.apexInfos, func(existing ApexInfo) bool {
		return existing.ApexVariationName == apex.ApexVariationName
	}) {
		return
	}
	m.apexInfos = append(m.apexInfos, apex)
}

// Implements ApexModule
func (m *ApexModuleBase) InAnyApex() bool {
	for _, apex_name := range m.ApexProperties.Apex_available {
		if apex_name != AvailableToPlatform {
			return true
		}
	}
	return false
}

// Implements ApexModule
func (m *ApexModuleBase) NotInPlatform() bool {
	return !m.AvailableFor(AvailableToPlatform)
}

// Implements ApexModule
func (m *ApexModuleBase) CanHaveApexVariants() bool {
	return m.canHaveApexVariants
}

// Implements ApexModule
func (m *ApexModuleBase) IsInstallableToApex() bool {
	// If needed, this will bel overridden by concrete types inheriting
	// ApexModuleBase
	return false
}

// Implements ApexModule
func (m *ApexModuleBase) UniqueApexVariations() bool {
	// If needed, this will bel overridden by concrete types inheriting
	// ApexModuleBase
	return false
}

// Implements ApexModule
func (m *ApexModuleBase) GetDepInSameApexChecker() DepInSameApexChecker {
	return BaseDepInSameApexChecker{}
}

type BaseDepInSameApexChecker struct{}

func (m BaseDepInSameApexChecker) OutgoingDepIsInSameApex(tag blueprint.DependencyTag) bool {
	return true
}

func (m BaseDepInSameApexChecker) IncomingDepIsInSameApex(tag blueprint.DependencyTag) bool {
	return true
}

const (
	AvailableToPlatform = "//apex_available:platform"
	AvailableToAnyApex  = "//apex_available:anyapex"
)

// CheckAvailableForApex provides the default algorithm for checking the apex availability. When the
// availability is empty, it defaults to ["//apex_available:platform"] which means "available to the
// platform but not available to any APEX". When the list is not empty, `what` is matched against
// the list. If there is any matching element in the list, thus function returns true. The special
// availability "//apex_available:anyapex" matches with anything except for
// "//apex_available:platform".
func CheckAvailableForApex(what string, apex_available []string) bool {
	if len(apex_available) == 0 {
		return what == AvailableToPlatform
	}

	// TODO b/248601389
	if what == "com.google.mainline.primary.libs" || what == "com.google.mainline.go.primary.libs" {
		return true
	}

	for _, apex_name := range apex_available {
		// exact match.
		if apex_name == what {
			return true
		}
		// //apex_available:anyapex matches with any apex name, but not //apex_available:platform
		if apex_name == AvailableToAnyApex && what != AvailableToPlatform {
			return true
		}
		// prefix match.
		if strings.HasSuffix(apex_name, ".*") && strings.HasPrefix(what, strings.TrimSuffix(apex_name, "*")) {
			return true
		}
		// TODO b/383863941: Remove once legacy name is no longer used
		if (apex_name == "com.android.btservices" && what == "com.android.bt") || (apex_name == "com.android.bt" && what == "com.android.btservices") {
			return true
		}
	}
	return false
}

// Implements ApexModule
func (m *ApexModuleBase) AvailableFor(what string) bool {
	return CheckAvailableForApex(what, m.ApexAvailableFor())
}

// Implements ApexModule
func (m *ApexModuleBase) AlwaysRequiresPlatformApexVariant() bool {
	return false
}

// Implements ApexModule
func (m *ApexModuleBase) NotAvailableForPlatform() bool {
	return m.ApexProperties.NotAvailableForPlatform
}

// Implements ApexModule
func (m *ApexModuleBase) SetNotAvailableForPlatform() {
	m.ApexProperties.NotAvailableForPlatform = true
}

// This function makes sure that the apex_available property is valid
func (m *ApexModuleBase) checkApexAvailableProperty(mctx BaseModuleContext) {
	for _, n := range m.ApexProperties.Apex_available {
		if n == AvailableToPlatform || n == AvailableToAnyApex {
			continue
		}
		// Prefix pattern should end with .* and has at least two components.
		if strings.Contains(n, "*") {
			if !strings.HasSuffix(n, ".*") {
				mctx.PropertyErrorf("apex_available", "Wildcard should end with .* like com.foo.*")
			}
			if strings.Count(n, ".") < 2 {
				mctx.PropertyErrorf("apex_available", "Wildcard requires two or more components like com.foo.*")
			}
			if strings.Count(n, "*") != 1 {
				mctx.PropertyErrorf("apex_available", "Wildcard is not allowed in the middle.")
			}
			continue
		}
		if !mctx.OtherModuleExists(n) && !mctx.Config().AllowMissingDependencies() {
			mctx.PropertyErrorf("apex_available", "%q is not a valid module name", n)
		}
	}
}

// AvailableToSameApexes returns true if the two modules are apex_available to
// exactly the same set of APEXes (and platform), i.e. if their apex_available
// properties have the same elements.
func AvailableToSameApexes(mod1, mod2 ApexModule) bool {
	mod1ApexAvail := SortedUniqueStrings(mod1.apexModuleBase().ApexProperties.Apex_available)
	mod2ApexAvail := SortedUniqueStrings(mod2.apexModuleBase().ApexProperties.Apex_available)
	if len(mod1ApexAvail) != len(mod2ApexAvail) {
		return false
	}
	for i, v := range mod1ApexAvail {
		if v != mod2ApexAvail[i] {
			return false
		}
	}
	return true
}

// UpdateUniqueApexVariationsForDeps sets UniqueApexVariationsForDeps if any dependencies that are
// in the same APEX have unique APEX variations so that the module can link against the right
// variant.
func UpdateUniqueApexVariationsForDeps(mctx BottomUpMutatorContext, am ApexModule) {
	// If any of the dependencies requires unique apex variations, so does this module.
	mctx.VisitDirectDeps(func(dep Module) {
		if depApexModule, ok := dep.(ApexModule); ok {
			if IsDepInSameApex(mctx, am, depApexModule) &&
				(depApexModule.UniqueApexVariations() ||
					depApexModule.apexModuleBase().ApexProperties.UniqueApexVariationsForDeps) {
				am.apexModuleBase().ApexProperties.UniqueApexVariationsForDeps = true
			}
		}
	})
}

////////////////////////////////////////////////////////////////////////////////////////////////////
//Below are routines for extra safety checks.
//
// BuildDepsInfoLists is to flatten the dependency graph for an apexBundle into a text file
// (actually two in slightly different formats). The files are mostly for debugging, for example to
// see why a certain module is included in an APEX via which dependency path.
//
// CheckMinSdkVersion is to make sure that all modules in an apexBundle satisfy the min_sdk_version
// requirement of the apexBundle.

// A dependency info for a single ApexModule, either direct or transitive.
type ApexModuleDepInfo struct {
	// Name of the dependency
	To string
	// List of dependencies To belongs to. Includes APEX itself, if a direct dependency.
	From []string
	// Whether the dependency belongs to the final compiled APEX.
	IsExternal bool
	// min_sdk_version of the ApexModule
	MinSdkVersion string
}

// A map of a dependency name to its ApexModuleDepInfo
type DepNameToDepInfoMap map[string]ApexModuleDepInfo

type ApexBundleDepsInfo struct {
	flatListPath Path
	fullListPath Path
}

type ApexBundleDepsInfoIntf interface {
	Updatable() bool
	FlatListPath() Path
	FullListPath() Path
}

type ApexBundleDepsData struct {
	Updatable    bool
	FlatListPath Path
}

var ApexBundleDepsDataProvider = blueprint.NewProvider[ApexBundleDepsData]()

func (d *ApexBundleDepsInfo) FlatListPath() Path {
	return d.flatListPath
}

func (d *ApexBundleDepsInfo) FullListPath() Path {
	return d.fullListPath
}

// Generate two module out files:
// 1. FullList with transitive deps and their parents in the dep graph
// 2. FlatList with a flat list of transitive deps
// In both cases transitive deps of external deps are not included. Neither are deps that are only
// available to APEXes; they are developed with updatability in mind and don't need manual approval.
func (d *ApexBundleDepsInfo) BuildDepsInfoLists(ctx ModuleContext, minSdkVersion string, depInfos DepNameToDepInfoMap) {
	var fullContent strings.Builder
	var flatContent strings.Builder

	fmt.Fprintf(&fullContent, "%s(minSdkVersion:%s):\n", ctx.ModuleName(), minSdkVersion)
	for _, key := range FirstUniqueStrings(SortedKeys(depInfos)) {
		info := depInfos[key]
		toName := fmt.Sprintf("%s(minSdkVersion:%s)", info.To, info.MinSdkVersion)
		if info.IsExternal {
			toName = toName + " (external)"
		}
		fmt.Fprintf(&fullContent, "  %s <- %s\n", toName, strings.Join(SortedUniqueStrings(info.From), ", "))
		fmt.Fprintf(&flatContent, "%s\n", toName)
	}

	fullListPath := PathForModuleOut(ctx, "depsinfo", "fulllist.txt")
	WriteFileRule(ctx, fullListPath, fullContent.String())
	d.fullListPath = fullListPath

	flatListPath := PathForModuleOut(ctx, "depsinfo", "flatlist.txt")
	WriteFileRule(ctx, flatListPath, flatContent.String())
	d.flatListPath = flatListPath

	ctx.Phony(fmt.Sprintf("%s-depsinfo", ctx.ModuleName()), fullListPath, flatListPath)
}

// Function called while walking an APEX's payload dependencies.
//
// Return true if the `to` module should be visited, false otherwise.
type PayloadDepsCallback func(ctx BaseModuleContext, from, to ModuleProxy, externalDep bool) bool
type WalkPayloadDepsFunc func(ctx BaseModuleContext, do PayloadDepsCallback)

// ModuleWithMinSdkVersionCheck represents a module that implements min_sdk_version checks
type ModuleWithMinSdkVersionCheck interface {
	Module
	MinSdkVersion(ctx EarlyModuleContext) ApiLevel
	CheckMinSdkVersion(ctx ModuleContext)
}

// CheckMinSdkVersion checks if every dependency of an updatable module sets min_sdk_version
// accordingly
func CheckMinSdkVersion(ctx ModuleContext, minSdkVersion ApiLevel, walk WalkPayloadDepsFunc) {
	// do not enforce min_sdk_version for host
	if ctx.Host() {
		return
	}

	// do not enforce for coverage build
	if ctx.Config().IsEnvTrue("EMMA_INSTRUMENT") || ctx.DeviceConfig().NativeCoverageEnabled() || ctx.DeviceConfig().ClangCoverageEnabled() {
		return
	}

	// do not enforce deps.min_sdk_version if APEX/APK doesn't set min_sdk_version
	if minSdkVersion.IsNone() {
		return
	}

	walk(ctx, func(ctx BaseModuleContext, from, to ModuleProxy, externalDep bool) bool {
		if externalDep {
			// external deps are outside the payload boundary, which is "stable"
			// interface. We don't have to check min_sdk_version for external
			// dependencies.
			return false
		}
		if !IsDepInSameApex(ctx, from, to) {
			return false
		}
		if info, ok := OtherModuleProvider(ctx, to, CommonModuleInfoProvider); ok && info.ModuleWithMinSdkVersionCheck {
			if info.MinSdkVersion.ApiLevel == nil || !info.MinSdkVersion.ApiLevel.Specified() {
				// This dependency performs its own min_sdk_version check, just make sure it sets min_sdk_version
				// to trigger the check.
				ctx.OtherModuleErrorf(to, "must set min_sdk_version")
			}
			return false
		}
		if err := ShouldSupportSdkVersion(ctx, to, minSdkVersion); err != nil {
			ctx.OtherModuleErrorf(to, "should support min_sdk_version(%v) for %q: %v."+
				"\n\nDependency path: %s\n\n"+
				"Consider adding 'min_sdk_version: %q' to %q",
				minSdkVersion, ctx.ModuleName(), err.Error(),
				ctx.GetPathString(false),
				minSdkVersion, ctx.OtherModuleName(to))
			return false
		}
		return true
	})
}

type MinSdkVersionFromValueContext interface {
	Config() Config
	DeviceConfig() DeviceConfig
	ModuleErrorContext
}

// Returns nil (success) if this module should support the given sdk version. Returns an
// error if not. No default implementation is provided for this method. A module type
// implementing this interface should provide an implementation. A module supports an sdk
// version when the module's min_sdk_version is equal to or less than the given sdk version.
func ShouldSupportSdkVersion(ctx BaseModuleContext, module Module, sdkVersion ApiLevel) error {
	info, ok := OtherModuleProvider(ctx, module, CommonModuleInfoProvider)
	if !ok || info.MinSdkVersionSupported.IsNone() {
		return fmt.Errorf("min_sdk_version is not specified")
	}
	minVer := info.MinSdkVersionSupported

	if minVer.GreaterThan(sdkVersion) {
		return fmt.Errorf("newer SDK(%v)", minVer)
	}

	return nil
}

// Construct ApiLevel object from min_sdk_version string value
func MinSdkVersionFromValue(ctx MinSdkVersionFromValueContext, value string) ApiLevel {
	if value == "" {
		return NoneApiLevel
	}
	apiLevel, err := ApiLevelFromUser(ctx, value)
	if err != nil {
		ctx.PropertyErrorf("min_sdk_version", "%s", err.Error())
		return NoneApiLevel
	}
	return apiLevel
}

var ApexExportsInfoProvider = blueprint.NewProvider[ApexExportsInfo]()

// ApexExportsInfo contains information about the artifacts provided by apexes to dexpreopt and hiddenapi
type ApexExportsInfo struct {
	// Canonical name of this APEX. Used to determine the path to the activated APEX on
	// device (/apex/<apex_name>)
	ApexName string

	// Path to the image profile file on host (or empty, if profile is not generated).
	ProfilePathOnHost Path

	// Map from the apex library name (without prebuilt_ prefix) to the dex file path on host
	LibraryNameToDexJarPathOnHost map[string]Path
}

var PrebuiltInfoProvider = blueprint.NewProvider[PrebuiltInfo]()

// contents of prebuilt_info.json
type PrebuiltInfo struct {
	// Name of the apex, without the prebuilt_ prefix
	Name string

	Is_prebuilt bool

	// This is relative to root of the workspace.
	// In case of mainline modules, this file contains the build_id that was used
	// to generate the mainline module prebuilt.
	Prebuilt_info_file_path string `json:",omitempty"`
}

// FragmentInApexTag is embedded into a dependency tag to allow apex modules to annotate
// their fragments in a way that allows the java bootclasspath modules to traverse from
// the apex to the fragment.
type FragmentInApexTag struct{}

func (FragmentInApexTag) isFragmentInApexTag() {}

type isFragmentInApexTagIntf interface {
	isFragmentInApexTag()
}

// IsFragmentInApexTag returns true if the dependency tag embeds FragmentInApexTag,
// signifying that it is a dependency from an apex module to its fragment.
func IsFragmentInApexTag(tag blueprint.DependencyTag) bool {
	_, ok := tag.(isFragmentInApexTagIntf)
	return ok
}
