// Copyright 2019 The Android Open Source Project
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

package rust

import (
	"fmt"
	"strconv"
	"strings"

	"android/soong/bloaty"

	"github.com/google/blueprint"
	"github.com/google/blueprint/depset"
	"github.com/google/blueprint/proptools"

	"android/soong/android"
	"android/soong/cc"
	cc_config "android/soong/cc/config"
	"android/soong/fuzz"
	"android/soong/rust/config"
)

var pctx = android.NewPackageContext("android/soong/rust")

type LibraryInfo struct {
	Rlib  bool
	Dylib bool
}

type CompilerInfo struct {
	StdLinkageForDevice    RustLinkage
	StdLinkageForNonDevice RustLinkage
	NoStdlibs              bool
	LibraryInfo            *LibraryInfo
}

type ProtobufDecoratorInfo struct{}

type SourceProviderInfo struct {
	Srcs                  android.Paths
	ProtobufDecoratorInfo *ProtobufDecoratorInfo
}

type RustInfo struct {
	AndroidMkSuffix               string
	RustSubName                   string
	TransitiveAndroidMkSharedLibs depset.DepSet[string]
	CompilerInfo                  *CompilerInfo
	SnapshotInfo                  *cc.SnapshotInfo
	SourceProviderInfo            *SourceProviderInfo
	XrefRustFiles                 android.Paths
	DocTimestampFile              android.OptionalPath
}

var RustInfoProvider = blueprint.NewProvider[*RustInfo]()

func init() {
	android.RegisterModuleType("rust_defaults", defaultsFactory)
	android.PreDepsMutators(registerPreDepsMutators)
	android.PostDepsMutators(registerPostDepsMutators)
	pctx.Import("android/soong/android")
	pctx.Import("android/soong/rust/config")
	pctx.ImportAs("cc_config", "android/soong/cc/config")
	android.InitRegistrationContext.RegisterParallelSingletonType("kythe_rust_extract", kytheExtractRustFactory)
}

func registerPreDepsMutators(ctx android.RegisterMutatorsContext) {
	ctx.Transition("rust_libraries", &libraryTransitionMutator{})
	ctx.Transition("rust_stdlinkage", &libstdTransitionMutator{})
	ctx.BottomUp("rust_begin", BeginMutator)
}

func registerPostDepsMutators(ctx android.RegisterMutatorsContext) {
	ctx.BottomUp("rust_sanitizers", rustSanitizerRuntimeMutator)
}

type Flags struct {
	GlobalRustFlags []string // Flags that apply globally to rust
	GlobalLinkFlags []string // Flags that apply globally to linker
	RustFlags       []string // Flags that apply to rust
	LinkFlags       []string // Flags that apply to linker
	ClippyFlags     []string // Flags that apply to clippy-driver, during the linting
	RustdocFlags    []string // Flags that apply to rustdoc
	Toolchain       config.Toolchain
	Coverage        bool
	Clippy          bool
	EmitXrefs       bool // If true, emit rules to aid cross-referencing
}

type BaseProperties struct {
	AndroidMkRlibs         []string `blueprint:"mutated"`
	AndroidMkDylibs        []string `blueprint:"mutated"`
	AndroidMkProcMacroLibs []string `blueprint:"mutated"`
	AndroidMkStaticLibs    []string `blueprint:"mutated"`
	AndroidMkHeaderLibs    []string `blueprint:"mutated"`

	ImageVariation string `blueprint:"mutated"`
	VndkVersion    string `blueprint:"mutated"`
	SubName        string `blueprint:"mutated"`

	// SubName is used by CC for tracking image variants / SDK versions. RustSubName is used for Rust-specific
	// subnaming which shouldn't be visible to CC modules (such as the rlib stdlinkage subname). This should be
	// appended before SubName.
	RustSubName string `blueprint:"mutated"`

	// Set by imageMutator
	ProductVariantNeeded       bool     `blueprint:"mutated"`
	VendorVariantNeeded        bool     `blueprint:"mutated"`
	CoreVariantNeeded          bool     `blueprint:"mutated"`
	VendorRamdiskVariantNeeded bool     `blueprint:"mutated"`
	RamdiskVariantNeeded       bool     `blueprint:"mutated"`
	RecoveryVariantNeeded      bool     `blueprint:"mutated"`
	ExtraVariants              []string `blueprint:"mutated"`

	// Allows this module to use non-APEX version of libraries. Useful
	// for building binaries that are started before APEXes are activated.
	Bootstrap *bool

	// Used by vendor snapshot to record dependencies from snapshot modules.
	SnapshotSharedLibs []string `blueprint:"mutated"`
	SnapshotStaticLibs []string `blueprint:"mutated"`
	SnapshotRlibs      []string `blueprint:"mutated"`
	SnapshotDylibs     []string `blueprint:"mutated"`

	// Make this module available when building for ramdisk.
	// On device without a dedicated recovery partition, the module is only
	// available after switching root into
	// /first_stage_ramdisk. To expose the module before switching root, install
	// the recovery variant instead.
	Ramdisk_available *bool

	// Make this module available when building for vendor ramdisk.
	// On device without a dedicated recovery partition, the module is only
	// available after switching root into
	// /first_stage_ramdisk. To expose the module before switching root, install
	// the recovery variant instead
	Vendor_ramdisk_available *bool

	// Normally Soong uses the directory structure to decide which modules
	// should be included (framework) or excluded (non-framework) from the
	// different snapshots (vendor, recovery, etc.), but this property
	// allows a partner to exclude a module normally thought of as a
	// framework module from the vendor snapshot.
	Exclude_from_vendor_snapshot *bool

	// Normally Soong uses the directory structure to decide which modules
	// should be included (framework) or excluded (non-framework) from the
	// different snapshots (vendor, recovery, etc.), but this property
	// allows a partner to exclude a module normally thought of as a
	// framework module from the recovery snapshot.
	Exclude_from_recovery_snapshot *bool

	// Make this module available when building for recovery
	Recovery_available *bool

	// The API level that this module is built against. The APIs of this API level will be
	// visible at build time, but use of any APIs newer than min_sdk_version will render the
	// module unloadable on older devices.  In the future it will be possible to weakly-link new
	// APIs, making the behavior match Java: such modules will load on older devices, but
	// calling new APIs on devices that do not support them will result in a crash.
	//
	// This property has the same behavior as sdk_version does for Java modules. For those
	// familiar with Android Gradle, the property behaves similarly to how compileSdkVersion
	// does for Java code.
	//
	// In addition, setting this property causes two variants to be built, one for the platform
	// and one for apps.
	Sdk_version *string

	// Minimum OS API level supported by this C or C++ module. This property becomes the value
	// of the __ANDROID_API__ macro. When the C or C++ module is included in an APEX or an APK,
	// this property is also used to ensure that the min_sdk_version of the containing module is
	// not older (i.e. less) than this module's min_sdk_version. When not set, this property
	// defaults to the value of sdk_version.  When this is set to "apex_inherit", this tracks
	// min_sdk_version of the containing APEX. When the module
	// is not built for an APEX, "apex_inherit" defaults to sdk_version.
	Min_sdk_version *string

	// Variant is an SDK variant created by sdkMutator
	IsSdkVariant bool `blueprint:"mutated"`

	// Set by factories of module types that can only be referenced from variants compiled against
	// the SDK.
	AlwaysSdk bool `blueprint:"mutated"`

	HideFromMake   bool `blueprint:"mutated"`
	PreventInstall bool `blueprint:"mutated"`

	Installable *bool
}

type Module struct {
	fuzz.FuzzModule

	VendorProperties cc.VendorProperties

	Properties BaseProperties

	hod        android.HostOrDeviceSupported
	multilib   android.Multilib
	testModule bool

	makeLinkType string

	afdo             *afdo
	compiler         compiler
	coverage         *coverage
	clippy           *clippy
	sanitize         *sanitize
	cachedToolchain  config.Toolchain
	sourceProvider   SourceProvider
	subAndroidMkOnce map[SubAndroidMkProvider]bool

	exportedLinkDirs []string

	// Output file to be installed, may be stripped or unstripped.
	outputFile android.OptionalPath

	// Cross-reference input file
	kytheFiles android.Paths

	docTimestampFile android.OptionalPath

	hideApexVariantFromMake bool

	// For apex variants, this is set as apex.min_sdk_version
	apexSdkVersion android.ApiLevel

	transitiveAndroidMkSharedLibs depset.DepSet[string]

	// Shared flags among stubs build rules of this module
	sharedFlags cc.SharedFlags
}

func (mod *Module) Header() bool {
	//TODO: If Rust libraries provide header variants, this needs to be updated.
	return false
}

func (mod *Module) SetPreventInstall() {
	mod.Properties.PreventInstall = true
}

func (mod *Module) SetHideFromMake() {
	mod.Properties.HideFromMake = true
}

func (mod *Module) HiddenFromMake() bool {
	return mod.Properties.HideFromMake
}

func (mod *Module) SanitizePropDefined() bool {
	// Because compiler is not set for some Rust modules where sanitize might be set, check that compiler is also not
	// nil since we need compiler to actually sanitize.
	return mod.sanitize != nil && mod.compiler != nil
}

func (mod *Module) IsPrebuilt() bool {
	if _, ok := mod.compiler.(*prebuiltLibraryDecorator); ok {
		return true
	}
	return false
}

func (mod *Module) SelectedStl() string {
	return ""
}

func (mod *Module) NonCcVariants() bool {
	if mod.compiler != nil {
		if library, ok := mod.compiler.(libraryInterface); ok {
			return library.buildRlib() || library.buildDylib()
		}
	}
	panic(fmt.Errorf("NonCcVariants called on non-library module: %q", mod.BaseModuleName()))
}

func (mod *Module) Static() bool {
	if mod.compiler != nil {
		if library, ok := mod.compiler.(libraryInterface); ok {
			return library.static()
		}
	}
	return false
}

func (mod *Module) Shared() bool {
	if mod.compiler != nil {
		if library, ok := mod.compiler.(libraryInterface); ok {
			return library.shared()
		}
	}
	return false
}

func (mod *Module) Dylib() bool {
	if mod.compiler != nil {
		if library, ok := mod.compiler.(libraryInterface); ok {
			return library.dylib()
		}
	}
	return false
}

func (mod *Module) Source() bool {
	if mod.compiler != nil {
		if library, ok := mod.compiler.(libraryInterface); ok && mod.sourceProvider != nil {
			return library.source()
		}
	}
	return false
}

func (mod *Module) RlibStd() bool {
	if mod.compiler != nil {
		if library, ok := mod.compiler.(libraryInterface); ok && library.rlib() {
			return library.rlibStd()
		}
	}
	panic(fmt.Errorf("RlibStd() called on non-rlib module: %q", mod.BaseModuleName()))
}

