#!/usr/bin/env python3
#
# Copyright 2025 The Android Open Source Project
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

import argparse
import glob
import hashlib
import os
import shutil
import stat
import struct
import subprocess
import sys
import zipfile

from ninja_determinism_test import Product, get_top, transitively_included_ninja_files

# Equivalent of soong's IsEnvTrue
def is_env_true(e: str) -> bool:
    value = os.environ.get(e, '').lower()
    return value == '1' or value == 'y' or value == 'yes' or value == 'on' or value == 'true'

def run_build_target_files_zip(product: Product, soong_only: bool) -> bool:
    """Runs a build and returns if it succeeded or not."""
    soong_only_arg = '--no-soong-only'
    if soong_only:
        soong_only_arg = '--soong-only'

    out_dir = os.getenv('OUT_DIR', 'out')

    if not os.path.exists(out_dir):
        os.mkdir(out_dir)

    # inner function so we can early return but not miss the final
    # move_artifacts_to_subfolder()
    def inner():
        with open(os.path.join(out_dir, 'build.log'), 'wb') as f:
            result = subprocess.run([
                'build/soong/soong_ui.bash',
                '--make-mode',
                'USE_RBE=true',
                'BUILD_DATETIME=1',
                'USE_FIXED_TIMESTAMP_IMG_FILES=true',
                'DISABLE_NOTICE_XML_GENERATION=true',
                f'TARGET_PRODUCT={product.product}',
                f'TARGET_RELEASE={product.release}',
                f'TARGET_BUILD_VARIANT={product.variant}',
                'installclean',
                soong_only_arg,
            ], stdout=f, stderr=subprocess.STDOUT, env=os.environ)

            if result.returncode != 0:
                return False

        with open(os.path.join(out_dir, 'build.log'), 'wb') as f:
            result = subprocess.run([
                'build/soong/soong_ui.bash',
                '--make-mode',
                'USE_RBE=true',
                'BUILD_DATETIME=1',
                'USE_FIXED_TIMESTAMP_IMG_FILES=true',
                'DISABLE_NOTICE_XML_GENERATION=true',
                f'TARGET_PRODUCT={product.product}',
                f'TARGET_RELEASE={product.release}',
                f'TARGET_BUILD_VARIANT={product.variant}',
                'target-files-package',
                'droid',
                soong_only_arg,
            ], stdout=f, stderr=subprocess.STDOUT, env=os.environ)

            if result.returncode != 0:
                return False

        with open(os.path.join(out_dir, 'build.log2'), 'wb') as f:
            # Split the dist into a separate invocation to limit dist to target_files.zip
            # This is expected to be faster than disting all droid.
            result = subprocess.run([
                'build/soong/soong_ui.bash',
                '--make-mode',
                'USE_RBE=true',
                'BUILD_DATETIME=1',
                'USE_FIXED_TIMESTAMP_IMG_FILES=true',
                'DISABLE_NOTICE_XML_GENERATION=true',
                f'TARGET_PRODUCT={product.product}',
                f'TARGET_RELEASE={product.release}',
                f'TARGET_BUILD_VARIANT={product.variant}',
                'target-files-package',
                'dist',
                soong_only_arg,
            ], stdout=f, stderr=subprocess.STDOUT, env=os.environ)

        with open(os.path.join(out_dir, 'build.log2'), 'rb') as f:
            log2_contents = f.read()
        with open(os.path.join(out_dir, 'build.log'), 'ab') as f:
            f.write(b"\n")
            f.write(log2_contents)

        if result.returncode != 0:
            return False

        return True

    succeeded = inner()
    move_artifacts_to_subfolder(product, soong_only)
    return succeeded

# These values are defined in build/soong/zip/zip.go
SHA_256_HEADER_ID = 0x4967
SHA_256_HEADER_SIGNATURE = 0x9514

