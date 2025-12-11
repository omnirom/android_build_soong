// Copyright (C) 2018 The Android Open Source Project
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

// package apex implements build rules for creating the APEX files which are container for
// lower-level system components. See https://source.android.com/devices/tech/ota/apex
package apex

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/google/blueprint"
	"github.com/google/blueprint/depset"
	"github.com/google/blueprint/proptools"

	"android/soong/android"
	"android/soong/bpf"
	"android/soong/cc"
	"android/soong/dexpreopt"
	prebuilt_etc "android/soong/etc"
	"android/soong/filesystem"
	"android/soong/java"
	"android/soong/rust"
	"android/soong/sh"
)

func init() {
	registerApexBuildComponents(android.InitRegistrationContext)
}

func registerApexBuildComponents(ctx android.RegistrationContext) {
	ctx.RegisterModuleType("apex", BundleFactory)
	ctx.RegisterModuleType("apex_test", TestApexBundleFactory)
	ctx.RegisterModuleType("apex_vndk", vndkApexBundleFactory)
	ctx.RegisterModuleType("apex_defaults", DefaultsFactory)
	ctx.RegisterModuleType("prebuilt_apex", PrebuiltFactory)
	ctx.RegisterModuleType("override_apex", OverrideApexFactory)
	ctx.RegisterModuleType("apex_set", apexSetFactory)

	ctx.PreDepsMutators(RegisterPreDepsMutators)
	ctx.PostDepsMutators(RegisterPostDepsMutators)
}

func RegisterPreDepsMutators(ctx android.RegisterMutatorsContext) {
	ctx.BottomUp("apex_vndk_deps", apexVndkDepsMutator).UsesReverseDependencies()
}

func RegisterPostDepsMutators(ctx android.RegisterMutatorsContext) {
	ctx.BottomUp("apex_unique", apexUniqueVariationsMutator)
	// Run mark_platform_availability before the apexMutator as the apexMutator needs to know whether
	// it should create a platform variant.
	ctx.BottomUp("mark_platform_availability", markPlatformAvailability)
	ctx.InfoBasedTransition("apex", android.NewGenericTransitionMutatorAdapter(&apexTransitionMutator{}))
}

type apexBundleProperties struct {
	// Json manifest file describing meta info of this APEX bundle. Refer to
	// system/apex/proto/apex_manifest.proto for the schema. Default: "apex_manifest.json"
	Manifest *string `android:"path"`

	// AndroidManifest.xml file used for the zip container of this APEX bundle. If unspecified,
	// a default one is automatically generated.
	AndroidManifest proptools.Configurable[string] `android:"path,replace_instead_of_append"`

	// Determines the file contexts file for setting the security contexts to files in this APEX
	// bundle. For platform APEXes, this should points to a file under /system/sepolicy Default:
	// /system/sepolicy/apex/<module_name>_file_contexts.
	File_contexts *string `android:"path"`

	// Path to the canned fs config file for customizing file's
	// uid/gid/mod/capabilities. The content of this file is appended to the
	// default config, so that the custom entries are preferred. The format is
	// /<path_or_glob> <uid> <gid> <mode> [capabilities=0x<cap>], where
	// path_or_glob is a path or glob pattern for a file or set of files,
	// uid/gid are numerial values of user ID and group ID, mode is octal value
	// for the file mode, and cap is hexadecimal value for the capability.
	Canned_fs_config proptools.Configurable[string] `android:"path,replace_instead_of_append"`

	ApexNativeDependencies

	Multilib apexMultilibProperties

	// List of runtime resource overlays (RROs) that are embedded inside this APEX.
	Rros []string

	// List of bootclasspath fragments that are embedded inside this APEX bundle.
	Bootclasspath_fragments proptools.Configurable[[]string]

	// List of systemserverclasspath fragments that are embedded inside this APEX bundle.
	Systemserverclasspath_fragments proptools.Configurable[[]string]

	// List of java libraries that are embedded inside this APEX bundle.
	Java_libs []string

	// List of sh binaries that are embedded inside this APEX bundle.
	Sh_binaries []string

	// List of platform_compat_config files that are embedded inside this APEX bundle.
	Compat_configs []string

	// List of filesystem images that are embedded inside this APEX bundle.
	Filesystems []string

	// List of module names which we don't want to add as transitive deps. This can be used as
	// a workaround when the current implementation collects more than necessary. For example,
	// Rust binaries with prefer_rlib:true add unnecessary dependencies.
	Unwanted_transitive_deps []string

	// Whether this APEX is considered updatable or not. When set to true, this will enforce
	// additional rules for making sure that the APEX is truly updatable. To be updatable,
	// min_sdk_version should be set as well. This will also disable the size optimizations like
	// symlinking to the system libs. Default is true.
	Updatable *bool

	// Marks that this APEX is designed to be updatable in the future, although it's not
	// updatable yet. This is used to mimic some of the build behaviors that are applied only to
	// updatable APEXes. Currently, this disables the size optimization, so that the size of
	// APEX will not increase when the APEX is actually marked as truly updatable. Default is
	// false.
	Future_updatable *bool

	// Whether this APEX can use platform APIs or not. Can be set to true only when `updatable:
	// false`. Default is false.
	Platform_apis *bool

	// Whether this APEX is installable to one of the partitions like system, vendor, etc.
	// Default: true.
	Installable *bool

	// The type of filesystem to use. Either 'ext4', 'f2fs' or 'erofs'. Default 'ext4'.
	Payload_fs_type *string

	// For telling the APEX to ignore special handling for system libraries such as bionic.
	// Default is false.
	Ignore_system_library_special_case *bool

	// Whenever apex_payload.img of the APEX should not be dm-verity signed. Should be only
	// used in tests.
	Test_only_unsigned_payload *bool

	// Whenever apex should be compressed, regardless of product flag used. Should be only
	// used in tests.
	Test_only_force_compression *bool

	// Put extra tags (signer=<value>) to apexkeys.txt, so that release tools can sign this apex
	// with the tool to sign payload contents.
	Custom_sign_tool *string

	// Whether this is a dynamic common lib apex, if so the native shared libs will be placed
	// in a special way that include the digest of the lib file under /lib(64)?
	Dynamic_common_lib_apex *bool

	// Canonical name of this APEX bundle. Used to determine the path to the
	// activated APEX on device (i.e. /apex/<apexVariationName>), and used for the
	// apex mutator variations. For override_apex modules, this is the name of the
	// overridden base module.
	ApexVariationName string `blueprint:"mutated"`

	// List of sanitizer names that this APEX is enabled for
	SanitizerNames []string `blueprint:"mutated"`

	HideFromMake bool `blueprint:"mutated"`

	// Name that dependencies can specify in their apex_available properties to refer to this module.
	// If not specified, this defaults to Soong module name. This must be the name of a Soong module.
	Apex_available_name *string

	// Variant version of the mainline module. Must be an integer between 0-9
	Variant_version *string
}

type ApexNativeDependencies struct {
	// List of native libraries that are embedded inside this APEX.
	Native_shared_libs proptools.Configurable[[]string]

	// List of JNI libraries that are embedded inside this APEX.
	Jni_libs proptools.Configurable[[]string]

	// List of rust dyn libraries that are embedded inside this APEX.
	Rust_dyn_libs []string

	// List of native executables that are embedded inside this APEX.
	Binaries proptools.Configurable[[]string]

	// List of native tests that are embedded inside this APEX.
	Tests []string

	// List of filesystem images that are embedded inside this APEX bundle.
	Filesystems []string

	// List of prebuilt_etcs that are embedded inside this APEX bundle.
	Prebuilts proptools.Configurable[[]string]

	// List of native libraries to exclude from this APEX.
	Exclude_native_shared_libs []string

	// List of JNI libraries to exclude from this APEX.
	Exclude_jni_libs []string

	// List of rust dyn libraries to exclude from this APEX.
	Exclude_rust_dyn_libs []string

	// List of native executables to exclude from this APEX.
	Exclude_binaries []string

	// List of native tests to exclude from this APEX.
	Exclude_tests []string

	// List of filesystem images to exclude from this APEX bundle.
	Exclude_filesystems []string

	// List of prebuilt_etcs to exclude from this APEX bundle.
	Exclude_prebuilts []string
}

type ResolvedApexNativeDependencies struct {
	// List of native libraries that are embedded inside this APEX.
	Native_shared_libs []string

	// List of JNI libraries that are embedded inside this APEX.
	Jni_libs []string

	// List of rust dyn libraries that are embedded inside this APEX.
	Rust_dyn_libs []string

	// List of native executables that are embedded inside this APEX.
	Binaries []string

	// List of native tests that are embedded inside this APEX.
	Tests []string

	// List of filesystem images that are embedded inside this APEX bundle.
	Filesystems []string

	// List of prebuilt_etcs that are embedded inside this APEX bundle.
	Prebuilts []string

	// List of native libraries to exclude from this APEX.
	Exclude_native_shared_libs []string

	// List of JNI libraries to exclude from this APEX.
	Exclude_jni_libs []string

	// List of rust dyn libraries to exclude from this APEX.
	Exclude_rust_dyn_libs []string

	// List of native executables to exclude from this APEX.
	Exclude_binaries []string

	// List of native tests to exclude from this APEX.
	Exclude_tests []string

	// List of filesystem images to exclude from this APEX bundle.
	Exclude_filesystems []string

	// List of prebuilt_etcs to exclude from this APEX bundle.
	Exclude_prebuilts []string
}

// Merge combines another ApexNativeDependencies into this one
func (a *ResolvedApexNativeDependencies) Merge(ctx android.BaseModuleContext, b ApexNativeDependencies) {
	a.Native_shared_libs = append(a.Native_shared_libs, b.Native_shared_libs.GetOrDefault(ctx, nil)...)
	a.Jni_libs = append(a.Jni_libs, b.Jni_libs.GetOrDefault(ctx, nil)...)
	a.Rust_dyn_libs = append(a.Rust_dyn_libs, b.Rust_dyn_libs...)
	a.Binaries = append(a.Binaries, b.Binaries.GetOrDefault(ctx, nil)...)
	a.Tests = append(a.Tests, b.Tests...)
	a.Filesystems = append(a.Filesystems, b.Filesystems...)
	a.Prebuilts = append(a.Prebuilts, b.Prebuilts.GetOrDefault(ctx, nil)...)

	a.Exclude_native_shared_libs = append(a.Exclude_native_shared_libs, b.Exclude_native_shared_libs...)
	a.Exclude_jni_libs = append(a.Exclude_jni_libs, b.Exclude_jni_libs...)
	a.Exclude_rust_dyn_libs = append(a.Exclude_rust_dyn_libs, b.Exclude_rust_dyn_libs...)
	a.Exclude_binaries = append(a.Exclude_binaries, b.Exclude_binaries...)
	a.Exclude_tests = append(a.Exclude_tests, b.Exclude_tests...)
	a.Exclude_filesystems = append(a.Exclude_filesystems, b.Exclude_filesystems...)
	a.Exclude_prebuilts = append(a.Exclude_prebuilts, b.Exclude_prebuilts...)
}

type apexMultilibProperties struct {
	// Native dependencies whose compile_multilib is "first"
	First ApexNativeDependencies

	// Native dependencies whose compile_multilib is "both"
	Both ApexNativeDependencies

	// Native dependencies whose compile_multilib is "prefer32"
	Prefer32 ApexNativeDependencies

	// Native dependencies whose compile_multilib is "32"
	Lib32 ApexNativeDependencies

	// Native dependencies whose compile_multilib is "64"
	Lib64 ApexNativeDependencies
}

type apexTargetBundleProperties struct {
	Target struct {
		// Multilib properties only for android.
		Android struct {
			Multilib apexMultilibProperties
		}

		// Multilib properties only for host.
		Host struct {
			Multilib apexMultilibProperties
		}

		// Multilib properties only for host linux_bionic.
		Linux_bionic struct {
			Multilib apexMultilibProperties
		}

		// Multilib properties only for host linux_glibc.
		Linux_glibc struct {
			Multilib apexMultilibProperties
		}
	}
}

type apexArchBundleProperties struct {
	Arch struct {
		Arm struct {
			ApexNativeDependencies
		}
		Arm64 struct {
			ApexNativeDependencies
		}
		Riscv64 struct {
			ApexNativeDependencies
		}
		X86 struct {
			ApexNativeDependencies
		}
		X86_64 struct {
			ApexNativeDependencies
		}
	}
}

// These properties can be used in override_apex to override the corresponding properties in the
// base apex.
type overridableProperties struct {
	// List of APKs that are embedded inside this APEX.
	Apps proptools.Configurable[[]string]

	// List of prebuilt files that are embedded inside this APEX bundle.
	Prebuilts proptools.Configurable[[]string]

	// List of BPF programs inside this APEX bundle.
	Bpfs proptools.Configurable[[]string]

	// Names of modules to be overridden. Listed modules can only be other binaries (in Make or
	// Soong). This does not completely prevent installation of the overridden binaries, but if
	// both binaries would be installed by default (in PRODUCT_PACKAGES) the other binary will
	// be removed from PRODUCT_PACKAGES.
	Overrides []string

	Multilib apexMultilibProperties

	// Logging parent value.
	Logging_parent string

	// Apex Container package name. Override value for attribute package:name in
	// AndroidManifest.xml
	Package_name proptools.Configurable[string]

	// A txt file containing list of files that are allowed to be included in this APEX.
	Allowed_files *string `android:"path"`

	// Name of the apex_key module that provides the private key to sign this APEX bundle.
	Key *string

	// Specifies the certificate and the private key to sign the zip container of this APEX. If
	// this is "foo", foo.x509.pem and foo.pk8 under PRODUCT_DEFAULT_DEV_CERTIFICATE are used
	// as the certificate and the private key, respectively. If this is ":module", then the
	// certificate and the private key are provided from the android_app_certificate module
	// named "module".
	Certificate *string

	// Whether this APEX can be compressed or not. Setting this property to false means this
	// APEX will never be compressed. When set to true, APEX will be compressed if other
	// conditions, e.g., target device needs to support APEX compression, are also fulfilled.
	// Default: false.
	Compressible *bool

	// Trim against a specific Dynamic Common Lib APEX
	Trim_against *string

	// The minimum SDK version that this APEX must support at minimum. This is usually set to
	// the SDK version that the APEX was first introduced.
	Min_sdk_version proptools.Configurable[string] `android:"replace_instead_of_append"`

	// Mainline beta namespace. Each mainline module would have a dedicated server side
	// namespace to support flag based A/B feature testing on mainline beta population.
	Beta_namespace *string
}

// installPair stores a path to a built object and its install location.  It is used for holding
// the installation location of the dexpreopt artifacts for system server jars in apexes that need
// to be installed when the apex is installed.
type installPair struct {
	from android.Path
	to   android.InstallPath
}

type installPairs []installPair

// String converts a list of installPair structs to the form accepted by LOCAL_SOONG_INSTALL_PAIRS.
func (p installPairs) String() string {
	sb := &strings.Builder{}
	for i, pair := range p {
		if i != 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(pair.from.String())
		sb.WriteByte(':')
		sb.WriteString(pair.to.String())
	}
	return sb.String()
}

