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

package java

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"

	"android/soong/android"
	"android/soong/dexpreopt"
)

// A tag to associated a dependency with a specific api scope.
type scopeDependencyTag struct {
	blueprint.BaseDependencyTag
	name     string
	apiScope *apiScope

	// Function for extracting appropriate path information from the dependency.
	depInfoExtractor func(paths *scopePaths, ctx android.ModuleContext, dep android.Module) error
}

// Extract tag specific information from the dependency.
func (tag scopeDependencyTag) extractDepInfo(ctx android.ModuleContext, dep android.Module, paths *scopePaths) {
	err := tag.depInfoExtractor(paths, ctx, dep)
	if err != nil {
		ctx.ModuleErrorf("has an invalid {scopeDependencyTag: %s} dependency on module %s: %s", tag.name, ctx.OtherModuleName(dep), err.Error())
	}
}

var _ android.ReplaceSourceWithPrebuilt = (*scopeDependencyTag)(nil)

func (tag scopeDependencyTag) ReplaceSourceWithPrebuilt() bool {
	return false
}

// Provides information about an api scope, e.g. public, system, test.
type apiScope struct {
	// The name of the api scope, e.g. public, system, test
	name string

	// The api scope that this scope extends.
	//
	// This organizes the scopes into an extension hierarchy.
	//
	// If set this means that the API provided by this scope includes the API provided by the scope
	// set in this field.
	extends *apiScope

	// The next api scope that a library that uses this scope can access.
	//
	// This organizes the scopes into an access hierarchy.
	//
	// If set this means that a library that can access this API can also access the API provided by
	// the scope set in this field.
	//
	// A module that sets sdk_version: "<scope>_current" should have access to the <scope> API of
	// every java_sdk_library that it depends on. If the library does not provide an API for <scope>
	// then it will traverse up this access hierarchy to find an API that it does provide.
	//
	// If this is not set then it defaults to the scope set in extends.
	canAccess *apiScope

	// The legacy enabled status for a specific scope can be dependent on other
	// properties that have been specified on the library so it is provided by
	// a function that can determine the status by examining those properties.
	legacyEnabledStatus func(module *SdkLibrary) bool

	// The default enabled status for non-legacy behavior, which is triggered by
	// explicitly enabling at least one api scope.
	defaultEnabledStatus bool

	// Gets a pointer to the scope specific properties.
	scopeSpecificProperties func(module *SdkLibrary) *ApiScopeProperties

	// The name of the field in the dynamically created structure.
	fieldName string

	// The name of the property in the java_sdk_library_import
	propertyName string

	// The tag to use to depend on the prebuilt stubs library module
	prebuiltStubsTag scopeDependencyTag

	// The tag to use to depend on the everything stubs library module.
	everythingStubsTag scopeDependencyTag

	// The tag to use to depend on the exportable stubs library module.
	exportableStubsTag scopeDependencyTag

	// The tag to use to depend on the stubs source module (if separate from the API module).
	stubsSourceTag scopeDependencyTag

	// The tag to use to depend on the stubs source and API module.
	stubsSourceAndApiTag scopeDependencyTag

	// The tag to use to depend on the module that provides the latest version of the API .txt file.
	latestApiModuleTag scopeDependencyTag

	// The tag to use to depend on the module that provides the latest version of the API removed.txt
	// file.
	latestRemovedApiModuleTag scopeDependencyTag

	// The scope specific prefix to add to the api file base of "current.txt" or "removed.txt".
	apiFilePrefix string

	// The scope specific suffix to add to the sdk library module name to construct a scope specific
	// module name.
	moduleSuffix string

	// SDK version that the stubs library is built against. Note that this is always
	// *current. Older stubs library built with a numbered SDK version is created from
	// the prebuilt jar.
	sdkVersion string

	// The annotation that identifies this API level, empty for the public API scope.
	annotation string

	// Extra arguments to pass to droidstubs for this scope.
	//
	// This is not used directly but is used to construct the droidstubsArgs.
	extraArgs []string

	// The args that must be passed to droidstubs to generate the API and stubs source
	// for this scope, constructed dynamically by initApiScope().
	//
	// The API only includes the additional members that this scope adds over the scope
	// that it extends.
	//
	// The stubs source must include the definitions of everything that is in this
	// api scope and all the scopes that this one extends.
	droidstubsArgs []string

	// Whether the api scope can be treated as unstable, and should skip compat checks.
	unstable bool

	// Represents the SDK kind of this scope.
	kind android.SdkKind
}

// Initialize a scope, creating and adding appropriate dependency tags
func initApiScope(scope *apiScope) *apiScope {
	name := scope.name
	scopeByName[name] = scope
	allScopeNames = append(allScopeNames, name)
	scope.propertyName = strings.ReplaceAll(name, "-", "_")
	scope.fieldName = proptools.FieldNameForProperty(scope.propertyName)
	scope.prebuiltStubsTag = scopeDependencyTag{
		name:             name + "-stubs",
		apiScope:         scope,
		depInfoExtractor: (*scopePaths).extractStubsLibraryInfoFromDependency,
	}
	scope.everythingStubsTag = scopeDependencyTag{
		name:             name + "-stubs-everything",
		apiScope:         scope,
		depInfoExtractor: (*scopePaths).extractEverythingStubsLibraryInfoFromDependency,
	}
	scope.exportableStubsTag = scopeDependencyTag{
		name:             name + "-stubs-exportable",
		apiScope:         scope,
		depInfoExtractor: (*scopePaths).extractExportableStubsLibraryInfoFromDependency,
	}
	scope.stubsSourceTag = scopeDependencyTag{
		name:             name + "-stubs-source",
		apiScope:         scope,
		depInfoExtractor: (*scopePaths).extractStubsSourceInfoFromDep,
	}
	scope.stubsSourceAndApiTag = scopeDependencyTag{
		name:             name + "-stubs-source-and-api",
		apiScope:         scope,
		depInfoExtractor: (*scopePaths).extractStubsSourceAndApiInfoFromApiStubsProvider,
	}
	scope.latestApiModuleTag = scopeDependencyTag{
		name:             name + "-latest-api",
		apiScope:         scope,
		depInfoExtractor: (*scopePaths).extractLatestApiPath,
	}
	scope.latestRemovedApiModuleTag = scopeDependencyTag{
		name:             name + "-latest-removed-api",
		apiScope:         scope,
		depInfoExtractor: (*scopePaths).extractLatestRemovedApiPath,
	}

	// To get the args needed to generate the stubs source append all the args from
	// this scope and all the scopes it extends as each set of args adds additional
	// members to the stubs.
	var scopeSpecificArgs []string
	if scope.annotation != "" {
		scopeSpecificArgs = []string{"--show-annotation", scope.annotation}
	}
	for s := scope; s != nil; s = s.extends {
		scopeSpecificArgs = append(scopeSpecificArgs, s.extraArgs...)

		// Ensure that the generated stubs includes all the API elements from the API scope
		// that this scope extends.
		if s != scope && s.annotation != "" {
			scopeSpecificArgs = append(scopeSpecificArgs, "--show-for-stub-purposes-annotation", s.annotation)
		}
	}

	// By default, a library that can access a scope can also access the scope it extends.
	if scope.canAccess == nil {
		scope.canAccess = scope.extends
	}

	// Escape any special characters in the arguments. This is needed because droidstubs
	// passes these directly to the shell command.
	scope.droidstubsArgs = proptools.ShellEscapeList(scopeSpecificArgs)

	return scope
}

func (scope *apiScope) stubsLibraryModuleNameSuffix() string {
	return ".stubs" + scope.moduleSuffix
}

func (scope *apiScope) exportableStubsLibraryModuleNameSuffix() string {
	return ".stubs.exportable" + scope.moduleSuffix
}

func (scope *apiScope) apiLibraryModuleName(baseName string) string {
	return scope.stubsLibraryModuleName(baseName) + ".from-text"
}

func (scope *apiScope) sourceStubsLibraryModuleName(baseName string) string {
	return scope.stubsLibraryModuleName(baseName) + ".from-source"
}

func (scope *apiScope) exportableSourceStubsLibraryModuleName(baseName string) string {
	return scope.exportableStubsLibraryModuleName(baseName) + ".from-source"
}

func (scope *apiScope) stubsLibraryModuleName(baseName string) string {
	return baseName + scope.stubsLibraryModuleNameSuffix()
}

func (scope *apiScope) exportableStubsLibraryModuleName(baseName string) string {
	return baseName + scope.exportableStubsLibraryModuleNameSuffix()
}

func (scope *apiScope) stubsSourceModuleName(baseName string) string {
	return baseName + ".stubs.source" + scope.moduleSuffix
}

func (scope *apiScope) String() string {
	return scope.name
}

// snapshotRelativeDir returns the snapshot directory into which the files related to scopes will
// be stored.
func (scope *apiScope) snapshotRelativeDir() string {
	return filepath.Join("sdk_library", scope.name)
}

// snapshotRelativeCurrentApiTxtPath returns the snapshot path to the API .txt file for the named
// library.
func (scope *apiScope) snapshotRelativeCurrentApiTxtPath(name string) string {
	return filepath.Join(scope.snapshotRelativeDir(), name+".txt")
}

// snapshotRelativeRemovedApiTxtPath returns the snapshot path to the removed API .txt file for the
// named library.
func (scope *apiScope) snapshotRelativeRemovedApiTxtPath(name string) string {
	return filepath.Join(scope.snapshotRelativeDir(), name+"-removed.txt")
}

type apiScopes []*apiScope

func (scopes apiScopes) Strings(accessor func(*apiScope) string) []string {
	var list []string
	for _, scope := range scopes {
		list = append(list, accessor(scope))
	}
	return list
}

// Method that maps the apiScopes properties to the index of each apiScopes elements.
// apiScopes property to be used as the key can be specified with the input accessor.
// Only a string property of apiScope can be used as the key of the map.
func (scopes apiScopes) MapToIndex(accessor func(*apiScope) string) map[string]int {
	ret := make(map[string]int)
	for i, scope := range scopes {
		ret[accessor(scope)] = i
	}
	return ret
}

func (scopes apiScopes) ConvertStubsLibraryExportableToEverything(name string) string {
	for _, scope := range scopes {
		if strings.HasSuffix(name, scope.exportableStubsLibraryModuleNameSuffix()) {
			return strings.TrimSuffix(name, scope.exportableStubsLibraryModuleNameSuffix()) +
				scope.stubsLibraryModuleNameSuffix()
		}
	}
	return name
}

func (scopes apiScopes) matchingScopeFromSdkKind(kind android.SdkKind) *apiScope {
	for _, scope := range scopes {
		if scope.kind == kind {
			return scope
		}
	}
	return nil
}

