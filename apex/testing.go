// Copyright 2021 Google Inc. All rights reserved.
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

package apex

import "android/soong/android"

const testDefaultUpdatableModuleVersion = "340090000"

var PrepareForTestWithApexBuildComponents = android.GroupFixturePreparers(
	android.FixtureRegisterWithContext(registerApexBuildComponents),
	android.FixtureRegisterWithContext(registerApexKeyBuildComponents),
	android.FixtureRegisterWithContext(registerApexDepsInfoComponents),
	android.FixtureAddTextFile("all_apex_certs/Android.bp", `
		all_apex_certs { name: "all_apex_certs" }
	`),
	// Additional files needed in tests that disallow non-existent source files.
	// This includes files that are needed by all, or at least most, instances of an apex module type.
	android.MockFS{
		// Needed by apex.
		"system/core/rootdir/etc/public.libraries.android.txt": nil,
		"build/soong/scripts/gen_ndk_backedby_apex.sh":         nil,
		// Needed by prebuilt_apex.
		"build/soong/scripts/unpack-prebuilt-apex.sh": nil,
		// Needed by all_apex_certs
		"build/make/target/product/security/testkey.x509.pem": nil,
	}.AddToFixture(),
	android.PrepareForTestWithBuildFlag("RELEASE_DEFAULT_UPDATABLE_MODULE_VERSION", testDefaultUpdatableModuleVersion),
)
