// Copyright (C) 2021 The Android Open Source Project
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

package filesystem

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"

	"android/soong/android"
)

func init() {
	android.RegisterModuleType("bootimg", BootimgFactory)
}

type bootimg struct {
	android.ModuleBase

	properties BootimgProperties

	output     android.Path
	installDir android.InstallPath

	bootImageType bootImageType
}

type BootimgProperties struct {
	// Set the name of the output. Defaults to <module_name>.img.
	Stem *string

	// Path to the linux kernel prebuilt file
	Kernel_prebuilt *string `android:"arch_variant,path"`

	// Filesystem module that is used as ramdisk
	Ramdisk_module *string

	// Path to the device tree blob (DTB) prebuilt file to add to this boot image
	Dtb_prebuilt *string `android:"arch_variant,path"`

	// Header version number. Must be set to one of the version numbers that are currently
	// supported. Refer to
	// https://source.android.com/devices/bootloader/boot-image-header
	Header_version *string

	// Determines the specific type of boot image this module is building. Can be boot,
	// vendor_boot or init_boot. Defaults to boot.
	// Refer to https://source.android.com/devices/bootloader/partitions/vendor-boot-partitions
	// for vendor_boot.
	// Refer to https://source.android.com/docs/core/architecture/partitions/generic-boot for
	// init_boot.
	Boot_image_type *string

	// Optional kernel commandline arguments
	Cmdline []string `android:"arch_variant"`

	// File that contains bootconfig parameters. This can be set only when `vendor_boot` is true
	// and `header_version` is greater than or equal to 4.
	Bootconfig *string `android:"arch_variant,path"`

	// The size of the partition on the device. It will be a build error if this built partition
	// image exceeds this size.
	Partition_size *int64

	// When set to true, sign the image with avbtool. Default is false.
	Use_avb *bool

	// This can either be "default", or "make_legacy". "make_legacy" will sign the boot image
	// like how build/make/core/Makefile does, to get bit-for-bit backwards compatibility. But
	// we may want to reconsider if it's necessary to have two modes in the future. The default
	// is "default"
	Avb_mode *string

	// Name of the partition stored in vbmeta desc. Defaults to the name of this module.
	Partition_name *string

	// Path to the private key that avbtool will use to sign this filesystem image.
	// TODO(jiyong): allow apex_key to be specified here
	Avb_private_key *string `android:"path_device_first"`

	// Hash and signing algorithm for avbtool. Default is SHA256_RSA4096.
	Avb_algorithm *string

	// The index used to prevent rollback of the image on device.
	Avb_rollback_index *int64

	// Rollback index location of this image. Must be 0, 1, 2, etc.
	Avb_rollback_index_location *int64

	// The security patch passed to as the com.android.build.<type>.security_patch avb property.
	// Replacement for the make variables BOOT_SECURITY_PATCH / INIT_BOOT_SECURITY_PATCH.
	Security_patch *string
}

type bootImageType int

const (
	unsupported bootImageType = iota
	boot
	vendorBoot
	initBoot
)

func toBootImageType(ctx android.ModuleContext, bootImageType string) bootImageType {
	switch bootImageType {
	case "boot":
		return boot
	case "vendor_boot":
		return vendorBoot
	case "init_boot":
		return initBoot
	default:
		ctx.ModuleErrorf("Unknown boot_image_type %s. Must be one of \"boot\", \"vendor_boot\", or \"init_boot\"", bootImageType)
	}
	return unsupported
}

func (b bootImageType) String() string {
	switch b {
	case boot:
		return "boot"
	case vendorBoot:
		return "vendor_boot"
	case initBoot:
		return "init_boot"
	default:
		panic("unknown boot image type")
	}
}

func (b bootImageType) isBoot() bool {
	return b == boot
}

func (b bootImageType) isVendorBoot() bool {
	return b == vendorBoot
}

func (b bootImageType) isInitBoot() bool {
	return b == initBoot
}