var (
	scopeByName    = make(map[string]*apiScope)
	allScopeNames  []string
	apiScopePublic = initApiScope(&apiScope{
		name: "public",

		// Public scope is enabled by default for both legacy and non-legacy modes.
		legacyEnabledStatus: func(module *SdkLibrary) bool {
			return true
		},
		defaultEnabledStatus: true,

		scopeSpecificProperties: func(module *SdkLibrary) *ApiScopeProperties {
			return &module.sdkLibraryProperties.Public
		},
		sdkVersion: "current",
		kind:       android.SdkPublic,
	})
	apiScopeSystem = initApiScope(&apiScope{
		name:                "system",
		extends:             apiScopePublic,
		legacyEnabledStatus: (*SdkLibrary).generateTestAndSystemScopesByDefault,
		scopeSpecificProperties: func(module *SdkLibrary) *ApiScopeProperties {
			return &module.sdkLibraryProperties.System
		},
		apiFilePrefix: "system-",
		moduleSuffix:  ".system",
		sdkVersion:    "system_current",
		annotation:    "android.annotation.SystemApi(client=android.annotation.SystemApi.Client.PRIVILEGED_APPS)",
		kind:          android.SdkSystem,
	})
	apiScopeTest = initApiScope(&apiScope{
		name:                "test",
		extends:             apiScopeSystem,
		legacyEnabledStatus: (*SdkLibrary).generateTestAndSystemScopesByDefault,
		scopeSpecificProperties: func(module *SdkLibrary) *ApiScopeProperties {
			return &module.sdkLibraryProperties.Test
		},
		apiFilePrefix: "test-",
		moduleSuffix:  ".test",
		sdkVersion:    "test_current",
		annotation:    "android.annotation.TestApi",
		unstable:      true,
		kind:          android.SdkTest,
	})
	apiScopeModuleLib = initApiScope(&apiScope{
		name:    "module-lib",
		extends: apiScopeSystem,
		// The module-lib scope is disabled by default in legacy mode.
		//
		// Enabling this would break existing usages.
		legacyEnabledStatus: func(module *SdkLibrary) bool {
			return false
		},
		scopeSpecificProperties: func(module *SdkLibrary) *ApiScopeProperties {
			return &module.sdkLibraryProperties.Module_lib
		},
		apiFilePrefix: "module-lib-",
		moduleSuffix:  ".module_lib",
		sdkVersion:    "module_current",
		annotation:    "android.annotation.SystemApi(client=android.annotation.SystemApi.Client.MODULE_LIBRARIES)",
		kind:          android.SdkModule,
	})
	apiScopeSystemServer = initApiScope(&apiScope{
		name:    "system-server",
		extends: apiScopePublic,

		// The system-server scope can access the module-lib scope.
		//
		// A module that provides a system-server API is appended to the standard bootclasspath that is
		// used by the system server. So, it should be able to access module-lib APIs provided by
		// libraries on the bootclasspath.
		canAccess: apiScopeModuleLib,

		// The system-server scope is disabled by default in legacy mode.
		//
		// Enabling this would break existing usages.
		legacyEnabledStatus: func(module *SdkLibrary) bool {
			return false
		},
		scopeSpecificProperties: func(module *SdkLibrary) *ApiScopeProperties {
			return &module.sdkLibraryProperties.System_server
		},
		apiFilePrefix: "system-server-",
		moduleSuffix:  ".system_server",
		sdkVersion:    "system_server_current",
		annotation:    "android.annotation.SystemApi(client=android.annotation.SystemApi.Client.SYSTEM_SERVER)",
		extraArgs: []string{
			"--hide-annotation", "android.annotation.Hide",
			// com.android.* classes are okay in this interface"
			"--hide", "InternalClasses",
		},
		kind: android.SdkSystemServer,
	})
	AllApiScopes = apiScopes{
		apiScopePublic,
		apiScopeSystem,
		apiScopeTest,
		apiScopeModuleLib,
		apiScopeSystemServer,
	}
	apiLibraryAdditionalProperties = map[string]string{
		"legacy.i18n.module.platform.api": "i18n.module.public.api.stubs.source.api.contribution",
		"stable.i18n.module.platform.api": "i18n.module.public.api.stubs.source.api.contribution",
		"conscrypt.module.platform.api":   "conscrypt.module.public.api.stubs.source.api.contribution",
	}
)

var (
	javaSdkLibrariesLock sync.Mutex
)

// TODO: these are big features that are currently missing
// 1) disallowing linking to the runtime shared lib
// 2) HTML generation

func init() {
	RegisterSdkLibraryBuildComponents(android.InitRegistrationContext)

	android.RegisterMakeVarsProvider(pctx, func(ctx android.MakeVarsContext) {
		javaSdkLibraries := javaSdkLibraries(ctx.Config())
		sort.Strings(*javaSdkLibraries)
		ctx.Strict("JAVA_SDK_LIBRARIES", strings.Join(*javaSdkLibraries, " "))
	})

	// Register sdk member types.
	android.RegisterSdkMemberType(javaSdkLibrarySdkMemberType)
}

func RegisterSdkLibraryBuildComponents(ctx android.RegistrationContext) {
	ctx.RegisterModuleType("java_sdk_library", SdkLibraryFactory)
	ctx.RegisterModuleType("java_sdk_library_import", sdkLibraryImportFactory)
}

// Properties associated with each api scope.
type ApiScopeProperties struct {
	// Indicates whether the api surface is generated.
	//
	// If this is set for any scope then all scopes must explicitly specify if they
	// are enabled. This is to prevent new usages from depending on legacy behavior.
	//
	// Otherwise, if this is not set for any scope then the default  behavior is
	// scope specific so please refer to the scope specific property documentation.
	Enabled *bool

	// The sdk_version to use for building the stubs.
	//
	// If not specified then it will use an sdk_version determined as follows:
	//
	// 1) If the sdk_version specified on the java_sdk_library is none then this
	// will be none. This is used for java_sdk_library instances that are used
	// to create stubs that contribute to the core_current sdk version.
	// 2) Otherwise, it is assumed that this library extends but does not
	// contribute directly to a specific sdk_version and so this uses the
	// sdk_version appropriate for the api scope. e.g. public will use
	// sdk_version: current, system will use sdk_version: system_current, etc.
	//
	// This does not affect the sdk_version used for either generating the stubs source
	// or the API file. They both have to use the same sdk_version as is used for
	// compiling the implementation library.
	Sdk_version *string

	// Extra libs used when compiling stubs for this scope.
	Libs []string

	// Name to override the api_surface that is passed down to droidstubs.
	Api_surface *string
}

type sdkLibraryProperties struct {
	// List of source files that are needed to compile the API, but are not part of runtime library.
	Api_srcs []string `android:"arch_variant"`

	// Visibility for impl library module. If not specified then defaults to the
	// visibility property.
	Impl_library_visibility []string

	// Visibility for stubs library modules. If not specified then defaults to the
	// visibility property.
	Stubs_library_visibility []string

	// Visibility for stubs source modules. If not specified then defaults to the
	// visibility property.
	Stubs_source_visibility []string

	// List of Java libraries that will be in the classpath when building the implementation lib
	Impl_only_libs []string `android:"arch_variant"`

	// List of Java libraries that will included in the implementation lib.
	Impl_only_static_libs []string `android:"arch_variant"`

	// List of Java libraries that will be in the classpath when building stubs
	Stub_only_libs []string `android:"arch_variant"`

	// List of Java libraries that will included in stub libraries
	Stub_only_static_libs []string `android:"arch_variant"`

	// list of package names that will be documented and publicized as API.
	// This allows the API to be restricted to a subset of the source files provided.
	// If this is unspecified then all the source files will be treated as being part
	// of the API.
	Api_packages []string

	// the relative path to the directory containing the api specification files.
	// Defaults to "api".
	Api_dir *string

	// Determines whether a runtime implementation library is built; defaults to false.
	//
	// If true then it also prevents the module from being used as a shared module, i.e.
	// it is as if shared_library: false, was set.
	Api_only *bool

	// local files that are used within user customized droiddoc options.
	Droiddoc_option_files []string

	// additional droiddoc options.
	// Available variables for substitution:
	//
	//  $(location <label>): the path to the droiddoc_option_files with name <label>
	Droiddoc_options []string

	// is set to true, Metalava will allow framework SDK to contain annotations.
	Annotations_enabled *bool

	// a list of top-level directories containing files to merge qualifier annotations
	// (i.e. those intended to be included in the stubs written) from.
	Merge_annotations_dirs []string

	// a list of top-level directories containing Java stub files to merge show/hide annotations from.
	Merge_inclusion_annotations_dirs []string

	// If set to true then don't create dist rules.
	No_dist *bool

	// The stem for the artifacts that are copied to the dist, if not specified
	// then defaults to the base module name.
	//
	// For each scope the following artifacts are copied to the apistubs/<scope>
	// directory in the dist.
	// * stubs impl jar -> <dist-stem>.jar
	// * API specification file -> api/<dist-stem>.txt
	// * Removed API specification file -> api/<dist-stem>-removed.txt
	//
	// Also used to construct the name of the filegroup (created by prebuilt_apis)
	// that references the latest released API and remove API specification files.
	// * API specification filegroup -> <dist-stem>.api.<scope>.latest
	// * Removed API specification filegroup -> <dist-stem>-removed.api.<scope>.latest
	// * API incompatibilities baseline filegroup -> <dist-stem>-incompatibilities.api.<scope>.latest
	Dist_stem *string

	// The subdirectory for the artifacts that are copied to the dist directory.  If not specified
	// then defaults to "unknown".  Should be set to "android" for anything that should be published
	// in the public Android SDK.
	Dist_group *string

	// A compatibility mode that allows historical API-tracking files to not exist.
	// Do not use.
	Unsafe_ignore_missing_latest_api bool

	// indicates whether system and test apis should be generated.
	Generate_system_and_test_apis bool `blueprint:"mutated"`

	// The properties specific to the public api scope
	//
	// Unless explicitly specified by using public.enabled the public api scope is
	// enabled by default in both legacy and non-legacy mode.
	Public ApiScopeProperties

	// The properties specific to the system api scope
	//
	// In legacy mode the system api scope is enabled by default when sdk_version
	// is set to something other than "none".
	//
	// In non-legacy mode the system api scope is disabled by default.
	System ApiScopeProperties

	// The properties specific to the test api scope
	//
	// In legacy mode the test api scope is enabled by default when sdk_version
	// is set to something other than "none".
	//
	// In non-legacy mode the test api scope is disabled by default.
	Test ApiScopeProperties

	// The properties specific to the module-lib api scope
	//
	// Unless explicitly specified by using module_lib.enabled the module_lib api
	// scope is disabled by default.
	Module_lib ApiScopeProperties

	// The properties specific to the system-server api scope
	//
	// Unless explicitly specified by using system_server.enabled the
	// system_server api scope is disabled by default.
	System_server ApiScopeProperties

	// Determines if the stubs are preferred over the implementation library
	// for linking, even when the client doesn't specify sdk_version. When this
	// is set to true, such clients are provided with the widest API surface that
	// this lib provides. Note however that this option doesn't affect the clients
	// that are in the same APEX as this library. In that case, the clients are
	// always linked with the implementation library. Default is false.
	Default_to_stubs *bool

	// Properties related to api linting.
	Api_lint struct {
		// Enable api linting.
		Enabled *bool

		// If API lint is enabled, this flag controls whether a set of legitimate lint errors
		// are turned off. The default is true.
		Legacy_errors_allowed *bool
	}

	// a list of aconfig_declarations module names that the stubs generated in this module
	// depend on.
	Aconfig_declarations []string

	// Determines if the module generates the stubs from the api signature files
	// instead of the source Java files. Defaults to true.
	Build_from_text_stub *bool

	// TODO: determines whether to create HTML doc or not
	// Html_doc *bool
}

// Paths to outputs from java_sdk_library and java_sdk_library_import.
//
// Fields that are android.Paths are always set (during GenerateAndroidBuildActions).
// OptionalPaths are always set by java_sdk_library but may not be set by
// java_sdk_library_import as not all instances provide that information.
type scopePaths struct {
	// The path (represented as Paths for convenience when returning) to the stubs header jar.
	//
	// That is the jar that is created by turbine.
	stubsHeaderPath android.Paths

	// The path (represented as Paths for convenience when returning) to the stubs implementation jar.
	//
	// This is not the implementation jar, it still only contains stubs.
	stubsImplPath android.Paths

	// The dex jar for the stubs.
	//
	// This is not the implementation jar, it still only contains stubs.
	stubsDexJarPath OptionalDexJarPath

	// The exportable dex jar for the stubs.
	// This is not the implementation jar, it still only contains stubs.
	// Includes unflagged apis and flagged apis enabled by release configurations.
	exportableStubsDexJarPath OptionalDexJarPath

	// The API specification file, e.g. system_current.txt.
	currentApiFilePath android.OptionalPath

	// The specification of API elements removed since the last release.
	removedApiFilePath android.OptionalPath

	// The stubs source jar.
	stubsSrcJar android.OptionalPath

	// Extracted annotations.
	annotationsZip android.OptionalPath

	// The path to the latest API file.
	latestApiPaths android.Paths

	// The path to the latest removed API file.
	latestRemovedApiPaths android.Paths
}

func (paths *scopePaths) extractStubsLibraryInfoFromDependency(ctx android.ModuleContext, dep android.Module) error {
	if lib, ok := android.OtherModuleProvider(ctx, dep, JavaInfoProvider); ok {
		paths.stubsHeaderPath = lib.HeaderJars
		paths.stubsImplPath = lib.ImplementationJars

		libDep := android.OtherModuleProviderOrDefault(ctx, dep, JavaInfoProvider)
		paths.stubsDexJarPath = libDep.DexJarBuildPath
		paths.exportableStubsDexJarPath = libDep.DexJarBuildPath
		return nil
	} else {
		return fmt.Errorf("expected module that has JavaInfoProvider, e.g. java_library")
	}
}