type apexBundle struct {
	// Inherited structs
	android.ModuleBase
	android.DefaultableModuleBase
	android.OverridableModuleBase

	// Properties
	properties            apexBundleProperties
	targetProperties      apexTargetBundleProperties
	archProperties        apexArchBundleProperties
	overridableProperties overridableProperties
	vndkProperties        apexVndkProperties // only for apex_vndk modules
	testProperties        apexTestProperties // only for apex_test modules

	///////////////////////////////////////////////////////////////////////////////////////////
	// Inputs

	// Keys for apex_payload.img
	publicKeyFile  android.Path
	privateKeyFile android.Path

	// Cert/priv-key for the zip container
	containerCertificateFile android.Path
	containerPrivateKeyFile  android.Path

	// Flags for special variants of APEX
	testApex bool
	vndkApex bool

	// File system type of apex_payload.img
	payloadFsType fsType

	// Whether to create symlink to the system file instead of having a file inside the apex or
	// not
	linkToSystemLib bool

	// List of files to be included in this APEX. This is filled in the first part of
	// GenerateAndroidBuildActions.
	filesInfo []apexFile

	// List of files that were excluded by the unwanted_transitive_deps property.
	unwantedTransitiveFilesInfo []apexFile

	// List of files that were excluded due to conflicts with other variants of the same module.
	duplicateTransitiveFilesInfo []apexFile

	// List of other module names that should be installed when this APEX gets installed (LOCAL_REQUIRED_MODULES).
	makeModulesToInstall []string

	///////////////////////////////////////////////////////////////////////////////////////////
	// Outputs (final and intermediates)

	// Processed apex manifest in JSONson format (for Q)
	manifestJsonOut android.WritablePath

	// Processed apex manifest in PB format (for R+)
	manifestPbOut android.WritablePath

	// Processed file_contexts files
	fileContexts android.WritablePath

	// The built APEX file. This is the main product.
	// Could be .apex or .capex
	outputFile android.WritablePath

	// The built uncompressed .apex file.
	outputApexFile android.WritablePath

	// The built APEX file in app bundle format. This file is not directly installed to the
	// device. For an APEX, multiple app bundles are created each of which is for a specific ABI
	// like arm, arm64, x86, etc. Then they are processed again (outside of the Android build
	// system) to be merged into a single app bundle file that Play accepts. See
	// vendor/google/build/build_unbundled_mainline_module.sh for more detail.
	bundleModuleFile android.WritablePath

	// Target directory to install this APEX. Usually out/target/product/<device>/<partition>/apex.
	installDir android.InstallPath

	// Path where this APEX was installed.
	installedFile android.InstallPath

	// Extra files that are installed alongside this APEX.
	extraInstalledFiles android.InstallPaths

	// The source and install locations for extraInstalledFiles for use in LOCAL_SOONG_INSTALL_PAIRS.
	extraInstalledPairs installPairs

	// fragment for this apex for apexkeys.txt
	apexKeysPath android.WritablePath

	// Installed locations of symlinks for backward compatibility.
	compatSymlinks android.InstallPaths

	// Text file having the list of individual files that are included in this APEX. Used for
	// debugging purpose.
	installedFilesFile android.Path

	// List of module names that this APEX is including (to be shown via *-deps-info target).
	// Used for debugging purpose.
	android.ApexBundleDepsInfo

	// Optional list of lint report zip files for apexes that contain java or app modules
	lintReports android.Paths

	isCompressed bool

	aconfigFiles []android.Path

	// Required modules, filled out during GenerateAndroidBuildActions and used in AndroidMk
	required []string

	// apkCerts of the apk-in-apex of this module
	apkCerts java.ApkCertsInfo
}

// apexFileClass represents a type of file that can be included in APEX.
type apexFileClass int

const (
	app apexFileClass = iota
	appSet
	etc
	javaSharedLib
	nativeExecutable
	nativeSharedLib
	nativeTest
	shBinary
)

// apexFile represents a file in an APEX bundle. This is created during the first half of
// GenerateAndroidBuildActions by traversing the dependencies of the APEX. Then in the second half
// of the function, this is used to create commands that copies the files into a staging directory,
// where they are packaged into the APEX file.
type apexFile struct {
	// buildFile is put in the installDir inside the APEX.
	builtFile  android.Path
	installDir string
	partition  string
	customStem string
	symlinks   []string // additional symlinks

	checkbuildTarget android.Path

	// Info for Android.mk Module name of `module` in AndroidMk. Note the generated AndroidMk
	// module for apexFile is named something like <AndroidMk module name>.<apex name>[<apex
	// suffix>]
	androidMkModuleName string             // becomes LOCAL_MODULE
	class               apexFileClass      // becomes LOCAL_MODULE_CLASS
	moduleDir           string             // becomes LOCAL_PATH
	dataPaths           []android.DataPath // becomes LOCAL_TEST_DATA

	// systemServerDexpreoptInstalls stores the list of dexpreopt artifacts for a system server jar.
	systemServerDexpreoptInstalls []java.DexpreopterInstall
	// systemServerDexJars stores the list of dexjars for a system server jar.
	systemServerDexJars android.Paths

	jacocoInfo            java.JacocoInfo  // only for javalibs and apps
	lintInfo              *java.LintInfo   // only for javalibs and apps
	certificate           java.Certificate // only for apps
	overriddenPackageName string           // only for apps

	transitiveDep bool
	isJniLib      bool

	multilib string

	// TODO(jiyong): remove this
	module    android.ModuleProxy
	providers *providerInfoForApexFile
}

type providerInfoForApexFile struct {
	commonInfo        *android.CommonModuleInfo
	javaInfo          *java.JavaInfo
	appInfo           *java.AppInfo
	ccInfo            *cc.CcInfo
	rustInfo          *rust.RustInfo
	linkableInfo      *cc.LinkableInfo
	vintfFragmentInfo *android.VintfFragmentInfo
}

// TODO(jiyong): shorten the arglist using an option struct
func newApexFile(ctx android.BaseModuleContext, builtFile android.Path, androidMkModuleName string,
	installDir string, class apexFileClass, module android.ModuleProxy) apexFile {
	ret := apexFile{
		builtFile:           builtFile,
		installDir:          installDir,
		androidMkModuleName: androidMkModuleName,
		class:               class,
		module:              module,
	}
	if !module.IsNil() {
		if buildTargetsInfo, ok := android.OtherModuleProvider(ctx, module, android.ModuleBuildTargetsProvider); ok {
			ret.checkbuildTarget = buildTargetsInfo.CheckbuildTarget
		}
		ret.moduleDir = ctx.OtherModuleDir(module)
		commonInfo := android.OtherModulePointerProviderOrDefault(ctx, module, android.CommonModuleInfoProvider)
		ret.partition = commonInfo.PartitionTag
		ret.multilib = commonInfo.Target.Arch.ArchType.Multilib
		ret.providers = &providerInfoForApexFile{}
		if commonInfo, ok := android.OtherModuleProvider(ctx, module, android.CommonModuleInfoProvider); ok {
			ret.providers.commonInfo = commonInfo
		}
		if javaInfo, ok := android.OtherModuleProvider(ctx, module, java.JavaInfoProvider); ok {
			ret.providers.javaInfo = javaInfo
		}
		if appInfo, ok := android.OtherModuleProvider(ctx, module, java.AppInfoProvider); ok {
			ret.providers.appInfo = appInfo
		}
		if ccInfo, ok := android.OtherModuleProvider(ctx, module, cc.CcInfoProvider); ok {
			ret.providers.ccInfo = ccInfo
		}
		if linkableInfo, ok := android.OtherModuleProvider(ctx, module, cc.LinkableInfoProvider); ok {
			ret.providers.linkableInfo = linkableInfo
		}
		if rustInfo, ok := android.OtherModuleProvider(ctx, module, rust.RustInfoProvider); ok {
			ret.providers.rustInfo = rustInfo
		}
		if vintfInfo, ok := android.OtherModuleProvider(ctx, module, android.VintfFragmentInfoProvider); ok {
			ret.providers.vintfFragmentInfo = &vintfInfo
		}
	}
	return ret
}

func (af *apexFile) ok() bool {
	return af.builtFile != nil && af.builtFile.String() != ""
}

// apexRelativePath returns the relative path of the given path from the install directory of this
// apexFile.
// TODO(jiyong): rename this
func (af *apexFile) apexRelativePath(path string) string {
	return filepath.Join(af.installDir, path)
}

// path returns path of this apex file relative to the APEX root
func (af *apexFile) path() string {
	return af.apexRelativePath(af.stem())
}

// stem returns the base filename of this apex file
func (af *apexFile) stem() string {
	if af.customStem != "" {
		return af.customStem
	}
	return af.builtFile.Base()
}

// symlinkPaths returns paths of the symlinks (if any) relative to the APEX root
func (af *apexFile) symlinkPaths() []string {
	var ret []string
	for _, symlink := range af.symlinks {
		ret = append(ret, af.apexRelativePath(symlink))
	}
	return ret
}

// availableToPlatform tests whether this apexFile is from a module that can be installed to the
// platform.
func (af *apexFile) availableToPlatform() bool {
	if af.providers == nil || af.providers.commonInfo == nil {
		return false
	}
	if af.providers.commonInfo.IsApexModule {
		return android.CheckAvailableForApex(android.AvailableToPlatform, af.providers.commonInfo.ApexAvailableFor)
	}
	return false
}

////////////////////////////////////////////////////////////////////////////////////////////////////
// Mutators
//
// Brief description about mutators for APEX. The following three mutators are the most important
// ones.
//
// 1) DepsMutator: from the properties like native_shared_libs, java_libs, etc., modules are added
// to the (direct) dependencies of this APEX bundle.
//
// 2) apexInfoMutator: this is a post-deps mutator, so runs after DepsMutator. Its goal is to
// collect modules that are direct and transitive dependencies of each APEX bundle. The collected
// modules are marked as being included in the APEX via BuildForApex().
//
// 3) apexMutator: this is a post-deps mutator that runs after apexInfoMutator. For each module that
// are marked by the apexInfoMutator, apex variations are created using CreateApexVariations().

type dependencyTag struct {
	blueprint.BaseDependencyTag
	name string

	// Determines if the dependent will be part of the APEX payload. Can be false for the
	// dependencies to the signing key module, etc.
	payload bool

	// True if the dependent can only be a source module, false if a prebuilt module is a suitable
	// replacement. This is needed because some prebuilt modules do not provide all the information
	// needed by the apex.
	sourceOnly bool

	// If not-nil and an APEX is a member of an SDK then dependencies of that APEX with this tag will
	// also be added as exported members of that SDK.
	memberType android.SdkMemberType

	installable bool
}

func (d *dependencyTag) SdkMemberType(_ android.ModuleContext, _ android.ModuleProxy) android.SdkMemberType {
	return d.memberType
}

func (d *dependencyTag) ExportMember() bool {
	return true
}

func (d *dependencyTag) String() string {
	return fmt.Sprintf("apex.dependencyTag{%q}", d.name)
}

func (d *dependencyTag) ReplaceSourceWithPrebuilt() bool {
	return !d.sourceOnly
}

func (d *dependencyTag) InstallDepNeeded() bool {
	return d.installable
}

var _ android.ReplaceSourceWithPrebuilt = &dependencyTag{}
var _ android.SdkMemberDependencyTag = &dependencyTag{}

var (
	androidAppTag  = &dependencyTag{name: "androidApp", payload: true}
	bpfTag         = &dependencyTag{name: "bpf", payload: true}
	certificateTag = &dependencyTag{name: "certificate"}
	executableTag  = &dependencyTag{name: "executable", payload: true}
	fsTag          = &dependencyTag{name: "filesystem", payload: true}
	bcpfTag        = &dependencyTag{name: "bootclasspathFragment", payload: true, sourceOnly: true, memberType: java.BootclasspathFragmentSdkMemberType}
	// The dexpreopt artifacts of apex system server jars are installed onto system image.
	sscpfTag        = &dependencyTag{name: "systemserverclasspathFragment", payload: true, sourceOnly: true, memberType: java.SystemServerClasspathFragmentSdkMemberType, installable: true}
	compatConfigTag = &dependencyTag{name: "compatConfig", payload: true, sourceOnly: true, memberType: java.CompatConfigSdkMemberType}
	javaLibTag      = &dependencyTag{name: "javaLib", payload: true}
	jniLibTag       = &dependencyTag{name: "jniLib", payload: true}
	keyTag          = &dependencyTag{name: "key"}
	prebuiltTag     = &dependencyTag{name: "prebuilt", payload: true}
	rroTag          = &dependencyTag{name: "rro", payload: true}
	sharedLibTag    = &dependencyTag{name: "sharedLib", payload: true}
	testTag         = &dependencyTag{name: "test", payload: true}
	shBinaryTag     = &dependencyTag{name: "shBinary", payload: true}
)

type fragmentInApexDepTag struct {
	blueprint.BaseDependencyTag
	android.FragmentInApexTag
}

func (fragmentInApexDepTag) ExcludeFromVisibilityEnforcement() {}

// fragmentInApexTag is used by apex modules to depend on their fragments.  Java bootclasspath
// modules can traverse from the apex to the fragment using android.IsFragmentInApexTag.
var fragmentInApexTag = fragmentInApexDepTag{}

// TODO(jiyong): shorten this function signature
func addDependenciesForNativeModules(ctx android.BottomUpMutatorContext, nativeModules ResolvedApexNativeDependencies, target android.Target, imageVariation string) {
	binVariations := target.Variations()
	libVariations := append(target.Variations(), blueprint.Variation{Mutator: "link", Variation: "shared"})
	rustLibVariations := append(
		target.Variations(), []blueprint.Variation{
			{Mutator: "rust_libraries", Variation: "dylib"},
		}...,
	)

	// Append "image" variation
	binVariations = append(binVariations, blueprint.Variation{Mutator: "image", Variation: imageVariation})
	libVariations = append(libVariations, blueprint.Variation{Mutator: "image", Variation: imageVariation})
	rustLibVariations = append(rustLibVariations, blueprint.Variation{Mutator: "image", Variation: imageVariation})

	// Use *FarVariation* to be able to depend on modules having conflicting variations with
	// this module. This is required since arch variant of an APEX bundle is 'common' but it is
	// 'arm' or 'arm64' for native shared libs.
	ctx.AddFarVariationDependencies(binVariations, executableTag,
		android.RemoveListFromList(nativeModules.Binaries, nativeModules.Exclude_binaries)...)
	ctx.AddFarVariationDependencies(binVariations, testTag,
		android.RemoveListFromList(nativeModules.Tests, nativeModules.Exclude_tests)...)
	ctx.AddFarVariationDependencies(libVariations, jniLibTag,
		android.RemoveListFromList(nativeModules.Jni_libs, nativeModules.Exclude_jni_libs)...)
	ctx.AddFarVariationDependencies(libVariations, sharedLibTag,
		android.RemoveListFromList(nativeModules.Native_shared_libs, nativeModules.Exclude_native_shared_libs)...)
	ctx.AddFarVariationDependencies(rustLibVariations, sharedLibTag,
		android.RemoveListFromList(nativeModules.Rust_dyn_libs, nativeModules.Exclude_rust_dyn_libs)...)
	ctx.AddFarVariationDependencies(target.Variations(), fsTag,
		android.RemoveListFromList(nativeModules.Filesystems, nativeModules.Exclude_filesystems)...)
	ctx.AddFarVariationDependencies(target.Variations(), prebuiltTag,
		android.RemoveListFromList(nativeModules.Prebuilts, nativeModules.Exclude_prebuilts)...)
}

func (a *apexBundle) combineProperties(ctx android.BottomUpMutatorContext) {
	proptools.AppendProperties(&a.properties.Multilib, &a.targetProperties.Target.Android.Multilib, nil)
}

// getImageVariationPair returns a pair for the image variation name as its
// prefix and suffix. The prefix indicates whether it's core/vendor/product and the
// suffix indicates the vndk version for vendor/product if vndk is enabled.
// getImageVariation can simply join the result of this function to get the
// image variation name.
func (a *apexBundle) getImageVariationPair() (string, string) {
	if a.vndkApex {
		return cc.VendorVariationPrefix, a.vndkVersion()
	}

	prefix := android.CoreVariation
	if a.SocSpecific() || a.DeviceSpecific() {
		prefix = android.VendorVariation
	} else if a.ProductSpecific() {
		prefix = android.ProductVariation
	}

	return prefix, ""
}