func (mod *Module) Rlib() bool {
	if mod.compiler != nil {
		if library, ok := mod.compiler.(libraryInterface); ok {
			return library.rlib()
		}
	}
	return false
}

func (mod *Module) Binary() bool {
	if binary, ok := mod.compiler.(binaryInterface); ok {
		return binary.binary()
	}
	return false
}

func (mod *Module) StaticExecutable() bool {
	if !mod.Binary() {
		return false
	}
	return mod.StaticallyLinked()
}

func (mod *Module) ApexExclude() bool {
	if mod.compiler != nil {
		if library, ok := mod.compiler.(libraryInterface); ok {
			return library.apexExclude()
		}
	}
	return false
}

func (mod *Module) Object() bool {
	// Rust has no modules which produce only object files.
	return false
}

func (mod *Module) Toc() android.OptionalPath {
	if mod.compiler != nil {
		if lib, ok := mod.compiler.(libraryInterface); ok {
			return lib.toc()
		}
	}
	panic(fmt.Errorf("Toc() called on non-library module: %q", mod.BaseModuleName()))
}

func (mod *Module) UseSdk() bool {
	return false
}

func (mod *Module) RelativeInstallPath() string {
	if mod.compiler != nil {
		return mod.compiler.relativeInstallPath()
	}
	return ""
}

func (mod *Module) UseVndk() bool {
	return mod.Properties.VndkVersion != ""
}

func (mod *Module) Bootstrap() bool {
	return Bool(mod.Properties.Bootstrap)
}

func (mod *Module) SubName() string {
	return mod.Properties.SubName
}

func (mod *Module) IsVndkPrebuiltLibrary() bool {
	// Rust modules do not provide VNDK prebuilts
	return false
}

func (mod *Module) IsVendorPublicLibrary() bool {
	// Rust modules do not currently support vendor_public_library
	return false
}

func (mod *Module) SdkAndPlatformVariantVisibleToMake() bool {
	// Rust modules to not provide Sdk variants
	return false
}

func (c *Module) IsVndkPrivate() bool {
	// Rust modules do not currently support VNDK variants
	return false
}

func (c *Module) IsLlndk() bool {
	// Rust modules do not currently support LLNDK variants
	return false
}

func (mod *Module) KernelHeadersDecorator() bool {
	return false
}

func (m *Module) NeedsLlndkVariants() bool {
	// Rust modules do not currently support LLNDK variants
	return false
}

func (m *Module) NeedsVendorPublicLibraryVariants() bool {
	// Rust modules do not currently support vendor_public_library
	return false
}

func (mod *Module) HasLlndkStubs() bool {
	// Rust modules do not currently support LLNDK stubs
	return false
}

func (mod *Module) SdkVersion() string {
	return String(mod.Properties.Sdk_version)
}

func (mod *Module) AlwaysSdk() bool {
	return mod.Properties.AlwaysSdk
}

func (mod *Module) IsSdkVariant() bool {
	return mod.Properties.IsSdkVariant
}

func (mod *Module) SplitPerApiLevel() bool {
	return cc.CanUseSdk(mod) && mod.IsCrt()
}

func (mod *Module) XrefRustFiles() android.Paths {
	return mod.kytheFiles
}

type Deps struct {
	Dylibs          []string
	Rlibs           []string
	Rustlibs        []string
	Stdlibs         []string
	ProcMacros      []string
	SharedLibs      []string
	StaticLibs      []string
	WholeStaticLibs []string
	HeaderLibs      []string

	// Used for data dependencies adjacent to tests
	DataLibs []string
	DataBins []string

	CrtBegin, CrtEnd []string
}

type PathDeps struct {
	DyLibs        RustLibraries
	RLibs         RustLibraries
	SharedLibs    android.Paths
	SharedLibDeps android.Paths
	StaticLibs    android.Paths
	ProcMacros    RustLibraries
	AfdoProfiles  android.Paths
	LinkerDeps    android.Paths

	// depFlags and depLinkFlags are rustc and linker (clang) flags.
	depFlags     []string
	depLinkFlags []string

	// track cc static-libs that have Rlib dependencies
	reexportedCcRlibDeps      []cc.RustRlibDep
	reexportedWholeCcRlibDeps []cc.RustRlibDep
	ccRlibDeps                []cc.RustRlibDep

	// linkDirs are link paths passed via -L to rustc. linkObjects are objects passed directly to the linker
	// Both of these are exported and propagate to dependencies.
	linkDirs              []string
	rustLibObjects        []string
	staticLibObjects      []string
	wholeStaticLibObjects []string
	sharedLibObjects      []string

	// exportedLinkDirs are exported linkDirs for direct rlib dependencies to
	// cc_library_static dependants of rlibs.
	// Track them separately from linkDirs so superfluous -L flags don't get emitted.
	exportedLinkDirs []string

	// Used by bindgen modules which call clang
	depClangFlags         []string
	depIncludePaths       android.Paths
	depGeneratedHeaders   android.Paths
	depSystemIncludePaths android.Paths

	CrtBegin android.Paths
	CrtEnd   android.Paths

	// Paths to generated source files
	SrcDeps          android.Paths
	srcProviderFiles android.Paths

	directImplementationDeps     android.Paths
	transitiveImplementationDeps []depset.DepSet[android.Path]
}

type RustLibraries []RustLibrary

type RustLibrary struct {
	Path      android.Path
	CrateName string
}

type exportedFlagsProducer interface {
	exportLinkDirs(...string)
	exportRustLibs(...string)
	exportStaticLibs(...string)
	exportWholeStaticLibs(...string)
	exportSharedLibs(...string)
}

type xref interface {
	XrefRustFiles() android.Paths
}

type flagExporter struct {
	linkDirs              []string
	ccLinkDirs            []string
	rustLibPaths          []string
	staticLibObjects      []string
	sharedLibObjects      []string
	wholeStaticLibObjects []string
	wholeRustRlibDeps     []cc.RustRlibDep
}

func (flagExporter *flagExporter) exportLinkDirs(dirs ...string) {
	flagExporter.linkDirs = android.FirstUniqueStrings(append(flagExporter.linkDirs, dirs...))
}

func (flagExporter *flagExporter) exportRustLibs(flags ...string) {
	flagExporter.rustLibPaths = android.FirstUniqueStrings(append(flagExporter.rustLibPaths, flags...))
}

func (flagExporter *flagExporter) exportStaticLibs(flags ...string) {
	flagExporter.staticLibObjects = android.FirstUniqueStrings(append(flagExporter.staticLibObjects, flags...))
}

func (flagExporter *flagExporter) exportSharedLibs(flags ...string) {
	flagExporter.sharedLibObjects = android.FirstUniqueStrings(append(flagExporter.sharedLibObjects, flags...))
}

func (flagExporter *flagExporter) exportWholeStaticLibs(flags ...string) {
	flagExporter.wholeStaticLibObjects = android.FirstUniqueStrings(append(flagExporter.wholeStaticLibObjects, flags...))
}

func (flagExporter *flagExporter) setRustProvider(ctx ModuleContext) {
	android.SetProvider(ctx, RustFlagExporterInfoProvider, RustFlagExporterInfo{
		LinkDirs:              flagExporter.linkDirs,
		RustLibObjects:        flagExporter.rustLibPaths,
		StaticLibObjects:      flagExporter.staticLibObjects,
		WholeStaticLibObjects: flagExporter.wholeStaticLibObjects,
		SharedLibPaths:        flagExporter.sharedLibObjects,
		WholeRustRlibDeps:     flagExporter.wholeRustRlibDeps,
	})
}

var _ exportedFlagsProducer = (*flagExporter)(nil)

func NewFlagExporter() *flagExporter {
	return &flagExporter{}
}

type RustFlagExporterInfo struct {
	Flags                 []string
	LinkDirs              []string
	RustLibObjects        []string
	StaticLibObjects      []string
	WholeStaticLibObjects []string
	SharedLibPaths        []string
	WholeRustRlibDeps     []cc.RustRlibDep
}

var RustFlagExporterInfoProvider = blueprint.NewProvider[RustFlagExporterInfo]()

func (mod *Module) isCoverageVariant() bool {
	return mod.coverage.Properties.IsCoverageVariant
}

var _ cc.Coverage = (*Module)(nil)

func (mod *Module) IsNativeCoverageNeeded(ctx cc.IsNativeCoverageNeededContext) bool {
	return mod.coverage != nil && mod.coverage.Properties.NeedCoverageVariant
}

func (mod *Module) VndkVersion() string {
	return mod.Properties.VndkVersion
}

func (mod *Module) ExportedCrateLinkDirs() []string {
	return mod.exportedLinkDirs
}

func (mod *Module) PreventInstall() bool {
	return mod.Properties.PreventInstall
}
func (c *Module) ForceDisableSanitizers() {
	c.sanitize.Properties.ForceDisable = true
}

func (mod *Module) MarkAsCoverageVariant(coverage bool) {
	mod.coverage.Properties.IsCoverageVariant = coverage
}

func (mod *Module) EnableCoverageIfNeeded() {
	mod.coverage.Properties.CoverageEnabled = mod.coverage.Properties.NeedCoverageBuild
}

func defaultsFactory() android.Module {
	return DefaultsFactory()
}

type Defaults struct {
	android.ModuleBase
	android.DefaultsModuleBase
}

func DefaultsFactory(props ...interface{}) android.Module {
	module := &Defaults{}

	module.AddProperties(props...)
	module.AddProperties(
		&BaseProperties{},
		&cc.AfdoProperties{},
		&cc.VendorProperties{},
		&BenchmarkProperties{},
		&BindgenProperties{},
		&BaseCompilerProperties{},
		&BinaryCompilerProperties{},
		&LibraryCompilerProperties{},
		&ProcMacroCompilerProperties{},
		&PrebuiltProperties{},
		&SourceProviderProperties{},
		&TestProperties{},
		&cc.CoverageProperties{},
		&cc.RustBindgenClangProperties{},
		&ClippyProperties{},
		&SanitizeProperties{},
		&fuzz.FuzzProperties{},
	)

	android.InitDefaultsModule(module)
	return module
}

func (mod *Module) CrateName() string {
	return mod.compiler.crateName()
}

func (mod *Module) CcLibrary() bool {
	if mod.compiler != nil {
		if _, ok := mod.compiler.(libraryInterface); ok {
			return true
		}
	}
	return false
}

func (mod *Module) CcLibraryInterface() bool {
	if mod.compiler != nil {
		// use build{Static,Shared}() instead of {static,shared}() here because this might be called before
		// VariantIs{Static,Shared} is set.
		if lib, ok := mod.compiler.(libraryInterface); ok && (lib.buildShared() || lib.buildStatic() || lib.buildRlib()) {
			return true
		}
	}
	return false
}