func (paths *scopePaths) extractEverythingStubsLibraryInfoFromDependency(ctx android.ModuleContext, dep android.Module) error {
	if lib, ok := android.OtherModuleProvider(ctx, dep, JavaInfoProvider); ok {
		paths.stubsHeaderPath = lib.HeaderJars
		if !ctx.Config().ReleaseHiddenApiExportableStubs() {
			paths.stubsImplPath = lib.ImplementationJars
		}

		libDep := android.OtherModuleProviderOrDefault(ctx, dep, JavaInfoProvider)
		paths.stubsDexJarPath = libDep.DexJarBuildPath
		return nil
	} else {
		return fmt.Errorf("expected module that has JavaInfoProvider, e.g. java_library")
	}
}

func (paths *scopePaths) extractExportableStubsLibraryInfoFromDependency(ctx android.ModuleContext, dep android.Module) error {
	if lib, ok := android.OtherModuleProvider(ctx, dep, JavaInfoProvider); ok {
		if ctx.Config().ReleaseHiddenApiExportableStubs() {
			paths.stubsImplPath = lib.ImplementationJars
		}

		libDep := android.OtherModuleProviderOrDefault(ctx, dep, JavaInfoProvider)
		paths.exportableStubsDexJarPath = libDep.DexJarBuildPath
		return nil
	} else {
		return fmt.Errorf("expected module that has JavaInfoProvider, e.g. java_library")
	}
}

func (paths *scopePaths) treatDepAsApiStubsProvider(ctx android.ModuleContext, dep android.Module,
	action func(*DroidStubsInfo, *StubsSrcInfo) error) error {
	apiStubsProvider, ok := android.OtherModuleProvider(ctx, dep, DroidStubsInfoProvider)
	if !ok {
		return fmt.Errorf("expected module that provides DroidStubsInfo, e.g. droidstubs")
	}

	apiStubsSrcProvider, ok := android.OtherModuleProvider(ctx, dep, StubsSrcInfoProvider)
	if !ok {
		return fmt.Errorf("expected module that provides StubsSrcInfo, e.g. droidstubs")
	}
	return action(&apiStubsProvider, &apiStubsSrcProvider)
}

func (paths *scopePaths) treatDepAsApiStubsSrcProvider(
	ctx android.ModuleContext, dep android.Module, action func(provider *StubsSrcInfo) error) error {
	if apiStubsProvider, ok := android.OtherModuleProvider(ctx, dep, StubsSrcInfoProvider); ok {
		err := action(&apiStubsProvider)
		if err != nil {
			return err
		}
		return nil
	} else {
		return fmt.Errorf("expected module that provides DroidStubsInfo, e.g. droidstubs")
	}
}

func (paths *scopePaths) extractApiInfoFromApiStubsProvider(provider *DroidStubsInfo, stubsType StubsType) error {
	var currentApiFilePathErr, removedApiFilePathErr error
	info, err := getStubsInfoForType(provider, stubsType)
	if err != nil {
		return err
	}
	if info.ApiFile == nil {
		currentApiFilePathErr = fmt.Errorf("expected module that provides ApiFile")
	}
	if info.RemovedApiFile == nil {
		removedApiFilePathErr = fmt.Errorf("expected module that provides RemovedApiFile")
	}
	combinedError := errors.Join(currentApiFilePathErr, removedApiFilePathErr)

	if combinedError == nil {
		paths.annotationsZip = android.OptionalPathForPath(info.AnnotationsZip)
		paths.currentApiFilePath = android.OptionalPathForPath(info.ApiFile)
		paths.removedApiFilePath = android.OptionalPathForPath(info.RemovedApiFile)
	}
	return combinedError
}

func (paths *scopePaths) extractStubsSourceInfoFromApiStubsProviders(provider *StubsSrcInfo, stubsType StubsType) error {
	path, err := getStubsSrcInfoForType(provider, stubsType)
	if err == nil {
		paths.stubsSrcJar = android.OptionalPathForPath(path)
	}
	return err
}

func (paths *scopePaths) extractStubsSourceInfoFromDep(ctx android.ModuleContext, dep android.Module) error {
	stubsType := Everything
	if ctx.Config().ReleaseHiddenApiExportableStubs() {
		stubsType = Exportable
	}
	return paths.treatDepAsApiStubsSrcProvider(ctx, dep, func(provider *StubsSrcInfo) error {
		return paths.extractStubsSourceInfoFromApiStubsProviders(provider, stubsType)
	})
}

func (paths *scopePaths) extractStubsSourceAndApiInfoFromApiStubsProvider(ctx android.ModuleContext, dep android.Module) error {
	stubsType := Everything
	if ctx.Config().ReleaseHiddenApiExportableStubs() {
		stubsType = Exportable
	}
	return paths.treatDepAsApiStubsProvider(ctx, dep, func(apiStubsProvider *DroidStubsInfo, apiStubsSrcProvider *StubsSrcInfo) error {
		extractApiInfoErr := paths.extractApiInfoFromApiStubsProvider(apiStubsProvider, stubsType)
		extractStubsSourceInfoErr := paths.extractStubsSourceInfoFromApiStubsProviders(apiStubsSrcProvider, stubsType)
		return errors.Join(extractApiInfoErr, extractStubsSourceInfoErr)
	})
}

func extractOutputPaths(ctx android.ModuleContext, dep android.Module) (android.Paths, error) {
	var paths android.Paths
	if sourceFileProducer, ok := android.OtherModuleProvider(ctx, dep, android.SourceFilesInfoProvider); ok {
		paths = sourceFileProducer.Srcs
		return paths, nil
	} else {
		return nil, fmt.Errorf("module %q does not produce source files", dep)
	}
}

func (paths *scopePaths) extractLatestApiPath(ctx android.ModuleContext, dep android.Module) error {
	outputPaths, err := extractOutputPaths(ctx, dep)
	paths.latestApiPaths = outputPaths
	return err
}

func (paths *scopePaths) extractLatestRemovedApiPath(ctx android.ModuleContext, dep android.Module) error {
	outputPaths, err := extractOutputPaths(ctx, dep)
	paths.latestRemovedApiPaths = outputPaths
	return err
}

func getStubsInfoForType(info *DroidStubsInfo, stubsType StubsType) (ret *StubsInfo, err error) {
	switch stubsType {
	case Everything:
		ret, err = &info.EverythingStubsInfo, nil
	case Exportable:
		ret, err = &info.ExportableStubsInfo, nil
	default:
		ret, err = nil, fmt.Errorf("stubs info not supported for the stub type %s", stubsType.String())
	}
	if ret == nil && err == nil {
		err = fmt.Errorf("stubs info is null for the stub type %s", stubsType.String())
	}
	return ret, err
}

func getStubsSrcInfoForType(info *StubsSrcInfo, stubsType StubsType) (ret android.Path, err error) {
	switch stubsType {
	case Everything:
		ret, err = info.EverythingStubsSrcJar, nil
	case Exportable:
		ret, err = info.ExportableStubsSrcJar, nil
	default:
		ret, err = nil, fmt.Errorf("stubs src info not supported for the stub type %s", stubsType.String())
	}
	if ret == nil && err == nil {
		err = fmt.Errorf("stubs src info is null for the stub type %s", stubsType.String())
	}
	return ret, err
}

type commonToSdkLibraryAndImportProperties struct {
	// Specifies whether this module can be used as an Android shared library; defaults
	// to true.
	//
	// An Android shared library is one that can be referenced in a <uses-library> element
	// in an AndroidManifest.xml.
	Shared_library *bool

	// Files containing information about supported java doc tags.
	Doctag_files []string `android:"path"`

	// Signals that this shared library is part of the bootclasspath starting
	// on the version indicated in this attribute.
	//
	// This will make platforms at this level and above to ignore
	// <uses-library> tags with this library name because the library is already
	// available
	On_bootclasspath_since *string

	// Signals that this shared library was part of the bootclasspath before
	// (but not including) the version indicated in this attribute.
	//
	// The system will automatically add a <uses-library> tag with this library to
	// apps that target any SDK less than the version indicated in this attribute.
	On_bootclasspath_before *string

	// Indicates that PackageManager should ignore this shared library if the
	// platform is below the version indicated in this attribute.
	//
	// This means that the device won't recognise this library as installed.
	Min_device_sdk *string

	// Indicates that PackageManager should ignore this shared library if the
	// platform is above the version indicated in this attribute.
	//
	// This means that the device won't recognise this library as installed.
	Max_device_sdk *string
}

// commonSdkLibraryAndImportModule defines the interface that must be provided by a module that
// embeds the commonToSdkLibraryAndImport struct.
type commonSdkLibraryAndImportModule interface {
	android.Module

	// Returns the name of the root java_sdk_library that creates the child stub libraries
	// This is the `name` as it appears in Android.bp, and not the name in Soong's build graph
	// (with the prebuilt_ prefix)
	//
	// e.g. in the following java_sdk_library_import
	// java_sdk_library_import {
	//    name: "framework-foo.v1",
	//    source_module_name: "framework-foo",
	// }
	// the values returned by
	// 1. Name(): prebuilt_framework-foo.v1 # unique
	// 2. BaseModuleName(): framework-foo # the source
	// 3. RootLibraryName: framework-foo.v1 # the undecordated `name` from Android.bp
	RootLibraryName() string
}

var _ android.ApexModule = (*SdkLibrary)(nil)

func (m *SdkLibrary) RootLibraryName() string {
	return m.BaseModuleName()
}

func (m *SdkLibraryImport) RootLibraryName() string {
	// m.BaseModuleName refers to the source of the import
	// use moduleBase.Name to get the name of the module as it appears in the .bp file
	return m.ModuleBase.Name()
}

// Common code between sdk library and sdk library import
type commonToSdkLibraryAndImport struct {
	module commonSdkLibraryAndImportModule

	scopePaths map[*apiScope]*scopePaths

	commonSdkLibraryProperties commonToSdkLibraryAndImportProperties

	// Paths to commonSdkLibraryProperties.Doctag_files
	doctagPaths android.Paths

	// Functionality related to this being used as a component of a java_sdk_library.
	EmbeddableSdkLibraryComponent

	// Path to the header jars of the implementation library
	// This is non-empty only when api_only is false.
	implLibraryHeaderJars android.Paths

	// The reference to the JavaInfo provided by implementation library created by
	// the source module. Is nil if the source module does not exist.
	implLibraryInfo *JavaInfo
}

func (c *commonToSdkLibraryAndImport) initCommon(module commonSdkLibraryAndImportModule) {
	c.module = module

	module.AddProperties(&c.commonSdkLibraryProperties)

	// Initialize this as an sdk library component.
	c.initSdkLibraryComponent(module)
}

func (c *commonToSdkLibraryAndImport) initCommonAfterDefaultsApplied() bool {
	namePtr := proptools.StringPtr(c.module.RootLibraryName())
	c.sdkLibraryComponentProperties.SdkLibraryName = namePtr

	// Only track this sdk library if this can be used as a shared library.
	if c.sharedLibrary() {
		// Use the name specified in the module definition as the owner.
		c.sdkLibraryComponentProperties.SdkLibraryToImplicitlyTrack = namePtr
	}

	return true
}

// uniqueApexVariations provides common implementation of the ApexModule.UniqueApexVariations
// method.
func (c *commonToSdkLibraryAndImport) uniqueApexVariations() bool {
	// A java_sdk_library that is a shared library produces an XML file that makes the shared library
	// usable from an AndroidManifest.xml's <uses-library> entry. That XML file contains the name of
	// the APEX and so it needs a unique variation per APEX.
	return c.sharedLibrary()
}

func (c *commonToSdkLibraryAndImport) generateCommonBuildActions(ctx android.ModuleContext) SdkLibraryInfo {
	c.doctagPaths = android.PathsForModuleSrc(ctx, c.commonSdkLibraryProperties.Doctag_files)

	everythingStubPaths := make(map[android.SdkKind]OptionalDexJarPath)
	exportableStubPaths := make(map[android.SdkKind]OptionalDexJarPath)
	removedApiFilePaths := make(map[android.SdkKind]android.OptionalPath)
	for kind := android.SdkNone; kind <= android.SdkPrivate; kind += 1 {
		everythingStubPath := makeUnsetDexJarPath()
		exportableStubPath := makeUnsetDexJarPath()
		removedApiFilePath := android.OptionalPath{}
		if scopePath := c.findClosestScopePath(sdkKindToApiScope(kind)); scopePath != nil {
			everythingStubPath = scopePath.stubsDexJarPath
			exportableStubPath = scopePath.exportableStubsDexJarPath
			removedApiFilePath = scopePath.removedApiFilePath
		}
		everythingStubPaths[kind] = everythingStubPath
		exportableStubPaths[kind] = exportableStubPath
		removedApiFilePaths[kind] = removedApiFilePath
	}

	return SdkLibraryInfo{
		EverythingStubDexJarPaths: everythingStubPaths,
		ExportableStubDexJarPaths: exportableStubPaths,
		RemovedTxtFiles:           removedApiFilePaths,
		SharedLibrary:             c.sharedLibrary(),
	}
}

