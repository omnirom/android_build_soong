// Copyright (C) 2025 The Android Open Source Project
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
	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"

	"android/soong/android"
)

// hostInitVerifierPartitionsToCheck is a hardcoded list of partitions in the order they are passed
// to host_init_verifier.  The list is copied from build/make/core/tasks/host_init_verifier.mk.
var hostInitVerifierPartitionsToCheck = []string{"system", "system_ext", "vendor", "odm", "product"}

// partitionStagingPath returns the string path to the staging directory for a partition and the path
// to the output file that causes the staging directory to be built.  If the partition is optional
// and not being built it returns the directory inside the partition that contains the optional partition
// (e.g. system/product if product is not built).
func partitionStagingPath(ctx android.ModuleContext, partitions map[string]FilesystemInfo,
	partitionType string) (android.OutputPath, android.Path, bool) {

	fsInfo, ok := partitions[partitionType]

	if ok {
		return fsInfo.RebasedDir, fsInfo.Output, true
	}

	switch partitionType {
	case "odm":
		if vendorFsInfo, ok := partitions["vendor"]; ok {
			partitionStagingDir := vendorFsInfo.RebasedDir.Join(ctx, partitionType)
			return partitionStagingDir, vendorFsInfo.Output, true
		}
	case "product", "system_ext":
		if systemFsInfo, ok := partitions["system"]; ok {
			partitionStagingDir := systemFsInfo.RebasedDir.Join(ctx, partitionType)
			return partitionStagingDir, systemFsInfo.Output, true
		}
	}

	return android.OutputPath{}, nil, false
}

func propertyContextsModuleName(partitionType string) string {
	if partitionType == "system" {
		return "plat_property_contexts"
	} else {
		return partitionType + "_property_contexts"
	}
}

func passwdFileModuleName(partitionType string) string {
	return "passwd_" + partitionType
}

type passwdFileDepTagType struct{ blueprint.BaseDependencyTag }
type propertyContextsDepTagType struct{ blueprint.BaseDependencyTag }

var passwdFileDepTag passwdFileDepTagType
var propertyContextsDepTag propertyContextsDepTagType

func (a *androidDevice) hostInitVerifierCheckDepsMutator(ctx android.BottomUpMutatorContext) {
	for _, typ := range hostInitVerifierPartitionsToCheck {
		firstVariations := ctx.Config().AndroidFirstDeviceTarget.Variations()
		ctx.AddVariationDependencies(firstVariations, passwdFileDepTag, passwdFileModuleName(typ))
		ctx.AddVariationDependencies(firstVariations, propertyContextsDepTag, propertyContextsModuleName(typ))
	}
}
func (a *androidDevice) hostInitVerifierCheck(ctx android.ModuleContext) {
	fsInfoMap := a.getFsInfos(ctx)

	rule := android.NewRuleBuilder(pctx, ctx)
	cmd := rule.Command().BuiltTool("host_init_verifier")

	outputFile := android.PathForModuleOut(ctx, "host_init_verifier_output.txt")

	for _, typ := range hostInitVerifierPartitionsToCheck {
		partitionStagingDir, partitionOutput, ok := partitionStagingPath(ctx, fsInfoMap, typ)
		if ok {
			cmd.FlagWithArg("--out_"+typ+" ", partitionStagingDir.String())
			cmd.Implicit(partitionOutput)
		}
	}

	for _, passwdModule := range ctx.GetDirectDepsProxyWithTag(passwdFileDepTag) {
		passwdFile := android.OutputFileForModule(ctx, passwdModule, "")
		cmd.FlagWithInput("-p ", passwdFile)
	}

	for _, propertyContextsModule := range ctx.GetDirectDepsProxyWithTag(propertyContextsDepTag) {
		propertyContextsFile := android.OutputFileForModule(ctx, propertyContextsModule, "")
		cmd.FlagWithInput("--property-contexts=", propertyContextsFile)
	}

	cmd.Text(" > ").Output(outputFile)

	rule.Build("host_init_verifier_check", "host_init_verifier check")

	if !ctx.Config().KatiEnabled() && proptools.Bool(a.deviceProps.Main_device) {
		// Only create the phony and dist target for Soong-only builds.
		ctx.Phony("host_init_verifier_check", outputFile)
		ctx.DistForGoal("droidcore-unbundled", outputFile)
	}
}
