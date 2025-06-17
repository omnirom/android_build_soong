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

package fsgen

import (
	"android/soong/filesystem"

	"github.com/google/blueprint/proptools"
)

var (
	// Most of the symlinks and directories listed here originate from create_root_structure.mk,
	// but the handwritten generic system image also recreates them:
	// https://cs.android.com/android/platform/superproject/main/+/main:build/make/target/product/generic/Android.bp;l=33;drc=db08311f1b6ef6cb0a4fbcc6263b89849360ce04
	// TODO(b/377734331): only generate the symlinks if the relevant partitions exist
	commonSymlinksFromRoot = []filesystem.SymlinkDefinition{
		{
			Target: proptools.StringPtr("/system/bin/init"),
			Name:   proptools.StringPtr("init"),
		},
		{
			Target: proptools.StringPtr("/system/etc"),
			Name:   proptools.StringPtr("etc"),
		},
		{
			Target: proptools.StringPtr("/system/bin"),
			Name:   proptools.StringPtr("bin"),
		},
		{
			Target: proptools.StringPtr("/data/user_de/0/com.android.shell/files/bugreports"),
			Name:   proptools.StringPtr("bugreports"),
		},
		{
			Target: proptools.StringPtr("/sys/kernel/debug"),
			Name:   proptools.StringPtr("d"),
		},
		{
			Target: proptools.StringPtr("/product/etc/security/adb_keys"),
			Name:   proptools.StringPtr("adb_keys"),
		},
		{
			Target: proptools.StringPtr("/vendor/odm/app"),
			Name:   proptools.StringPtr("odm/app"),
		},
		{
			Target: proptools.StringPtr("/vendor/odm/bin"),
			Name:   proptools.StringPtr("odm/bin"),
		},
		{
			Target: proptools.StringPtr("/vendor/odm/etc"),
			Name:   proptools.StringPtr("odm/etc"),
		},
		{
			Target: proptools.StringPtr("/vendor/odm/firmware"),
			Name:   proptools.StringPtr("odm/firmware"),
		},
		{
			Target: proptools.StringPtr("/vendor/odm/framework"),
			Name:   proptools.StringPtr("odm/framework"),
		},
		{
			Target: proptools.StringPtr("/vendor/odm/lib"),
			Name:   proptools.StringPtr("odm/lib"),
		},
		{
			Target: proptools.StringPtr("/vendor/odm/lib64"),
			Name:   proptools.StringPtr("odm/lib64"),
		},
		{
			Target: proptools.StringPtr("/vendor/odm/overlay"),
			Name:   proptools.StringPtr("odm/overlay"),
		},
		{
			Target: proptools.StringPtr("/vendor/odm/priv-app"),
			Name:   proptools.StringPtr("odm/priv-app"),
		},
		{
			Target: proptools.StringPtr("/vendor/odm/usr"),
			Name:   proptools.StringPtr("odm/usr"),
		},
		// For Treble Generic System Image (GSI), system-as-root GSI needs to work on
		// both devices with and without /odm_dlkm partition. Those symlinks are for
		// devices without /odm_dlkm partition. For devices with /odm_dlkm
		// partition, mount odm_dlkm.img under /odm_dlkm will hide those symlinks.
		// Note that /odm_dlkm/lib is omitted because odm DLKMs should be accessed
		// via /odm/lib/modules directly. All of this also applies to the vendor_dlkm symlink
		{
			Target: proptools.StringPtr("/odm/odm_dlkm/etc"),
			Name:   proptools.StringPtr("odm_dlkm/etc"),
		},
		{
			Target: proptools.StringPtr("/vendor/vendor_dlkm/etc"),
			Name:   proptools.StringPtr("vendor_dlkm/etc"),
		},
	}

	// Common directories between partitions that may be listed as `Dirs` property in the
	// filesystem module.
	commonPartitionDirs = []string{
		// From generic_rootdirs in build/make/target/product/generic/Android.bp
		"apex",
		"bootstrap-apex",
		"config",
		"data",
		"data_mirror",
		"debug_ramdisk",
		"dev",
		"linkerconfig",
		"metadata",
		"mnt",
		"odm",
		"odm_dlkm",
		"oem",
		"postinstall",
		"proc",
		"second_stage_resources",
		"storage",
		"sys",
		"system",
		"system_dlkm",
		"tmp",
		"vendor",
		"vendor_dlkm",

		// from android_rootdirs in build/make/target/product/generic/Android.bp
		"system_ext",
		"product",
	}
)
