#!/bin/bash

# Copyright (C) 2023 The Android Open Source Project
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

set -uo pipefail

# Integration test for verifying generated SBOM for cuttlefish device.

if [ ! -e "build/make/core/Makefile" ]; then
  echo "$0 must be run from the top of the Android source tree."
  exit 1
fi

function setup {
  tmp_dir="$(mktemp -d tmp.XXXXXX)"
  trap 'cleanup "${tmp_dir}"' EXIT
  echo "${tmp_dir}"
}

function cleanup {
  tmp_dir="$1"; shift
  rm -rf "${tmp_dir}"
}

function run_soong {
  local out_dir="$1"; shift
  local targets="$1"; shift
  if [ "$#" -ge 1 ]; then
    local apps=$1; shift
    SOONG_ONLY=false TARGET_PRODUCT="${target_product}" TARGET_RELEASE="${target_release}" TARGET_BUILD_VARIANT="${target_build_variant}" OUT_DIR="${out_dir}" TARGET_BUILD_UNBUNDLED=true TARGET_BUILD_APPS=$apps \
        build/soong/soong_ui.bash --make-mode ${targets}
  else
    TARGET_PRODUCT="${target_product}" TARGET_RELEASE="${target_release}" TARGET_BUILD_VARIANT="${target_build_variant}" OUT_DIR="${out_dir}" \
        build/soong/soong_ui.bash --make-mode ${targets}
  fi
}

function diff_files {
  local file_list_file="$1"; shift
  local files_in_spdx_file="$1"; shift
  local partition_name="$1"; shift
  local exclude="$1"; shift

  diff "$file_list_file" "$files_in_spdx_file" $exclude
  if [ $? != "0" ]; then
   echo Found diffs in $f and SBOM.
   exit 1
  else
   echo No diffs.
  fi
}

