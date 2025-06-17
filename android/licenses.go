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

package android

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/google/blueprint"
	"github.com/google/blueprint/gobtools"
)

// Adds cross-cutting licenses dependency to propagate license metadata through the build system.
//
// Stage 1 - bottom-up records package-level default_applicable_licenses property mapped by package name.
// Stage 2 - bottom-up converts licenses property or package default_applicable_licenses to dependencies.
// Stage 3 - bottom-up type-checks every added applicable license dependency and license_kind dependency.
// Stage 4 - GenerateBuildActions calculates properties for the union of license kinds, conditions and texts.

type licensesDependencyTag struct {
	blueprint.BaseDependencyTag
}

func (l licensesDependencyTag) SdkMemberType(Module) SdkMemberType {
	// Add the supplied module to the sdk as a license module.
	return LicenseModuleSdkMemberType
}

func (l licensesDependencyTag) ExportMember() bool {
	// The license module will only every be referenced from within the sdk. This will ensure that it
	// gets a unique name and so avoid clashing with the original license module.
	return false
}

var (
	licensesTag = licensesDependencyTag{}

	// License modules, i.e. modules depended upon via a licensesTag, must be automatically added to
	// any sdk/module_exports to which their referencing module is a member.
	_ SdkMemberDependencyTag = licensesTag
)

// Describes the property provided by a module to reference applicable licenses.
type applicableLicensesProperty interface {
	// The name of the property. e.g. default_applicable_licenses or licenses
	getName() string
	// The values assigned to the property. (Must reference license modules.)
	getStrings() []string
}

type applicableLicensesPropertyImpl struct {
	name             string
	licensesProperty *[]string
}

type applicableLicensesPropertyImplGob struct {
	Name             string
	LicensesProperty []string
}

func (a *applicableLicensesPropertyImpl) ToGob() *applicableLicensesPropertyImplGob {
	return &applicableLicensesPropertyImplGob{
		Name:             a.name,
		LicensesProperty: *a.licensesProperty,
	}
}

func (a *applicableLicensesPropertyImpl) FromGob(data *applicableLicensesPropertyImplGob) {
	a.name = data.Name
	a.licensesProperty = &data.LicensesProperty
}

func (a applicableLicensesPropertyImpl) GobEncode() ([]byte, error) {
	return gobtools.CustomGobEncode[applicableLicensesPropertyImplGob](&a)
}

func (a *applicableLicensesPropertyImpl) GobDecode(data []byte) error {
	return gobtools.CustomGobDecode[applicableLicensesPropertyImplGob](data, a)
}

func newApplicableLicensesProperty(name string, licensesProperty *[]string) applicableLicensesProperty {
	return applicableLicensesPropertyImpl{
		name:             name,
		licensesProperty: licensesProperty,
	}
}

func (p applicableLicensesPropertyImpl) getName() string {
	return p.name
}

func (p applicableLicensesPropertyImpl) getStrings() []string {
	return *p.licensesProperty
}

// Set the primary applicable licenses property for a module.
func setPrimaryLicensesProperty(module Module, name string, licensesProperty *[]string) {
	module.base().primaryLicensesProperty = newApplicableLicensesProperty(name, licensesProperty)
}

// Storage blob for a package's default_applicable_licenses mapped by package directory.
type licensesContainer struct {
	licenses []string
}

func (r licensesContainer) getLicenses() []string {
	return r.licenses
}

var packageDefaultLicensesMap = NewOnceKey("packageDefaultLicensesMap")

// The map from package dir name to default applicable licenses as a licensesContainer.
func moduleToPackageDefaultLicensesMap(config Config) *sync.Map {
	return config.Once(packageDefaultLicensesMap, func() interface{} {
		return &sync.Map{}
	}).(*sync.Map)
}

// Registers the function that maps each package to its default_applicable_licenses.
//
// This goes before defaults expansion so the defaults can pick up the package default.
func RegisterLicensesPackageMapper(ctx RegisterMutatorsContext) {
	ctx.BottomUp("licensesPackageMapper", licensesPackageMapper)
}

// Registers the function that gathers the license dependencies for each module.
//
// This goes after defaults expansion so that it can pick up default licenses and before visibility enforcement.
func RegisterLicensesPropertyGatherer(ctx RegisterMutatorsContext) {
	ctx.BottomUp("licensesPropertyGatherer", licensesPropertyGatherer)
}

// Registers the function that verifies the licenses and license_kinds dependency types for each module.
func RegisterLicensesDependencyChecker(ctx RegisterMutatorsContext) {
	ctx.BottomUp("licensesPropertyChecker", licensesDependencyChecker)
}