// bootimg is the image for the boot partition. It consists of header, kernel, ramdisk, and dtb.
func BootimgFactory() android.Module {
	module := &bootimg{}
	module.AddProperties(&module.properties)
	android.InitAndroidArchModule(module, android.DeviceSupported, android.MultilibFirst)
	return module
}

type bootimgDep struct {
	blueprint.BaseDependencyTag
	kind string
}

var bootimgRamdiskDep = bootimgDep{kind: "ramdisk"}

func (b *bootimg) DepsMutator(ctx android.BottomUpMutatorContext) {
	ramdisk := proptools.String(b.properties.Ramdisk_module)
	if ramdisk != "" {
		ctx.AddDependency(ctx.Module(), bootimgRamdiskDep, ramdisk)
	}
}

func (b *bootimg) installFileName() string {
	return proptools.StringDefault(b.properties.Stem, b.BaseModuleName()+".img")
}

func (b *bootimg) partitionName() string {
	return proptools.StringDefault(b.properties.Partition_name, b.BaseModuleName())
}

func (b *bootimg) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	b.bootImageType = toBootImageType(ctx, proptools.StringDefault(b.properties.Boot_image_type, "boot"))
	if b.bootImageType == unsupported {
		return
	}

	kernelProp := proptools.String(b.properties.Kernel_prebuilt)
	if b.bootImageType.isVendorBoot() && kernelProp != "" {
		ctx.PropertyErrorf("kernel_prebuilt", "vendor_boot partition can't have kernel")
		return
	}
	if b.bootImageType.isBoot() && kernelProp == "" {
		ctx.PropertyErrorf("kernel_prebuilt", "boot partition must have kernel")
		return
	}

	kernelPath := b.getKernelPath(ctx)
	unsignedOutput := b.buildBootImage(ctx, kernelPath)

	output := unsignedOutput
	if proptools.Bool(b.properties.Use_avb) {
		// This bootimg module supports 2 modes of avb signing. It is not clear to this author
		// why there are differences, but one of them is to match the behavior of make-built boot
		// images.
		switch proptools.StringDefault(b.properties.Avb_mode, "default") {
		case "default":
			output = b.signImage(ctx, unsignedOutput)
		case "make_legacy":
			output = b.addAvbFooter(ctx, unsignedOutput, kernelPath)
		default:
			ctx.PropertyErrorf("avb_mode", `Unknown value for avb_mode, expected "default" or "make_legacy", got: %q`, *b.properties.Avb_mode)
		}
	}

	b.installDir = android.PathForModuleInstall(ctx, "etc")
	ctx.InstallFile(b.installDir, b.installFileName(), output)

	ctx.SetOutputFiles([]android.Path{output}, "")
	b.output = output

	// Set the Filesystem info of the ramdisk dependency.
	// `android_device` will use this info to package `target_files.zip`
	if ramdisk := proptools.String(b.properties.Ramdisk_module); ramdisk != "" {
		ramdiskModule := ctx.GetDirectDepWithTag(ramdisk, bootimgRamdiskDep)
		fsInfo, _ := android.OtherModuleProvider(ctx, ramdiskModule, FilesystemProvider)
		android.SetProvider(ctx, FilesystemProvider, fsInfo)
	} else {
		setCommonFilesystemInfo(ctx, b)
	}

	// Set BootimgInfo for building target_files.zip
	dtbPath := b.getDtbPath(ctx)
	android.SetProvider(ctx, BootimgInfoProvider, BootimgInfo{
		Cmdline:             b.properties.Cmdline,
		Kernel:              kernelPath,
		Dtb:                 dtbPath,
		Bootconfig:          b.getBootconfigPath(ctx),
		Output:              output,
		PropFileForMiscInfo: b.buildPropFileForMiscInfo(ctx),
		HeaderVersion:       proptools.String(b.properties.Header_version),
	})

	extractedPublicKey := android.PathForModuleOut(ctx, b.partitionName()+".avbpubkey")
	if b.properties.Avb_private_key != nil {
		key := android.PathForModuleSrc(ctx, proptools.String(b.properties.Avb_private_key))
		ctx.Build(pctx, android.BuildParams{
			Rule:   extractPublicKeyRule,
			Input:  key,
			Output: extractedPublicKey,
		})
	}
	var ril int
	if b.properties.Avb_rollback_index_location != nil {
		ril = proptools.Int(b.properties.Avb_rollback_index_location)
	}

	android.SetProvider(ctx, vbmetaPartitionProvider, vbmetaPartitionInfo{
		Name:                  b.bootImageType.String(),
		RollbackIndexLocation: ril,
		PublicKey:             extractedPublicKey,
		Output:                output,
	})

	// Dump compliance metadata
	complianceMetadataInfo := ctx.ComplianceMetadataInfo()
	prebuiltFilesCopied := make([]string, 0)
	if kernelPath != nil {
		prebuiltFilesCopied = append(prebuiltFilesCopied, kernelPath.String()+":kernel")
	}
	if dtbPath != nil {
		prebuiltFilesCopied = append(prebuiltFilesCopied, dtbPath.String()+":dtb.img")
	}
	complianceMetadataInfo.SetPrebuiltFilesCopied(prebuiltFilesCopied)

	if ramdisk := proptools.String(b.properties.Ramdisk_module); ramdisk != "" {
		buildComplianceMetadata(ctx, bootimgRamdiskDep)
	}
}