def get_local_file_sha256_fields(zip_filepath: os.PathLike) -> dict[str, bytes]:
    if not os.path.exists(zip_filepath):
        print(f"Error: File not found at {zip_filepath}", file=sys.stderr)
        return None

    sha256_checksums: dict[str, bytes] = {}

    with zipfile.ZipFile(zip_filepath, 'r') as zip_ref:
        infolist = zip_ref.infolist()

        for member_info in infolist:
            # Skip if the entry is a directory.
            if member_info.is_dir():
                continue

            local_extra_data = member_info.extra

            i = 0
            found_sha_in_file = None
            while i + 4 <= len(local_extra_data): # Need at least 4 (header ID + data size)
                block_header_id, block_data_size = struct.unpack('<HH', local_extra_data[i:i+4])

                current_block_end = i + 4 + block_data_size

                # Check if the block is SHA256 block
                if block_header_id == SHA_256_HEADER_ID:
                    if block_data_size >= 2:
                        data_bytes = local_extra_data[i+4 : current_block_end]

                        # Check internal signature
                        internal_sig = struct.unpack('<H', data_bytes[0:2])[0]
                        if internal_sig == SHA_256_HEADER_SIGNATURE:
                            found_sha_in_file = data_bytes[2:]
                            break

                i += (4 + block_data_size)

            if found_sha_in_file:
                sha256_checksums[member_info.filename] = found_sha_in_file
            elif member_info.external_attr != 0:
                # Upper 16 bits of external_attr are UNIX permissions.
                # If the file is a symlink then add its target as the value of the map.
                mode = (member_info.external_attr >> 16) & 0xFFFF
                if stat.S_ISLNK(mode):
                    target = zip_ref.read(member_info.filename)
                    sha256_checksums[member_info.filename] = target
            else:
                print(f"{member_info.filename} sha not found", file=sys.stderr)

    return sha256_checksums

def find_build_id() -> str | None:
    tag_file_path = os.path.join(os.getenv('OUT_DIR', 'out'), 'file_name_tag.txt')
    build_id = None

    with open(tag_file_path, 'r', encoding='utf-8') as f:
            build_id = f.read().strip()

    return build_id


def get_sub_dist_dir(product: Product, soong_only: bool = None) -> str:
    subdir = os.path.join('soong_only_diffs', product.product)
    if soong_only is not None:
        subdir = os.path.join(subdir, "soong_only" if soong_only else "soong_plus_make")

    out_dir = os.getenv('OUT_DIR', 'out')
    dist_dir = os.getenv('DIST_DIR', os.path.join(out_dir, 'dist'))
    subdistdir = os.path.join(dist_dir, subdir)
    return subdistdir


def zip_ninja_files(subdistdir: str, product: Product):
    out_dir = os.getenv('OUT_DIR', 'out')
    root_dir = os.path.dirname(out_dir)
    root_ninja_name = f'combined-{product.product}.ninja'
    if is_env_true('EMMA_INSTRUMENT'):
        root_ninja_name = f'combined-{product.product}.coverage.ninja'
    root_ninja_name = os.path.join(out_dir, root_ninja_name)
    if not os.path.isfile(root_ninja_name):
        return
    files_to_zip = transitively_included_ninja_files(out_dir, root_ninja_name, {})

    zip_filename = os.path.join(subdistdir, "ninja_files.zip")
    with zipfile.ZipFile(zip_filename, 'w', compression=zipfile.ZIP_DEFLATED) as zipf:
        for file in files_to_zip:
            zipf.write(filename=file, arcname=os.path.relpath(file, root_dir))

def move_artifacts_to_subfolder(product: Product, soong_only: bool):
    out_dir = os.getenv('OUT_DIR', 'out')
    dist_dir = os.getenv('DIST_DIR', os.path.join(out_dir, 'dist'))
    subdistdir = get_sub_dist_dir(product, soong_only)
    if os.path.exists(subdistdir):
        shutil.rmtree(subdistdir)
    os.makedirs(subdistdir)
    zip_ninja_files(subdistdir, product)

    build_id = find_build_id()

    files_to_move = [
        os.path.join(dist_dir, f'{product.product}-target_files-{build_id}.zip'), # target_files.zip
        os.path.join(out_dir, 'build.log'),
    ]

    for file in files_to_move:
        if os.path.isfile(file):
            shutil.move(file, subdistdir)