// The component names for different outputs of the java_sdk_library.
//
// They are similar to the names used for the child modules it creates
const (
	stubsSourceComponentName = "stubs.source"

	apiTxtComponentName = "api.txt"

	removedApiTxtComponentName = "removed-api.txt"

	annotationsComponentName = "annotations.zip"
)

func (module *commonToSdkLibraryAndImport) setOutputFiles(ctx android.ModuleContext) {
	if module.doctagPaths != nil {
		ctx.SetOutputFiles(module.doctagPaths, ".doctags")
	}
	for _, scopeName := range android.SortedKeys(scopeByName) {
		paths := module.findScopePaths(scopeByName[scopeName])
		if paths == nil {
			continue
		}
		componentToOutput := map[string]android.OptionalPath{
			stubsSourceComponentName:   paths.stubsSrcJar,
			apiTxtComponentName:        paths.currentApiFilePath,
			removedApiTxtComponentName: paths.removedApiFilePath,
			annotationsComponentName:   paths.annotationsZip,
		}
		for _, component := range android.SortedKeys(componentToOutput) {
			if componentToOutput[component].Valid() {
				ctx.SetOutputFiles(android.Paths{componentToOutput[component].Path()}, "."+scopeName+"."+component)
			}
		}
	}
}

func (c *commonToSdkLibraryAndImport) getScopePathsCreateIfNeeded(scope *apiScope) *scopePaths {
	if c.scopePaths == nil {
		c.scopePaths = make(map[*apiScope]*scopePaths)
	}
	paths := c.scopePaths[scope]
	if paths == nil {
		paths = &scopePaths{}
		c.scopePaths[scope] = paths
	}

	return paths
}

func (c *commonToSdkLibraryAndImport) findScopePaths(scope *apiScope) *scopePaths {
	if c.scopePaths == nil {
		return nil
	}

	return c.scopePaths[scope]
}

// If this does not support the requested api scope then find the closest available
// scope it does support. Returns nil if no such scope is available.
func (c *commonToSdkLibraryAndImport) findClosestScopePath(scope *apiScope) *scopePaths {
	for s := scope; s != nil; s = s.canAccess {
		if paths := c.findScopePaths(s); paths != nil {
			return paths
		}
	}

	// This should never happen outside tests as public should be the base scope for every
	// scope and is enabled by default.
	return nil
}

// sdkKindToApiScope maps from android.SdkKind to apiScope.
func sdkKindToApiScope(kind android.SdkKind) *apiScope {
	var apiScope *apiScope
	switch kind {
	case android.SdkSystem:
		apiScope = apiScopeSystem
	case android.SdkModule:
		apiScope = apiScopeModuleLib
	case android.SdkTest:
		apiScope = apiScopeTest
	case android.SdkSystemServer:
		apiScope = apiScopeSystemServer
	default:
		apiScope = apiScopePublic
	}
	return apiScope
}

func (c *commonToSdkLibraryAndImport) sdkComponentPropertiesForChildLibrary() interface{} {
	componentProps := &struct {
		SdkLibraryName              *string
		SdkLibraryToImplicitlyTrack *string
	}{}

	namePtr := proptools.StringPtr(c.module.RootLibraryName())
	componentProps.SdkLibraryName = namePtr

	if c.sharedLibrary() {
		// Mark the stubs library as being components of this java_sdk_library so that
		// any app that includes code which depends (directly or indirectly) on the stubs
		// library will have the appropriate <uses-library> invocation inserted into its
		// manifest if necessary.
		componentProps.SdkLibraryToImplicitlyTrack = namePtr
	}

	return componentProps
}

func (c *commonToSdkLibraryAndImport) sharedLibrary() bool {
	return proptools.BoolDefault(c.commonSdkLibraryProperties.Shared_library, true)
}

// Check if the stub libraries should be compiled for dex
func (c *commonToSdkLibraryAndImport) stubLibrariesCompiledForDex() bool {
	// Always compile the dex file files for the stub libraries if they will be used on the
	// bootclasspath.
	return !c.sharedLibrary()
}

// Properties related to the use of a module as an component of a java_sdk_library.
type SdkLibraryComponentProperties struct {
	// The name of the java_sdk_library/_import module.
	SdkLibraryName *string `blueprint:"mutated"`

	// The name of the java_sdk_library/_import to add to a <uses-library> entry
	// in the AndroidManifest.xml of any Android app that includes code that references
	// this module. If not set then no java_sdk_library/_import is tracked.
	SdkLibraryToImplicitlyTrack *string `blueprint:"mutated"`
}

// Structure to be embedded in a module struct that needs to support the
// SdkLibraryComponentDependency interface.
type EmbeddableSdkLibraryComponent struct {
	sdkLibraryComponentProperties SdkLibraryComponentProperties
}

func (e *EmbeddableSdkLibraryComponent) initSdkLibraryComponent(module android.Module) {
	module.AddProperties(&e.sdkLibraryComponentProperties)
}

// to satisfy SdkLibraryComponentDependency
func (e *EmbeddableSdkLibraryComponent) SdkLibraryName() *string {
	return e.sdkLibraryComponentProperties.SdkLibraryName
}

// to satisfy SdkLibraryComponentDependency
func (e *EmbeddableSdkLibraryComponent) OptionalSdkLibraryImplementation() *string {
	// For shared libraries, this is the same as the SDK library name. If a Java library or app
	// depends on a component library (e.g. a stub library) it still needs to know the name of the
	// run-time library and the corresponding module that provides the implementation. This name is
	// passed to manifest_fixer (to be added to AndroidManifest.xml) and added to CLC (to be used
	// in dexpreopt).
	//
	// For non-shared SDK (component or not) libraries this returns `nil`, as they are not
	// <uses-library> and should not be added to the manifest or to CLC.
	return e.sdkLibraryComponentProperties.SdkLibraryToImplicitlyTrack
}

// Implemented by modules that are (or possibly could be) a component of a java_sdk_library
// (including the java_sdk_library) itself.
type SdkLibraryComponentDependency interface {
	UsesLibraryDependency

	// SdkLibraryName returns the name of the java_sdk_library/_import module.
	SdkLibraryName() *string

	// The name of the implementation library for the optional SDK library or nil, if there isn't one.
	OptionalSdkLibraryImplementation() *string
}

// Make sure that all the module types that are components of java_sdk_library/_import
// and which can be referenced (directly or indirectly) from an android app implement
// the SdkLibraryComponentDependency interface.
var _ SdkLibraryComponentDependency = (*Library)(nil)
var _ SdkLibraryComponentDependency = (*Import)(nil)
var _ SdkLibraryComponentDependency = (*SdkLibrary)(nil)
var _ SdkLibraryComponentDependency = (*SdkLibraryImport)(nil)

type SdkLibraryInfo struct {
	// GeneratingLibs is the names of the library modules that this sdk library
	// generates. Note that this only includes the name of the modules that other modules can
	// depend on, and is not a holistic list of generated modules.
	GeneratingLibs []string

	// Map of sdk kind to the dex jar for the "everything" stubs.
	// It is needed by the hiddenapi processing tool which processes dex files.
	EverythingStubDexJarPaths map[android.SdkKind]OptionalDexJarPath

	// Map of sdk kind to the dex jar for the "exportable" stubs.
	// It is needed by the hiddenapi processing tool which processes dex files.
	ExportableStubDexJarPaths map[android.SdkKind]OptionalDexJarPath

	// Map of sdk kind to the optional path to the removed.txt file.
	RemovedTxtFiles map[android.SdkKind]android.OptionalPath

	// Whether if this can be used as a shared library.
	SharedLibrary bool

	Prebuilt bool
}

var SdkLibraryInfoProvider = blueprint.NewProvider[SdkLibraryInfo]()

func getGeneratingLibs(ctx android.ModuleContext, sdkVersion android.SdkSpec, sdkLibraryModuleName string, sdkInfo SdkLibraryInfo) []string {
	apiLevel := sdkVersion.ApiLevel
	if apiLevel.IsPreview() {
		return sdkInfo.GeneratingLibs
	}

	generatingPrebuilts := []string{}
	for _, apiScope := range AllApiScopes {
		scopePrebuiltModuleName := prebuiltApiModuleName("sdk", sdkLibraryModuleName, apiScope.name, apiLevel.String())
		if ctx.OtherModuleExists(scopePrebuiltModuleName) {
			generatingPrebuilts = append(generatingPrebuilts, scopePrebuiltModuleName)
		}
	}
	return generatingPrebuilts
}

type SdkLibrary struct {
	Library

	sdkLibraryProperties sdkLibraryProperties

	// Map from api scope to the scope specific property structure.
	scopeToProperties map[*apiScope]*ApiScopeProperties

	commonToSdkLibraryAndImport

	apexSystemServerDexpreoptInstalls []DexpreopterInstall
	apexSystemServerDexJars           android.Paths
}

func (module *SdkLibrary) generateTestAndSystemScopesByDefault() bool {
	return module.sdkLibraryProperties.Generate_system_and_test_apis
}

var _ UsesLibraryDependency = (*SdkLibrary)(nil)

// To satisfy the UsesLibraryDependency interface
func (module *SdkLibrary) DexJarBuildPath(ctx android.ModuleErrorfContext) OptionalDexJarPath {
	if module.implLibraryInfo != nil {
		return module.implLibraryInfo.DexJarFile
	}
	return makeUnsetDexJarPath()
}

// To satisfy the UsesLibraryDependency interface
func (module *SdkLibrary) DexJarInstallPath() android.Path {
	if module.implLibraryInfo != nil {
		return module.implLibraryInfo.InstallFile
	}
	return nil
}

func (module *SdkLibrary) getGeneratedApiScopes(ctx android.EarlyModuleContext) apiScopes {
	// Check to see if any scopes have been explicitly enabled. If any have then all
	// must be.
	anyScopesExplicitlyEnabled := false
	for _, scope := range AllApiScopes {
		scopeProperties := module.scopeToProperties[scope]
		if scopeProperties.Enabled != nil {
			anyScopesExplicitlyEnabled = true
			break
		}
	}

	var generatedScopes apiScopes
	enabledScopes := make(map[*apiScope]struct{})
	for _, scope := range AllApiScopes {
		scopeProperties := module.scopeToProperties[scope]
		// If any scopes are explicitly enabled then ignore the legacy enabled status.
		// This is to ensure that any new usages of this module type do not rely on legacy
		// behaviour.
		defaultEnabledStatus := false
		if anyScopesExplicitlyEnabled {
			defaultEnabledStatus = scope.defaultEnabledStatus
		} else {
			defaultEnabledStatus = scope.legacyEnabledStatus(module)
		}
		enabled := proptools.BoolDefault(scopeProperties.Enabled, defaultEnabledStatus)
		if enabled {
			enabledScopes[scope] = struct{}{}
			generatedScopes = append(generatedScopes, scope)
		}
	}

	// Now check to make sure that any scope that is extended by an enabled scope is also
	// enabled.
	for _, scope := range AllApiScopes {
		if _, ok := enabledScopes[scope]; ok {
			extends := scope.extends
			if extends != nil {
				if _, ok := enabledScopes[extends]; !ok {
					ctx.ModuleErrorf("enabled api scope %q depends on disabled scope %q", scope, extends)
				}
			}
		}
	}

	return generatedScopes
}

var _ android.ModuleWithMinSdkVersionCheck = (*SdkLibrary)(nil)

func (module *SdkLibrary) CheckMinSdkVersion(ctx android.ModuleContext) {
	CheckMinSdkVersion(ctx, &module.Library)
}

func CheckMinSdkVersion(ctx android.ModuleContext, module *Library) {
	android.CheckMinSdkVersion(ctx, module.MinSdkVersion(ctx), func(c android.BaseModuleContext, do android.PayloadDepsCallback) {
		ctx.WalkDepsProxy(func(child, parent android.ModuleProxy) bool {
			isExternal := !android.IsDepInSameApex(ctx, module, child)
			if am, ok := android.OtherModuleProvider(ctx, child, android.CommonModuleInfoProvider); ok && am.IsApexModule {
				if !do(ctx, parent, child, isExternal) {
					return false
				}
			}
			return !isExternal
		})
	})
}