// Maps each package to its default applicable licenses.
func licensesPackageMapper(ctx BottomUpMutatorContext) {
	p, ok := ctx.Module().(*packageModule)
	if !ok {
		return
	}

	licenses := getLicenses(ctx, p)

	dir := ctx.ModuleDir()
	c := makeLicensesContainer(licenses)
	moduleToPackageDefaultLicensesMap(ctx.Config()).Store(dir, c)
}

// Copies the default_applicable_licenses property values for mapping by package directory.
func makeLicensesContainer(propVals []string) licensesContainer {
	licenses := make([]string, 0, len(propVals))
	licenses = append(licenses, propVals...)

	return licensesContainer{licenses}
}

// Gathers the applicable licenses into dependency references after defaults expansion.
func licensesPropertyGatherer(ctx BottomUpMutatorContext) {
	m, ok := ctx.Module().(Module)
	if !ok {
		return
	}

	if exemptFromRequiredApplicableLicensesProperty(m) {
		return
	}

	licenses := getLicenses(ctx, m)

	var fullyQualifiedLicenseNames []string
	for _, license := range licenses {
		fullyQualifiedLicenseName := license
		if !strings.HasPrefix(license, "//") {
			licenseModuleDir := ctx.OtherModuleDir(m)
			for licenseModuleDir != "." && !ctx.OtherModuleExists(fmt.Sprintf("//%s:%s", licenseModuleDir, license)) {
				licenseModuleDir = filepath.Dir(licenseModuleDir)
			}
			if licenseModuleDir == "." {
				fullyQualifiedLicenseName = license
			} else {
				fullyQualifiedLicenseName = fmt.Sprintf("//%s:%s", licenseModuleDir, license)
			}
		}
		fullyQualifiedLicenseNames = append(fullyQualifiedLicenseNames, fullyQualifiedLicenseName)
	}

	ctx.AddVariationDependencies(nil, licensesTag, fullyQualifiedLicenseNames...)
}

// Verifies the license and license_kind dependencies are each the correct kind of module.
func licensesDependencyChecker(ctx BottomUpMutatorContext) {
	m, ok := ctx.Module().(Module)
	if !ok {
		return
	}

	// license modules have no licenses, but license_kinds must refer to license_kind modules
	if _, ok := m.(*licenseModule); ok {
		for _, module := range ctx.GetDirectDepsWithTag(licenseKindTag) {
			if _, ok := module.(*licenseKindModule); !ok {
				ctx.ModuleErrorf("license_kinds property %q is not a license_kind module", ctx.OtherModuleName(module))
			}
		}
		return
	}

	if exemptFromRequiredApplicableLicensesProperty(m) {
		return
	}

	for _, module := range ctx.GetDirectDepsWithTag(licensesTag) {
		if _, ok := module.(*licenseModule); !ok {
			propertyName := "licenses"
			primaryProperty := m.base().primaryLicensesProperty
			if primaryProperty != nil {
				propertyName = primaryProperty.getName()
			}
			ctx.ModuleErrorf("%s property %q is not a license module", propertyName, ctx.OtherModuleName(module))
		}
	}
}

// Flattens license and license_kind dependencies into calculated properties.
//
// Re-validates applicable licenses properties refer only to license modules and license_kinds properties refer
// only to license_kind modules.
func licensesPropertyFlattener(ctx ModuleContext) {
	m, ok := ctx.Module().(Module)
	if !ok {
		return
	}

	if exemptFromRequiredApplicableLicensesProperty(m) {
		return
	}

	var licenses []string
	var texts NamedPaths
	var conditions []string
	var kinds []string
	for _, module := range ctx.GetDirectDepsProxyWithTag(licensesTag) {
		if l, ok := OtherModuleProvider(ctx, module, LicenseInfoProvider); ok {
			licenses = append(licenses, ctx.OtherModuleName(module))
			if m.base().commonProperties.Effective_package_name == nil && l.PackageName != nil {
				m.base().commonProperties.Effective_package_name = l.PackageName
			}
			texts = append(texts, l.EffectiveLicenseText...)
			kinds = append(kinds, l.EffectiveLicenseKinds...)
			conditions = append(conditions, l.EffectiveLicenseConditions...)
		} else {
			propertyName := "licenses"
			primaryProperty := m.base().primaryLicensesProperty
			if primaryProperty != nil {
				propertyName = primaryProperty.getName()
			}
			ctx.ModuleErrorf("%s property %q is not a license module", propertyName, ctx.OtherModuleName(module))
		}
	}

	m.base().commonProperties.Effective_license_text = SortedUniqueNamedPaths(texts)
	m.base().commonProperties.Effective_license_kinds = SortedUniqueStrings(kinds)
	m.base().commonProperties.Effective_license_conditions = SortedUniqueStrings(conditions)

	// Make the license information available for other modules.
	licenseInfo := LicensesInfo{
		Licenses: licenses,
	}
	SetProvider(ctx, LicensesInfoProvider, licenseInfo)
}

