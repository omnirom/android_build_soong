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
	"strings"

	"github.com/google/blueprint"
)

func init() {
	RegisterParallelSingletonType("logtags", LogtagsSingleton)
}

type LogtagsInfo struct {
	Logtags Paths
}

var LogtagsProviderKey = blueprint.NewProvider[*LogtagsInfo]()

func LogtagsSingleton() Singleton {
	return &logtagsSingleton{}
}

type logtagsSingleton struct{}

func MergedLogtagsPath(ctx PathContext) OutputPath {
	return PathForIntermediates(ctx, "all-event-log-tags.txt")
}

func (l *logtagsSingleton) GenerateBuildActions(ctx SingletonContext) {
	var allLogtags Paths
	ctx.VisitAllModuleProxies(func(module ModuleProxy) {
		if !OtherModulePointerProviderOrDefault(ctx, module, CommonModuleInfoProvider).ExportedToMake {
			return
		}
		if logtagsInfo, ok := OtherModuleProvider(ctx, module, LogtagsProviderKey); ok {
			allLogtags = append(allLogtags, logtagsInfo.Logtags...)
		}
	})
	allLogtags = SortedUniquePaths(allLogtags)
	filteredLogTags := make([]Path, 0, len(allLogtags))
	for _, p := range allLogtags {
		// Logic copied from make:
		// https://cs.android.com/android/platform/superproject/main/+/main:build/make/core/Makefile;l=987;drc=0585bb1bcf4c89065adaf709f48acc8b869fd3ce
		if !strings.HasPrefix(p.String(), "vendor/") && !strings.HasPrefix(p.String(), "device/") && !strings.HasPrefix(p.String(), "out/") {
			filteredLogTags = append(filteredLogTags, p)
		}
	}

	builder := NewRuleBuilder(pctx, ctx)
	builder.Command().
		BuiltTool("merge-event-log-tags").
		FlagWithOutput("-o ", MergedLogtagsPath(ctx)).
		Inputs(filteredLogTags)
	builder.Build("all-event-log-tags.txt", "merge logtags")
}
