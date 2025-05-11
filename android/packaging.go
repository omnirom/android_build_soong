// Copyright 2020 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License")
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
	"path/filepath"
	"sort"

	"github.com/google/blueprint"
	"github.com/google/blueprint/gobtools"
	"github.com/google/blueprint/proptools"
)

// PackagingSpec abstracts a request to place a built artifact at a certain path in a package. A
// package can be the traditional <partition>.img, but isn't limited to those. Other examples could
// be a new filesystem image that is a subset of system.img (e.g. for an Android-like mini OS
// running on a VM), or a zip archive for some of the host tools.
type PackagingSpec struct {
	// Path relative to the root of the package
	relPathInPackage string

	// The path to the built artifact
	srcPath Path

	// If this is not empty, then relPathInPackage should be a symlink to this target. (Then
	// srcPath is of course ignored.)
	symlinkTarget string

	// Whether relPathInPackage should be marked as executable or not
	executable bool

	effectiveLicenseFiles *Paths

	partition string

	// Whether this packaging spec represents an installation of the srcPath (i.e. this struct
	// is created via InstallFile or InstallSymlink) or a simple packaging (i.e. created via
	// PackageFile).
	skipInstall bool

	// Paths of aconfig files for the built artifact
	aconfigPaths *Paths

	// ArchType of the module which produced this packaging spec
	archType ArchType

	// List of module names that this packaging spec overrides
	overrides *[]string

	// Name of the module where this packaging spec is output of
	owner string
}

type packagingSpecGob struct {
	RelPathInPackage      string
	SrcPath               Path
	SymlinkTarget         string
	Executable            bool
	EffectiveLicenseFiles *Paths
	Partition             string
	SkipInstall           bool
	AconfigPaths          *Paths
	ArchType              ArchType
	Overrides             *[]string
	Owner                 string
}

func (p *PackagingSpec) ToGob() *packagingSpecGob {
	return &packagingSpecGob{
		RelPathInPackage:      p.relPathInPackage,
		SrcPath:               p.srcPath,
		SymlinkTarget:         p.symlinkTarget,
		Executable:            p.executable,
		EffectiveLicenseFiles: p.effectiveLicenseFiles,
		Partition:             p.partition,
		SkipInstall:           p.skipInstall,
		AconfigPaths:          p.aconfigPaths,
		ArchType:              p.archType,
		Overrides:             p.overrides,
		Owner:                 p.owner,
	}
}

func (p *PackagingSpec) FromGob(data *packagingSpecGob) {
	p.relPathInPackage = data.RelPathInPackage
	p.srcPath = data.SrcPath
	p.symlinkTarget = data.SymlinkTarget
	p.executable = data.Executable
	p.effectiveLicenseFiles = data.EffectiveLicenseFiles
	p.partition = data.Partition
	p.skipInstall = data.SkipInstall
	p.aconfigPaths = data.AconfigPaths
	p.archType = data.ArchType
	p.overrides = data.Overrides
	p.owner = data.Owner
}

func (p *PackagingSpec) GobEncode() ([]byte, error) {
	return gobtools.CustomGobEncode[packagingSpecGob](p)
}

func (p *PackagingSpec) GobDecode(data []byte) error {
	return gobtools.CustomGobDecode[packagingSpecGob](data, p)
}

func (p *PackagingSpec) Equals(other *PackagingSpec) bool {
	if other == nil {
		return false
	}
	if p.relPathInPackage != other.relPathInPackage {
		return false
	}
	if p.srcPath != other.srcPath || p.symlinkTarget != other.symlinkTarget {
		return false
	}
	if p.executable != other.executable {
		return false
	}
	if p.partition != other.partition {
		return false
	}
	return true
}

// Get file name of installed package
func (p *PackagingSpec) FileName() string {
	if p.relPathInPackage != "" {
		return filepath.Base(p.relPathInPackage)
	}

	return ""
}

// Path relative to the root of the package
func (p *PackagingSpec) RelPathInPackage() string {
	return p.relPathInPackage
}

func (p *PackagingSpec) SetRelPathInPackage(relPathInPackage string) {
	p.relPathInPackage = relPathInPackage
}

func (p *PackagingSpec) EffectiveLicenseFiles() Paths {
	if p.effectiveLicenseFiles == nil {
		return Paths{}
	}
	return *p.effectiveLicenseFiles
}

func (p *PackagingSpec) Partition() string {
	return p.partition
}

func (p *PackagingSpec) SetPartition(partition string) {
	p.partition = partition
}