// Update a property NamedPath array with a distinct union of its values and a list of new values.
func namePathProps(prop *NamedPaths, name *string, values ...Path) {
	if name == nil {
		for _, value := range values {
			*prop = append(*prop, NamedPath{value, ""})
		}
	} else {
		for _, value := range values {
			*prop = append(*prop, NamedPath{value, *name})
		}
	}
	*prop = SortedUniqueNamedPaths(*prop)
}

// Get the licenses property falling back to the package default.
func getLicenses(ctx BaseModuleContext, module Module) []string {
	if exemptFromRequiredApplicableLicensesProperty(module) {
		return nil
	}

	primaryProperty := module.base().primaryLicensesProperty
	if primaryProperty == nil {
		if !ctx.Config().IsEnvFalse("ANDROID_REQUIRE_LICENSES") {
			ctx.ModuleErrorf("module type %q must have an applicable licenses property", ctx.OtherModuleType(module))
		}
		return nil
	}

	licenses := primaryProperty.getStrings()
	if len(licenses) > 0 {
		s := make(map[string]bool)
		for _, l := range licenses {
			if _, ok := s[l]; ok {
				ctx.ModuleErrorf("duplicate %q %s", l, primaryProperty.getName())
			}
			s[l] = true
		}
		return licenses
	}

	dir := ctx.OtherModuleDir(module)

	moduleToApplicableLicenses := moduleToPackageDefaultLicensesMap(ctx.Config())
	value, ok := moduleToApplicableLicenses.Load(dir)
	var c licensesContainer
	if ok {
		c = value.(licensesContainer)
	} else {
		c = licensesContainer{}
	}
	return c.getLicenses()
}

// Returns whether a module is an allowed list of modules that do not have or need applicable licenses.
func exemptFromRequiredApplicableLicensesProperty(module Module) bool {
	switch reflect.TypeOf(module).String() {
	case "*android.licenseModule": // is a license, doesn't need one
	case "*android.licenseKindModule": // is a license, doesn't need one
	case "*android.genNoticeModule": // contains license texts as data
	case "*android.NamespaceModule": // just partitions things, doesn't add anything
	case "*android.soongConfigModuleTypeModule": // creates aliases for modules with licenses
	case "*android.soongConfigModuleTypeImport": // creates aliases for modules with licenses
	case "*android.soongConfigStringVariableDummyModule": // used for creating aliases
	case "*android.soongConfigBoolVariableDummyModule": // used for creating aliases
	default:
		return false
	}
	return true
}

// LicensesInfo contains information about licenses for a specific module.
type LicensesInfo struct {
	// The list of license modules this depends upon, either explicitly or through default package
	// configuration.
	Licenses []string
}

var LicensesInfoProvider = blueprint.NewProvider[LicensesInfo]()

func init() {
	RegisterMakeVarsProvider(pctx, licensesMakeVarsProvider)
}

func licensesMakeVarsProvider(ctx MakeVarsContext) {
	ctx.Strict("BUILD_LICENSE_METADATA",
		ctx.Config().HostToolPath(ctx, "build_license_metadata").String())
	ctx.Strict("COPY_LICENSE_METADATA",
		ctx.Config().HostToolPath(ctx, "copy_license_metadata").String())
	ctx.Strict("HTMLNOTICE", ctx.Config().HostToolPath(ctx, "htmlnotice").String())
	ctx.Strict("XMLNOTICE", ctx.Config().HostToolPath(ctx, "xmlnotice").String())
	ctx.Strict("TEXTNOTICE", ctx.Config().HostToolPath(ctx, "textnotice").String())
	ctx.Strict("COMPLIANCENOTICE_SHIPPEDLIBS", ctx.Config().HostToolPath(ctx, "compliancenotice_shippedlibs").String())
	ctx.Strict("COMPLIANCE_LISTSHARE", ctx.Config().HostToolPath(ctx, "compliance_listshare").String())
	ctx.Strict("COMPLIANCE_CHECKSHARE", ctx.Config().HostToolPath(ctx, "compliance_checkshare").String())
}