func (mod *Module) RustLibraryInterface() bool {
	if mod.compiler != nil {
		if _, ok := mod.compiler.(libraryInterface); ok {
			return true
		}
	}
	return false
}

func (mod *Module) IsFuzzModule() bool {
	if _, ok := mod.compiler.(*fuzzDecorator); ok {
		return true
	}
	return false
}

func (mod *Module) FuzzModuleStruct() fuzz.FuzzModule {
	return mod.FuzzModule
}

func (mod *Module) FuzzPackagedModule() fuzz.FuzzPackagedModule {
	if fuzzer, ok := mod.compiler.(*fuzzDecorator); ok {
		return fuzzer.fuzzPackagedModule
	}
	panic(fmt.Errorf("FuzzPackagedModule called on non-fuzz module: %q", mod.BaseModuleName()))
}

func (mod *Module) FuzzSharedLibraries() android.RuleBuilderInstalls {
	if fuzzer, ok := mod.compiler.(*fuzzDecorator); ok {
		return fuzzer.sharedLibraries
	}
	panic(fmt.Errorf("FuzzSharedLibraries called on non-fuzz module: %q", mod.BaseModuleName()))
}

func (mod *Module) UnstrippedOutputFile() android.Path {
	if mod.compiler != nil {
		return mod.compiler.unstrippedOutputFilePath()
	}
	return nil
}

func (mod *Module) SetStatic() {
	if mod.compiler != nil {
		if library, ok := mod.compiler.(libraryInterface); ok {
			library.setStatic()
			return
		}
	}
	panic(fmt.Errorf("SetStatic called on non-library module: %q", mod.BaseModuleName()))
}

func (mod *Module) SetShared() {
	if mod.compiler != nil {
		if library, ok := mod.compiler.(libraryInterface); ok {
			library.setShared()
			return
		}
	}
	panic(fmt.Errorf("SetShared called on non-library module: %q", mod.BaseModuleName()))
}

func (mod *Module) BuildStaticVariant() bool {
	if mod.compiler != nil {
		if library, ok := mod.compiler.(libraryInterface); ok {
			return library.buildStatic()
		}
	}
	panic(fmt.Errorf("BuildStaticVariant called on non-library module: %q", mod.BaseModuleName()))
}

func (mod *Module) BuildRlibVariant() bool {
	if mod.compiler != nil {
		if library, ok := mod.compiler.(libraryInterface); ok {
			return library.buildRlib()
		}
	}
	panic(fmt.Errorf("BuildRlibVariant called on non-library module: %q", mod.BaseModuleName()))
}

func (mod *Module) BuildSharedVariant() bool {
	if mod.compiler != nil {
		if library, ok := mod.compiler.(libraryInterface); ok {
			return library.buildShared()
		}
	}
	panic(fmt.Errorf("BuildSharedVariant called on non-library module: %q", mod.BaseModuleName()))
}

func (mod *Module) Module() android.Module {
	return mod
}

func (mod *Module) OutputFile() android.OptionalPath {
	return mod.outputFile
}

func (mod *Module) CoverageFiles() android.Paths {
	if mod.compiler != nil {
		return android.Paths{}
	}
	panic(fmt.Errorf("CoverageFiles called on non-library module: %q", mod.BaseModuleName()))
}

// Rust does not produce gcno files, and therefore does not produce a coverage archive.
func (mod *Module) CoverageOutputFile() android.OptionalPath {
	return android.OptionalPath{}
}

func (mod *Module) IsNdk(config android.Config) bool {
	return false
}

func (mod *Module) IsStubs() bool {
	if lib, ok := mod.compiler.(libraryInterface); ok {
		return lib.BuildStubs()
	}
	return false
}

func (mod *Module) HasStubsVariants() bool {
	if lib, ok := mod.compiler.(libraryInterface); ok {
		return lib.HasStubsVariants()
	}
	return false
}

func (mod *Module) ApexSdkVersion() android.ApiLevel {
	return mod.apexSdkVersion
}

func (mod *Module) RustApexExclude() bool {
	return mod.ApexExclude()
}

func (mod *Module) getSharedFlags() *cc.SharedFlags {
	shared := &mod.sharedFlags
	if shared.FlagsMap == nil {
		shared.NumSharedFlags = 0
		shared.FlagsMap = make(map[string]string)
	}
	return shared
}

func (mod *Module) ImplementationModuleNameForMake() string {
	name := mod.BaseModuleName()
	if versioned, ok := mod.compiler.(cc.VersionedInterface); ok {
		name = versioned.ImplementationModuleName(name)
	}
	return name
}

func (mod *Module) Multilib() string {
	return mod.Arch().ArchType.Multilib
}

func (mod *Module) IsCrt() bool {
	// Rust does not currently provide any crt modules.
	return false
}

func (mod *Module) installable(apexInfo android.ApexInfo) bool {
	if !proptools.BoolDefault(mod.Installable(), mod.EverInstallable()) {
		return false
	}

	// The apex variant is not installable because it is included in the APEX and won't appear
	// in the system partition as a standalone file.
	if !apexInfo.IsForPlatform() {
		return false
	}

	return mod.OutputFile().Valid() && !mod.Properties.PreventInstall
}

func (ctx moduleContext) apexVariationName() string {
	apexInfo, _ := android.ModuleProvider(ctx, android.ApexInfoProvider)
	return apexInfo.ApexVariationName
}

var _ cc.LinkableInterface = (*Module)(nil)
var _ cc.VersionedLinkableInterface = (*Module)(nil)

func (mod *Module) Init() android.Module {
	mod.AddProperties(&mod.Properties)
	mod.AddProperties(&mod.VendorProperties)

	if mod.afdo != nil {
		mod.AddProperties(mod.afdo.props()...)
	}
	if mod.compiler != nil {
		mod.AddProperties(mod.compiler.compilerProps()...)
	}
	if mod.coverage != nil {
		mod.AddProperties(mod.coverage.props()...)
	}
	if mod.clippy != nil {
		mod.AddProperties(mod.clippy.props()...)
	}
	if mod.sourceProvider != nil {
		mod.AddProperties(mod.sourceProvider.SourceProviderProps()...)
	}
	if mod.sanitize != nil {
		mod.AddProperties(mod.sanitize.props()...)
	}

	android.InitAndroidArchModule(mod, mod.hod, mod.multilib)
	android.InitApexModule(mod)

	android.InitDefaultableModule(mod)
	return mod
}

func newBaseModule(hod android.HostOrDeviceSupported, multilib android.Multilib) *Module {
	return &Module{
		hod:      hod,
		multilib: multilib,
	}
}
func newModule(hod android.HostOrDeviceSupported, multilib android.Multilib) *Module {
	module := newBaseModule(hod, multilib)
	module.afdo = &afdo{}
	module.coverage = &coverage{}
	module.clippy = &clippy{}
	module.sanitize = &sanitize{}
	return module
}

type ModuleContext interface {
	android.ModuleContext
	ModuleContextIntf
}

type BaseModuleContext interface {
	android.BaseModuleContext
	ModuleContextIntf
}

type DepsContext interface {
	android.BottomUpMutatorContext
	ModuleContextIntf
}

type ModuleContextIntf interface {
	RustModule() *Module
	toolchain() config.Toolchain
}

type depsContext struct {
	android.BottomUpMutatorContext
}

type moduleContext struct {
	android.ModuleContext
}

type baseModuleContext struct {
	android.BaseModuleContext
}

func (ctx *moduleContext) RustModule() *Module {
	return ctx.Module().(*Module)
}

func (ctx *moduleContext) toolchain() config.Toolchain {
	return ctx.RustModule().toolchain(ctx)
}

func (ctx *depsContext) RustModule() *Module {
	return ctx.Module().(*Module)
}

func (ctx *depsContext) toolchain() config.Toolchain {
	return ctx.RustModule().toolchain(ctx)
}

func (ctx *baseModuleContext) RustModule() *Module {
	return ctx.Module().(*Module)
}

func (ctx *baseModuleContext) toolchain() config.Toolchain {
	return ctx.RustModule().toolchain(ctx)
}

func (mod *Module) nativeCoverage() bool {
	// Bug: http://b/137883967 - native-bridge modules do not currently work with coverage
	if mod.Target().NativeBridge == android.NativeBridgeEnabled {
		return false
	}
	return mod.compiler != nil && mod.compiler.nativeCoverage()
}

func (mod *Module) SetStl(s string) {
	// STL is a CC concept; do nothing for Rust
}

func (mod *Module) SetSdkVersion(s string) {
	mod.Properties.Sdk_version = StringPtr(s)
}

func (mod *Module) SetMinSdkVersion(s string) {
	mod.Properties.Min_sdk_version = StringPtr(s)
}

func (mod *Module) VersionedInterface() cc.VersionedInterface {
	if _, ok := mod.compiler.(cc.VersionedInterface); ok {
		return mod.compiler.(cc.VersionedInterface)
	}
	return nil
}

func (mod *Module) EverInstallable() bool {
	return mod.compiler != nil &&
		// Check to see whether the module is actually ever installable.
		mod.compiler.everInstallable()
}

func (mod *Module) Installable() *bool {
	return mod.Properties.Installable
}

func (mod *Module) ProcMacro() bool {
	if pm, ok := mod.compiler.(procMacroInterface); ok {
		return pm.ProcMacro()
	}
	return false
}

func (mod *Module) toolchain(ctx android.BaseModuleContext) config.Toolchain {
	if mod.cachedToolchain == nil {
		mod.cachedToolchain = config.FindToolchain(ctx.Os(), ctx.Arch())
	}
	return mod.cachedToolchain
}

func (mod *Module) ccToolchain(ctx android.BaseModuleContext) cc_config.Toolchain {
	return cc_config.FindToolchain(ctx.Os(), ctx.Arch())
}

func (d *Defaults) GenerateAndroidBuildActions(ctx android.ModuleContext) {
}