func (p *PackagingSpec) SkipInstall() bool {
	return p.skipInstall
}

// Paths of aconfig files for the built artifact
func (p *PackagingSpec) GetAconfigPaths() Paths {
	return *p.aconfigPaths
}

type PackageModule interface {
	Module
	packagingBase() *PackagingBase

	// AddDeps adds dependencies to the `deps` modules. This should be called in DepsMutator.
	// When adding the dependencies, depTag is used as the tag. If `deps` modules are meant to
	// be copied to a zip in CopyDepsToZip, `depTag` should implement PackagingItem marker interface.
	AddDeps(ctx BottomUpMutatorContext, depTag blueprint.DependencyTag)

	// GatherPackagingSpecs gathers PackagingSpecs of transitive dependencies.
	GatherPackagingSpecs(ctx ModuleContext) map[string]PackagingSpec
	GatherPackagingSpecsWithFilter(ctx ModuleContext, filter func(PackagingSpec) bool) map[string]PackagingSpec
	GatherPackagingSpecsWithFilterAndModifier(ctx ModuleContext, filter func(PackagingSpec) bool, modifier func(*PackagingSpec)) map[string]PackagingSpec

	// CopyDepsToZip zips the built artifacts of the dependencies into the given zip file and
	// returns zip entries in it. This is expected to be called in GenerateAndroidBuildActions,
	// followed by a build rule that unzips it and creates the final output (img, zip, tar.gz,
	// etc.) from the extracted files
	CopyDepsToZip(ctx ModuleContext, specs map[string]PackagingSpec, zipOut WritablePath) []string
}

// PackagingBase provides basic functionality for packaging dependencies. A module is expected to
// include this struct and call InitPackageModule.
type PackagingBase struct {
	properties PackagingProperties

	// Allows this module to skip missing dependencies. In most cases, this is not required, but
	// for rare cases like when there's a dependency to a module which exists in certain repo
	// checkouts, this is needed.
	IgnoreMissingDependencies bool

	// If this is set to true by a module type inheriting PackagingBase, the deps property
	// collects the first target only even with compile_multilib: true.
	DepsCollectFirstTargetOnly bool

	// If this is set to try by a module type inheriting PackagingBase, the module type is
	// allowed to utilize High_priority_deps.
	AllowHighPriorityDeps bool
}

type DepsProperty struct {
	// Deps that have higher priority in packaging when there is a packaging conflict.
	// For example, if multiple files are being installed to same filepath, the install file
	// of the module listed in this property will have a higher priority over those in other
	// deps properties.
	High_priority_deps []string `android:"arch_variant"`

	// Modules to include in this package
	Deps proptools.Configurable[[]string] `android:"arch_variant"`
}

type packagingMultilibProperties struct {
	First    DepsProperty `android:"arch_variant"`
	Common   DepsProperty `android:"arch_variant"`
	Lib32    DepsProperty `android:"arch_variant"`
	Lib64    DepsProperty `android:"arch_variant"`
	Both     DepsProperty `android:"arch_variant"`
	Prefer32 DepsProperty `android:"arch_variant"`
}

type packagingArchProperties struct {
	Arm64  DepsProperty
	Arm    DepsProperty
	X86_64 DepsProperty
	X86    DepsProperty
}

type PackagingProperties struct {
	DepsProperty

	Multilib packagingMultilibProperties `android:"arch_variant"`
	Arch     packagingArchProperties
}

func InitPackageModule(p PackageModule) {
	base := p.packagingBase()
	p.AddProperties(&base.properties, &base.properties.DepsProperty)
}

func (p *PackagingBase) packagingBase() *PackagingBase {
	return p
}