function test_sbom_aosp_cf_x86_64_phone {
  # Setup
  out_dir="$(setup)"

  # Test
  # m droid, build sbom later in case additional dependencies might be built and included in partition images.
  run_soong "${out_dir}" "droid dump.erofs lz4"

  soong_sbom_out=$out_dir/soong/sbom/$target_product
  product_out=$out_dir/target/product/vsoc_x86_64
  sbom_test=$product_out/sbom_test
  mkdir -p $sbom_test
  cp $product_out/*.img $sbom_test

  # m sbom
  run_soong "${out_dir}" "sbom"

  # Generate installed file list from .img files in PRODUCT_OUT
  dump_erofs=$out_dir/host/linux-x86/bin/dump.erofs
  lz4=$out_dir/host/linux-x86/bin/lz4

  declare -A diff_excludes

  # Example output of dump.erofs is as below, and the data used in the test start
  # at line 11. Column 1 is inode id, column 2 is inode type and column 3 is name.
  # Each line is captured in variable "entry", awk is used to get type and name.
  # Output of dump.erofs:
  #     File : /
  #     Size: 160  On-disk size: 160  directory
  #     NID: 39   Links: 10   Layout: 2   Compression ratio: 100.00%
  #     Inode size: 64   Extent size: 0   Xattr size: 16
  #     Uid: 0   Gid: 0  Access: 0755/rwxr-xr-x
  #     Timestamp: 2023-02-14 01:15:54.000000000
  #
  #            NID TYPE  FILENAME
  #             39    2  .
  #             39    2  ..
  #             47    2  app
  #        1286748    2  bin
  #        1286754    2  etc
  #        5304814    2  lib
  #        5309056    2  lib64
  #        5309130    2  media
  #        5388910    2  overlay
  #        5479537    2  priv-app
  EROFS_IMAGES="\
    $sbom_test/product.img \
    $sbom_test/system.img \
    $sbom_test/system_ext.img \
    $sbom_test/system_dlkm.img \
    $sbom_test/system_other.img \
    $sbom_test/odm.img \
    $sbom_test/odm_dlkm.img \
    $sbom_test/vendor.img \
    $sbom_test/vendor_dlkm.img"
  for f in $EROFS_IMAGES; do
    partition_name=$(basename $f | cut -d. -f1)
    file_list_file="${sbom_test}/sbom-${partition_name}-files.txt"
    files_in_soong_spdx_file="${sbom_test}/soong-sbom-${partition_name}-files-in-spdx.txt"
    rm "$file_list_file" > /dev/null 2>&1 || true
    all_dirs="/"
    while [ ! -z "$all_dirs" ]; do
      dir=$(echo "$all_dirs" | cut -d ' ' -f1)
      all_dirs=$(echo "$all_dirs" | cut -d ' ' -f1 --complement -s)
      entries=$($dump_erofs --ls --path "$dir" $f | tail -n +11)
      while read -r entry; do
        inode_type=$(echo $entry | awk -F ' ' '{print $2}')
        name=$(echo $entry | awk -F ' ' '{print $3}')
        case $inode_type in
          "2")  # directory
            all_dirs=$(echo "$all_dirs $dir/$name" | sed 's/^\s*//')
            ;;
          "1"|"7")  # 1: file, 7: symlink
            (
            if [ "$partition_name" != "system" ]; then
              # system partition is mounted to /, not to prepend partition name.
              printf %s "/$partition_name"
            fi
            echo "$dir/$name" | sed 's#^//#/#'
            ) >> "$file_list_file"
            ;;
        esac
      done <<< "$entries"
    done
    sort -n -o "$file_list_file" "$file_list_file"

    # Diff the file list from image and file list in SBOM created by Soong
    grep "FileName: /${partition_name}/" $soong_sbom_out/sbom.spdx | sed 's/^FileName: //' > "$files_in_soong_spdx_file"
    if [ "$partition_name" = "system" ]; then
      # system partition is mounted to /, so include FileName starts with /root/ too.
      grep "FileName: /root/" $soong_sbom_out/sbom.spdx | sed 's/^FileName: \/root//' >> "$files_in_soong_spdx_file"
    fi
    sort -n -o "$files_in_soong_spdx_file" "$files_in_soong_spdx_file"

    echo ============ Diffing files in $f and SBOM created by Soong
    exclude=
    if [ -v 'diff_excludes[$partition_name]' ]; then
     exclude=${diff_excludes[$partition_name]}
    fi
    diff_files "$file_list_file" "$files_in_soong_spdx_file" "$partition_name" "$exclude"
  done

  RAMDISK_IMAGES="$product_out/ramdisk.img"
  for f in $RAMDISK_IMAGES; do
    partition_name=$(basename $f | cut -d. -f1)
    file_list_file="${sbom_test}/sbom-${partition_name}-files.txt"
    files_in_soong_spdx_file="${sbom_test}/sbom-${partition_name}-files-in-soong-spdx.txt"
    # lz4 decompress $f to stdout
    # cpio list all entries like ls -l
    # grep filter normal files and symlinks
    # awk get entry names
    # sed remove partition name from entry names
    $lz4 -c -d $f | cpio -tv 2>/dev/null | grep '^[-l]' | awk -F ' ' '{print $9}' | sed "s:^:/$partition_name/:" | sort -n > "$file_list_file"

    grep "FileName: /${partition_name}/" $soong_sbom_out/sbom.spdx | sed 's/^FileName: //' | sort -n > "$files_in_soong_spdx_file"

    echo ============ Diffing files in $f and SBOM created by Soong
    diff_files "$file_list_file" "$files_in_soong_spdx_file" "$partition_name" ""
  done

  verify_package_verification_code "$soong_sbom_out/sbom.spdx"

  verify_packages_licenses "$soong_sbom_out/sbom.spdx"

  # Teardown
  cleanup "${out_dir}"
}

function verify_package_verification_code {
  local sbom_file="$1"; shift

  local -a file_checksums
  local package_product_found=
  while read -r line;
  do
    if grep -q 'PackageVerificationCode' <<<"$line"
    then
      package_product_found=true
    fi
    if [ -n "$package_product_found" ]
    then
      if grep -q 'FileChecksum' <<< "$line"
      then
        checksum=$(echo $line | sed 's/^.*: //')
        file_checksums+=("$checksum")
      fi
    fi
  done <<< "$(grep -E 'PackageVerificationCode|FileChecksum' $sbom_file)"
  IFS=$'\n' file_checksums=($(sort <<<"${file_checksums[*]}")); unset IFS
  IFS= expected_package_verification_code=$(printf "${file_checksums[*]}" | sha1sum | sed 's/[[:space:]]*-//'); unset IFS

  actual_package_verification_code=$(grep PackageVerificationCode $sbom_file | sed 's/PackageVerificationCode: //g')
  if [ $actual_package_verification_code = $expected_package_verification_code ]
  then
    echo "Package verification code is correct."
  else
    echo "Unexpected package verification code."
    exit 1
  fi
}

