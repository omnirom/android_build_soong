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

import (
	"fmt"
	"strings"

	"github.com/google/blueprint/proptools"
)

const (
	deviceCmType          = "device_cm"
	systemManifestType    = "system_manifest"
	productManifestType   = "product_manifest"
	systemExtManifestType = "system_ext_manifest"
	vendorManifestType    = "vendor_manifest"
	odmManifestType       = "odm_manifest"

	defaultDcm               = "system/libhidl/vintfdata/device_compatibility_matrix.default.xml"
	defaultSystemManifest    = "system/libhidl/vintfdata/manifest.xml"
	defaultSystemExtManifest = "system/libhidl/vintfdata/system_ext_manifest.default.xml"
)

type vintfDataProperties struct {
	// Optional name for the installed file. If unspecified it will be manifest.xml by default.
	Filename *string

	// Type of the vintf data type, the allowed type are device_compatibility_matrix, system_manifest,
	// product_manifest, and system_ext_manifest.
	Type *string
}

type vintfDataRule struct {
	ModuleBase

	properties vintfDataProperties

	installDirPath InstallPath
	outputFilePath Path
	noAction       bool
}

func init() {
	registerVintfDataComponents(InitRegistrationContext)
}

func registerVintfDataComponents(ctx RegistrationContext) {
	ctx.RegisterModuleType("vintf_data", vintfDataFactory)
}

// vintf_fragment module processes vintf fragment file and installs under etc/vintf/manifest.
func vintfDataFactory() Module {
	m := &vintfDataRule{}
	m.AddProperties(
		&m.properties,
	)
	InitAndroidArchModule(m, DeviceSupported, MultilibFirst)

	return m
}

func (m *vintfDataRule) GenerateAndroidBuildActions(ctx ModuleContext) {
	builder := NewRuleBuilder(pctx, ctx)
	gensrc := PathForModuleOut(ctx, "manifest.xml")
	assembleVintfEnvs := []string{}
	inputPaths := make(Paths, 0)

	switch proptools.String(m.properties.Type) {
	case deviceCmType:
		assembleVintfEnvs = append(assembleVintfEnvs, fmt.Sprintf("BOARD_SYSTEMSDK_VERSIONS=\"%s\"", strings.Join(ctx.DeviceConfig().SystemSdkVersions(), " ")))

		deviceMatrixs := PathsForSource(ctx, ctx.Config().DeviceMatrixFile())
		if len(deviceMatrixs) > 0 {
			inputPaths = append(inputPaths, deviceMatrixs...)
		} else {
			inputPaths = append(inputPaths, PathForSource(ctx, defaultDcm))
		}
	case systemManifestType:
		assembleVintfEnvs = append(assembleVintfEnvs, fmt.Sprintf("PLATFORM_SYSTEMSDK_VERSIONS=\"%s\"", strings.Join(ctx.DeviceConfig().PlatformSystemSdkVersions(), " ")))

		inputPaths = append(inputPaths, PathForSource(ctx, defaultSystemManifest))
		systemManifestFiles := PathsForSource(ctx, ctx.Config().SystemManifestFile())
		if len(systemManifestFiles) > 0 {
			inputPaths = append(inputPaths, systemManifestFiles...)
		}
	case productManifestType:
		productManifestFiles := PathsForSource(ctx, ctx.Config().ProductManifestFiles())
		// Only need to generate the manifest if PRODUCT_MANIFEST_FILES not defined.
		if len(productManifestFiles) == 0 {
			m.noAction = true
			return
		}

		inputPaths = append(inputPaths, productManifestFiles...)
	case systemExtManifestType:
		assembleVintfEnvs = append(assembleVintfEnvs, fmt.Sprintf("PROVIDED_VNDK_VERSIONS=\"%s\"", strings.Join(ctx.DeviceConfig().ExtraVndkVersions(), " ")))

		inputPaths = append(inputPaths, PathForSource(ctx, defaultSystemExtManifest))
		systemExtManifestFiles := PathsForSource(ctx, ctx.Config().SystemExtManifestFiles())
		if len(systemExtManifestFiles) > 0 {
			inputPaths = append(inputPaths, systemExtManifestFiles...)
		}
	case vendorManifestType:
		assembleVintfEnvs = append(assembleVintfEnvs, fmt.Sprintf("BOARD_SEPOLICY_VERS=\"%s\"", ctx.DeviceConfig().BoardSepolicyVers()))
		assembleVintfEnvs = append(assembleVintfEnvs, fmt.Sprintf("PRODUCT_ENFORCE_VINTF_MANIFEST=%t", *ctx.Config().productVariables.Enforce_vintf_manifest))
		deviceManifestFiles := PathsForSource(ctx, ctx.Config().DeviceManifestFiles())
		// Only need to generate the manifest if DEVICE_MANIFEST_FILE is defined.
		if len(deviceManifestFiles) == 0 {
			m.noAction = true
			return
		}

		inputPaths = append(inputPaths, deviceManifestFiles...)
	case odmManifestType:
		assembleVintfEnvs = append(assembleVintfEnvs, "VINTF_IGNORE_TARGET_FCM_VERSION=true")
		odmManifestFiles := PathsForSource(ctx, ctx.Config().OdmManifestFiles())
		// Only need to generate the manifest if ODM_MANIFEST_FILES is defined.
		if len(odmManifestFiles) == 0 {
			m.noAction = true
			return
		}

		inputPaths = append(inputPaths, odmManifestFiles...)
	default:
		panic(fmt.Errorf("For %s: The attribute 'type' value only allowed device_cm, system_manifest, product_manifest, system_ext_manifest!", ctx.Module().Name()))
	}

	// Process vintf fragment source file with assemble_vintf tool
	builder.Command().
		Implicits(inputPaths).
		Flags(assembleVintfEnvs).
		BuiltTool("assemble_vintf").
		FlagWithArg("-i ", strings.Join(inputPaths.Strings(), ":")).
		FlagWithOutput("-o ", gensrc)

	builder.Build("assemble_vintf", "Process vintf data "+gensrc.String())

	m.installDirPath = PathForModuleInstall(ctx, "etc", "vintf")
	m.outputFilePath = gensrc

	installFileName := "manifest.xml"
	if filename := proptools.String(m.properties.Filename); filename != "" {
		installFileName = filename
	}

	ctx.InstallFile(m.installDirPath, installFileName, gensrc)
}

// Make this module visible to AndroidMK so it can be referenced from modules defined from Android.mk files
func (m *vintfDataRule) AndroidMkEntries() []AndroidMkEntries {
	if m.noAction {
		return []AndroidMkEntries{}
	}

	return []AndroidMkEntries{{
		Class:      "ETC",
		OutputFile: OptionalPathForPath(m.outputFilePath),
	}}
}