// From deps and multilib.*.deps, select the dependencies that are for the given arch deps is for
// the current archicture when this module is not configured for multi target. When configured for
// multi target, deps is selected for each of the targets and is NOT selected for the current
// architecture which would be Common.
// It returns two lists, the normal and high priority deps, respectively.
func (p *PackagingBase) getDepsForArch(ctx BaseModuleContext, arch ArchType) ([]string, []string) {
	var normalDeps []string
	var highPriorityDeps []string

	get := func(prop DepsProperty) {
		normalDeps = append(normalDeps, prop.Deps.GetOrDefault(ctx, nil)...)
		highPriorityDeps = append(highPriorityDeps, prop.High_priority_deps...)
	}
	has := func(prop DepsProperty) bool {
		return len(prop.Deps.GetOrDefault(ctx, nil)) > 0 || len(prop.High_priority_deps) > 0
	}

	if arch == ctx.Target().Arch.ArchType && len(ctx.MultiTargets()) == 0 {
		get(p.properties.DepsProperty)
	} else if arch.Multilib == "lib32" {
		get(p.properties.Multilib.Lib32)
		// multilib.prefer32.deps are added for lib32 only when they support 32-bit arch
		for _, dep := range p.properties.Multilib.Prefer32.Deps.GetOrDefault(ctx, nil) {
			if checkIfOtherModuleSupportsLib32(ctx, dep) {
				normalDeps = append(normalDeps, dep)
			}
		}
		for _, dep := range p.properties.Multilib.Prefer32.High_priority_deps {
			if checkIfOtherModuleSupportsLib32(ctx, dep) {
				highPriorityDeps = append(highPriorityDeps, dep)
			}
		}
	} else if arch.Multilib == "lib64" {
		get(p.properties.Multilib.Lib64)
		// multilib.prefer32.deps are added for lib64 only when they don't support 32-bit arch
		for _, dep := range p.properties.Multilib.Prefer32.Deps.GetOrDefault(ctx, nil) {
			if !checkIfOtherModuleSupportsLib32(ctx, dep) {
				normalDeps = append(normalDeps, dep)
			}
		}
		for _, dep := range p.properties.Multilib.Prefer32.High_priority_deps {
			if !checkIfOtherModuleSupportsLib32(ctx, dep) {
				highPriorityDeps = append(highPriorityDeps, dep)
			}
		}
	} else if arch == Common {
		get(p.properties.Multilib.Common)
	}

	if p.DepsCollectFirstTargetOnly {
		if has(p.properties.Multilib.First) {
			ctx.PropertyErrorf("multilib.first.deps", "not supported. use \"deps\" instead")
		}
		for i, t := range ctx.MultiTargets() {
			if t.Arch.ArchType == arch {
				get(p.properties.Multilib.Both)
				if i == 0 {
					get(p.properties.DepsProperty)
				}
			}
		}
	} else {
		if has(p.properties.Multilib.Both) {
			ctx.PropertyErrorf("multilib.both.deps", "not supported. use \"deps\" instead")
		}
		for i, t := range ctx.MultiTargets() {
			if t.Arch.ArchType == arch {
				get(p.properties.DepsProperty)
				if i == 0 {
					get(p.properties.Multilib.First)
				}
			}
		}
	}

	if ctx.Arch().ArchType == Common {
		switch arch {
		case Arm64:
			get(p.properties.Arch.Arm64)
		case Arm:
			get(p.properties.Arch.Arm)
		case X86_64:
			get(p.properties.Arch.X86_64)
		case X86:
			get(p.properties.Arch.X86)
		}
	}

	if len(highPriorityDeps) > 0 && !p.AllowHighPriorityDeps {
		ctx.ModuleErrorf("Usage of high_priority_deps is not allowed for %s module type", ctx.ModuleType())
	}

	return FirstUniqueStrings(normalDeps), FirstUniqueStrings(highPriorityDeps)
}

func getSupportedTargets(ctx BaseModuleContext) []Target {
	var ret []Target
	// The current and the common OS targets are always supported
	ret = append(ret, ctx.Target())
	if ctx.Arch().ArchType != Common {
		ret = append(ret, Target{Os: ctx.Os(), Arch: Arch{ArchType: Common}})
	}
	// If this module is configured for multi targets, those should be supported as well
	ret = append(ret, ctx.MultiTargets()...)
	return ret
}

// getLib32Target returns the 32-bit target from the list of targets this module supports. If this
// module doesn't support 32-bit target, nil is returned.
func getLib32Target(ctx BaseModuleContext) *Target {
	for _, t := range getSupportedTargets(ctx) {
		if t.Arch.ArchType.Multilib == "lib32" {
			return &t
		}
	}
	return nil
}

// checkIfOtherModuleSUpportsLib32 returns true if 32-bit variant of dep exists.
func checkIfOtherModuleSupportsLib32(ctx BaseModuleContext, dep string) bool {
	t := getLib32Target(ctx)
	if t == nil {
		// This packaging module doesn't support 32bit. No point of checking if dep supports 32-bit
		// or not.
		return false
	}
	return ctx.OtherModuleFarDependencyVariantExists(t.Variations(), dep)
}

// PackagingItem is a marker interface for dependency tags.
// Direct dependencies with a tag implementing PackagingItem are packaged in CopyDepsToZip().
type PackagingItem interface {
	// IsPackagingItem returns true if the dep is to be packaged
	IsPackagingItem() bool
}