function verify_packages_licenses {
  local sbom_file="$1"; shift

  num_of_packages=$(grep 'PackageName:' $sbom_file | wc -l)
  num_of_declared_licenses=$(grep 'PackageLicenseDeclared:' $sbom_file | wc -l)
  if [ "$num_of_packages" = "$num_of_declared_licenses" ]
  then
    echo "Number of packages with declared license is correct."
  else
    echo "Number of packages with declared license is WRONG."
    exit 1
  fi

  # PRODUCT and 4 prebuilt packages have "PackageLicenseDeclared: NOASSERTION"
  # All other packages have declared licenses
  num_of_packages_with_noassertion_license=$(grep 'PackageLicenseDeclared: NOASSERTION' $sbom_file | wc -l)
  if [ $num_of_packages_with_noassertion_license -lt 10 ]
  then
    echo "Number of packages with NOASSERTION license is correct."
  else
    echo "Number of packages with NOASSERTION license is WRONG."
    exit 1
  fi

  num_of_files=$(grep 'FileName:' $sbom_file | wc -l)
  num_of_concluded_licenses=$(grep 'LicenseConcluded:' $sbom_file | wc -l)
  if [ "$num_of_files" = "$num_of_concluded_licenses" ]
  then
    echo "Number of files with concluded license is correct."
  else
    echo "Number of files with concluded license is WRONG."
    exit 1
  fi
}