SHA_DIFF_ALLOWLIST = {
    "IMAGES/system.img",
    "IMAGES/userdata.img",
    "IMAGES/vbmeta_system.img",
    "META/apkcerts.txt",
    "META/misc_info.txt",
    "META/vbmeta_digest.txt",
}

def get_comparison_report_path(product: Product):
    return os.path.join(get_sub_dist_dir(product), 'comparison_report.txt')

def compare_sha_maps(product: Product, soong_only_map: dict[str, bytes], soong_plus_make_map: dict[str, bytes]) -> bool:
    """Compares two sha maps and reports any missing or different entries."""
    all_keys = sorted(list(soong_only_map.keys() | soong_plus_make_map.keys()))
    all_identical = True
    with open(get_comparison_report_path(product), 'wt') as file:
        for key in all_keys:
            allowlisted = key in SHA_DIFF_ALLOWLIST
            allowlisted_str = "ALLOWLISTED" if allowlisted else "NOT ALLOWLISTED"
            if key not in soong_only_map:
                print(f'{key} not found in soong only build target_files.zip ({allowlisted_str})', file=file)
                all_identical = all_identical and allowlisted
            elif key not in soong_plus_make_map:
                print(f'{key} not found in soong plus make build target_files.zip ({allowlisted_str})', file=file)
                all_identical = all_identical and allowlisted
            elif soong_only_map[key] != soong_plus_make_map[key]:
                print(f'{key} sha value differ between soong only build and soong plus make build ({allowlisted_str})', file=file)
                all_identical = all_identical and allowlisted

    return all_identical

def get_zip_sha_map(product: Product, soong_only: bool) -> dict[str, bytes]:
    """Runs the build and returns the map of entries to its SHA256 values of target_files.zip."""
    subdistdir = get_sub_dist_dir(product, soong_only)
    target_files_zip_glob = os.path.join(subdistdir, f'{product.product}-target_files-*.zip')
    target_files_zip = glob.glob(target_files_zip_glob)
    if len(target_files_zip) != 1:
        sys.exit(f'Could not find {target_files_zip_glob}')
    zip_sha_map = get_local_file_sha256_fields(target_files_zip[0])
    if zip_sha_map is None:
        sys.exit("Could not construct sha map for target_files.zip entries for soong only build")

    return zip_sha_map

_INSTALLED_IMG_FILES = [
    "boot.img",
    "bootloader.img",
    "dtbo.img",
    "product.img",
    "pvmfw.img",
    "ramdisk.img",
    "system_dlkm.img",
    "system_ext.img",
    "system_other.img",
    "system.img",
    "userdata.img",
    "vbmeta.img",
    "vbmeta_system.img",
    "vbmeta_vendor.img",
    "vendor_boot.img",
    "vendor_dlkm.img",
    "vendor.img",
    "vendor_kernel_boot.img",
    "vendor_ramdisk.img",
]

# TODO (b/435530838): Remove this allowlist.
_INSTALLED_IMG_FILES_SHA_DIFF_ALLOWLIST = [
    "userdata.img",
]

def get_installed_img_sha(path: str) -> str:
    """Returns the SHA256 value of a file."""
    sha256_hash = hashlib.sha256()
    chunk_size = 1024 * 1024 # 1 MB
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(chunk_size), b""):
            sha256_hash.update(chunk)
        return sha256_hash.hexdigest()

def get_installed_img_sha_map(product: Product) -> dict[str, str]:
    """Returns the map of installed .img to its SHA256 value."""
    out_dir = os.getenv('OUT_DIR', 'out')
    install_dir = os.path.join(out_dir, "target", "product", product.product)
    zip_sha_map = {}
    for img in _INSTALLED_IMG_FILES:
        img_path = os.path.join(install_dir, img)
        # Some devices do not build partitions like dtbo.img
        # Skip if .img file is not found in install dir.
        if os.path.exists(img_path):
            zip_sha_map[img] = get_installed_img_sha(img_path)

    return zip_sha_map