// getImageVariation returns the image variant name for this apexBundle. In most cases, it's simply
// android.CoreVariation, but gets complicated for the vendor APEXes and the VNDK APEX.
func (a *apexBundle) getImageVariation() string {
	prefix, vndkVersion := a.getImageVariationPair()
	return prefix + vndkVersion
}

func (a *apexBundle) DepsMutator(ctx android.BottomUpMutatorContext) {
	// apexBundle is a multi-arch targets module. Arch variant of apexBundle is set to 'common'.
	// arch-specific targets are enabled by the compile_multilib setting of the apex bundle. For
	// each target os/architectures, appropriate dependencies are selected by their
	// target.<os>.multilib.<type> groups and are added as (direct) dependencies.
	targets := ctx.MultiTargets()
	imageVariation := a.getImageVariation()

	a.combineProperties(ctx)

	has32BitTarget := false
	for _, target := range targets {
		if target.Arch.ArchType.Multilib == "lib32" {
			has32BitTarget = true
		}
	}
	for i, target := range targets {
		var deps ResolvedApexNativeDependencies

		// Add native modules targeting both ABIs. When multilib.* is omitted for
		// native_shared_libs/jni_libs/tests, it implies multilib.both
		deps.Merge(ctx, a.properties.Multilib.Both)
		deps.Merge(ctx, ApexNativeDependencies{
			Native_shared_libs: a.properties.Native_shared_libs,
			Rust_dyn_libs:      a.properties.Rust_dyn_libs,
			Tests:              a.properties.Tests,
			Jni_libs:           a.properties.Jni_libs,
		})

		// Add native modules targeting the first ABI When multilib.* is omitted for
		// binaries, it implies multilib.first
		isPrimaryAbi := i == 0
		if isPrimaryAbi {
			deps.Merge(ctx, a.properties.Multilib.First)
			deps.Merge(ctx, ApexNativeDependencies{
				Native_shared_libs: proptools.NewConfigurable[[]string](nil, nil),
				Tests:              nil,
				Jni_libs:           proptools.NewConfigurable[[]string](nil, nil),
				Binaries:           a.properties.Binaries,
			})
		}

		// Add native modules targeting either 32-bit or 64-bit ABI
		switch target.Arch.ArchType.Multilib {
		case "lib32":
			deps.Merge(ctx, a.properties.Multilib.Lib32)
			deps.Merge(ctx, a.properties.Multilib.Prefer32)
		case "lib64":
			deps.Merge(ctx, a.properties.Multilib.Lib64)
			if !has32BitTarget {
				deps.Merge(ctx, a.properties.Multilib.Prefer32)
			}
		}

		// Add native modules targeting a specific arch variant
		switch target.Arch.ArchType {
		case android.Arm:
			deps.Merge(ctx, a.archProperties.Arch.Arm.ApexNativeDependencies)
		case android.Arm64:
			deps.Merge(ctx, a.archProperties.Arch.Arm64.ApexNativeDependencies)
		case android.Riscv64:
			deps.Merge(ctx, a.archProperties.Arch.Riscv64.ApexNativeDependencies)
		case android.X86:
			deps.Merge(ctx, a.archProperties.Arch.X86.ApexNativeDependencies)
		case android.X86_64:
			deps.Merge(ctx, a.archProperties.Arch.X86_64.ApexNativeDependencies)
		default:
			panic(fmt.Errorf("unsupported arch %v\n", ctx.Arch().ArchType))
		}

		addDependenciesForNativeModules(ctx, deps, target, imageVariation)
		if isPrimaryAbi {
			ctx.AddFarVariationDependencies([]blueprint.Variation{
				{Mutator: "os", Variation: target.OsVariation()},
				{Mutator: "arch", Variation: target.ArchVariation()},
			}, shBinaryTag, a.properties.Sh_binaries...)
		}
	}

	// Common-arch dependencies come next
	commonVariation := ctx.Config().AndroidCommonTarget.Variations()
	ctx.AddFarVariationDependencies(commonVariation, rroTag, a.properties.Rros...)
	ctx.AddFarVariationDependencies(commonVariation, bcpfTag, a.properties.Bootclasspath_fragments.GetOrDefault(ctx, nil)...)
	ctx.AddFarVariationDependencies(commonVariation, fragmentInApexTag, a.properties.Bootclasspath_fragments.GetOrDefault(ctx, nil)...)
	ctx.AddFarVariationDependencies(commonVariation, sscpfTag, a.properties.Systemserverclasspath_fragments.GetOrDefault(ctx, nil)...)
	ctx.AddFarVariationDependencies(commonVariation, javaLibTag, a.properties.Java_libs...)
	ctx.AddFarVariationDependencies(commonVariation, fsTag, a.properties.Filesystems...)
	ctx.AddFarVariationDependencies(commonVariation, compatConfigTag, a.properties.Compat_configs...)

	// Add a reverse dependency to all_apex_certs singleton module.
	// all_apex_certs will use this dependency to collect the certificate of this apex.
	ctx.AddReverseDependency(ctx.Module(), allApexCertsDepTag, "all_apex_certs")

	// TODO: When all branches contain this singleton module, make this strict
	// TODO: Add this dependency only for mainline prebuilts and not every prebuilt module
	if ctx.OtherModuleExists("all_apex_contributions") {
		ctx.AddDependency(ctx.Module(), android.AcDepTag, "all_apex_contributions")
	}
}

type allApexCertsDependencyTag struct {
	blueprint.DependencyTag
}

func (_ allApexCertsDependencyTag) ExcludeFromVisibilityEnforcement() {}

var allApexCertsDepTag = allApexCertsDependencyTag{}

// DepsMutator for the overridden properties.
func (a *apexBundle) OverridablePropertiesDepsMutator(ctx android.BottomUpMutatorContext) {
	if a.overridableProperties.Allowed_files != nil {
		android.ExtractSourceDeps(ctx, a.overridableProperties.Allowed_files)
	}

	commonVariation := ctx.Config().AndroidCommonTarget.Variations()
	ctx.AddFarVariationDependencies(commonVariation, androidAppTag, a.overridableProperties.Apps.GetOrDefault(ctx, nil)...)
	ctx.AddFarVariationDependencies(commonVariation, bpfTag, a.overridableProperties.Bpfs.GetOrDefault(ctx, nil)...)
	if prebuilts := a.overridableProperties.Prebuilts.GetOrDefault(ctx, nil); len(prebuilts) > 0 {
		// For prebuilt_etc, use the first variant (64 on 64/32bit device, 32 on 32bit device)
		// regardless of the TARGET_PREFER_* setting. See b/144532908
		arches := ctx.DeviceConfig().Arches()
		if len(arches) != 0 {
			archForPrebuiltEtc := arches[0]
			for _, arch := range arches {
				// Prefer 64-bit arch if there is any
				if arch.ArchType.Multilib == "lib64" {
					archForPrebuiltEtc = arch
					break
				}
			}
			ctx.AddFarVariationDependencies([]blueprint.Variation{
				{Mutator: "os", Variation: ctx.Os().String()},
				{Mutator: "arch", Variation: archForPrebuiltEtc.String()},
			}, prebuiltTag, prebuilts...)
		}
	}

	// Dependencies for signing
	if String(a.overridableProperties.Key) == "" {
		ctx.PropertyErrorf("key", "missing")
		return
	}
	ctx.AddDependency(ctx.Module(), keyTag, String(a.overridableProperties.Key))

	cert := android.SrcIsModule(a.getCertString(ctx))
	if cert != "" {
		ctx.AddDependency(ctx.Module(), certificateTag, cert)
		// empty cert is not an error. Cert and private keys will be directly found under
		// PRODUCT_DEFAULT_DEV_CERTIFICATE
	}
}

var _ ApexTransitionMutator = (*apexBundle)(nil)

func (a *apexBundle) ApexVariationName() string {
	return a.properties.ApexVariationName
}

type generateApexInfoContext interface {
	android.MinSdkVersionFromValueContext
	Module() android.Module
	ModuleName() string
}

// generateApexInfo returns an android.ApexInfo configuration that should be used for dependencies of this apex.
func (a *apexBundle) generateApexInfo(ctx generateApexInfoContext) android.ApexInfo {
	// The VNDK APEX is special. For the APEX, the membership is described in a very different
	// way. There is no dependency from the VNDK APEX to the VNDK libraries. Instead, VNDK
	// libraries are self-identified by their vndk.enabled properties. There is no need to run
	// this mutator for the APEX as nothing will be collected so return an empty ApexInfo.
	if a.vndkApex {
		return android.ApexInfo{}
	}

	minSdkVersion := a.minSdkVersion(ctx)
	// When min_sdk_version is not set, the apex is built against FutureApiLevel.
	if minSdkVersion.IsNone() {
		minSdkVersion = android.FutureApiLevel
	}

	// This is the main part of this mutator. Mark the collected dependencies that they need to
	// be built for this apexBundle.

	apexVariationName := ctx.ModuleName() // could be com.android.foo
	if a.GetOverriddenBy() != "" {
		// use the overridden name com.mycompany.android.foo
		apexVariationName = a.GetOverriddenBy()
	}

	apexInfo := android.ApexInfo{
		ApexVariationName: apexVariationName,
		MinSdkVersion:     minSdkVersion,
		Updatable:         a.Updatable(),
		UsePlatformApis:   a.UsePlatformApis(),
		BaseApexName:      ctx.ModuleName(),
		ApexAvailableName: proptools.String(a.properties.Apex_available_name),
	}
	return apexInfo
}

func (a *apexBundle) ApexTransitionMutatorSplit(ctx android.BaseModuleContext) []android.ApexInfo {
	return []android.ApexInfo{a.generateApexInfo(ctx)}
}

func (a *apexBundle) ApexTransitionMutatorOutgoing(ctx android.OutgoingTransitionContext, sourceInfo android.ApexInfo) android.ApexInfo {
	return sourceInfo
}

func (a *apexBundle) ApexTransitionMutatorIncoming(ctx android.IncomingTransitionContext, outgoingInfo android.ApexInfo) android.ApexInfo {
	return a.generateApexInfo(ctx)
}

func (a *apexBundle) ApexTransitionMutatorMutate(ctx android.BottomUpMutatorContext, info android.ApexInfo) {
	android.SetProvider(ctx, android.ApexBundleInfoProvider, android.ApexBundleInfo{})
	a.properties.ApexVariationName = info.ApexVariationName
}

type ApexTransitionMutator interface {
	ApexTransitionMutatorSplit(ctx android.BaseModuleContext) []android.ApexInfo
	ApexTransitionMutatorOutgoing(ctx android.OutgoingTransitionContext, sourceInfo android.ApexInfo) android.ApexInfo
	ApexTransitionMutatorIncoming(ctx android.IncomingTransitionContext, outgoingInfo android.ApexInfo) android.ApexInfo
	ApexTransitionMutatorMutate(ctx android.BottomUpMutatorContext, info android.ApexInfo)
}

// TODO: b/215736885 Whittle the denylist
// Transitive deps of certain mainline modules baseline NewApi errors
// Skip these mainline modules for now
var (
	skipStrictUpdatabilityLintAllowlist = []string{
		// go/keep-sorted start
		"PackageManagerTestApex",
		"com.android.adservices",
		"com.android.appsearch",
		"com.android.art",
		"com.android.art.debug",
		"com.android.bt",
		"com.android.cellbroadcast",
		"com.android.configinfrastructure",
		"com.android.conscrypt",
		"com.android.extservices",
		"com.android.extservices_tplus",
		"com.android.healthfitness",
		"com.android.ipsec",
		"com.android.media",
		"com.android.mediaprovider",
		"com.android.ondevicepersonalization",
		"com.android.os.statsd",
		"com.android.permission",
		"com.android.profiling",
		"com.android.rkpd",
		"com.android.scheduling",
		"com.android.tethering",
		"com.android.uwb",
		"com.android.wifi",
		"test_com.android.art",
		"test_com.android.cellbroadcast",
		"test_com.android.conscrypt",
		"test_com.android.extservices",
		"test_com.android.ipsec",
		"test_com.android.media",
		"test_com.android.mediaprovider",
		"test_com.android.os.statsd",
		"test_com.android.permission",
		"test_com.android.wifi",
		"test_imgdiag_com.android.art",
		"test_jitzygote_com.android.art",
		// go/keep-sorted end
	}
)

func (a *apexBundle) checkStrictUpdatabilityLinting(mctx android.ModuleContext) bool {
	// The allowlist contains the base apex name, so use that instead of the ApexVariationName
	return a.Updatable() && !android.InList(mctx.ModuleName(), skipStrictUpdatabilityLintAllowlist)
}

// apexUniqueVariationsMutator checks if any dependencies use unique apex variations. If so, use
// unique apex variations for this module. See android/apex.go for more about unique apex variant.
// TODO(jiyong): move this to android/apex.go?
func apexUniqueVariationsMutator(mctx android.BottomUpMutatorContext) {
	if !mctx.Module().Enabled(mctx) {
		return
	}
	if am, ok := mctx.Module().(android.ApexModule); ok {
		android.UpdateUniqueApexVariationsForDeps(mctx, am)
		android.SetProvider(mctx, android.DepInSameApexInfoProvider, android.DepInSameApexInfo{
			Checker: am.GetDepInSameApexChecker(),
		})
	}
}

// markPlatformAvailability marks whether or not a module can be available to platform. A module
// cannot be available to platform if 1) it is explicitly marked as not available (i.e.
// "//apex_available:platform" is absent) or 2) it depends on another module that isn't (or can't
// be) available to platform
// TODO(jiyong): move this to android/apex.go?
func markPlatformAvailability(mctx android.BottomUpMutatorContext) {
	// Recovery is not considered as platform
	if mctx.Module().InstallInRecovery() {
		return
	}

	am, ok := mctx.Module().(android.ApexModule)
	if !ok {
		return
	}

	availableToPlatform := am.AvailableFor(android.AvailableToPlatform)

	// If any of the dep is not available to platform, this module is also considered as being
	// not available to platform even if it has "//apex_available:platform"
	mctx.VisitDirectDepsProxy(func(child android.ModuleProxy) {
		if !android.IsDepInSameApex(mctx, am, child) {
			// if the dependency crosses apex boundary, don't consider it
			return
		}
		info := android.OtherModuleProviderOrDefault(mctx, child, android.PlatformAvailabilityInfoProvider)
		if info.NotAvailableToPlatform {
			availableToPlatform = false
			// TODO(b/154889534) trigger an error when 'am' has
			// "//apex_available:platform"
		}
	})

	// Exception 1: check to see if the module always requires it.
	if am.AlwaysRequiresPlatformApexVariant() {
		availableToPlatform = true
	}

	// Exception 2: bootstrap bionic libraries are also always available to platform
	if cc.InstallToBootstrap(mctx.ModuleName(), mctx.Config()) {
		availableToPlatform = true
	}

	if !availableToPlatform {
		android.SetProvider(mctx, android.PlatformAvailabilityInfoProvider, android.PlatformAvailabilityInfo{
			NotAvailableToPlatform: true,
		})
	}

}

type apexTransitionMutator struct{}

func (a *apexTransitionMutator) Split(ctx android.BaseModuleContext) []android.ApexInfo {
	if ai, ok := ctx.Module().(ApexTransitionMutator); ok {
		return ai.ApexTransitionMutatorSplit(ctx)
	}
	return []android.ApexInfo{{}}
}

