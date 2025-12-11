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

package filesystem

import (
	"android/soong/android"
	"android/soong/cc"
	"android/soong/etc"
	"android/soong/phony"
)

var PrepareForTestWithFilesystemBuildComponents = android.FixtureRegisterWithContext(registerBuildComponents)
var prepareForTestWithAndroidDeviceComponents = android.GroupFixturePreparers(
	android.FixtureRegisterWithContext(func(ctx android.RegistrationContext) {
		ctx.RegisterModuleType("android_device", AndroidDeviceFactory)
		ctx.RegisterModuleType("build_prop", android.BuildPropFactory)
	}),
	phony.PrepareForTestWithPhony,
	cc.PrepareForTestWithCcBuildComponents,
	cc.PrepareForTestWithCcDefaultModules,
	etc.PrepareForTestWithPrebuiltEtc,
	android.SetBuildDateFileEnvVarForTests(),
	android.FixtureMergeMockFs(android.MockFS{
		"images/Android.bp": []byte(`
			android_filesystem {
				name: "system_image",
				base_dir: "system",
			}
			android_filesystem {
				name: "vendor_image",
			}
			android_filesystem {
				name: "product_image",
			}
			android_filesystem {
				name: "odm_image",
			}
			android_filesystem {
				name: "system_ext_image",
			}

			prebuilt_etc { name: "passwd_system", src: "passwd" }
			prebuilt_etc { name: "passwd_system_ext", src: "passwd" }
			prebuilt_etc { name: "passwd_vendor", src: "passwd" }
			prebuilt_etc { name: "passwd_odm", src: "passwd" }
			prebuilt_etc { name: "passwd_product", src: "passwd" }

			prebuilt_etc { name: "plat_property_contexts", src: "props" }
			prebuilt_etc { name: "system_ext_property_contexts", src: "props" }
			prebuilt_etc { name: "vendor_property_contexts", src: "props" }
			prebuilt_etc { name: "odm_property_contexts", src: "props" }
			prebuilt_etc { name: "product_property_contexts", src: "props" }
		`),
		"android_device_files/Android.bp": []byte(`
			phony {
				name: "file_contexts_bin_gen",
			}
			cc_library {
				name: "liblz4",
				host_supported: true,
				stl: "none",
				system_shared_libs: [],
			}
		`),
		"prop/Android.bp": []byte(`
            build_prop {
                name: "system-build.prop",
                stem: "build.prop",
            }
		`),
	}),
)
