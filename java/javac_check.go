// Copyright 2025 Google Inc. All rights reserved.
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
	"android/soong/android"
)

func init() {
	android.InitRegistrationContext.RegisterParallelSingletonType("javac_singleton", javacSingletonFactory)
}

func javacSingletonFactory() android.Singleton {
	return &javacSingleton{}
}

type javacSingleton struct{}

func (j *javacSingleton) GenerateBuildActions(ctx android.SingletonContext) {
	buf := make([]android.Path, 0, 10)
	ctx.VisitAllModuleProxies(func(m android.ModuleProxy) {
		if javaInfo, ok := android.OtherModuleProvider(ctx, m, JavaInfoProvider); ok {
			commonInfo := android.OtherModulePointerProviderOrDefault(ctx, m, android.CommonModuleInfoProvider)
			// Historically javac-check was implemented in make, so maintain the behavior that
			// it only builds soong modules exported to make
			if commonInfo.SkipAndroidMkProcessing {
				return
			}
			files := buf[:0]
			files = append(files, javaInfo.ImplementationAndResourcesJars...)
			files = append(files, javaInfo.ImplementationJars...)
			ctx.Phony("javac-check", files...)
			ctx.Phony("javac-check-"+ctx.ModuleName(m), files...)
		}
	})
}
