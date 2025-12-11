// Copyright 2015 Google Inc. All rights reserved.
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
	"fmt"
	"slices"
	"strings"

	"android/soong/android"

	"github.com/google/blueprint"
)

func init() {
	android.InitRegistrationContext.RegisterParallelSingletonType("apkcerts_singleton", apkCertsSingletonFactory)
}

// Info that should be included into the apkcerts.txt file.
// The info can be provided as either a text file containing a subset of the final apkcerts.txt,
// or as a certificate and name. The text file will be preferred if it exists
type ApkCertInfo struct {
	ApkCertsFile android.Path

	Certificate Certificate
	Name        string

	// True if LOCAL_MODULE_TAGS would contain "tests" in a make build.
	// In make this caused the partition in the apkcerts.txt file to be "data" instead of "system"
	Test bool
}

var ApkCertInfoProvider = blueprint.NewProvider[ApkCertInfo]()

type ApkCertsInfo []ApkCertInfo

var ApkCertsInfoProvider = blueprint.NewProvider[ApkCertsInfo]()

func apkCertsSingletonFactory() android.Singleton {
	return &apkCertsSingleton{}
}

type apkCertsSingleton struct{}

func (a *apkCertsSingleton) GenerateBuildActions(ctx android.SingletonContext) {
	apkCerts := []string{}
	var apkCertsFiles android.Paths
	ctx.VisitAllModuleProxies(func(m android.ModuleProxy) {
		commonInfo, ok := android.OtherModuleProvider(ctx, m, android.CommonModuleInfoProvider)
		if !ok || commonInfo.SkipAndroidMkProcessing {
			return
		}

		partition := commonInfo.PartitionTag

		specifiesPartition := commonInfo.SocSpecific || commonInfo.Vendor ||
			commonInfo.Proprietary || commonInfo.SystemExtSpecific || commonInfo.ProductSpecific ||
			commonInfo.DeviceSpecific

		if info, ok := android.OtherModuleProvider(ctx, m, ApkCertsInfoProvider); ok {
			for _, certInfo := range info {
				if certInfo.ApkCertsFile != nil {
					apkCertsFiles = append(apkCertsFiles, certInfo.ApkCertsFile)
				} else {
					// Partition information of apk-in-apex is not exported to the legacy Make packaging system.
					// Hardcode the partition to "system"
					apkCerts = append(apkCerts, FormatApkCertsLine(certInfo.Certificate, certInfo.Name, "system"))
				}
			}
		} else if info, ok := android.OtherModuleProvider(ctx, m, ApkCertInfoProvider); ok {
			if info.ApkCertsFile != nil {
				apkCertsFiles = append(apkCertsFiles, info.ApkCertsFile)
			} else {
				// From base_rules.mk
				if info.Test && partition == "system" && !specifiesPartition {
					partition = "data"
				}

				apkCerts = append(apkCerts, FormatApkCertsLine(info.Certificate, info.Name, partition))
			}
		}
	})
	slices.Sort(apkCerts) // sort by name
	apkCertsInfoWithoutAppSets := android.PathForOutput(ctx, "apkcerts_singleton", "apkcerts_without_app_sets.txt")
	android.WriteFileRule(ctx, apkCertsInfoWithoutAppSets, strings.Join(apkCerts, "\n"))
	apkCertsInfo := ApkCertsFile(ctx)
	ctx.Build(pctx, android.BuildParams{
		Rule:        android.CatAndSortAndUnique,
		Description: "combine apkcerts.txt",
		Output:      apkCertsInfo,
		Inputs:      append(apkCertsFiles, apkCertsInfoWithoutAppSets),
	})
}

func (s *apkCertsSingleton) MakeVars(ctx android.MakeVarsContext) {
	ctx.Strict("SOONG_APKCERTS_FILE", ApkCertsFile(ctx).String())
}

func ApkCertsFile(ctx android.PathContext) android.WritablePath {
	return android.PathForOutput(ctx, "apkcerts_singleton", "apkcerts.txt")
}

func FormatApkCertsLine(cert Certificate, name, partition string) string {
	pem := cert.AndroidMkString()
	var key string
	if cert.Key == nil {
		key = ""
	} else {
		key = cert.Key.String()
	}
	return fmt.Sprintf(`name="%s" certificate="%s" private_key="%s" partition="%s"`, name, pem, key, partition)
}
