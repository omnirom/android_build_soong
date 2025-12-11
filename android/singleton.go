// Copyright 2017 Google Inc. All rights reserved.
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
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/google/blueprint"
)

// SingletonContext
type SingletonContext interface {
	blueprintSingletonContext() blueprint.SingletonContext

	Config() Config
	DeviceConfig() DeviceConfig

	ModuleName(module ModuleOrProxy) string
	ModuleDir(module ModuleOrProxy) string
	ModuleSubDir(module ModuleOrProxy) string
	ModuleType(module ModuleOrProxy) string
	BlueprintFile(module ModuleOrProxy) string

	// ModuleVariantsFromName returns the list of module variants named `name` in the same namespace as `referer` enforcing visibility rules.
	// Allows generating build actions for `referer` based on the metadata for `name` deferred until the singleton context.
	ModuleVariantsFromName(referer ModuleProxy, name string) []ModuleProxy

	otherModuleProvider(module ModuleOrProxy, provider blueprint.AnyProviderKey) (any, bool)

	// setSingletonProvider sets the value for a provider for the current singleton.
	// It panics if not called during the appropriate GenerateBuildActions pass for
	// the provider, if the provider is not registered as a singleton provider, if the value
	// is not of the appropriate type, or if the value has already been set. The value should not
	// be modified after being passed to setSingletonProvider.
	//
	// This method shouldn't be used directly, prefer the type-safe android.SetProvider instead.
	setSingletonProvider(provider blueprint.AnyProviderKey, value any)

	// otherSingletonProvider returns the value, if any, for the provider for a singleton. If the value for the
	// provider was not set it returns the zero value of the type of the provider, which means the
	// return value can always be type-asserted to the type of the provider. The return value should
	// always be considered read-only. It panics if called before the appropriate
	// GenerateBuildActions completes for the provider on the singleton, or if the provider is not
	// registered as a singleton provider.
	otherSingletonProvider(singleton blueprint.SingletonProxy, provider blueprint.AnyProviderKey) (any, bool)

	ModuleErrorf(module ModuleOrProxy, format string, args ...interface{})
	Errorf(format string, args ...interface{})
	Failed() bool

	Variable(pctx PackageContext, name, value string)
	Rule(pctx PackageContext, name string, params blueprint.RuleParams, argNames ...string) blueprint.Rule
	Build(pctx PackageContext, params BuildParams)

	// Phony creates a Make-style phony rule, a rule with no commands that can depend on other
	// phony rules or real files.  Phony can be called on the same name multiple times to add
	// additional dependencies.
	Phony(name string, deps ...Path)

	RequireNinjaVersion(major, minor, micro int)

	// SetOutDir sets the value of the top-level "builddir" Ninja variable
	// that controls where Ninja stores its build log files.  This value can be
	// set at most one time for a single build, later calls are ignored.
	SetOutDir(pctx PackageContext, value string)

	// Eval takes a string with embedded ninja variables, and returns a string
	// with all of the variables recursively expanded. Any variables references
	// are expanded in the scope of the PackageContext.
	Eval(pctx PackageContext, ninjaStr string) (string, error)

	VisitAllModulesBlueprint(visit func(blueprint.Module))
	VisitAllModules(visit func(Module))
	VisitAllModuleProxies(visit func(proxy ModuleProxy))
	VisitAllModulesIf(pred func(Module) bool, visit func(Module))
	VisitAllModulesOrProxies(visit func(ModuleOrProxy))

	VisitDirectDeps(module Module, visit func(Module))
	VisitDirectDepsIf(module Module, pred func(Module) bool, visit func(Module))

	VisitDirectDepsProxies(module ModuleProxy, visit func(ModuleProxy))

	VisitAllModuleVariants(module Module, visit func(Module))

	VisitAllModuleVariantProxies(module ModuleProxy, visit func(proxy ModuleProxy))

	VisitAllSingletons(visit func(singleton blueprint.SingletonProxy))

	PrimaryModule(module Module) Module

	PrimaryModuleProxy(module ModuleProxy) ModuleProxy

	IsPrimaryModule(module ModuleOrProxy) bool
	IsFinalModule(module ModuleOrProxy) bool

	AddNinjaFileDeps(deps ...string)

	// GlobWithDeps returns a list of files that match the specified pattern but do not match any
	// of the patterns in excludes.  It also adds efficient dependencies to rerun the primary
	// builder whenever a file matching the pattern as added or removed, without rerunning if a
	// file that does not match the pattern is added to a searched directory.
	GlobWithDeps(pattern string, excludes []string) ([]string, error)

	// OtherModulePropertyErrorf reports an error on the line number of the given property of the given module
	OtherModulePropertyErrorf(module ModuleOrProxy, property string, format string, args ...interface{})

	// HasMutatorFinished returns true if the given mutator has finished running.
	// It will panic if given an invalid mutator name.
	HasMutatorFinished(mutatorName string) bool

	// DistForGoals creates a rule to copy one or more Paths to the artifacts
	// directory on the build server when any of the specified goals are built.
	DistForGoal(goal string, paths ...Path)

	// DistForGoalWithFilename creates a rule to copy a Path to the artifacts
	// directory on the build server with the given filename when the specified
	// goal is built.
	DistForGoalWithFilename(goal string, path Path, filename string)

	// DistForGoalWithFilenameTag creates a rule to copy a Path to the artifacts
	// directory on the build server with the given filename appended with the
	// `-FILE_NAME_TAG_PLACEHOLDER` suffix when the specified goal is built.
	DistForGoalWithFilenameTag(goal string, path Path, filename string)

	// DistForGoals creates a rule to copy one or more Paths to the artifacts
	// directory on the build server when any of the specified goals are built.
	DistForGoals(goals []string, paths ...Path)

	// DistForGoalsWithFilename creates a rule to copy a Path to the artifacts
	// directory on the build server with the given filename when any of the
	// specified goals are built.
	DistForGoalsWithFilename(goals []string, path Path, filename string)

	// OtherModuleDependencyTag returns the dependency tag used to depend on a module, or nil if there is no dependency
	// on the module.  When called inside a Visit* method with current module being visited, and there are multiple
	// dependencies on the module being visited, it returns the dependency tag used for the current dependency.
	OtherModuleDependencyTag(module ModuleOrProxy) blueprint.DependencyTag

	GetIncrementalAnalysis() bool

	// OtherModuleNamespace returns the namespace of the module.
	OtherModuleNamespace(module ModuleOrProxy) *Namespace
}