func (mod *Module) GenerateAndroidBuildActions(actx android.ModuleContext) {
	ctx := &moduleContext{
		ModuleContext: actx,
	}

	apexInfo, _ := android.ModuleProvider(actx, android.ApexInfoProvider)
	if !apexInfo.IsForPlatform() {
		mod.hideApexVariantFromMake = true
	}

	toolchain := mod.toolchain(ctx)
	mod.makeLinkType = cc.GetMakeLinkType(actx, mod)

	mod.Properties.SubName = cc.GetSubnameProperty(actx, mod)

	if !toolchain.Supported() {
		// This toolchain's unsupported, there's nothing to do for this mod.
		return
	}

	deps := mod.depsToPaths(ctx)
	// Export linkDirs for CC rust generatedlibs
	mod.exportedLinkDirs = append(mod.exportedLinkDirs, deps.exportedLinkDirs...)
	mod.exportedLinkDirs = append(mod.exportedLinkDirs, deps.linkDirs...)

	flags := Flags{
		Toolchain: toolchain,
	}

	// Calculate rustc flags
	if mod.afdo != nil {
		flags, deps = mod.afdo.flags(actx, flags, deps)
	}
	if mod.compiler != nil {
		flags = mod.compiler.compilerFlags(ctx, flags)
		flags = mod.compiler.cfgFlags(ctx, flags)
		flags = mod.compiler.featureFlags(ctx, mod, flags)
	}
	if mod.coverage != nil {
		flags, deps = mod.coverage.flags(ctx, flags, deps)
	}
	if mod.clippy != nil {
		flags, deps = mod.clippy.flags(ctx, flags, deps)
	}
	if mod.sanitize != nil {
		flags, deps = mod.sanitize.flags(ctx, flags, deps)
	}

	// SourceProvider needs to call GenerateSource() before compiler calls
	// compile() so it can provide the source. A SourceProvider has
	// multiple variants (e.g. source, rlib, dylib). Only the "source"
	// variant is responsible for effectively generating the source. The
	// remaining variants relies on the "source" variant output.
	if mod.sourceProvider != nil {
		if mod.compiler.(libraryInterface).source() {
			mod.sourceProvider.GenerateSource(ctx, deps)
			mod.sourceProvider.setSubName(ctx.ModuleSubDir())
		} else {
			sourceMod := actx.GetDirectDepProxyWithTag(mod.Name(), sourceDepTag)
			sourceLib := android.OtherModuleProviderOrDefault(ctx, sourceMod, RustInfoProvider).SourceProviderInfo
			mod.sourceProvider.setOutputFiles(sourceLib.Srcs)
		}
		ctx.CheckbuildFile(mod.sourceProvider.Srcs()...)
	}

	if mod.compiler != nil && !mod.compiler.Disabled() {
		mod.compiler.initialize(ctx)
		buildOutput := mod.compiler.compile(ctx, flags, deps)
		if ctx.Failed() {
			return
		}
		mod.outputFile = android.OptionalPathForPath(buildOutput.outputFile)
		ctx.CheckbuildFile(buildOutput.outputFile)
		if buildOutput.kytheFile != nil {
			mod.kytheFiles = append(mod.kytheFiles, buildOutput.kytheFile)
		}
		bloaty.MeasureSizeForPaths(ctx, mod.compiler.strippedOutputFilePath(), android.OptionalPathForPath(mod.compiler.unstrippedOutputFilePath()))

		mod.docTimestampFile = mod.compiler.rustdoc(ctx, flags, deps)

		apexInfo, _ := android.ModuleProvider(actx, android.ApexInfoProvider)
		if !proptools.BoolDefault(mod.Installable(), mod.EverInstallable()) && !mod.ProcMacro() {
			// If the module has been specifically configure to not be installed then
			// hide from make as otherwise it will break when running inside make as the
			// output path to install will not be specified. Not all uninstallable
			// modules can be hidden from make as some are needed for resolving make
			// side dependencies. In particular, proc-macros need to be captured in the
			// host snapshot.
			mod.HideFromMake()
			mod.SkipInstall()
		} else if !mod.installable(apexInfo) {
			mod.SkipInstall()
		}

		// Still call install though, the installs will be stored as PackageSpecs to allow
		// using the outputs in a genrule.
		if mod.OutputFile().Valid() {
			mod.compiler.install(ctx)
			if ctx.Failed() {
				return
			}
			// Export your own directory as a linkDir
			mod.exportedLinkDirs = append(mod.exportedLinkDirs, linkPathFromFilePath(mod.OutputFile().Path()))

		}

		android.SetProvider(ctx, cc.ImplementationDepInfoProvider, &cc.ImplementationDepInfo{
			ImplementationDeps: depset.New(depset.PREORDER, deps.directImplementationDeps, deps.transitiveImplementationDeps),
		})

		ctx.Phony("rust", ctx.RustModule().OutputFile().Path())
	}

	linkableInfo := cc.CreateCommonLinkableInfo(ctx, mod)
	linkableInfo.Static = mod.Static()
	linkableInfo.Shared = mod.Shared()
	linkableInfo.CrateName = mod.CrateName()
	linkableInfo.ExportedCrateLinkDirs = mod.ExportedCrateLinkDirs()
	if lib, ok := mod.compiler.(cc.VersionedInterface); ok {
		linkableInfo.StubsVersion = lib.StubsVersion()
	}

	android.SetProvider(ctx, cc.LinkableInfoProvider, linkableInfo)

	rustInfo := &RustInfo{
		AndroidMkSuffix:               mod.AndroidMkSuffix(),
		RustSubName:                   mod.Properties.RustSubName,
		TransitiveAndroidMkSharedLibs: mod.transitiveAndroidMkSharedLibs,
		XrefRustFiles:                 mod.XrefRustFiles(),
		DocTimestampFile:              mod.docTimestampFile,
	}
	if mod.compiler != nil {
		rustInfo.CompilerInfo = &CompilerInfo{
			NoStdlibs:              mod.compiler.noStdlibs(),
			StdLinkageForDevice:    mod.compiler.stdLinkage(true),
			StdLinkageForNonDevice: mod.compiler.stdLinkage(false),
		}
		if lib, ok := mod.compiler.(libraryInterface); ok {
			rustInfo.CompilerInfo.LibraryInfo = &LibraryInfo{
				Dylib: lib.dylib(),
				Rlib:  lib.rlib(),
			}
		}
		if lib, ok := mod.compiler.(cc.SnapshotInterface); ok {
			rustInfo.SnapshotInfo = &cc.SnapshotInfo{
				SnapshotAndroidMkSuffix: lib.SnapshotAndroidMkSuffix(),
			}
		}
	}
	if mod.sourceProvider != nil {
		rustInfo.SourceProviderInfo = &SourceProviderInfo{
			Srcs: mod.sourceProvider.Srcs(),
		}
		if _, ok := mod.sourceProvider.(*protobufDecorator); ok {
			rustInfo.SourceProviderInfo.ProtobufDecoratorInfo = &ProtobufDecoratorInfo{}
		}
	}
	android.SetProvider(ctx, RustInfoProvider, rustInfo)

	ccInfo := &cc.CcInfo{
		IsPrebuilt: mod.IsPrebuilt(),
	}

	// Define the linker info if compiler != nil because Rust currently
	// does compilation and linking in one step. If this changes in the future,
	// move this as appropriate.
	baseCompilerProps := mod.compiler.baseCompilerProps()
	ccInfo.LinkerInfo = &cc.LinkerInfo{
		WholeStaticLibs: baseCompilerProps.Whole_static_libs.GetOrDefault(ctx, nil),
		StaticLibs:      baseCompilerProps.Static_libs.GetOrDefault(ctx, nil),
		SharedLibs:      baseCompilerProps.Shared_libs.GetOrDefault(ctx, nil),
	}

	android.SetProvider(ctx, cc.CcInfoProvider, ccInfo)

	mod.setOutputFiles(ctx)

	buildComplianceMetadataInfo(ctx, mod, deps)

	moduleInfoJSON := ctx.ModuleInfoJSON()
	if mod.compiler != nil {
		mod.compiler.moduleInfoJSON(ctx, moduleInfoJSON)
	}
}

func (mod *Module) setOutputFiles(ctx ModuleContext) {
	if mod.sourceProvider != nil && (mod.compiler == nil || mod.compiler.Disabled()) {
		ctx.SetOutputFiles(mod.sourceProvider.Srcs(), "")
	} else if mod.OutputFile().Valid() {
		ctx.SetOutputFiles(android.Paths{mod.OutputFile().Path()}, "")
	} else {
		ctx.SetOutputFiles(android.Paths{}, "")
	}
	if mod.compiler != nil {
		ctx.SetOutputFiles(android.PathsIfNonNil(mod.compiler.unstrippedOutputFilePath()), "unstripped")
	}
}

func buildComplianceMetadataInfo(ctx *moduleContext, mod *Module, deps PathDeps) {
	// Dump metadata that can not be done in android/compliance-metadata.go
	metadataInfo := ctx.ComplianceMetadataInfo()
	metadataInfo.SetStringValue(android.ComplianceMetadataProp.IS_STATIC_LIB, strconv.FormatBool(mod.Static()))
	metadataInfo.SetStringValue(android.ComplianceMetadataProp.BUILT_FILES, mod.outputFile.String())

	// Static libs
	staticDeps := ctx.GetDirectDepsProxyWithTag(rlibDepTag)
	staticDepNames := make([]string, 0, len(staticDeps))
	for _, dep := range staticDeps {
		staticDepNames = append(staticDepNames, dep.Name())
	}
	ccStaticDeps := ctx.GetDirectDepsProxyWithTag(cc.StaticDepTag(false))
	for _, dep := range ccStaticDeps {
		staticDepNames = append(staticDepNames, dep.Name())
	}

	staticDepPaths := make([]string, 0, len(deps.StaticLibs)+len(deps.RLibs))
	// C static libraries
	for _, dep := range deps.StaticLibs {
		staticDepPaths = append(staticDepPaths, dep.String())
	}
	// Rust static libraries
	for _, dep := range deps.RLibs {
		staticDepPaths = append(staticDepPaths, dep.Path.String())
	}
	metadataInfo.SetListValue(android.ComplianceMetadataProp.STATIC_DEPS, android.FirstUniqueStrings(staticDepNames))
	metadataInfo.SetListValue(android.ComplianceMetadataProp.STATIC_DEP_FILES, android.FirstUniqueStrings(staticDepPaths))

	// C Whole static libs
	ccWholeStaticDeps := ctx.GetDirectDepsProxyWithTag(cc.StaticDepTag(true))
	wholeStaticDepNames := make([]string, 0, len(ccWholeStaticDeps))
	for _, dep := range ccStaticDeps {
		wholeStaticDepNames = append(wholeStaticDepNames, dep.Name())
	}
	metadataInfo.SetListValue(android.ComplianceMetadataProp.STATIC_DEPS, android.FirstUniqueStrings(staticDepNames))
}