var BootimgInfoProvider = blueprint.NewProvider[BootimgInfo]()

type BootimgInfo struct {
	Cmdline             []string
	Kernel              android.Path
	Dtb                 android.Path
	Bootconfig          android.Path
	Output              android.Path
	PropFileForMiscInfo android.Path
	HeaderVersion       string
}

func (b *bootimg) getKernelPath(ctx android.ModuleContext) android.Path {
	var kernelPath android.Path
	kernelName := proptools.String(b.properties.Kernel_prebuilt)
	if kernelName != "" {
		kernelPath = android.PathForModuleSrc(ctx, kernelName)
	}
	return kernelPath
}

func (b *bootimg) getDtbPath(ctx android.ModuleContext) android.Path {
	var dtbPath android.Path
	dtbName := proptools.String(b.properties.Dtb_prebuilt)
	if dtbName != "" {
		dtbPath = android.PathForModuleSrc(ctx, dtbName)
	}
	return dtbPath
}

func (b *bootimg) getBootconfigPath(ctx android.ModuleContext) android.Path {
	var bootconfigPath android.Path
	bootconfigName := proptools.String(b.properties.Bootconfig)
	if bootconfigName != "" {
		bootconfigPath = android.PathForModuleSrc(ctx, bootconfigName)
	}
	return bootconfigPath
}