type singletonAdaptor struct {
	Singleton

	buildParams []BuildParams
	ruleParams  map[blueprint.Rule]blueprint.RuleParams
}

var _ testBuildProvider = (*singletonAdaptor)(nil)
var _ blueprint.Singleton = (*singletonAdaptor)(nil)

func (s *singletonAdaptor) IncrementalSupported() bool {
	if im, ok := s.Singleton.(blueprint.Incremental); ok {
		return im.IncrementalSupported()
	}
	return false
}

func (s *singletonAdaptor) GenerateBuildActions(ctx blueprint.SingletonContext) {
	sctx := &singletonContextAdaptor{
		SingletonContext: ctx,
		phonies:          make(phonyMap),
	}
	if sctx.Config().captureBuild {
		sctx.ruleParams = make(map[blueprint.Rule]blueprint.RuleParams)
	}

	s.Singleton.GenerateBuildActions(sctx)

	s.buildParams = sctx.buildParams
	s.ruleParams = sctx.ruleParams

	if len(sctx.dists) > 0 {
		dists := getSingletonDists(sctx.Config())
		dists.lock.Lock()
		defer dists.lock.Unlock()
		dists.dists = append(dists.dists, sctx.dists...)
	}

	if len(sctx.phonies) > 0 {
		SetSingletonProvider(sctx, SingletonPhonyProvider, PhonyInfo{sctx.phonies})
	}
}

func (s *singletonAdaptor) BuildParamsForTests() []BuildParams {
	return s.buildParams
}

func (s *singletonAdaptor) RuleParamsForTests() map[blueprint.Rule]blueprint.RuleParams {
	return s.ruleParams
}

var singletonDistsKey = NewOnceKey("singletonDistsKey")

type singletonDistsAndLock struct {
	dists []dist
	lock  sync.Mutex
}