func (mod *Module) deps(ctx DepsContext) Deps {
	deps := Deps{}

	if mod.compiler != nil {
		deps = mod.compiler.compilerDeps(ctx, deps)
	}
	if mod.sourceProvider != nil {
		deps = mod.sourceProvider.SourceProviderDeps(ctx, deps)
	}

	if mod.coverage != nil {
		deps = mod.coverage.deps(ctx, deps)
	}

	if mod.sanitize != nil {
		deps = mod.sanitize.deps(ctx, deps)
	}

	deps.Rlibs = android.LastUniqueStrings(deps.Rlibs)
	deps.Dylibs = android.LastUniqueStrings(deps.Dylibs)
	deps.Rustlibs = android.LastUniqueStrings(deps.Rustlibs)
	deps.ProcMacros = android.LastUniqueStrings(deps.ProcMacros)
	deps.SharedLibs = android.LastUniqueStrings(deps.SharedLibs)
	deps.StaticLibs = android.LastUniqueStrings(deps.StaticLibs)
	deps.Stdlibs = android.LastUniqueStrings(deps.Stdlibs)
	deps.WholeStaticLibs = android.LastUniqueStrings(deps.WholeStaticLibs)
	return deps

}

type dependencyTag struct {
	blueprint.BaseDependencyTag
	name      string
	library   bool
	procMacro bool
	dynamic   bool
}

// InstallDepNeeded returns true for rlibs, dylibs, and proc macros so that they or their transitive
// dependencies (especially C/C++ shared libs) are installed as dependencies of a rust binary.
func (d dependencyTag) InstallDepNeeded() bool {
	return d.library || d.procMacro
}

var _ android.InstallNeededDependencyTag = dependencyTag{}

func (d dependencyTag) LicenseAnnotations() []android.LicenseAnnotation {
	if d.library && d.dynamic {
		return []android.LicenseAnnotation{android.LicenseAnnotationSharedDependency}
	}
	return nil
}

func (d dependencyTag) PropagateAconfigValidation() bool {
	return d == rlibDepTag || d == sourceDepTag
}

var _ android.PropagateAconfigValidationDependencyTag = dependencyTag{}

var _ android.LicenseAnnotationsDependencyTag = dependencyTag{}

var (
	customBindgenDepTag = dependencyTag{name: "customBindgenTag"}
	rlibDepTag          = dependencyTag{name: "rlibTag", library: true}
	dylibDepTag         = dependencyTag{name: "dylib", library: true, dynamic: true}
	procMacroDepTag     = dependencyTag{name: "procMacro", procMacro: true}
	sourceDepTag        = dependencyTag{name: "source"}
	dataLibDepTag       = dependencyTag{name: "data lib"}
	dataBinDepTag       = dependencyTag{name: "data bin"}
)

func IsDylibDepTag(depTag blueprint.DependencyTag) bool {
	tag, ok := depTag.(dependencyTag)
	return ok && tag == dylibDepTag
}

func IsRlibDepTag(depTag blueprint.DependencyTag) bool {
	tag, ok := depTag.(dependencyTag)
	return ok && tag == rlibDepTag
}

type autoDep struct {
	variation string
	depTag    dependencyTag
}

var (
	sourceVariation = "source"
	rlibVariation   = "rlib"
	dylibVariation  = "dylib"
	rlibAutoDep     = autoDep{variation: rlibVariation, depTag: rlibDepTag}
	dylibAutoDep    = autoDep{variation: dylibVariation, depTag: dylibDepTag}
)

type autoDeppable interface {
	autoDep(ctx android.BottomUpMutatorContext) autoDep
}

func (mod *Module) begin(ctx BaseModuleContext) {
	if mod.coverage != nil {
		mod.coverage.begin(ctx)
	}
	if mod.sanitize != nil {
		mod.sanitize.begin(ctx)
	}

	if mod.UseSdk() && mod.IsSdkVariant() {
		sdkVersion := ""
		if ctx.Device() {
			sdkVersion = mod.SdkVersion()
		}
		version, err := cc.NativeApiLevelFromUser(ctx, sdkVersion)
		if err != nil {
			ctx.PropertyErrorf("sdk_version", err.Error())
			mod.Properties.Sdk_version = nil
		} else {
			mod.Properties.Sdk_version = StringPtr(version.String())
		}
	}

}

func (mod *Module) Prebuilt() *android.Prebuilt {
	if p, ok := mod.compiler.(rustPrebuilt); ok {
		return p.prebuilt()
	}
	return nil
}

func (mod *Module) Symlinks() []string {
	// TODO update this to return the list of symlinks when Rust supports defining symlinks
	return nil
}

func rustMakeLibName(rustInfo *RustInfo, linkableInfo *cc.LinkableInfo, commonInfo *android.CommonModuleInfo, depName string) string {
	if rustInfo != nil {
		// Use base module name for snapshots when exporting to Makefile.
		if rustInfo.SnapshotInfo != nil {
			baseName := commonInfo.BaseModuleName
			return baseName + rustInfo.SnapshotInfo.SnapshotAndroidMkSuffix + rustInfo.AndroidMkSuffix
		}
	}
	return cc.MakeLibName(nil, linkableInfo, commonInfo, depName)
}

func collectIncludedProtos(mod *Module, rustInfo *RustInfo, linkableInfo *cc.LinkableInfo) {
	if protoMod, ok := mod.sourceProvider.(*protobufDecorator); ok {
		if rustInfo.SourceProviderInfo.ProtobufDecoratorInfo != nil {
			protoMod.additionalCrates = append(protoMod.additionalCrates, linkableInfo.CrateName)
		}
	}
}

