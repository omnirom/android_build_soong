// Copyright 2020 Google Inc. All rights reserved.
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

// This file contains the module implementations for runtime_resource_overlay and
// override_runtime_resource_overlay.

import (
	"android/soong/android"

	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"
)

func init() {
	RegisterRuntimeResourceOverlayBuildComponents(android.InitRegistrationContext)
}

func RegisterRuntimeResourceOverlayBuildComponents(ctx android.RegistrationContext) {
	ctx.RegisterModuleType("runtime_resource_overlay", RuntimeResourceOverlayFactory)
	ctx.RegisterModuleType("autogen_runtime_resource_overlay", AutogenRuntimeResourceOverlayFactory)
	ctx.RegisterModuleType("override_runtime_resource_overlay", OverrideRuntimeResourceOverlayModuleFactory)
}

type RuntimeResourceOverlayInfo struct {
	OutputFile                    android.Path
	Certificate                   Certificate
	Theme                         string
	OverriddenManifestPackageName string
}

var RuntimeResourceOverlayInfoProvider = blueprint.NewProvider[RuntimeResourceOverlayInfo]()

type RuntimeResourceOverlay struct {
	android.ModuleBase
	android.DefaultableModuleBase
	android.OverridableModuleBase
	aapt

	properties            RuntimeResourceOverlayProperties
	overridableProperties OverridableRuntimeResourceOverlayProperties

	certificate Certificate

	outputFile android.Path
	installDir android.InstallPath
}

type RuntimeResourceOverlayProperties struct {
	// the name of a certificate in the default certificate directory or an android_app_certificate
	// module name in the form ":module".
	Certificate proptools.Configurable[string] `android:"replace_instead_of_append"`

	// Name of the signing certificate lineage file.
	Lineage *string

	// For overriding the --rotation-min-sdk-version property of apksig
	RotationMinSdkVersion *string

	// optional theme name. If specified, the overlay package will be applied
	// only when the ro.boot.vendor.overlay.theme system property is set to the same value.
	Theme *string

	// If not blank, set to the version of the sdk to compile against. This
	// can be either an API version (e.g. "29" for API level 29 AKA Android 10)
	// or special subsets of the current platform, for example "none", "current",
	// "core", "system", "test". See build/soong/java/sdk.go for the full and
	// up-to-date list of possible values.
	// Defaults to compiling against the current platform.
	Sdk_version *string

	// if not blank, set the minimum version of the sdk that the compiled artifacts will run against.
	// Defaults to sdk_version if not set.
	Min_sdk_version *string

	// list of android_library modules whose resources are extracted and linked against statically
	Static_libs proptools.Configurable[[]string]

	// list of android_app modules whose resources are extracted and linked against
	Resource_libs []string

	// Names of modules to be overridden. Listed modules can only be other overlays
	// (in Make or Soong).
	// This does not completely prevent installation of the overridden overlays, but if both
	// overlays would be installed by default (in PRODUCT_PACKAGES) the other overlay will be removed
	// from PRODUCT_PACKAGES.
	Overrides []string
}

// RRO's partition logic is different from the partition logic of other modules defined in soong/android/paths.go
// The default partition for RRO is "/product" and not "/system"
func rroPartition(ctx android.ModuleContext) string {
	var partition string
	if ctx.DeviceSpecific() {
		partition = ctx.DeviceConfig().OdmPath()
	} else if ctx.SocSpecific() {
		partition = ctx.DeviceConfig().VendorPath()
	} else if ctx.SystemExtSpecific() {
		partition = ctx.DeviceConfig().SystemExtPath()
	} else {
		partition = ctx.DeviceConfig().ProductPath()
	}
	return partition
}

func (r *RuntimeResourceOverlay) DepsMutator(ctx android.BottomUpMutatorContext) {
	sdkDep := decodeSdkDep(ctx, android.SdkContext(r))
	if sdkDep.hasFrameworkLibs() {
		r.aapt.deps(ctx, sdkDep)
	}

	cert := android.SrcIsModule(r.properties.Certificate.GetOrDefault(ctx, ""))
	if cert != "" {
		ctx.AddDependency(ctx.Module(), certificateTag, cert)
	}

	ctx.AddVariationDependencies(nil, staticLibTag, r.properties.Static_libs.GetOrDefault(ctx, nil)...)
	ctx.AddVariationDependencies(nil, libTag, r.properties.Resource_libs...)

	for _, aconfig_declaration := range r.aaptProperties.Flags_packages {
		ctx.AddDependency(ctx.Module(), aconfigDeclarationTag, aconfig_declaration)
	}
}