func getSingletonDists(config Config) *singletonDistsAndLock {
	return config.Once(singletonDistsKey, func() interface{} {
		return &singletonDistsAndLock{}
	}).(*singletonDistsAndLock)
}

type Singleton interface {
	GenerateBuildActions(SingletonContext)
}

type singletonContextAdaptor struct {
	blueprint.SingletonContext

	buildParams []BuildParams
	ruleParams  map[blueprint.Rule]blueprint.RuleParams
	dists       []dist
	phonies     phonyMap
}

func (s *singletonContextAdaptor) blueprintSingletonContext() blueprint.SingletonContext {
	return s.SingletonContext
}

func (s *singletonContextAdaptor) Config() Config {
	return s.SingletonContext.Config().(Config)
}

func (s *singletonContextAdaptor) DeviceConfig() DeviceConfig {
	return DeviceConfig{s.Config().deviceConfig}
}

func (s *singletonContextAdaptor) Variable(pctx PackageContext, name, value string) {
	s.SingletonContext.Variable(pctx.PackageContext, name, value)
}

func (s *singletonContextAdaptor) Rule(pctx PackageContext, name string, params blueprint.RuleParams, argNames ...string) blueprint.Rule {
	if s.Config().UseRemoteBuild() {
		if params.Pool == nil {
			// When USE_REWRAPPER=true is set and the rule is not supported by RBE,
			// restrict jobs to the local parallelism value
			params.Pool = localPool
		} else if params.Pool == remotePool {
			// remotePool is a fake pool used to identify rule that are supported for remoting. If the rule's
			// pool is the remotePool, replace with nil so that ninja runs it at NINJA_REMOTE_NUM_JOBS
			// parallelism.
			params.Pool = nil
		}
	}
	rule := s.SingletonContext.Rule(pctx.PackageContext, name, params, argNames...)
	if s.Config().captureBuild {
		s.ruleParams[rule] = params
	}
	return rule
}

func (s *singletonContextAdaptor) Build(pctx PackageContext, params BuildParams) {
	if s.Config().captureBuild {
		s.buildParams = append(s.buildParams, params)
	}
	bparams := convertBuildParams(params)
	s.SingletonContext.Build(pctx.PackageContext, bparams)
}

func (s *singletonContextAdaptor) Phony(name string, deps ...Path) {
	s.phonies[name] = append(s.phonies[name], deps...)
}

func (s *singletonContextAdaptor) SetOutDir(pctx PackageContext, value string) {
	s.SingletonContext.SetOutDir(pctx.PackageContext, value)
}

func (s *singletonContextAdaptor) Eval(pctx PackageContext, ninjaStr string) (string, error) {
	return s.SingletonContext.Eval(pctx.PackageContext, ninjaStr)
}

func (s *singletonContextAdaptor) ModuleErrorf(module ModuleOrProxy, fmt string, args ...any) {
	s.SingletonContext.ModuleErrorf(module, fmt, args...)
}

// visitAdaptor wraps a visit function that takes an android.Module parameter into
// a function that takes a blueprint.Module parameter and only calls the visit function if the
// blueprint.Module is an android.Module.
func visitAdaptor(visit func(Module)) func(blueprint.Module) {
	return func(module blueprint.Module) {
		if aModule, ok := module.(Module); ok {
			visit(aModule)
		}
	}
}

// visitProxyAdaptor wraps a visit function that takes an android.ModuleProxy parameter into
// a function that takes a blueprint.ModuleProxy parameter.
func visitProxyAdaptor(visit func(proxy ModuleProxy)) func(proxy blueprint.ModuleProxy) {
	return func(module blueprint.ModuleProxy) {
		visit(ModuleProxy{module})
	}
}

// predAdaptor wraps a pred function that takes an android.Module parameter
// into a function that takes an blueprint.Module parameter and only calls the visit function if the
// blueprint.Module is an android.Module, otherwise returns false.
func predAdaptor(pred func(Module) bool) func(blueprint.Module) bool {
	return func(module blueprint.Module) bool {
		if aModule, ok := module.(Module); ok {
			return pred(aModule)
		} else {
			return false
		}
	}
}