type sdkLibraryComponentTag struct {
	blueprint.BaseDependencyTag
	name string
}

// Mark this tag so dependencies that use it are excluded from visibility enforcement.
func (t sdkLibraryComponentTag) ExcludeFromVisibilityEnforcement() {}

var xmlPermissionsFileTag = sdkLibraryComponentTag{name: "xml-permissions-file"}

func IsXmlPermissionsFileDepTag(depTag blueprint.DependencyTag) bool {
	if dt, ok := depTag.(sdkLibraryComponentTag); ok {
		return dt == xmlPermissionsFileTag
	}
	return false
}

var implLibraryTag = sdkLibraryComponentTag{name: "impl-library"}

var _ android.InstallNeededDependencyTag = sdkLibraryComponentTag{}

func (t sdkLibraryComponentTag) InstallDepNeeded() bool {
	return t.name == "xml-permissions-file" || t.name == "impl-library"
}

// Add the dependencies on the child modules in the component deps mutator.
func (module *SdkLibrary) ComponentDepsMutator(ctx android.BottomUpMutatorContext) {
	for _, apiScope := range module.getGeneratedApiScopes(ctx) {
		// Add dependencies to the stubs library
		stubModuleName := module.stubsLibraryModuleName(apiScope)
		ctx.AddVariationDependencies(nil, apiScope.everythingStubsTag, stubModuleName)

		exportableStubModuleName := module.exportableStubsLibraryModuleName(apiScope)
		ctx.AddVariationDependencies(nil, apiScope.exportableStubsTag, exportableStubModuleName)

		// Add a dependency on the stubs source in order to access both stubs source and api information.
		ctx.AddVariationDependencies(nil, apiScope.stubsSourceAndApiTag, module.droidstubsModuleName(apiScope))

		if module.compareAgainstLatestApi(apiScope) {
			// Add dependencies on the latest finalized version of the API .txt file.
			latestApiModuleName := module.latestApiModuleName(apiScope)
			ctx.AddDependency(module, apiScope.latestApiModuleTag, latestApiModuleName)

			// Add dependencies on the latest finalized version of the remove API .txt file.
			latestRemovedApiModuleName := module.latestRemovedApiModuleName(apiScope)
			ctx.AddDependency(module, apiScope.latestRemovedApiModuleTag, latestRemovedApiModuleName)
		}
	}

	if module.requiresRuntimeImplementationLibrary() {
		// Add dependency to the rule for generating the implementation library.
		ctx.AddDependency(module, implLibraryTag, module.implLibraryModuleName())

		if module.sharedLibrary() {
			// Add dependency to the rule for generating the xml permissions file
			ctx.AddDependency(module, xmlPermissionsFileTag, module.xmlPermissionsModuleName())
		}
	}
}

// Add other dependencies as normal.
func (module *SdkLibrary) DepsMutator(ctx android.BottomUpMutatorContext) {
	// If the module does not create an implementation library or defaults to stubs,
	// mark the top level sdk library as stubs module as the module will provide stubs via
	// "magic" when listed as a dependency in the Android.bp files.
	notCreateImplLib := proptools.Bool(module.sdkLibraryProperties.Api_only)
	preferStubs := proptools.Bool(module.sdkLibraryProperties.Default_to_stubs)
	module.properties.Is_stubs_module = proptools.BoolPtr(notCreateImplLib || preferStubs)

	var missingApiModules []string
	for _, apiScope := range module.getGeneratedApiScopes(ctx) {
		if apiScope.unstable {
			continue
		}
		if m := module.latestApiModuleName(apiScope); !ctx.OtherModuleExists(m) {
			missingApiModules = append(missingApiModules, m)
		}
		if m := module.latestRemovedApiModuleName(apiScope); !ctx.OtherModuleExists(m) {
			missingApiModules = append(missingApiModules, m)
		}
		if m := module.latestIncompatibilitiesModuleName(apiScope); !ctx.OtherModuleExists(m) {
			missingApiModules = append(missingApiModules, m)
		}
	}
	if len(missingApiModules) != 0 && !module.sdkLibraryProperties.Unsafe_ignore_missing_latest_api {
		m := module.Name() + " is missing tracking files for previously released library versions.\n"
		m += "You need to do one of the following:\n"
		m += "- Add `unsafe_ignore_missing_latest_api: true` to your blueprint (to disable compat tracking)\n"
		m += "- Add a set of prebuilt txt files representing the last released version of this library for compat checking.\n"
		m += "  (the current set of API files can be used as a seed for this compatibility tracking\n"
		m += "\n"
		m += "The following filegroup modules are missing:\n  "
		m += strings.Join(missingApiModules, "\n  ") + "\n"
		m += "Please see the documentation of the prebuilt_apis module type (and a usage example in prebuilts/sdk) for a convenient way to generate these."
		ctx.ModuleErrorf(m)
	}
}

func (module *SdkLibrary) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	if disableSourceApexVariant(ctx) {
		// Prebuilts are active, do not create the installation rules for the source javalib.
		// Even though the source javalib is not used, we need to hide it to prevent duplicate installation rules.
		// TODO (b/331665856): Implement a principled solution for this.
		module.HideFromMake()
		module.SkipInstall()
	}

	module.stem = proptools.StringDefault(module.overridableProperties.Stem, ctx.ModuleName())

	module.provideHiddenAPIPropertyInfo(ctx)

	// Collate the components exported by this module. All scope specific modules are exported but
	// the impl and xml component modules are not.
	exportedComponents := map[string]struct{}{}
	var implLib android.ModuleProxy
	// Record the paths to the header jars of the library (stubs and impl).
	// When this java_sdk_library is depended upon from others via "libs" property,
	// the recorded paths will be returned depending on the link type of the caller.
	ctx.VisitDirectDepsProxy(func(to android.ModuleProxy) {
		tag := ctx.OtherModuleDependencyTag(to)

		// Extract information from any of the scope specific dependencies.
		if scopeTag, ok := tag.(scopeDependencyTag); ok {
			apiScope := scopeTag.apiScope
			scopePaths := module.getScopePathsCreateIfNeeded(apiScope)

			// Extract information from the dependency. The exact information extracted
			// is determined by the nature of the dependency which is determined by the tag.
			scopeTag.extractDepInfo(ctx, to, scopePaths)

			exportedComponents[ctx.OtherModuleName(to)] = struct{}{}

			ctx.Phony(ctx.ModuleName(), scopePaths.stubsHeaderPath...)
		}

		if tag == implLibraryTag {
			if dep, ok := android.OtherModuleProvider(ctx, to, JavaInfoProvider); ok {
				module.implLibraryHeaderJars = append(module.implLibraryHeaderJars, dep.HeaderJars...)
				module.implLibraryInfo = dep
				implLib = to
			}
		}
	})

	sdkLibInfo := module.generateCommonBuildActions(ctx)
	apexInfo, _ := android.ModuleProvider(ctx, android.ApexInfoProvider)
	if !apexInfo.IsForPlatform() {
		module.hideApexVariantFromMake = true
	}

	if module.implLibraryInfo != nil {
		if ctx.Device() {
			module.classesJarPaths = module.implLibraryInfo.ImplementationJars
			module.bootDexJarPath = module.implLibraryInfo.BootDexJarPath
			module.uncompressDexState = module.implLibraryInfo.UncompressDexState
			module.active = module.implLibraryInfo.Active
		}

		module.outputFile = module.implLibraryInfo.OutputFile
		module.dexJarFile = makeDexJarPathFromPath(module.implLibraryInfo.DexJarFile.Path())
		module.headerJarFile = module.implLibraryInfo.HeaderJars[0]
		module.implementationAndResourcesJar = module.implLibraryInfo.ImplementationAndResourcesJars[0]
		module.apexSystemServerDexpreoptInstalls = module.implLibraryInfo.DexpreopterInfo.ApexSystemServerDexpreoptInstalls
		module.apexSystemServerDexJars = module.implLibraryInfo.DexpreopterInfo.ApexSystemServerDexJars
		module.dexpreopter.configPath = module.implLibraryInfo.ConfigPath
		module.dexpreopter.outputProfilePathOnHost = module.implLibraryInfo.DexpreopterInfo.OutputProfilePathOnHost

		// Properties required for Library.AndroidMkEntries
		module.logtagsSrcs = module.implLibraryInfo.LogtagsSrcs
		module.dexpreopter.builtInstalled = module.implLibraryInfo.BuiltInstalled
		module.jacocoReportClassesFile = module.implLibraryInfo.JacocoReportClassesFile
		module.dexer.proguardDictionary = module.implLibraryInfo.ProguardDictionary
		module.dexer.proguardUsageZip = module.implLibraryInfo.ProguardUsageZip
		module.linter.reports = module.implLibraryInfo.LinterReports

		if lintInfo, ok := android.OtherModuleProvider(ctx, implLib, LintProvider); ok {
			android.SetProvider(ctx, LintProvider, lintInfo)
		}

		if !module.Host() {
			module.hostdexInstallFile = module.implLibraryInfo.HostdexInstallFile
		}

		if installFilesInfo, ok := android.OtherModuleProvider(ctx, implLib, android.InstallFilesProvider); ok {
			if installFilesInfo.CheckbuildTarget != nil {
				ctx.CheckbuildFile(installFilesInfo.CheckbuildTarget)
			}
		}
	}

	// Make the set of components exported by this module available for use elsewhere.
	exportedComponentInfo := android.ExportedComponentsInfo{Components: android.SortedKeys(exportedComponents)}
	android.SetProvider(ctx, android.ExportedComponentsInfoProvider, exportedComponentInfo)

	// Provide additional information for inclusion in an sdk's generated .info file.
	additionalSdkInfo := map[string]interface{}{}
	additionalSdkInfo["dist_stem"] = module.distStem()
	baseModuleName := module.distStem()
	scopes := map[string]interface{}{}
	additionalSdkInfo["scopes"] = scopes
	for scope, scopePaths := range module.scopePaths {
		scopeInfo := map[string]interface{}{}
		scopes[scope.name] = scopeInfo
		scopeInfo["current_api"] = scope.snapshotRelativeCurrentApiTxtPath(baseModuleName)
		scopeInfo["removed_api"] = scope.snapshotRelativeRemovedApiTxtPath(baseModuleName)
		if p := scopePaths.latestApiPaths; len(p) > 0 {
			// The last path in the list is the one that applies to this scope, the
			// preceding ones, if any, are for the scope(s) that it extends.
			scopeInfo["latest_api"] = p[len(p)-1].String()
		}
		if p := scopePaths.latestRemovedApiPaths; len(p) > 0 {
			// The last path in the list is the one that applies to this scope, the
			// preceding ones, if any, are for the scope(s) that it extends.
			scopeInfo["latest_removed_api"] = p[len(p)-1].String()
		}
	}
	android.SetProvider(ctx, android.AdditionalSdkInfoProvider, android.AdditionalSdkInfo{additionalSdkInfo})
	module.setOutputFiles(ctx)

	var generatingLibs []string
	for _, apiScope := range AllApiScopes {
		if _, ok := module.scopePaths[apiScope]; ok {
			generatingLibs = append(generatingLibs, module.stubsLibraryModuleName(apiScope))
		}
	}

	if module.requiresRuntimeImplementationLibrary() && module.implLibraryInfo != nil {
		generatingLibs = append(generatingLibs, module.implLibraryModuleName())
		setOutputFilesFromJavaInfo(ctx, module.implLibraryInfo)
	}

	javaInfo := &JavaInfo{
		JacocoReportClassesFile: module.jacocoReportClassesFile,
	}
	setExtraJavaInfo(ctx, ctx.Module(), javaInfo)
	android.SetProvider(ctx, JavaInfoProvider, javaInfo)

	sdkLibInfo.GeneratingLibs = generatingLibs
	sdkLibInfo.Prebuilt = false
	android.SetProvider(ctx, SdkLibraryInfoProvider, sdkLibInfo)
}

func setOutputFilesFromJavaInfo(ctx android.ModuleContext, info *JavaInfo) {
	ctx.SetOutputFiles(append(android.PathsIfNonNil(info.OutputFile), info.ExtraOutputFiles...), "")
	ctx.SetOutputFiles(android.PathsIfNonNil(info.OutputFile), android.DefaultDistTag)
	ctx.SetOutputFiles(info.ImplementationAndResourcesJars, ".jar")
	ctx.SetOutputFiles(info.HeaderJars, ".hjar")
	if info.ProguardDictionary.Valid() {
		ctx.SetOutputFiles(android.Paths{info.ProguardDictionary.Path()}, ".proguard_map")
	}
	ctx.SetOutputFiles(info.GeneratedSrcjars, ".generated_srcjars")
}