function test_sbom_unbundled_modules {
  # Setup
  out_dir="$(setup)"

  APEXES="\
    com.google.android.adbd \
    com.google.android.adservices \
    com.google.android.appsearch \
    com.google.android.art \
    com.google.android.bt \
    com.google.android.cellbroadcast \
    com.google.android.configinfrastructure \
    com.google.android.conscrypt \
    com.google.android.crashrecovery \
    com.google.android.extservices \
    com.google.android.healthfitness \
    com.google.android.ipsec \
    com.google.android.media \
    com.google.android.media.swcodec \
    com.google.android.mediaprovider \
    com.google.android.neuralnetworks \
    com.google.android.nfcservices \
    com.google.android.ondevicepersonalization \
    com.google.android.os.statsd \
    com.google.android.permission \
    com.google.android.profiling \
    com.google.android.resolv \
    com.google.android.rkpd \
    com.google.android.scheduling \
    com.google.android.sdkext \
    com.google.android.tethering \
    com.google.android.tzdata6 \
    com.google.android.uprobestats \
    com.google.android.uwb \
    com.google.android.wifi"

  APKS="\
    CaptivePortalLoginGoogle \
    DocumentsUIGoogle \
    GoogleExtServices \
    GooglePermissionController \
    NetworkStackGoogle \
    NetworkStackNextGoogle"

  # run_soong to build com.android.adbd.apex
  run_soong "${out_dir}" "sbom dist apex-ls deapexer debugfs fsck.erofs" "${APEXES} ${APKS}"

  apex_ls=${out_dir}/host/linux-x86/bin/apex-ls
  deapexer=${out_dir}/host/linux-x86/bin/deapexer
  debugfs=${out_dir}/host/linux-x86/bin/debugfs
  fsckerofs=${out_dir}/host/linux-x86/bin/fsck.erofs
  dist_dir=${DIST_DIR-${out_dir}/dist}
  diff_found=false
  for apex_name in ${APEXES}; do
    # Verify file list
    apex_file=${out_dir}/target/product/module_arm64/system/apex/${apex_name}.apex
    sbom_file=${dist_dir}/sbom/${apex_name}.apex.spdx.json
    echo "============ Diffing files in $apex_file and SBOM"
    set +e
    # apex-ls prints the list of all files and directories
    # grep removes directories
    # sed removes leading ./ in file names
    diff <(${apex_ls} ${apex_file} | grep -v "/$" | grep -v "apex_manifest.pb$" | sed -E 's#^\./(.*)#\1#' | sort -n) \
         <(grep '"fileName": ' ${sbom_file} | sed -E 's/.*"fileName": "(.*)",/\1/' | grep -v "${apex_name}.apex" | grep -v '\.[a]$' | grep -v '/android_.*\.o$' | grep -v '\.rlib$' | grep -v '/android_common_.*\.jar$' | grep -v '/linux_glibc_common/.*\.jar$' | sort -n )

    if [ $? != "0" ]; then
      echo "Diffs found in $apex_file and SBOM"
      diff_found=true
    else
      echo "No diffs."
    fi
    set -e

    # Verify checksum of files
    apex_unzipped=${out_dir}/target/product/module_arm64/system/apex/${apex_name}_unziped
    rm -rf ${apex_unzipped}
    ${deapexer} --debugfs_path ${debugfs} --fsckerofs_path ${fsckerofs} extract ${apex_file} ${apex_unzipped}

    apex_file_checksum_in_sbom=
    declare -A checksums_in_sbom
    while read -r filename; do
      read -r checksum
      case ${filename} in
        /*) # apex file
          apex_file_checksum_in_sbom=${checksum}
          ;;
        *) # files in apex
          checksums_in_sbom[${filename}]=${checksum}
          ;;
      esac
    done <<< "$(grep -E '("fileName":)|("checksumValue":)'  ${sbom_file} | sed -E 's/(.*"fileName": |.*"checksumValue": )"(.*)",?/\2/')"

    checksum_is_wrong=false
    while read -r filename; do
        file_sha1=$(sha1sum ${apex_unzipped}/${filename} | cut -d' ' -f1)
        if [ "${file_sha1}" != "${checksums_in_sbom[$filename]}" ]; then
          echo "Checksum is wrong: ${apex_file}#${filename}"
          checksum_is_wrong=true
        fi
    done <<< "$(find ${apex_unzipped} -mindepth 1 -type f -printf '%P\n' | grep -v "^apex_manifest.pb$")"

    apex_file_sha1=$(sha1sum ${apex_file} | cut -d' ' -f1)
    if [ "${apex_file_sha1}" != "${apex_file_checksum_in_sbom}" ]; then
      echo "Checksum is wrong: ${apex_file}"
      checksum_is_wrong=true
    fi

    if [ "${checksum_is_wrong}" = "true" ]; then
      diff_found=true
    else
      echo "Checksums are OK."
    fi
  done

  # Verify SBOM of APKs
  for apk in ${APKS}; do
    sbom_file=${dist_dir}/sbom/${apk}.apk.spdx.json
    echo "============ Diffing files in ${apk}.apk and SBOM"
    # There is only one file in SBOM of APKs
    file_number=$(grep '"fileName": ' ${sbom_file} | sed -E 's/.*"fileName": "(.*)",/\1/' | wc -l)
    if [ "$file_number" != "1" ]; then
      echo "Diffs found in $sbom_file"
      diff_found=true
    else
      echo "No diffs."
    fi
  done

  if [ $diff_found = "true" ]; then
    echo "Diff found, exit with error."
    exit 1
  fi

  # Teardown
  cleanup "${out_dir}"
}

target_product=aosp_cf_x86_64_phone
target_release=trunk_staging
target_build_variant=eng
for i in "$@"; do
  case $i in
    TARGET_PRODUCT=*)
      target_product=${i#*=}
      shift
      ;;
    TARGET_RELEASE=*)
      target_release=${i#*=}
      shift
      ;;
    TARGET_BUILD_VARIANT=*)
      target_build_variant=${i#*=}
      shift
      ;;
    *)
      echo "Unknown command line arguments: $i"
      exit 1
      ;;
  esac
done

echo "target product: $target_product, target_release: $target_release, target build variant: $target_build_variant"
case $target_product in
  aosp_cf_x86_64_phone)
    test_sbom_aosp_cf_x86_64_phone
    ;;
  module_arm64)
    test_sbom_unbundled_modules
    ;;
  *)
    echo "Unknown TARGET_PRODUCT: $target_product"
    exit 1
    ;;
esac