func (s *singletonContextAdaptor) ModuleName(module ModuleOrProxy) string {
	return s.SingletonContext.ModuleName(module)
}

func (s *singletonContextAdaptor) ModuleDir(module ModuleOrProxy) string {
	return s.SingletonContext.ModuleDir(module)
}

func (s *singletonContextAdaptor) ModuleSubDir(module ModuleOrProxy) string {
	return s.SingletonContext.ModuleSubDir(module)
}

func (s *singletonContextAdaptor) ModuleType(module ModuleOrProxy) string {
	return s.SingletonContext.ModuleType(module)
}

func (s *singletonContextAdaptor) BlueprintFile(module ModuleOrProxy) string {
	return s.SingletonContext.BlueprintFile(module)
}

func (s *singletonContextAdaptor) VisitAllModulesBlueprint(visit func(blueprint.Module)) {
	s.SingletonContext.VisitAllModules(visit)
}

func (s *singletonContextAdaptor) VisitAllModules(visit func(Module)) {
	s.SingletonContext.VisitAllModules(visitAdaptor(visit))
}

func (s *singletonContextAdaptor) VisitAllModuleProxies(visit func(proxy ModuleProxy)) {
	s.SingletonContext.VisitAllModuleProxies(visitProxyAdaptor(visit))
}

func (s *singletonContextAdaptor) VisitAllModulesOrProxies(visit func(ModuleOrProxy)) {
	s.SingletonContext.VisitAllModulesOrProxies(func(module blueprint.ModuleOrProxy) {
		visit(module)
	})
}

func (s *singletonContextAdaptor) VisitAllModulesIf(pred func(Module) bool, visit func(Module)) {
	s.SingletonContext.VisitAllModulesIf(predAdaptor(pred), visitAdaptor(visit))
}

func (s *singletonContextAdaptor) VisitDirectDeps(module Module, visit func(Module)) {
	s.SingletonContext.VisitDirectDeps(module, visitAdaptor(visit))
}

func (s *singletonContextAdaptor) VisitDirectDepsIf(module Module, pred func(Module) bool, visit func(Module)) {
	s.SingletonContext.VisitDirectDepsIf(module, predAdaptor(pred), visitAdaptor(visit))
}

func (s *singletonContextAdaptor) VisitDirectDepsProxies(module ModuleProxy, visit func(ModuleProxy)) {
	s.SingletonContext.VisitDirectDepsProxies(module.ModuleProxy, visitProxyAdaptor(visit))
}

func (s *singletonContextAdaptor) OtherModuleDependencyTag(module ModuleOrProxy) blueprint.DependencyTag {
	return s.SingletonContext.OtherModuleDependencyTag(module)
}

func (s *singletonContextAdaptor) VisitAllModuleVariants(module Module, visit func(Module)) {
	s.SingletonContext.VisitAllModuleVariants(module, visitAdaptor(visit))
}

func (s *singletonContextAdaptor) VisitAllModuleVariantProxies(module ModuleProxy, visit func(proxy ModuleProxy)) {
	s.SingletonContext.VisitAllModuleVariantProxies(module.ModuleProxy, visitProxyAdaptor(visit))
}

func (s *singletonContextAdaptor) VisitAllSingletons(visit func(singleton blueprint.SingletonProxy)) {
	s.SingletonContext.VisitAllSingletons(visit)
}

func (s *singletonContextAdaptor) PrimaryModule(module Module) Module {
	return s.SingletonContext.PrimaryModule(module).(Module)
}

func (s *singletonContextAdaptor) PrimaryModuleProxy(module ModuleProxy) ModuleProxy {
	return ModuleProxy{s.SingletonContext.PrimaryModuleProxy(module.ModuleProxy)}
}

func (s *singletonContextAdaptor) IsPrimaryModule(module ModuleOrProxy) bool {
	return s.SingletonContext.IsPrimaryModule(module)
}

func (s *singletonContextAdaptor) IsFinalModule(module ModuleOrProxy) bool {
	return s.SingletonContext.IsFinalModule(module)
}

