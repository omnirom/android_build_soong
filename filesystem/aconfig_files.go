// Copyright (C) 2024 The Android Open Source Project
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
	"android/soong/android"

	"github.com/google/blueprint/proptools"
)

func (f *filesystem) buildAconfigFlagsFiles(ctx android.ModuleContext, builder *android.RuleBuilder, specs map[string]android.PackagingSpec, dir android.OutputPath) {
	if !proptools.Bool(f.properties.Gen_aconfig_flags_pb) {
		return
	}

	var caches []android.Path
	for _, ps := range specs {
		caches = append(caches, ps.GetAconfigPaths()...)
	}
	caches = android.SortedUniquePaths(caches)

	installAconfigFlagsPath := dir.Join(ctx, "etc", "aconfig_flags.pb")
	cmd := builder.Command().
		BuiltTool("aconfig").
		Text(" dump-cache --dedup --format protobuf --out").
		Output(installAconfigFlagsPath).
		Textf("--filter container:%s", f.PartitionType())
	for _, cache := range caches {
		cmd.FlagWithInput("--cache ", cache)
	}
	f.appendToEntry(ctx, installAconfigFlagsPath)

	installAconfigStorageDir := dir.Join(ctx, "etc", "aconfig")
	builder.Command().Text("mkdir -p").Text(installAconfigStorageDir.String())

	generatePartitionAconfigStorageFile := func(fileType, fileName string) {
		outputPath := installAconfigStorageDir.Join(ctx, fileName)
		builder.Command().
			BuiltTool("aconfig").
			FlagWithArg("create-storage --container ", f.PartitionType()).
			FlagWithArg("--file ", fileType).
			FlagWithOutput("--out ", outputPath).
			FlagWithArg("--cache ", installAconfigFlagsPath.String())
		f.appendToEntry(ctx, outputPath)
	}

	if ctx.Config().ReleaseCreateAconfigStorageFile() {
		generatePartitionAconfigStorageFile("package_map", "package.map")
		generatePartitionAconfigStorageFile("flag_map", "flag.map")
		generatePartitionAconfigStorageFile("flag_val", "flag.val")
		generatePartitionAconfigStorageFile("flag_info", "flag.info")
	}
}