func (b *bootimg) buildBootImage(ctx android.ModuleContext, kernel android.Path) android.Path {
	output := android.PathForModuleOut(ctx, "unsigned", b.installFileName())

	builder := android.NewRuleBuilder(pctx, ctx)
	cmd := builder.Command().BuiltTool("mkbootimg")

	if kernel != nil {
		cmd.FlagWithInput("--kernel ", kernel)
	}

	// These arguments are passed for boot.img and init_boot.img generation
	if b.bootImageType.isBoot() || b.bootImageType.isInitBoot() {
		cmd.FlagWithArg("--os_version ", ctx.Config().PlatformVersionLastStable())
		cmd.FlagWithArg("--os_patch_level ", ctx.Config().PlatformSecurityPatch())
	}

	if b.getDtbPath(ctx) != nil {
		cmd.FlagWithInput("--dtb ", b.getDtbPath(ctx))
	}

	cmdline := strings.Join(b.properties.Cmdline, " ")
	if cmdline != "" {
		flag := "--cmdline "
		if b.bootImageType.isVendorBoot() {
			flag = "--vendor_cmdline "
		}
		cmd.FlagWithArg(flag, proptools.ShellEscapeIncludingSpaces(cmdline))
	}

	headerVersion := proptools.String(b.properties.Header_version)
	if headerVersion == "" {
		ctx.PropertyErrorf("header_version", "must be set")
		return output
	}
	verNum, err := strconv.Atoi(headerVersion)
	if err != nil {
		ctx.PropertyErrorf("header_version", "%q is not a number", headerVersion)
		return output
	}
	if verNum < 3 {
		ctx.PropertyErrorf("header_version", "must be 3 or higher for vendor_boot")
		return output
	}
	cmd.FlagWithArg("--header_version ", headerVersion)

	ramdiskName := proptools.String(b.properties.Ramdisk_module)
	if ramdiskName != "" {
		ramdisk := ctx.GetDirectDepWithTag(ramdiskName, bootimgRamdiskDep)
		if filesystem, ok := ramdisk.(*filesystem); ok {
			flag := "--ramdisk "
			if b.bootImageType.isVendorBoot() {
				flag = "--vendor_ramdisk "
			}
			cmd.FlagWithInput(flag, filesystem.OutputPath())
		} else {
			ctx.PropertyErrorf("ramdisk", "%q is not android_filesystem module", ramdisk.Name())
			return output
		}
	}

	bootconfig := proptools.String(b.properties.Bootconfig)
	if bootconfig != "" {
		if !b.bootImageType.isVendorBoot() {
			ctx.PropertyErrorf("bootconfig", "requires vendor_boot: true")
			return output
		}
		if verNum < 4 {
			ctx.PropertyErrorf("bootconfig", "requires header_version: 4 or later")
			return output
		}
		cmd.FlagWithInput("--vendor_bootconfig ", android.PathForModuleSrc(ctx, bootconfig))
	}

	// Output flag for boot.img and init_boot.img
	flag := "--output "
	if b.bootImageType.isVendorBoot() {
		flag = "--vendor_boot "
	}
	cmd.FlagWithOutput(flag, output)

	if b.properties.Partition_size != nil {
		assertMaxImageSize(builder, output, *b.properties.Partition_size, proptools.Bool(b.properties.Use_avb))
	}

	builder.Build("build_bootimg", fmt.Sprintf("Creating %s", b.BaseModuleName()))
	return output
}