func (s *singletonContextAdaptor) ModuleVariantsFromName(referer ModuleProxy, name string) []ModuleProxy {
	// get module reference for visibility enforcement
	qualified := createVisibilityModuleProxyReference(s, s.ModuleName(referer), s.ModuleDir(referer), referer)

	modules := s.SingletonContext.ModuleVariantsFromName(referer.ModuleProxy, name)
	result := make([]ModuleProxy, 0, len(modules))
	for _, module := range modules {
		// enforce visibility
		depName := s.ModuleName(module)
		depDir := s.ModuleDir(module)
		depQualified := qualifiedModuleName{depDir, depName}
		// Targets are always visible to other targets in their own package.
		if depQualified.pkg != qualified.name.pkg {
			rule := effectiveVisibilityRules(s.Config(), depQualified)
			if !rule.matches(qualified) {
				s.ModuleErrorf(referer, "module %q references %q which is not visible to this module\nYou may need to add %q to its visibility",
					referer.Name(), depQualified, "//"+s.ModuleDir(referer))
				continue
			}
		}
		result = append(result, ModuleProxy{module})
	}
	return result
}

func (s *singletonContextAdaptor) otherModuleProvider(module ModuleOrProxy, provider blueprint.AnyProviderKey) (any, bool) {
	return s.SingletonContext.ModuleProvider(module, provider)
}

func (s *singletonContextAdaptor) setSingletonProvider(provider blueprint.AnyProviderKey, value any) {
	s.SingletonContext.SetSingletonProvider(provider, value)
}

func (s *singletonContextAdaptor) otherSingletonProvider(singleton blueprint.SingletonProxy, provider blueprint.AnyProviderKey) (any, bool) {
	return s.SingletonContext.OtherSingletonProvider(singleton, provider)
}

func (s *singletonContextAdaptor) OtherModulePropertyErrorf(module ModuleOrProxy, property string, format string, args ...interface{}) {
	s.blueprintSingletonContext().OtherModulePropertyErrorf(module, property, format, args...)
}

func (s *singletonContextAdaptor) HasMutatorFinished(mutatorName string) bool {
	return s.blueprintSingletonContext().HasMutatorFinished(mutatorName)
}
func (s *singletonContextAdaptor) DistForGoal(goal string, paths ...Path) {
	s.DistForGoals([]string{goal}, paths...)
}

func (s *singletonContextAdaptor) DistForGoalWithFilename(goal string, path Path, filename string) {
	s.DistForGoalsWithFilename([]string{goal}, path, filename)
}

func (s *singletonContextAdaptor) DistForGoalWithFilenameTag(goal string, path Path, filename string) {
	insertBeforeExtension := func(file, insertion string) string {
		ext := filepath.Ext(file)
		return strings.TrimSuffix(file, ext) + insertion + ext
	}

	s.DistForGoalWithFilename(goal, path, insertBeforeExtension(filename, "-FILE_NAME_TAG_PLACEHOLDER"))
}

func (s *singletonContextAdaptor) DistForGoals(goals []string, paths ...Path) {
	var copies distCopies
	for _, path := range paths {
		copies = append(copies, distCopy{
			from: path,
			dest: path.Base(),
		})
	}
	s.dists = append(s.dists, dist{
		goals: slices.Clone(goals),
		paths: copies,
	})
}

func (s *singletonContextAdaptor) DistForGoalsWithFilename(goals []string, path Path, filename string) {
	s.dists = append(s.dists, dist{
		goals: slices.Clone(goals),
		paths: distCopies{{from: path, dest: filename}},
	})
}

func (s *singletonContextAdaptor) GetIncrementalAnalysis() bool {
	return s.SingletonContext.GetIncrementalAnalysis()
}

func (s *singletonContextAdaptor) OtherModuleNamespace(module ModuleOrProxy) *Namespace {
	return s.SingletonContext.OtherModuleNamespace(module).(*Namespace)
}

func SetSingletonProvider[K any](ctx SingletonContext, provider blueprint.ProviderKey[K], value K) {
	ctx.setSingletonProvider(provider, value)
}

func OtherSingletonProvider[K any](ctx SingletonContext, singleton blueprint.SingletonProxy, provider blueprint.ProviderKey[K]) (K, bool) {
	value, ok := ctx.otherSingletonProvider(singleton, provider)
	if !ok {
		var k K
		return k, false
	}
	return value.(K), ok
}