func (module *SdkLibrary) ApexSystemServerDexpreoptInstalls() []DexpreopterInstall {
	return module.apexSystemServerDexpreoptInstalls
}

func (module *SdkLibrary) ApexSystemServerDexJars() android.Paths {
	return module.apexSystemServerDexJars
}

func (module *SdkLibrary) AndroidMkEntries() []android.AndroidMkEntries {
	if !module.requiresRuntimeImplementationLibrary() {
		return nil
	}
	entriesList := module.Library.AndroidMkEntries()
	entries := &entriesList[0]
	entries.Required = append(entries.Required, module.implLibraryModuleName())
	if module.sharedLibrary() {
		entries.Required = append(entries.Required, module.xmlPermissionsModuleName())
	}
	return entriesList
}

// The dist path of the stub artifacts
func (module *SdkLibrary) apiDistPath(apiScope *apiScope) string {
	return path.Join("apistubs", module.distGroup(), apiScope.name)
}

// Get the sdk version for use when compiling the stubs library.
func (module *SdkLibrary) sdkVersionForStubsLibrary(mctx android.EarlyModuleContext, apiScope *apiScope) string {
	scopeProperties := module.scopeToProperties[apiScope]
	if scopeProperties.Sdk_version != nil {
		return proptools.String(scopeProperties.Sdk_version)
	}

	sdkDep := decodeSdkDep(mctx, android.SdkContext(&module.Library))
	if sdkDep.hasStandardLibs() {
		// If building against a standard sdk then use the sdk version appropriate for the scope.
		return apiScope.sdkVersion
	} else {
		// Otherwise, use no system module.
		return "none"
	}
}

func (module *SdkLibrary) distStem() string {
	return proptools.StringDefault(module.sdkLibraryProperties.Dist_stem, module.BaseModuleName())
}

// distGroup returns the subdirectory of the dist path of the stub artifacts.
func (module *SdkLibrary) distGroup() string {
	return proptools.StringDefault(module.sdkLibraryProperties.Dist_group, "unknown")
}

func latestPrebuiltApiModuleName(name string, apiScope *apiScope) string {
	return PrebuiltApiModuleName(name, apiScope.name, "latest")
}

func latestPrebuiltApiCombinedModuleName(name string, apiScope *apiScope) string {
	return PrebuiltApiCombinedModuleName(name, apiScope.name, "latest")
}

func (module *SdkLibrary) latestApiFilegroupName(apiScope *apiScope) string {
	return ":" + module.latestApiModuleName(apiScope)
}

func (module *SdkLibrary) latestApiModuleName(apiScope *apiScope) string {
	return latestPrebuiltApiCombinedModuleName(module.distStem(), apiScope)
}

func (module *SdkLibrary) latestRemovedApiFilegroupName(apiScope *apiScope) string {
	return ":" + module.latestRemovedApiModuleName(apiScope)
}

func (module *SdkLibrary) latestRemovedApiModuleName(apiScope *apiScope) string {
	return latestPrebuiltApiCombinedModuleName(module.distStem()+"-removed", apiScope)
}

func (module *SdkLibrary) latestIncompatibilitiesFilegroupName(apiScope *apiScope) string {
	return ":" + module.latestIncompatibilitiesModuleName(apiScope)
}

func (module *SdkLibrary) latestIncompatibilitiesModuleName(apiScope *apiScope) string {
	return latestPrebuiltApiModuleName(module.distStem()+"-incompatibilities", apiScope)
}

// The listed modules' stubs contents do not match the corresponding txt files,
// but require additional api contributions to generate the full stubs.
// This method returns the name of the additional api contribution module
// for corresponding sdk_library modules.
func (module *SdkLibrary) apiLibraryAdditionalApiContribution() string {
	if val, ok := apiLibraryAdditionalProperties[module.Name()]; ok {
		return val
	}
	return ""
}

func childModuleVisibility(childVisibility []string) []string {
	if childVisibility == nil {
		// No child visibility set. The child will use the visibility of the sdk_library.
		return nil
	}

	// Prepend an override to ignore the sdk_library's visibility, and rely on the child visibility.
	var visibility []string
	visibility = append(visibility, "//visibility:override")
	visibility = append(visibility, childVisibility...)
	return visibility
}

func (module *SdkLibrary) compareAgainstLatestApi(apiScope *apiScope) bool {
	return !(apiScope.unstable || module.sdkLibraryProperties.Unsafe_ignore_missing_latest_api)
}

// Implements android.ApexModule
func (m *SdkLibrary) GetDepInSameApexChecker() android.DepInSameApexChecker {
	return SdkLibraryDepInSameApexChecker{}
}

type SdkLibraryDepInSameApexChecker struct {
	android.BaseDepInSameApexChecker
}

func (m SdkLibraryDepInSameApexChecker) OutgoingDepIsInSameApex(tag blueprint.DependencyTag) bool {
	if tag == xmlPermissionsFileTag {
		return true
	}
	if tag == implLibraryTag {
		return true
	}
	return depIsInSameApex(tag)
}

// Implements android.ApexModule
func (module *SdkLibrary) UniqueApexVariations() bool {
	return module.uniqueApexVariations()
}

func (module *SdkLibrary) ModuleBuildFromTextStubs() bool {
	return proptools.BoolDefault(module.sdkLibraryProperties.Build_from_text_stub, true)
}

var javaSdkLibrariesKey = android.NewOnceKey("javaSdkLibraries")

func javaSdkLibraries(config android.Config) *[]string {
	return config.Once(javaSdkLibrariesKey, func() interface{} {
		return &[]string{}
	}).(*[]string)
}

func (module *SdkLibrary) getApiDir() string {
	return proptools.StringDefault(module.sdkLibraryProperties.Api_dir, "api")
}

// For a java_sdk_library module, create internal modules for stubs, docs,
// runtime libs and xml file. If requested, the stubs and docs are created twice
// once for public API level and once for system API level
func (module *SdkLibrary) CreateInternalModules(mctx android.DefaultableHookContext) {
	if len(module.properties.Srcs) == 0 {
		mctx.PropertyErrorf("srcs", "java_sdk_library must specify srcs")
		return
	}

	// If this builds against standard libraries (i.e. is not part of the core libraries)
	// then assume it provides both system and test apis.
	sdkDep := decodeSdkDep(mctx, android.SdkContext(&module.Library))
	hasSystemAndTestApis := sdkDep.hasStandardLibs()
	module.sdkLibraryProperties.Generate_system_and_test_apis = hasSystemAndTestApis

	missingCurrentApi := false

	generatedScopes := module.getGeneratedApiScopes(mctx)

	apiDir := module.getApiDir()
	for _, scope := range generatedScopes {
		for _, api := range []string{"current.txt", "removed.txt"} {
			path := path.Join(mctx.ModuleDir(), apiDir, scope.apiFilePrefix+api)
			p := android.ExistentPathForSource(mctx, path)
			if !p.Valid() {
				if mctx.Config().AllowMissingDependencies() {
					mctx.AddMissingDependencies([]string{path})
				} else {
					mctx.ModuleErrorf("Current api file %#v doesn't exist", path)
					missingCurrentApi = true
				}
			}
		}
	}

	if missingCurrentApi {
		script := "build/soong/scripts/gen-java-current-api-files.sh"
		p := android.ExistentPathForSource(mctx, script)

		if !p.Valid() {
			panic(fmt.Sprintf("script file %s doesn't exist", script))
		}

		mctx.ModuleErrorf("One or more current api files are missing. "+
			"You can update them by:\n"+
			"%s %q %s && m update-api",
			script, filepath.Join(mctx.ModuleDir(), apiDir),
			strings.Join(generatedScopes.Strings(func(s *apiScope) string { return s.apiFilePrefix }), " "))
		return
	}

	for _, scope := range generatedScopes {
		// Use the stubs source name for legacy reasons.
		module.createDroidstubs(mctx, scope, module.droidstubsModuleName(scope), scope.droidstubsArgs)

		module.createFromSourceStubsLibrary(mctx, scope)
		module.createExportableFromSourceStubsLibrary(mctx, scope)

		if mctx.Config().BuildFromTextStub() && module.ModuleBuildFromTextStubs() {
			module.createApiLibrary(mctx, scope)
		}
		module.createTopLevelStubsLibrary(mctx, scope)
		module.createTopLevelExportableStubsLibrary(mctx, scope)
	}

	if module.requiresRuntimeImplementationLibrary() {
		// Create child module to create an implementation library.
		//
		// This temporarily creates a second implementation library that can be explicitly
		// referenced.
		//
		// TODO(b/156618935) - update comment once only one implementation library is created.
		module.createImplLibrary(mctx)

		// Only create an XML permissions file that declares the library as being usable
		// as a shared library if required.
		if module.sharedLibrary() {
			module.createXmlFile(mctx)
		}

		// record java_sdk_library modules so that they are exported to make
		javaSdkLibraries := javaSdkLibraries(mctx.Config())
		javaSdkLibrariesLock.Lock()
		defer javaSdkLibrariesLock.Unlock()
		*javaSdkLibraries = append(*javaSdkLibraries, module.BaseModuleName())
	}

	// Add the impl_only_libs and impl_only_static_libs *after* we're done using them in submodules.
	module.properties.Libs = append(module.properties.Libs, module.sdkLibraryProperties.Impl_only_libs...)
	module.properties.Static_libs.AppendSimpleValue(module.sdkLibraryProperties.Impl_only_static_libs)
}

func (module *SdkLibrary) InitSdkLibraryProperties() {
	module.addHostAndDeviceProperties()
	module.AddProperties(&module.sdkLibraryProperties)

	module.initSdkLibraryComponent(module)

	module.properties.Installable = proptools.BoolPtr(true)
	module.deviceProperties.IsSDKLibrary = true
}

func (module *SdkLibrary) requiresRuntimeImplementationLibrary() bool {
	return !proptools.Bool(module.sdkLibraryProperties.Api_only)
}

func moduleStubLinkType(j *Module) (stub bool, ret sdkLinkType) {
	kind := android.ToSdkKind(proptools.String(j.properties.Stub_contributing_api))
	switch kind {
	case android.SdkPublic:
		return true, javaSdk
	case android.SdkSystem:
		return true, javaSystem
	case android.SdkModule:
		return true, javaModule
	case android.SdkTest:
		return true, javaSystem
	case android.SdkSystemServer:
		return true, javaSystemServer
	// Default value for all modules other than java_sdk_library-generated stub submodules
	case android.SdkInvalid:
		return false, javaPlatform
	default:
		panic(fmt.Sprintf("stub_contributing_api set as an unsupported sdk kind %s", kind.String()))
	}
}

// java_sdk_library is a special Java library that provides optional platform APIs to apps.
// In practice, it can be viewed as a combination of several modules: 1) stubs library that clients
// are linked against to, 2) droiddoc module that internally generates API stubs source files,
// 3) the real runtime shared library that implements the APIs, and 4) XML file for adding
// the runtime lib to the classpath at runtime if requested via <uses-library>.
func SdkLibraryFactory() android.Module {
	module := &SdkLibrary{}

	// Initialize information common between source and prebuilt.
	module.initCommon(module)

	module.InitSdkLibraryProperties()
	android.InitApexModule(module)
	InitJavaModule(module, android.HostAndDeviceSupported)

	// Initialize the map from scope to scope specific properties.
	scopeToProperties := make(map[*apiScope]*ApiScopeProperties)
	for _, scope := range AllApiScopes {
		scopeToProperties[scope] = scope.scopeSpecificProperties(module)
	}
	module.scopeToProperties = scopeToProperties

	// Add the properties containing visibility rules so that they are checked.
	android.AddVisibilityProperty(module, "impl_library_visibility", &module.sdkLibraryProperties.Impl_library_visibility)
	android.AddVisibilityProperty(module, "stubs_library_visibility", &module.sdkLibraryProperties.Stubs_library_visibility)
	android.AddVisibilityProperty(module, "stubs_source_visibility", &module.sdkLibraryProperties.Stubs_source_visibility)

	module.SetDefaultableHook(func(ctx android.DefaultableHookContext) {
		// If no implementation is required then it cannot be used as a shared library
		// either.
		if !module.requiresRuntimeImplementationLibrary() {
			// If shared_library has been explicitly set to true then it is incompatible
			// with api_only: true.
			if proptools.Bool(module.commonSdkLibraryProperties.Shared_library) {
				ctx.PropertyErrorf("api_only/shared_library", "inconsistent settings, shared_library and api_only cannot both be true")
			}
			// Set shared_library: false.
			module.commonSdkLibraryProperties.Shared_library = proptools.BoolPtr(false)
		}

		if module.initCommonAfterDefaultsApplied() {
			module.CreateInternalModules(ctx)
		}
	})
	return module
}