var _ PackagingItem = (*PackagingItemAlwaysDepTag)(nil)

// DepTag provides default implementation of PackagingItem interface.
// PackagingBase-derived modules can define their own dependency tag by embedding this, which
// can be passed to AddDeps() or AddDependencies().
type PackagingItemAlwaysDepTag struct {
}

// IsPackagingItem returns true if the dep is to be packaged
func (PackagingItemAlwaysDepTag) IsPackagingItem() bool {
	return true
}

type highPriorityDepTag struct {
	blueprint.BaseDependencyTag
	PackagingItemAlwaysDepTag
}

// See PackageModule.AddDeps
func (p *PackagingBase) AddDeps(ctx BottomUpMutatorContext, depTag blueprint.DependencyTag) {
	addDep := func(t Target, dep string, highPriority bool) {
		if p.IgnoreMissingDependencies && !ctx.OtherModuleExists(dep) {
			return
		}
		targetVariation := t.Variations()
		sharedVariation := blueprint.Variation{
			Mutator:   "link",
			Variation: "shared",
		}
		// If a shared variation exists, use that. Static variants do not provide any standalone files
		// for packaging.
		if ctx.OtherModuleFarDependencyVariantExists([]blueprint.Variation{sharedVariation}, dep) {
			targetVariation = append(targetVariation, sharedVariation)
		}
		depTagToUse := depTag
		if highPriority {
			depTagToUse = highPriorityDepTag{}
		}

		ctx.AddFarVariationDependencies(targetVariation, depTagToUse, dep)
	}
	for _, t := range getSupportedTargets(ctx) {
		normalDeps, highPriorityDeps := p.getDepsForArch(ctx, t.Arch.ArchType)
		for _, dep := range normalDeps {
			addDep(t, dep, false)
		}
		for _, dep := range highPriorityDeps {
			addDep(t, dep, true)
		}
	}
}

// See PackageModule.GatherPackagingSpecs
func (p *PackagingBase) GatherPackagingSpecsWithFilterAndModifier(ctx ModuleContext, filter func(PackagingSpec) bool, modifier func(*PackagingSpec)) map[string]PackagingSpec {
	// packaging specs gathered from the dep that are not high priorities.
	var regularPriorities []PackagingSpec

	// all packaging specs gathered from the high priority deps.
	var highPriorities []PackagingSpec

	// Name of the dependency which requested the packaging spec.
	// If this dep is overridden, the packaging spec will not be installed via this dependency chain.
	// (the packaging spec might still be installed if there are some other deps which depend on it).
	var depNames []string

	// list of module names overridden
	var overridden []string

	var arches []ArchType
	for _, target := range getSupportedTargets(ctx) {
		arches = append(arches, target.Arch.ArchType)
	}

	// filter out packaging specs for unsupported architecture
	filterArch := func(ps PackagingSpec) bool {
		for _, arch := range arches {
			if arch == ps.archType {
				return true
			}
		}
		return false
	}

	ctx.VisitDirectDepsProxy(func(child ModuleProxy) {
		depTag := ctx.OtherModuleDependencyTag(child)
		if pi, ok := depTag.(PackagingItem); !ok || !pi.IsPackagingItem() {
			return
		}
		for _, ps := range OtherModuleProviderOrDefault(
			ctx, child, InstallFilesProvider).TransitivePackagingSpecs.ToList() {
			if !filterArch(ps) {
				continue
			}

			if filter != nil {
				if !filter(ps) {
					continue
				}
			}

			if modifier != nil {
				modifier(&ps)
			}

			if _, ok := depTag.(highPriorityDepTag); ok {
				highPriorities = append(highPriorities, ps)
			} else {
				regularPriorities = append(regularPriorities, ps)
			}

			depNames = append(depNames, child.Name())
			if ps.overrides != nil {
				overridden = append(overridden, *ps.overrides...)
			}
		}
	})

	filterOverridden := func(input []PackagingSpec) []PackagingSpec {
		// input minus packaging specs that are overridden
		var filtered []PackagingSpec
		for index, ps := range input {
			if ps.owner != "" && InList(ps.owner, overridden) {
				continue
			}
			// The dependency which requested this packaging spec has been overridden.
			if InList(depNames[index], overridden) {
				continue
			}
			filtered = append(filtered, ps)
		}
		return filtered
	}

	filteredRegularPriority := filterOverridden(regularPriorities)

	m := make(map[string]PackagingSpec)
	for _, ps := range filteredRegularPriority {
		dstPath := ps.relPathInPackage
		if existingPs, ok := m[dstPath]; ok {
			if !existingPs.Equals(&ps) {
				ctx.ModuleErrorf("packaging conflict at %v:\n%v\n%v", dstPath, existingPs, ps)
			}
			continue
		}
		m[dstPath] = ps
	}

	filteredHighPriority := filterOverridden(highPriorities)
	highPriorityPs := make(map[string]PackagingSpec)
	for _, ps := range filteredHighPriority {
		dstPath := ps.relPathInPackage
		if existingPs, ok := highPriorityPs[dstPath]; ok {
			if !existingPs.Equals(&ps) {
				ctx.ModuleErrorf("packaging conflict at %v:\n%v\n%v", dstPath, existingPs, ps)
			}
			continue
		}
		highPriorityPs[dstPath] = ps
		m[dstPath] = ps
	}

	return m
}