func (a *apexTransitionMutator) OutgoingTransition(ctx android.OutgoingTransitionContext, sourceInfo android.ApexInfo) android.ApexInfo {
	if ai, ok := ctx.Module().(ApexTransitionMutator); ok {
		return ai.ApexTransitionMutatorOutgoing(ctx, sourceInfo)
	}
	return android.ApexInfo{}
}

func (a *apexTransitionMutator) IncomingTransition(ctx android.IncomingTransitionContext, outgoingInfo android.ApexInfo) android.ApexInfo {
	if ai, ok := ctx.Module().(ApexTransitionMutator); ok {
		return ai.ApexTransitionMutatorIncoming(ctx, outgoingInfo)
	}
	return android.ApexInfo{}
}

func (a *apexTransitionMutator) Mutate(ctx android.BottomUpMutatorContext, info android.ApexInfo) {
	if ai, ok := ctx.Module().(ApexTransitionMutator); ok {
		ai.ApexTransitionMutatorMutate(ctx, info)
	}
}

func (a *apexTransitionMutator) TransitionInfoFromVariation(variation string) android.ApexInfo {
	panic(fmt.Errorf("adding dependencies on explicit apex variations is not supported"))
}

const (
	// File extensions of an APEX for different packaging methods
	imageApexSuffix  = ".apex"
	imageCapexSuffix = ".capex"

	// variant names each of which is for a packaging method
	imageApexType = "image"

	ext4FsType  = "ext4"
	f2fsFsType  = "f2fs"
	erofsFsType = "erofs"
)

func (a *apexBundle) Exportable() bool {
	return true
}

func (a *apexBundle) TaggedOutputs() map[string]android.Paths {
	ret := make(map[string]android.Paths)
	ret["apex"] = android.Paths{a.outputFile}
	return ret
}

// Implements cc.UseCoverage
func (a *apexBundle) IsNativeCoverageNeeded(ctx cc.IsNativeCoverageNeededContext) bool {
	return ctx.DeviceConfig().NativeCoverageEnabled()
}

var _ cc.UseCoverage = &apexBundle{}

// Implements cc.Coverage
func (a *apexBundle) HideFromMake() {
	a.properties.HideFromMake = true
	// This HideFromMake is shadowing the ModuleBase one, call through to it for now.
	// TODO(ccross): untangle these
	a.ModuleBase.HideFromMake()
}

var _ android.ApexBundleDepsInfoIntf = (*apexBundle)(nil)

// Implements android.ApexBundleDepsInfoIntf
func (a *apexBundle) Updatable() bool {
	return proptools.BoolDefault(a.properties.Updatable, true)
}

func (a *apexBundle) FutureUpdatable() bool {
	return proptools.BoolDefault(a.properties.Future_updatable, false)
}

func (a *apexBundle) UsePlatformApis() bool {
	return proptools.BoolDefault(a.properties.Platform_apis, false)
}

type apexValidationType int

const (
	hostApexVerifier apexValidationType = iota
	apexSepolicyTests
)

func (a *apexBundle) skipValidation(validationType apexValidationType) bool {
	switch validationType {
	case hostApexVerifier:
		return proptools.Bool(a.testProperties.Skip_validations.Host_apex_verifier)
	case apexSepolicyTests:
		return proptools.Bool(a.testProperties.Skip_validations.Apex_sepolicy_tests)
	}
	panic("Unknown validation type")
}

// getCertString returns the name of the cert that should be used to sign this APEX. This is
// basically from the "certificate" property, but could be overridden by the device config.
func (a *apexBundle) getCertString(ctx android.BaseModuleContext) string {
	moduleName := ctx.ModuleName()
	// VNDK APEXes share the same certificate. To avoid adding a new VNDK version to the
	// OVERRIDE_* list, we check with the pseudo module name to see if its certificate is
	// overridden.
	if a.vndkApex {
		moduleName = vndkApexName
	}
	certificate, overridden := ctx.DeviceConfig().OverrideCertificateFor(moduleName)
	if overridden {
		return ":" + certificate
	}
	return String(a.overridableProperties.Certificate)
}

// See the installable property
func (a *apexBundle) installable() bool {
	return proptools.BoolDefault(a.properties.Installable, true)
}

// See the test_only_unsigned_payload property
func (a *apexBundle) testOnlyShouldSkipPayloadSign() bool {
	return proptools.Bool(a.properties.Test_only_unsigned_payload)
}

// See the test_only_force_compression property
func (a *apexBundle) testOnlyShouldForceCompression() bool {
	return proptools.Bool(a.properties.Test_only_force_compression)
}

// See the dynamic_common_lib_apex property
func (a *apexBundle) dynamic_common_lib_apex() bool {
	return proptools.BoolDefault(a.properties.Dynamic_common_lib_apex, false)
}

// These functions are interfacing with cc/sanitizer.go. The entire APEX (along with all of its
// members) can be sanitized, either forcibly, or by the global configuration. For some of the
// sanitizers, extra dependencies can be forcibly added as well.

func (a *apexBundle) EnableSanitizer(sanitizerName string) {
	if !android.InList(sanitizerName, a.properties.SanitizerNames) {
		a.properties.SanitizerNames = append(a.properties.SanitizerNames, sanitizerName)
	}
}

func (a *apexBundle) IsSanitizerEnabled(config android.Config, sanitizerName string) bool {
	if android.InList(sanitizerName, a.properties.SanitizerNames) {
		return true
	}

	// Then follow the global setting
	var globalSanitizerNames []string
	arches := config.SanitizeDeviceArch()
	if len(arches) == 0 || android.InList(a.Arch().ArchType.Name, arches) {
		globalSanitizerNames = config.SanitizeDevice()
	}
	return android.InList(sanitizerName, globalSanitizerNames)
}

func (a *apexBundle) AddSanitizerDependencies(ctx android.BottomUpMutatorContext, sanitizerName string) {
	// TODO(jiyong): move this info (the sanitizer name, the lib name, etc.) to cc/sanitize.go
	// Keep only the mechanism here.
	if sanitizerName == "hwaddress" && strings.HasPrefix(a.Name(), "com.android.runtime") {
		imageVariation := a.getImageVariation()
		for _, target := range ctx.MultiTargets() {
			if target.Arch.ArchType.Multilib == "lib64" {
				addDependenciesForNativeModules(ctx, ResolvedApexNativeDependencies{
					Native_shared_libs: []string{"libclang_rt.hwasan"},
					Tests:              nil,
					Jni_libs:           nil,
				}, target, imageVariation)
				break
			}
		}
	}
}

func setDirInApexForNativeBridge(commonInfo *android.CommonModuleInfo, dir *string) {
	if commonInfo.Target.NativeBridge == android.NativeBridgeEnabled {
		*dir = filepath.Join(*dir, commonInfo.Target.NativeBridgeRelativePath)
	}
}

// apexFileFor<Type> functions below create an apexFile struct for a given Soong module. The
// returned apexFile saves information about the Soong module that will be used for creating the
// build rules.
func apexFileForNativeLibrary(ctx android.BaseModuleContext, module android.ModuleProxy,
	commonInfo *android.CommonModuleInfo, ccMod *cc.LinkableInfo, handleSpecialLibs bool) apexFile {
	// Decide the APEX-local directory by the multilib of the library In the future, we may
	// query this to the module.
	// TODO(jiyong): use the new PackagingSpec
	var dirInApex string
	switch ccMod.Multilib {
	case "lib32":
		dirInApex = "lib"
	case "lib64":
		dirInApex = "lib64"
	}
	setDirInApexForNativeBridge(commonInfo, &dirInApex)
	if handleSpecialLibs && cc.InstallToBootstrap(commonInfo.BaseModuleName, ctx.Config()) {
		// Special case for Bionic libs and other libs installed with them. This is to
		// prevent those libs from being included in the search path
		// /apex/com.android.runtime/${LIB}. This exclusion is required because those libs
		// in the Runtime APEX are available via the legacy paths in /system/lib/. By the
		// init process, the libs in the APEX are bind-mounted to the legacy paths and thus
		// will be loaded into the default linker namespace (aka "platform" namespace). If
		// the libs are directly in /apex/com.android.runtime/${LIB} then the same libs will
		// be loaded again into the runtime linker namespace, which will result in double
		// loading of them, which isn't supported.
		dirInApex = filepath.Join(dirInApex, "bionic")
	}
	// This needs to go after the runtime APEX handling because otherwise we would get
	// weird paths like lib64/rel_install_path/bionic rather than
	// lib64/bionic/rel_install_path.
	dirInApex = filepath.Join(dirInApex, ccMod.RelativeInstallPath)

	fileToCopy := android.OutputFileForModule(ctx, module, "")
	androidMkModuleName := commonInfo.BaseModuleName + ccMod.SubName
	return newApexFile(ctx, fileToCopy, androidMkModuleName, dirInApex, nativeSharedLib, module)
}

func apexFileForExecutable(ctx android.BaseModuleContext, module android.ModuleProxy,
	commonInfo *android.CommonModuleInfo, ccInfo *cc.CcInfo) apexFile {
	linkableInfo := android.OtherModuleProviderOrDefault(ctx, module, cc.LinkableInfoProvider)
	dirInApex := "bin"
	setDirInApexForNativeBridge(commonInfo, &dirInApex)
	dirInApex = filepath.Join(dirInApex, linkableInfo.RelativeInstallPath)
	fileToCopy := android.OutputFileForModule(ctx, module, "")
	androidMkModuleName := commonInfo.BaseModuleName + linkableInfo.SubName
	af := newApexFile(ctx, fileToCopy, androidMkModuleName, dirInApex, nativeExecutable, module)
	af.symlinks = linkableInfo.Symlinks
	af.dataPaths = ccInfo.DataPaths
	return af
}

func apexFileForRustExecutable(ctx android.BaseModuleContext, module android.ModuleProxy,
	commonInfo *android.CommonModuleInfo) apexFile {
	linkableInfo := android.OtherModuleProviderOrDefault(ctx, module, cc.LinkableInfoProvider)
	dirInApex := "bin"
	setDirInApexForNativeBridge(commonInfo, &dirInApex)
	dirInApex = filepath.Join(dirInApex, linkableInfo.RelativeInstallPath)
	fileToCopy := android.OutputFileForModule(ctx, module, "")
	androidMkModuleName := commonInfo.BaseModuleName + linkableInfo.SubName
	af := newApexFile(ctx, fileToCopy, androidMkModuleName, dirInApex, nativeExecutable, module)
	return af
}

func apexFileForShBinary(ctx android.BaseModuleContext, module android.ModuleProxy,
	commonInfo *android.CommonModuleInfo, sh *sh.ShBinaryInfo) apexFile {
	dirInApex := filepath.Join("bin", sh.SubDir)
	setDirInApexForNativeBridge(commonInfo, &dirInApex)
	fileToCopy := sh.OutputFile
	af := newApexFile(ctx, fileToCopy, commonInfo.BaseModuleName, dirInApex, shBinary, module)
	af.symlinks = sh.Symlinks
	return af
}

func apexFileForPrebuiltEtc(ctx android.BaseModuleContext, module android.ModuleProxy,
	prebuilt *prebuilt_etc.PrebuiltEtcInfo, outputFile android.Path) apexFile {
	dirInApex := filepath.Join(prebuilt.BaseDir, prebuilt.SubDir)
	makeModuleName := strings.ReplaceAll(filepath.Join(dirInApex, outputFile.Base()), "/", "_")
	return newApexFile(ctx, outputFile, makeModuleName, dirInApex, etc, module)
}

func apexFileForCompatConfig(ctx android.BaseModuleContext, module android.ModuleProxy,
	config *java.PlatformCompatConfigInfo, depName string) apexFile {
	dirInApex := filepath.Join("etc", config.SubDir)
	fileToCopy := config.CompatConfig
	return newApexFile(ctx, fileToCopy, depName, dirInApex, etc, module)
}

func apexFileForVintfFragment(ctx android.BaseModuleContext, module android.ModuleProxy,
	commonInfo *android.CommonModuleInfo, vf *android.VintfFragmentInfo) apexFile {
	dirInApex := filepath.Join("etc", "vintf")

	return newApexFile(ctx, vf.OutputFile, commonInfo.BaseModuleName, dirInApex, etc, module)
}

// javaModule is an interface to handle all Java modules (java_library, dex_import, etc) in the same
// way.
type javaModule interface {
	android.Module
	BaseModuleName() string
	DexJarBuildPath(ctx android.ModuleErrorfContext) java.OptionalDexJarPath
	Stem() string
}

var _ javaModule = (*java.Library)(nil)
var _ javaModule = (*java.Import)(nil)
var _ javaModule = (*java.SdkLibrary)(nil)
var _ javaModule = (*java.DexImport)(nil)
var _ javaModule = (*java.SdkLibraryImport)(nil)

// apexFileForJavaModule creates an apexFile for a java module's dex implementation jar.
func apexFileForJavaModule(ctx android.ModuleContext, module android.ModuleProxy, javaInfo *java.JavaInfo) apexFile {
	return apexFileForJavaModuleWithFile(ctx, module, javaInfo, javaInfo.DexJarBuildPath.PathOrNil())
}

// apexFileForJavaModuleWithFile creates an apexFile for a java module with the supplied file.
func apexFileForJavaModuleWithFile(ctx android.ModuleContext, module android.ModuleProxy,
	javaInfo *java.JavaInfo, dexImplementationJar android.Path) apexFile {
	dirInApex := "javalib"
	commonInfo := android.OtherModulePointerProviderOrDefault(ctx, module, android.CommonModuleInfoProvider)
	af := newApexFile(ctx, dexImplementationJar, commonInfo.BaseModuleName, dirInApex, javaSharedLib, module)
	af.jacocoInfo = javaInfo.JacocoInfo
	if lintInfo, ok := android.OtherModuleProvider(ctx, module, java.LintProvider); ok {
		af.lintInfo = lintInfo
	}
	af.customStem = javaInfo.Stem + ".jar"
	// Collect any system server dex jars and dexpreopt artifacts for installation alongside the apex.
	// TODO: b/338641779 - Remove special casing of sdkLibrary once bcpf and sscpf depends
	// on the implementation library
	if javaInfo.DexpreopterInfo != nil {
		af.systemServerDexpreoptInstalls = append(af.systemServerDexpreoptInstalls, javaInfo.DexpreopterInfo.ApexSystemServerDexpreoptInstalls...)
		af.systemServerDexJars = append(af.systemServerDexJars, javaInfo.DexpreopterInfo.ApexSystemServerDexJars...)
	}
	return af
}

func apexFileForJavaModuleProfile(ctx android.BaseModuleContext, commonInfo *android.CommonModuleInfo,
	javaInfo *java.JavaInfo) *apexFile {
	if profilePathOnHost := javaInfo.DexpreopterInfo.OutputProfilePathOnHost; profilePathOnHost != nil {
		dirInApex := "javalib"
		af := newApexFile(ctx, profilePathOnHost, commonInfo.BaseModuleName+"-profile", dirInApex, etc, android.ModuleProxy{})
		af.customStem = javaInfo.Stem + ".jar.prof"
		return &af
	}
	return nil
}

func sanitizedBuildIdForPath(ctx android.BaseModuleContext) string {
	buildId := ctx.Config().BuildId()

	// The build ID is used as a suffix for a filename, so ensure that
	// the set of characters being used are sanitized.
	// - any word character: [a-zA-Z0-9_]
	// - dots: .
	// - dashes: -
	validRegex := regexp.MustCompile(`^[\w\.\-\_]+$`)
	if !validRegex.MatchString(buildId) {
		ctx.ModuleErrorf("Unable to use build id %s as filename suffix, valid characters are [a-z A-Z 0-9 _ . -].", buildId)
	}
	return buildId
}