func (r *RuntimeResourceOverlay) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	// Compile and link resources
	r.aapt.hasNoCode = true
	// Do not remove resources without default values nor dedupe resource configurations with the same value
	aaptLinkFlags := []string{"--no-resource-deduping", "--no-resource-removal"}

	// Add TARGET_AAPT_CHARACTERISTICS values to AAPT link flags if they exist and --product flags were not provided.
	hasProduct := android.PrefixInList(r.aaptProperties.Aaptflags, "--product")
	if !hasProduct && len(ctx.Config().ProductAAPTCharacteristics()) > 0 {
		aaptLinkFlags = append(aaptLinkFlags, "--product", ctx.Config().ProductAAPTCharacteristics())
	}

	if !Bool(r.aaptProperties.Aapt_include_all_resources) {
		// Product AAPT config
		for _, aaptConfig := range ctx.Config().ProductAAPTConfig() {
			aaptLinkFlags = append(aaptLinkFlags, "-c", aaptConfig)
		}

		// Product AAPT preferred config
		if len(ctx.Config().ProductAAPTPreferredConfig()) > 0 {
			aaptLinkFlags = append(aaptLinkFlags, "--preferred-density", ctx.Config().ProductAAPTPreferredConfig())
		}
	}

	// Allow the override of "package name" and "overlay target package name"
	manifestPackageName, overridden := ctx.DeviceConfig().OverrideManifestPackageNameFor(ctx.ModuleName())
	if overridden || r.overridableProperties.Package_name != nil {
		// The product override variable has a priority over the package_name property.
		if !overridden {
			manifestPackageName = *r.overridableProperties.Package_name
		}
		aaptLinkFlags = append(aaptLinkFlags, generateAaptRenamePackageFlags(manifestPackageName, false)...)
	}
	if r.overridableProperties.Target_package_name != nil {
		aaptLinkFlags = append(aaptLinkFlags,
			"--rename-overlay-target-package "+*r.overridableProperties.Target_package_name)
	}
	if r.overridableProperties.Category != nil {
		aaptLinkFlags = append(aaptLinkFlags,
			"--rename-overlay-category "+*r.overridableProperties.Category)
	}
	aconfigTextFilePaths := getAconfigFilePaths(ctx)
	r.aapt.buildActions(ctx,
		aaptBuildActionOptions{
			sdkContext:                     r,
			enforceDefaultTargetSdkVersion: false,
			extraLinkFlags:                 aaptLinkFlags,
			aconfigTextFiles:               aconfigTextFilePaths,
		},
	)

	// Sign the built package
	_, _, certificates := collectAppDeps(ctx, r, false, false)
	r.certificate, certificates = processMainCert(r.ModuleBase, r.properties.Certificate.GetOrDefault(ctx, ""), certificates, ctx)
	signed := android.PathForModuleOut(ctx, "signed", r.Name()+".apk")
	var lineageFile android.Path
	if lineage := String(r.properties.Lineage); lineage != "" {
		lineageFile = android.PathForModuleSrc(ctx, lineage)
	}

	rotationMinSdkVersion := String(r.properties.RotationMinSdkVersion)

	SignAppPackage(ctx, signed, r.aapt.exportPackage, certificates, nil, lineageFile, rotationMinSdkVersion)

	r.outputFile = signed
	partition := rroPartition(ctx)
	r.installDir = android.PathForModuleInPartitionInstall(ctx, partition, "overlay", String(r.properties.Theme))
	ctx.InstallFile(r.installDir, r.outputFile.Base(), r.outputFile)

	android.SetProvider(ctx, FlagsPackagesProvider, FlagsPackages{
		AconfigTextFiles: aconfigTextFilePaths,
	})

	android.SetProvider(ctx, RuntimeResourceOverlayInfoProvider, RuntimeResourceOverlayInfo{
		OutputFile:  r.outputFile,
		Certificate: r.Certificate(),
		Theme:       r.Theme(),
	})

	ctx.SetOutputFiles([]android.Path{r.outputFile}, "")

	buildComplianceMetadata(ctx)
}