//
// SDK library prebuilts
//

// Properties associated with each api scope.
type sdkLibraryScopeProperties struct {
	Jars []string `android:"path"`

	Sdk_version *string

	// List of shared java libs that this module has dependencies to
	Libs []string

	// The stubs source.
	Stub_srcs []string `android:"path"`

	// The current.txt
	Current_api *string `android:"path"`

	// The removed.txt
	Removed_api *string `android:"path"`

	// Annotation zip
	Annotations *string `android:"path"`
}

type sdkLibraryImportProperties struct {
	// List of shared java libs, common to all scopes, that this module has
	// dependencies to
	Libs []string

	// If set to true, compile dex files for the stubs. Defaults to false.
	Compile_dex *bool

	// If not empty, classes are restricted to the specified packages and their sub-packages.
	Permitted_packages []string

	// Name of the source soong module that gets shadowed by this prebuilt
	// If unspecified, follows the naming convention that the source module of
	// the prebuilt is Name() without "prebuilt_" prefix
	Source_module_name *string
}

type SdkLibraryImport struct {
	android.ModuleBase
	android.DefaultableModuleBase
	prebuilt android.Prebuilt
	android.ApexModuleBase

	hiddenAPI
	dexpreopter

	properties sdkLibraryImportProperties

	// Map from api scope to the scope specific property structure.
	scopeProperties map[*apiScope]*sdkLibraryScopeProperties

	commonToSdkLibraryAndImport

	// Build path to the dex implementation jar obtained from the prebuilt_apex, if any.
	dexJarFile    OptionalDexJarPath
	dexJarFileErr error

	// Expected install file path of the source module(sdk_library)
	// or dex implementation jar obtained from the prebuilt_apex, if any.
	installFile android.Path
}

// The type of a structure that contains a field of type sdkLibraryScopeProperties
// for each apiscope in allApiScopes, e.g. something like:
//
//	struct {
//	  Public sdkLibraryScopeProperties
//	  System sdkLibraryScopeProperties
//	  ...
//	}
var allScopeStructType = createAllScopePropertiesStructType()

// Dynamically create a structure type for each apiscope in allApiScopes.
func createAllScopePropertiesStructType() reflect.Type {
	var fields []reflect.StructField
	for _, apiScope := range AllApiScopes {
		field := reflect.StructField{
			Name: apiScope.fieldName,
			Type: reflect.TypeOf(sdkLibraryScopeProperties{}),
		}
		fields = append(fields, field)
	}

	return reflect.StructOf(fields)
}

// Create an instance of the scope specific structure type and return a map
// from apiscope to a pointer to each scope specific field.
func createPropertiesInstance() (interface{}, map[*apiScope]*sdkLibraryScopeProperties) {
	allScopePropertiesPtr := reflect.New(allScopeStructType)
	allScopePropertiesStruct := allScopePropertiesPtr.Elem()
	scopeProperties := make(map[*apiScope]*sdkLibraryScopeProperties)

	for _, apiScope := range AllApiScopes {
		field := allScopePropertiesStruct.FieldByName(apiScope.fieldName)
		scopeProperties[apiScope] = field.Addr().Interface().(*sdkLibraryScopeProperties)
	}

	return allScopePropertiesPtr.Interface(), scopeProperties
}

// java_sdk_library_import imports a prebuilt java_sdk_library.
func sdkLibraryImportFactory() android.Module {
	module := &SdkLibraryImport{}

	allScopeProperties, scopeToProperties := createPropertiesInstance()
	module.scopeProperties = scopeToProperties
	module.AddProperties(&module.properties, allScopeProperties, &module.importDexpreoptProperties)

	// Initialize information common between source and prebuilt.
	module.initCommon(module)

	android.InitPrebuiltModule(module, &[]string{""})
	android.InitApexModule(module)
	InitJavaModule(module, android.HostAndDeviceSupported)

	module.SetDefaultableHook(func(mctx android.DefaultableHookContext) {
		if module.initCommonAfterDefaultsApplied() {
			module.createInternalModules(mctx)
		}
	})
	return module
}

var _ PermittedPackagesForUpdatableBootJars = (*SdkLibraryImport)(nil)

func (module *SdkLibraryImport) PermittedPackagesForUpdatableBootJars() []string {
	return module.properties.Permitted_packages
}

func (module *SdkLibraryImport) Prebuilt() *android.Prebuilt {
	return &module.prebuilt
}

func (module *SdkLibraryImport) Name() string {
	return module.prebuilt.Name(module.ModuleBase.Name())
}

func (module *SdkLibraryImport) BaseModuleName() string {
	return proptools.StringDefault(module.properties.Source_module_name, module.ModuleBase.Name())
}

func (module *SdkLibraryImport) createInternalModules(mctx android.DefaultableHookContext) {

	// If the build is configured to use prebuilts then force this to be preferred.
	if mctx.Config().AlwaysUsePrebuiltSdks() {
		module.prebuilt.ForcePrefer()
	}

	for apiScope, scopeProperties := range module.scopeProperties {
		if len(scopeProperties.Jars) == 0 {
			continue
		}

		module.createJavaImportForStubs(mctx, apiScope, scopeProperties)

		if len(scopeProperties.Stub_srcs) > 0 {
			module.createPrebuiltStubsSources(mctx, apiScope, scopeProperties)
		}

		if scopeProperties.Current_api != nil {
			module.createPrebuiltApiContribution(mctx, apiScope, scopeProperties)
		}
	}

	javaSdkLibraries := javaSdkLibraries(mctx.Config())
	javaSdkLibrariesLock.Lock()
	defer javaSdkLibrariesLock.Unlock()
	*javaSdkLibraries = append(*javaSdkLibraries, module.BaseModuleName())
}

// Add the dependencies on the child module in the component deps mutator so that it
// creates references to the prebuilt and not the source modules.
func (module *SdkLibraryImport) ComponentDepsMutator(ctx android.BottomUpMutatorContext) {
	for apiScope, scopeProperties := range module.scopeProperties {
		if len(scopeProperties.Jars) == 0 {
			continue
		}

		// Add dependencies to the prebuilt stubs library
		ctx.AddVariationDependencies(nil, apiScope.prebuiltStubsTag, android.PrebuiltNameFromSource(module.stubsLibraryModuleName(apiScope)))

		if len(scopeProperties.Stub_srcs) > 0 {
			// Add dependencies to the prebuilt stubs source library
			ctx.AddVariationDependencies(nil, apiScope.stubsSourceTag, android.PrebuiltNameFromSource(module.droidstubsModuleName(apiScope)))
		}
	}
}

// Add other dependencies as normal.
func (module *SdkLibraryImport) DepsMutator(ctx android.BottomUpMutatorContext) {

	implName := module.implLibraryModuleName()
	if ctx.OtherModuleExists(implName) {
		ctx.AddVariationDependencies(nil, implLibraryTag, implName)

		xmlPermissionsModuleName := module.xmlPermissionsModuleName()
		if module.sharedLibrary() && ctx.OtherModuleExists(xmlPermissionsModuleName) {
			// Add dependency to the rule for generating the xml permissions file
			ctx.AddDependency(module, xmlPermissionsFileTag, xmlPermissionsModuleName)
		}
	}
}

var _ android.ApexModule = (*SdkLibraryImport)(nil)

// Implements android.ApexModule
func (m *SdkLibraryImport) GetDepInSameApexChecker() android.DepInSameApexChecker {
	return SdkLibraryImportDepIsInSameApexChecker{}
}

type SdkLibraryImportDepIsInSameApexChecker struct {
	android.BaseDepInSameApexChecker
}

func (m SdkLibraryImportDepIsInSameApexChecker) OutgoingDepIsInSameApex(tag blueprint.DependencyTag) bool {
	if tag == xmlPermissionsFileTag {
		return true
	}

	// None of the other dependencies of the java_sdk_library_import are in the same apex
	// as the one that references this module.
	return false
}

// Implements android.ApexModule
func (m *SdkLibraryImport) MinSdkVersionSupported(ctx android.BaseModuleContext) android.ApiLevel {
	return android.MinApiLevel
}

func (module *SdkLibraryImport) UniqueApexVariations() bool {
	return module.uniqueApexVariations()
}

// MinSdkVersion - Implements hiddenAPIModule
func (module *SdkLibraryImport) MinSdkVersion(ctx android.EarlyModuleContext) android.ApiLevel {
	return android.NoneApiLevel
}

var _ hiddenAPIModule = (*SdkLibraryImport)(nil)

func (module *SdkLibraryImport) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	// Assume that source module(sdk_library) is installed in /<sdk_library partition>/framework
	module.installFile = android.PathForModuleInstall(ctx, "framework", module.Stem()+".jar")

	// Record the paths to the prebuilt stubs library and stubs source.
	ctx.VisitDirectDepsProxy(func(to android.ModuleProxy) {
		tag := ctx.OtherModuleDependencyTag(to)

		// Extract information from any of the scope specific dependencies.
		if scopeTag, ok := tag.(scopeDependencyTag); ok {
			apiScope := scopeTag.apiScope
			scopePaths := module.getScopePathsCreateIfNeeded(apiScope)

			// Extract information from the dependency. The exact information extracted
			// is determined by the nature of the dependency which is determined by the tag.
			scopeTag.extractDepInfo(ctx, to, scopePaths)
		} else if tag == implLibraryTag {
			if implInfo, ok := android.OtherModuleProvider(ctx, to, JavaInfoProvider); ok {
				module.implLibraryInfo = implInfo
			} else {
				ctx.ModuleErrorf("implementation library must be of type *java.Library but was %T", to)
			}
		}
	})
	sdkLibInfo := module.generateCommonBuildActions(ctx)

	// Populate the scope paths with information from the properties.
	for apiScope, scopeProperties := range module.scopeProperties {
		if len(scopeProperties.Jars) == 0 {
			continue
		}

		paths := module.getScopePathsCreateIfNeeded(apiScope)
		paths.annotationsZip = android.OptionalPathForModuleSrc(ctx, scopeProperties.Annotations)
		paths.currentApiFilePath = android.OptionalPathForModuleSrc(ctx, scopeProperties.Current_api)
		paths.removedApiFilePath = android.OptionalPathForModuleSrc(ctx, scopeProperties.Removed_api)
	}

	if ctx.Device() {
		// Shared libraries deapexed from prebuilt apexes are no longer supported.
		// Set the dexJarBuildPath to a fake path.
		// This allows soong analysis pass, but will be an error during ninja execution if there are
		// any rdeps.
		ai, _ := android.ModuleProvider(ctx, android.ApexInfoProvider)
		if ai.ForPrebuiltApex {
			module.dexJarFile = makeDexJarPathFromPath(android.PathForModuleInstall(ctx, "intentionally_no_longer_supported"))
			module.initHiddenAPI(ctx, module.dexJarFile, module.findScopePaths(apiScopePublic).stubsImplPath[0], nil)
		}
	}

	var generatingLibs []string
	for _, apiScope := range AllApiScopes {
		if scopeProperties, ok := module.scopeProperties[apiScope]; ok {
			if len(scopeProperties.Jars) == 0 {
				continue
			}
			generatingLibs = append(generatingLibs, module.stubsLibraryModuleName(apiScope))
		}
	}

	module.setOutputFiles(ctx)
	if module.implLibraryInfo != nil {
		generatingLibs = append(generatingLibs, module.implLibraryModuleName())
		setOutputFilesFromJavaInfo(ctx, module.implLibraryInfo)
	}

	javaInfo := &JavaInfo{}
	if module.implLibraryInfo != nil {
		javaInfo.JacocoReportClassesFile = module.implLibraryInfo.JacocoReportClassesFile
	}

	setExtraJavaInfo(ctx, ctx.Module(), javaInfo)
	android.SetProvider(ctx, JavaInfoProvider, javaInfo)

	sdkLibInfo.GeneratingLibs = generatingLibs
	sdkLibInfo.Prebuilt = true
	android.SetProvider(ctx, SdkLibraryInfoProvider, sdkLibInfo)
}