func apexFilesForAndroidApp(ctx android.BaseModuleContext, module android.ModuleProxy,
	commonInfo *android.CommonModuleInfo, aapp *java.AppInfo) []apexFile {
	appDir := "app"
	if aapp.Privileged {
		appDir = "priv-app"
	}

	// TODO(b/224589412, b/226559955): Ensure that the subdirname is suffixed
	// so that PackageManager correctly invalidates the existing installed apk
	// in favour of the new APK-in-APEX.  See bugs for more information.
	dirInApex := filepath.Join(appDir, aapp.InstallApkName+"@"+sanitizedBuildIdForPath(ctx))
	fileToCopy := aapp.OutputFile

	af := newApexFile(ctx, fileToCopy, commonInfo.BaseModuleName, dirInApex, app, module)
	af.jacocoInfo = aapp.JacocoInfo
	if lintInfo, ok := android.OtherModuleProvider(ctx, module, java.LintProvider); ok {
		af.lintInfo = lintInfo
	}
	af.certificate = aapp.Certificate

	if aapp.OverriddenManifestPackageName != nil {
		af.overriddenPackageName = *aapp.OverriddenManifestPackageName
	}

	apexFiles := []apexFile{}

	if allowlist := aapp.PrivAppAllowlist; allowlist.Valid() {
		dirInApex := filepath.Join("etc", "permissions")
		privAppAllowlist := newApexFile(ctx, allowlist.Path(), commonInfo.BaseModuleName+"_privapp", dirInApex, etc, module)
		apexFiles = append(apexFiles, privAppAllowlist)
	}

	apexFiles = append(apexFiles, af)

	return apexFiles
}

func apexFileForRuntimeResourceOverlay(ctx android.BaseModuleContext, module android.ModuleProxy, rro java.RuntimeResourceOverlayInfo) apexFile {
	rroDir := "overlay"
	dirInApex := filepath.Join(rroDir, rro.Theme)
	fileToCopy := rro.OutputFile
	af := newApexFile(ctx, fileToCopy, module.Name(), dirInApex, app, module)
	af.certificate = rro.Certificate

	return af
}

func apexFileForBpfProgram(ctx android.BaseModuleContext, builtFile android.Path, apex_sub_dir string, bpfProgram android.ModuleProxy) apexFile {
	dirInApex := filepath.Join("etc", "bpf", apex_sub_dir)
	return newApexFile(ctx, builtFile, builtFile.Base(), dirInApex, etc, bpfProgram)
}

func apexFileForFilesystem(ctx android.BaseModuleContext, buildFile android.Path, module android.ModuleProxy) apexFile {
	dirInApex := filepath.Join("etc", "fs")
	return newApexFile(ctx, buildFile, buildFile.Base(), dirInApex, etc, module)
}

// WalkPayloadDeps visits dependencies that contributes to the payload of this APEX. For each of the
// visited module, the `do` callback is executed. Returning true in the callback continues the visit
// to the child modules. Returning false makes the visit to continue in the sibling or the parent
// modules. This is used in check* functions below.
func (a *apexBundle) WalkPayloadDeps(ctx android.BaseModuleContext, do android.PayloadDepsCallback) {
	ctx.WalkDepsProxy(func(child, parent android.ModuleProxy) bool {
		if !android.OtherModulePointerProviderOrDefault(ctx, child, android.CommonModuleInfoProvider).CanHaveApexVariants {
			return false
		}
		// Filter-out unwanted depedendencies
		depTag := ctx.OtherModuleDependencyTag(child)
		if _, ok := depTag.(android.ExcludeFromApexContentsTag); ok {
			return false
		}
		if dt, ok := depTag.(*dependencyTag); ok && !dt.payload {
			return false
		}
		if depTag == android.RequiredDepTag {
			return false
		}

		externalDep := !android.IsDepInSameApex(ctx, parent, child)

		// Visit actually
		return do(ctx, parent, child, externalDep)
	})
}

// filesystem type of the apex_payload.img inside the APEX. Currently, ext4 and f2fs are supported.
type fsType int

const (
	ext4 fsType = iota
	f2fs
	erofs
)

func (f fsType) string() string {
	switch f {
	case ext4:
		return ext4FsType
	case f2fs:
		return f2fsFsType
	case erofs:
		return erofsFsType
	default:
		panic(fmt.Errorf("unknown APEX payload type %d", f))
	}
}

func (a *apexBundle) setCompression(ctx android.ModuleContext) {
	if a.isCompressable() {
		if !a.Updatable() {
			ctx.PropertyErrorf("compressible", "do not compress non-updatable APEX")
		}
		if a.PartitionTag(ctx.DeviceConfig()) != "system" {
			ctx.PropertyErrorf("compressible", "do not compress non-system APEX")
		}
	}

	if a.testOnlyShouldForceCompression() {
		a.isCompressed = true
	} else {
		a.isCompressed = ctx.Config().ApexCompressionEnabled() && a.isCompressable()
	}
}

func (a *apexBundle) setSystemLibLink(ctx android.ModuleContext) {
	// Optimization. If we are building bundled APEX, for the files that are gathered due to the
	// transitive dependencies, don't place them inside the APEX, but place a symlink pointing
	// the same library in the system partition, thus effectively sharing the same libraries
	// across the APEX boundary. For unbundled APEX, all the gathered files are actually placed
	// in the APEX.
	a.linkToSystemLib = !ctx.Config().UnbundledBuild() && a.installable()

	// APEXes targeting other than system/system_ext partitions use vendor/product variants.
	// So we can't link them to /system/lib libs which are core variants.
	if a.SocSpecific() || a.DeviceSpecific() || (a.ProductSpecific() && ctx.Config().EnforceProductPartitionInterface()) {
		a.linkToSystemLib = false
	}

	forced := ctx.Config().ForceApexSymlinkOptimization()
	updatable := a.Updatable() || a.FutureUpdatable()

	// We don't need the optimization for updatable APEXes, as it might give false signal
	// to the system health when the APEXes are still bundled (b/149805758).
	if !forced && updatable {
		a.linkToSystemLib = false
	}
}

func (a *apexBundle) setPayloadFsType(ctx android.ModuleContext) {
	defaultFsType := ctx.Config().DefaultApexPayloadType()
	switch proptools.StringDefault(a.properties.Payload_fs_type, defaultFsType) {
	case ext4FsType:
		a.payloadFsType = ext4
	case f2fsFsType:
		a.payloadFsType = f2fs
	case erofsFsType:
		a.payloadFsType = erofs
	default:
		ctx.PropertyErrorf("payload_fs_type", "%q is not a valid filesystem for apex [ext4, f2fs, erofs]", *a.properties.Payload_fs_type)
	}
}

func (a *apexBundle) isCompressable() bool {
	return proptools.BoolDefault(a.overridableProperties.Compressible, false) && !a.testApex
}

func (a *apexBundle) commonBuildActions(ctx android.ModuleContext) bool {
	a.checkApexAvailability(ctx)
	a.checkUpdatable(ctx)
	a.CheckMinSdkVersion(ctx)
	a.checkStaticLinkingToStubLibraries(ctx)
	a.checkStaticExecutables(ctx)
	a.enforceAppUpdatability(ctx)
	if len(a.properties.Tests) > 0 && !a.testApex {
		ctx.PropertyErrorf("tests", "property allowed only in apex_test module type")
		return false
	}
	return true
}

type visitorContext struct {
	// all the files that will be included in this APEX
	filesInfo []apexFile

	// native lib dependencies
	provideNativeLibs []string
	requireNativeLibs []string

	handleSpecialLibs bool

	// if true, raise error on duplicate apexFile
	checkDuplicate bool

	// visitor skips these from this list of module names
	unwantedTransitiveDeps []string

	// unwantedTransitiveFilesInfo contains files that would have been in the apex
	// except that they were listed in unwantedTransitiveDeps.
	unwantedTransitiveFilesInfo []apexFile

	// duplicateTransitiveFilesInfo contains files that would ahve been in the apex
	// except that another variant of the same module was already in the apex.
	duplicateTransitiveFilesInfo []apexFile
}

func (vctx *visitorContext) normalizeFileInfo(mctx android.ModuleContext) {
	encountered := make(map[string]apexFile)
	for _, f := range vctx.filesInfo {
		// Skips unwanted transitive deps. This happens, for example, with Rust binaries with prefer_rlib:true.
		// TODO(b/295593640)
		// Needs additional verification for the resulting APEX to ensure that skipped artifacts don't make problems.
		// For example, DT_NEEDED modules should be found within the APEX unless they are marked in `requiredNativeLibs`.
		if f.transitiveDep && !f.module.IsNil() && android.InList(mctx.OtherModuleName(f.module), vctx.unwantedTransitiveDeps) {
			vctx.unwantedTransitiveFilesInfo = append(vctx.unwantedTransitiveFilesInfo, f)
			continue
		}
		if f.builtFile == nil {
			mctx.ModuleErrorf("Dependency %q had nil builtFile. Make sure the module has an output file. (the installable and compile_dex properties can affect this)", f.androidMkModuleName)
			continue
		}
		dest := filepath.Join(f.installDir, f.builtFile.Base())
		if e, ok := encountered[dest]; !ok {
			encountered[dest] = f
		} else {
			if vctx.checkDuplicate && f.builtFile.String() != e.builtFile.String() {
				mctx.ModuleErrorf("apex file %v is provided by two different files %v and %v",
					dest, e.builtFile, f.builtFile)
				return
			} else {
				vctx.duplicateTransitiveFilesInfo = append(vctx.duplicateTransitiveFilesInfo, f)
			}
			// If a module is directly included and also transitively depended on
			// consider it as directly included.
			e.transitiveDep = e.transitiveDep && f.transitiveDep
			// If a module is added as both a JNI library and a regular shared library, consider it as a
			// JNI library.
			e.isJniLib = e.isJniLib || f.isJniLib
			encountered[dest] = e
		}
	}
	vctx.filesInfo = vctx.filesInfo[:0]
	for _, v := range encountered {
		vctx.filesInfo = append(vctx.filesInfo, v)
	}

	sort.Slice(vctx.filesInfo, func(i, j int) bool {
		// Sort by destination path so as to ensure consistent ordering even if the source of the files
		// changes.
		return vctx.filesInfo[i].path() < vctx.filesInfo[j].path()
	})
}

// enforcePartitionTagOnApexSystemServerJar checks that the partition tags of an apex system server jar  matches
// the partition tags of the top-level apex.
// e.g. if the top-level apex sets system_ext_specific to true, the javalib must set this property to true as well.
// This check ensures that the dexpreopt artifacts of the apex system server jar is installed in the same partition
// as the apex.
func (a *apexBundle) enforcePartitionTagOnApexSystemServerJar(ctx android.ModuleContext) {
	global := dexpreopt.GetGlobalConfig(ctx)
	ctx.VisitDirectDepsProxyWithTag(sscpfTag, func(child android.ModuleProxy) {
		info, ok := android.OtherModuleProvider(ctx, child, java.LibraryNameToPartitionInfoProvider)
		if !ok {
			ctx.ModuleErrorf("Could not find partition info of apex system server jars.")
		}
		apexPartition := ctx.Module().PartitionTag(ctx.DeviceConfig())
		for javalib, javalibPartition := range info.LibraryNameToPartition {
			if !global.AllApexSystemServerJars(ctx).ContainsJar(javalib) {
				continue // not an apex system server jar
			}
			if apexPartition != javalibPartition {
				ctx.ModuleErrorf(`
%s is an apex systemserver jar, but its partition does not match the partition of its containing apex. Expected %s, Got %s`,
					javalib, apexPartition, javalibPartition)
			}
		}
	})
}