func (b *bootimg) addAvbFooter(ctx android.ModuleContext, unsignedImage android.Path, kernel android.Path) android.Path {
	output := android.PathForModuleOut(ctx, b.installFileName())
	builder := android.NewRuleBuilder(pctx, ctx)
	builder.Command().Text("cp").Input(unsignedImage).Output(output)
	cmd := builder.Command().BuiltTool("avbtool").
		Text("add_hash_footer").
		FlagWithInput("--image ", output)

	if b.properties.Partition_size != nil {
		cmd.FlagWithArg("--partition_size ", strconv.FormatInt(*b.properties.Partition_size, 10))
	} else {
		cmd.Flag("--dynamic_partition_size")
	}

	// If you don't provide a salt, avbtool will use random bytes for the salt.
	// This is bad for determinism (cached builds and diff tests are affected), so instead,
	// we try to provide a salt. The requirements for a salt are not very clear, one aspect of it
	// is that if it's unpredictable, attackers trying to change the contents of a partition need
	// to find a new hash collision every release, because the salt changed.
	if kernel != nil {
		cmd.Textf(`--salt $(sha256sum "%s" | cut -d " " -f 1)`, kernel.String())
		cmd.Implicit(kernel)
	} else {
		cmd.Textf(`--salt $(sha256sum "%s" "%s" | cut -d " " -f 1 | tr -d '\n')`, ctx.Config().BuildNumberFile(ctx), ctx.Config().Getenv("BUILD_DATETIME_FILE"))
		cmd.OrderOnly(ctx.Config().BuildNumberFile(ctx))
	}

	cmd.FlagWithArg("--partition_name ", b.bootImageType.String())

	if b.properties.Avb_algorithm != nil {
		cmd.FlagWithArg("--algorithm ", proptools.NinjaAndShellEscape(*b.properties.Avb_algorithm))
	}

	if b.properties.Avb_private_key != nil {
		key := android.PathForModuleSrc(ctx, proptools.String(b.properties.Avb_private_key))
		cmd.FlagWithInput("--key ", key)
	}

	if !b.bootImageType.isVendorBoot() {
		cmd.FlagWithArg("--prop ", proptools.NinjaAndShellEscape(fmt.Sprintf(
			"com.android.build.%s.os_version:%s", b.bootImageType.String(), ctx.Config().PlatformVersionLastStable())))
	}

	fingerprintFile := ctx.Config().BuildFingerprintFile(ctx)
	cmd.FlagWithArg("--prop ", fmt.Sprintf("com.android.build.%s.fingerprint:$(cat %s)", b.bootImageType.String(), fingerprintFile.String()))
	cmd.OrderOnly(fingerprintFile)

	if b.properties.Security_patch != nil {
		cmd.FlagWithArg("--prop ", proptools.NinjaAndShellEscape(fmt.Sprintf(
			"com.android.build.%s.security_patch:%s", b.bootImageType.String(), *b.properties.Security_patch)))
	}

	if b.properties.Avb_rollback_index != nil {
		cmd.FlagWithArg("--rollback_index ", strconv.FormatInt(*b.properties.Avb_rollback_index, 10))
	}

	builder.Build("add_avb_footer", fmt.Sprintf("Adding avb footer to %s", b.BaseModuleName()))
	return output
}

func (b *bootimg) signImage(ctx android.ModuleContext, unsignedImage android.Path) android.Path {
	propFile, toolDeps := b.buildPropFile(ctx)

	output := android.PathForModuleOut(ctx, b.installFileName())
	builder := android.NewRuleBuilder(pctx, ctx)
	builder.Command().Text("cp").Input(unsignedImage).Output(output)
	builder.Command().BuiltTool("verity_utils").
		Input(propFile).
		Implicits(toolDeps).
		Output(output)

	builder.Build("sign_bootimg", fmt.Sprintf("Signing %s", b.BaseModuleName()))
	return output
}

func (b *bootimg) buildPropFile(ctx android.ModuleContext) (android.Path, android.Paths) {
	var sb strings.Builder
	var deps android.Paths
	addStr := func(name string, value string) {
		fmt.Fprintf(&sb, "%s=%s\n", name, value)
	}
	addPath := func(name string, path android.Path) {
		addStr(name, path.String())
		deps = append(deps, path)
	}

	addStr("avb_hash_enable", "true")
	addPath("avb_avbtool", ctx.Config().HostToolPath(ctx, "avbtool"))
	algorithm := proptools.StringDefault(b.properties.Avb_algorithm, "SHA256_RSA4096")
	addStr("avb_algorithm", algorithm)
	key := android.PathForModuleSrc(ctx, proptools.String(b.properties.Avb_private_key))
	addPath("avb_key_path", key)
	addStr("avb_add_hash_footer_args", "") // TODO(jiyong): add --rollback_index
	partitionName := proptools.StringDefault(b.properties.Partition_name, b.Name())
	addStr("partition_name", partitionName)

	propFile := android.PathForModuleOut(ctx, "prop")
	android.WriteFileRule(ctx, propFile, sb.String())
	return propFile, deps
}