def compare_installed_img_sha_maps(product: Product, soong_only_map: dict[str, str], soong_plus_make_map: dict[str, str]) -> bool:
    """Compares two sha maps of installed .img files and reports any missing or different entries."""
    all_keys = sorted(list(soong_only_map.keys() | soong_plus_make_map.keys()))
    all_identical = True
    # Append diffs to report.
    with open(get_comparison_report_path(product), 'at') as file:
        for key in all_keys:
            allowlisted = key in _INSTALLED_IMG_FILES_SHA_DIFF_ALLOWLIST
            allowlisted_str = "ALLOWLISTED" if allowlisted else "NOT ALLOWLISTED"
            if key not in soong_only_map:
                print(f'$ANDROID_PRODUCT_OUT/{key} not found in soong only droid builds ({allowlisted_str})', file=file)
                all_identical = all_identical and allowlisted
            elif key not in soong_plus_make_map:
                print(f'$ANDROID_PRODUCT_OUT/{key} not found in soong plus make droid builds ({allowlisted_str})', file=file)
                all_identical = all_identical and allowlisted
            elif soong_only_map[key] != soong_plus_make_map[key]:
                print(f'$ANDROID_PRODUCT_OUT/{key} sha value differ between soong only build and soong plus make build ({allowlisted_str})', file=file)
                all_identical = all_identical and allowlisted

    return all_identical

def parse_args():
    parser = argparse.ArgumentParser()
    parser.add_argument("products", nargs='+', help="one or more target product names")
    return parser.parse_args()

def main():
    os.chdir(get_top())

    args = parse_args()

    products = [
        Product(
          p,
          'trunk_staging',
          'userdebug',
        ) for p in args.products
    ]

    target_files_differ_products = []
    soong_only_build_failed_products = []
    soong_plus_make_build_failed_products = []
    for product in products:
        soong_only = True
        soong_only_success = run_build_target_files_zip(product, soong_only)
        soong_only_zip_sha_map = None
        soong_only_installed_img_sha_map = None
        if soong_only_success:
            soong_only_zip_sha_map = get_zip_sha_map(product, soong_only)
            soong_only_installed_img_sha_map = get_installed_img_sha_map(product)
        else:
            soong_only_build_failed_products.append(product)

        soong_only = False
        soong_plus_make_success = run_build_target_files_zip(product, soong_only)
        soong_plus_make_zip_sha_map = None
        soong_plus_make_installed_img_sha_map = None
        if soong_plus_make_success:
            soong_plus_make_zip_sha_map = get_zip_sha_map(product, soong_only)
            soong_plus_make_installed_img_sha_map = get_installed_img_sha_map(product)
        else:
            soong_plus_make_build_failed_products.append(product)

        if soong_only_zip_sha_map and soong_plus_make_zip_sha_map:
            if not compare_sha_maps(product, soong_only_zip_sha_map, soong_plus_make_zip_sha_map):
                target_files_differ_products.append(product)

        if soong_only_installed_img_sha_map and soong_plus_make_installed_img_sha_map:
            if not compare_installed_img_sha_maps(product, soong_only_installed_img_sha_map, soong_plus_make_installed_img_sha_map):
                target_files_differ_products.append(product)

        print(f"Diff test for {product.product} completed.")

    for p in soong_plus_make_build_failed_products:
        print(f"{p.product}: soong+make build failed", file=sys.stderr)
    for p in soong_only_build_failed_products:
        print(f"{p.product}: soong-only build failed", file=sys.stderr)
    for p in target_files_differ_products:
        print(f"{p.product}: target-file.zip and/or $ANDROID_PRODUCT_OUT differs", file=sys.stderr)

    if len(products) == 1:
        report = get_comparison_report_path(products[0])
        if os.path.isfile(report):
            with open(report) as f:
                print(f.read(), file=sys.stderr)

    if soong_plus_make_build_failed_products or soong_only_build_failed_products or target_files_differ_products:
        sys.exit(1)
    else:
        print("target_files.zip are identical between soong only build and soong plus make build")

if __name__ == "__main__":
    main()