func (a *apexBundle) depVisitor(vctx *visitorContext, ctx android.ModuleContext, child, parent android.ModuleProxy) bool {
	depTag := ctx.OtherModuleDependencyTag(child)
	if _, ok := depTag.(android.ExcludeFromApexContentsTag); ok {
		return false
	}
	commonInfo := android.OtherModulePointerProviderOrDefault(ctx, child, android.CommonModuleInfoProvider)
	if !commonInfo.Enabled {
		return false
	}
	depName := ctx.OtherModuleName(child)
	if android.EqualModules(parent, ctx.Module()) {
		switch depTag {
		case sharedLibTag, jniLibTag:
			isJniLib := depTag == jniLibTag
			propertyName := "native_shared_libs"
			if isJniLib {
				propertyName = "jni_libs"
			}

			if ch, ok := android.OtherModuleProvider(ctx, child, cc.LinkableInfoProvider); ok {
				if ch.IsStubs {
					ctx.PropertyErrorf(propertyName, "%q is a stub. Remove it from the list.", depName)
				}
				fi := apexFileForNativeLibrary(ctx, child, commonInfo, ch, vctx.handleSpecialLibs)
				fi.isJniLib = isJniLib
				vctx.filesInfo = append(vctx.filesInfo, fi)
				// Collect the list of stub-providing libs except:
				// - VNDK libs are only for vendors
				// - bootstrap bionic libs are treated as provided by system
				if ch.HasStubsVariants && !a.vndkApex && !cc.InstallToBootstrap(commonInfo.BaseModuleName, ctx.Config()) {
					vctx.provideNativeLibs = append(vctx.provideNativeLibs, fi.stem())
				}
				return true // track transitive dependencies
			} else {
				ctx.PropertyErrorf(propertyName,
					"%q is not a VersionLinkableInterface (e.g. cc_library or rust_ffi module)", depName)
			}

		case executableTag:
			if ccInfo, ok := android.OtherModuleProvider(ctx, child, cc.CcInfoProvider); ok {
				vctx.filesInfo = append(vctx.filesInfo, apexFileForExecutable(ctx, child, commonInfo, ccInfo))
				return true // track transitive dependencies
			}
			if _, ok := android.OtherModuleProvider(ctx, child, rust.RustInfoProvider); ok {
				vctx.filesInfo = append(vctx.filesInfo, apexFileForRustExecutable(ctx, child, commonInfo))
				return true // track transitive dependencies
			} else {
				ctx.PropertyErrorf("binaries",
					"%q is neither cc_binary, rust_binary, (embedded) py_binary, (host) blueprint_go_binary, nor (host) bootstrap_go_binary", depName)
			}
		case shBinaryTag:
			if csh, ok := android.OtherModuleProvider(ctx, child, sh.ShBinaryInfoProvider); ok {
				vctx.filesInfo = append(vctx.filesInfo, apexFileForShBinary(ctx, child, commonInfo, &csh))
			} else {
				ctx.PropertyErrorf("sh_binaries", "%q is not a sh_binary module", depName)
			}
		case bcpfTag:
			_, ok := android.OtherModuleProvider(ctx, child, java.BootclasspathFragmentInfoProvider)
			if !ok {
				ctx.PropertyErrorf("bootclasspath_fragments", "%q is not a bootclasspath_fragment module", depName)
				return false
			}
			vctx.filesInfo = append(vctx.filesInfo, apexBootclasspathFragmentFiles(ctx, child)...)
			return true
		case sscpfTag:
			if _, ok := android.OtherModuleProvider(ctx, child, java.LibraryNameToPartitionInfoProvider); !ok {
				ctx.PropertyErrorf("systemserverclasspath_fragments",
					"%q is not a systemserverclasspath_fragment module", depName)
				return false
			}
			if af := apexClasspathFragmentProtoFile(ctx, child); af != nil {
				vctx.filesInfo = append(vctx.filesInfo, *af)
			}
			return true
		case javaLibTag:
			if ctx.OtherModuleHasProvider(child, java.JavaLibraryInfoProvider) ||
				ctx.OtherModuleHasProvider(child, java.JavaDexImportInfoProvider) ||
				ctx.OtherModuleHasProvider(child, java.SdkLibraryInfoProvider) {
				javaInfo := android.OtherModuleProviderOrDefault(ctx, child, java.JavaInfoProvider)
				af := apexFileForJavaModule(ctx, child, javaInfo)
				if !af.ok() {
					ctx.PropertyErrorf("java_libs", "%q is not configured to be compiled into dex", depName)
					return false
				}
				vctx.filesInfo = append(vctx.filesInfo, af)
				return true // track transitive dependencies
			} else {
				ctx.PropertyErrorf("java_libs", "%q of type %q is not supported", depName, ctx.OtherModuleType(child))
			}
		case androidAppTag:
			if appInfo, ok := android.OtherModuleProvider(ctx, child, java.AppInfoProvider); ok {
				if appInfo.AppSet {
					appDir := "app"
					if appInfo.Privileged {
						appDir = "priv-app"
					}
					// TODO(b/224589412, b/226559955): Ensure that the dirname is
					// suffixed so that PackageManager correctly invalidates the
					// existing installed apk in favour of the new APK-in-APEX.
					// See bugs for more information.
					appDirName := filepath.Join(appDir, commonInfo.BaseModuleName+"@"+sanitizedBuildIdForPath(ctx))
					af := newApexFile(ctx, appInfo.OutputFile, commonInfo.BaseModuleName, appDirName, appSet, child)
					af.certificate = java.PresignedCertificate
					vctx.filesInfo = append(vctx.filesInfo, af)
				} else {
					vctx.filesInfo = append(vctx.filesInfo, apexFilesForAndroidApp(ctx, child, commonInfo, appInfo)...)
				}

				// androidMkForFiles is only called if a.installable(), so historically make would
				// only see certs from apks inside installable apexes
				if a.installable() {
					if apkCertInfo, ok := android.OtherModuleProvider(ctx, child, java.ApkCertInfoProvider); ok {
						a.apkCerts = append(a.apkCerts, apkCertInfo)
					}
				}

				if !appInfo.AppSet && !appInfo.Prebuilt && !appInfo.TestHelperApp {
					return true // track transitive dependencies
				}
			} else {
				ctx.PropertyErrorf("apps", "%q is not an android_app module", depName)
			}
		case rroTag:
			if rro, ok := android.OtherModuleProvider(ctx, child, java.RuntimeResourceOverlayInfoProvider); ok {
				vctx.filesInfo = append(vctx.filesInfo, apexFileForRuntimeResourceOverlay(ctx, child, rro))
			} else {
				ctx.PropertyErrorf("rros", "%q is not an runtime_resource_overlay module", depName)
			}
		case bpfTag:
			if bpfProgram, ok := android.OtherModuleProvider(ctx, child, bpf.BpfInfoProvider); ok {
				filesToCopy := android.OutputFilesForModule(ctx, child, "")
				apex_sub_dir := bpfProgram.SubDir
				for _, bpfFile := range filesToCopy {
					vctx.filesInfo = append(vctx.filesInfo, apexFileForBpfProgram(ctx, bpfFile, apex_sub_dir, child))
				}
			} else {
				ctx.PropertyErrorf("bpfs", "%q is not a bpf module", depName)
			}
		case fsTag:
			if fs, ok := android.OtherModuleProvider(ctx, child, filesystem.FilesystemProvider); ok {
				vctx.filesInfo = append(vctx.filesInfo, apexFileForFilesystem(ctx, fs.Output, child))
			} else {
				ctx.PropertyErrorf("filesystems", "%q is not a filesystem module", depName)
			}
		case prebuiltTag:
			if prebuilt, ok := android.OtherModuleProvider(ctx, child, prebuilt_etc.PrebuiltEtcInfoProvider); ok {
				filesToCopy := android.OutputFilesForModule(ctx, child, "")
				for _, etcFile := range filesToCopy {
					vctx.filesInfo = append(vctx.filesInfo, apexFileForPrebuiltEtc(ctx, child, &prebuilt, etcFile))
				}
			} else {
				ctx.PropertyErrorf("prebuilts", "%q is not a prebuilt_etc module", depName)
			}
		case compatConfigTag:
			if compatConfig, ok := android.OtherModuleProvider(ctx, child, java.PlatformCompatConfigInfoProvider); ok {
				vctx.filesInfo = append(vctx.filesInfo, apexFileForCompatConfig(ctx, child, &compatConfig, depName))
			} else {
				ctx.PropertyErrorf("compat_configs", "%q is not a platform_compat_config module", depName)
			}
		case testTag:
			if ccInfo, ok := android.OtherModuleProvider(ctx, child, cc.CcInfoProvider); ok {
				af := apexFileForExecutable(ctx, child, commonInfo, ccInfo)
				// We make this a nativeExecutable instead of a nativeTest because we don't want
				// the androidmk modules generated in AndroidMkForFiles to be treated as real
				// tests that are then packaged into suites. Our AndroidMkForFiles does not
				// implement enough functionality to support real tests.
				af.class = nativeExecutable
				vctx.filesInfo = append(vctx.filesInfo, af)
				return true // track transitive dependencies
			} else {
				ctx.PropertyErrorf("tests", "%q is not a cc module", depName)
			}
		case keyTag:
			if key, ok := android.OtherModuleProvider(ctx, child, ApexKeyInfoProvider); ok {
				a.privateKeyFile = key.PrivateKeyFile
				a.publicKeyFile = key.PublicKeyFile
			} else {
				ctx.PropertyErrorf("key", "%q is not an apex_key module", depName)
			}
		case certificateTag:
			if dep, ok := android.OtherModuleProvider(ctx, child, java.AndroidAppCertificateInfoProvider); ok {
				a.containerCertificateFile = dep.Certificate.Pem
				a.containerPrivateKeyFile = dep.Certificate.Key
			} else {
				ctx.ModuleErrorf("certificate dependency %q must be an android_app_certificate module", depName)
			}
		}
		return false
	}

	if a.vndkApex {
		return false
	}

	// indirect dependencies
	if !commonInfo.IsApexModule {
		return false
	}
	// We cannot use a switch statement on `depTag` here as the checked
	// tags used below are private (e.g. `cc.sharedDepTag`).
	if cc.IsSharedDepTag(depTag) || cc.IsRuntimeDepTag(depTag) {
		if ch, ok := android.OtherModuleProvider(ctx, child, cc.LinkableInfoProvider); ok {
			af := apexFileForNativeLibrary(ctx, child, commonInfo, ch, vctx.handleSpecialLibs)
			af.transitiveDep = true

			if ch.IsStubs || ch.HasStubsVariants {
				// If the dependency is a stubs lib, don't include it in this APEX,
				// but make sure that the lib is installed on the device.
				// In case no APEX is having the lib, the lib is installed to the system
				// partition.
				//
				// Always include if we are a host-apex however since those won't have any
				// system libraries.
				//
				// Skip the dependency in unbundled builds where the device image is not
				// being built.
				if ch.IsStubsImplementationRequired &&
					!commonInfo.NotInPlatform && !ctx.Config().UnbundledBuild() {
					// we need a module name for Make
					name := ch.ImplementationModuleNameForMake + ch.SubName
					if !android.InList(name, a.makeModulesToInstall) {
						a.makeModulesToInstall = append(a.makeModulesToInstall, name)
					}
				}
				vctx.requireNativeLibs = append(vctx.requireNativeLibs, af.stem())
				// Don't track further
				return false
			}

			// If the dep is not considered to be in the same
			// apex, don't add it to filesInfo so that it is not
			// included in this APEX.
			// TODO(jiyong): move this to at the top of the
			// else-if clause for the indirect dependencies.
			// Currently, that's impossible because we would
			// like to record requiredNativeLibs even when
			// DepIsInSameAPex is false. We also shouldn't do
			// this for host.
			if !android.IsDepInSameApex(ctx, parent, child) {
				return false
			}

			vctx.filesInfo = append(vctx.filesInfo, af)
			return true // track transitive dependencies
		}
	} else if cc.IsHeaderDepTag(depTag) {
		// nothing
	} else if java.IsJniDepTag(depTag) {
		// Because APK-in-APEX embeds jni_libs transitively, we don't need to track transitive deps
	} else if java.IsXmlPermissionsFileDepTag(depTag) {
		if prebuilt, ok := android.OtherModuleProvider(ctx, child, prebuilt_etc.PrebuiltEtcInfoProvider); ok {
			filesToCopy := android.OutputFilesForModule(ctx, child, "")
			for _, etcFile := range filesToCopy {
				vctx.filesInfo = append(vctx.filesInfo, apexFileForPrebuiltEtc(ctx, child, &prebuilt, etcFile))
			}
		}
	} else if rust.IsDylibDepTag(depTag) {
		if _, ok := android.OtherModuleProvider(ctx, child, rust.RustInfoProvider); ok &&
			commonInfo.IsInstallableToApex {
			if !android.IsDepInSameApex(ctx, parent, child) {
				return false
			}

			linkableInfo := android.OtherModuleProviderOrDefault(ctx, child, cc.LinkableInfoProvider)
			af := apexFileForNativeLibrary(ctx, child, commonInfo, linkableInfo, vctx.handleSpecialLibs)
			af.transitiveDep = true
			vctx.filesInfo = append(vctx.filesInfo, af)
			return true // track transitive dependencies
		}
	} else if rust.IsRlibDepTag(depTag) {
		// Rlib is statically linked, but it might have shared lib
		// dependencies. Track them.
		return true
	} else if java.IsBootclasspathFragmentContentDepTag(depTag) {
		// Add the contents of the bootclasspath fragment to the apex.
		if ctx.OtherModuleHasProvider(child, java.JavaLibraryInfoProvider) ||
			ctx.OtherModuleHasProvider(child, java.SdkLibraryInfoProvider) {
			af := apexFileForBootclasspathFragmentContentModule(ctx, parent, child)
			if !af.ok() {
				ctx.PropertyErrorf("bootclasspath_fragments",
					"bootclasspath_fragment content %q is not configured to be compiled into dex", depName)
				return false
			}
			vctx.filesInfo = append(vctx.filesInfo, af)
			return true // track transitive dependencies
		} else {
			ctx.PropertyErrorf("bootclasspath_fragments",
				"bootclasspath_fragment content %q of type %q is not supported", depName, ctx.OtherModuleType(child))
		}
	} else if java.IsSystemServerClasspathFragmentContentDepTag(depTag) {
		// Add the contents of the systemserverclasspath fragment to the apex.
		if ctx.OtherModuleHasProvider(child, java.JavaLibraryInfoProvider) ||
			ctx.OtherModuleHasProvider(child, java.SdkLibraryInfoProvider) {
			javaInfo := android.OtherModuleProviderOrDefault(ctx, child, java.JavaInfoProvider)
			af := apexFileForJavaModule(ctx, child, javaInfo)
			vctx.filesInfo = append(vctx.filesInfo, af)
			if profileAf := apexFileForJavaModuleProfile(ctx, commonInfo, javaInfo); profileAf != nil {
				vctx.filesInfo = append(vctx.filesInfo, *profileAf)
			}
			return true // track transitive dependencies
		} else {
			ctx.PropertyErrorf("systemserverclasspath_fragments",
				"systemserverclasspath_fragment content %q of type %q is not supported", depName, ctx.OtherModuleType(child))
		}
	} else if depTag == android.DarwinUniversalVariantTag {
		// nothing
	} else if depTag == android.RequiredDepTag {
		// nothing
	} else if commonInfo.IsInstallableToApex {
		ctx.ModuleErrorf("unexpected tag %s for indirect dependency %q", android.PrettyPrintTag(depTag), depName)
	} else if android.IsVintfDepTag(depTag) {
		if vf, ok := android.OtherModuleProvider(ctx, child, android.VintfFragmentInfoProvider); ok {
			apexFile := apexFileForVintfFragment(ctx, child, commonInfo, &vf)
			vctx.filesInfo = append(vctx.filesInfo, apexFile)
		}
	}

	return false
}

func (a *apexBundle) shouldCheckDuplicate(ctx android.ModuleContext) bool {
	return ctx.DeviceConfig().DeviceArch() != ""
}

// Creates build rules for an APEX. It consists of the following major steps:
//
// 1) do some validity checks such as apex_available, min_sdk_version, etc.
// 2) traverse the dependency tree to collect apexFile structs from them.
// 3) some fields in apexBundle struct are configured
// 4) generate the build rules to create the APEX. This is mostly done in builder.go.
func (a *apexBundle) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	////////////////////////////////////////////////////////////////////////////////////////////
	// 1) do some validity checks such as apex_available, min_sdk_version, etc.
	if !a.commonBuildActions(ctx) {
		return
	}
	////////////////////////////////////////////////////////////////////////////////////////////
	// 2) traverse the dependency tree to collect apexFile structs from them.

	// TODO(jiyong): do this using WalkPayloadDeps
	// TODO(jiyong): make this clean!!!
	vctx := visitorContext{
		handleSpecialLibs:      !android.Bool(a.properties.Ignore_system_library_special_case),
		checkDuplicate:         a.shouldCheckDuplicate(ctx),
		unwantedTransitiveDeps: a.properties.Unwanted_transitive_deps,
	}
	ctx.WalkDepsProxy(func(child, parent android.ModuleProxy) bool { return a.depVisitor(&vctx, ctx, child, parent) })
	vctx.normalizeFileInfo(ctx)
	if a.privateKeyFile == nil {
		if ctx.Config().AllowMissingDependencies() {
			// TODO(b/266099037): a better approach for slim manifests.
			ctx.AddMissingDependencies([]string{String(a.overridableProperties.Key)})
			// Create placeholder paths for later stages that expect to see those paths,
			// though they won't be used.
			var unusedPath = android.PathForModuleOut(ctx, "nonexistentprivatekey")
			android.ErrorRule(ctx, unusedPath, "Private key not available")
			a.privateKeyFile = unusedPath
		} else {
			ctx.PropertyErrorf("key", "private_key for %q could not be found", String(a.overridableProperties.Key))
			return
		}
	}

	if a.publicKeyFile == nil {
		if ctx.Config().AllowMissingDependencies() {
			// TODO(b/266099037): a better approach for slim manifests.
			ctx.AddMissingDependencies([]string{String(a.overridableProperties.Key)})
			// Create placeholder paths for later stages that expect to see those paths,
			// though they won't be used.
			var unusedPath = android.PathForModuleOut(ctx, "nonexistentpublickey")
			android.ErrorRule(ctx, unusedPath, "Public key not available")
			a.publicKeyFile = unusedPath
		} else {
			ctx.PropertyErrorf("key", "public_key for %q could not be found", String(a.overridableProperties.Key))
			return
		}
	}

	////////////////////////////////////////////////////////////////////////////////////////////
	// 3) some fields in apexBundle struct are configured
	a.installDir = android.PathForModuleInstall(ctx, "apex")
	a.filesInfo = vctx.filesInfo
	a.unwantedTransitiveFilesInfo = vctx.unwantedTransitiveFilesInfo
	a.duplicateTransitiveFilesInfo = vctx.duplicateTransitiveFilesInfo

	a.setPayloadFsType(ctx)
	a.setSystemLibLink(ctx)
	a.compatSymlinks = makeCompatSymlinks(a.BaseModuleName(), ctx)

	////////////////////////////////////////////////////////////////////////////////////////////
	// 3.a) some artifacts are generated from the collected files
	aconfigFiles := a.buildAconfigFiles(ctx)
	a.filesInfo = append(a.filesInfo, aconfigFiles...)

	////////////////////////////////////////////////////////////////////////////////////////////
	// 4) generate the build rules to create the APEX. This is done in builder.go.
	a.buildManifest(ctx, vctx.provideNativeLibs, vctx.requireNativeLibs)
	a.installApexSystemServerFiles(ctx)
	a.buildApex(ctx)
	a.buildApexDependencyInfo(ctx)
	a.buildLintReports(ctx)
	a.reexportJacocoInfo(ctx)

	// Set a provider for dexpreopt of bootjars
	a.provideApexExportsInfo(ctx)

	a.providePrebuiltInfo(ctx)

	a.required = a.RequiredModuleNames(ctx)
	a.required = append(a.required, a.VintfFragmentModuleNames(ctx)...)

	a.setOutputFiles(ctx)
	a.enforcePartitionTagOnApexSystemServerJar(ctx)

	a.verifyNativeImplementationLibs(ctx)
	a.enforceNoVintfInUpdatable(ctx)

	android.SetProvider(ctx, android.ApexBundleDepsDataProvider, android.ApexBundleDepsData{
		FlatListPath: a.FlatListPath(),
		Updatable:    a.Updatable(),
	})

	android.SetProvider(ctx, filesystem.ApexKeyPathInfoProvider, filesystem.ApexKeyPathInfo{a.apexKeysPath})

	android.SetProvider(ctx, java.ApkCertsInfoProvider, a.apkCerts)
	a.setSymbolInfosProvider(ctx)

	pem, key := a.getCertificateAndPrivateKey(ctx)
	android.SetProvider(ctx, android.ApexBundleTypeInfoProvider, android.ApexBundleTypeInfo{
		Pem: pem,
		Key: key,
	})

	moduleInfoJSON := ctx.ModuleInfoJSON()
	moduleInfoJSON.Class = []string{"ETC"}
	moduleInfoJSON.SystemSharedLibs = []string{"none"}
	moduleInfoJSON.Disabled = false

	// Build compliance metadata
	if a.installable() && slices.Contains(ctx.Config().UnbundledBuildApps(), a.Name()) {
		builtFiles := []string{}
		for _, file := range aconfigFiles {
			builtFiles = append(builtFiles, file.builtFile.String())
		}

		filesContained := []string{}
		buildOutputPaths := []string{}
		filesContained = append(filesContained, a.installedFile.String())
		buildOutputPaths = append(buildOutputPaths, a.outputFile.String())
		for _, f := range a.filesInfo {
			filesContained = append(filesContained, f.path())
			buildOutputPaths = append(buildOutputPaths, f.builtFile.String())
			imageDir := android.PathForModuleOut(ctx, "image"+imageApexSuffix).String()
			for _, symlinkPath := range f.symlinkPaths() {
				filesContained = append(filesContained, symlinkPath)
				buildOutputPaths = append(buildOutputPaths, filepath.Join(imageDir, symlinkPath))
				builtFiles = append(builtFiles, filepath.Join(imageDir, symlinkPath))
			}
		}
		complianceMetadataInfo := ctx.ComplianceMetadataInfo()
		complianceMetadataInfo.SetFilesContained(filesContained)
		complianceMetadataInfo.SetBuildOutputPathsOfFilesContained(buildOutputPaths)
		complianceMetadataInfo.AddBuiltFiles(builtFiles...)
	}
}