func (r *RuntimeResourceOverlay) SdkVersion(ctx android.EarlyModuleContext) android.SdkSpec {
	return android.SdkSpecFrom(ctx, String(r.properties.Sdk_version))
}

func (r *RuntimeResourceOverlay) SystemModules() string {
	return ""
}

func (r *RuntimeResourceOverlay) MinSdkVersion(ctx android.EarlyModuleContext) android.ApiLevel {
	if r.properties.Min_sdk_version != nil {
		return android.ApiLevelFrom(ctx, *r.properties.Min_sdk_version)
	}
	return r.SdkVersion(ctx).ApiLevel
}

func (r *RuntimeResourceOverlay) ReplaceMaxSdkVersionPlaceholder(ctx android.EarlyModuleContext) android.ApiLevel {
	return android.SdkSpecPrivate.ApiLevel
}

func (r *RuntimeResourceOverlay) TargetSdkVersion(ctx android.EarlyModuleContext) android.ApiLevel {
	return r.SdkVersion(ctx).ApiLevel
}

func (r *RuntimeResourceOverlay) Certificate() Certificate {
	return r.certificate
}

func (r *RuntimeResourceOverlay) Theme() string {
	return String(r.properties.Theme)
}

// runtime_resource_overlay generates a resource-only apk file that can overlay application and
// system resources at run time.
func RuntimeResourceOverlayFactory() android.Module {
	module := &RuntimeResourceOverlay{}
	module.AddProperties(
		&module.properties,
		&module.aaptProperties,
		&module.overridableProperties)

	android.InitAndroidMultiTargetsArchModule(module, android.DeviceSupported, android.MultilibCommon)
	android.InitDefaultableModule(module)
	android.InitOverridableModule(module, &module.properties.Overrides)
	return module
}

// runtime_resource_overlay properties that can be overridden by override_runtime_resource_overlay
type OverridableRuntimeResourceOverlayProperties struct {
	// the package name of this app. The package name in the manifest file is used if one was not given.
	Package_name *string

	// the target package name of this overlay app. The target package name in the manifest file is used if one was not given.
	Target_package_name *string

	// the rro category of this overlay. The category in the manifest file is used if one was not given.
	Category *string
}

type OverrideRuntimeResourceOverlay struct {
	android.ModuleBase
	android.OverrideModuleBase
}

func (i *OverrideRuntimeResourceOverlay) GenerateAndroidBuildActions(_ android.ModuleContext) {
	// All the overrides happen in the base module.
	// TODO(jungjw): Check the base module type.
}

// override_runtime_resource_overlay is used to create a module based on another
// runtime_resource_overlay module by overriding some of its properties.
func OverrideRuntimeResourceOverlayModuleFactory() android.Module {
	m := &OverrideRuntimeResourceOverlay{}
	m.AddProperties(&OverridableRuntimeResourceOverlayProperties{})

	android.InitAndroidMultiTargetsArchModule(m, android.DeviceSupported, android.MultilibCommon)
	android.InitOverrideModule(m)
	return m
}

var (
	generateOverlayManifestFile = pctx.AndroidStaticRule("generate_overlay_manifest",
		blueprint.RuleParams{
			Command: "build/make/tools/generate-enforce-rro-android-manifest.py " +
				"--package-info $in " +
				"--partition ${partition} " +
				"--priority ${priority} -o $out",
			CommandDeps: []string{"build/make/tools/generate-enforce-rro-android-manifest.py"},
		}, "partition", "priority",
	)
)

type AutogenRuntimeResourceOverlay struct {
	android.ModuleBase
	aapt

	properties AutogenRuntimeResourceOverlayProperties

	certificate Certificate
	outputFile  android.Path
}

type AutogenRuntimeResourceOverlayProperties struct {
	Base        *string
	Sdk_version *string
	Manifest    *string `android:"path"`
}

func AutogenRuntimeResourceOverlayFactory() android.Module {
	m := &AutogenRuntimeResourceOverlay{}
	m.AddProperties(&m.properties)
	android.InitAndroidArchModule(m, android.DeviceSupported, android.MultilibCommon)

	return m
}