func (mod *Module) depsToPaths(ctx android.ModuleContext) PathDeps {
	var depPaths PathDeps

	directRlibDeps := []*cc.LinkableInfo{}
	directDylibDeps := []*cc.LinkableInfo{}
	directProcMacroDeps := []*cc.LinkableInfo{}
	directSharedLibDeps := []cc.SharedLibraryInfo{}
	directStaticLibDeps := [](*cc.LinkableInfo){}
	directSrcProvidersDeps := []*android.ModuleProxy{}
	directSrcDeps := []android.SourceFilesInfo{}

	// For the dependency from platform to apex, use the latest stubs
	mod.apexSdkVersion = android.FutureApiLevel
	apexInfo, _ := android.ModuleProvider(ctx, android.ApexInfoProvider)
	if !apexInfo.IsForPlatform() {
		mod.apexSdkVersion = apexInfo.MinSdkVersion
	}

	if android.InList("hwaddress", ctx.Config().SanitizeDevice()) {
		// In hwasan build, we override apexSdkVersion to the FutureApiLevel(10000)
		// so that even Q(29/Android10) apexes could use the dynamic unwinder by linking the newer stubs(e.g libc(R+)).
		// (b/144430859)
		mod.apexSdkVersion = android.FutureApiLevel
	}

	skipModuleList := map[string]bool{}

	var transitiveAndroidMkSharedLibs []depset.DepSet[string]
	var directAndroidMkSharedLibs []string

	ctx.VisitDirectDepsProxy(func(dep android.ModuleProxy) {
		depName := ctx.OtherModuleName(dep)
		depTag := ctx.OtherModuleDependencyTag(dep)
		modStdLinkage := mod.compiler.stdLinkage(ctx.Device())

		if _, exists := skipModuleList[depName]; exists {
			return
		}

		if depTag == android.DarwinUniversalVariantTag {
			return
		}

		rustInfo, hasRustInfo := android.OtherModuleProvider(ctx, dep, RustInfoProvider)
		ccInfo, _ := android.OtherModuleProvider(ctx, dep, cc.CcInfoProvider)
		linkableInfo, hasLinkableInfo := android.OtherModuleProvider(ctx, dep, cc.LinkableInfoProvider)
		commonInfo := android.OtherModulePointerProviderOrDefault(ctx, dep, android.CommonModuleInfoProvider)
		if hasRustInfo && !linkableInfo.Static && !linkableInfo.Shared {
			//Handle Rust Modules
			makeLibName := rustMakeLibName(rustInfo, linkableInfo, commonInfo, depName+rustInfo.RustSubName)

			switch {
			case depTag == dylibDepTag:
				dylib := rustInfo.CompilerInfo.LibraryInfo
				if dylib == nil || !dylib.Dylib {
					ctx.ModuleErrorf("mod %q not an dylib library", depName)
					return
				}
				directDylibDeps = append(directDylibDeps, linkableInfo)
				mod.Properties.AndroidMkDylibs = append(mod.Properties.AndroidMkDylibs, makeLibName)
				mod.Properties.SnapshotDylibs = append(mod.Properties.SnapshotDylibs, cc.BaseLibName(depName))

				depPaths.directImplementationDeps = append(depPaths.directImplementationDeps, android.OutputFileForModule(ctx, dep, ""))
				if info, ok := android.OtherModuleProvider(ctx, dep, cc.ImplementationDepInfoProvider); ok {
					depPaths.transitiveImplementationDeps = append(depPaths.transitiveImplementationDeps, info.ImplementationDeps)
				}

				if !rustInfo.CompilerInfo.NoStdlibs {
					rustDepStdLinkage := rustInfo.CompilerInfo.StdLinkageForNonDevice
					if ctx.Device() {
						rustDepStdLinkage = rustInfo.CompilerInfo.StdLinkageForDevice
					}
					if rustDepStdLinkage != modStdLinkage {
						ctx.ModuleErrorf("Rust dependency %q has the wrong StdLinkage; expected %#v, got %#v", depName, modStdLinkage, rustDepStdLinkage)
						return
					}
				}

			case depTag == rlibDepTag:
				rlib := rustInfo.CompilerInfo.LibraryInfo
				if rlib == nil || !rlib.Rlib {
					ctx.ModuleErrorf("mod %q not an rlib library", makeLibName)
					return
				}
				directRlibDeps = append(directRlibDeps, linkableInfo)
				mod.Properties.AndroidMkRlibs = append(mod.Properties.AndroidMkRlibs, makeLibName)
				mod.Properties.SnapshotRlibs = append(mod.Properties.SnapshotRlibs, cc.BaseLibName(depName))

				// rust_ffi rlibs may export include dirs, so collect those here.
				exportedInfo, _ := android.OtherModuleProvider(ctx, dep, cc.FlagExporterInfoProvider)
				depPaths.depIncludePaths = append(depPaths.depIncludePaths, exportedInfo.IncludeDirs...)
				depPaths.exportedLinkDirs = append(depPaths.exportedLinkDirs, linkPathFromFilePath(linkableInfo.OutputFile.Path()))

				// rlibs are not installed, so don't add the output file to directImplementationDeps
				if info, ok := android.OtherModuleProvider(ctx, dep, cc.ImplementationDepInfoProvider); ok {
					depPaths.transitiveImplementationDeps = append(depPaths.transitiveImplementationDeps, info.ImplementationDeps)
				}

				if !rustInfo.CompilerInfo.NoStdlibs {
					rustDepStdLinkage := rustInfo.CompilerInfo.StdLinkageForNonDevice
					if ctx.Device() {
						rustDepStdLinkage = rustInfo.CompilerInfo.StdLinkageForDevice
					}
					if rustDepStdLinkage != modStdLinkage {
						ctx.ModuleErrorf("Rust dependency %q has the wrong StdLinkage; expected %#v, got %#v", depName, modStdLinkage, rustDepStdLinkage)
						return
					}
				}

				if !mod.Rlib() {
					depPaths.ccRlibDeps = append(depPaths.ccRlibDeps, exportedInfo.RustRlibDeps...)
				} else {
					// rlibs need to reexport these
					depPaths.reexportedCcRlibDeps = append(depPaths.reexportedCcRlibDeps, exportedInfo.RustRlibDeps...)
				}

			case depTag == procMacroDepTag:
				directProcMacroDeps = append(directProcMacroDeps, linkableInfo)
				mod.Properties.AndroidMkProcMacroLibs = append(mod.Properties.AndroidMkProcMacroLibs, makeLibName)
				// proc_macro link dirs need to be exported, so collect those here.
				depPaths.exportedLinkDirs = append(depPaths.exportedLinkDirs, linkPathFromFilePath(linkableInfo.OutputFile.Path()))

			case depTag == sourceDepTag:
				if _, ok := mod.sourceProvider.(*protobufDecorator); ok {
					collectIncludedProtos(mod, rustInfo, linkableInfo)
				}
			case cc.IsStaticDepTag(depTag):
				// Rust FFI rlibs should not be declared in a Rust modules
				// "static_libs" list as we can't handle them properly at the
				// moment (for example, they only produce an rlib-std variant).
				// Instead, a normal rust_library variant should be used.
				ctx.PropertyErrorf("static_libs",
					"found '%s' in static_libs; use a rust_library module in rustlibs instead of a rust_ffi module in static_libs",
					depName)

			}

			transitiveAndroidMkSharedLibs = append(transitiveAndroidMkSharedLibs, rustInfo.TransitiveAndroidMkSharedLibs)

			if android.IsSourceDepTagWithOutputTag(depTag, "") {
				// Since these deps are added in path_properties.go via AddDependencies, we need to ensure the correct
				// OS/Arch variant is used.
				var helper string
				if ctx.Host() {
					helper = "missing 'host_supported'?"
				} else {
					helper = "device module defined?"
				}

				if commonInfo.Target.Os != ctx.Os() {
					ctx.ModuleErrorf("OS mismatch on dependency %q (%s)", dep.Name(), helper)
					return
				} else if commonInfo.Target.Arch.ArchType != ctx.Arch().ArchType {
					ctx.ModuleErrorf("Arch mismatch on dependency %q (%s)", dep.Name(), helper)
					return
				}
				directSrcProvidersDeps = append(directSrcProvidersDeps, &dep)
			}

			exportedRustInfo, _ := android.OtherModuleProvider(ctx, dep, RustFlagExporterInfoProvider)
			exportedInfo, _ := android.OtherModuleProvider(ctx, dep, RustFlagExporterInfoProvider)
			//Append the dependencies exported objects, except for proc-macros which target a different arch/OS
			if depTag != procMacroDepTag {
				depPaths.depFlags = append(depPaths.depFlags, exportedInfo.Flags...)
				depPaths.rustLibObjects = append(depPaths.rustLibObjects, exportedInfo.RustLibObjects...)
				depPaths.sharedLibObjects = append(depPaths.sharedLibObjects, exportedInfo.SharedLibPaths...)
				depPaths.staticLibObjects = append(depPaths.staticLibObjects, exportedInfo.StaticLibObjects...)
				depPaths.wholeStaticLibObjects = append(depPaths.wholeStaticLibObjects, exportedInfo.WholeStaticLibObjects...)
				depPaths.linkDirs = append(depPaths.linkDirs, exportedInfo.LinkDirs...)

				depPaths.reexportedWholeCcRlibDeps = append(depPaths.reexportedWholeCcRlibDeps, exportedRustInfo.WholeRustRlibDeps...)
				if !mod.Rlib() {
					depPaths.ccRlibDeps = append(depPaths.ccRlibDeps, exportedRustInfo.WholeRustRlibDeps...)
				}
			}

			if depTag == dylibDepTag || depTag == rlibDepTag || depTag == procMacroDepTag {
				linkFile := linkableInfo.UnstrippedOutputFile
				linkDir := linkPathFromFilePath(linkFile)
				if lib, ok := mod.compiler.(exportedFlagsProducer); ok {
					lib.exportLinkDirs(linkDir)
				}
			}

			if depTag == sourceDepTag {
				if _, ok := mod.sourceProvider.(*protobufDecorator); ok && mod.Source() {
					if rustInfo.SourceProviderInfo.ProtobufDecoratorInfo != nil {
						exportedInfo, _ := android.OtherModuleProvider(ctx, dep, cc.FlagExporterInfoProvider)
						depPaths.depIncludePaths = append(depPaths.depIncludePaths, exportedInfo.IncludeDirs...)
					}
				}
			}
		} else if hasLinkableInfo {
			//Handle C dependencies
			makeLibName := cc.MakeLibName(ccInfo, linkableInfo, commonInfo, depName)
			if !hasRustInfo {
				if commonInfo.Target.Os != ctx.Os() {
					ctx.ModuleErrorf("OS mismatch between %q and %q", ctx.ModuleName(), depName)
					return
				}
				if commonInfo.Target.Arch.ArchType != ctx.Arch().ArchType {
					ctx.ModuleErrorf("Arch mismatch between %q and %q", ctx.ModuleName(), depName)
					return
				}
			}
			ccLibPath := linkableInfo.OutputFile
			if !ccLibPath.Valid() {
				if !ctx.Config().AllowMissingDependencies() {
					ctx.ModuleErrorf("Invalid output file when adding dep %q to %q", depName, ctx.ModuleName())
				} else {
					ctx.AddMissingDependencies([]string{depName})
				}
				return
			}

			linkPath := linkPathFromFilePath(ccLibPath.Path())

			exportDep := false
			switch {
			case cc.IsStaticDepTag(depTag):
				if cc.IsWholeStaticLib(depTag) {
					// rustc will bundle static libraries when they're passed with "-lstatic=<lib>". This will fail
					// if the library is not prefixed by "lib".
					if mod.Binary() {
						// Since binaries don't need to 'rebundle' these like libraries and only use these for the
						// final linkage, pass the args directly to the linker to handle these cases.
						depPaths.depLinkFlags = append(depPaths.depLinkFlags, []string{"-Wl,--whole-archive", ccLibPath.Path().String(), "-Wl,--no-whole-archive"}...)
					} else if libName, ok := libNameFromFilePath(ccLibPath.Path()); ok {
						depPaths.depFlags = append(depPaths.depFlags, "-lstatic:+whole-archive="+libName)
						depPaths.depLinkFlags = append(depPaths.depLinkFlags, ccLibPath.Path().String())
					} else {
						ctx.ModuleErrorf("'%q' cannot be listed as a whole_static_library in Rust modules unless the output is prefixed by 'lib'", depName, ctx.ModuleName())
					}
				}

				exportedInfo, _ := android.OtherModuleProvider(ctx, dep, cc.FlagExporterInfoProvider)
				if cc.IsWholeStaticLib(depTag) {
					// Add whole staticlibs to wholeStaticLibObjects to propagate to Rust all dependents.
					depPaths.wholeStaticLibObjects = append(depPaths.wholeStaticLibObjects, ccLibPath.String())

					// We also propagate forward whole-static'd cc staticlibs with rust_ffi_rlib dependencies
					// We don't need to check a hypothetical exportedRustInfo.WholeRustRlibDeps because we
					// wouldn't expect a rust_ffi_rlib to be listed in `static_libs` (Soong explicitly disallows this)
					depPaths.reexportedWholeCcRlibDeps = append(depPaths.reexportedWholeCcRlibDeps, exportedInfo.RustRlibDeps...)
				} else {
					// If not whole_static, add to staticLibObjects, which only propagate through rlibs to their dependents.
					depPaths.staticLibObjects = append(depPaths.staticLibObjects, ccLibPath.String())

					if mod.Rlib() {
						// rlibs propagate their inherited rust_ffi_rlibs forward.
						depPaths.reexportedCcRlibDeps = append(depPaths.reexportedCcRlibDeps, exportedInfo.RustRlibDeps...)
					}
				}

				depPaths.linkDirs = append(depPaths.linkDirs, linkPath)
				depPaths.depIncludePaths = append(depPaths.depIncludePaths, exportedInfo.IncludeDirs...)
				depPaths.depSystemIncludePaths = append(depPaths.depSystemIncludePaths, exportedInfo.SystemIncludeDirs...)
				depPaths.depClangFlags = append(depPaths.depClangFlags, exportedInfo.Flags...)
				depPaths.depGeneratedHeaders = append(depPaths.depGeneratedHeaders, exportedInfo.GeneratedHeaders...)

				if !mod.Rlib() {
					// rlibs don't need to build the generated static library, so they don't need to track these.
					depPaths.ccRlibDeps = append(depPaths.ccRlibDeps, exportedInfo.RustRlibDeps...)
				}

				directStaticLibDeps = append(directStaticLibDeps, linkableInfo)

				// Record baseLibName for snapshots.
				mod.Properties.SnapshotStaticLibs = append(mod.Properties.SnapshotStaticLibs, cc.BaseLibName(depName))

				mod.Properties.AndroidMkStaticLibs = append(mod.Properties.AndroidMkStaticLibs, makeLibName)
			case cc.IsSharedDepTag(depTag):
				// For the shared lib dependencies, we may link to the stub variant
				// of the dependency depending on the context (e.g. if this
				// dependency crosses the APEX boundaries).
				sharedLibraryInfo, exportedInfo := cc.ChooseStubOrImpl(ctx, dep)

				if !sharedLibraryInfo.IsStubs {
					// TODO(b/362509506): remove this additional check once all apex_exclude uses are switched to stubs.
					if !linkableInfo.RustApexExclude {
						depPaths.directImplementationDeps = append(depPaths.directImplementationDeps, android.OutputFileForModule(ctx, dep, ""))
						if info, ok := android.OtherModuleProvider(ctx, dep, cc.ImplementationDepInfoProvider); ok {
							depPaths.transitiveImplementationDeps = append(depPaths.transitiveImplementationDeps, info.ImplementationDeps)
						}
					}
				}

				// Re-get linkObject as ChooseStubOrImpl actually tells us which
				// object (either from stub or non-stub) to use.
				ccLibPath = android.OptionalPathForPath(sharedLibraryInfo.SharedLibrary)
				if !ccLibPath.Valid() {
					if !ctx.Config().AllowMissingDependencies() {
						ctx.ModuleErrorf("Invalid output file when adding dep %q to %q", depName, ctx.ModuleName())
					} else {
						ctx.AddMissingDependencies([]string{depName})
					}
					return
				}
				linkPath = linkPathFromFilePath(ccLibPath.Path())

				depPaths.linkDirs = append(depPaths.linkDirs, linkPath)
				depPaths.sharedLibObjects = append(depPaths.sharedLibObjects, ccLibPath.String())
				depPaths.depIncludePaths = append(depPaths.depIncludePaths, exportedInfo.IncludeDirs...)
				depPaths.depSystemIncludePaths = append(depPaths.depSystemIncludePaths, exportedInfo.SystemIncludeDirs...)
				depPaths.depClangFlags = append(depPaths.depClangFlags, exportedInfo.Flags...)
				depPaths.depGeneratedHeaders = append(depPaths.depGeneratedHeaders, exportedInfo.GeneratedHeaders...)
				directSharedLibDeps = append(directSharedLibDeps, sharedLibraryInfo)

				// Record baseLibName for snapshots.
				mod.Properties.SnapshotSharedLibs = append(mod.Properties.SnapshotSharedLibs, cc.BaseLibName(depName))

				directAndroidMkSharedLibs = append(directAndroidMkSharedLibs, makeLibName)
				exportDep = true
			case cc.IsHeaderDepTag(depTag):
				exportedInfo, _ := android.OtherModuleProvider(ctx, dep, cc.FlagExporterInfoProvider)
				depPaths.depIncludePaths = append(depPaths.depIncludePaths, exportedInfo.IncludeDirs...)
				depPaths.depSystemIncludePaths = append(depPaths.depSystemIncludePaths, exportedInfo.SystemIncludeDirs...)
				depPaths.depGeneratedHeaders = append(depPaths.depGeneratedHeaders, exportedInfo.GeneratedHeaders...)
				mod.Properties.AndroidMkHeaderLibs = append(mod.Properties.AndroidMkHeaderLibs, makeLibName)
			case depTag == cc.CrtBeginDepTag:
				depPaths.CrtBegin = append(depPaths.CrtBegin, ccLibPath.Path())
			case depTag == cc.CrtEndDepTag:
				depPaths.CrtEnd = append(depPaths.CrtEnd, ccLibPath.Path())
			}

			// Make sure shared dependencies are propagated
			if lib, ok := mod.compiler.(exportedFlagsProducer); ok && exportDep {
				lib.exportLinkDirs(linkPath)
				lib.exportSharedLibs(ccLibPath.String())
			}
		} else {
			switch {
			case depTag == cc.CrtBeginDepTag:
				depPaths.CrtBegin = append(depPaths.CrtBegin, android.OutputFileForModule(ctx, dep, ""))
			case depTag == cc.CrtEndDepTag:
				depPaths.CrtEnd = append(depPaths.CrtEnd, android.OutputFileForModule(ctx, dep, ""))
			}
		}

		if srcDep, ok := android.OtherModuleProvider(ctx, dep, android.SourceFilesInfoProvider); ok {
			if android.IsSourceDepTagWithOutputTag(depTag, "") {
				// These are usually genrules which don't have per-target variants.
				directSrcDeps = append(directSrcDeps, srcDep)
			}
		}
	})

	mod.transitiveAndroidMkSharedLibs = depset.New[string](depset.PREORDER, directAndroidMkSharedLibs, transitiveAndroidMkSharedLibs)

	var rlibDepFiles RustLibraries
	aliases := mod.compiler.Aliases()
	for _, dep := range directRlibDeps {
		crateName := dep.CrateName
		if alias, aliased := aliases[crateName]; aliased {
			crateName = alias
		}
		rlibDepFiles = append(rlibDepFiles, RustLibrary{Path: dep.UnstrippedOutputFile, CrateName: crateName})
	}
	var dylibDepFiles RustLibraries
	for _, dep := range directDylibDeps {
		crateName := dep.CrateName
		if alias, aliased := aliases[crateName]; aliased {
			crateName = alias
		}
		dylibDepFiles = append(dylibDepFiles, RustLibrary{Path: dep.UnstrippedOutputFile, CrateName: crateName})
	}
	var procMacroDepFiles RustLibraries
	for _, dep := range directProcMacroDeps {
		crateName := dep.CrateName
		if alias, aliased := aliases[crateName]; aliased {
			crateName = alias
		}
		procMacroDepFiles = append(procMacroDepFiles, RustLibrary{Path: dep.UnstrippedOutputFile, CrateName: crateName})
	}

	var staticLibDepFiles android.Paths
	for _, dep := range directStaticLibDeps {
		staticLibDepFiles = append(staticLibDepFiles, dep.OutputFile.Path())
	}

	var sharedLibFiles android.Paths
	var sharedLibDepFiles android.Paths
	for _, dep := range directSharedLibDeps {
		sharedLibFiles = append(sharedLibFiles, dep.SharedLibrary)
		if dep.TableOfContents.Valid() {
			sharedLibDepFiles = append(sharedLibDepFiles, dep.TableOfContents.Path())
		} else {
			sharedLibDepFiles = append(sharedLibDepFiles, dep.SharedLibrary)
		}
	}

	var srcProviderDepFiles android.Paths
	for _, dep := range directSrcProvidersDeps {
		srcs := android.OutputFilesForModule(ctx, *dep, "")
		srcProviderDepFiles = append(srcProviderDepFiles, srcs...)
	}
	for _, dep := range directSrcDeps {
		srcs := dep.Srcs
		srcProviderDepFiles = append(srcProviderDepFiles, srcs...)
	}

	depPaths.RLibs = append(depPaths.RLibs, rlibDepFiles...)
	depPaths.DyLibs = append(depPaths.DyLibs, dylibDepFiles...)
	depPaths.SharedLibs = append(depPaths.SharedLibs, sharedLibFiles...)
	depPaths.SharedLibDeps = append(depPaths.SharedLibDeps, sharedLibDepFiles...)
	depPaths.StaticLibs = append(depPaths.StaticLibs, staticLibDepFiles...)
	depPaths.ProcMacros = append(depPaths.ProcMacros, procMacroDepFiles...)
	depPaths.SrcDeps = append(depPaths.SrcDeps, srcProviderDepFiles...)

	// Dedup exported flags from dependencies
	depPaths.linkDirs = android.FirstUniqueStrings(depPaths.linkDirs)
	depPaths.rustLibObjects = android.FirstUniqueStrings(depPaths.rustLibObjects)
	depPaths.staticLibObjects = android.FirstUniqueStrings(depPaths.staticLibObjects)
	depPaths.wholeStaticLibObjects = android.FirstUniqueStrings(depPaths.wholeStaticLibObjects)
	depPaths.sharedLibObjects = android.FirstUniqueStrings(depPaths.sharedLibObjects)
	depPaths.depFlags = android.FirstUniqueStrings(depPaths.depFlags)
	depPaths.depClangFlags = android.FirstUniqueStrings(depPaths.depClangFlags)
	depPaths.depIncludePaths = android.FirstUniquePaths(depPaths.depIncludePaths)
	depPaths.depSystemIncludePaths = android.FirstUniquePaths(depPaths.depSystemIncludePaths)
	depPaths.depLinkFlags = android.FirstUniqueStrings(depPaths.depLinkFlags)
	depPaths.reexportedCcRlibDeps = android.FirstUniqueFunc(depPaths.reexportedCcRlibDeps, cc.EqRustRlibDeps)
	depPaths.reexportedWholeCcRlibDeps = android.FirstUniqueFunc(depPaths.reexportedWholeCcRlibDeps, cc.EqRustRlibDeps)
	depPaths.ccRlibDeps = android.FirstUniqueFunc(depPaths.ccRlibDeps, cc.EqRustRlibDeps)

	return depPaths
}