// Set prebuiltInfoProvider. This will be used by `apex_prebuiltinfo_singleton` to print out a metadata file
// with information about whether source or prebuilt of an apex was used during the build.
func (a *apexBundle) providePrebuiltInfo(ctx android.ModuleContext) {
	info := android.PrebuiltJsonInfo{
		Name:        a.Name(),
		Is_prebuilt: false,
	}
	android.SetProvider(ctx, android.PrebuiltJsonInfoProvider, info)
}

// Set a provider containing information about the jars and .prof provided by the apex
// Apexes built from source retrieve this information by visiting `bootclasspath_fragments`
// Used by dex_bootjars to generate the boot image
func (a *apexBundle) provideApexExportsInfo(ctx android.ModuleContext) {
	ctx.VisitDirectDepsProxyWithTag(bcpfTag, func(child android.ModuleProxy) {
		if info, ok := android.OtherModuleProvider(ctx, child, java.BootclasspathFragmentApexContentInfoProvider); ok {
			exports := android.ApexExportsInfo{
				ApexName:                      a.ApexVariationName(),
				ProfilePathOnHost:             info.ProfilePathOnHost(),
				LibraryNameToDexJarPathOnHost: info.DexBootJarPathMap(),
			}
			android.SetProvider(ctx, android.ApexExportsInfoProvider, exports)
		}
	})
}

// Set output files to outputFiles property, which is later used to set the
// OutputFilesProvider
func (a *apexBundle) setOutputFiles(ctx android.ModuleContext) {
	// default dist path
	ctx.SetOutputFiles(android.Paths{a.outputFile}, "")
	ctx.SetOutputFiles(android.Paths{a.outputFile}, android.DefaultDistTag)
	// uncompressed one
	if a.outputApexFile != nil {
		ctx.SetOutputFiles(android.Paths{a.outputApexFile}, imageApexSuffix)
	}
}

// enforceAppUpdatability propagates updatable=true to apps of updatable apexes
func (a *apexBundle) enforceAppUpdatability(mctx android.ModuleContext) {
	if !a.Enabled(mctx) {
		return
	}
	if a.Updatable() {
		// checking direct deps is sufficient since apex->apk is a direct edge, even when inherited via apex_defaults
		mctx.VisitDirectDepsProxy(func(module android.ModuleProxy) {
			if appInfo, ok := android.OtherModuleProvider(mctx, module, java.AppInfoProvider); ok {
				// ignore android_test_app and android_app_import
				if !appInfo.TestHelperApp && !appInfo.Prebuilt && !appInfo.Updatable {
					mctx.ModuleErrorf("app dependency %s must have updatable: true", mctx.OtherModuleName(module))
				}
			}
		})
	}
}

// apexBootclasspathFragmentFiles returns the list of apexFile structures defining the files that
// the bootclasspath_fragment contributes to the apex.
func apexBootclasspathFragmentFiles(ctx android.ModuleContext, module android.ModuleProxy) []apexFile {
	bootclasspathFragmentInfo, _ := android.OtherModuleProvider(ctx, module, java.BootclasspathFragmentApexContentInfoProvider)
	var filesToAdd []apexFile

	// Add classpaths.proto config.
	if af := apexClasspathFragmentProtoFile(ctx, module); af != nil {
		filesToAdd = append(filesToAdd, *af)
	}

	pathInApex := bootclasspathFragmentInfo.ProfileInstallPathInApex()
	if pathInApex != "" {
		pathOnHost := bootclasspathFragmentInfo.ProfilePathOnHost()
		tempPath := android.PathForModuleOut(ctx, "boot_image_profile", pathInApex)

		if pathOnHost != nil {
			// We need to copy the profile to a temporary path with the right filename because the apexer
			// will take the filename as is.
			ctx.Build(pctx, android.BuildParams{
				Rule:   android.Cp,
				Input:  pathOnHost,
				Output: tempPath,
			})
			ctx.ComplianceMetadataInfo().AddBuiltFiles(tempPath.String())
		} else {
			// At this point, the boot image profile cannot be generated. It is probably because the boot
			// image profile source file does not exist on the branch, or it is not available for the
			// current build target.
			// However, we cannot enforce the boot image profile to be generated because some build
			// targets (such as module SDK) do not need it. It is only needed when the APEX is being
			// built. Therefore, we create an error rule so that an error will occur at the ninja phase
			// only if the APEX is being built.
			android.ErrorRule(ctx, tempPath, "Boot image profile cannot be generated")
		}

		androidMkModuleName := filepath.Base(pathInApex)
		af := newApexFile(ctx, tempPath, androidMkModuleName, filepath.Dir(pathInApex), etc, android.ModuleProxy{})
		filesToAdd = append(filesToAdd, af)
	}

	return filesToAdd
}

// apexClasspathFragmentProtoFile returns *apexFile structure defining the classpath.proto config that
// the module contributes to the apex; or nil if the proto config was not generated.
func apexClasspathFragmentProtoFile(ctx android.ModuleContext, module android.ModuleProxy) *apexFile {
	info, _ := android.OtherModuleProvider(ctx, module, java.ClasspathFragmentProtoContentInfoProvider)
	if !info.ClasspathFragmentProtoGenerated {
		return nil
	}
	classpathProtoOutput := info.ClasspathFragmentProtoOutput
	af := newApexFile(ctx, classpathProtoOutput, classpathProtoOutput.Base(), info.ClasspathFragmentProtoInstallDir.Rel(), etc, android.ModuleProxy{})
	return &af
}

// apexFileForBootclasspathFragmentContentModule creates an apexFile for a bootclasspath_fragment
// content module, i.e. a library that is part of the bootclasspath.
func apexFileForBootclasspathFragmentContentModule(ctx android.ModuleContext, fragmentModule, javaModule android.ModuleProxy) apexFile {
	bootclasspathFragmentInfo, _ := android.OtherModuleProvider(ctx, fragmentModule, java.BootclasspathFragmentApexContentInfoProvider)

	// Get the dexBootJar from the bootclasspath_fragment as that is responsible for performing the
	// hidden API encpding.
	dexBootJar, err := bootclasspathFragmentInfo.DexBootJarPathForContentModule(javaModule)
	if err != nil {
		ctx.ModuleErrorf("%s", err)
	}

	// Create an apexFile as for a normal java module but with the dex boot jar provided by the
	// bootclasspath_fragment.
	javaInfo := android.OtherModuleProviderOrDefault(ctx, javaModule, java.JavaInfoProvider)
	af := apexFileForJavaModuleWithFile(ctx, javaModule, javaInfo, dexBootJar)
	return af
}

///////////////////////////////////////////////////////////////////////////////////////////////////
// Factory functions
//

func newApexBundle() *apexBundle {
	module := &apexBundle{}

	module.AddProperties(&module.properties)
	module.AddProperties(&module.targetProperties)
	module.AddProperties(&module.archProperties)
	module.AddProperties(&module.overridableProperties)

	android.InitAndroidMultiTargetsArchModule(module, android.DeviceSupported, android.MultilibCommon)
	android.InitDefaultableModule(module)
	android.InitOverridableModule(module, &module.overridableProperties.Overrides)
	return module
}

type apexTestProperties struct {
	// Boolean flags for validation checks. Test APEXes can turn on/off individual checks.
	Skip_validations struct {
		// Skips `Apex_sepolicy_tests` check if true
		Apex_sepolicy_tests *bool
		// Skips `Host_apex_verifier` check if true
		Host_apex_verifier *bool
	}
}

// apex_test is an APEX for testing. The difference from the ordinary apex module type is that
// certain compatibility checks such as apex_available are not done for apex_test.
func TestApexBundleFactory() android.Module {
	bundle := newApexBundle()
	bundle.testApex = true
	bundle.AddProperties(&bundle.testProperties)
	return bundle
}

// apex packages other modules into an APEX file which is a packaging format for system-level
// components like binaries, shared libraries, etc.
func BundleFactory() android.Module {
	return newApexBundle()
}

type Defaults struct {
	android.ModuleBase
	android.DefaultsModuleBase
}

// apex_defaults provides defaultable properties to other apex modules.
func DefaultsFactory() android.Module {
	module := &Defaults{}

	module.AddProperties(
		&apexBundleProperties{},
		&apexTargetBundleProperties{},
		&apexArchBundleProperties{},
		&overridableProperties{},
	)

	android.InitDefaultsModule(module)
	return module
}

type OverrideApex struct {
	android.ModuleBase
	android.OverrideModuleBase
}

func (o *OverrideApex) GenerateAndroidBuildActions(_ android.ModuleContext) {
	// All the overrides happen in the base module.
}

// override_apex is used to create an apex module based on another apex module by overriding some of
// its properties.
func OverrideApexFactory() android.Module {
	m := &OverrideApex{}

	m.AddProperties(&overridableProperties{})

	android.InitAndroidMultiTargetsArchModule(m, android.DeviceSupported, android.MultilibCommon)
	android.InitOverrideModule(m)
	return m
}

///////////////////////////////////////////////////////////////////////////////////////////////////
// Vality check routines
//
// These are called in at the very beginning of GenerateAndroidBuildActions to flag an error when
// certain conditions are not met.
//
// TODO(jiyong): move these checks to a separate go file.

var _ android.ModuleWithMinSdkVersionCheck = (*apexBundle)(nil)

// Ensures that min_sdk_version of the included modules are equal or less than the min_sdk_version
// of this apexBundle.
func (a *apexBundle) CheckMinSdkVersion(ctx android.ModuleContext) {
	if a.testApex || a.vndkApex {
		return
	}
	// apexBundle::minSdkVersion reports its own errors.
	minSdkVersion := a.minSdkVersion(ctx)
	android.CheckMinSdkVersion(ctx, minSdkVersion, a.WalkPayloadDeps)
}

// Returns apex's min_sdk_version string value, honoring overrides
func (a *apexBundle) minSdkVersionValue(ctx android.MinSdkVersionFromValueContext) string {
	// Only override the minSdkVersion value on Apexes which already specify
	// a min_sdk_version (it's optional for non-updatable apexes), and that its
	// min_sdk_version value is lower than the one to override with.
	minApiLevel := android.MinSdkVersionFromValue(ctx, a.overridableProperties.Min_sdk_version.GetOrDefault(a.ConfigurableEvaluator(ctx), ""))
	if minApiLevel.IsNone() {
		return ""
	}

	overrideMinSdkValue := ctx.DeviceConfig().ApexGlobalMinSdkVersionOverride()
	overrideApiLevel := android.MinSdkVersionFromValue(ctx, overrideMinSdkValue)
	if !overrideApiLevel.IsNone() && overrideApiLevel.CompareTo(minApiLevel) > 0 {
		minApiLevel = overrideApiLevel
	}

	return minApiLevel.String()
}

// Returns apex's min_sdk_version SdkSpec, honoring overrides
func (a *apexBundle) MinSdkVersion(ctx android.MinSdkVersionFromValueContext) android.ApiLevel {
	return a.minSdkVersion(ctx)
}

// Returns apex's min_sdk_version ApiLevel, honoring overrides
func (a *apexBundle) minSdkVersion(ctx android.MinSdkVersionFromValueContext) android.ApiLevel {
	return android.MinSdkVersionFromValue(ctx, a.minSdkVersionValue(ctx))
}

// Ensures that a lib providing stub isn't statically linked
func (a *apexBundle) checkStaticLinkingToStubLibraries(ctx android.ModuleContext) {
	// Practically, we only care about regular APEXes on the device.
	if a.testApex || a.vndkApex {
		return
	}

	librariesDirectlyInApex := make(map[string]bool)
	ctx.VisitDirectDepsProxyWithTag(sharedLibTag, func(dep android.ModuleProxy) {
		librariesDirectlyInApex[ctx.OtherModuleName(dep)] = true
	})

	a.WalkPayloadDeps(ctx, func(ctx android.BaseModuleContext, from, to android.ModuleProxy, externalDep bool) bool {
		if info, ok := android.OtherModuleProvider(ctx, to, cc.LinkableInfoProvider); ok {
			// If `to` is not actually in the same APEX as `from` then it does not need
			// apex_available and neither do any of its dependencies.
			if externalDep {
				// As soon as the dependency graph crosses the APEX boundary, don't go further.
				return false
			}

			apexName := ctx.ModuleName()
			fromName := ctx.OtherModuleName(from)
			toName := ctx.OtherModuleName(to)

			// The dynamic linker and crash_dump tool in the runtime APEX is an
			// exception to this rule. It can't make the static dependencies dynamic
			// because it can't do the dynamic linking for itself.
			// Same rule should be applied to linkerconfig, because it should be executed
			// only with static linked libraries before linker is available with ld.config.txt
			if apexName == "com.android.runtime" && (fromName == "linker" || fromName == "crash_dump" || fromName == "linkerconfig") {
				return false
			}

			// b/389067742 adds libz as an exception to this check. Although libz is
			// a part of NDK and thus provides a stable interface, it never was the
			// intention because the upstream zlib provides neither ABI- nor behavior-
			// stability. Therefore, we want to allow portable components like APEXes to
			// bundle libz by statically linking to it.
			if toName == "libz" {
				return false
			}

			isStubLibraryFromOtherApex := info.HasStubsVariants && !librariesDirectlyInApex[toName]
			if isStubLibraryFromOtherApex && !externalDep {
				ctx.ModuleErrorf("%q required by %q is a native library providing stub. "+
					"It shouldn't be included in this APEX via static linking. Dependency path: %s", to.String(), fromName, ctx.GetPathString(false))
			}
		}
		return true
	})
}