type rroDependencyTag struct {
	blueprint.DependencyTag
}

// Autogenerated RROs should always depend on the source android_app that created it.
func (tag rroDependencyTag) ReplaceSourceWithPrebuilt() bool {
	return false
}

var rroDepTag = rroDependencyTag{}

func (a *AutogenRuntimeResourceOverlay) DepsMutator(ctx android.BottomUpMutatorContext) {
	sdkDep := decodeSdkDep(ctx, android.SdkContext(a))
	if sdkDep.hasFrameworkLibs() {
		a.aapt.deps(ctx, sdkDep)
	}
	ctx.AddDependency(ctx.Module(), rroDepTag, proptools.String(a.properties.Base))
}

func (a *AutogenRuntimeResourceOverlay) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	if !a.Enabled(ctx) {
		return
	}
	var rroDirs android.Paths
	// Get rro dirs of the base app
	ctx.VisitDirectDepsWithTag(rroDepTag, func(m android.Module) {
		aarDep, _ := m.(AndroidLibraryDependency)
		if ctx.InstallInProduct() {
			rroDirs = filterRRO(aarDep.RRODirsDepSet(), product)
		} else {
			rroDirs = filterRRO(aarDep.RRODirsDepSet(), device)
		}
	})

	if len(rroDirs) == 0 {
		return
	}

	// Generate a manifest file
	genManifest := android.PathForModuleGen(ctx, "AndroidManifest.xml")
	partition := "vendor"
	priority := "0"
	if ctx.InstallInProduct() {
		partition = "product"
		priority = "1"
	}
	ctx.Build(pctx, android.BuildParams{
		Rule:   generateOverlayManifestFile,
		Input:  android.PathForModuleSrc(ctx, proptools.String(a.properties.Manifest)),
		Output: genManifest,
		Args: map[string]string{
			"partition": partition,
			"priority":  priority,
		},
	})

	// Compile and link resources into package-res.apk
	a.aapt.hasNoCode = true
	aaptLinkFlags := []string{"--auto-add-overlay", "--keep-raw-values", "--no-resource-deduping", "--no-resource-removal"}

	a.aapt.buildActions(ctx,
		aaptBuildActionOptions{
			sdkContext:       a,
			extraLinkFlags:   aaptLinkFlags,
			rroDirs:          &rroDirs,
			manifestForAapt:  genManifest,
			aconfigTextFiles: getAconfigFilePaths(ctx),
		},
	)

	if a.exportPackage == nil {
		return
	}
	// Sign the built package
	var certificates []Certificate
	a.certificate, certificates = processMainCert(a.ModuleBase, "", nil, ctx)
	signed := android.PathForModuleOut(ctx, "signed", a.Name()+".apk")
	SignAppPackage(ctx, signed, a.exportPackage, certificates, nil, nil, "")
	a.outputFile = signed

	// Install the signed apk
	installDir := android.PathForModuleInstall(ctx, "overlay")
	ctx.InstallFile(installDir, signed.Base(), signed)

	android.SetProvider(ctx, RuntimeResourceOverlayInfoProvider, RuntimeResourceOverlayInfo{
		OutputFile:  signed,
		Certificate: a.certificate,
	})
}

func (a *AutogenRuntimeResourceOverlay) SdkVersion(ctx android.EarlyModuleContext) android.SdkSpec {
	return android.SdkSpecFrom(ctx, String(a.properties.Sdk_version))
}

func (a *AutogenRuntimeResourceOverlay) SystemModules() string {
	return ""
}

func (a *AutogenRuntimeResourceOverlay) MinSdkVersion(ctx android.EarlyModuleContext) android.ApiLevel {
	return a.SdkVersion(ctx).ApiLevel
}

func (r *AutogenRuntimeResourceOverlay) ReplaceMaxSdkVersionPlaceholder(ctx android.EarlyModuleContext) android.ApiLevel {
	return android.SdkSpecPrivate.ApiLevel
}

func (a *AutogenRuntimeResourceOverlay) TargetSdkVersion(ctx android.EarlyModuleContext) android.ApiLevel {
	return a.SdkVersion(ctx).ApiLevel
}

func (a *AutogenRuntimeResourceOverlay) InstallInProduct() bool {
	return a.ProductSpecific()
}