func (mod *Module) InstallInData() bool {
	if mod.compiler == nil {
		return false
	}
	return mod.compiler.inData()
}

func (mod *Module) InstallInRamdisk() bool {
	return mod.InRamdisk()
}

func (mod *Module) InstallInVendorRamdisk() bool {
	return mod.InVendorRamdisk()
}

func (mod *Module) InstallInRecovery() bool {
	return mod.InRecovery()
}

func linkPathFromFilePath(filepath android.Path) string {
	return strings.Split(filepath.String(), filepath.Base())[0]
}

// usePublicApi returns true if the rust variant should link against NDK (publicapi)
func (r *Module) usePublicApi() bool {
	return r.Device() && r.UseSdk()
}

// useVendorApi returns true if the rust variant should link against LLNDK (vendorapi)
func (r *Module) useVendorApi() bool {
	return r.Device() && (r.InVendor() || r.InProduct())
}

func (mod *Module) DepsMutator(actx android.BottomUpMutatorContext) {
	ctx := &depsContext{
		BottomUpMutatorContext: actx,
	}

	deps := mod.deps(ctx)
	var commonDepVariations []blueprint.Variation

	if ctx.Os() == android.Android {
		deps.SharedLibs, _ = cc.FilterNdkLibs(mod, ctx.Config(), deps.SharedLibs)
	}

	stdLinkage := "dylib-std"
	if mod.compiler.stdLinkage(ctx.Device()) == RlibLinkage {
		stdLinkage = "rlib-std"
	}

	rlibDepVariations := commonDepVariations

	if lib, ok := mod.compiler.(libraryInterface); !ok || !lib.sysroot() {
		rlibDepVariations = append(rlibDepVariations,
			blueprint.Variation{Mutator: "rust_stdlinkage", Variation: stdLinkage})
	}

	// rlibs
	rlibDepVariations = append(rlibDepVariations, blueprint.Variation{Mutator: "rust_libraries", Variation: rlibVariation})
	for _, lib := range deps.Rlibs {
		depTag := rlibDepTag
		actx.AddVariationDependencies(rlibDepVariations, depTag, lib)
	}

	// dylibs
	dylibDepVariations := append(commonDepVariations, blueprint.Variation{Mutator: "rust_libraries", Variation: dylibVariation})

	for _, lib := range deps.Dylibs {
		actx.AddVariationDependencies(dylibDepVariations, dylibDepTag, lib)
	}

	// rustlibs
	if deps.Rustlibs != nil {
		if !mod.compiler.Disabled() {
			for _, lib := range deps.Rustlibs {
				autoDep := mod.compiler.(autoDeppable).autoDep(ctx)
				if autoDep.depTag == rlibDepTag {
					// Handle the rlib deptag case
					actx.AddVariationDependencies(rlibDepVariations, rlibDepTag, lib)

				} else {
					// autoDep.depTag is a dylib depTag. Not all rustlibs may be available as a dylib however.
					// Check for the existence of the dylib deptag variant. Select it if available,
					// otherwise select the rlib variant.
					autoDepVariations := append(commonDepVariations,
						blueprint.Variation{Mutator: "rust_libraries", Variation: autoDep.variation})
					if actx.OtherModuleDependencyVariantExists(autoDepVariations, lib) {
						actx.AddVariationDependencies(autoDepVariations, autoDep.depTag, lib)

					} else {
						// If there's no dylib dependency available, try to add the rlib dependency instead.
						actx.AddVariationDependencies(rlibDepVariations, rlibDepTag, lib)

					}
				}
			}
		} else if _, ok := mod.sourceProvider.(*protobufDecorator); ok {
			for _, lib := range deps.Rustlibs {
				srcProviderVariations := append(commonDepVariations,
					blueprint.Variation{Mutator: "rust_libraries", Variation: sourceVariation})

				// Only add rustlib dependencies if they're source providers themselves.
				// This is used to track which crate names need to be added to the source generated
				// in the rust_protobuf mod.rs.
				if actx.OtherModuleDependencyVariantExists(srcProviderVariations, lib) {
					actx.AddVariationDependencies(srcProviderVariations, sourceDepTag, lib)
				}
			}
		}
	}

	// stdlibs
	if deps.Stdlibs != nil {
		if mod.compiler.stdLinkage(ctx.Device()) == RlibLinkage {
			for _, lib := range deps.Stdlibs {
				actx.AddVariationDependencies(append(commonDepVariations, []blueprint.Variation{{Mutator: "rust_libraries", Variation: "rlib"}}...),
					rlibDepTag, lib)
			}
		} else {
			for _, lib := range deps.Stdlibs {
				actx.AddVariationDependencies(dylibDepVariations, dylibDepTag, lib)

			}
		}
	}

	for _, lib := range deps.SharedLibs {
		depTag := cc.SharedDepTag()
		name, version := cc.StubsLibNameAndVersion(lib)

		variations := []blueprint.Variation{
			{Mutator: "link", Variation: "shared"},
		}
		cc.AddSharedLibDependenciesWithVersions(ctx, mod, variations, depTag, name, version, false)
	}

	for _, lib := range deps.WholeStaticLibs {
		depTag := cc.StaticDepTag(true)

		actx.AddVariationDependencies([]blueprint.Variation{
			{Mutator: "link", Variation: "static"},
		}, depTag, lib)
	}

	for _, lib := range deps.StaticLibs {
		depTag := cc.StaticDepTag(false)

		actx.AddVariationDependencies([]blueprint.Variation{
			{Mutator: "link", Variation: "static"},
		}, depTag, lib)
	}

	actx.AddVariationDependencies(nil, cc.HeaderDepTag(), deps.HeaderLibs...)

	crtVariations := cc.GetCrtVariations(ctx, mod)
	for _, crt := range deps.CrtBegin {
		actx.AddVariationDependencies(crtVariations, cc.CrtBeginDepTag, crt)
	}
	for _, crt := range deps.CrtEnd {
		actx.AddVariationDependencies(crtVariations, cc.CrtEndDepTag, crt)
	}

	if mod.sourceProvider != nil {
		if bindgen, ok := mod.sourceProvider.(*bindgenDecorator); ok &&
			bindgen.Properties.Custom_bindgen != "" {
			actx.AddFarVariationDependencies(ctx.Config().BuildOSTarget.Variations(), customBindgenDepTag,
				bindgen.Properties.Custom_bindgen)
		}
	}

	actx.AddVariationDependencies([]blueprint.Variation{
		{Mutator: "link", Variation: "shared"},
	}, dataLibDepTag, deps.DataLibs...)

	actx.AddVariationDependencies(nil, dataBinDepTag, deps.DataBins...)

	// proc_macros are compiler plugins, and so we need the host arch variant as a dependendcy.
	actx.AddFarVariationDependencies(ctx.Config().BuildOSTarget.Variations(), procMacroDepTag, deps.ProcMacros...)

	mod.afdo.addDep(ctx, actx)
}