func (b *bootimg) getAvbHashFooterArgs(ctx android.ModuleContext) string {
	ret := ""
	if !b.bootImageType.isVendorBoot() {
		ret += "--prop " + fmt.Sprintf("com.android.build.%s.os_version:%s", b.bootImageType.String(), ctx.Config().PlatformVersionLastStable())
	}

	fingerprintFile := ctx.Config().BuildFingerprintFile(ctx)
	ret += " --prop " + fmt.Sprintf("com.android.build.%s.fingerprint:{CONTENTS_OF:%s}", b.bootImageType.String(), fingerprintFile.String())

	if b.properties.Security_patch != nil {
		ret += " --prop " + fmt.Sprintf("com.android.build.%s.security_patch:%s", b.bootImageType.String(), *b.properties.Security_patch)
	}

	if b.properties.Avb_rollback_index != nil {
		ret += " --rollback_index " + strconv.FormatInt(*b.properties.Avb_rollback_index, 10)
	}
	return strings.TrimSpace(ret)
}

func (b *bootimg) buildPropFileForMiscInfo(ctx android.ModuleContext) android.Path {
	var sb strings.Builder
	addStr := func(name string, value string) {
		fmt.Fprintf(&sb, "%s=%s\n", name, value)
	}

	bootImgType := proptools.String(b.properties.Boot_image_type)
	addStr("avb_"+bootImgType+"_add_hash_footer_args", b.getAvbHashFooterArgs(ctx))
	if ramdisk := proptools.String(b.properties.Ramdisk_module); ramdisk != "" {
		ramdiskModule := ctx.GetDirectDepWithTag(ramdisk, bootimgRamdiskDep)
		fsInfo, _ := android.OtherModuleProvider(ctx, ramdiskModule, FilesystemProvider)
		if fsInfo.HasOrIsRecovery {
			// Create a dup entry for recovery
			addStr("avb_recovery_add_hash_footer_args", strings.ReplaceAll(b.getAvbHashFooterArgs(ctx), bootImgType, "recovery"))
		}
	}
	if b.properties.Avb_private_key != nil {
		addStr("avb_"+bootImgType+"_algorithm", proptools.StringDefault(b.properties.Avb_algorithm, "SHA256_RSA4096"))
		addStr("avb_"+bootImgType+"_key_path", android.PathForModuleSrc(ctx, proptools.String(b.properties.Avb_private_key)).String())
		addStr("avb_"+bootImgType+"_rollback_index_location", strconv.Itoa(proptools.Int(b.properties.Avb_rollback_index_location)))
	}
	if b.properties.Partition_size != nil {
		addStr(bootImgType+"_size", strconv.FormatInt(*b.properties.Partition_size, 10))
	}
	if bootImgType != "boot" {
		addStr(bootImgType, "true")
	}

	propFilePreProcessing := android.PathForModuleOut(ctx, "prop_for_misc_info_pre_processing")
	android.WriteFileRuleVerbatim(ctx, propFilePreProcessing, sb.String())
	propFile := android.PathForModuleOut(ctx, "prop_file_for_misc_info")
	ctx.Build(pctx, android.BuildParams{
		Rule:   textFileProcessorRule,
		Input:  propFilePreProcessing,
		Output: propFile,
	})

	return propFile
}

var _ android.AndroidMkEntriesProvider = (*bootimg)(nil)

// Implements android.AndroidMkEntriesProvider
func (b *bootimg) AndroidMkEntries() []android.AndroidMkEntries {
	return []android.AndroidMkEntries{android.AndroidMkEntries{
		Class:      "ETC",
		OutputFile: android.OptionalPathForPath(b.output),
		ExtraEntries: []android.AndroidMkExtraEntriesFunc{
			func(ctx android.AndroidMkExtraEntriesContext, entries *android.AndroidMkEntries) {
				entries.SetString("LOCAL_MODULE_PATH", b.installDir.String())
				entries.SetString("LOCAL_INSTALLED_MODULE_STEM", b.installFileName())
			},
		},
	}}
}

var _ Filesystem = (*bootimg)(nil)

func (b *bootimg) OutputPath() android.Path {
	return b.output
}

func (b *bootimg) SignedOutputPath() android.Path {
	if proptools.Bool(b.properties.Use_avb) {
		return b.OutputPath()
	}
	return nil
}