// See PackageModule.GatherPackagingSpecs
func (p *PackagingBase) GatherPackagingSpecsWithFilter(ctx ModuleContext, filter func(PackagingSpec) bool) map[string]PackagingSpec {
	return p.GatherPackagingSpecsWithFilterAndModifier(ctx, filter, nil)
}

// See PackageModule.GatherPackagingSpecs
func (p *PackagingBase) GatherPackagingSpecs(ctx ModuleContext) map[string]PackagingSpec {
	return p.GatherPackagingSpecsWithFilter(ctx, nil)
}

// CopySpecsToDir is a helper that will add commands to the rule builder to copy the PackagingSpec
// entries into the specified directory.
func (p *PackagingBase) CopySpecsToDir(ctx ModuleContext, builder *RuleBuilder, specs map[string]PackagingSpec, dir WritablePath) (entries []string) {
	dirsToSpecs := make(map[WritablePath]map[string]PackagingSpec)
	dirsToSpecs[dir] = specs
	return p.CopySpecsToDirs(ctx, builder, dirsToSpecs)
}

// CopySpecsToDirs is a helper that will add commands to the rule builder to copy the PackagingSpec
// entries into corresponding directories.
func (p *PackagingBase) CopySpecsToDirs(ctx ModuleContext, builder *RuleBuilder, dirsToSpecs map[WritablePath]map[string]PackagingSpec) (entries []string) {
	empty := true
	for _, specs := range dirsToSpecs {
		if len(specs) > 0 {
			empty = false
			break
		}
	}
	if empty {
		return entries
	}

	seenDir := make(map[string]bool)

	dirs := make([]WritablePath, 0, len(dirsToSpecs))
	for dir, _ := range dirsToSpecs {
		dirs = append(dirs, dir)
	}
	sort.Slice(dirs, func(i, j int) bool {
		return dirs[i].String() < dirs[j].String()
	})

	for _, dir := range dirs {
		specs := dirsToSpecs[dir]
		for _, k := range SortedKeys(specs) {
			ps := specs[k]
			destPath := filepath.Join(dir.String(), ps.relPathInPackage)
			destDir := filepath.Dir(destPath)
			entries = append(entries, ps.relPathInPackage)
			if _, ok := seenDir[destDir]; !ok {
				seenDir[destDir] = true
				builder.Command().Textf("mkdir -p %s", destDir)
			}
			if ps.symlinkTarget == "" {
				builder.Command().Text("cp").Input(ps.srcPath).Text(destPath)
			} else {
				builder.Command().Textf("ln -sf %s %s", ps.symlinkTarget, destPath)
			}
			if ps.executable {
				builder.Command().Textf("chmod a+x %s", destPath)
			}
		}
	}

	return entries
}

// See PackageModule.CopyDepsToZip
func (p *PackagingBase) CopyDepsToZip(ctx ModuleContext, specs map[string]PackagingSpec, zipOut WritablePath) (entries []string) {
	builder := NewRuleBuilder(pctx, ctx)

	dir := PathForModuleOut(ctx, ".zip")
	builder.Command().Text("rm").Flag("-rf").Text(dir.String())
	builder.Command().Text("mkdir").Flag("-p").Text(dir.String())
	entries = p.CopySpecsToDir(ctx, builder, specs, dir)

	builder.Command().
		BuiltTool("soong_zip").
		FlagWithOutput("-o ", zipOut).
		FlagWithArg("-C ", dir.String()).
		Flag("-L 0"). // no compression because this will be unzipped soon
		FlagWithArg("-D ", dir.String())
	builder.Command().Text("rm").Flag("-rf").Text(dir.String())

	builder.Build("zip_deps", fmt.Sprintf("Zipping deps for %s", ctx.ModuleName()))
	return entries
}