func BeginMutator(ctx android.BottomUpMutatorContext) {
	if mod, ok := ctx.Module().(*Module); ok && mod.Enabled(ctx) {
		mod.beginMutator(ctx)
	}
}

func (mod *Module) beginMutator(actx android.BottomUpMutatorContext) {
	ctx := &baseModuleContext{
		BaseModuleContext: actx,
	}

	mod.begin(ctx)
}

func (mod *Module) Name() string {
	name := mod.ModuleBase.Name()
	if p, ok := mod.compiler.(interface {
		Name(string) string
	}); ok {
		name = p.Name(name)
	}
	return name
}

func (mod *Module) disableClippy() {
	if mod.clippy != nil {
		mod.clippy.Properties.Clippy_lints = proptools.StringPtr("none")
	}
}

var _ android.HostToolProvider = (*Module)(nil)

func (mod *Module) HostToolPath() android.OptionalPath {
	if !mod.Host() {
		return android.OptionalPath{}
	}
	if binary, ok := mod.compiler.(*binaryDecorator); ok {
		return android.OptionalPathForPath(binary.baseCompiler.path)
	} else if pm, ok := mod.compiler.(*procMacroDecorator); ok {
		// Even though proc-macros aren't strictly "tools", since they target the compiler
		// and act as compiler plugins, we treat them similarly.
		return android.OptionalPathForPath(pm.baseCompiler.path)
	}
	return android.OptionalPath{}
}

var _ android.ApexModule = (*Module)(nil)

// If a module is marked for exclusion from apexes, don't provide apex variants.
// TODO(b/362509506): remove this once all apex_exclude usages are removed.
func (m *Module) CanHaveApexVariants() bool {
	if m.ApexExclude() {
		return false
	} else {
		return m.ApexModuleBase.CanHaveApexVariants()
	}
}

func (mod *Module) MinSdkVersion() string {
	return String(mod.Properties.Min_sdk_version)
}

// Implements android.ApexModule
func (mod *Module) MinSdkVersionSupported(ctx android.BaseModuleContext) android.ApiLevel {
	minSdkVersion := mod.MinSdkVersion()
	if minSdkVersion == "apex_inherit" {
		return android.MinApiLevel
	}

	if minSdkVersion == "" {
		return android.NoneApiLevel
	}
	// Not using nativeApiLevelFromUser because the context here is not
	// necessarily a native context.
	ver, err := android.ApiLevelFromUserWithConfig(ctx.Config(), minSdkVersion)
	if err != nil {
		return android.NoneApiLevel
	}

	return ver
}

// Implements android.ApexModule
func (mod *Module) AlwaysRequiresPlatformApexVariant() bool {
	// stub libraries and native bridge libraries are always available to platform
	// TODO(b/362509506): remove the ApexExclude() check once all apex_exclude uses are switched to stubs.
	return mod.IsStubs() || mod.Target().NativeBridge == android.NativeBridgeEnabled || mod.ApexExclude()
}

// Implements android.ApexModule
type RustDepInSameApexChecker struct {
	Static           bool
	HasStubsVariants bool
	ApexExclude      bool
	Host             bool
}

func (mod *Module) GetDepInSameApexChecker() android.DepInSameApexChecker {
	return RustDepInSameApexChecker{
		Static:           mod.Static(),
		HasStubsVariants: mod.HasStubsVariants(),
		ApexExclude:      mod.ApexExclude(),
		Host:             mod.Host(),
	}
}

func (r RustDepInSameApexChecker) OutgoingDepIsInSameApex(depTag blueprint.DependencyTag) bool {
	if depTag == procMacroDepTag || depTag == customBindgenDepTag {
		return false
	}

	if r.Static && cc.IsSharedDepTag(depTag) {
		// shared_lib dependency from a static lib is considered as crossing
		// the APEX boundary because the dependency doesn't actually is
		// linked; the dependency is used only during the compilation phase.
		return false
	}

	if depTag == cc.StubImplDepTag {
		// We don't track from an implementation library to its stubs.
		return false
	}

	if cc.ExcludeInApexDepTag(depTag) {
		return false
	}

	// TODO(b/362509506): remove once all apex_exclude uses are switched to stubs.
	if r.ApexExclude {
		return false
	}

	return true
}

func (r RustDepInSameApexChecker) IncomingDepIsInSameApex(depTag blueprint.DependencyTag) bool {
	if r.Host {
		return false
	}
	// TODO(b/362509506): remove once all apex_exclude uses are switched to stubs.
	if r.ApexExclude {
		return false
	}

	if r.HasStubsVariants {
		if cc.IsSharedDepTag(depTag) && !cc.IsExplicitImplSharedDepTag(depTag) {
			// dynamic dep to a stubs lib crosses APEX boundary
			return false
		}
		if cc.IsRuntimeDepTag(depTag) {
			// runtime dep to a stubs lib also crosses APEX boundary
			return false
		}
		if cc.IsHeaderDepTag(depTag) {
			return false
		}
	}
	return true
}

// Overrides ApexModule.IsInstallabeToApex()
func (mod *Module) IsInstallableToApex() bool {
	// TODO(b/362509506): remove once all apex_exclude uses are switched to stubs.
	if mod.ApexExclude() {
		return false
	}

	if mod.compiler != nil {
		if lib, ok := mod.compiler.(libraryInterface); ok {
			return (lib.shared() || lib.dylib()) && !lib.BuildStubs()
		}
		if _, ok := mod.compiler.(*binaryDecorator); ok {
			return true
		}
	}
	return false
}

// If a library file has a "lib" prefix, extract the library name without the prefix.
func libNameFromFilePath(filepath android.Path) (string, bool) {
	libName := strings.TrimSuffix(filepath.Base(), filepath.Ext())
	if strings.HasPrefix(libName, "lib") {
		libName = libName[3:]
		return libName, true
	}
	return "", false
}

func kytheExtractRustFactory() android.Singleton {
	return &kytheExtractRustSingleton{}
}

type kytheExtractRustSingleton struct {
}

func (k kytheExtractRustSingleton) GenerateBuildActions(ctx android.SingletonContext) {
	var xrefTargets android.Paths
	ctx.VisitAllModuleProxies(func(module android.ModuleProxy) {
		if rustModule, ok := android.OtherModuleProvider(ctx, module, RustInfoProvider); ok {
			xrefTargets = append(xrefTargets, rustModule.XrefRustFiles...)
		}
	})
	if len(xrefTargets) > 0 {
		ctx.Phony("xref_rust", xrefTargets...)
	}
}

func (c *Module) Partition() string {
	return ""
}

var Bool = proptools.Bool
var BoolDefault = proptools.BoolDefault
var String = proptools.String
var StringPtr = proptools.StringPtr