var _ UsesLibraryDependency = (*SdkLibraryImport)(nil)

// to satisfy UsesLibraryDependency interface
func (module *SdkLibraryImport) DexJarBuildPath(ctx android.ModuleErrorfContext) OptionalDexJarPath {
	// The dex implementation jar extracted from the .apex file should be used in preference to the
	// source.
	if module.dexJarFileErr != nil {
		ctx.ModuleErrorf(module.dexJarFileErr.Error())
	}
	if module.dexJarFile.IsSet() {
		return module.dexJarFile
	}
	if module.implLibraryInfo == nil {
		return makeUnsetDexJarPath()
	} else {
		return module.implLibraryInfo.DexJarFile
	}
}

// to satisfy UsesLibraryDependency interface
func (module *SdkLibraryImport) DexJarInstallPath() android.Path {
	return module.installFile
}

// to satisfy UsesLibraryDependency interface
func (module *SdkLibraryImport) ClassLoaderContexts() dexpreopt.ClassLoaderContextMap {
	return nil
}

// to satisfy apex.javaDependency interface
func (module *SdkLibraryImport) JacocoReportClassesFile() android.Path {
	if module.implLibraryInfo == nil {
		return nil
	} else {
		return module.implLibraryInfo.JacocoReportClassesFile
	}
}

// to satisfy apex.javaDependency interface
func (module *SdkLibraryImport) Stem() string {
	return module.BaseModuleName()
}

var _ ApexDependency = (*SdkLibraryImport)(nil)

// to satisfy java.ApexDependency interface
func (module *SdkLibraryImport) HeaderJars() android.Paths {
	if module.implLibraryInfo == nil {
		return nil
	} else {
		return module.implLibraryInfo.HeaderJars
	}
}

// to satisfy java.ApexDependency interface
func (module *SdkLibraryImport) ImplementationAndResourcesJars() android.Paths {
	if module.implLibraryInfo == nil {
		return nil
	} else {
		return module.implLibraryInfo.ImplementationAndResourcesJars
	}
}

// to satisfy java.DexpreopterInterface interface
func (module *SdkLibraryImport) IsInstallable() bool {
	return true
}

var _ android.RequiredFilesFromPrebuiltApex = (*SdkLibraryImport)(nil)

func (module *SdkLibraryImport) RequiredFilesFromPrebuiltApex(ctx android.BaseModuleContext) []string {
	name := module.BaseModuleName()
	return requiredFilesFromPrebuiltApexForImport(name, &module.dexpreopter)
}

func (j *SdkLibraryImport) UseProfileGuidedDexpreopt() bool {
	return proptools.Bool(j.importDexpreoptProperties.Dex_preopt.Profile_guided)
}

type sdkLibrarySdkMemberType struct {
	android.SdkMemberTypeBase
}

func (s *sdkLibrarySdkMemberType) AddDependencies(ctx android.SdkDependencyContext, dependencyTag blueprint.DependencyTag, names []string) {
	ctx.AddVariationDependencies(nil, dependencyTag, names...)
}

func (s *sdkLibrarySdkMemberType) IsInstance(module android.Module) bool {
	_, ok := module.(*SdkLibrary)
	return ok
}

func (s *sdkLibrarySdkMemberType) AddPrebuiltModule(ctx android.SdkMemberContext, member android.SdkMember) android.BpModule {
	return ctx.SnapshotBuilder().AddPrebuiltModule(member, "java_sdk_library_import")
}

func (s *sdkLibrarySdkMemberType) CreateVariantPropertiesStruct() android.SdkMemberProperties {
	return &sdkLibrarySdkMemberProperties{}
}

var javaSdkLibrarySdkMemberType = &sdkLibrarySdkMemberType{
	android.SdkMemberTypeBase{
		PropertyName: "java_sdk_libs",
		SupportsSdk:  true,
	},
}

type sdkLibrarySdkMemberProperties struct {
	android.SdkMemberPropertiesBase

	// Stem name for files in the sdk snapshot.
	//
	// This is used to construct the path names of various sdk library files in the sdk snapshot to
	// make sure that they match the finalized versions of those files in prebuilts/sdk.
	//
	// This property is marked as keep so that it will be kept in all instances of this struct, will
	// not be cleared but will be copied to common structs. That is needed because this field is used
	// to construct many file names for other parts of this struct and so it needs to be present in
	// all structs. If it was not marked as keep then it would be cleared in some structs and so would
	// be unavailable for generating file names if there were other properties that were still set.
	Stem string `sdk:"keep"`

	// Scope to per scope properties.
	Scopes map[*apiScope]*scopeProperties

	// The Java stubs source files.
	Stub_srcs []string

	// The naming scheme.
	Naming_scheme *string

	// True if the java_sdk_library_import is for a shared library, false
	// otherwise.
	Shared_library *bool

	// True if the stub imports should produce dex jars.
	Compile_dex *bool

	// The paths to the doctag files to add to the prebuilt.
	Doctag_paths android.Paths

	Permitted_packages []string

	// Signals that this shared library is part of the bootclasspath starting
	// on the version indicated in this attribute.
	//
	// This will make platforms at this level and above to ignore
	// <uses-library> tags with this library name because the library is already
	// available
	On_bootclasspath_since *string

	// Signals that this shared library was part of the bootclasspath before
	// (but not including) the version indicated in this attribute.
	//
	// The system will automatically add a <uses-library> tag with this library to
	// apps that target any SDK less than the version indicated in this attribute.
	On_bootclasspath_before *string

	// Indicates that PackageManager should ignore this shared library if the
	// platform is below the version indicated in this attribute.
	//
	// This means that the device won't recognise this library as installed.
	Min_device_sdk *string

	// Indicates that PackageManager should ignore this shared library if the
	// platform is above the version indicated in this attribute.
	//
	// This means that the device won't recognise this library as installed.
	Max_device_sdk *string

	DexPreoptProfileGuided *bool `supported_build_releases:"UpsideDownCake+"`
}

type scopeProperties struct {
	Jars           android.Paths
	StubsSrcJar    android.Path
	CurrentApiFile android.Path
	RemovedApiFile android.Path
	AnnotationsZip android.Path `supported_build_releases:"Tiramisu+"`
	SdkVersion     string
}

func (s *sdkLibrarySdkMemberProperties) PopulateFromVariant(ctx android.SdkMemberContext, variant android.Module) {
	sdk := variant.(*SdkLibrary)

	// Copy the stem name for files in the sdk snapshot.
	s.Stem = sdk.distStem()

	s.Scopes = make(map[*apiScope]*scopeProperties)
	for _, apiScope := range AllApiScopes {
		paths := sdk.findScopePaths(apiScope)
		if paths == nil {
			continue
		}

		jars := paths.stubsImplPath
		if len(jars) > 0 {
			properties := scopeProperties{}
			properties.Jars = jars
			properties.SdkVersion = sdk.sdkVersionForStubsLibrary(ctx.SdkModuleContext(), apiScope)
			properties.StubsSrcJar = paths.stubsSrcJar.Path()
			if paths.currentApiFilePath.Valid() {
				properties.CurrentApiFile = paths.currentApiFilePath.Path()
			}
			if paths.removedApiFilePath.Valid() {
				properties.RemovedApiFile = paths.removedApiFilePath.Path()
			}
			// The annotations zip is only available for modules that set annotations_enabled: true.
			if paths.annotationsZip.Valid() {
				properties.AnnotationsZip = paths.annotationsZip.Path()
			}
			s.Scopes[apiScope] = &properties
		}
	}

	s.Shared_library = proptools.BoolPtr(sdk.sharedLibrary())
	s.Compile_dex = sdk.dexProperties.Compile_dex
	s.Doctag_paths = sdk.doctagPaths
	s.Permitted_packages = sdk.PermittedPackagesForUpdatableBootJars()
	s.On_bootclasspath_since = sdk.commonSdkLibraryProperties.On_bootclasspath_since
	s.On_bootclasspath_before = sdk.commonSdkLibraryProperties.On_bootclasspath_before
	s.Min_device_sdk = sdk.commonSdkLibraryProperties.Min_device_sdk
	s.Max_device_sdk = sdk.commonSdkLibraryProperties.Max_device_sdk

	if sdk.implLibraryInfo != nil && sdk.implLibraryInfo.ProfileGuided {
		s.DexPreoptProfileGuided = proptools.BoolPtr(true)
	}
}

func (s *sdkLibrarySdkMemberProperties) AddToPropertySet(ctx android.SdkMemberContext, propertySet android.BpPropertySet) {
	if s.Naming_scheme != nil {
		propertySet.AddProperty("naming_scheme", proptools.String(s.Naming_scheme))
	}
	if s.Shared_library != nil {
		propertySet.AddProperty("shared_library", *s.Shared_library)
	}
	if s.Compile_dex != nil {
		propertySet.AddProperty("compile_dex", *s.Compile_dex)
	}
	if len(s.Permitted_packages) > 0 {
		propertySet.AddProperty("permitted_packages", s.Permitted_packages)
	}
	dexPreoptSet := propertySet.AddPropertySet("dex_preopt")
	if s.DexPreoptProfileGuided != nil {
		dexPreoptSet.AddProperty("profile_guided", proptools.Bool(s.DexPreoptProfileGuided))
	}

	stem := s.Stem

	for _, apiScope := range AllApiScopes {
		if properties, ok := s.Scopes[apiScope]; ok {
			scopeSet := propertySet.AddPropertySet(apiScope.propertyName)

			scopeDir := apiScope.snapshotRelativeDir()

			var jars []string
			for _, p := range properties.Jars {
				dest := filepath.Join(scopeDir, stem+"-stubs.jar")
				ctx.SnapshotBuilder().CopyToSnapshot(p, dest)
				jars = append(jars, dest)
			}
			scopeSet.AddProperty("jars", jars)

			if ctx.SdkModuleContext().Config().IsEnvTrue("SOONG_SDK_SNAPSHOT_USE_SRCJAR") {
				// Copy the stubs source jar into the snapshot zip as is.
				srcJarSnapshotPath := filepath.Join(scopeDir, stem+".srcjar")
				ctx.SnapshotBuilder().CopyToSnapshot(properties.StubsSrcJar, srcJarSnapshotPath)
				scopeSet.AddProperty("stub_srcs", []string{srcJarSnapshotPath})
			} else {
				// Merge the stubs source jar into the snapshot zip so that when it is unpacked
				// the source files are also unpacked.
				snapshotRelativeDir := filepath.Join(scopeDir, stem+"_stub_sources")
				ctx.SnapshotBuilder().UnzipToSnapshot(properties.StubsSrcJar, snapshotRelativeDir)
				scopeSet.AddProperty("stub_srcs", []string{snapshotRelativeDir})
			}

			if properties.CurrentApiFile != nil {
				currentApiSnapshotPath := apiScope.snapshotRelativeCurrentApiTxtPath(stem)
				ctx.SnapshotBuilder().CopyToSnapshot(properties.CurrentApiFile, currentApiSnapshotPath)
				scopeSet.AddProperty("current_api", currentApiSnapshotPath)
			}

			if properties.RemovedApiFile != nil {
				removedApiSnapshotPath := apiScope.snapshotRelativeRemovedApiTxtPath(stem)
				ctx.SnapshotBuilder().CopyToSnapshot(properties.RemovedApiFile, removedApiSnapshotPath)
				scopeSet.AddProperty("removed_api", removedApiSnapshotPath)
			}

			if properties.AnnotationsZip != nil {
				annotationsSnapshotPath := filepath.Join(scopeDir, stem+"_annotations.zip")
				ctx.SnapshotBuilder().CopyToSnapshot(properties.AnnotationsZip, annotationsSnapshotPath)
				scopeSet.AddProperty("annotations", annotationsSnapshotPath)
			}

			if properties.SdkVersion != "" {
				scopeSet.AddProperty("sdk_version", properties.SdkVersion)
			}
		}
	}

	if len(s.Doctag_paths) > 0 {
		dests := []string{}
		for _, p := range s.Doctag_paths {
			dest := filepath.Join("doctags", p.Rel())
			ctx.SnapshotBuilder().CopyToSnapshot(p, dest)
			dests = append(dests, dest)
		}
		propertySet.AddProperty("doctag_files", dests)
	}
}
