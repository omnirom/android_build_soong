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
)

func init() {
	RegisterParallelSingletonType("product_packages_file_singleton", productPackagesFileSingletonFactory)
}

func productPackagesFileSingletonFactory() Singleton {
	return &productPackagesFileSingleton{}
}

type productPackagesFileSingleton struct{}

func (s *productPackagesFileSingleton) GenerateBuildActions(ctx SingletonContext) {
	// There's no HasDeviceName() function, but the device name and device product should always
	// both be present or not.
	if ctx.Config().HasDeviceProduct() {
		productPackages := ctx.Config().productVariables.PartitionVarsForSoongMigrationOnlyDoNotUse.ProductPackages
		output := PathForArbitraryOutput(ctx, "target", "product", ctx.Config().DeviceName(), "product_packages.txt")
		WriteFileRule(ctx, output, strings.Join(productPackages, "\n"))
	}
}