// checkUpdatable enforces APEX and its transitive dep properties to have desired values for updatable APEXes.
func (a *apexBundle) checkUpdatable(ctx android.ModuleContext) {
	if a.Updatable() {
		if a.minSdkVersionValue(ctx) == "" {
			ctx.PropertyErrorf("updatable", "updatable APEXes should set min_sdk_version as well")
		}
		if a.minSdkVersion(ctx).IsCurrent() {
			ctx.PropertyErrorf("updatable", "updatable APEXes should not set min_sdk_version to current. Please use a finalized API level or a recognized in-development codename")
		}
		if a.UsePlatformApis() {
			ctx.PropertyErrorf("updatable", "updatable APEXes can't use platform APIs")
		}
		if a.FutureUpdatable() {
			ctx.PropertyErrorf("future_updatable", "Already updatable. Remove `future_updatable: true:`")
		}
		a.checkJavaStableSdkVersion(ctx)
		a.checkClasspathFragments(ctx)
	}
}

// checkClasspathFragments enforces that all classpath fragments in deps generate classpaths.proto config.
func (a *apexBundle) checkClasspathFragments(ctx android.ModuleContext) {
	ctx.VisitDirectDepsProxy(func(module android.ModuleProxy) {
		if tag := ctx.OtherModuleDependencyTag(module); tag == bcpfTag || tag == sscpfTag {
			info, _ := android.OtherModuleProvider(ctx, module, java.ClasspathFragmentProtoContentInfoProvider)
			if !info.ClasspathFragmentProtoGenerated {
				ctx.OtherModuleErrorf(module, "is included in updatable apex %v, it must not set generate_classpaths_proto to false", ctx.ModuleName())
			}
		}
	})
}

// checkJavaStableSdkVersion enforces that all Java deps are using stable SDKs to compile.
func (a *apexBundle) checkJavaStableSdkVersion(ctx android.ModuleContext) {
	// Visit direct deps only. As long as we guarantee top-level deps are using stable SDKs,
	// java's checkLinkType guarantees correct usage for transitive deps
	ctx.VisitDirectDepsProxy(func(module android.ModuleProxy) {
		tag := ctx.OtherModuleDependencyTag(module)
		switch tag {
		case javaLibTag, androidAppTag:
			if err := java.CheckStableSdkVersion(ctx, module); err != nil {
				ctx.ModuleErrorf("cannot depend on \"%v\": %v", ctx.OtherModuleName(module), err)
			}
		}
	})
}

// checkApexAvailability ensures that the all the dependencies are marked as available for this APEX.
func (a *apexBundle) checkApexAvailability(ctx android.ModuleContext) {
	// Let's be practical. Availability for test, host, and the VNDK apex isn't important
	if a.testApex || a.vndkApex {
		return
	}

	// Because APEXes targeting other than system/system_ext partitions can't set
	// apex_available, we skip checks for these APEXes
	if a.SocSpecific() || a.DeviceSpecific() || (a.ProductSpecific() && ctx.Config().EnforceProductPartitionInterface()) {
		return
	}

	// Temporarily bypass /product APEXes with a specific prefix.
	// TODO: b/352818241 - Remove this after APEX availability is enforced for /product APEXes.
	if a.ProductSpecific() && strings.HasPrefix(a.ApexVariationName(), "com.sdv.") {
		return
	}

	// Coverage build adds additional dependencies for the coverage-only runtime libraries.
	// Requiring them and their transitive depencies with apex_available is not right
	// because they just add noise.
	if ctx.Config().IsEnvTrue("EMMA_INSTRUMENT") || a.IsNativeCoverageNeeded(ctx) {
		return
	}

	a.WalkPayloadDeps(ctx, func(ctx android.BaseModuleContext, from, to android.ModuleProxy, externalDep bool) bool {
		// As soon as the dependency graph crosses the APEX boundary, don't go further.
		if externalDep {
			return false
		}

		apexName := ctx.ModuleName()
		for _, props := range ctx.Module().GetProperties() {
			if apexProps, ok := props.(*apexBundleProperties); ok {
				if apexProps.Apex_available_name != nil {
					apexName = *apexProps.Apex_available_name
				}
			}
		}
		fromName := ctx.OtherModuleName(from)
		toName := ctx.OtherModuleName(to)

		if android.CheckAvailableForApex(apexName,
			android.OtherModuleProviderOrDefault(ctx, to, android.ApexAvailableInfoProvider).ApexAvailableFor) {
			return true
		}

		// Let's give some hint for apex_available
		hint := fmt.Sprintf("%q", apexName)

		if strings.HasPrefix(apexName, "com.") && !strings.HasPrefix(apexName, "com.android.") && strings.Count(apexName, ".") >= 2 {
			// In case of a partner APEX, prefix format might be an option.
			components := strings.Split(apexName, ".")
			components[len(components)-1] = "*"
			hint += fmt.Sprintf(" or %q", strings.Join(components, "."))
		}

		ctx.ModuleErrorf("%q requires %q that doesn't list the APEX under 'apex_available'."+
			"\n\nDependency path:%s\n\n"+
			"Consider adding %s to 'apex_available' property of %q",
			fromName, toName, ctx.GetPathString(true), hint, toName)
		// Visit this module's dependencies to check and report any issues with their availability.
		return true
	})
}

// checkStaticExecutable ensures that executables in an APEX are not static.
func (a *apexBundle) checkStaticExecutables(ctx android.ModuleContext) {
	ctx.VisitDirectDepsProxy(func(module android.ModuleProxy) {
		if ctx.OtherModuleDependencyTag(module) != executableTag {
			return
		}

		if android.OtherModuleProviderOrDefault(ctx, module, cc.LinkableInfoProvider).StaticExecutable {
			apex := a.ApexVariationName()
			exec := ctx.OtherModuleName(module)
			if isStaticExecutableAllowed(apex, exec) {
				return
			}
			ctx.ModuleErrorf("executable %s is static", ctx.OtherModuleName(module))
		}
	})
}

// A small list of exceptions where static executables are allowed in APEXes.
func isStaticExecutableAllowed(apex string, exec string) bool {
	m := map[string][]string{
		"com.android.runtime": {
			"linker",
			"linkerconfig",
		},
	}
	execNames, ok := m[apex]
	return ok && android.InList(exec, execNames)
}

// Collect information for opening IDE project files in java/jdeps.go.
func (a *apexBundle) IDEInfo(ctx android.BaseModuleContext, dpInfo *android.IdeInfo) {
	dpInfo.Deps = append(dpInfo.Deps, a.properties.Java_libs...)
	dpInfo.Deps = append(dpInfo.Deps, a.properties.Bootclasspath_fragments.GetOrDefault(ctx, nil)...)
	dpInfo.Deps = append(dpInfo.Deps, a.properties.Systemserverclasspath_fragments.GetOrDefault(ctx, nil)...)
}

func init() {
	android.AddNeverAllowRules(createBcpPermittedPackagesRules(qBcpPackages())...)
	android.AddNeverAllowRules(createBcpPermittedPackagesRules(rBcpPackages())...)
}

func createBcpPermittedPackagesRules(bcpPermittedPackages map[string][]string) []android.Rule {
	rules := make([]android.Rule, 0, len(bcpPermittedPackages))
	for jar, permittedPackages := range bcpPermittedPackages {
		permittedPackagesRule := android.NeverAllow().
			With("name", jar).
			WithMatcher("permitted_packages", android.NotInList(permittedPackages)).
			Because(jar +
				" bootjar may only use these package prefixes: " + strings.Join(permittedPackages, ",") +
				". Please consider the following alternatives:\n" +
				"    1. If the offending code is from a statically linked library, consider " +
				"removing that dependency and using an alternative already in the " +
				"bootclasspath, or perhaps a shared library." +
				"    2. Move the offending code into an allowed package.\n" +
				"    3. Jarjar the offending code. Please be mindful of the potential system " +
				"health implications of bundling that code, particularly if the offending jar " +
				"is part of the bootclasspath.")

		rules = append(rules, permittedPackagesRule)
	}
	return rules
}

// DO NOT EDIT! These are the package prefixes that are exempted from being AOT'ed by ART.
// Adding code to the bootclasspath in new packages will cause issues on module update.
func qBcpPackages() map[string][]string {
	return map[string][]string{
		"conscrypt": {
			"android.net.ssl",
			"com.android.org.conscrypt",
		},
		"updatable-media": {
			"android.media",
		},
	}
}

// DO NOT EDIT! These are the package prefixes that are exempted from being AOT'ed by ART.
// Adding code to the bootclasspath in new packages will cause issues on module update.
func rBcpPackages() map[string][]string {
	return map[string][]string{
		"framework-mediaprovider": {
			"android.provider",
		},
		"framework-permission": {
			"android.permission",
			"android.app.role",
			"com.android.permission",
			"com.android.role",
		},
		"framework-sdkextensions": {
			"android.os.ext",
		},
		"framework-statsd": {
			"android.app",
			"android.os",
			"android.util",
			"com.android.internal.statsd",
			"com.android.server.stats",
		},
		"framework-wifi": {
			"com.android.server.wifi",
			"com.android.wifi.x",
			"android.hardware.wifi",
			"android.net.wifi",
		},
		"framework-tethering": {
			"android.net",
		},
	}
}

// verifyNativeImplementationLibs compares the list of transitive implementation libraries used to link native
// libraries in the apex against the list of implementation libraries in the apex, ensuring that none of the
// libraries in the apex have references to private APIs from outside the apex.
func (a *apexBundle) verifyNativeImplementationLibs(ctx android.ModuleContext) {
	var directImplementationLibs android.Paths
	var transitiveImplementationLibs []depset.DepSet[android.Path]

	if a.testApex {
		return
	}

	if a.UsePlatformApis() {
		return
	}

	checkApexTag := func(tag blueprint.DependencyTag) bool {
		switch tag {
		case sharedLibTag, jniLibTag, executableTag, androidAppTag:
			return true
		default:
			return false
		}
	}

	checkTransitiveTag := func(tag blueprint.DependencyTag) bool {
		switch {
		case cc.IsSharedDepTag(tag), java.IsJniDepTag(tag), rust.IsRlibDepTag(tag), rust.IsDylibDepTag(tag), checkApexTag(tag):
			return true
		default:
			return false
		}
	}

	var appEmbeddedJNILibs android.Paths
	ctx.VisitDirectDepsProxy(func(dep android.ModuleProxy) {
		tag := ctx.OtherModuleDependencyTag(dep)
		if !checkApexTag(tag) {
			return
		}
		if tag == sharedLibTag || tag == jniLibTag {
			outputFile := android.OutputFileForModule(ctx, dep, "")
			directImplementationLibs = append(directImplementationLibs, outputFile)
		}
		if info, ok := android.OtherModuleProvider(ctx, dep, cc.ImplementationDepInfoProvider); ok {
			transitiveImplementationLibs = append(transitiveImplementationLibs, info.ImplementationDeps)
		}
		if info, ok := android.OtherModuleProvider(ctx, dep, java.AppInfoProvider); ok {
			appEmbeddedJNILibs = append(appEmbeddedJNILibs, info.EmbeddedJNILibs...)
		}
	})

	depSet := depset.New(depset.PREORDER, directImplementationLibs, transitiveImplementationLibs)
	allImplementationLibs := depSet.ToList()

	allFileInfos := slices.Concat(a.filesInfo, a.unwantedTransitiveFilesInfo, a.duplicateTransitiveFilesInfo)

	for _, lib := range allImplementationLibs {
		inApex := slices.ContainsFunc(allFileInfos, func(fi apexFile) bool {
			return fi.builtFile == lib
		})
		inApkInApex := slices.Contains(appEmbeddedJNILibs, lib)

		if !inApex && !inApkInApex {
			ctx.ModuleErrorf("library in apex transitively linked against implementation library %q not in apex", lib)
			var depPath []android.ModuleProxy
			ctx.WalkDepsProxy(func(child, parent android.ModuleProxy) bool {
				if depPath != nil {
					return false
				}

				tag := ctx.OtherModuleDependencyTag(child)

				if android.EqualModules(parent, ctx.Module()) {
					if !checkApexTag(tag) {
						return false
					}
				}

				if checkTransitiveTag(tag) {
					if android.OutputFileForModule(ctx, child, "") == lib {
						depPath = ctx.GetProxyWalkPath()
					}
					return true
				}

				return false
			})
			if depPath != nil {
				ctx.ModuleErrorf("dependency path:")
				for _, m := range depPath {
					ctx.ModuleErrorf("   %s", ctx.OtherModuleName(m))
				}
				return
			}
		}
	}
}

// TODO(b/399527905) libvintf is not forward compatible.
func (a *apexBundle) enforceNoVintfInUpdatable(ctx android.ModuleContext) {
	if !a.Updatable() {
		return
	}
	for _, fi := range a.filesInfo {
		if match, _ := path.Match("etc/vintf/*", fi.path()); match {
			ctx.ModuleErrorf("VINTF fragment (%s) is not supported in updatable APEX.", fi.path())
			break
		}
	}
}

func (a *apexBundle) setSymbolInfosProvider(ctx android.ModuleContext) {
	if !a.properties.HideFromMake && a.installable() {
		infos := &cc.SymbolInfos{}
		for _, fi := range a.filesInfo {
			linkToSystemLib := a.linkToSystemLib && fi.transitiveDep && fi.availableToPlatform()
			if linkToSystemLib {
				// No need to copy the file since it's linked to the system file
				continue
			}
			moduleDir := android.PathForModuleInPartitionInstall(ctx, "apex", a.BaseModuleName(), fi.installDir)
			info := &cc.SymbolInfo{
				Name:          a.fullModuleName(a.BaseModuleName(), linkToSystemLib, &fi),
				ModuleDir:     moduleDir.String(),
				Uninstallable: !a.installable(),
			}
			switch fi.class {
			case nativeSharedLib, nativeExecutable, nativeTest:
				info.Stem = fi.stem()
				if fi.providers != nil && fi.providers.linkableInfo != nil {
					linkableInfo := fi.providers.linkableInfo
					if linkableInfo.UnstrippedOutputFile != nil {
						info.UnstrippedBinaryPath = linkableInfo.UnstrippedOutputFile
					}
				}
				if info.UnstrippedBinaryPath != nil {
					infos.AppendSymbols(info)
				}
			case app:
				if fi.providers != nil && fi.providers.appInfo != nil {
					appInfo := fi.providers.appInfo
					// The following logic comes from java.GetJniSymbolInfos
					for _, install := range java.JNISymbolsInstalls(appInfo.JniLibs, moduleDir.String()) {
						info := &cc.SymbolInfo{
							Name:                 fi.providers.commonInfo.BaseModuleName,
							ModuleDir:            filepath.Dir(install.To),
							Uninstallable:        fi.providers.commonInfo.SkipInstall || !fi.providers.javaInfo.Installable || fi.providers.commonInfo.NoFullInstall,
							UnstrippedBinaryPath: install.From,
							InstalledStem:        filepath.Base(install.To),
						}
						infos.AppendSymbols(info)
					}
				}
			}
		}

		cc.CopySymbolsAndSetSymbolsInfoProvider(ctx, infos)
	}
}
