// Copyright (C) 2024 The Android Open Source Project
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.arebapache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package filesystem

import (
	"testing"

	"android/soong/android"
)

var prepareForHostInitVerifierCheckTest = android.GroupFixturePreparers(
	android.PrepareForIntegrationTestWithAndroid,
	android.PrepareForTestWithAndroidBuildComponents,
	PrepareForTestWithFilesystemBuildComponents,
	prepareForTestWithAndroidDeviceComponents,
)

func TestHostInitVerifierCheck(t *testing.T) {
	result := android.GroupFixturePreparers(prepareForHostInitVerifierCheckTest).
		RunTestWithBp(t, `
			android_device {
				name: "test_device",
				system_partition_name: "system_image",
				product_partition_name: "product_image",
				vendor_partition_name: "vendor_image",
				odm_partition_name: "odm_image",
				system_ext_partition_name: "system_ext_image",
			}
		`)

	checkModule := result.ModuleForTests(t, "test_device", "android_arm64_armv8-a")
	rule := checkModule.Output("host_init_verifier_output.txt")
	cmd := rule.RuleParams.Command

	// Check that the command contains flags for all the partition images.
	android.AssertStringDoesContain(t, "command", cmd, "--out_system out/soong/.intermediates/images/system_image/android_common/system_image/system ")
	android.AssertStringDoesContain(t, "command", cmd, "--out_system_ext out/soong/.intermediates/images/system_ext_image/android_common/system_ext_image ")
	android.AssertStringDoesContain(t, "command", cmd, "--out_vendor out/soong/.intermediates/images/vendor_image/android_common/vendor_image ")
	android.AssertStringDoesContain(t, "command", cmd, "--out_product out/soong/.intermediates/images/product_image/android_common/product_image ")
	android.AssertStringDoesContain(t, "command", cmd, "--out_odm out/soong/.intermediates/images/odm_image/android_common/odm_image ")

	// Check that the command contains flags for all the passwd and property files.
	android.AssertStringDoesContain(t, "command", cmd, "-p out/soong/.intermediates/images/passwd_system/android_arm64_armv8-a/passwd_system")
	android.AssertStringDoesContain(t, "command", cmd, "--property-contexts=out/soong/.intermediates/images/plat_property_contexts/android_arm64_armv8-a/plat_property_contexts")

	android.AssertStringDoesContain(t, "command", cmd, "-p out/soong/.intermediates/images/passwd_system_ext/android_arm64_armv8-a/passwd_system_ext")
	android.AssertStringDoesContain(t, "command", cmd, "--property-contexts=out/soong/.intermediates/images/system_ext_property_contexts/android_arm64_armv8-a/system_ext_property_contexts")

	android.AssertStringDoesContain(t, "command", cmd, "-p out/soong/.intermediates/images/passwd_vendor/android_arm64_armv8-a/passwd_vendor")
	android.AssertStringDoesContain(t, "command", cmd, "--property-contexts=out/soong/.intermediates/images/vendor_property_contexts/android_arm64_armv8-a/vendor_property_contexts")

	android.AssertStringDoesContain(t, "command", cmd, "-p out/soong/.intermediates/images/passwd_odm/android_arm64_armv8-a/passwd_odm")
	android.AssertStringDoesContain(t, "command", cmd, "--property-contexts=out/soong/.intermediates/images/odm_property_contexts/android_arm64_armv8-a/odm_property_contexts")

	android.AssertStringDoesContain(t, "command", cmd, "-p out/soong/.intermediates/images/passwd_product/android_arm64_armv8-a/passwd_product")
	android.AssertStringDoesContain(t, "command", cmd, "--property-contexts=out/soong/.intermediates/images/product_property_contexts/android_arm64_armv8-a/product_property_contexts")
}

func TestHostInitVerifierCheck_MissingDeps(t *testing.T) {
	result := android.GroupFixturePreparers(
		prepareForHostInitVerifierCheckTest,
	).RunTestWithBp(t, `
		android_device {
			name: "test_device",
			vendor_partition_name: "vendor_image",
		}
	`)

	checkModule := result.ModuleForTests(t, "test_device", "android_arm64_armv8-a")
	rule := checkModule.Output("host_init_verifier_output.txt")
	cmd := rule.RuleParams.Command

	// Check that the command contains flags for only vendor and odm, with odm as a subdirectory of vendor.
	android.AssertStringDoesContain(t, "command", cmd, "--out_vendor out/soong/.intermediates/images/vendor_image/android_common/vendor_image")
	android.AssertStringDoesContain(t, "command", cmd, "--out_odm out/soong/.intermediates/images/vendor_image/android_common/vendor_image/odm")

	// Check that there is no --out_system flag when the device has no system image.
	android.AssertStringDoesNotContain(t, "command", cmd, "--out_system")

	// Check that the command contains flags for all the passwd and property files, even for partitions
	// that aren't being built (matching the behavior of host_init_verifier.mk).
	android.AssertStringDoesContain(t, "command", cmd, "-p out/soong/.intermediates/images/passwd_system/android_arm64_armv8-a/passwd_system")
	android.AssertStringDoesContain(t, "command", cmd, "--property-contexts=out/soong/.intermediates/images/plat_property_contexts/android_arm64_armv8-a/plat_property_contexts")

	android.AssertStringDoesContain(t, "command", cmd, "-p out/soong/.intermediates/images/passwd_system_ext/android_arm64_armv8-a/passwd_system_ext")
	android.AssertStringDoesContain(t, "command", cmd, "--property-contexts=out/soong/.intermediates/images/system_ext_property_contexts/android_arm64_armv8-a/system_ext_property_contexts")

	android.AssertStringDoesContain(t, "command", cmd, "-p out/soong/.intermediates/images/passwd_vendor/android_arm64_armv8-a/passwd_vendor")
	android.AssertStringDoesContain(t, "command", cmd, "--property-contexts=out/soong/.intermediates/images/vendor_property_contexts/android_arm64_armv8-a/vendor_property_contexts")

	android.AssertStringDoesContain(t, "command", cmd, "-p out/soong/.intermediates/images/passwd_odm/android_arm64_armv8-a/passwd_odm")
	android.AssertStringDoesContain(t, "command", cmd, "--property-contexts=out/soong/.intermediates/images/odm_property_contexts/android_arm64_armv8-a/odm_property_contexts")

	android.AssertStringDoesContain(t, "command", cmd, "-p out/soong/.intermediates/images/passwd_product/android_arm64_armv8-a/passwd_product")
	android.AssertStringDoesContain(t, "command", cmd, "--property-contexts=out/soong/.intermediates/images/product_property_contexts/android_arm64_armv8-a/product_property_contexts")
}
